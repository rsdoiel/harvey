package harvey

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
