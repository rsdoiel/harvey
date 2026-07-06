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
// ─── retrieve_memory ──────────────────────────────────────────────────────────

// newToolAgentWithMemory creates a newToolAgent and opens a real MemorySystem
// backed by a temp workspace. Callers must call ms.Close() when done.
func newToolAgentWithMemory(t *testing.T) (*Agent, *ToolRegistry, *MemorySystem) {
	t.Helper()
	a, _ := newToolAgent(t, nil)
	ms, err := OpenMemory(a.Workspace, &a.Config.Memory)
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	a.Memory = ms
	// Re-register so the tool handler closes over the updated a.Memory.
	reg := NewToolRegistry()
	RegisterBuiltinTools(reg, a)
	return a, reg, ms
}

func TestRetrieveMemory_NoMemorySystem(t *testing.T) {
	// Agent with no memory configured — tool must return a friendly message, not an error.
	_, reg := newToolAgent(t, nil) // a.Memory is nil
	result, err := dispatch(t, reg, "retrieve_memory", map[string]any{"query": "git"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not available") {
		t.Errorf("expected 'not available' message; got: %q", result)
	}
}

func TestRetrieveMemory_EmptyStore(t *testing.T) {
	// Real empty memory store — Recall returns no results.
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	result, err := dispatch(t, reg, "retrieve_memory", map[string]any{"query": "git rebase"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "no matching memories found") {
		t.Errorf("expected 'no matching memories found'; got: %q", result)
	}
}

func TestRetrieveMemory_FactualMemoriesReturned(t *testing.T) {
	// Save a workspace_profile memory (score 1.0, always first) and verify it appears.
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	doc := &MemoryDoc{
		Meta: MemoryMeta{
			ID:          "test_ws_profile_001",
			Type:        MemoryTypeWorkspaceProfile,
			Confidence:  0.9,
			Description: "Harvey is the terminal coding agent under test",
		},
		FountainBody: "FADE IN:\n\nINT. MEMORY - TEST\n\nThis is a test workspace profile entry.\n\nTHE END.\n",
	}
	if err := ms.Store.Save(doc, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := dispatch(t, reg, "retrieve_memory", map[string]any{"query": "workspace"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[memory context]") {
		t.Errorf("expected '[memory context]' header; got: %q", result)
	}
	if !strings.Contains(result, "workspace profile") {
		t.Errorf("expected 'workspace profile' source header; got: %q", result)
	}
}

func TestRetrieveMemory_TopKCaps(t *testing.T) {
	// Save 3 workspace_profile entries; request top_k=1 — only one should appear.
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	for i := 1; i <= 3; i++ {
		doc := &MemoryDoc{
			Meta: MemoryMeta{
				ID:          fmt.Sprintf("ws_entry_%d", i),
				Type:        MemoryTypeWorkspaceProfile,
				Confidence:  0.9,
				Description: fmt.Sprintf("Profile entry %d", i),
			},
			FountainBody: fmt.Sprintf("FADE IN:\n\nINT. MEMORY - ENTRY %d\n\nContent %d.\n\nTHE END.\n", i, i),
		}
		if err := ms.Store.Save(doc, nil); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	// Use Dispatch directly so top_k is a JSON number, not a quoted string.
	result, err := reg.Dispatch(context.Background(), "retrieve_memory",
		`{"query":"profile","top_k":1}`, 1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With top_k=1 exactly one "Profile entry N" line should appear.
	count := strings.Count(result, "Profile entry ")
	if count != 1 {
		t.Errorf("expected 1 profile entry with top_k=1, got %d; result: %q", count, result)
	}
}

func TestRetrieveMemory_EmptyQuery(t *testing.T) {
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	_, err := dispatch(t, reg, "retrieve_memory", map[string]any{"query": "  "})
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
}

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

// ─── update_memory ───────────────────────────────────────────────────────────

func TestUpdateMemory_NoMemorySystem(t *testing.T) {
	_, reg := newToolAgent(t, nil)
	result, err := dispatch(t, reg, "update_memory", map[string]any{
		"id":      "tool_use_abc123",
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not available") {
		t.Errorf("expected 'not available'; got: %q", result)
	}
}

func TestUpdateMemory_UnknownID(t *testing.T) {
	a, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()
	a.Config.Security.SafeMode = false

	result, err := dispatch(t, reg, "update_memory", map[string]any{
		"id":      "tool_use_doesnotexist",
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found'; got: %q", result)
	}
}

func TestUpdateMemory_Success(t *testing.T) {
	a, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()
	a.Config.Security.SafeMode = false

	// Save a memory directly so we have a known ID to update.
	original := NewMemoryDoc("tool_use_upd001", MemoryTypeToolUse,
		"original description", "original summary", nil)
	original.FountainBody = BuildFountainBody("2026-01-01 00:00:00", [][2]string{{"HARVEY", "original description"}})
	if err := ms.Store.Save(original, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := dispatch(t, reg, "update_memory", map[string]any{
		"id":      "tool_use_upd001",
		"content": "updated description after fix",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Memory updated: tool_use_upd001") {
		t.Errorf("expected 'Memory updated: tool_use_upd001'; got: %q", result)
	}

	// Verify the change persisted in the store.
	metas, err := ms.Store.List("tool_use")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, m := range metas {
		if m.ID == "tool_use_upd001" && strings.Contains(m.Description, "updated description after fix") {
			found = true
		}
	}
	if !found {
		t.Error("updated description should be persisted in the store")
	}
}

func TestUpdateMemory_SafeMode(t *testing.T) {
	// DefaultConfig has SafeMode=true.
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	doc := NewMemoryDoc("tool_use_safe001", MemoryTypeToolUse, "original", "original", nil)
	doc.FountainBody = BuildFountainBody("2026-01-01 00:00:00", [][2]string{{"HARVEY", "original"}})
	if err := ms.Store.Save(doc, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := dispatch(t, reg, "update_memory", map[string]any{
		"id":      "tool_use_safe001",
		"content": "changed content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "safe mode") {
		t.Errorf("expected 'safe mode'; got: %q", result)
	}
	// Original description must be unchanged.
	metas, _ := ms.Store.List("tool_use")
	for _, m := range metas {
		if m.ID == "tool_use_safe001" && strings.Contains(m.Description, "changed content") {
			t.Error("safe mode must not modify the stored memory")
		}
	}
}

// ─── delete_memory ───────────────────────────────────────────────────────────

func TestDeleteMemory_NoMemorySystem(t *testing.T) {
	_, reg := newToolAgent(t, nil)
	result, err := dispatch(t, reg, "delete_memory", map[string]any{"id": "tool_use_abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not available") {
		t.Errorf("expected 'not available'; got: %q", result)
	}
}

func TestDeleteMemory_UnknownID(t *testing.T) {
	a, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()
	a.Config.Security.SafeMode = false

	result, err := dispatch(t, reg, "delete_memory", map[string]any{"id": "tool_use_doesnotexist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found'; got: %q", result)
	}
}

func TestDeleteMemory_Success(t *testing.T) {
	a, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()
	a.Config.Security.SafeMode = false

	doc := NewMemoryDoc("tool_use_del001", MemoryTypeToolUse, "to be deleted", "to be deleted", nil)
	doc.FountainBody = BuildFountainBody("2026-01-01 00:00:00", [][2]string{{"HARVEY", "to be deleted"}})
	if err := ms.Store.Save(doc, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	before, _ := ms.Store.Count()
	if before != 1 {
		t.Fatalf("expected 1 active memory before delete; got %d", before)
	}

	result, err := dispatch(t, reg, "delete_memory", map[string]any{"id": "tool_use_del001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Memory archived: tool_use_del001") {
		t.Errorf("expected 'Memory archived: tool_use_del001'; got: %q", result)
	}

	after, _ := ms.Store.Count()
	if after != 0 {
		t.Errorf("expected 0 active memories after delete; got %d", after)
	}
}

func TestDeleteMemory_SafeMode(t *testing.T) {
	// DefaultConfig has SafeMode=true.
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	doc := NewMemoryDoc("tool_use_delsafe01", MemoryTypeToolUse, "keep me", "keep me", nil)
	doc.FountainBody = BuildFountainBody("2026-01-01 00:00:00", [][2]string{{"HARVEY", "keep me"}})
	if err := ms.Store.Save(doc, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := dispatch(t, reg, "delete_memory", map[string]any{"id": "tool_use_delsafe01"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "safe mode") {
		t.Errorf("expected 'safe mode'; got: %q", result)
	}
	count, _ := ms.Store.Count()
	if count != 1 {
		t.Errorf("safe mode must not archive the memory; count = %d", count)
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

// ─── add_memory ──────────────────────────────────────────────────────────────

func TestAddMemory_NoMemorySystem(t *testing.T) {
	_, reg := newToolAgent(t, nil) // a.Memory is nil
	result, err := dispatch(t, reg, "add_memory", map[string]any{
		"content":     "always run go test before committing",
		"memory_type": "tool_use",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not available") {
		t.Errorf("expected 'not available' message; got: %q", result)
	}
}

func TestAddMemory_InvalidType(t *testing.T) {
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	result, err := dispatch(t, reg, "add_memory", map[string]any{
		"content":     "some content",
		"memory_type": "bogus_type",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "invalid memory_type") {
		t.Errorf("expected 'invalid memory_type' message; got: %q", result)
	}
}

func TestAddMemory_SaveAndList(t *testing.T) {
	a, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()
	a.Config.Security.SafeMode = false

	// Use reg.Dispatch directly to pass a proper JSON array for tags.
	result, err := reg.Dispatch(context.Background(), "add_memory",
		`{"content":"prefer uv over pip","memory_type":"user_preference","tags":["python","uv"]}`,
		1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Memory saved:") {
		t.Errorf("expected 'Memory saved:' in result; got: %q", result)
	}

	metas, err := ms.Store.List("user_preference")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) == 0 {
		t.Fatal("expected at least one memory in store after add_memory")
	}
	found := false
	for _, m := range metas {
		if strings.Contains(m.Description, "prefer uv over pip") {
			found = true
			break
		}
	}
	if !found {
		t.Error("saved memory should contain the content as description")
	}
}

func TestAddMemory_SafeMode(t *testing.T) {
	// DefaultConfig has SafeMode=true — no override needed.
	_, reg, ms := newToolAgentWithMemory(t)
	defer ms.Close()

	result, err := dispatch(t, reg, "add_memory", map[string]any{
		"content":     "some content",
		"memory_type": "tool_use",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "safe mode") {
		t.Errorf("expected 'safe mode' in result; got: %q", result)
	}
	metas, _ := ms.Store.List("tool_use")
	if len(metas) != 0 {
		t.Error("safe mode must not write to the store")
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
