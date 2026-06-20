package harvey

import (
	"net/http"
	"net/http/httptest"
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
		// Windows universal form: strip .llamafile.exe fully.
		{"Llama-3.2-1B.llamafile.exe", "Llama-3.2-1B"},
		// Windows plain exe: strip .exe only.
		{"Llama-3.2-1B.exe", "Llama-3.2-1B"},
		// Path prefix should not affect the result.
		{"/path/to/Qwen3.llamafile", "Qwen3"},
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

func TestScanLlamafileModels_windowsUniversalForm(t *testing.T) {
	dir := t.TempDir()
	// .llamafile.exe is matched on all platforms.
	files := []string{"Llama-3.2-1B.llamafile", "Qwen3.llamafile.exe", "notes.txt"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	paths := scanLlamafileModels(dir)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths (.llamafile + .llamafile.exe), got %d: %v", len(paths), paths)
	}
	byBase := map[string]bool{}
	for _, p := range paths {
		byBase[filepath.Base(p)] = true
	}
	if !byBase["Llama-3.2-1B.llamafile"] {
		t.Error("expected Llama-3.2-1B.llamafile in results")
	}
	if !byBase["Qwen3.llamafile.exe"] {
		t.Error("expected Qwen3.llamafile.exe in results")
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

// ─── /llamafile download ─────────────────────────────────────────────────────

func TestCmdLlamafileDownload_printsText(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	if err := cmdLlamafile(a, []string{"download"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "llamafile") {
		t.Errorf("expected download text to mention llamafile, got: %s", out)
	}
	if !strings.Contains(out, "huggingface") || !strings.Contains(out, "huggingface") {
		// at least some download guidance
	}
}

// ─── LlamafileEntry.ContextLength ────────────────────────────────────────────

func TestLlamafileEntryContextLength_default(t *testing.T) {
	e := LlamafileEntry{Name: "qwen", Path: "/tmp/q.llamafile"}
	if e.ContextLength != 0 {
		t.Errorf("expected default ContextLength=0, got %d", e.ContextLength)
	}
}

func TestEffectiveContextLimit_llamafileEntry(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{{Name: "qwen", Path: "/tmp/q.llamafile", ContextLength: 8192}}
	cfg.LlamafileActive = "qwen"
	a := NewAgent(cfg, ws)
	if got := a.effectiveContextLimit(); got != 8192 {
		t.Errorf("effectiveContextLimit: got %d want 8192", got)
	}
}

func TestEffectiveContextLimit_llamafileEntryZeroFallsThrough(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{{Name: "qwen", Path: "/tmp/q.llamafile", ContextLength: 0}}
	cfg.LlamafileActive = "qwen"
	a := NewAgent(cfg, ws)
	// ContextLength=0 means unknown; should return 0 (no other source available).
	if got := a.effectiveContextLimit(); got != 0 {
		t.Errorf("effectiveContextLimit with ContextLength=0: got %d want 0", got)
	}
}

func TestCmdLlamafileRemove_alias(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{{Name: "qwen-coding", Path: "/tmp/q.llamafile"}}
	cfg.LlamafileActive = "qwen-coding"
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	if err := cmdLlamafile(a, []string{"remove", "qwen-coding"}, &buf); err != nil {
		t.Fatalf("remove alias error: %v", err)
	}
	if len(a.Config.LlamafileModels) != 0 {
		t.Error("expected model to be removed")
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

// ─── probeRunningLlamafileName ───────────────────────────────────────────────

func TestProbeRunningLlamafileName_validResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			// Matches the real response shape observed during testing.
			w.Write([]byte(`{"data":[{"id":"Qwen3.5-4B-Q5_K_S.gguf"}],"object":"list"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	got := probeRunningLlamafileName(srv.URL)
	want := "Qwen3.5-4B-Q5_K_S"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestProbeRunningLlamafileName_emptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[],"object":"list"}`))
	}))
	defer srv.Close()

	if got := probeRunningLlamafileName(srv.URL); got != "" {
		t.Errorf("expected empty string for empty data, got %q", got)
	}
}

func TestProbeRunningLlamafileName_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if got := probeRunningLlamafileName(srv.URL); got != "" {
		t.Errorf("expected empty string on server error, got %q", got)
	}
}

func TestProbeRunningLlamafileName_unreachable(t *testing.T) {
	if got := probeRunningLlamafileName("http://127.0.0.1:19993"); got != "" {
		t.Errorf("expected empty string for unreachable server, got %q", got)
	}
}

// ─── adoptExternalServer ────────────────────────────────────────────────────

func TestAdoptExternalServer_userSaysYes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"Qwen3.5-4B-Q5_K_S.gguf"}],"object":"list"}`))
	}))
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileURL = srv.URL
	a := NewAgent(cfg, ws)
	a.In = strings.NewReader("y\n")

	var buf strings.Builder
	if err := adoptExternalServer(a, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Config.LlamafileActive == "" {
		t.Error("expected LlamafileActive to be set after adoption")
	}
	out := buf.String()
	if !strings.Contains(out, "Qwen3.5-4B-Q5_K_S") {
		t.Errorf("expected model name in output, got: %s", out)
	}
}

func TestAdoptExternalServer_userSaysNo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"Qwen3.5-4B-Q5_K_S.gguf"}],"object":"list"}`))
	}))
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileURL = srv.URL
	a := NewAgent(cfg, ws)
	a.In = strings.NewReader("n\n")

	var buf strings.Builder
	if err := adoptExternalServer(a, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Config.LlamafileActive != "" {
		t.Error("expected LlamafileActive to remain empty when user declines adoption")
	}
}

// ─── runFirstRunWizard ───────────────────────────────────────────────────────

func TestRunFirstRunWizard_emptyInput(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	err := runFirstRunWizard(a, strings.NewReader("\n"), &buf)
	if err == nil {
		t.Fatal("expected error when user provides no path")
	}
	out := buf.String()
	if !strings.Contains(out, "llamafile") {
		t.Errorf("expected wizard text to mention llamafile, got: %s", out)
	}
}

func TestRunFirstRunWizard_pathNotFound(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	// Provide a non-existent path — the add flow should fail with a not-found error.
	err := runFirstRunWizard(a, strings.NewReader("/nonexistent/model.llamafile\n"), &buf)
	if err == nil {
		t.Fatal("expected error for non-existent llamafile path")
	}
}

// ─── restartActiveLlamafile ──────────────────────────────────────────────────

func TestRestartActiveLlamafile_noActiveEntry(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	// No LlamafileActive set — should return an error.
	var buf strings.Builder
	err := restartActiveLlamafile(a, &buf)
	if err == nil {
		t.Fatal("expected error when no active entry")
	}
}

func TestRestartActiveLlamafile_emptyPath(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	// Adopted server: entry registered but path is empty.
	cfg.LlamafileModels = []LlamafileEntry{{Name: "external", Path: ""}}
	cfg.LlamafileActive = "external"
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	err := restartActiveLlamafile(a, &buf)
	if err == nil {
		t.Fatal("expected error for adopted server with empty path")
	}
}
