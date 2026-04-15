package harvey

import (
	"strings"
	"testing"
)

// newTestAgent returns an Agent with a real workspace in a temp directory,
// suitable for testing command handlers without a live LLM backend.
// In defaults to an empty reader; tests that exercise interactive prompts
// should replace it with strings.NewReader("y\n") etc.
func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return &Agent{
		Config:    DefaultConfig(),
		Workspace: ws,
		In:        strings.NewReader(""),
		commands:  make(map[string]*Command),
	}
}

// ─── extractCodeBlock ─────────────────────────────────────────────────────────

func TestExtractCodeBlock_noBlock(t *testing.T) {
	text := "This is plain text with no code block."
	_, ok := extractCodeBlock(text)
	if ok {
		t.Error("expected ok=false for text without code block")
	}
}

func TestExtractCodeBlock_simpleBlock(t *testing.T) {
	text := "Here is some code:\n```\nfmt.Println(\"hello\")\n```\ndone."
	got, ok := extractCodeBlock(text)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "fmt.Println(\"hello\")\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestExtractCodeBlock_withLanguageTag(t *testing.T) {
	text := "```go\npackage main\n```"
	got, ok := extractCodeBlock(text)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "package main\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestExtractCodeBlock_unterminated(t *testing.T) {
	text := "```\nsome code with no closing fence"
	_, ok := extractCodeBlock(text)
	if ok {
		t.Error("expected ok=false for unterminated block")
	}
}

func TestExtractCodeBlock_multipleBlocks(t *testing.T) {
	text := "```\nfirst\n```\n```\nsecond\n```"
	got, ok := extractCodeBlock(text)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Only the first block should be returned.
	if got != "first\n" {
		t.Errorf("got %q, want first block only", got)
	}
}

// ─── /read ────────────────────────────────────────────────────────────────────

func TestCmdRead_noArgs(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdRead(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.History) != 0 {
		t.Error("expected no history added for empty args")
	}
}

func TestCmdRead_fileNotFound(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdRead(a, []string{"nonexistent.txt"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No readable files → nothing added to history.
	if len(a.History) != 0 {
		t.Error("expected no history added when file not found")
	}
	if !strings.Contains(out.String(), "✗") {
		t.Error("expected error marker in output")
	}
}

func TestCmdRead_success(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace.WriteFile("hello.txt", []byte("hello world\n"), 0o644)

	var out strings.Builder
	if err := cmdRead(a, []string{"hello.txt"}, &out); err != nil {
		t.Fatalf("cmdRead: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	msg := a.History[0]
	if msg.Role != "user" {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if !strings.Contains(msg.Content, "hello world") {
		t.Error("file contents not in history message")
	}
	if !strings.Contains(msg.Content, "hello.txt") {
		t.Error("filename not in history message")
	}
}

func TestCmdRead_multipleFiles(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace.WriteFile("a.txt", []byte("aaa"), 0o644)
	a.Workspace.WriteFile("b.txt", []byte("bbb"), 0o644)

	var out strings.Builder
	if err := cmdRead(a, []string{"a.txt", "b.txt"}, &out); err != nil {
		t.Fatalf("cmdRead: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 combined history message, got %d", len(a.History))
	}
	content := a.History[0].Content
	if !strings.Contains(content, "aaa") || !strings.Contains(content, "bbb") {
		t.Error("both file contents should appear in single history message")
	}
}

func TestCmdRead_partialError(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace.WriteFile("exists.txt", []byte("data"), 0o644)

	var out strings.Builder
	if err := cmdRead(a, []string{"exists.txt", "missing.txt"}, &out); err != nil {
		t.Fatalf("cmdRead: %v", err)
	}
	// One file succeeded — history should still be populated.
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message for partial success, got %d", len(a.History))
	}
}

// ─── /write ───────────────────────────────────────────────────────────────────

func TestCmdWrite_noArgs(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdWrite(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCmdWrite_noHistory(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdWrite(a, []string{"out.txt"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No assistant reply") {
		t.Error("expected 'No assistant reply' message")
	}
}

func TestCmdWrite_withCodeBlock(t *testing.T) {
	a := newTestAgent(t)
	a.AddMessage("user", "write me a function")
	a.AddMessage("assistant", "Sure:\n```go\nfunc hello() {}\n```\nDone.")

	var out strings.Builder
	if err := cmdWrite(a, []string{"hello.go"}, &out); err != nil {
		t.Fatalf("cmdWrite: %v", err)
	}

	data, err := a.Workspace.ReadFile("hello.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "func hello() {}\n" {
		t.Errorf("file content = %q, want %q", data, "func hello() {}\n")
	}
	if !strings.Contains(out.String(), "first code block") {
		t.Error("expected 'first code block' in output")
	}
}

func TestCmdWrite_withoutCodeBlock(t *testing.T) {
	a := newTestAgent(t)
	a.AddMessage("assistant", "Here is plain text with no fences.")

	var out strings.Builder
	if err := cmdWrite(a, []string{"reply.txt"}, &out); err != nil {
		t.Fatalf("cmdWrite: %v", err)
	}

	data, _ := a.Workspace.ReadFile("reply.txt")
	if !strings.Contains(string(data), "plain text") {
		t.Error("expected full reply written to file")
	}
	if !strings.Contains(out.String(), "full reply") {
		t.Error("expected 'full reply' in output")
	}
}

// ─── /run ─────────────────────────────────────────────────────────────────────

func TestCmdRun_noArgs(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdRun(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.History) != 0 {
		t.Error("expected no history added for empty args")
	}
}

func TestCmdRun_success(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdRun(a, []string{"echo", "hello"}, &out); err != nil {
		t.Fatalf("cmdRun: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	msg := a.History[0]
	if msg.Role != "user" {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if !strings.Contains(msg.Content, "echo hello") {
		t.Error("command not in history message")
	}
	if !strings.Contains(msg.Content, "hello") {
		t.Error("command output not in history message")
	}
}

func TestCmdRun_nonzeroExit(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	// 'false' always exits 1 on POSIX systems.
	if err := cmdRun(a, []string{"false"}, &out); err != nil {
		t.Fatalf("cmdRun: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	if !strings.Contains(a.History[0].Content, "exit 1") {
		t.Error("expected exit code in history message")
	}
	if !strings.Contains(out.String(), "exit 1") {
		t.Error("expected exit code in terminal output")
	}
}

func TestCmdRun_truncation(t *testing.T) {
	a := newTestAgent(t)
	// Write a file larger than maxRunOutput into the workspace.
	big := strings.Repeat("x", maxRunOutput+100)
	if err := a.Workspace.WriteFile("big.txt", []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	bigPath, _ := a.Workspace.AbsPath("big.txt")

	var out strings.Builder
	if err := cmdRun(a, []string{"cat", bigPath}, &out); err != nil {
		t.Fatalf("cmdRun: %v", err)
	}
	content := a.History[0].Content
	if !strings.Contains(content, "output truncated") {
		t.Error("expected truncation notice in history message")
	}
}
