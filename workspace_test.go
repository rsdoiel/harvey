package harvey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceNewWorkspace(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if ws.Root == "" {
		t.Fatal("Root is empty")
	}
	// harvey/ sub-directory must be created.
	if _, err := os.Stat(filepath.Join(ws.Root, "agents")); err != nil {
		t.Errorf("agents dir not created: %v", err)
	}
}

func TestWorkspaceAbsPath_valid(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	got, err := ws.AbsPath("src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(ws.Root, "src", "main.go")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestWorkspaceAbsPath_escape(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cases := []string{
		"../../etc/passwd",
		"../outside",
	}
	for _, rel := range cases {
		if _, err := ws.AbsPath(rel); err == nil {
			t.Errorf("AbsPath(%q): expected error for escaping path", rel)
		}
	}
}

func TestWorkspaceReadWriteFile(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	content := []byte("hello harvey\n")

	if err := ws.WriteFile("notes/hello.txt", content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ws.ReadFile("notes/hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q want %q", got, content)
	}
}

func TestWorkspaceReadFile_escape(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	if _, err := ws.ReadFile("../../etc/passwd"); err == nil {
		t.Error("expected error for escaping path")
	}
}

func TestWorkspaceListDir(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	ws.WriteFile("a.txt", []byte("a"), 0o644)
	ws.WriteFile("b.txt", []byte("b"), 0o644)

	entries, err := ws.ListDir(".")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["a.txt"] || !names["b.txt"] {
		t.Errorf("expected a.txt and b.txt in listing; got %v", names)
	}
}

func TestWorkspaceMkdirAll(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	if err := ws.MkdirAll("deep/nested/dir"); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info, err := os.Stat(filepath.Join(ws.Root, "deep", "nested", "dir"))
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}
