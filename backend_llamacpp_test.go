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
		{"notamodel.txt", 512},       // should be ignored
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

func TestLlamaCppBackend_PinCPU_Default(t *testing.T) {
	cfg := DefaultConfig()
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if b.pinCPU {
		t.Error("pinCPU should default to false")
	}
}

func TestLlamaCppBackend_PinCPU_FromConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.PinCPU = true
	b := NewLlamaCppBackend(cfg, t.TempDir())
	if !b.pinCPU {
		t.Error("pinCPU = false, want true")
	}
}

// ─── launch plan: BLAS/OpenMP thread isolation + CPU pinning ─────────────────

func TestBuildLaunchPlan_EnvIsolation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.Threads = 3
	b := NewLlamaCppBackend(cfg, t.TempDir())

	plan := b.buildLaunchPlan("/models/m.gguf", "8081", "/usr/local/bin/llama-server",
		[]string{"FOO=bar"}, tasksetNotFound)

	wantEnv := map[string]bool{
		"FOO=bar":                false,
		"BLIS_NUM_THREADS=1":     false,
		"OPENBLAS_NUM_THREADS=1": false,
		"OMP_NUM_THREADS=1":      false,
	}
	for _, e := range plan.env {
		if _, ok := wantEnv[e]; ok {
			wantEnv[e] = true
		}
	}
	for k, seen := range wantEnv {
		if !seen {
			t.Errorf("env missing %q; got %v", k, plan.env)
		}
	}
}

func TestBuildLaunchPlan_NoPinning_WhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.Threads = 3
	cfg.LlamaCpp.PinCPU = false
	b := NewLlamaCppBackend(cfg, t.TempDir())

	plan := b.buildLaunchPlan("/models/m.gguf", "8081", "/usr/local/bin/llama-server",
		nil, tasksetFound)

	if plan.bin != "/usr/local/bin/llama-server" {
		t.Errorf("bin = %q, want unpinned llama-server path", plan.bin)
	}
	wantArgs := []string{"--model", "/models/m.gguf", "--port", "8081", "--threads", "3"}
	if !equalStrings(plan.args, wantArgs) {
		t.Errorf("args = %v, want %v", plan.args, wantArgs)
	}
}

func TestBuildLaunchPlan_Pinning_TasksetFound(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.Threads = 3
	cfg.LlamaCpp.PinCPU = true
	b := NewLlamaCppBackend(cfg, t.TempDir())

	plan := b.buildLaunchPlan("/models/m.gguf", "8081", "/usr/local/bin/llama-server",
		nil, tasksetFound)

	if plan.bin != "/usr/bin/taskset" {
		t.Errorf("bin = %q, want /usr/bin/taskset", plan.bin)
	}
	wantArgs := []string{"-c", "0-2", "/usr/local/bin/llama-server", "--model", "/models/m.gguf", "--port", "8081", "--threads", "3"}
	if !equalStrings(plan.args, wantArgs) {
		t.Errorf("args = %v, want %v", plan.args, wantArgs)
	}
}

func TestBuildLaunchPlan_Pinning_TasksetNotFound_FallsBackUnpinned(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.Threads = 3
	cfg.LlamaCpp.PinCPU = true
	b := NewLlamaCppBackend(cfg, t.TempDir())

	plan := b.buildLaunchPlan("/models/m.gguf", "8081", "/usr/local/bin/llama-server",
		nil, tasksetNotFound)

	if plan.bin != "/usr/local/bin/llama-server" {
		t.Errorf("bin = %q, want unpinned llama-server path when taskset is unavailable", plan.bin)
	}
}

func TestBuildLaunchPlan_Pinning_NoThreads_IsNoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LlamaCpp.PinCPU = true // Threads left at 0 — no basis for a core range
	b := NewLlamaCppBackend(cfg, t.TempDir())

	plan := b.buildLaunchPlan("/models/m.gguf", "8081", "/usr/local/bin/llama-server",
		nil, tasksetFound)

	if plan.bin != "/usr/local/bin/llama-server" {
		t.Errorf("bin = %q, want unpinned llama-server path when Threads is 0", plan.bin)
	}
}

func tasksetFound() (string, error)    { return "/usr/bin/taskset", nil }
func tasksetNotFound() (string, error) { return "", os.ErrNotExist }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

// ─── capability probe wiring ──────────────────────────────────────────────────

// TestLlamaCppProbeAndCache_WritesCapabilityOnToolModel verifies that after a
// llama.cpp model is started, the ModelCache contains a capability entry derived
// from the server's /props response. Uses a mock server so no real llama-server
// binary is required.
func TestLlamaCppProbeAndCache_WritesCapabilityOnToolModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/props":
			// Qwen-style template — tool calls supported.
			w.Write([]byte(`{"chat_template":"...{% for tool in tools %}<tool_call>{% endfor %}..."}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cache, err := OpenModelCache(ws, "")
	if err != nil {
		t.Fatalf("OpenModelCache: %v", err)
	}
	defer cache.Close()

	a := newTestAgent(t)
	a.Workspace = ws
	a.ModelCache = cache

	probeLlamaCppAndCache(a, "phi4-Q4_K_M", srv.URL)

	cap, err := cache.Get("phi4-Q4_K_M")
	if err != nil || cap == nil {
		t.Fatal("expected capability entry in ModelCache after probe, got nil")
	}
	if cap.SupportsTools != CapYes {
		t.Errorf("expected SupportsTools=CapYes, got %v", cap.SupportsTools)
	}
	if cap.ToolMode != ToolModeStructured {
		t.Errorf("expected ToolMode=ToolModeStructured, got %q", cap.ToolMode)
	}
	if cap.ProbeLevel != "fast" {
		t.Errorf("expected ProbeLevel=fast, got %q", cap.ProbeLevel)
	}
}

// TestLlamaCppProbeAndCache_SkipsIfAlreadyProbed verifies that an existing
// cache entry with ProbeLevel != "none" is not overwritten.
func TestLlamaCppProbeAndCache_SkipsIfAlreadyProbed(t *testing.T) {
	probeCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			probeCalled = true
			w.Write([]byte(`{"chat_template":"<tool_call>"}`))
		}
	}))
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cache, err := OpenModelCache(ws, "")
	if err != nil {
		t.Fatalf("OpenModelCache: %v", err)
	}
	defer cache.Close()

	// Pre-seed the cache with a thorough probe so the fast probe should be skipped.
	_ = cache.Set(&ModelCapability{
		Name:          "phi4-Q4_K_M",
		SupportsTools: CapNo,
		ProbeLevel:    "thorough",
		ProbedAt:      time.Now(),
	})

	a := newTestAgent(t)
	a.Workspace = ws
	a.ModelCache = cache

	probeLlamaCppAndCache(a, "phi4-Q4_K_M", srv.URL)

	if probeCalled {
		t.Error("expected probe to be skipped for already-probed model, but /props was called")
	}
	cap, _ := cache.Get("phi4-Q4_K_M")
	if cap.SupportsTools != CapNo {
		t.Error("existing cache entry should not have been overwritten")
	}
}

// TestLlamaCppProbeAndCache_NoCache_IsNoop verifies that calling
// probeLlamaCppAndCache with a nil ModelCache does not panic.
func TestLlamaCppProbeAndCache_NoCache_IsNoop(t *testing.T) {
	a := newTestAgent(t)
	a.ModelCache = nil
	// Must not panic.
	probeLlamaCppAndCache(a, "some-model", "http://127.0.0.1:1")
}
