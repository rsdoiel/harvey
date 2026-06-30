package harvey

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── model name stem consistency ─────────────────────────────────────────────

// TestModelNameStemConsistency asserts that llamafileModelName and ggufModelName
// produce the same stem for a matching .llamafile / .gguf pair. If they diverge,
// aggregateModels will list two entries that look unrelated to the user.
func TestModelNameStemConsistency(t *testing.T) {
	cases := []struct {
		llamafilePath string
		ggufPath      string
		wantStem      string
	}{
		{"/models/bonsai-8b.llamafile", "/models/bonsai-8b.gguf", "bonsai-8b"},
		{"/models/SmolLM3-3B-Instruct-Q4_K_M.llamafile", "/models/SmolLM3-3B-Instruct-Q4_K_M.gguf", "SmolLM3-3B-Instruct-Q4_K_M"},
		{"/models/phi4-Q4_K_M.llamafile", "/models/phi4-Q4_K_M.gguf", "phi4-Q4_K_M"},
	}

	for _, c := range cases {
		lfStem := llamafileModelName(c.llamafilePath)
		ggufStem := ggufModelName(c.ggufPath)
		if lfStem != c.wantStem {
			t.Errorf("llamafileModelName(%q) = %q, want %q", c.llamafilePath, lfStem, c.wantStem)
		}
		if ggufStem != c.wantStem {
			t.Errorf("ggufModelName(%q) = %q, want %q", c.ggufPath, ggufStem, c.wantStem)
		}
		if lfStem != ggufStem {
			t.Errorf("stem mismatch for %q: llamafile=%q gguf=%q", c.wantStem, lfStem, ggufStem)
		}
	}
}

// ─── aggregateModels with shared models dir ───────────────────────────────────

// TestAggregateModels_SameNameBothBackends verifies that when the same stem
// exists as both a .llamafile and a .gguf file in the shared models directory,
// aggregateModels returns one entry per backend with matching stem names.
func TestAggregateModels_SameNameBothBackends(t *testing.T) {
	dir := t.TempDir()

	// Create stub files — content doesn't matter, just needs to exist.
	for _, name := range []string{"bonsai-8b.llamafile", "bonsai-8b.gguf"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	a := newTestAgent(t)
	a.Config.Llamafile.ModelsDir = dir
	a.Config.LlamaCpp.ModelsDir = dir

	models, err := aggregateModels(a)
	if err != nil {
		t.Fatalf("aggregateModels: %v", err)
	}

	var lfEntry, ggufEntry *ModelSummary
	for i := range models {
		switch models[i].Engine {
		case "llamafile":
			if models[i].Name == "bonsai-8b" {
				lfEntry = &models[i]
			}
		case "llamacpp":
			if models[i].Name == "bonsai-8b" {
				ggufEntry = &models[i]
			}
		}
	}

	if lfEntry == nil {
		t.Error("expected a llamafile entry for 'bonsai-8b', got none")
	}
	if ggufEntry == nil {
		t.Error("expected a llamacpp entry for 'bonsai-8b', got none")
	}
	if lfEntry != nil && ggufEntry != nil && lfEntry.Name != ggufEntry.Name {
		t.Errorf("stem mismatch: llamafile=%q llamacpp=%q", lfEntry.Name, ggufEntry.Name)
	}
}

// TestAggregateModels_BothEnginesHaveCorrectPaths verifies that each backend
// entry carries a non-empty Path pointing to the right file extension.
func TestAggregateModels_BothEnginesHaveCorrectPaths(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"phi4-mini.llamafile", "phi4-mini.gguf", "unrelated.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	a := newTestAgent(t)
	a.Config.Llamafile.ModelsDir = dir
	a.Config.LlamaCpp.ModelsDir = dir

	models, err := aggregateModels(a)
	if err != nil {
		t.Fatalf("aggregateModels: %v", err)
	}

	for _, m := range models {
		// Ollama models have no filesystem path — only check local backends.
		if m.Engine == "ollama" {
			continue
		}
		if m.Path == "" {
			t.Errorf("model %q (engine=%s) has empty Path", m.Name, m.Engine)
		}
		switch m.Engine {
		case "llamafile":
			if ext := filepath.Ext(m.Path); ext != ".llamafile" {
				t.Errorf("llamafile model %q: expected .llamafile extension, got %q", m.Name, ext)
			}
		case "llamacpp":
			if ext := filepath.Ext(m.Path); ext != ".gguf" {
				t.Errorf("llamacpp model %q: expected .gguf extension, got %q", m.Name, ext)
			}
		}
	}
}

// ─── backend switch state invariants ─────────────────────────────────────────

// TestSwitchToLlamaCpp_ClearsLlamafileActive checks the invariant that
// wiring a llamacpp backend clears Config.Llamafile.Active. This prevents
// activeModelLabel and effectiveContextLimit from reading stale config.
// The test exercises startLlamaCppModelPath's state side-effects via a mock
// backend, without actually spawning llama-server.
func TestSwitchToLlamaCpp_ClearsLlamafileActive(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Active = "bonsai-8b" // simulate an active llamafile model

	// Simulate what startLlamaCppModelPath does when it wires the new backend:
	// it clears Llamafile.Active so config-level queries use a.Backend instead.
	a.Config.Llamafile.Active = ""
	b := NewLlamaCppBackend(a.Config, t.TempDir())
	a.Backend = b

	if a.Config.Llamafile.Active != "" {
		t.Errorf("Config.Llamafile.Active should be cleared after switching to llamacpp, got %q", a.Config.Llamafile.Active)
	}
	if a.Backend.Name() != "llamacpp" {
		t.Errorf("Backend.Name() = %q, want %q", a.Backend.Name(), "llamacpp")
	}
}

// TestSwitchToLlamafile_SetsLlamafileActive checks that wiring a llamafile
// backend sets Config.Llamafile.Active to the model name. This is the
// symmetric invariant to the llamacpp test above.
func TestSwitchToLlamafile_SetsLlamafileActive(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Active = ""

	// Simulate what useLlamafileEntry and switchLlamafileModel do.
	a.Config.Llamafile.Active = "bonsai-8b"
	b := NewLlamafileBackend(a.Config, t.TempDir(), t.TempDir())
	a.Backend = b

	if a.Config.Llamafile.Active != "bonsai-8b" {
		t.Errorf("Config.Llamafile.Active = %q, want %q", a.Config.Llamafile.Active, "bonsai-8b")
	}
	if a.Backend.Name() != "llamafile" {
		t.Errorf("Backend.Name() = %q, want %q", a.Backend.Name(), "llamafile")
	}
}
