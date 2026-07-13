package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewAssayClient_ReturnsNonNilClient verifies that newAssayClient creates
// a valid LLMClient for the given base URL and model name.
func TestNewAssayClient_ReturnsNonNilClient(t *testing.T) {
	client := newAssayClient("http://localhost:11434", "test-model")
	if client == nil {
		t.Fatal("newAssayClient returned nil")
	}
	_ = client.Close()
}

// TestListOpenAIModels_ParsesModelIDs verifies that listOpenAIModels extracts
// model IDs from the /v1/models JSON response.
func TestListOpenAIModels_ParsesModelIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"phi4-Q4_K_M"},{"id":"llama3.2:3b"}]}`)
	}))
	defer srv.Close()

	models, err := listOpenAIModels(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %v", len(models), models)
	}
	if models[0] != "phi4-Q4_K_M" {
		t.Errorf("expected phi4-Q4_K_M, got %s", models[0])
	}
	if models[1] != "llama3.2:3b" {
		t.Errorf("expected llama3.2:3b, got %s", models[1])
	}
}

// TestListOpenAIModels_SkipsEmptyIDs verifies that listOpenAIModels omits
// entries with an empty model ID.
func TestListOpenAIModels_SkipsEmptyIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"phi4"},{"id":""}]}`)
	}))
	defer srv.Close()

	models, err := listOpenAIModels(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 || models[0] != "phi4" {
		t.Errorf("expected [phi4], got %v", models)
	}
}

// TestListOpenAIModels_ErrorOnBadJSON verifies that listOpenAIModels returns
// an error when the server responds with malformed JSON.
func TestListOpenAIModels_ErrorOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer srv.Close()

	_, err := listOpenAIModels(srv.URL)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

// ─── buildGuideMessages ───────────────────────────────────────────────────────

func TestBuildGuideMessages_WithGuide(t *testing.T) {
	msgs := buildGuideMessages("Always use %w for error wrapping.", "Write a function.", true)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d: %v", len(msgs), msgs)
	}
	if msgs[0].Role != "system" || msgs[0].Content != "Always use %w for error wrapping." {
		t.Errorf("expected system message with guide text, got %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "Write a function." {
		t.Errorf("expected user message with prompt text, got %+v", msgs[1])
	}
}

func TestBuildGuideMessages_WithoutGuide(t *testing.T) {
	msgs := buildGuideMessages("Always use %w for error wrapping.", "Write a function.", false)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (user only), got %d: %v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Write a function." {
		t.Errorf("expected user message with prompt text, got %+v", msgs[0])
	}
}

func TestBuildGuideMessages_EmptyGuideTextFallsBackToPlain(t *testing.T) {
	msgs := buildGuideMessages("", "Write a function.", true)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (no empty system message sent), got %d: %v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user message, got %+v", msgs[0])
	}
}

// ─── writeReport (guide-compare) ──────────────────────────────────────────────

// TestWriteReport_GuideCompare_RendersDeltaTable is the first direct test of
// writeReport's rendering logic (none existed before this, for RagCompare
// either). It constructs a base/guide result pair where the guide variant
// fixes a failing check, and asserts the summary table shows the pass-count
// delta.
func TestWriteReport_GuideCompare_RendersDeltaTable(t *testing.T) {
	corpus := &Corpus{
		Prompts: []Prompt{
			{ID: "go-error-wrap", Category: "go", Description: "Error wrapping", Language: "go"},
		},
	}
	ar := AssayResults{
		RunAt:        time.Now(),
		Backend:      "Ollama",
		GuideCompare: true,
		GuideFile:    "/tmp/guide.txt",
		Results: []PromptResult{
			{
				PromptID: "go-error-wrap", Category: "go", Model: "llama3.2:3b", Variant: "base",
				Response:     "func Foo() {}",
				TokensPerSec: 5.0,
				Checks:       []CheckResult{{Name: "contains(%w)", Passed: false}},
				AutoPass:     false,
			},
			{
				PromptID: "go-error-wrap", Category: "go", Model: "llama3.2:3b", Variant: "guide",
				Response:     `func Foo() error { return fmt.Errorf("x: %w", err) }`,
				TokensPerSec: 4.5,
				Checks:       []CheckResult{{Name: "contains(%w)", Passed: true}},
				AutoPass:     true,
			},
		},
	}

	dir := t.TempDir()
	if err := writeReport(dir, ar, corpus); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	report := string(data)

	if !strings.Contains(report, "Base pass | Guide pass") {
		t.Errorf("expected a guide-compare summary header, got:\n%s", report)
	}
	if !strings.Contains(report, "0/1 | 1/1 | +1") {
		t.Errorf("expected a summary row with base=0/1, guide=1/1, delta=+1, got:\n%s", report)
	}
	if !strings.Contains(report, "Base response") || !strings.Contains(report, "Guide response") {
		t.Errorf("expected collapsed base/guide response sections, got:\n%s", report)
	}
	if !strings.Contains(report, "func Foo() {}") || !strings.Contains(report, `fmt.Errorf("x: %w", err)`) {
		t.Errorf("expected both variants' response bodies present, got:\n%s", report)
	}
}
