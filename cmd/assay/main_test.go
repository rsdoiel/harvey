package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	harvey "github.com/rsdoiel/harvey"
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

// H2 of exit-codes-plan.md: main is split into mainRun so the command line can
// be tested in process. Only usage errors change their exit status here (2).

func runAssay(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = mainRun(append([]string{"assay"}, args...), &o, &e)
	return code, o.String(), e.String()
}

func TestMainRun_VersionAndHelpExitZero(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"-v"}, {"-version"}, {"--help"}, {"-h"}, {"-help"}} {
		code, out, errOut := runAssay(t, args...)
		if code != 0 || out == "" || errOut != "" {
			t.Errorf("assay %v: exit %d, stdout %d bytes, stderr %q; want 0, some output, none",
				args, code, len(out), errOut)
		}
	}
	if _, out, _ := runAssay(t, "--version"); !strings.Contains(out, harvey.Version) {
		t.Errorf("--version prints %q, want it to contain %q", out, harvey.Version)
	}
}

func TestMainRun_UsageErrorsExitTwoWithNothingOnStdout(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		msg  string
	}{
		{"unknown flag", []string{"--bogus"}, "bogus"},
		{"flag missing its value", []string{"--corpus"}, "corpus"},
		{"bad integer value", []string{"--rag-top-k", "many"}, "rag-top-k"},
		{"surplus positional argument", []string{"extra"}, "unexpected argument: extra"},
		{"surplus after flags", []string{"--models", "m", "extra"}, "unexpected argument: extra"},
		{"--rag-compare needs --rag-db", []string{"--rag-compare"}, "--rag-compare requires --rag-db"},
		{"--guide-compare needs --guide-file", []string{"--guide-compare"}, "--guide-compare requires --guide-file"},
		{"the two compares conflict", []string{"--rag-compare", "--rag-db", "x", "--guide-compare", "--guide-file", "y"},
			"--guide-compare and --rag-compare are mutually exclusive"},
		{"the two local backends conflict", []string{"--llamafile", "x", "--llamacpp", "http://x"},
			"--llamafile and --llamacpp are mutually exclusive"},
	} {
		code, out, errOut := runAssay(t, tc.args...)
		if code != 2 {
			t.Errorf("%s: assay %v exit %d, want 2", tc.name, tc.args, code)
		}
		if out != "" {
			t.Errorf("%s: stdout %q, want nothing", tc.name, out)
		}
		if !strings.Contains(errOut, tc.msg) {
			t.Errorf("%s: stderr %q, want it to contain %q", tc.name, errOut, tc.msg)
		}
	}
}

// H4 of exit-codes-plan.md: assay's inputs, availability and outputs by class,
// and the bulk rule: a run does everything, then exits with the class of the
// first failed step. Failing automatic checks are results, not failures.

// fakeModelServer speaks the parts of an OpenAI-compatible server assay uses
// (/v1/chat/completions, streaming or not) and Ollama's /api/tags. A chat
// request for a model in fail gets an HTTP 500.
func fakeModelServer(t *testing.T, tags []string, fail ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			var models []map[string]string
			for _, n := range tags {
				models = append(models, map[string]string{"name": n})
			}
			json.NewEncoder(w).Encode(map[string]any{"models": models})
		case "/v1/chat/completions":
			var req struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			for _, f := range fail {
				if req.Model == f {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
			}
			if req.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"say hi"},"finish_reason":null}]}`+"\n\n")
				fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
				fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id": "1", "object": "chat.completion", "model": req.Model,
				"choices": []map[string]any{{"index": 0, "finish_reason": "stop",
					"message": map[string]string{"role": "assistant", "content": "say hi"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const oneCheckCorpus = `version: "1"
description: test
prompts:
  - id: p1
    category: cat-a
    description: d
    language: go
    prompt: say hi
    checks:
      contains: ["hi"]
    human: []
    notes: ""
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func closedURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

func TestMainRun_InputsAndAvailabilityByClass(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	bad := writeFile(t, dir, "bad.yaml", "prompts: [broken\n")
	empty := writeFile(t, dir, "empty.yaml", "version: \"1\"\nprompts: []\n")
	file := writeFile(t, dir, "afile", "x")
	up := fakeModelServer(t, nil) // Ollama that is running and has no models
	closed := closedURL(t)

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"corpus missing", []string{"--corpus", filepath.Join(dir, "nope.yaml"), "--models", "m", "--ollama", up.URL}, 66},
		{"corpus malformed", []string{"--corpus", bad, "--models", "m", "--ollama", up.URL}, 65},
		{"corpus with no prompts", []string{"--corpus", empty, "--models", "m", "--ollama", up.URL}, 65},
		{"category matches nothing", []string{"--corpus", corpus, "--models", "m", "--category", "nosuch", "--ollama", up.URL}, 1},
		{"guide file missing", []string{"--corpus", corpus, "--models", "m", "--guide-file", filepath.Join(dir, "nope"), "--ollama", up.URL}, 66},
		{"llamafile missing", []string{"--corpus", corpus, "--llamafile", filepath.Join(dir, "nope.llamafile")}, 66},
		{"llamafile of the wrong kind", []string{"--corpus", corpus, "--llamafile", file}, 66},
		{"ollama unreachable while listing models", []string{"--corpus", corpus, "--ollama", closed}, 69},
		{"llama.cpp unreachable while listing models", []string{"--corpus", corpus, "--llamacpp", closed}, 69},
		{"ollama up with no models", []string{"--corpus", corpus, "--ollama", up.URL}, 1},
		{"output directory cannot be created", []string{"--corpus", corpus, "--models", "m", "--ollama", up.URL, "--output", filepath.Join(file, "sub")}, 73},
	} {
		code, _, errOut := runAssay(t, tc.args...)
		if code != tc.want {
			t.Errorf("%s: exit %d, want %d\nstderr: %.300s", tc.name, code, tc.want, errOut)
		}
	}
}

// A RAG store that cannot be opened is a classified failure, never 70.
func TestMainRun_RagStoreThatCannotBeOpenedIsClassified(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	file := writeFile(t, dir, "afile", "x")
	up := fakeModelServer(t, nil)
	code, _, errOut := runAssay(t, "--corpus", corpus, "--models", "m", "--ollama", up.URL,
		"--rag-db", filepath.Join(file, "sub", "r.db"), "--output", filepath.Join(dir, "out"))
	if code == 0 || code == 70 {
		t.Errorf("exit %d, want a classified failure\nstderr: %.300s", code, errOut)
	}
}

func readResults(t *testing.T, outDir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "results.json"))
	if err != nil {
		t.Fatalf("results.json was not written: %v", err)
	}
	var ar struct {
		Results []map[string]any `json:"Results"`
	}
	if err := json.Unmarshal(data, &ar); err != nil {
		t.Fatal(err)
	}
	return ar.Results
}

// Working calls exit 0, including when the prompt's automatic checks fail:
// a failing check is a result of the evaluation.
func TestMainRun_FailingChecksAreResultsNotFailures(t *testing.T) {
	dir := t.TempDir()
	srv := fakeModelServer(t, nil)
	pass := writeFile(t, dir, "pass.yaml", oneCheckCorpus)
	fail := writeFile(t, dir, "fail.yaml", strings.Replace(oneCheckCorpus, `["hi"]`, `["never-said"]`, 1))
	for name, c := range map[string]string{"passing": pass, "failing": fail} {
		out := filepath.Join(dir, "out-"+name)
		code, _, errOut := runAssay(t, "--corpus", c, "--models", "m", "--ollama", srv.URL, "--output", out)
		if code != 0 {
			t.Errorf("%s checks: exit %d, want 0\nstderr: %.300s", name, code, errOut)
		}
		if len(readResults(t, out)) != 1 {
			t.Errorf("%s checks: expected one result recorded", name)
		}
	}
}

// Every call failing is a failed run, but the run still finishes and writes
// its report, and the exit status names the class of the first failure.
func TestMainRun_FailedCallsFinishTheRunThenExit69(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	out := filepath.Join(dir, "out")
	code, stdout, errOut := runAssay(t, "--corpus", corpus, "--models", "m", "--ollama", closedURL(t), "--output", out)
	if code != 69 {
		t.Fatalf("exit %d, want 69\nstderr: %.300s", code, errOut)
	}
	if !strings.Contains(errOut, "1 model call") {
		t.Errorf("stderr %q does not say how many calls failed", errOut)
	}
	if !strings.Contains(stdout, "Results written") {
		t.Errorf("the run stopped before writing its report:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(out, "report.md")); err != nil {
		t.Errorf("report.md missing: %v", err)
	}
	if got := readResults(t, out); len(got) != 1 {
		t.Errorf("results = %d, want the failed call recorded", len(got))
	}
}

// One model failing among several: the others still run and are recorded.
func TestMainRun_SomeCallsFailingStillRunTheRest(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	srv := fakeModelServer(t, nil, "bad")
	out := filepath.Join(dir, "out")
	code, _, errOut := runAssay(t, "--corpus", corpus, "--models", "bad,good", "--ollama", srv.URL, "--output", out)
	if code != 69 {
		t.Fatalf("exit %d, want 69\nstderr: %.300s", code, errOut)
	}
	if got := readResults(t, out); len(got) != 2 {
		t.Errorf("results = %d, want both models recorded", len(got))
	}
}

// A report that cannot be written is 74, and the other output is still tried.
func TestMainRun_UnwritableReportIsIOAndJSONIsStillWritten(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	srv := fakeModelServer(t, nil)
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(out, "report.md"), 0o755); err != nil { // a directory where the file goes
		t.Fatal(err)
	}
	code, _, errOut := runAssay(t, "--corpus", corpus, "--models", "m", "--ollama", srv.URL, "--output", out)
	if code != 74 {
		t.Errorf("exit %d, want 74\nstderr: %.300s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(out, "results.json")); err != nil {
		t.Errorf("results.json was not written after report.md failed: %v", err)
	}
}

// When two things fail, the exit status is the class of the first: the failed
// call (69) comes before the unwritable report (74).
func TestMainRun_TheFirstFailureDecidesTheClass(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(out, "report.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runAssay(t, "--corpus", corpus, "--models", "m", "--ollama", closedURL(t), "--output", out)
	if code != 69 {
		t.Errorf("exit %d, want 69 (the call failed before the report did)\nstderr: %.300s", code, errOut)
	}
}
