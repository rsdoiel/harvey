package harvey

import (
	"context"
	"fmt"
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
		cfg.OllamaContextLength = 100
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
		cfg.OllamaContextLength = 100
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

// TestReadFile_PermissionDenied verifies that read_file returns a permission
// error when the agent's permissions exclude reading the requested path.
func TestReadFile_PermissionDenied(t *testing.T) {
	a, reg := newToolAgent(t, func(cfg *Config) {
		// Restrict to read-only at root, no read on secrets/
		cfg.Permissions = map[string][]string{
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
		cfg.SafeMode = false // FileFormatter requires safe_mode=false
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

// TestWriteFile_PermissionDenied verifies that write_file returns a permission
// error when the workspace denies writes on the target path.
func TestWriteFile_PermissionDenied(t *testing.T) {
	agent, _ := newToolAgent(t, func(cfg *Config) {
		// Root has read-only; no write permission.
		cfg.Permissions = map[string][]string{
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
