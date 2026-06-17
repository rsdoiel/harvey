package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLlamafileModelName(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/models/Llama-3.2-3B-Instruct.Q4_K_M.llamafile", "Llama-3.2-3B-Instruct.Q4_K_M"},
		{"gemma-3.llamafile", "gemma-3"},
		{"/home/user/Models/phi-4.llamafile", "phi-4"},
		{"no-suffix", "no-suffix"},
	}
	for _, c := range cases {
		got := llamafileModelName(c.path)
		if got != c.want {
			t.Errorf("llamafileModelName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestScanLlamafileModels_empty(t *testing.T) {
	if paths := scanLlamafileModels("/nonexistent/dir/that/does/not/exist"); len(paths) != 0 {
		t.Fatalf("expected nil/empty for missing dir, got %v", paths)
	}
}

func TestScanLlamafileModels_findsFiles(t *testing.T) {
	dir := t.TempDir()
	// Create two llamafile binaries and one non-llamafile file.
	for _, name := range []string{"alpha.llamafile", "beta.llamafile", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	paths := scanLlamafileModels(dir)
	if len(paths) != 2 {
		t.Fatalf("expected 2 .llamafile paths, got %d: %v", len(paths), paths)
	}
	for _, p := range paths {
		if filepath.Ext(p) != ".llamafile" {
			t.Errorf("unexpected file in results: %s", p)
		}
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		input string
		want  string
	}{
		{"~/Models", filepath.Join(home, "Models")},
		{"~", home},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, c := range cases {
		got := expandTilde(c.input)
		if got != c.want {
			t.Errorf("expandTilde(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestConfigActiveEntry_none(t *testing.T) {
	cfg := DefaultConfig()
	if e := cfg.ActiveLlamafileEntry(); e != nil {
		t.Fatalf("expected nil when LlamafileActive is empty, got %+v", e)
	}
}

func TestConfigActiveEntry_found(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{{Name: "qwen", Path: "/tmp/qwen.llamafile"}}
	cfg.LlamafileActive = "qwen"
	e := cfg.ActiveLlamafileEntry()
	if e == nil {
		t.Fatal("expected non-nil entry")
	}
	if e.Path != "/tmp/qwen.llamafile" {
		t.Errorf("unexpected path: %s", e.Path)
	}
}

func TestConfigAddOrUpdateEntry_insert(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: "alpha", Path: "/tmp/alpha.llamafile"})
	if len(cfg.LlamafileModels) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(cfg.LlamafileModels))
	}
}

func TestConfigAddOrUpdateEntry_update(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: "alpha", Path: "/tmp/alpha.llamafile"})
	cfg.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: "alpha", Path: "/tmp/alpha-v2.llamafile"})
	if len(cfg.LlamafileModels) != 1 {
		t.Fatalf("expected 1 entry after update, got %d", len(cfg.LlamafileModels))
	}
	if cfg.LlamafileModels[0].Path != "/tmp/alpha-v2.llamafile" {
		t.Errorf("path not updated: %s", cfg.LlamafileModels[0].Path)
	}
}

func TestCmdLlamafileList_empty(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	if err := cmdLlamafileList(a, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "No llamafile models registered") {
		t.Errorf("expected empty-list message, got: %s", out)
	}
}

func TestCmdLlamafileUse_notRegistered(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	err := cmdLlamafileUse(a, []string{"nonexistent"}, &buf)
	if err == nil {
		t.Fatal("expected error for unregistered model name")
	}
}
