package harvey

import (
	"strings"
	"testing"
)

// ─── /read-chunks ────────────────────────────────────────────────────────────

func TestCmdReadChunks_NoArgs(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "synthesis"}
	var out strings.Builder
	if err := cmdReadChunks(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected usage message, got: %s", out.String())
	}
}

func TestCmdReadChunks_NoClient(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdReadChunks(a, []string{"doc.md", "summarize"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No backend") {
		t.Errorf("expected 'No backend' message, got: %s", out.String())
	}
}

// TestCmdReadChunks_BypassesThreshold is the core regression test: a file
// that comfortably fits within any reasonable context budget (and would
// never trigger the automatic overflow guard in builtin_tools.go or
// file_inject.go) must still be split into multiple chunks and run through
// the map-reduce path when /read-chunks is invoked explicitly with a small
// --chunk-size override.
func TestCmdReadChunks_BypassesThreshold(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "chunk analysis result"}

	content := "First paragraph about topic A.\n\n" +
		"Second paragraph about topic B.\n\n" +
		"Third paragraph about topic C.\n\n" +
		"Fourth paragraph about topic D.\n"
	if err := a.Workspace.WriteFile("small.md", []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out strings.Builder
	err := cmdReadChunks(a, []string{"small.md", "--chunk-size", "20", "--max-chunks", "10", "Summarize each section."}, &out)
	if err != nil {
		t.Fatalf("cmdReadChunks: %v", err)
	}
	if !strings.Contains(out.String(), "chunk analysis result") {
		t.Errorf("expected synthesis result in output, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Processing chunk") {
		t.Errorf("expected map-reduce progress output (proof multiple chunks ran), got: %s", out.String())
	}
}

func TestCmdReadChunks_InstructionFallsBackToLastUserMessage(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "ok"}
	a.AddMessage("user", "What is the topic drift here?")

	if err := a.Workspace.WriteFile("doc.md", []byte("Some content.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out strings.Builder
	if err := cmdReadChunks(a, []string{"doc.md"}, &out); err != nil {
		t.Fatalf("cmdReadChunks: %v", err)
	}
	if strings.Contains(out.String(), "no instruction") {
		t.Errorf("expected fallback to last user message, got: %s", out.String())
	}
}

func TestCmdReadChunks_NoInstructionNoHistory(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "ok"}
	if err := a.Workspace.WriteFile("doc.md", []byte("Some content.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var out strings.Builder
	if err := cmdReadChunks(a, []string{"doc.md"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "no instruction") {
		t.Errorf("expected 'no instruction' message, got: %s", out.String())
	}
}

func TestCmdReadChunks_PermissionDenied(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "ok"}
	a.Config.Security.Permissions = map[string][]string{".": {"write"}} // no read
	if err := a.Workspace.WriteFile("secret.md", []byte("classified\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var out strings.Builder
	if err := cmdReadChunks(a, []string{"secret.md", "summarize"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "permission denied") {
		t.Errorf("expected permission denied message, got: %s", out.String())
	}
}

func TestCmdReadChunks_InvalidChunkSizeFlag(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "ok"}
	var out strings.Builder
	if err := cmdReadChunks(a, []string{"doc.md", "--chunk-size", "notanumber"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "invalid --chunk-size") {
		t.Errorf("expected invalid --chunk-size message, got: %s", out.String())
	}
}

func TestCmdReadChunks_AddsResultToHistory(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "the synthesized answer"}
	if err := a.Workspace.WriteFile("doc.md", []byte("Paragraph one.\n\nParagraph two.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var out strings.Builder
	if err := cmdReadChunks(a, []string{"doc.md", "Summarize."}, &out); err != nil {
		t.Fatalf("cmdReadChunks: %v", err)
	}
	if len(a.History) < 2 {
		t.Fatalf("expected history to contain the /read-chunks exchange, got %d messages", len(a.History))
	}
	last := a.History[len(a.History)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content, "the synthesized answer") {
		t.Errorf("expected last history message to be the synthesis, got role=%q content=%q", last.Role, last.Content)
	}
}
