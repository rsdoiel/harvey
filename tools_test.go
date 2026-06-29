package harvey

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── resolveWorkspacePath ─────────────────────────────────────────────────────

func TestResolveWorkspacePath_relative(t *testing.T) {
	root := t.TempDir()
	// Create the file so EvalSymlinks succeeds.
	f := filepath.Join(root, "file.txt")
	os.WriteFile(f, []byte("hi"), 0o644)

	// Resolve symlinks on the expected path (macOS /var → /private/var).
	wantReal, _ := filepath.EvalSymlinks(f)
	if wantReal == "" {
		wantReal = f
	}

	got, err := resolveWorkspacePath(root, "file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantReal {
		t.Errorf("got %q, want %q", got, wantReal)
	}
}

func TestResolveWorkspacePath_traversal(t *testing.T) {
	root := t.TempDir()
	_, err := resolveWorkspacePath(root, "../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestResolveWorkspacePath_agentsBlocked(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	os.MkdirAll(agentsDir, 0o755)
	secret := filepath.Join(agentsDir, "harvey.yaml")
	os.WriteFile(secret, []byte("secret"), 0o644)

	_, err := resolveWorkspacePath(root, "agents/harvey.yaml")
	if err == nil {
		t.Fatal("expected error for agents/ path, got nil")
	}
}

func TestResolveWorkspacePath_sensitiveFile(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, ".env")
	os.WriteFile(envFile, []byte("SECRET=1"), 0o644)

	_, err := resolveWorkspacePath(root, ".env")
	if err == nil {
		t.Fatal("expected error for .env file, got nil")
	}
}

// ── sensitiveFileDenied ───────────────────────────────────────────────────────

func TestSensitiveFileDenied(t *testing.T) {
	cases := []struct {
		path    string
		denied  bool
	}{
		{"/proj/.env", true},
		{"/proj/.env.local", true},
		{"/proj/secret.pem", true},
		{"/proj/key.key", true},
		{"/proj/cert.p12", true},
		{"/proj/cert.pfx", true},
		{"/proj/harvey.yaml", true},
		{"/proj/main.go", false},
		{"/proj/README.md", false},
		{"/proj/config.yaml", false},
	}
	for _, c := range cases {
		got := sensitiveFileDenied(c.path)
		if got != c.denied {
			t.Errorf("sensitiveFileDenied(%q) = %v, want %v", c.path, got, c.denied)
		}
	}
}

// ── capOutput ────────────────────────────────────────────────────────────────

func TestCapOutput_underLimit(t *testing.T) {
	s := strings.Repeat("x", 100)
	got := capOutput(s, 200)
	if got != s {
		t.Errorf("expected unchanged output, got truncated")
	}
}

func TestCapOutput_overLimit(t *testing.T) {
	s := strings.Repeat("x", 200)
	got := capOutput(s, 100)
	if len(got) <= 100 {
		t.Fatal("expected output to be longer than cap (truncation notice added)")
	}
	if !strings.Contains(got, "[output truncated at 100 bytes]") {
		t.Errorf("truncation notice missing: %q", got)
	}
	if got[:100] != s[:100] {
		t.Errorf("first 100 bytes don't match original")
	}
}

// ── ToolRegistry ─────────────────────────────────────────────────────────────

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	r.RegisterTool("echo", "echo back", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		msg, _ := args["msg"].(string)
		return msg, nil
	})

	if r.Len() != 1 {
		t.Fatalf("expected 1 tool, got %d", r.Len())
	}
	_, handler, ok := r.GetTool("echo")
	if !ok {
		t.Fatal("expected echo to be registered")
	}
	got, err := handler(context.Background(), map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestToolRegistry_GetToolSchemas_alphabetical(t *testing.T) {
	r := NewToolRegistry()
	for _, name := range []string{"z_tool", "a_tool", "m_tool"} {
		n := name
		r.RegisterTool(n, "desc", map[string]any{"type": "object"}, func(ctx context.Context, args map[string]any) (string, error) { return n, nil })
	}
	schemas := r.GetToolSchemas()
	if len(schemas) != 3 {
		t.Fatalf("expected 3 schemas, got %d", len(schemas))
	}
	names := []string{schemas[0].Function.Name, schemas[1].Function.Name, schemas[2].Function.Name}
	want := []string{"a_tool", "m_tool", "z_tool"}
	for i := range names {
		if names[i] != want[i] {
			t.Errorf("schemas[%d].Name = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestToolRegistry_Dispatch_unknownTool(t *testing.T) {
	r := NewToolRegistry()
	_, err := r.Dispatch(context.Background(), "nonexistent", `{}`, 0)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestToolRegistry_Dispatch_invalidJSON(t *testing.T) {
	r := NewToolRegistry()
	r.RegisterTool("noop", "desc", nil, func(ctx context.Context, args map[string]any) (string, error) { return "", nil })
	_, err := r.Dispatch(context.Background(), "noop", `not-json`, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON arguments")
	}
}

// ── builtin tools ────────────────────────────────────────────────────────────

func makeTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cfg := DefaultConfig()
	cfg.ToolsEnabled = false // don't register tools in NewAgent; we register manually below
	a := &Agent{
		Config:    cfg,
		Workspace: ws,
		AuditBuffer: NewAuditBuffer(16),
	}
	return a, root
}

func TestBuiltinTool_ReadFile(t *testing.T) {
	a, root := makeTestAgent(t)
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "read_file", `{"path":"hello.txt"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestBuiltinTool_ReadFile_traversal(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	_, err := r.Dispatch(context.Background(), "read_file", `{"path":"../../etc/passwd"}`, 0)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestBuiltinTool_WriteFile(t *testing.T) {
	a, root := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	_, err := r.Dispatch(context.Background(), "write_file", `{"path":"out.txt","content":"written"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != "written" {
		t.Errorf("got %q, want %q", string(data), "written")
	}
}

// TestBuiltinTool_WriteFile_emptyPathError checks that an empty or missing
// path returns an error message that includes a concrete example, giving the
// model enough context to retry with a correct argument.
func TestBuiltinTool_WriteFile_emptyPathError(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	for _, argsJSON := range []string{
		`{"path":"","content":"x"}`,
		`{"content":"x"}`,
	} {
		_, err := r.Dispatch(context.Background(), "write_file", argsJSON, 0)
		if err == nil {
			t.Fatalf("args %s: expected error for empty/missing path", argsJSON)
		}
		msg := err.Error()
		if !strings.Contains(msg, "path") {
			t.Errorf("args %s: error should mention 'path', got: %s", argsJSON, msg)
		}
		if !strings.Contains(msg, ".md") && !strings.Contains(msg, "e.g.") {
			t.Errorf("args %s: error should include a concrete example, got: %s", argsJSON, msg)
		}
	}
}

func TestBuiltinTool_ListFiles(t *testing.T) {
	a, root := makeTestAgent(t)
	os.WriteFile(filepath.Join(root, "a.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(root, "b.go"), []byte(""), 0o644)

	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "list_files", `{}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a.go") || !strings.Contains(result, "b.go") {
		t.Errorf("expected both files in listing, got: %q", result)
	}
}

func TestBuiltinTool_SearchFiles(t *testing.T) {
	a, root := makeTestAgent(t)
	os.WriteFile(filepath.Join(root, "code.go"), []byte("func main() {}\nfunc helper() {}\n"), 0o644)

	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "search_files", `{"pattern":"func "}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "code.go") {
		t.Errorf("expected code.go in results, got: %q", result)
	}
}

func TestBuiltinTool_GitCommand_disallowed(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	_, err := r.Dispatch(context.Background(), "git_command", `{"subcommand":"push"}`, 0)
	if err == nil {
		t.Fatal("expected error for disallowed git subcommand")
	}
}

// ── datetime tools ───────────────────────────────────────────────────────────

func TestBuiltinTool_CurrentDatetime_human(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "current_datetime", `{}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"local:", "utc:", "day:", "unix:"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in result, got: %s", want, result)
		}
	}
}

func TestBuiltinTool_CurrentDatetime_rfc3339(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "current_datetime", `{"format":"rfc3339"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "local:") || !strings.Contains(result, "utc:") {
		t.Errorf("expected both local and utc lines, got: %s", result)
	}
}

func TestBuiltinTool_CurrentDatetime_unix(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "current_datetime", `{"format":"unix"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) < 10 {
		t.Errorf("expected a Unix timestamp (10+ digits), got: %s", result)
	}
}

func TestBuiltinTool_DatetimeDiff_knownInterval(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "datetime_diff",
		`{"from":"2026-01-01T00:00:00Z","to":"2026-01-03T02:30:00Z"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "2 days") {
		t.Errorf("expected '2 days' in result, got: %s", result)
	}
	if !strings.Contains(result, "2 hours") {
		t.Errorf("expected '2 hours' in result, got: %s", result)
	}
	if !strings.Contains(result, "30 minutes") {
		t.Errorf("expected '30 minutes' in result, got: %s", result)
	}
}

func TestBuiltinTool_DatetimeDiff_toDefaultsToNow(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	// "from" far in the past; no "to" → should use now without error.
	_, err := r.Dispatch(context.Background(), "datetime_diff",
		`{"from":"2000-01-01T00:00:00Z"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuiltinTool_DatetimeDiff_invalidFrom(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	_, err := r.Dispatch(context.Background(), "datetime_diff", `{"from":"not-a-date"}`, 0)
	if err == nil {
		t.Fatal("expected error for unparseable 'from'")
	}
}

func TestBuiltinTool_FormatDatetime_toRFC3339(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "format_datetime",
		`{"datetime":"2026-05-25","format":"rfc3339"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "2026-05-25") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestBuiltinTool_FormatDatetime_dateOnly(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	result, err := r.Dispatch(context.Background(), "format_datetime",
		`{"datetime":"2026-05-25T09:14:00Z","format":"date"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "2026-05-25" {
		t.Errorf("got %q, want %q", result, "2026-05-25")
	}
}

func TestBuiltinTool_FormatDatetime_invalidInput(t *testing.T) {
	a, _ := makeTestAgent(t)
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	_, err := r.Dispatch(context.Background(), "format_datetime",
		`{"datetime":"not-a-date","format":"rfc3339"}`, 0)
	if err == nil {
		t.Fatal("expected error for unparseable datetime")
	}
}

// ── parseDateTimeString ───────────────────────────────────────────────────────

func TestParseDateTimeString_rfc3339(t *testing.T) {
	_, err := parseDateTimeString("2026-05-25T09:14:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDateTimeString_dateOnly(t *testing.T) {
	_, err := parseDateTimeString("2026-05-25")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDateTimeString_monthDayYear(t *testing.T) {
	_, err := parseDateTimeString("May 25 2026")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDateTimeString_invalid(t *testing.T) {
	_, err := parseDateTimeString("yesterday")
	if err == nil {
		t.Fatal("expected error for unrecognised input")
	}
}

// ── formatDuration ────────────────────────────────────────────────────────────

func TestFormatDuration_days(t *testing.T) {
	d := 2*24*time.Hour + 3*time.Hour + 14*time.Minute
	got := formatDuration(d)
	if !strings.Contains(got, "2 days") || !strings.Contains(got, "3 hours") || !strings.Contains(got, "14 minutes") {
		t.Errorf("unexpected: %q", got)
	}
}

func TestFormatDuration_seconds(t *testing.T) {
	got := formatDuration(45 * time.Second)
	if !strings.Contains(got, "seconds") {
		t.Errorf("expected 'seconds', got %q", got)
	}
}

func TestFormatDuration_singular(t *testing.T) {
	got := formatDuration(1*24*time.Hour + 1*time.Hour + 1*time.Minute)
	if !strings.Contains(got, "1 day") || strings.Contains(got, "1 days") {
		t.Errorf("expected singular 'day', got %q", got)
	}
}

// ── read_file chunking guard ──────────────────────────────────────────────────

// makeChunkTestAgent returns an Agent ready for chunking guard tests.
// contextTokens sets OllamaContextLength so remainingContext is predictable.
// input is wired to a.In for interactive prompt tests.
func makeChunkTestAgent(t *testing.T, contextTokens int, input string) (*Agent, string) {
	t.Helper()
	a, root := makeTestAgent(t)
	a.Config.Ollama.ContextLength = contextTokens
	a.Config.Chunking = DefaultChunkConfig()
	a.Client = &mockLLMClient{reply: "chunk synthesis result"}
	a.In = strings.NewReader(input)
	return a, root
}

func TestReadFile_UnderBudget(t *testing.T) {
	// File is small relative to context — chunking guard must not trigger.
	a, root := makeChunkTestAgent(t, 100000, "") // plenty of context
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)
	result, err := r.Dispatch(context.Background(), "read_file", `{"path":"small.txt"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected file content, got %q", result)
	}
}

func TestReadFile_OverBudget_Cancelled(t *testing.T) {
	// Tiny context, large file, user types "no" → cancelled.
	a, root := makeChunkTestAgent(t, 10, "no\n") // context so tiny any file overflows
	content := strings.Repeat("x", 400) // 400 bytes ≈ 100 tokens > budget ~7
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)
	result, err := r.Dispatch(context.Background(), "read_file", `{"path":"big.txt"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "File read cancelled by user." {
		t.Errorf("expected cancellation message, got %q", result)
	}
}

func TestReadFile_OverBudget_ChunkingDisabled(t *testing.T) {
	// Chunking disabled → file is read normally regardless of size.
	a, root := makeChunkTestAgent(t, 10, "no\n") // tiny context
	a.Config.Chunking.Enabled = false
	content := strings.Repeat("y", 400)
	if err := os.WriteFile(filepath.Join(root, "big2.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)
	result, err := r.Dispatch(context.Background(), "read_file", `{"path":"big2.txt"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "yyyy") {
		t.Errorf("expected file content when chunking disabled, got %q", result)
	}
}

func TestReadFile_OverBudget_AcceptsInstruction(t *testing.T) {
	// User enters a custom instruction → chunk analysis runs, returns synthesis.
	a, root := makeChunkTestAgent(t, 10, "Extract all headings.\n")
	content := strings.Repeat("a", 400)
	if err := os.WriteFile(filepath.Join(root, "big3.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)
	result, err := r.Dispatch(context.Background(), "read_file", `{"path":"big3.md"}`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// mockLLMClient always returns "chunk synthesis result".
	if result != "chunk synthesis result" {
		t.Errorf("expected synthesis result, got %q", result)
	}
}

// ── promptChunkInstruction ────────────────────────────────────────────────────

func TestPromptChunkInstruction_UserTypesNo(t *testing.T) {
	_, cancelled := promptChunkInstruction(strings.NewReader("no\n"), io.Discard, "doc.md", 100, 50, "last msg")
	if !cancelled {
		t.Error("expected cancelled=true when user types 'no'")
	}
}

func TestPromptChunkInstruction_AcceptsSuggestion(t *testing.T) {
	instr, cancelled := promptChunkInstruction(strings.NewReader("\n"), io.Discard, "doc.md", 100, 50, "summarize")
	if cancelled {
		t.Error("expected cancelled=false when user presses Enter with suggestion")
	}
	if instr != "summarize" {
		t.Errorf("expected suggestion %q, got %q", "summarize", instr)
	}
}

func TestPromptChunkInstruction_CustomInstruction(t *testing.T) {
	instr, cancelled := promptChunkInstruction(strings.NewReader("extract headings\n"), io.Discard, "doc.md", 100, 50, "")
	if cancelled {
		t.Error("expected cancelled=false for custom instruction")
	}
	if instr != "extract headings" {
		t.Errorf("expected %q, got %q", "extract headings", instr)
	}
}

func TestPromptChunkInstruction_EmptyNoSuggestion(t *testing.T) {
	_, cancelled := promptChunkInstruction(strings.NewReader("\n"), io.Discard, "doc.md", 100, 50, "")
	if !cancelled {
		t.Error("expected cancelled=true when user presses Enter with no suggestion")
	}
}
