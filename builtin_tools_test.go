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

// newToolAgent creates a minimal Agent with a workspace, registered builtin
// tools, and the given config overrides applied after DefaultConfig().
func newToolAgent(t *testing.T, override func(*Config)) (*Agent, *ToolRegistry) {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cfg := DefaultConfig()
	if override != nil {
		override(cfg)
	}
	reg := NewToolRegistry()
	a := &Agent{
		Config:    cfg,
		Workspace: ws,
		Tools:     reg,
		In:        strings.NewReader(""),
		Out:       io.Discard,
	}
	RegisterBuiltinTools(reg, a)
	return a, reg
}

// dispatch is a thin convenience wrapper around ToolRegistry.Dispatch.
func dispatch(t *testing.T, reg *ToolRegistry, name string, args map[string]any) (string, error) {
	t.Helper()
	var sb strings.Builder
	for k, v := range args {
		if sb.Len() > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%q:%q", k, fmt.Sprint(v)))
	}
	argsJSON := "{" + sb.String() + "}"
	return reg.Dispatch(context.Background(), name, argsJSON, 1024*1024)
}

// ─── read_file ────────────────────────────────────────────────────────────────

// TestReadFile_Normal verifies that read_file returns the contents of a plain
// text file in the workspace.
func TestReadFile_Normal(t *testing.T) {
	a, reg := newToolAgent(t, nil)

	if err := a.Workspace.WriteFile("hello.txt", []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := dispatch(t, reg, "read_file", map[string]any{"path": "hello.txt"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("read_file: want content to include 'hello world', got %q", got)
	}
}

// TestReadFile_ChunkingDisabled verifies that when chunking.enabled is false,
// read_file reads an over-budget file without triggering the chunking prompt.
func TestReadFile_ChunkingDisabled(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		// Very small context so any file would be "over-budget" if chunking were enabled.
		cfg.Ollama.ContextLength = 100
		cfg.Chunking = DefaultChunkConfig()
		cfg.Chunking.Enabled = false
	})
	// Use a mock client so a.Client != nil (guards in read_file check this).
	a.Client = &mockLLMClient{}

	// Write a file large enough to exceed the 100-token budget.
	content := strings.Repeat("the quick brown fox jumps over the lazy dog ", 20)
	if err := a.Workspace.WriteFile("big.txt", []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := dispatch(t, reg, "read_file", map[string]any{"path": "big.txt"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(got, "quick brown fox") {
		t.Errorf("read_file: expected plain file content, got %q", got)
	}
}

// TestReadFile_ChunkingEnabledUserCancels verifies that when chunking is
// enabled and the file exceeds the context budget, typing "no" returns the
// cancellation sentinel without reading the file body.
func TestReadFile_ChunkingEnabledUserCancels(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		cfg.Ollama.ContextLength = 100
		cfg.Chunking = DefaultChunkConfig()
		cfg.Chunking.Enabled = true
	})
	a.Client = &mockLLMClient{}
	// Pipe "no" as user input so promptChunkInstruction cancels.
	a.In = strings.NewReader("no\n")

	content := strings.Repeat("the quick brown fox jumps over the lazy dog ", 20)
	if err := a.Workspace.WriteFile("big2.txt", []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := dispatch(t, reg, "read_file", map[string]any{"path": "big2.txt"})
	if err != nil {
		t.Fatalf("read_file: unexpected error: %v", err)
	}
	if !strings.Contains(got, "cancelled") {
		t.Errorf("read_file: expected cancellation message, got %q", got)
	}
}

// TestReadFile_ChunkingEnabledContextLimitUnknown is the regression test for
// the llamafile adopt-external-server bug: when effectiveContextLimit() is
// unknown (no Ollama.ContextLength, no llamafile entry, no model cache
// entry), remainingContext() returns 0. The chunking guard must still fire
// with a conservative fallback budget instead of silently skipping straight
// to a full raw read (which is what produced the Gemma4-E4B garbled output
// reported in TODO.md — the chunk-prompt option was never reached).
func TestReadFile_ChunkingEnabledContextLimitUnknown(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		// Deliberately leave context length unset on every source
		// effectiveContextLimit() checks — this is the "unknown limit" case.
		cfg.Chunking = DefaultChunkConfig()
		cfg.Chunking.Enabled = true
	})
	a.Client = &mockLLMClient{}
	// Pipe "no" so the test only needs to observe that the chunk prompt was
	// reached, not exercise the full map-reduce path.
	a.In = strings.NewReader("no\n")

	// 20000 bytes comfortably exceeds the 4096-token (~16384 byte) fallback
	// budget at the default 0.80 threshold.
	content := strings.Repeat("the quick brown fox jumps over the lazy dog ", 500)
	if err := a.Workspace.WriteFile("huge.txt", []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := dispatch(t, reg, "read_file", map[string]any{"path": "huge.txt"})
	if err != nil {
		t.Fatalf("read_file: unexpected error: %v", err)
	}
	if !strings.Contains(got, "cancelled") {
		t.Errorf("read_file: expected the chunking guard to fire and reach the cancel path even with an unknown context limit; got %q", got)
	}
}

// TestReadFile_PermissionDenied verifies that read_file returns a permission
// error when the agent's permissions exclude reading the requested path.
func TestReadFile_PermissionDenied(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		// Restrict to read-only at root, no read on secrets/
		cfg.Security.Permissions = map[string][]string{
			".":        {PermRead, PermWrite, PermExec, PermDelete},
			"secrets/": {PermExec}, // no read
		}
	})

	absSecrets := filepath.Join(a.Workspace.Root, "secrets")
	if err := os.MkdirAll(absSecrets, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(absSecrets, "token.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := dispatch(t, reg, "read_file", map[string]any{"path": "secrets/token.txt"})
	if err == nil {
		t.Fatal("read_file: expected permission error, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("read_file: expected 'permission denied' in error, got %q", err.Error())
	}
}

// TestReadFile_PDF_NoTool verifies that read_file returns an error when a PDF
// file is requested but pdftotext is not available (or the file is not a real
// PDF), without panicking.
func TestReadFile_PDF_NoTool(t *testing.T) {
	a, reg := newToolAgent(t, nil)

	// Write a fake "PDF" (plain text with .pdf extension).
	if err := a.Workspace.WriteFile("fake.pdf", []byte("not a real pdf"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should either succeed (if pdftotext is installed and handles it) or return
	// a read_file error.  The critical invariant is: no panic and the error (if
	// any) is wrapped as "read_file: ...".
	_, err := dispatch(t, reg, "read_file", map[string]any{"path": "fake.pdf"})
	if err != nil && !strings.HasPrefix(err.Error(), "read_file:") {
		t.Errorf("expected error prefixed with 'read_file:', got %q", err.Error())
	}
}

// ─── write_file ───────────────────────────────────────────────────────────────

// TestWriteFile_Basic verifies that write_file creates a file with the given
// content and returns a byte-count confirmation message.
func TestWriteFile_Basic(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = false
	})

	got, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "output.txt",
		"content": "hello from write_file",
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(got, "wrote") {
		t.Errorf("write_file: expected confirmation message, got %q", got)
	}

	data, readErr := os.ReadFile(filepath.Join(a.Workspace.Root, "output.txt"))
	if readErr != nil {
		t.Fatalf("verify: %v", readErr)
	}
	if string(data) != "hello from write_file" {
		t.Errorf("write_file: file content %q, want 'hello from write_file'", string(data))
	}
}

// TestWriteFile_AutoFormatGo verifies that write_file applies gofmt to a Go
// source file when auto-format is enabled and safe_mode is off.
func TestWriteFile_AutoFormatGo(t *testing.T) {
	_, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = true
		cfg.Security.SafeMode = false // FileFormatter requires safe_mode=false
	})

	// Deliberately un-formatted Go source (extra blank lines, bad indent).
	unformatted := "package main\n\nfunc    main() {\nfmt.Println(\"hi\")\n}\n"

	got, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "main.go",
		"content": unformatted,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}

	// The message should mention "formatted" (either "formatted" or "already formatted").
	if !strings.Contains(got, "formatted") {
		t.Errorf("write_file auto-format: expected 'formatted' in message, got %q", got)
	}
}

// captureStatus is a StatusReporter test double that records every
// SensorEvent reported to it, for asserting on Direction A sensor wiring.
type captureStatus struct {
	events []SensorEvent
}

func (c *captureStatus) UpdateStatus(msg string) {}
func (c *captureStatus) ReportSensor(ev SensorEvent) {
	c.events = append(c.events, ev)
}

// TestWriteFile_FormatCheckSensor_reportsWhenAutoFormatOff verifies that an
// unformatted Go file produces a human-visible SensorEvent when AutoFormat
// is off (so the on-disk content genuinely still differs from gofmt's
// output), and that no tokens are spent on it in the default config.
func TestWriteFile_FormatCheckSensor_reportsWhenAutoFormatOff(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = false
	})
	status := &captureStatus{}
	a.ActiveStatus = status

	unformatted := "package main\n\nfunc    main() {\nfmt.Println(\"hi\")\n}\n"
	got, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "main.go",
		"content": unformatted,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}

	if len(status.events) == 0 {
		t.Fatal("expected a SensorEvent for unformatted Go content when AutoFormat is off")
	}
	if status.events[0].Class != Computational {
		t.Errorf("expected Computational class, got %v", status.events[0].Class)
	}
	if strings.Contains(got, "not in canonical format") {
		t.Errorf("expected no format-check finding in tool result by default, got %q", got)
	}
}

// TestWriteFile_FormatCheckSensor_injectsWhenConfigured verifies that
// SensorInjectFormatFindings appends the finding to the tool-result string
// (spending tokens), as an explicit opt-in.
func TestWriteFile_FormatCheckSensor_injectsWhenConfigured(t *testing.T) {
	_, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = false
		cfg.SensorInjectFormatFindings = true
	})

	unformatted := "package main\n\nfunc    main() {\nfmt.Println(\"hi\")\n}\n"
	got, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "main.go",
		"content": unformatted,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(got, "not in canonical format") {
		t.Errorf("expected format-check finding appended to tool result, got %q", got)
	}
}

// TestWriteFile_FormatCheckSensor_silentWhenAlreadyFormatted verifies that a
// successful auto-format leaves nothing for Check() to find — no sensor
// event, no tool-result appendix.
func TestWriteFile_FormatCheckSensor_silentWhenAlreadyFormatted(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = true
		cfg.Security.SafeMode = false
	})
	status := &captureStatus{}
	a.ActiveStatus = status

	unformatted := "package main\n\nfunc    main() {\nfmt.Println(\"hi\")\n}\n"
	_, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "main.go",
		"content": unformatted,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if len(status.events) != 0 {
		t.Errorf("expected no sensor findings after successful auto-format, got %v", status.events)
	}
}

// writeGoMod writes a minimal go.mod into the agent's workspace root, so
// runGoVet's hasGoModule gate passes.
func writeGoMod(t *testing.T, a *Agent) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "go.mod"), []byte("module testmod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("writeGoMod: %v", err)
	}
}

func hasEventKind(events []SensorEvent, kind string) bool {
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

// TestWriteFile_GoVetSensor_reportsRealFinding verifies that a real go vet
// finding (a Printf format/argument mismatch) is both human-visible
// (SensorEvent) and model-visible (tool-result appendix, unconditionally —
// go vet has no low-severity tier to gate behind, per
// computational-sensors-design.md).
func TestWriteFile_GoVetSensor_reportsRealFinding(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = true
		cfg.Security.SafeMode = false
	})
	writeGoMod(t, a)
	status := &captureStatus{}
	a.ActiveStatus = status

	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Printf(\"%d\\n\", \"not a number\")\n}\n"
	got, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "main.go",
		"content": src,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}

	if !hasEventKind(status.events, "go_vet") {
		t.Errorf("expected a go_vet SensorEvent, got %v", status.events)
	}
	if !strings.Contains(got, "Printf") {
		t.Errorf("expected the go vet finding appended to the tool result, got %q", got)
	}
}

// TestWriteFile_GoVetSensor_suppressesCompileError verifies that a
// mid-edit compile error (undefined symbol) never surfaces as a false
// sensor finding and never blocks the write.
func TestWriteFile_GoVetSensor_suppressesCompileError(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = true
		cfg.Security.SafeMode = false
	})
	writeGoMod(t, a)
	status := &captureStatus{}
	a.ActiveStatus = status

	src := "package main\n\nfunc main() {\n\tundefinedFunction()\n}\n"
	got, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "main.go",
		"content": src,
	})
	if err != nil {
		t.Fatalf("write_file: %v (compile errors must not block the write)", err)
	}

	if hasEventKind(status.events, "go_vet") {
		t.Errorf("expected no go_vet SensorEvent for a compile error, got %v", status.events)
	}
	if strings.Contains(got, "undefined") {
		t.Errorf("expected no compile-error text in the tool result, got %q", got)
	}
}

// TestWriteFile_GoVetSensor_skipsWithoutGoModule verifies go vet is never
// invoked in a workspace without a go.mod, even for a file that would
// otherwise trigger a finding.
func TestWriteFile_GoVetSensor_skipsWithoutGoModule(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		cfg.AutoFormat = true
		cfg.Security.SafeMode = false
	})
	status := &captureStatus{}
	a.ActiveStatus = status
	// Deliberately no go.mod written.

	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Printf(\"%d\\n\", \"not a number\")\n}\n"
	_, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "main.go",
		"content": src,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}

	if hasEventKind(status.events, "go_vet") {
		t.Errorf("expected go vet to be skipped without a go.mod, got %v", status.events)
	}
}

// TestWriteFile_PermissionDenied verifies that write_file returns a permission
// error when the workspace denies writes on the target path.
func TestWriteFile_PermissionDenied(t *testing.T) {
	agent, _ := newToolAgent(t, func(cfg *Config) {
		// Root has read-only; no write permission.
		cfg.Security.Permissions = map[string][]string{
			".": {PermRead},
		}
	})
	// Re-register with the updated config that has restricted permissions.
	reg2 := NewToolRegistry()
	RegisterBuiltinTools(reg2, agent)

	_, err := dispatch(t, reg2, "write_file", map[string]any{
		"path":    "output.txt",
		"content": "should fail",
	})
	if err == nil {
		t.Fatal("write_file: expected permission error, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("write_file: expected 'permission denied' in error, got %q", err.Error())
	}
}

// ─── list_files ───────────────────────────────────────────────────────────────

// TestListFiles_NestedDirectory verifies that list_files on a subdirectory
// returns only the immediate children of that directory.
func TestListFiles_NestedDirectory(t *testing.T) {
	a, reg := newToolAgent(t, nil)

	// Build:  sub/alpha.txt  sub/beta.txt  root.txt
	subDir := filepath.Join(a.Workspace.Root, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(subDir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "root.txt"), []byte("root"), 0o644); err != nil {
		t.Fatalf("WriteFile root.txt: %v", err)
	}

	got, err := dispatch(t, reg, "list_files", map[string]any{"path": "sub"})
	if err != nil {
		t.Fatalf("list_files: %v", err)
	}
	if !strings.Contains(got, "alpha.txt") {
		t.Errorf("list_files: expected 'alpha.txt' in output, got:\n%s", got)
	}
	if !strings.Contains(got, "beta.txt") {
		t.Errorf("list_files: expected 'beta.txt' in output, got:\n%s", got)
	}
	// Root-level file should NOT appear in the sub/ listing.
	if strings.Contains(got, "root.txt") {
		t.Errorf("list_files: 'root.txt' should not appear in sub/ listing, got:\n%s", got)
	}
}

// TestListFiles_Root verifies that list_files with no path argument lists the
// workspace root.
func TestListFiles_Root(t *testing.T) {
	a, reg := newToolAgent(t, nil)

	if err := a.Workspace.WriteFile("readme.md", []byte("# readme"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := dispatch(t, reg, "list_files", map[string]any{})
	if err != nil {
		t.Fatalf("list_files: %v", err)
	}
	if !strings.Contains(got, "readme.md") {
		t.Errorf("list_files root: expected 'readme.md', got:\n%s", got)
	}
}

// ─── path validation ──────────────────────────────────────────────────────────

// TestReadFile_PathTraversal verifies that read_file rejects paths that escape
// the workspace root via "../" traversal.
func TestReadFile_PathTraversal(t *testing.T) {
	_, reg := newToolAgent(t, nil)

	_, err := dispatch(t, reg, "read_file", map[string]any{"path": "../../etc/passwd"})
	if err == nil {
		t.Fatal("read_file: expected error for path traversal, got nil")
	}
}

// TestWriteFile_PathTraversal verifies that write_file rejects paths that
// escape the workspace via "../" traversal.

func TestWriteFile_PathTraversal(t *testing.T) {
	_, reg := newToolAgent(t, func(cfg *Config) { cfg.AutoFormat = false })

	_, err := dispatch(t, reg, "write_file", map[string]any{
		"path":    "../../tmp/escape.txt",
		"content": "escaped",
	})
	if err == nil {
		t.Fatal("write_file: expected error for path traversal, got nil")
	}
}

// ─── filter_context ──────────────────────────────────────────────────────────

func TestFilterContext_EmptyHistory(t *testing.T) {
	_, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })

	result, err := dispatch(t, reg, "filter_context", map[string]any{"criteria": "go test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "nothing to filter") {
		t.Errorf("expected 'nothing to filter'; got: %q", result)
	}
}

func TestFilterContext_KeywordRemovesMatch(t *testing.T) {
	// No RAG store configured → keyword fallback.
	a, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })
	a.AddMessage("system", "You are Harvey.")
	a.AddMessage("user", "question about go test failures")
	a.AddMessage("assistant", "here is the answer")
	a.AddMessage("user", "something completely unrelated")

	result, err := dispatch(t, reg, "filter_context", map[string]any{"criteria": "go test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Filtered 1") {
		t.Errorf("expected 'Filtered 1'; got: %q", result)
	}
	// system + 2 non-matching messages remain
	if len(a.History) != 3 {
		t.Errorf("expected 3 remaining messages; got %d", len(a.History))
	}
	for _, m := range a.History {
		if strings.Contains(strings.ToLower(m.Content), "go test") {
			t.Errorf("filtered message still in history: %q", m.Content)
		}
	}
}

func TestFilterContext_SystemMessagesNeverFiltered(t *testing.T) {
	// Even if the system message matches the criteria, it must be preserved.
	a, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })
	a.AddMessage("system", "go test is part of the system prompt")
	a.AddMessage("user", "go test failures are annoying") // matches → removed
	a.AddMessage("user", "something unrelated")          // no match → kept

	result, err := dispatch(t, reg, "filter_context", map[string]any{"criteria": "go test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Filtered 1") {
		t.Errorf("expected 'Filtered 1'; got: %q", result)
	}
	// system + 1 non-matching user message remain
	if len(a.History) != 2 {
		t.Errorf("expected 2 remaining entries; got %d — %v", len(a.History), a.History)
	}
	if a.History[0].Role != "system" {
		t.Errorf("first entry must be the system message; got role=%q", a.History[0].Role)
	}
}

func TestFilterContext_NoMatches(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })
	a.AddMessage("user", "unrelated content")
	a.AddMessage("assistant", "also unrelated")

	result, err := dispatch(t, reg, "filter_context", map[string]any{"criteria": "go test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Filtered 0") {
		t.Errorf("expected 'Filtered 0'; got: %q", result)
	}
	if len(a.History) != 2 {
		t.Errorf("history should be unchanged; got %d entries", len(a.History))
	}
}

func TestFilterContext_SafeMode(t *testing.T) {
	// DefaultConfig has SafeMode=true.
	a, reg := newToolAgent(t, nil)
	a.AddMessage("user", "question about go test")
	a.AddMessage("assistant", "answer")

	result, err := dispatch(t, reg, "filter_context", map[string]any{"criteria": "go test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "safe mode") {
		t.Errorf("expected 'safe mode' in result; got: %q", result)
	}
	if len(a.History) != 2 {
		t.Errorf("safe mode must not modify history; got %d entries", len(a.History))
	}
}

func TestFilterContext_EmptyCriteria(t *testing.T) {
	_, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })

	_, err := dispatch(t, reg, "filter_context", map[string]any{"criteria": ""})
	if err == nil {
		t.Fatal("expected error for empty criteria")
	}
}

// ─── summary_context ─────────────────────────────────────────────────────────

func TestSummaryContext_NoClient(t *testing.T) {
	a, reg := newToolAgent(t, nil) // a.Client is nil
	a.AddMessage("user", "hello")
	a.AddMessage("assistant", "world")

	result, err := dispatch(t, reg, "summary_context", map[string]any{"span": "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "no LLM client") {
		t.Errorf("expected 'no LLM client' message; got: %q", result)
	}
	if len(a.History) != 2 {
		t.Errorf("history should be unchanged (2 msgs); got %d", len(a.History))
	}
}

func TestSummaryContext_SpanAll(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })
	a.Client = &mockLLMClient{reply: "We discussed Go patterns."}
	a.AddMessage("system", "You are Harvey.")
	a.AddMessage("user", "question one")
	a.AddMessage("assistant", "answer one")
	a.AddMessage("user", "question two")
	a.AddMessage("assistant", "answer two")

	result, err := dispatch(t, reg, "summary_context", map[string]any{"span": "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Summarised") {
		t.Errorf("expected 'Summarised' in result; got: %q", result)
	}
	// system prompt + 1 summary entry
	if len(a.History) != 2 {
		t.Errorf("expected 2 history entries (system + summary); got %d", len(a.History))
	}
	last := a.History[len(a.History)-1]
	if last.Role != "system" {
		t.Errorf("summary entry role = %q, want system", last.Role)
	}
	if !strings.Contains(last.Content, "[Summary]") {
		t.Errorf("expected [Summary] prefix; got: %q", last.Content)
	}
	if !strings.Contains(last.Content, "We discussed Go patterns") {
		t.Errorf("expected LLM reply in summary; got: %q", last.Content)
	}
}

func TestSummaryContext_SpanN(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })
	a.Client = &mockLLMClient{reply: "Summary of first three."}
	for i := 1; i <= 6; i++ {
		if i%2 == 1 {
			a.AddMessage("user", fmt.Sprintf("question %d", i))
		} else {
			a.AddMessage("assistant", fmt.Sprintf("answer %d", i))
		}
	}

	result, err := dispatch(t, reg, "summary_context", map[string]any{"span": "3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Summarised 3") {
		t.Errorf("expected 'Summarised 3' in result; got: %q", result)
	}
	// 1 summary + 3 remaining
	if len(a.History) != 4 {
		t.Errorf("expected 4 history entries (summary + 3 remaining); got %d", len(a.History))
	}
	if a.History[0].Role != "system" || !strings.Contains(a.History[0].Content, "[Summary]") {
		t.Errorf("first entry should be summary system message; got role=%q", a.History[0].Role)
	}
}

func TestSummaryContext_SpanExceedsHistory(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = false })
	a.Client = &mockLLMClient{reply: "Brief summary."}
	a.AddMessage("user", "msg 1")
	a.AddMessage("assistant", "resp 1")
	a.AddMessage("user", "msg 2")

	result, err := dispatch(t, reg, "summary_context", map[string]any{"span": "100"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Summarised 3") {
		t.Errorf("expected 'Summarised 3' in result; got: %q", result)
	}
}

func TestSummaryContext_TooFewMessages(t *testing.T) {
	a, reg := newToolAgent(t, nil)
	a.Client = &mockLLMClient{reply: "irrelevant"}
	a.AddMessage("user", "only one message")

	result, err := dispatch(t, reg, "summary_context", map[string]any{"span": "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not enough") {
		t.Errorf("expected 'not enough' message; got: %q", result)
	}
	if len(a.History) != 1 {
		t.Errorf("history should be unchanged; got %d entries", len(a.History))
	}
}

func TestSummaryContext_SafeMode(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) { cfg.Security.SafeMode = true })
	a.Client = &mockLLMClient{reply: "irrelevant"}
	a.AddMessage("user", "hello")
	a.AddMessage("assistant", "world")

	result, err := dispatch(t, reg, "summary_context", map[string]any{"span": "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "safe mode") {
		t.Errorf("expected 'safe mode' in result; got: %q", result)
	}
	if len(a.History) != 2 {
		t.Errorf("safe mode must not modify history; got %d entries", len(a.History))
	}
}
