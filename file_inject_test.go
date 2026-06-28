package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── injectFileContext ─────────────────────────────────────────────────────────

func TestInjectFileContext_NoPathTokens(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	prompt := "What is the meaning of life?"
	got := injectFileContext(ws, prompt)
	if got != prompt {
		t.Errorf("expected unchanged prompt; got: %s", got)
	}
}

func TestInjectFileContext_MissingFile(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	prompt := "Please review missing_file.md"
	got := injectFileContext(ws, prompt)
	if got != prompt {
		t.Errorf("expected unchanged prompt when file does not exist; got: %s", got)
	}
}

func TestInjectFileContext_ExistingTextFile(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)

	content := "# Hello\n\nThis is a test document.\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	prompt := "Please review doc.md and summarise it."
	got := injectFileContext(ws, prompt)

	if !strings.Contains(got, "### File: doc.md") {
		t.Errorf("expected file header in output; got:\n%s", got)
	}
	if !strings.Contains(got, content) {
		t.Errorf("expected file content injected; got:\n%s", got)
	}
	if !strings.HasSuffix(got, prompt) {
		t.Errorf("expected original prompt at end; got:\n%s", got)
	}
}

func TestInjectFileContext_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	prompt := "Compare a.md and b.go"
	got := injectFileContext(ws, prompt)

	if !strings.Contains(got, "### File: a.md") {
		t.Errorf("expected a.md header; got:\n%s", got)
	}
	if !strings.Contains(got, "### File: b.go") {
		t.Errorf("expected b.go header; got:\n%s", got)
	}
}

func TestInjectFileContext_DeduplicatesRepeatedPath(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	prompt := "Look at note.md, then review note.md again."
	got := injectFileContext(ws, prompt)

	count := strings.Count(got, "### File: note.md")
	if count != 1 {
		t.Errorf("expected note.md injected exactly once, got %d times", count)
	}
}

func TestInjectFileContext_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)

	big := make([]byte, maxInjectFileBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "huge.md"), big, 0644); err != nil {
		t.Fatal(err)
	}

	prompt := "Read huge.md"
	got := injectFileContext(ws, prompt)
	if got != prompt {
		t.Errorf("expected unchanged prompt for oversized file; got:\n%s", got)
	}
}

func TestInjectFileContext_NonTextExtension(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	if err := os.WriteFile(filepath.Join(dir, "image.png"), []byte("fakepng"), 0644); err != nil {
		t.Fatal(err)
	}

	// .png contains "/" in path separators only if subdirectory; here it's root-level.
	// looksLikePath("image.png") → false (not in language registry).
	// Confirm the function leaves it alone.
	prompt := "Look at image.png"
	got := injectFileContext(ws, prompt)
	if got != prompt {
		t.Errorf("expected unchanged prompt for non-text extension; got:\n%s", got)
	}
}

func TestInjectFileContext_Directory(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	// "subdir" has no extension and no "/" separator, so looksLikePath is false.
	// A path like "subdir/foo.md" (which contains "/") would match looksLikePath,
	// but the stat would show it's not a file we can inject.
	prompt := "What is in subdir/?"
	got := injectFileContext(ws, prompt)
	// subdir/ as a path-like token: looksLikePath says yes (contains "/").
	// AbsPath resolves it; Stat says it's a directory — should be skipped.
	if strings.Contains(got, "### File:") {
		t.Errorf("expected no injection for directory token; got:\n%s", got)
	}
}

// ─── toolsReliable ────────────────────────────────────────────────────────────

func TestToolsReliable_ToolsDisabled(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ToolsEnabled = false
	if a.toolsReliable() {
		t.Error("expected toolsReliable=false when ToolsEnabled=false")
	}
}

func TestToolsReliable_NilToolRegistry(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = nil
	if a.toolsReliable() {
		t.Error("expected toolsReliable=false when Tools=nil")
	}
}

func TestToolsReliable_NilModelCache(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = &ToolRegistry{}
	a.ModelCache = nil
	// No cache means capability unknown — conservative: not reliable.
	if a.toolsReliable() {
		t.Error("expected toolsReliable=false when ModelCache=nil")
	}
}

func TestToolsReliable_CapYes(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	mc, err := OpenModelCache(ws, "")
	if err != nil {
		t.Fatalf("OpenModelCache: %v", err)
	}
	defer mc.Close()

	if err := mc.Set(&ModelCapability{Name: "granite4.1:8b", SupportsTools: CapYes}); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = &ToolRegistry{}
	a.ModelCache = mc
	// AnyLLMClient with model name "granite4.1:8b"
	a.Client = newOllamaLLMClient("http://localhost:11434", "granite4.1:8b", 0)

	if !a.toolsReliable() {
		t.Error("expected toolsReliable=true for CapYes model with tools enabled")
	}
}

func TestToolsReliable_CapNo(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	mc, err := OpenModelCache(ws, "")
	if err != nil {
		t.Fatalf("OpenModelCache: %v", err)
	}
	defer mc.Close()

	if err := mc.Set(&ModelCapability{Name: "phi4:latest", SupportsTools: CapNo}); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = &ToolRegistry{}
	a.ModelCache = mc
	a.Client = newOllamaLLMClient("http://localhost:11434", "phi4:latest", 0)

	if a.toolsReliable() {
		t.Error("expected toolsReliable=false for CapNo model")
	}
}

// ─── integration: injectFileContext called on prompt with real file ────────────

func TestInjectFileContext_PreservesPromptAtEnd(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	body := "line 1\nline 2\n"
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	prompt := "Review notes.md"
	got := injectFileContext(ws, prompt)

	// File header comes before the original prompt.
	headerIdx := strings.Index(got, "### File: notes.md")
	promptIdx := strings.Index(got, prompt)
	if headerIdx == -1 || promptIdx == -1 {
		t.Fatalf("missing header or prompt in output:\n%s", got)
	}
	if headerIdx > promptIdx {
		t.Errorf("file header must appear before the original prompt")
	}
}

func TestInjectFileContext_Idempotent(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes"), 0644); err != nil {
		t.Fatal(err)
	}

	once := injectFileContext(ws, "Review notes.md")
	twice := injectFileContext(ws, once)

	count := strings.Count(twice, "### File: notes.md")
	if count != 1 {
		t.Errorf("expected notes.md injected exactly once after two calls, got %d", count)
	}
}

func TestInjectFileContext_SubdirPath(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)

	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "spec.md"), []byte("# Spec"), 0644); err != nil {
		t.Fatal(err)
	}

	prompt := fmt.Sprintf("Please read docs/spec.md")
	got := injectFileContext(ws, prompt)

	if !strings.Contains(got, "### File: docs/spec.md") {
		t.Errorf("expected docs/spec.md injected; got:\n%s", got)
	}
}

// ─── looksLikeCantReadFile ─────────────────────────────────────────────────────

func TestLooksLikeCantReadFile_DetectsRefusals(t *testing.T) {
	cases := []string{
		"I don't have the capability to read files on your behalf.",
		"I don't have the ability to access external files.",
		"I cannot directly read files from your file system.",
		"I can't directly read file contents.",
		"I'm unable to read the file you mentioned.",
		"I'm unable to access files on your system.",
		"I cannot access the file directly.",
		"I can't access the file path provided.",
		"Please provide the file content so I can assist you.",
		"Please provide the content of the file.",
		"Please share the file with me.",
		"Please paste the text here and I can help.",
		"Could you provide the file contents?",
		"I don't have access to the file system.",
		"I don't have direct access to the files.",
	}
	for _, c := range cases {
		if !looksLikeCantReadFile(c) {
			t.Errorf("expected match for: %q", c)
		}
	}
}

func TestLooksLikeCantReadFile_NoFalsePositives(t *testing.T) {
	cases := []string{
		"Here is the content of the file you requested.",
		"The function reads the value from the provided path.",
		"Based on the code in main.go, the issue is on line 42.",
		"I've reviewed the documentation and here is my analysis.",
		"",
	}
	for _, c := range cases {
		if looksLikeCantReadFile(c) {
			t.Errorf("unexpected match for: %q", c)
		}
	}
}

func TestToolsReliable_ToolModeStructured_OverridesCapUnknown(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	mc, _ := OpenModelCache(ws, "")
	defer mc.Close()

	// CapUnknown alone → false. ToolMode="structured" should override to true.
	if err := mc.Set(&ModelCapability{Name: "q:latest", SupportsTools: CapUnknown, ToolMode: ToolModeStructured, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = &ToolRegistry{}
	a.ModelCache = mc
	a.Client = newOllamaLLMClient("http://localhost:11434", "q:latest", 0)

	if !a.toolsReliable() {
		t.Error("expected toolsReliable=true when ToolMode=structured")
	}
}

func TestToolsReliable_ToolModeInject_OverridesCapYes(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	mc, _ := OpenModelCache(ws, "")
	defer mc.Close()

	// CapYes alone → true. ToolMode="inject" must override to false.
	if err := mc.Set(&ModelCapability{Name: "r:latest", SupportsTools: CapYes, ToolMode: ToolModeInject, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = &ToolRegistry{}
	a.ModelCache = mc
	a.Client = newOllamaLLMClient("http://localhost:11434", "r:latest", 0)

	if a.toolsReliable() {
		t.Error("expected toolsReliable=false when ToolMode=inject (even with CapYes)")
	}
}

func TestToolsReliable_ToolModeNone(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	mc, _ := OpenModelCache(ws, "")
	defer mc.Close()
	if err := mc.Set(&ModelCapability{Name: "s:latest", SupportsTools: CapYes, ToolMode: ToolModeNone, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = &ToolRegistry{}
	a.ModelCache = mc
	a.Client = newOllamaLLMClient("http://localhost:11434", "s:latest", 0)

	if a.toolsReliable() {
		t.Error("expected toolsReliable=false for ToolMode=none")
	}
}

func TestToolsReliable_ToolModeProse(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	mc, _ := OpenModelCache(ws, "")
	defer mc.Close()
	if err := mc.Set(&ModelCapability{Name: "t:latest", SupportsTools: CapYes, ToolMode: ToolModeProse, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.ToolsEnabled = true
	a.Tools = &ToolRegistry{}
	a.ModelCache = mc
	a.Client = newOllamaLLMClient("http://localhost:11434", "t:latest", 0)

	if a.toolsReliable() {
		t.Error("expected toolsReliable=false for ToolMode=prose")
	}
}

// ─── sequence mock (multi-turn) ───────────────────────────────────────────────

// seqMockLLMClient returns successive replies from a queue; the last reply is
// repeated once exhausted.
type seqMockLLMClient struct {
	replies []string
	idx     int
}

func (m *seqMockLLMClient) Name() string { return "mock-seq" }
func (m *seqMockLLMClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	reply := m.replies[m.idx]
	if m.idx < len(m.replies)-1 {
		m.idx++
	}
	fmt.Fprint(out, reply)
	return ChatStats{}, nil
}
func (m *seqMockLLMClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (m *seqMockLLMClient) Close() error                               { return nil }

// ─── option 2 retry integration ───────────────────────────────────────────────

func TestRunChatTurn_CannotReadRetry_InjectsAndRetries(t *testing.T) {
	a := newTestAgent(t)
	fileContent := "# Secret Notes\nThis is the file content.\n"
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "secret.md"), []byte(fileContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate a model that is known to reliably use tools (option 1 skipped),
	// but this turn it declines to read the file. Option 2 should catch it.
	a.toolsReliableOverride = func() bool { return true }

	// First call: model declines to read. Second call: model answers using the injected content.
	a.Client = &seqMockLLMClient{
		replies: []string{
			"I don't have the capability to read files. Please provide the content.",
			"The document discusses secret notes with placeholder content.",
		},
	}

	var out strings.Builder
	_, _, err := a.runChatTurn(context.Background(), "Please review secret.md", &out,
		bufio.NewReader(strings.NewReader("")), false, "HARVEY")
	if err != nil {
		t.Fatalf("runChatTurn: %v", err)
	}

	outputStr := out.String()
	// Warning line must appear.
	if !strings.Contains(outputStr, "retrying with content") {
		t.Errorf("expected retry warning in output; got:\n%s", outputStr)
	}
	// Final displayed reply must be the retry response (not the refusal).
	if !strings.Contains(outputStr, "The document discusses secret notes") {
		t.Errorf("expected retry response in output; got:\n%s", outputStr)
	}
	// The file content must appear in the user message sent to the model on retry.
	// Verify by checking the history: the user message should contain the injected block.
	var userMessages []string
	for _, m := range a.History {
		if m.Role == "user" {
			userMessages = append(userMessages, m.Content)
		}
	}
	if len(userMessages) == 0 {
		t.Fatal("no user messages in history after retry")
	}
	lastUser := userMessages[len(userMessages)-1]
	if !strings.Contains(lastUser, "### File: secret.md") {
		t.Errorf("expected file header in final user message; got:\n%s", lastUser)
	}
	if !strings.Contains(lastUser, fileContent) {
		t.Errorf("expected file content in final user message; got:\n%s", lastUser)
	}
}

func TestRunChatTurn_CannotReadRetry_SkipsWhenNoFiles(t *testing.T) {
	a := newTestAgent(t)
	// No files in workspace; refusal pattern triggers but nothing to inject.
	a.Client = &seqMockLLMClient{
		replies: []string{
			"I don't have the capability to read files.",
		},
	}

	var out strings.Builder
	_, _, err := a.runChatTurn(context.Background(), "Explain quantum mechanics", &out,
		bufio.NewReader(strings.NewReader("")), false, "HARVEY")
	if err != nil {
		t.Fatalf("runChatTurn: %v", err)
	}

	// No retry warning — no files to inject.
	if strings.Contains(out.String(), "retrying with content") {
		t.Errorf("unexpected retry warning when no files to inject; output:\n%s", out.String())
	}
}

// ─── injectOrChunk ────────────────────────────────────────────────────────────

// TestInjectOrChunk_SmallFile verifies that a file within maxInjectFileBytes is
// injected directly, identical to injectFileContext behaviour.
func TestInjectOrChunk_SmallFile(t *testing.T) {
	a := newTestAgent(t)
	a.Config.OllamaContextLength = 8192
	a.Config.Chunking = DefaultChunkConfig()
	a.Client = &mockLLMClient{reply: "ok"}

	content := "small file content"
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "small.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	got := a.injectOrChunk(context.Background(), "review small.md", &out)

	if !strings.Contains(got, "### File: small.md") {
		t.Errorf("expected file header for small file; got:\n%s", got)
	}
	if !strings.Contains(got, content) {
		t.Errorf("expected file content; got:\n%s", got)
	}
}

// TestInjectOrChunk_LargeFileChunkingDisabled verifies that a large file is
// skipped with a hint message when chunking is disabled.
func TestInjectOrChunk_LargeFileChunkingDisabled(t *testing.T) {
	a := newTestAgent(t)
	a.Config.OllamaContextLength = 100
	a.Config.Chunking = DefaultChunkConfig()
	a.Config.Chunking.Enabled = false
	a.Client = &mockLLMClient{reply: "ok"}

	big := strings.Repeat("word ", maxInjectFileBytes/4) // > 16KB
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "big.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	got := a.injectOrChunk(context.Background(), "review big.md", &out)

	if strings.Contains(got, "### File: big.md") {
		t.Errorf("large file should not be injected when chunking is disabled")
	}
	if got != "review big.md" {
		t.Errorf("prompt should be unchanged when file is skipped; got:\n%s", got)
	}
	hint := out.String()
	if !strings.Contains(hint, "skipping") {
		t.Errorf("expected skip hint in output; got:\n%s", hint)
	}
}

// TestInjectOrChunk_LargeFileUserCancels verifies that typing "no" at the
// chunk prompt leaves the file uninjected.
func TestInjectOrChunk_LargeFileUserCancels(t *testing.T) {
	a := newTestAgent(t)
	a.Config.OllamaContextLength = 100 // tiny context so any large file exceeds budget
	a.Config.Chunking = DefaultChunkConfig()
	a.Config.Chunking.Enabled = true
	a.Client = &mockLLMClient{reply: "chunk summary"}
	a.In = strings.NewReader("no\n")

	big := strings.Repeat("paragraph text here. ", maxInjectFileBytes/4)
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "large.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	got := a.injectOrChunk(context.Background(), "review large.md", &out)

	if strings.Contains(got, "### File:") || strings.Contains(got, "### Analysis of") {
		t.Errorf("cancelled chunking should leave prompt unmodified; got:\n%s", got)
	}
	if got != "review large.md" {
		t.Errorf("prompt should be unchanged after cancel; got:\n%s", got)
	}
}

// TestInjectOrChunk_LargeFileRunsChunking verifies that when the user provides
// an instruction, RunChunkedAnalysis runs and its synthesis is prepended.
func TestInjectOrChunk_LargeFileRunsChunking(t *testing.T) {
	a := newTestAgent(t)
	a.Config.OllamaContextLength = 100
	a.Config.Chunking = DefaultChunkConfig()
	a.Config.Chunking.Enabled = true
	a.Client = &mockLLMClient{reply: "chunk summary of this section"}
	a.In = strings.NewReader("summarise each section\n")

	big := strings.Repeat("This is a paragraph about something important. ", maxInjectFileBytes/4)
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "doc.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	got := a.injectOrChunk(context.Background(), "review doc.md", &out)

	if !strings.Contains(got, "### Analysis of doc.md") {
		t.Errorf("expected analysis header in prompt; got:\n%s", got)
	}
	if !strings.HasSuffix(got, "review doc.md") {
		t.Errorf("original prompt should remain at end; got:\n%s", got)
	}
}

// TestInjectOrChunk_LargeFileFitsInBudget verifies that a file larger than
// maxInjectFileBytes but within the context budget is injected directly
// without prompting the user.
func TestInjectOrChunk_LargeFileFitsInBudget(t *testing.T) {
	a := newTestAgent(t)
	// Very large context limit so the file easily fits in budget.
	a.Config.OllamaContextLength = 1_000_000
	a.Config.Chunking = DefaultChunkConfig()
	a.Config.Chunking.Enabled = true
	a.Client = &mockLLMClient{reply: "ok"}
	// a.In defaults to strings.NewReader("") — no input needed since prompt is not shown.

	// File slightly larger than maxInjectFileBytes (16KB) but << 1M-token budget.
	medium := strings.Repeat("some content ", (maxInjectFileBytes+512)/13)
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "medium.md"), []byte(medium), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	got := a.injectOrChunk(context.Background(), "review medium.md", &out)

	if !strings.Contains(got, "### File: medium.md") {
		t.Errorf("file within budget should be injected directly; got:\n%s", got)
	}
	// No prompt should have been shown.
	if strings.Contains(out.String(), "Context overflow") {
		t.Errorf("should not show context-overflow prompt when file fits in budget; out:\n%s", out.String())
	}
}

func TestRunChatTurn_CannotReadRetry_SkipsWhenAlreadyInjected(t *testing.T) {
	a := newTestAgent(t)
	// File exists but will be injected by option 1 (toolsReliable=false by default
	// since no ModelCache is set). If the model still refuses, option 2 should not
	// retry because injectFileContext is idempotent — augmented == retryAugmented.
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "data.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	callCount := 0
	type countingMock struct{ seqMockLLMClient }
	a.Client = &seqMockLLMClient{
		replies: []string{
			"I don't have the capability to read files.",
			"second answer",
		},
	}

	// Option 1 fires (toolsReliable=false) — augmented already has ### File: data.md.
	// Option 2 should detect retryAugmented == augmented and skip.
	_ = callCount
	var out strings.Builder
	a.runChatTurn(context.Background(), "Review data.md", &out,
		bufio.NewReader(strings.NewReader("")), false, "HARVEY")

	// Only one call to the model (no retry).
	if strings.Contains(out.String(), "retrying with content") {
		t.Errorf("unexpected retry when option 1 already injected content")
	}
}
