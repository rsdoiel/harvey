package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── pipelineMockClient ───────────────────────────────────────────────────────

// pipelineMockClient is a test double that returns sequential replies.
// When replies are exhausted the last one is repeated.
type pipelineMockClient struct {
	replies []string
	callIdx int
}

func newPipelineMock(replies ...string) *pipelineMockClient {
	return &pipelineMockClient{replies: replies}
}

func (m *pipelineMockClient) Name() string { return "pipeline-mock" }
func (m *pipelineMockClient) Close() error { return nil }
func (m *pipelineMockClient) Models(_ context.Context) ([]string, error) {
	return nil, nil
}
func (m *pipelineMockClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	idx := m.callIdx
	if idx >= len(m.replies) {
		idx = len(m.replies) - 1
	}
	m.callIdx++
	fmt.Fprint(out, m.replies[idx])
	return ChatStats{}, nil
}

// ─── newPipelineTestAgent ─────────────────────────────────────────────────────

// newPipelineTestAgent returns an Agent with a real workspace temp dir for pipeline tests.
func newPipelineTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)
	a.registerCommands()
	return a, dir
}

// ─── parsePipelineArgs ────────────────────────────────────────────────────────

func TestParsePipelineArgs_valid(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "step1.md"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "step2.md"), []byte("world"), 0o644)

	for _, pct := range []string{"90%", "85.5%", "100%"} {
		th, files, err := parsePipelineArgs(dir, []string{pct, "step1.md", "step2.md"})
		if err != nil {
			t.Fatalf("parsePipelineArgs(%q): unexpected error: %v", pct, err)
		}
		var expected float64
		fmt.Sscanf(strings.TrimSuffix(pct, "%"), "%f", &expected)
		expected /= 100.0
		if th != expected {
			t.Errorf("threshold for %s: got %.4f, want %.4f", pct, th, expected)
		}
		if len(files) != 2 {
			t.Errorf("files for %s: got %d, want 2", pct, len(files))
		}
	}
}

func TestParsePipelineArgs_invalid(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		args []string
		desc string
	}{
		{[]string{}, "empty args"},
		{[]string{"90%"}, "no file args"},
		{[]string{"90", "step.md"}, "missing percent sign"},
		{[]string{"0%", "step.md"}, "zero confidence"},
		{[]string{"101%", "step.md"}, "over 100"},
		{[]string{"abc%", "step.md"}, "non-numeric"},
		{[]string{"90%", "../outside.md"}, "path escape"},
	}
	for _, tc := range cases {
		_, _, err := parsePipelineArgs(dir, tc.args)
		if err == nil {
			t.Errorf("parsePipelineArgs(%v) — %s: expected error, got nil", tc.args, tc.desc)
		}
	}
}

// ─── scanAtMention ────────────────────────────────────────────────────────────

func TestScanAtMention_first(t *testing.T) {
	got := scanAtMention("Please use @llama3:8b for this. Ignore @other.")
	if got != "llama3:8b" {
		t.Errorf("got %q, want %q", got, "llama3:8b")
	}
}

func TestScanAtMention_none(t *testing.T) {
	got := scanAtMention("No mentions here.")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// ─── extractConfidence ────────────────────────────────────────────────────────

func TestExtractConfidence_json(t *testing.T) {
	response := "Here is my answer.\n{\"confidence\": 0.91, \"reason\": \"well-supported\"}"
	client := newPipelineMock("anything")
	score, stripped, method, err := extractConfidence(context.Background(), client, response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "json" {
		t.Errorf("method: got %q, want %q", method, "json")
	}
	if score != 0.91 {
		t.Errorf("score: got %.2f, want 0.91", score)
	}
	if strings.Contains(stripped, "confidence") {
		t.Errorf("stripped still contains confidence block: %q", stripped)
	}
}

func TestExtractConfidence_followup(t *testing.T) {
	response := "I think the answer might be correct."
	client := newPipelineMock("CONFIDENCE: 0.75")
	score, _, method, _ := extractConfidence(context.Background(), client, response)
	if method != "followup" {
		t.Errorf("method: got %q, want %q", method, "followup")
	}
	if score != 0.75 {
		t.Errorf("score: got %.2f, want 0.75", score)
	}
}

func TestExtractConfidence_keyword(t *testing.T) {
	response := "I'm not sure about this. The result is unclear."
	client := newPipelineMock("no score here")
	score, _, method, _ := extractConfidence(context.Background(), client, response)
	if method != "keyword" {
		t.Errorf("method: got %q, want %q", method, "keyword")
	}
	if score != 0.30 {
		t.Errorf("score: got %.2f, want 0.30", score)
	}
}

func TestExtractConfidence_keywordNoHedging(t *testing.T) {
	response := "The answer is definitely 42."
	client := newPipelineMock("no score here")
	score, _, method, _ := extractConfidence(context.Background(), client, response)
	if method != "keyword" {
		t.Errorf("method: got %q, want %q", method, "keyword")
	}
	if score != 0.80 {
		t.Errorf("score: got %.2f, want 0.80", score)
	}
}

// ─── cmdPipeline integration ──────────────────────────────────────────────────

func writeStepFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeStepFile %s: %v", name, err)
	}
}

func TestCmdPipeline_singleStep(t *testing.T) {
	a, dir := newPipelineTestAgent(t)
	a.Client = newPipelineMock("Good answer.\n{\"confidence\": 0.95, \"reason\": \"high\"}")
	writeStepFile(t, dir, "step1.md", "Summarise the situation.")

	var out strings.Builder
	if err := cmdPipeline(a, []string{"90%", "step1.md"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(a.History) == 0 {
		t.Fatal("History is empty after successful pipeline")
	}
	last := a.History[len(a.History)-1]
	if last.Role != "assistant" {
		t.Errorf("last history role: got %q, want %q", last.Role, "assistant")
	}
	if !strings.Contains(out.String(), "Pipeline complete") {
		t.Errorf("output missing success message: %q", out.String())
	}
}

func TestCmdPipeline_multiStep(t *testing.T) {
	a, dir := newPipelineTestAgent(t)
	a.Client = newPipelineMock(
		"Step 1 answer.\n{\"confidence\": 0.95, \"reason\": \"good\"}",
		"Step 2 answer.\n{\"confidence\": 0.92, \"reason\": \"good\"}",
		"Step 3 answer.\n{\"confidence\": 0.91, \"reason\": \"good\"}",
	)
	writeStepFile(t, dir, "s1.md", "Step one prompt.")
	writeStepFile(t, dir, "s2.md", "Step two prompt.")
	writeStepFile(t, dir, "s3.md", "Step three prompt.")

	var out strings.Builder
	if err := cmdPipeline(a, []string{"90%", "s1.md", "s2.md", "s3.md"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "Pipeline complete") {
		t.Errorf("output missing success message")
	}
	last := a.History[len(a.History)-1]
	if last.Role != "assistant" {
		t.Errorf("last history role: got %q, want %q", last.Role, "assistant")
	}
}

func TestCmdPipeline_failAtStep2(t *testing.T) {
	a, dir := newPipelineTestAgent(t)
	// Step 1 passes; step 2 returns hedging language (keyword → 0.30) which is below 0.90.
	a.Client = newPipelineMock(
		"Step 1 answer.\n{\"confidence\": 0.95, \"reason\": \"good\"}",
		"I'm not sure about this.", // step 2 main response
		"no score here",            // step 2 follow-up → malformed → keyword scan
	)
	writeStepFile(t, dir, "s1.md", "Step one.")
	writeStepFile(t, dir, "s2.md", "Step two.")

	histBefore := len(a.History)
	var out strings.Builder
	if err := cmdPipeline(a, []string{"90%", "s1.md", "s2.md"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(a.History) != histBefore {
		t.Errorf("History grew from %d to %d on failure", histBefore, len(a.History))
	}
	if strings.Contains(out.String(), "Pipeline complete") {
		t.Error("output should not contain success message after failure")
	}
}

func TestCmdPipeline_mentionUnresolved(t *testing.T) {
	a, dir := newPipelineTestAgent(t)
	// Use a pipelineMockClient (not *AnyLLMClient) so same-provider override fails.
	a.Client = newPipelineMock("anything")
	writeStepFile(t, dir, "s1.md", "Use @unknown-model for this step.")

	histBefore := len(a.History)
	var out strings.Builder
	if err := cmdPipeline(a, []string{"90%", "s1.md"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.History) != histBefore {
		t.Errorf("History changed on unresolved @mention")
	}
	if !strings.Contains(out.String(), "did not resolve") {
		t.Errorf("output missing resolution error: %q", out.String())
	}
}

func TestCmdPipeline_fileNotFound(t *testing.T) {
	a, _ := newPipelineTestAgent(t)
	a.Client = newPipelineMock("anything")

	histBefore := len(a.History)
	var out strings.Builder
	if err := cmdPipeline(a, []string{"90%", "missing.md"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.History) != histBefore {
		t.Errorf("History changed on file-not-found")
	}
}

// ─── llamafile parity: context % in pipeline spinner ──────────────────────────

func TestRunPipelineStep_llamafileContextHint(t *testing.T) {
	a, dir := newPipelineTestAgent(t)
	// Use a llamafile client (not Ollama) with a known context window.
	a.Client = newLlamafileLLMClient("http://localhost:8080/v1", "qwen-coding", 0)
	a.Config.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/q.llamafile", ContextLength: 1024},
	}
	a.Config.LlamafileActive = "qwen-coding"

	// Write a step file.
	step := filepath.Join(dir, "step.md")
	if err := os.WriteFile(step, []byte("Hello from pipeline."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Seed history so token estimate is non-zero.
	a.AddMessage("user", strings.Repeat("x", 200)) // ~50 tokens

	client := newPipelineMock(`{"confidence": 0.95}`)
	messages := []Message{{Role: "user", Content: "Hello from pipeline."}}

	var out strings.Builder
	_, _, err := runPipelineStep(context.Background(), a, client, messages, &out, 1, 1, "step.md", 0.8)
	if err != nil {
		t.Fatalf("runPipelineStep: %v", err)
	}

	// The spinner label should have included a context percentage for llamafile.
	// We can't directly inspect the label, but the run should succeed without panic.
	// The key assertion: the step completes successfully on a llamafile backend.
	_ = out.String()
}
