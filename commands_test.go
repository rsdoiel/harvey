package harvey

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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
	cfg := DefaultConfig()
	cfg.Security.SafeMode = false // tests exercise command mechanics, not safe mode policy
	return &Agent{
		Config:    cfg,
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

// ─── autoExecuteReply ─────────────────────────────────────────────────────────

// newReader wraps s in a bufio.Reader — used to supply "y\n" responses to
// interactive prompts in tests.
func newReader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func TestAutoExecuteReply_writesTaggedBlocks(t *testing.T) {
	a := newTestAgent(t)
	// Empty input → Enter → default "yes" for the confirmation prompt.
	reply := "Here is your script:\n\n```bash:testout/hello.bash\n#!/bin/bash\necho hi\n```\n"

	var out strings.Builder
	a.autoExecuteReply(reply, &out, newReader(""), context.Background())

	data, err := a.Workspace.ReadFile("testout/hello.bash")
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(string(data), "echo hi") {
		t.Errorf("unexpected file content: %q", data)
	}
	if !strings.Contains(out.String(), "✓ wrote") {
		t.Error("expected write confirmation in output")
	}
}

func TestAutoExecuteReply_skipBlock(t *testing.T) {
	a := newTestAgent(t)
	reply := "```bash:testout/skip.bash\necho skip\n```\n"

	var out strings.Builder
	// "n" → skip
	a.autoExecuteReply(reply, &out, newReader("n\n"), context.Background())

	if _, err := a.Workspace.ReadFile("testout/skip.bash"); err == nil {
		t.Error("expected file NOT to be created when user chose 'n'")
	}
}

func TestAutoExecuteReply_noRunFromSuggestions(t *testing.T) {
	a := newTestAgent(t)
	reply := "Run `/run echo hello` to test."

	var out strings.Builder
	a.autoExecuteReply(reply, &out, newReader(""), context.Background())

	// /run suggestions are never auto-executed — no history messages expected.
	if len(a.History) != 0 {
		t.Errorf("expected no auto-run from suggestions, got %d history messages", len(a.History))
	}
}

// TestAutoExecuteReply_untaggedFallback_writes confirms that a plain fenced
// code block (no path in the fence line) is offered to the user for writing
// when no tagged blocks are present — the APERTUS-TOOLS failure mode.
func TestAutoExecuteReply_untaggedFallback_writes(t *testing.T) {
	a := newTestAgent(t)
	// Plain bash fence — no path tag, exactly what APERTUS-TOOLS emits.
	reply := "Here is your script:\n\n```bash\n#!/bin/bash\necho hello\n```\n"

	var out strings.Builder
	// Simulate user typing "parse_pdf.sh" at the path prompt.
	a.autoExecuteReply(reply, &out, newReader("parse_pdf.sh\n"), context.Background())

	data, err := a.Workspace.ReadFile("parse_pdf.sh")
	if err != nil {
		t.Fatalf("file not created by untagged fallback: %v", err)
	}
	if !strings.Contains(string(data), "echo hello") {
		t.Errorf("unexpected file content: %q", data)
	}
	if !strings.Contains(out.String(), "✓ wrote") {
		t.Errorf("expected write confirmation; got: %s", out.String())
	}
}

// TestAutoExecuteReply_untaggedFallback_skip confirms that pressing Enter
// (empty path) skips the write without creating any file.
func TestAutoExecuteReply_untaggedFallback_skip(t *testing.T) {
	a := newTestAgent(t)
	reply := "```bash\necho skip\n```\n"

	var out strings.Builder
	// Empty input → skip.
	a.autoExecuteReply(reply, &out, newReader("\n"), context.Background())

	if _, err := a.Workspace.ReadFile("parse_pdf.sh"); err == nil {
		t.Error("expected no file created when user skips the untagged fallback")
	}
}

// TestAutoExecuteReply_untaggedFallback_noBlockNoPrompt confirms that plain
// prose (no fenced block at all) does not trigger the fallback prompt.
func TestAutoExecuteReply_untaggedFallback_noBlockNoPrompt(t *testing.T) {
	a := newTestAgent(t)
	reply := "Here is an explanation with no code block."

	var out strings.Builder
	a.autoExecuteReply(reply, &out, newReader(""), context.Background())

	// Nothing should be written and the output should contain no path prompt.
	if strings.Contains(out.String(), "Write to file") {
		t.Error("prompt should not appear when there is no code block")
	}
}

// TestAutoExecuteReply_untaggedFallback_suggestedPath_confirms checks that when
// the last user message contains an unambiguous filename, the promptAction box
// is shown pre-filled with that path and Enter (yes) writes the file.
func TestAutoExecuteReply_untaggedFallback_suggestedPath_confirms(t *testing.T) {
	a := newTestAgent(t)
	a.History = []Message{
		{Role: "user", Content: "Write the bash script parse_pdf.sh to the workspace directory."},
		{Role: "assistant", Content: ""},
	}
	reply := "Here is your script:\n\n```bash\n#!/bin/bash\necho hello\n```\n"

	var out strings.Builder
	// Enter = yes (accept the suggested path via promptAction).
	a.autoExecuteReply(reply, &out, newReader("\n"), context.Background())

	data, err := a.Workspace.ReadFile("parse_pdf.sh")
	if err != nil {
		t.Fatalf("file not created when user accepted suggested path: %v", err)
	}
	if !strings.Contains(string(data), "echo hello") {
		t.Errorf("unexpected file content: %q", data)
	}
	// The promptAction box should include the suggested filename.
	if !strings.Contains(out.String(), "parse_pdf.sh") {
		t.Errorf("expected suggested filename in prompt output; got: %s", out.String())
	}
}

// TestAutoExecuteReply_untaggedFallback_suggestedPath_declines checks that "n"
// at the promptAction box leaves the file unwritten.
func TestAutoExecuteReply_untaggedFallback_suggestedPath_declines(t *testing.T) {
	a := newTestAgent(t)
	a.History = []Message{
		{Role: "user", Content: "Write parse_pdf.sh to the workspace."},
		{Role: "assistant", Content: ""},
	}
	reply := "```bash\n#!/bin/bash\necho hello\n```\n"

	var out strings.Builder
	a.autoExecuteReply(reply, &out, newReader("n\n"), context.Background())

	if _, err := a.Workspace.ReadFile("parse_pdf.sh"); err == nil {
		t.Error("file should not be created when user declines the suggested path")
	}
}

// ─── suggestPathFromHistory ───────────────────────────────────────────────────

func TestSuggestPathFromHistory_findsFilename(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "Write the bash script parse_pdf.sh to the workspace directory."},
	}
	if got := suggestPathFromHistory(history); got != "parse_pdf.sh" {
		t.Errorf("got %q, want parse_pdf.sh", got)
	}
}

func TestSuggestPathFromHistory_noPath(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "What can you tell me about PDF parsing strategies?"},
	}
	if got := suggestPathFromHistory(history); got != "" {
		t.Errorf("got %q, want empty string for prose-only message", got)
	}
}

func TestSuggestPathFromHistory_usesLastUserMessage(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "Read old.sh for me."},
		{Role: "assistant", Content: "Here is the content…"},
		{Role: "user", Content: "Now write it as new.sh"},
	}
	if got := suggestPathFromHistory(history); got != "new.sh" {
		t.Errorf("got %q, want new.sh (from last user message)", got)
	}
}

func TestSuggestPathFromHistory_ambiguousReturnsEmpty(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "Write parse_pdf.sh and also rename old_parser.sh"},
	}
	if got := suggestPathFromHistory(history); got != "" {
		t.Errorf("got %q, want empty string for ambiguous (multiple path tokens)", got)
	}
}

func TestSuggestPathFromHistory_stripsQuotesAndPunctuation(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "Please write `parse_pdf.sh` to disk."},
	}
	if got := suggestPathFromHistory(history); got != "parse_pdf.sh" {
		t.Errorf("got %q, want parse_pdf.sh (backtick-quoted token)", got)
	}
}

func TestSuggestPathFromHistory_emptyHistory(t *testing.T) {
	if got := suggestPathFromHistory(nil); got != "" {
		t.Errorf("got %q, want empty string for nil history", got)
	}
}

// ─── /read-dir ────────────────────────────────────────────────────────────────

func writeWSFile(t *testing.T, a *Agent, rel string, content []byte) {
	t.Helper()
	if err := a.Workspace.WriteFile(rel, content, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
}

func TestCmdReadDir_basicRead(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "hello.txt", []byte("hello world\n"))
	writeWSFile(t, a, "world.go", []byte("package main\n"))

	var out strings.Builder
	if err := cmdReadDir(a, nil, &out); err != nil {
		t.Fatal(err)
	}

	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	msg := a.History[0].Content
	if !strings.Contains(msg, "hello.txt") || !strings.Contains(msg, "world.go") {
		t.Errorf("expected both files in context:\n%s", msg)
	}
	if !strings.Contains(msg, "hello world") {
		t.Error("expected file content in context")
	}
}

func TestCmdReadDir_hiddenFilesSkipped(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "visible.txt", []byte("visible\n"))
	writeWSFile(t, a, ".hidden", []byte("secret\n"))

	var out strings.Builder
	if err := cmdReadDir(a, nil, &out); err != nil {
		t.Fatal(err)
	}

	if len(a.History) == 0 {
		t.Fatal("expected history message")
	}
	msg := a.History[0].Content
	if strings.Contains(msg, ".hidden") {
		t.Error("hidden file should be skipped")
	}
	if !strings.Contains(msg, "visible.txt") {
		t.Error("visible file should be included")
	}
}

func TestCmdReadDir_binaryFilesSkipped(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "text.txt", []byte("normal text\n"))
	binary := make([]byte, 10)
	binary[3] = 0 // null byte marks binary
	writeWSFile(t, a, "data.bin", binary)

	var out strings.Builder
	if err := cmdReadDir(a, nil, &out); err != nil {
		t.Fatal(err)
	}

	if len(a.History) == 0 {
		t.Fatal("expected history message")
	}
	msg := a.History[0].Content
	if strings.Contains(msg, "data.bin") {
		t.Error("binary file should be skipped")
	}
	if !strings.Contains(msg, "text.txt") {
		t.Error("text file should be included")
	}
}

func TestCmdReadDir_depthOne_noSubdir(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "top.txt", []byte("top\n"))
	writeWSFile(t, a, "sub/deep.txt", []byte("deep\n"))

	var out strings.Builder
	if err := cmdReadDir(a, []string{".", "--depth", "1"}, &out); err != nil {
		t.Fatal(err)
	}

	if len(a.History) == 0 {
		t.Fatal("expected history message")
	}
	msg := a.History[0].Content
	if strings.Contains(msg, "deep.txt") {
		t.Error("subdir file should be excluded at depth 1")
	}
	if !strings.Contains(msg, "top.txt") {
		t.Error("top-level file should be included")
	}
}

func TestCmdReadDir_depthTwo_includesSubdir(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "top.txt", []byte("top\n"))
	writeWSFile(t, a, "sub/file.txt", []byte("sub\n"))
	writeWSFile(t, a, "sub/nested/deep.txt", []byte("deep\n"))

	var out strings.Builder
	// default depth=2 reads root files + one level of subdirs
	if err := cmdReadDir(a, nil, &out); err != nil {
		t.Fatal(err)
	}

	if len(a.History) == 0 {
		t.Fatal("expected history message")
	}
	msg := a.History[0].Content
	if !strings.Contains(msg, "top.txt") {
		t.Error("top-level file should be included")
	}
	if !strings.Contains(msg, "sub/file.txt") {
		t.Error("first-level subdir file should be included at depth 2")
	}
	if strings.Contains(msg, "deep.txt") {
		t.Error("second-level subdir file should be excluded at depth 2")
	}
}

func TestCmdReadDir_agentsDirSkipped(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "good.txt", []byte("good\n"))
	writeWSFile(t, a, "agents/secret.yaml", []byte("model: foo\n"))

	var out strings.Builder
	if err := cmdReadDir(a, nil, &out); err != nil {
		t.Fatal(err)
	}

	if len(a.History) == 0 {
		t.Fatal("expected history message")
	}
	msg := a.History[0].Content
	if strings.Contains(msg, "secret.yaml") {
		t.Error("agents/ file should be skipped")
	}
}

func TestCmdReadDir_sensitiveFileSkipped(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "code.go", []byte("package main\n"))
	writeWSFile(t, a, "id_rsa.key", []byte("-----BEGIN RSA-----\n"))

	var out strings.Builder
	if err := cmdReadDir(a, nil, &out); err != nil {
		t.Fatal(err)
	}

	if len(a.History) == 0 {
		t.Fatal("expected history message")
	}
	msg := a.History[0].Content
	if strings.Contains(msg, "id_rsa.key") {
		t.Error("sensitive file should be skipped")
	}
	if !strings.Contains(msg, "code.go") {
		t.Error("normal file should be included")
	}
}

func TestCmdReadDir_noWorkspace(t *testing.T) {
	a := &Agent{Config: DefaultConfig(), commands: make(map[string]*Command)}
	var out strings.Builder
	if err := cmdReadDir(a, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No workspace") {
		t.Error("expected 'No workspace' message when workspace is nil")
	}
}

func TestCmdReadDir_notADirectory(t *testing.T) {
	a := newTestAgent(t)
	writeWSFile(t, a, "file.txt", []byte("not a dir\n"))

	var out strings.Builder
	if err := cmdReadDir(a, []string{"file.txt"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not a directory") {
		t.Errorf("expected 'not a directory' message, got: %s", out.String())
	}
}

// ── route commands ────────────────────────────────────────────────────────────

// newTestAgentWithRoutes returns a test agent with an initialised route
// registry containing one Anthropic endpoint.
func newTestAgentWithRoutes(t *testing.T) *Agent {
	t.Helper()
	a := newTestAgent(t)
	a.Routes = NewRouteRegistry()
	a.Routes.Add(&RouteEndpoint{
		Name:  "claude",
		URL:   "anthropic://",
		Model: "claude-3-5-sonnet",
		Kind:  KindAnthropic,
	})
	return a
}

func TestRouteModels_noArgs(t *testing.T) {
	a := newTestAgent(t)
	a.Routes = NewRouteRegistry()
	var out strings.Builder
	if err := routeModels(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Usage") {
		t.Errorf("expected usage message, got: %s", out.String())
	}
}

func TestRouteModels_badURL(t *testing.T) {
	a := newTestAgent(t)
	a.Routes = NewRouteRegistry()
	var out strings.Builder
	if err := routeModels(a, []string{"bogus://"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "unrecognised") {
		t.Errorf("expected unrecognised URL message, got: %s", out.String())
	}
}

func TestRouteProbe_notFound(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	var out strings.Builder
	if err := routeProbe(a, "ghost", &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("expected not-found message, got: %s", out.String())
	}
}

func TestRouteProbe_knownEndpoint(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	var out strings.Builder
	if err := routeProbe(a, "claude", &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := out.String()
	for _, want := range []string{"claude", "anthropic", "claude-3-5-sonnet"} {
		if !strings.Contains(body, want) {
			t.Errorf("probe output missing %q:\n%s", want, body)
		}
	}
}

func TestRouteSet_noArgs(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	var out strings.Builder
	if err := routeSet(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Usage") {
		t.Errorf("expected usage message, got: %s", out.String())
	}
}

func TestRouteSet_notFound(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	var out strings.Builder
	if err := routeSet(a, []string{"ghost", "tools", "on"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("expected not-found message, got: %s", out.String())
	}
}

func TestRouteSet_toolsOn(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	var out strings.Builder
	if err := routeSet(a, []string{"claude", "tools", "on"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ep := a.Routes.Lookup("claude")
	if ep == nil || !ep.Tools {
		t.Error("expected ep.Tools to be true after 'set tools on'")
	}
}

func TestRouteSet_toolsOff(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	// Enable first, then disable.
	ep := a.Routes.Lookup("claude")
	ep.Tools = true

	var out strings.Builder
	if err := routeSet(a, []string{"claude", "tools", "off"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.Tools {
		t.Error("expected ep.Tools to be false after 'set tools off'")
	}
}

func TestRouteSet_unknownKey(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	var out strings.Builder
	if err := routeSet(a, []string{"claude", "badkey", "on"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Unknown setting") {
		t.Errorf("expected unknown setting message, got: %s", out.String())
	}
}

func TestRouteSet_unknownValue(t *testing.T) {
	a := newTestAgentWithRoutes(t)
	var out strings.Builder
	if err := routeSet(a, []string{"claude", "tools", "maybe"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Unknown value") {
		t.Errorf("expected unknown value message, got: %s", out.String())
	}
}

// ─── tab completion ────────────────────────────────────────────────────────────

func TestSubcommandCompletion_memory(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	// "/memory " (trailing space) → all subcommands
	got := completer("/memory ")
	if len(got) == 0 {
		t.Fatal("expected subcommand completions for '/memory ', got none")
	}
	for _, want := range []string{"mine", "list", "show", "recall", "profile"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in completions, got %v", want, got)
		}
	}
}

func TestSubcommandCompletion_prefix(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	// "/memory m" → only subcommands starting with "m"
	got := completer("/memory m")
	for _, g := range got {
		if !strings.HasPrefix(g, "m") {
			t.Errorf("expected all completions to start with 'm', got %q", g)
		}
	}
	if len(got) == 0 {
		t.Error("expected at least one completion for '/memory m'")
	}
}

func TestSubcommandCompletion_rag(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	got := completer("/rag ")
	for _, want := range []string{"list", "new", "use", "drop", "ingest", "status", "query", "on", "off"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in /rag completions, got %v", want, got)
		}
	}
}

func TestSubcommandCompletion_noSubcommands(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	// "/status " has no subcommands — should return nil, not panic
	got := completer("/status ")
	if len(got) != 0 {
		t.Errorf("expected no completions for '/status ', got %v", got)
	}
}

func TestSubcommandCompletion_sorted(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	got := completer("/rag ")
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("completions not sorted: %q before %q", got[i-1], got[i])
		}
	}
}

func TestArgCompletion_ragStoreNames(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.RagStores = []RagStoreEntry{
		{Name: "harvey"},
		{Name: "project-docs"},
	}
	a.registerCommands()
	completer := a.buildCompleter()

	got := completer("/rag use ")
	if len(got) != 2 {
		t.Fatalf("expected 2 store candidates, got %d: %v", len(got), got)
	}
	for _, want := range []string{"harvey", "project-docs"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in /rag use completions, got %v", want, got)
		}
	}
}

func TestArgCompletion_ragStorePrefixFilter(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.RagStores = []RagStoreEntry{
		{Name: "harvey"},
		{Name: "project-docs"},
	}
	a.registerCommands()
	completer := a.buildCompleter()

	got := completer("/rag use h")
	if len(got) != 1 || got[0] != "harvey" {
		t.Errorf("prefix 'h' should match only 'harvey', got %v", got)
	}
}

func TestArgCompletion_memoryTypes(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	got := completer("/memory list ")
	if len(got) == 0 {
		t.Fatal("expected memory type candidates for '/memory list ', got none")
	}
	for _, want := range []string{"tool_use", "workflow", "user_preference"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in /memory list completions, got %v", want, got)
		}
	}
}

func TestArgCompletion_noRegistration(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	// /rag new takes a user-chosen name — no ArgCompletion registered
	got := completer("/rag new ")
	if len(got) != 0 {
		t.Errorf("expected no completions for '/rag new', got %v", got)
	}
}

func TestArgCompletion_llamafileNames(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Models = []LlamafileEntry{
		{Name: "granite3.3-2b"},
		{Name: "llama3.2-1b"},
	}
	a.registerCommands()
	completer := a.buildCompleter()

	got := completer("/llamafile use ")
	if len(got) != 2 {
		t.Fatalf("expected 2 llamafile candidates, got %d: %v", len(got), got)
	}
}

func TestArgCompletion_memoryIDCandidates_emptyStore(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	completer := a.buildCompleter()

	// Empty store — should return empty without panicking.
	got := completer("/memory show ")
	// nil or empty is fine; just must not panic
	_ = got
}

// ─── /model command ──────────────────────────────────────────────────────────

func TestCmdModelShow_noBackend(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	if err := cmdModel(a, nil, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "none") {
		t.Errorf("expected 'none' in output when no backend configured, got: %s", buf.String())
	}
}

func TestCmdModelShow_withLlamafile(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.Active = "qwen-coding"
	a := NewAgent(cfg, ws)
	a.Client = &mockLLMClient{}
	var buf strings.Builder
	if err := cmdModel(a, nil, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "qwen-coding") {
		t.Errorf("expected model name in output, got: %s", buf.String())
	}
}

func TestCmdModelList_empty(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	if err := cmdModel(a, []string{"list"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not panic and should produce some output.
	if buf.Len() == 0 {
		t.Error("expected non-empty output from /model list")
	}
}

func TestCmdModelList_withLlamafiles(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.Models = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/qwen.llamafile"},
		{Name: "phi-mini", Path: "/tmp/phi.llamafile"},
	}
	cfg.Llamafile.Active = "qwen-coding"
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	if err := cmdModel(a, []string{"list"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "qwen-coding") {
		t.Errorf("expected qwen-coding in list output, got: %s", out)
	}
	if !strings.Contains(out, "phi-mini") {
		t.Errorf("expected phi-mini in list output, got: %s", out)
	}
}

func TestCmdModelUse_notFound(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	// No registered models — use should fail gracefully.
	err := cmdModel(a, []string{"use", "nonexistent"}, &buf)
	// Either an error or an "not found" message in output is acceptable.
	if err == nil && !strings.Contains(buf.String(), "not found") && !strings.Contains(buf.String(), "no") {
		t.Errorf("expected error or not-found message, got: %s", buf.String())
	}
}

// ─── vocabulary alias tests ───────────────────────────────────────────────────

func TestRagRemove_alias(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	// No stores registered — remove should report that, not error
	err := cmdRag(a, []string{"remove", "nonexistent"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkillShow_aliasForInfo(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	a.Skills = SkillCatalog{}
	var bufShow, bufInfo strings.Builder
	// Both should produce the same output (or same error path) for an unknown skill.
	errShow := cmdSkill(a, []string{"show", "nonexistent"}, &bufShow)
	errInfo := cmdSkill(a, []string{"info", "nonexistent"}, &bufInfo)
	// Both either error or print "not found" — they must behave identically.
	if (errShow == nil) != (errInfo == nil) {
		t.Errorf("show/info error mismatch: show=%v info=%v", errShow, errInfo)
	}
}

func TestSkillSetNew_aliasForCreate(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var bufNew, bufCreate strings.Builder
	errNew := cmdSkillSet(a, []string{"new", "myskill"}, &bufNew)
	errCreate := cmdSkillSet(a, []string{"create", "myskill"}, &bufCreate)
	// Both attempt the same operation; behaviour and error path are identical.
	if (errNew == nil) != (errCreate == nil) {
		t.Errorf("new/create error mismatch: new=%v create=%v", errNew, errCreate)
	}
}

func TestSkillSetShow_aliasForInfo(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var bufShow, bufInfo strings.Builder
	errShow := cmdSkillSet(a, []string{"show", "nonexistent"}, &bufShow)
	errInfo := cmdSkillSet(a, []string{"info", "nonexistent"}, &bufInfo)
	if (errShow == nil) != (errInfo == nil) {
		t.Errorf("show/info error mismatch: show=%v info=%v", errShow, errInfo)
	}
}

func TestModelAliasAdd_aliasForSet(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ModelAliases = make(map[string]ModelAlias)
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	// Use "add" verb to define an alias.
	if err := cmdModelAlias(a, []string{"add", "coder", "qwen2.5-coder:7b"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Config.ModelAliases["coder"].Model != "qwen2.5-coder:7b" {
		t.Errorf("alias not set: %v", a.Config.ModelAliases)
	}
}

func TestSessionUse_aliasForContinue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.spmd"
	// Create a properly-formatted session using NewRecorder.
	rec, err := NewRecorder(path, "ollama (mock)", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordTurn("Hello.", "Hi!"); err != nil {
		t.Fatal(err)
	}
	rec.Close()

	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	if err := cmdSession(a, []string{"use", path}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.History) == 0 {
		t.Error("expected history to be loaded after /session use")
	}
}

func TestSessionList_emptyDir(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	a.SessionsDir = t.TempDir() // empty dir
	var buf strings.Builder
	if err := cmdSession(a, []string{"list"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No sessions") && !strings.Contains(out, "no sessions") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestRouteUse_setsActiveRoute(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	a.Routes = NewRouteRegistry()
	a.Routes.Add(&RouteEndpoint{Name: "pi2", URL: "ollama://192.0.2.12:11434", Model: "llama3:8b"})
	var buf strings.Builder
	if err := cmdRoute(a, []string{"use", "pi2"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ActiveRoute != "pi2" {
		t.Errorf("ActiveRoute: got %q want %q", a.ActiveRoute, "pi2")
	}
}

func TestRouteUse_noArgs_clearsActiveRoute(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	a.ActiveRoute = "pi2"
	var buf strings.Builder
	if err := cmdRoute(a, []string{"use"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ActiveRoute != "" {
		t.Errorf("expected ActiveRoute cleared, got %q", a.ActiveRoute)
	}
}

// ─── /rag show ────────────────────────────────────────────────────────────────

func TestRagShow_activeStore(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Memory.RagStores = []RagStoreEntry{
		{Name: "golang", DBPath: "agents/rag/golang.db", EmbeddingModel: "nomic-embed-text"},
	}
	cfg.Memory.RagActive = "golang"
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	if err := cmdRag(a, []string{"show"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "golang") {
		t.Errorf("expected store name in output, got: %s", out)
	}
	if !strings.Contains(out, "nomic-embed-text") {
		t.Errorf("expected embedding model in output, got: %s", out)
	}
	if !strings.Contains(out, "agents/rag/golang.db") {
		t.Errorf("expected db path in output, got: %s", out)
	}
}

func TestRagShow_namedStore(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Memory.RagStores = []RagStoreEntry{
		{Name: "golang", DBPath: "agents/rag/golang.db", EmbeddingModel: "nomic-embed-text"},
		{Name: "writing", DBPath: "agents/rag/writing.db", EmbeddingModel: "nomic-embed-text"},
	}
	cfg.Memory.RagActive = "golang"
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	if err := cmdRag(a, []string{"show", "writing"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "writing") {
		t.Errorf("expected 'writing' in output, got: %s", out)
	}
	if !strings.Contains(out, "agents/rag/writing.db") {
		t.Errorf("expected writing db path in output, got: %s", out)
	}
}

func TestRagShow_notFound(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	if err := cmdRag(a, []string{"show", "nonexistent"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "not found") && !strings.Contains(buf.String(), "No RAG") {
		t.Errorf("expected not-found message, got: %s", buf.String())
	}
}

func TestRagShow_noActive(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	if err := cmdRag(a, []string{"show"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No store") && !strings.Contains(out, "no store") && !strings.Contains(out, "not configured") {
		t.Errorf("expected no-store message, got: %s", out)
	}
}

// ─── /ollama use (interactive picker) ────────────────────────────────────────

// ollamaTagsMockServer starts an httptest server that returns a two-model
// /api/tags response and an empty /api/ps response.
func ollamaTagsMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "llama3.2:3b", "size": 2019393189, "details": map[string]string{"family": "llama"}},
					{"name": "nomic-embed-text:latest", "size": 274302560, "details": map[string]string{"family": "nomic"}},
				},
			})
		case "/api/ps":
			json.NewEncoder(w).Encode(map[string]interface{}{"models": []interface{}{}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestOllamaUse_noArg_showsNumberedList(t *testing.T) {
	srv := ollamaTagsMockServer(t)
	defer srv.Close()

	a := newTestAgent(t)
	a.Config.Ollama.URL = srv.URL
	a.In = strings.NewReader("0\n") // invalid selection → Cancelled

	var buf strings.Builder
	err := cmdOllama(a, []string{"use"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "llama3.2:3b") {
		t.Errorf("expected model name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[") {
		t.Errorf("expected numbered list brackets in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Cancelled") {
		t.Errorf("expected Cancelled on invalid selection, got:\n%s", out)
	}
}

func TestOllamaUse_noArg_validSelection(t *testing.T) {
	srv := ollamaTagsMockServer(t)
	defer srv.Close()

	a := newTestAgent(t)
	a.Config.Ollama.URL = srv.URL
	a.In = strings.NewReader("1\n") // select first model

	var buf strings.Builder
	// modelSwitch will fail to connect for the LLM client but should still
	// attempt the switch and print the model name.
	cmdOllama(a, []string{"use"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "llama3.2:3b") {
		t.Errorf("expected selected model name in output, got:\n%s", out)
	}
}

// ─── removeModelFromConfig ────────────────────────────────────────────────────

func TestRemoveModelFromConfig_AliasRemoved(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{"g": {Model: "granite4.1:3b"}, "q": {Model: "qwen2.5:7b"}}
	changed := removeModelFromConfig(cfg, "granite4.1:3b")
	if !changed {
		t.Fatal("expected changed=true when alias value matches")
	}
	if _, ok := cfg.ModelAliases["g"]; ok {
		t.Error("expected alias 'g' to be removed")
	}
	if _, ok := cfg.ModelAliases["q"]; !ok {
		t.Error("expected alias 'q' to remain")
	}
}

func TestRemoveModelFromConfig_ModelMapKeyRemoved(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Memory.RagStores = []RagStoreEntry{
		{
			Name:     "docs",
			ModelMap: map[string]string{"granite4.1:3b": "nomic-embed-text:latest", "qwen2.5:7b": "nomic-embed-text:latest"},
		},
	}
	changed := removeModelFromConfig(cfg, "granite4.1:3b")
	if !changed {
		t.Fatal("expected changed=true when model_map key matches")
	}
	if _, ok := cfg.Memory.RagStores[0].ModelMap["granite4.1:3b"]; ok {
		t.Error("expected 'granite4.1:3b' key to be removed from model_map")
	}
	if _, ok := cfg.Memory.RagStores[0].ModelMap["qwen2.5:7b"]; !ok {
		t.Error("expected 'qwen2.5:7b' key to remain in model_map")
	}
}

func TestRemoveModelFromConfig_NoChange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{"g": {Model: "granite4.1:3b"}}
	changed := removeModelFromConfig(cfg, "llama3.2:3b") // not present anywhere
	if changed {
		t.Error("expected changed=false when model name is not referenced")
	}
}

func TestRemoveModelFromConfig_CaseInsensitive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{"g": {Model: "Granite4.1:3B"}}
	changed := removeModelFromConfig(cfg, "granite4.1:3b")
	if !changed {
		t.Error("expected changed=true for case-insensitive match")
	}
}

// ─── pruneStaleOllamaRefs ─────────────────────────────────────────────────────

func TestPruneStaleOllamaRefs_RemovesStaleAlias(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"g": {Model: "granite4.1:3b"},          // stale — not in live list
		"n": {Model: "nomic-embed-text:latest"}, // live — keep
	}

	var out strings.Builder
	n, err := pruneStaleOllamaRefs(a, []string{"nomic-embed-text:latest"}, &out)
	if err != nil {
		t.Fatalf("pruneStaleOllamaRefs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 removal, got %d", n)
	}
	if _, ok := a.Config.ModelAliases["g"]; ok {
		t.Error("stale alias 'g' should have been removed")
	}
	if _, ok := a.Config.ModelAliases["n"]; !ok {
		t.Error("live alias 'n' should remain")
	}
}

func TestPruneStaleOllamaRefs_RemovesStaleModelMapKey(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.RagStores = []RagStoreEntry{
		{
			Name:     "docs",
			ModelMap: map[string]string{
				"granite4.1:3b":          "nomic-embed-text:latest", // stale
				"nomic-embed-text:latest": "nomic-embed-text:latest", // live (self-map, but valid)
			},
		},
	}

	var out strings.Builder
	n, err := pruneStaleOllamaRefs(a, []string{"nomic-embed-text:latest"}, &out)
	if err != nil {
		t.Fatalf("pruneStaleOllamaRefs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 removal, got %d", n)
	}
	if _, ok := a.Config.Memory.RagStores[0].ModelMap["granite4.1:3b"]; ok {
		t.Error("stale model_map key should have been removed")
	}
}

func TestPruneStaleOllamaRefs_NoStaleRefs(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{"n": {Model: "nomic-embed-text:latest"}}

	var out strings.Builder
	n, err := pruneStaleOllamaRefs(a, []string{"nomic-embed-text:latest", "llama3.2:3b"}, &out)
	if err != nil {
		t.Fatalf("pruneStaleOllamaRefs: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 removals when all refs are live, got %d", n)
	}
}

// ─── /ollama clean ────────────────────────────────────────────────────────────

func TestCmdOllama_Clean_NoStaleRefs(t *testing.T) {
	srv := ollamaTagsMockServer(t)
	defer srv.Close()

	a := newTestAgent(t)
	a.Config.Ollama.URL = srv.URL
	// Model alias points to a live model.
	a.Config.ModelAliases = map[string]ModelAlias{"n": {Model: "nomic-embed-text:latest"}}

	var buf strings.Builder
	if err := cmdOllama(a, []string{"clean"}, &buf); err != nil {
		t.Fatalf("cmdOllama clean: %v", err)
	}
	if !strings.Contains(buf.String(), "No stale entries") {
		t.Errorf("expected 'No stale entries' message, got:\n%s", buf.String())
	}
	if _, ok := a.Config.ModelAliases["n"]; !ok {
		t.Error("live alias should not have been removed")
	}
}

func TestCmdOllama_Clean_RemovesStaleRefs(t *testing.T) {
	srv := ollamaTagsMockServer(t) // serves llama3.2:3b and nomic-embed-text:latest
	defer srv.Close()

	a := newTestAgent(t)
	a.Config.Ollama.URL = srv.URL
	// granite4.1:3b is NOT in the mock server's model list — should be pruned.
	a.Config.ModelAliases = map[string]ModelAlias{
		"g": {Model: "granite4.1:3b"},
		"l": {Model: "llama3.2:3b"},
	}

	var buf strings.Builder
	if err := cmdOllama(a, []string{"clean"}, &buf); err != nil {
		t.Fatalf("cmdOllama clean: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Removed") {
		t.Errorf("expected 'Removed' in output, got:\n%s", out)
	}
	if _, ok := a.Config.ModelAliases["g"]; ok {
		t.Error("stale alias 'g' should have been pruned")
	}
	if _, ok := a.Config.ModelAliases["l"]; !ok {
		t.Error("live alias 'l' should remain")
	}
}

// ─── /model use (no arg → picker) ────────────────────────────────────────────

func TestCmdModelUse_noArg_noModels(t *testing.T) {
	a := newTestAgent(t)
	// No llamafile models, no aliases — picker should say nothing to choose.
	a.In = strings.NewReader("\n")
	var buf strings.Builder
	if err := cmdModel(a, []string{"use"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// When there are no models, should print a helpful message rather than silently printing usage.
	if !strings.Contains(out, "no") && !strings.Contains(out, "No") && !strings.Contains(out, "register") && !strings.Contains(out, "Usage") {
		t.Errorf("expected no-models message, got: %s", out)
	}
}

func TestCmdModelUse_noArg_showsPicker(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Models = []LlamafileEntry{
		{Name: "qwen-coder", Path: "/tmp/qwen.llamafile"},
		{Name: "gemma4", Path: "/tmp/gemma4.llamafile"},
	}
	// Simulate user entering "0" (invalid) → cancellation without model switch.
	a.In = strings.NewReader("0\n")
	var buf strings.Builder
	if err := cmdModel(a, []string{"use"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "qwen-coder") {
		t.Errorf("expected model name qwen-coder in picker output, got: %s", out)
	}
	if !strings.Contains(out, "gemma4") {
		t.Errorf("expected model name gemma4 in picker output, got: %s", out)
	}
}

func TestCmdModelUse_noArg_selectsModel(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Models = []LlamafileEntry{
		{Name: "qwen-coder", Path: "/tmp/qwen.llamafile"},
	}
	// Select item 1.
	a.In = strings.NewReader("1\n")
	var buf strings.Builder
	// cmdModel will call cmdLlamafileUse which tries to start the process — that
	// will fail since the path doesn't exist. We just verify the picker was shown
	// and the correct name was handed to the switch logic.
	cmdModel(a, []string{"use"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "qwen-coder") {
		t.Errorf("expected selected model name in output, got: %s", out)
	}
}

// ─── /session use (no arg → picker) ──────────────────────────────────────────

func TestSessionUse_noArg_noSessions(t *testing.T) {
	a := newTestAgent(t)
	a.SessionsDir = t.TempDir() // empty
	a.In = strings.NewReader("\n")
	var buf strings.Builder
	if err := cmdSession(a, []string{"use"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No session") && !strings.Contains(out, "no session") {
		t.Errorf("expected no-sessions message, got: %s", out)
	}
}

func TestSessionUse_noArg_showsPicker(t *testing.T) {
	sessDir := t.TempDir()

	// Write two minimal session files.
	for _, name := range []string{"alpha.spmd", "beta.spmd"} {
		rec, err := NewRecorder(sessDir+"/"+name, "mock", sessDir)
		if err != nil {
			t.Fatal(err)
		}
		_ = rec.RecordTurn("hello", "hi")
		rec.Close()
	}

	a := newTestAgent(t)
	a.SessionsDir = sessDir
	// Simulate user pressing Enter (no selection) → cancel.
	a.In = strings.NewReader("\n")
	var buf strings.Builder
	if err := cmdSession(a, []string{"use"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "alpha") && !strings.Contains(out, "beta") {
		t.Errorf("expected session names in picker output, got: %s", out)
	}
}

func TestSessionContinue_noArg_showsPicker(t *testing.T) {
	sessDir := t.TempDir()
	rec, err := NewRecorder(sessDir+"/session.spmd", "mock", sessDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = rec.RecordTurn("hello", "hi")
	rec.Close()

	a := newTestAgent(t)
	a.SessionsDir = sessDir
	a.In = strings.NewReader("\n") // cancel
	var buf strings.Builder
	if err := cmdSession(a, []string{"continue"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "session") {
		t.Errorf("expected session name in picker output, got: %s", out)
	}
}

// ─── S4 observation attribution ───────────────────────────────────────────────

// newTestAgentWithKB returns an Agent with a live KnowledgeBase and a current
// project set, ready for /kb observe and /kb cite tests.
func newTestAgentWithKB(t *testing.T) (*Agent, int64) {
	t.Helper()
	a := newTestAgent(t)
	kb, err := OpenKnowledgeBase(a.Workspace, "")
	if err != nil {
		t.Fatalf("OpenKnowledgeBase: %v", err)
	}
	t.Cleanup(func() { kb.Close() })
	a.KB = kb
	pid, err := kb.AddProject("test-project", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	a.Config.Memory.CurrentProjectID = pid
	return a, pid
}

func TestKBObserve_SetsLastObservationID(t *testing.T) {
	a, _ := newTestAgentWithKB(t)
	var buf strings.Builder
	if err := kbObserve(a, []string{"note", "something interesting"}, &buf); err != nil {
		t.Fatalf("kbObserve: %v", err)
	}
	if a.LastObservationID == 0 {
		t.Error("expected LastObservationID to be set after /kb observe")
	}
}

func TestKBObserve_HintsRAGSources(t *testing.T) {
	a, _ := newTestAgentWithKB(t)
	// Seed a source and set LastRAGInfo with it.
	srcID, _ := a.KB.AddSource(Source{Title: "Test Paper", IdentifierType: "doi", IdentifierValue: "10.1/test"})
	a.LastRAGInfo = &RAGAugmentInfo{
		StoreName: "docs.db",
		Chunks:    1,
		TopScore:  0.9,
		Sources: []RAGChunkRef{
			{Source: "paper.md", SourceDOI: "10.1/test", SourceTitle: "Test Paper"},
		},
	}
	_ = srcID

	var buf strings.Builder
	if err := kbObserve(a, []string{"finding", "RAG helped here"}, &buf); err != nil {
		t.Fatalf("kbObserve: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "cite") {
		t.Errorf("expected /kb cite hint in output when LastRAGInfo has sources; got:\n%s", out)
	}
}

func TestKBCite_LinksToLastObservation(t *testing.T) {
	a, pid := newTestAgentWithKB(t)
	obsID, _ := a.KB.AddObservation(pid, "note", "needs a citation")
	a.LastObservationID = obsID
	srcID, _ := a.KB.AddSource(Source{Title: "Cited Work"})

	var buf strings.Builder
	if err := kbCite(a, []string{strconv.FormatInt(srcID, 10)}, &buf); err != nil {
		t.Fatalf("kbCite: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "linked") {
		t.Errorf("expected 'linked' in output; got: %s", out)
	}
	// Verify the link is actually in the DB.
	sources, err := a.KB.ObservationSources(obsID)
	if err != nil {
		t.Fatalf("ObservationSources: %v", err)
	}
	if len(sources) != 1 || sources[0].Title != "Cited Work" {
		t.Errorf("expected 'Cited Work' linked to observation; got: %v", sources)
	}
}

func TestKBCite_NoLastObservation(t *testing.T) {
	a, _ := newTestAgentWithKB(t)
	var buf strings.Builder
	if err := kbCite(a, []string{"1"}, &buf); err != nil {
		t.Fatalf("kbCite: %v", err)
	}
	if !strings.Contains(buf.String(), "No recent observation") {
		t.Errorf("expected 'No recent observation' message; got: %s", buf.String())
	}
}

func TestKBShow_WithSources(t *testing.T) {
	a, pid := newTestAgentWithKB(t)
	obsID, _ := a.KB.AddObservation(pid, "finding", "important finding")
	srcID, _ := a.KB.AddSource(Source{Title: "Key Reference", IdentifierType: "doi", IdentifierValue: "10.1/key"})
	_ = a.KB.LinkObservationSource(obsID, srcID, "cited")

	var buf strings.Builder
	if err := kbShow(a, []string{strconv.FormatInt(obsID, 10)}, &buf); err != nil {
		t.Fatalf("kbShow: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "important finding") {
		t.Errorf("expected observation body in output; got: %s", out)
	}
	if !strings.Contains(out, "Key Reference") {
		t.Errorf("expected source title in output; got: %s", out)
	}
}

func TestKBShow_RetractedSourceWarning(t *testing.T) {
	a, pid := newTestAgentWithKB(t)
	obsID, _ := a.KB.AddObservation(pid, "note", "based on a paper")
	srcID, _ := a.KB.AddSource(Source{Title: "Retracted Paper"})
	_ = a.KB.LinkObservationSource(obsID, srcID, "cited")
	_ = a.KB.RetractSource(srcID, "Withdrawn by publisher")

	var buf strings.Builder
	if err := kbShow(a, []string{strconv.FormatInt(obsID, 10)}, &buf); err != nil {
		t.Fatalf("kbShow: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "RETRACTED") {
		t.Errorf("expected RETRACTED warning in output; got: %s", out)
	}
}

func TestClearHistory_ClearsLastRAGInfo(t *testing.T) {
	a := newTestAgent(t)
	a.LastRAGInfo = &RAGAugmentInfo{StoreName: "docs.db", Chunks: 1, TopScore: 0.9}
	a.LastObservationID = 42
	a.ClearHistory()
	if a.LastRAGInfo != nil {
		t.Error("expected LastRAGInfo to be nil after ClearHistory")
	}
	if a.LastObservationID != 0 {
		t.Error("expected LastObservationID to be 0 after ClearHistory")
	}
}

// ─── /model mode ──────────────────────────────────────────────────────────────

func newTestAgentWithCache(t *testing.T, modelName string) (*Agent, *ModelCache) {
	t.Helper()
	a := newTestAgent(t)
	mc, err := OpenModelCache(a.Workspace, "")
	if err != nil {
		t.Fatalf("OpenModelCache: %v", err)
	}
	t.Cleanup(func() { mc.Close() })
	if modelName != "" {
		if err := mc.Set(&ModelCapability{Name: modelName, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
			t.Fatalf("Set: %v", err)
		}
		a.Client = newOllamaLLMClient("http://localhost:11434", modelName, 0)
	}
	a.ModelCache = mc
	return a, mc
}

func TestCmdModelMode_SetsActiveModel(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "phi4:latest")

	var out strings.Builder
	if err := cmdModel(a, []string{"mode", "inject"}, &out); err != nil {
		t.Fatalf("cmdModel mode inject: %v", err)
	}

	got, err := mc.Get("phi4:latest")
	if err != nil || got == nil {
		t.Fatalf("Get after mode set: %v, %v", got, err)
	}
	if got.ToolMode != ToolModeInject {
		t.Errorf("ToolMode: got %q want %q", got.ToolMode, ToolModeInject)
	}
	if !strings.Contains(out.String(), "inject") {
		t.Errorf("expected confirmation in output; got: %s", out.String())
	}
}

func TestCmdModelMode_SetsNamedModel(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "granite4.1:8b")

	var out strings.Builder
	if err := cmdModel(a, []string{"mode", "llama3.2:latest", "prose"}, &out); err != nil {
		t.Fatalf("cmdModel mode llama3.2 prose: %v", err)
	}

	got, err := mc.Get("llama3.2:latest")
	if err != nil || got == nil {
		t.Fatalf("Get llama3.2 after mode set: %v, %v", got, err)
	}
	if got.ToolMode != ToolModeProse {
		t.Errorf("ToolMode: got %q want %q", got.ToolMode, ToolModeProse)
	}
}

func TestCmdModelMode_InvalidMode(t *testing.T) {
	a, _ := newTestAgentWithCache(t, "phi4:latest")

	var out strings.Builder
	if err := cmdModel(a, []string{"mode", "turbo"}, &out); err != nil {
		t.Fatalf("unexpected error for invalid mode: %v", err)
	}
	if !strings.Contains(out.String(), "Unknown mode") && !strings.Contains(out.String(), "unknown mode") {
		t.Errorf("expected unknown mode message; got: %s", out.String())
	}
}

func TestCmdModelMode_NoModelCache(t *testing.T) {
	a := newTestAgent(t)
	a.ModelCache = nil
	a.Client = newOllamaLLMClient("http://localhost:11434", "phi4:latest", 0)

	var out strings.Builder
	err := cmdModel(a, []string{"mode", "inject"}, &out)
	if err == nil {
		t.Error("expected error when ModelCache is nil")
	}
}

func TestCmdModelMode_ShowCurrentMode(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "phi4:latest")
	// Pre-set a mode so show has something to report.
	if err := mc.Set(&ModelCapability{Name: "phi4:latest", ToolMode: ToolModeNone, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := cmdModel(a, []string{"mode"}, &out); err != nil {
		t.Fatalf("cmdModel mode (show): %v", err)
	}
	if !strings.Contains(out.String(), "none") {
		t.Errorf("expected mode in output; got: %s", out.String())
	}
}

func TestCmdModelMode_AutoClearsOverride(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "phi4:latest")
	// Start with an explicit override.
	if err := mc.Set(&ModelCapability{Name: "phi4:latest", ToolMode: ToolModeInject, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := cmdModel(a, []string{"mode", "auto"}, &out); err != nil {
		t.Fatalf("cmdModel mode auto: %v", err)
	}

	got, err := mc.Get("phi4:latest")
	if err != nil || got == nil {
		t.Fatalf("Get after mode auto: %v, %v", got, err)
	}
	if got.ToolMode != ToolModeAuto {
		t.Errorf("ToolMode after auto: got %q, want %q (ToolModeAuto)", got.ToolMode, ToolModeAuto)
	}
	if !strings.Contains(out.String(), "auto") {
		t.Errorf("expected 'auto' in confirmation message; got: %s", out.String())
	}
}

func TestCmdModelMode_AutoOnNamedModel(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "granite4.1:8b")
	if err := mc.Set(&ModelCapability{Name: "llama3.2:latest", ToolMode: ToolModeProse, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := cmdModel(a, []string{"mode", "llama3.2:latest", "auto"}, &out); err != nil {
		t.Fatalf("cmdModel mode llama3.2 auto: %v", err)
	}

	got, err := mc.Get("llama3.2:latest")
	if err != nil || got == nil {
		t.Fatalf("Get llama3.2 after auto: %v, %v", got, err)
	}
	if got.ToolMode != ToolModeAuto {
		t.Errorf("ToolMode: got %q, want %q (ToolModeAuto)", got.ToolMode, ToolModeAuto)
	}
}

func TestCmdModelMode_ShowAutoWhenNotSet(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "phi4:latest")
	// Ensure entry exists with no explicit ToolMode.
	if err := mc.Set(&ModelCapability{Name: "phi4:latest", ToolMode: ToolModeAuto, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := cmdModel(a, []string{"mode"}, &out); err != nil {
		t.Fatalf("cmdModel mode (show auto): %v", err)
	}
	if !strings.Contains(out.String(), "auto") {
		t.Errorf("expected 'auto' in output when ToolMode is not set; got: %s", out.String())
	}
}
