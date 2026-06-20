package harvey

import (
	"bufio"
	"context"
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
	cfg := DefaultConfig()
	cfg.SafeMode = false // tests exercise command mechanics, not safe mode policy
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
	a.Config.LlamafileModels = []LlamafileEntry{
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
	cfg.LlamafileActive = "qwen-coding"
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
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/qwen.llamafile"},
		{Name: "phi-mini", Path: "/tmp/phi.llamafile"},
	}
	cfg.LlamafileActive = "qwen-coding"
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
