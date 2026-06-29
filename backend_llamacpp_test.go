package harvey

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLlamaCppBackend_Name(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.Name() != "llamacpp" {
		t.Errorf("Name() = %q, want %q", b.Name(), "llamacpp")
	}
}

func TestLlamaCppBackend_BaseURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.URL = "http://127.0.0.1:8081"
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.BaseURL() != "http://127.0.0.1:8081" {
		t.Errorf("BaseURL() = %q, want %q", b.BaseURL(), "http://127.0.0.1:8081")
	}
}

func TestLlamaCppBackend_ActiveModel_Default(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.ActiveModel() != "" {
		t.Errorf("ActiveModel() before Start = %q, want empty", b.ActiveModel())
	}
}

func TestLlamaCppBackend_StartedByHarvey_Default(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.StartedByHarvey() {
		t.Error("StartedByHarvey() should be false on a new backend (no proc)")
	}
}

func TestLlamaCppBackend_IsRunning_DefaultFalse(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.IsRunning() {
		t.Error("IsRunning() should be false before Detect")
	}
}

func TestLlamaCppBackend_Detect_HealthyServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.LlamaCpp.URL = srv.URL
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if !b.Detect() {
		t.Error("Detect() should return true when /health returns 200")
	}
	if !b.IsRunning() {
		t.Error("IsRunning() should be true after successful Detect")
	}
}

func TestLlamaCppBackend_Detect_UnreachableServer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.URL = "http://127.0.0.1:19999"
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.Detect() {
		t.Error("Detect() should return false when server is unreachable")
	}
}

func TestLlamaCppBackend_ListModels_EmptyDir(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = t.TempDir()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	models, err := b.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("ListModels in empty dir: got %d models, want 0", len(models))
	}
}

func TestLlamaCppBackend_ListModels_WithFiles(t *testing.T) {
	dir := t.TempDir()

	files := []struct {
		name string
		size int
	}{
		{"phi4-Q4_K_M.gguf", 1024},
		{"qwen2.5-7b-Q5_K_S.gguf", 2048},
		{"notamodel.txt", 512}, // should be ignored
		{"notamodel.llamafile", 512}, // should be ignored
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, make([]byte, f.size), 0o644); err != nil {
			t.Fatalf("creating %s: %v", f.name, err)
		}
	}

	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = dir
	b := NewLlamaCppBackend(cfg, t.TempDir())
	models, err := b.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("ListModels: got %d models, want 2", len(models))
	}
	for _, m := range models {
		if m.Engine != "llamacpp" {
			t.Errorf("model %q: Engine = %q, want %q", m.Name, m.Engine, "llamacpp")
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

func TestLlamaCppBackend_NewClient_NoModel(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	_, err := b.NewClient()
	if err == nil {
		t.Error("NewClient() should return error when no active model is set")
	}
}

func TestLlamaCppBackend_NewClient_WithModel(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	b.activeModel = "phi4-Q4_K_M"
	client, err := b.NewClient()
	if err != nil {
		t.Fatalf("NewClient(): %v", err)
	}
	if client == nil {
		t.Error("NewClient() returned nil client")
	}
}

func TestLlamaCppBackend_Stop_NoProc(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if err := b.Stop(); err != nil {
		t.Errorf("Stop() on backend with no proc should be no-op, got: %v", err)
	}
}

func TestLlamaCppBackend_StartTimeout_Default(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.startTimeout != 120*time.Second {
		t.Errorf("startTimeout = %v, want 120s", b.startTimeout)
	}
}

func TestLlamaCppBackend_GPULayers_FromConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.GPULayers = 35
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.gpuLayers != 35 {
		t.Errorf("gpuLayers = %d, want 35", b.gpuLayers)
	}
}

func TestLlamaCppBackend_CtxSize_FromConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.CtxSize = 4096
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.ctxSize != 4096 {
		t.Errorf("ctxSize = %d, want 4096", b.ctxSize)
	}
}

func TestLlamaCppBackend_Threads_FromConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.Threads = 8
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.threads != 8 {
		t.Errorf("threads = %d, want 8", b.threads)
	}
}

func TestGGUFModelName(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/home/user/Models/phi4-Q4_K_M.gguf", "phi4-Q4_K_M"},
		{"phi4.gguf", "phi4"},
		{"/Models/qwen2.5-7b-instruct-q5_k_s.gguf", "qwen2.5-7b-instruct-q5_k_s"},
	}
	for _, c := range cases {
		got := ggufModelName(c.path)
		if got != c.want {
			t.Errorf("ggufModelName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestPortFromURL(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"http://127.0.0.1:8081", "8081"},
		{"http://localhost:9000", "9000"},
		{"http://127.0.0.1", ""},
	}
	for _, c := range cases {
		got := portFromURL(c.url)
		if got != c.want {
			t.Errorf("portFromURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// Regression: LlamaCppBackend.NewClient was calling newLlamafileLLMClient,
// so a.Client.Name() returned "llamafile (model)" in /status output.
func TestLlamaCppBackend_NewClient_ProviderIsLlamacpp(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	b.activeModel = "SmolLM3-3B-Instruct-Q4_K_M"
	client, err := b.NewClient()
	if err != nil {
		t.Fatalf("NewClient(): %v", err)
	}
	ac, ok := client.(*AnyLLMClient)
	if !ok {
		t.Fatal("client is not *AnyLLMClient")
	}
	if ac.ProviderName() != "llamacpp" {
		t.Errorf("ProviderName() = %q, want %q — /status will show wrong backend", ac.ProviderName(), "llamacpp")
	}
}

// Regression: ModelPath must reconstruct the full .gguf path from modelsDir
// so restartActiveLlamaCpp can restart llama-server without re-asking the user.
func TestLlamaCppBackend_ModelPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = "/Users/user/Models"
	b := NewLlamaCppBackend(cfg, t.TempDir())

	if got := b.ModelPath(); got != "" {
		t.Errorf("ModelPath() before Start = %q, want empty", got)
	}

	b.activeModel = "SmolLM3-3B-Instruct-Q4_K_M"
	want := "/Users/user/Models/SmolLM3-3B-Instruct-Q4_K_M.gguf"
	if got := b.ModelPath(); got != want {
		t.Errorf("ModelPath() = %q, want %q", got, want)
	}
}

// Regression: restartActiveLlamaCpp must return an error rather than panic
// when the backend is nil or a different type.
func TestRestartActiveLlamaCpp_WrongBackend(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)
	a.Backend = NewLlamafileBackend(cfg, t.TempDir(), ws.Root) // not a llamacpp backend

	err := restartActiveLlamaCpp(a, io.Discard)
	if err == nil {
		t.Error("expected error when backend is not *LlamaCppBackend")
	}
}

func TestRestartActiveLlamaCpp_NoActiveModel(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)
	b := NewLlamaCppBackend(cfg, t.TempDir())
	// activeModel is empty — ModelPath() returns ""
	a.Backend = b

	err := restartActiveLlamaCpp(a, io.Discard)
	if err == nil {
		t.Error("expected error when no active model path is known")
	}
}

func TestScanGGUFModels(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.gguf", "b.gguf", "c.llamafile", "d.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths := scanGGUFModels(dir)
	if len(paths) != 2 {
		t.Errorf("scanGGUFModels: got %d, want 2", len(paths))
	}
}
