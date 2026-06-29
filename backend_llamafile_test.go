package harvey

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLlamafileBackend_Name(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.Name() != "llamafile" {
		t.Errorf("Name() = %q, want %q", b.Name(), "llamafile")
	}
}

func TestLlamafileBackend_BaseURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Llamafile.URL = "http://127.0.0.1:8080"
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.BaseURL() != "http://127.0.0.1:8080" {
		t.Errorf("BaseURL() = %q, want %q", b.BaseURL(), "http://127.0.0.1:8080")
	}
}

func TestLlamafileBackend_ActiveModel_Default(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.ActiveModel() != "" {
		t.Errorf("ActiveModel() before start = %q, want %q", b.ActiveModel(), "")
	}
}

func TestLlamafileBackend_StartedByHarvey_Default(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.StartedByHarvey() {
		t.Error("StartedByHarvey() should be false on a new backend (no proc)")
	}
}

func TestLlamafileBackend_IsRunning_DefaultFalse(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.IsRunning() {
		t.Error("IsRunning() should be false before Detect")
	}
}

func TestLlamafileBackend_Detect_ReachableServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Llamafile.URL = srv.URL
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if !b.Detect() {
		t.Error("Detect() should return true when /v1/models returns 200")
	}
	if !b.IsRunning() {
		t.Error("IsRunning() should be true after successful Detect")
	}
}

func TestLlamafileBackend_Detect_UnreachableServer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Llamafile.URL = "http://127.0.0.1:19998"
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.Detect() {
		t.Error("Detect() should return false when server is unreachable")
	}
}

func TestLlamafileBackend_ListModels_EmptyDir(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Llamafile.ModelsDir = t.TempDir() // empty dir
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	models, err := b.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("ListModels in empty dir: got %d models, want 0", len(models))
	}
}

func TestLlamafileBackend_ListModels_WithFiles(t *testing.T) {
	dir := t.TempDir()

	// Create fake .llamafile files.
	files := []struct {
		name string
		size int
	}{
		{"phi4-Q4_K_M.llamafile", 1024},
		{"qwen2.5-7b.llamafile", 2048},
		{"notamodel.txt", 512}, // should be ignored
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, make([]byte, f.size), 0o755); err != nil {
			t.Fatalf("creating %s: %v", f.name, err)
		}
	}

	cfg := DefaultConfig()
	cfg.Llamafile.ModelsDir = dir
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	models, err := b.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("ListModels: got %d models, want 2", len(models))
	}
	for _, m := range models {
		if m.Engine != "llamafile" {
			t.Errorf("model %q: Engine = %q, want %q", m.Name, m.Engine, "llamafile")
		}
		if m.Path == "" {
			t.Errorf("model %q: Path should not be empty", m.Name)
		}
		if m.SizeBytes == 0 {
			t.Errorf("model %q: SizeBytes should not be zero", m.Name)
		}
		if m.Modified.IsZero() {
			t.Errorf("model %q: Modified should not be zero", m.Name)
		}
	}
}

func TestLlamafileBackend_NewClient_NoModel(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	_, err := b.NewClient()
	if err == nil {
		t.Error("NewClient() should return error when no active model is set")
	}
}

func TestLlamafileBackend_NewClient_WithModel(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	b.activeModel = "phi4-Q4_K_M"
	client, err := b.NewClient()
	if err != nil {
		t.Fatalf("NewClient(): %v", err)
	}
	if client == nil {
		t.Error("NewClient() returned nil client")
	}
}

func TestLlamafileBackend_Stop_NoProc(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if err := b.Stop(); err != nil {
		t.Errorf("Stop() on backend with no proc should be no-op, got: %v", err)
	}
}

func TestLlamafileBackend_StartupTimeout_Default(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.startupTimeout != 120*time.Second {
		t.Errorf("startupTimeout = %v, want 120s", b.startupTimeout)
	}
}

func TestLlamafileBackend_GPULayers_FromConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Llamafile.GPULayers = 35
	b := NewLlamafileBackend(cfg, t.TempDir(), t.TempDir())
	if b.gpuLayers != 35 {
		t.Errorf("gpuLayers = %d, want 35", b.gpuLayers)
	}
}
