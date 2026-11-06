package harvey

import (
	"strings"
	"testing"
)

// TestCmdStatus_MemoryDisabled verifies that when memory is disabled the
// Profile line is not printed at all.
func TestCmdStatus_MemoryDisabled(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Memory.Enabled = false
	a := &Agent{Config: cfg, Workspace: ws, commands: make(map[string]*Command)}
	a.registerCommands()

	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatalf("cmdStatus: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Profile:") {
		t.Errorf("Profile line should not appear when memory is disabled; got:\n%s", got)
	}
}

// ─── /status token estimate ─────────────────────────────────────────────────

func TestCmdStatus_ollamaShowsTokenEstimate(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Ollama.Model = "qwen-coding"
	cfg.Ollama.ContextLength = 32768
	a := &Agent{
		Config:   cfg,
		Workspace: ws,
		commands: make(map[string]*Command),
	}
	a.registerCommands()
	a.Client = newOllamaLLMClient("http://localhost:11434", "qwen-coding", 0)
	a.AddMessage("user", strings.Repeat("x", 400)) // ~100 tokens

	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatalf("cmdStatus: %v", err)
	}
	got := out.String()
	// Should show a token estimate (with ~ prefix).
	if !strings.Contains(got, "Tokens:") {
		t.Errorf("expected Tokens line for the Ollama backend, got:\n%s", got)
	}
	if !strings.Contains(got, "~") {
		t.Errorf("expected ~ estimate qualifier, got:\n%s", got)
	}
}
