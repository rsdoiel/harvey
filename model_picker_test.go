package harvey

import (
	"io"
	"strings"
	"testing"
)

// ─── promptLazyRegister ───────────────────────────────────────────────────────

func TestPromptLazyRegister_SkipsWhenAliasExistsForSameEngine(t *testing.T) {
	// When an alias already points to the same model name AND engine,
	// promptLazyRegister must return the alias name without prompting.
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"bonsai": {Model: "Bonsai-8B", Engine: "llamafile"},
	}
	a.In = strings.NewReader("") // no input expected — prompt must not appear

	item := ModelSummary{Name: "Bonsai-8B", Engine: "llamafile", Path: "/models/Bonsai-8B.llamafile"}
	alias, err := promptLazyRegister(a, item, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alias != "bonsai" {
		t.Errorf("expected existing alias %q, got %q", "bonsai", alias)
	}
}

func TestPromptLazyRegister_PromptsWhenSameNameDifferentEngine(t *testing.T) {
	// An alias exists for the same model name but under a different engine (Ollama).
	// Picking the llamafile version must prompt for a new alias, not reuse the Ollama one.
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"bonsai": {Model: "Bonsai-8B", Engine: "ollama"},
	}
	// User presses Enter to skip the alias prompt.
	a.In = strings.NewReader("\n\n")

	item := ModelSummary{Name: "Bonsai-8B", Engine: "llamafile", Path: "/models/Bonsai-8B.llamafile"}
	var out strings.Builder
	alias, err := promptLazyRegister(a, item, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty alias: user pressed Enter to skip.
	if alias != "" {
		t.Errorf("expected empty alias when user skips, got %q", alias)
	}
	// The prompt must have appeared (engine mismatch should not reuse Ollama alias).
	if !strings.Contains(out.String(), "Save alias") {
		t.Errorf("expected alias prompt to appear for cross-engine same name, got: %q", out.String())
	}
}

func TestPromptLazyRegister_SetsEngineOnNewAlias(t *testing.T) {
	// When a new alias is saved via the picker, Engine must be stored
	// so future same-name cross-engine models don't collide.
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{}
	// User types "bonsai-file" as the alias name, no tags.
	a.In = strings.NewReader("bonsai-file\n\n")

	item := ModelSummary{Name: "Bonsai-8B", Engine: "llamafile", Path: "/models/Bonsai-8B.llamafile"}
	alias, err := promptLazyRegister(a, item, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alias != "bonsai-file" {
		t.Errorf("expected alias %q, got %q", "bonsai-file", alias)
	}
	saved, ok := a.Config.ModelAliases["bonsai-file"]
	if !ok {
		t.Fatal("alias not saved to ModelAliases")
	}
	if saved.Engine != "llamafile" {
		t.Errorf("Engine not saved: got %q, want %q", saved.Engine, "llamafile")
	}
}

func TestPromptLazyRegister_LegacyAliasNoEngineMatchesAnyEngine(t *testing.T) {
	// Backward compat: an alias with Engine=="" (hand-written or from old harvey.yaml)
	// must match regardless of the item's engine so existing aliases keep working.
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"bonsai": {Model: "Bonsai-8B", Engine: ""}, // legacy: no engine stored
	}
	a.In = strings.NewReader("") // no prompt expected

	item := ModelSummary{Name: "Bonsai-8B", Engine: "llamafile"}
	alias, err := promptLazyRegister(a, item, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alias != "bonsai" {
		t.Errorf("legacy alias should match any engine, got alias=%q", alias)
	}
}
