package harvey

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── /model list — all backends ───────────────────────────────────────────────

// TestCmdModelList_ShowsLlamafileEntries verifies that /model list prints
// registered llamafile models.
func TestCmdModelList_ShowsLlamafileEntries(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Models = []LlamafileEntry{
		{Name: "bonsai-8b", Path: "/models/bonsai-8b.llamafile"},
	}

	var out strings.Builder
	if err := cmdModelList(a, &out); err != nil {
		t.Fatalf("cmdModelList: %v", err)
	}
	if !strings.Contains(out.String(), "bonsai-8b") {
		t.Errorf("expected 'bonsai-8b' in output, got:\n%s", out.String())
	}
}

// TestCmdModelList_ShowsGGUFEntries verifies that /model list prints .gguf
// models found in the LlamaCpp models directory.
func TestCmdModelList_ShowsGGUFEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "smollm3.gguf"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.LlamaCpp.ModelsDir = dir

	var out strings.Builder
	if err := cmdModelList(a, &out); err != nil {
		t.Fatalf("cmdModelList: %v", err)
	}
	if !strings.Contains(out.String(), "smollm3") {
		t.Errorf("expected 'smollm3' in output, got:\n%s", out.String())
	}
}

// TestCmdModelList_ShowsEngineLabels verifies that each entry is labelled with
// its backend engine so the user can distinguish llamafile from llamacpp.
func TestCmdModelList_ShowsEngineLabels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "phi4.gguf"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Config.LlamaCpp.ModelsDir = dir
	a.Config.Llamafile.Models = []LlamafileEntry{
		{Name: "phi4", Path: "/models/phi4.llamafile"},
	}

	var out strings.Builder
	if err := cmdModelList(a, &out); err != nil {
		t.Fatalf("cmdModelList: %v", err)
	}
	if !strings.Contains(out.String(), "llamafile") {
		t.Errorf("expected 'llamafile' label in output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "llamacpp") {
		t.Errorf("expected 'llamacpp' label in output, got:\n%s", out.String())
	}
}

// ─── /model clean — all backends ─────────────────────────────────────────────

// TestCmdModelClean_RemovesStaleOllamaAlias verifies that /model clean removes
// an alias whose Engine is "ollama" and whose model is not in the live set.
func TestCmdModelClean_RemovesStaleOllamaAlias(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"gone":  {Model: "removed-model:8b", Engine: "ollama"},
		"alive": {Model: "installed:8b", Engine: "ollama"},
	}

	liveOllama := []string{"installed:8b"}
	n, err := pruneStaleModelRefs(a, liveOllama, nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("pruneStaleModelRefs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 removal, got %d", n)
	}
	if _, ok := a.Config.ModelAliases["gone"]; ok {
		t.Error("alias 'gone' should have been removed")
	}
	if _, ok := a.Config.ModelAliases["alive"]; !ok {
		t.Error("alias 'alive' should still exist")
	}
}

// TestCmdModelClean_RemovesStaleLlamafileAlias verifies that /model clean also
// prunes aliases pointing to llamafile models that are no longer on disk.
func TestCmdModelClean_RemovesStaleLlamafileAlias(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"missing": {Model: "gone-model", Engine: "llamafile"},
		"present": {Model: "bonsai-8b", Engine: "llamafile"},
	}

	liveLlamafile := []string{"bonsai-8b"}
	n, err := pruneStaleModelRefs(a, nil, liveLlamafile, nil, io.Discard)
	if err != nil {
		t.Fatalf("pruneStaleModelRefs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 removal, got %d", n)
	}
	if _, ok := a.Config.ModelAliases["missing"]; ok {
		t.Error("alias 'missing' should have been removed")
	}
}

// TestCmdModelClean_PreservesLegacyAliasWithNoEngine verifies that aliases
// with Engine=="" (legacy hand-written entries) are never pruned — we cannot
// determine which backend they belong to without engine info.
func TestCmdModelClean_PreservesLegacyAliasWithNoEngine(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"legacy": {Model: "some-model", Engine: ""}, // no engine — unknown provenance
	}

	n, err := pruneStaleModelRefs(a, nil, nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("pruneStaleModelRefs: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 removals for legacy alias, got %d", n)
	}
	if _, ok := a.Config.ModelAliases["legacy"]; !ok {
		t.Error("legacy alias should be preserved")
	}
}

// ─── /ollama command removed ──────────────────────────────────────────────────

// TestOllamaCommandRemoved verifies that the /ollama command is no longer
// registered in the agent's command table after the boundary redesign.
func TestOllamaCommandRemoved(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	if _, ok := a.commands["ollama"]; ok {
		t.Error("/ollama command should have been removed from Harvey; delegate to 'ollama' CLI instead")
	}
}

// ─── /model download removed ──────────────────────────────────────────────────

// TestModelDownloadSubcommandAbsent verifies that "download" is not listed as
// a subcommand of /model. It was a help-text-only stub with no implementation;
// users should follow the URL in /help llamafile or visit HuggingFace directly.
func TestModelDownloadSubcommandAbsent(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	cmd, ok := a.commands["model"]
	if !ok {
		t.Fatal("/model command not registered")
	}
	for _, sub := range cmd.Subcommands {
		if sub == "download" {
			t.Error("/model download should have been removed — it was never implemented; direct users to HuggingFace instead")
		}
	}
}

// TestModelHelpTextNoDownload verifies that ModelHelpText no longer mentions
// the /model download subcommand.
func TestModelHelpTextNoDownload(t *testing.T) {
	if strings.Contains(ModelHelpText, "/model download") {
		t.Error("ModelHelpText still references /model download which has been removed")
	}
}

// ─── /model drop removed; /llamafile already removed ─────────────────────────

// TestModelDropSubcommandAbsent verifies that "drop" is not a subcommand of
// /model. Llamafile model files are managed on disk (rm ~/Models/foo.llamafile);
// there is no Harvey-level registry to clean up.
func TestModelDropSubcommandAbsent(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	cmd, ok := a.commands["model"]
	if !ok {
		t.Fatal("/model command not registered")
	}
	for _, sub := range cmd.Subcommands {
		if sub == "drop" {
			t.Error("/model drop should have been removed — model files are managed on disk or via the ollama CLI")
		}
	}
}

// TestLlamafileCommandAbsent verifies the /llamafile command is not registered.
// Its subcommands (add, list, start, stop, status) are now part of /model.
func TestLlamafileCommandAbsent(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	if _, ok := a.commands["llamafile"]; ok {
		t.Error("/llamafile command should not be registered; use /model for all model management")
	}
}

// TestModelHelpTextNoDropOrLlamafileRedirect verifies that ModelHelpText no
// longer references the removed /model drop subcommand.
func TestModelHelpTextNoDropOrLlamafileRedirect(t *testing.T) {
	if strings.Contains(ModelHelpText, "/model drop") {
		t.Error("ModelHelpText still references /model drop which has been removed")
	}
}
