package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ─── SaveLlamafileConfig — no longer persists Active ─────────────────────────

// TestSaveLlamafileConfig_DoesNotPersistActive asserts that SaveLlamafileConfig
// never writes the active: key to harvey.yaml. The blank-slate policy means
// Harvey no longer auto-starts the last used model at session start.
func TestSaveLlamafileConfig_DoesNotPersistActive(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Llamafile.Active = "bonsai-8b" // simulate a model that was used last session
	cfg.Llamafile.Models = []LlamafileEntry{
		{Name: "bonsai-8b", Path: "/models/bonsai-8b.llamafile"},
	}

	if err := SaveLlamafileConfig(ws, cfg); err != nil {
		t.Fatalf("SaveLlamafileConfig: %v", err)
	}

	yamlPath := filepath.Join(ws.Root, "agents", "harvey.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("reading harvey.yaml: %v", err)
	}

	// The active: key must not appear under llamafile: in the saved YAML.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing harvey.yaml: %v", err)
	}
	if lf, ok := raw["llamafile"].(map[string]interface{}); ok {
		if _, found := lf["active"]; found {
			t.Errorf("harvey.yaml should not contain llamafile.active after blank-slate change, but it does:\n%s", data)
		}
	}

	// The models list should still be saved.
	if !strings.Contains(string(data), "bonsai-8b") {
		t.Errorf("harvey.yaml should still contain the model registry entry, but got:\n%s", data)
	}
}

// ─── Llamafile GPULayers — safe CPU-only default ─────────────────────────────

// TestDefaultConfig_LlamafileGPULayersDefaultsToZero is the regression test
// for a 2+ hour hang observed on Raspberry Pi hardware: the old default of 99
// (-ngl 99, "maximise GPU") forces maximum GPU-offload on hardware that has no
// usable GPU-compute backend, which can cause severe slowdowns or an
// effective hang rather than a clean CPU fallback. 0 (CPU-only) is safe on
// every platform Harvey targets; users with a real GPU opt in via
// harvey.yaml's gpu_layers field. This matches LlamaCppConfig.GPULayers,
// which already defaults to 0 for the same reason.
func TestDefaultConfig_LlamafileGPULayersDefaultsToZero(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Llamafile.GPULayers != 0 {
		t.Errorf("DefaultConfig().Llamafile.GPULayers = %d, want 0 (CPU-only default)", cfg.Llamafile.GPULayers)
	}
}

// TestSaveLlamafileConfig_DoesNotPersistDefaultGPULayers asserts that leaving
// GPULayers at its default (0) does not write gpu_layers: to harvey.yaml,
// keeping the saved config minimal.
func TestSaveLlamafileConfig_DoesNotPersistDefaultGPULayers(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cfg := DefaultConfig() // GPULayers left at the default (0)

	if err := SaveLlamafileConfig(ws, cfg); err != nil {
		t.Fatalf("SaveLlamafileConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.Root, "agents", "harvey.yaml"))
	if err != nil {
		t.Fatalf("reading harvey.yaml: %v", err)
	}
	if strings.Contains(string(data), "gpu_layers") {
		t.Errorf("harvey.yaml should not contain gpu_layers when left at the default, but got:\n%s", data)
	}
}

// TestSaveLlamafileConfig_PersistsCustomGPULayers verifies that a
// user-customized GPULayers value (e.g. opting into GPU offload on capable
// hardware) is still persisted.
func TestSaveLlamafileConfig_PersistsCustomGPULayers(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Llamafile.GPULayers = 35

	if err := SaveLlamafileConfig(ws, cfg); err != nil {
		t.Fatalf("SaveLlamafileConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.Root, "agents", "harvey.yaml"))
	if err != nil {
		t.Fatalf("reading harvey.yaml: %v", err)
	}
	if !strings.Contains(string(data), "gpu_layers: 35") {
		t.Errorf("harvey.yaml should persist the customized gpu_layers: 35, but got:\n%s", data)
	}
}

// TestSaveLlamafileConfig_PreservesExistingActive verifies that if a prior
// harvey.yaml contains llamafile.active (written by an older Harvey), saving
// again does NOT re-add it. The old value is silently dropped.
func TestSaveLlamafileConfig_PreservesExistingActive(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}

	// Write a legacy harvey.yaml that includes active:.
	agentsDir := filepath.Join(ws.Root, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	legacyYAML := "llamafile:\n  active: legacy-model\n  models:\n    - name: legacy-model\n      path: /models/legacy.llamafile\n"
	yamlPath := filepath.Join(agentsDir, "harvey.yaml")
	if err := os.WriteFile(yamlPath, []byte(legacyYAML), 0644); err != nil {
		t.Fatalf("writing legacy yaml: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Llamafile.Active = "legacy-model"
	cfg.Llamafile.Models = []LlamafileEntry{
		{Name: "legacy-model", Path: "/models/legacy.llamafile"},
	}

	if err := SaveLlamafileConfig(ws, cfg); err != nil {
		t.Fatalf("SaveLlamafileConfig: %v", err)
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("reading harvey.yaml: %v", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing harvey.yaml: %v", err)
	}
	if lf, ok := raw["llamafile"].(map[string]interface{}); ok {
		if _, found := lf["active"]; found {
			t.Errorf("SaveLlamafileConfig should have removed legacy active: key, but it's still present:\n%s", data)
		}
	}
}

// ─── selectBackend — Case 1 removed ──────────────────────────────────────────

// TestActiveModelLabel_FallsBackToBoth verifies the invariant that
// activeModelLabel still works correctly when Config.Llamafile.Active is set
// (used for in-session status display), but that this field alone no longer
// drives auto-start at startup.
func TestActiveModelLabel_LlamafileActive(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Active = "bonsai-8b"

	got := activeModelLabel(a)
	if !strings.Contains(got, "bonsai-8b") || !strings.Contains(got, "llamafile") {
		t.Errorf("activeModelLabel = %q, want to contain 'bonsai-8b' and 'llamafile'", got)
	}
}
