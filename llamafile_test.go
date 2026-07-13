package harvey

import (
	"fmt"
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

// TestResolveSwitchPath_DiskPathWinsOverStaleRegistry reproduces the bug
// where picking "gemma" from a fresh disk-scan list loaded Apertus instead:
// a stale registry entry sharing the requested name must never override the
// exact path the caller just resolved from a live disk scan.
func TestResolveSwitchPath_DiskPathWinsOverStaleRegistry(t *testing.T) {
	entry := &LlamafileEntry{Name: "gemma", Path: "/home/user/Models/Apertus-8B-Instruct-2509.llamafile"}
	got, err := resolveSwitchPath("gemma", entry, "/home/user/Models/gemma-4-E4B-it-Q5_K_M.llamafile", "")
	if err != nil {
		t.Fatalf("resolveSwitchPath: unexpected error: %v", err)
	}
	want := "/home/user/Models/gemma-4-E4B-it-Q5_K_M.llamafile"
	if got != want {
		t.Errorf("resolveSwitchPath = %q, want %q (disk path must win over stale registry entry)", got, want)
	}
}

// TestResolveSwitchPath_FallsBackToRegistryWhenNoDiskPath verifies typed
// "/model use NAME" (no picker, so no diskPath) still resolves via the
// registry, with workspace-relative paths joined against workspaceRoot.
func TestResolveSwitchPath_FallsBackToRegistryWhenNoDiskPath(t *testing.T) {
	entry := &LlamafileEntry{Name: "gemma", Path: "models/gemma.llamafile"}
	got, err := resolveSwitchPath("gemma", entry, "", "/home/user/ws")
	if err != nil {
		t.Fatalf("resolveSwitchPath: unexpected error: %v", err)
	}
	want := "/home/user/ws/models/gemma.llamafile"
	if got != want {
		t.Errorf("resolveSwitchPath = %q, want %q", got, want)
	}
}

// TestResolveSwitchPath_ErrorsWhenNeitherAvailable verifies the original
// "no llamafile registered" error is preserved when there is no registry
// entry and no disk path to fall back to.
func TestResolveSwitchPath_ErrorsWhenNeitherAvailable(t *testing.T) {
	_, err := resolveSwitchPath("missing", nil, "", "")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected error to mention the model name, got: %v", err)
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
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "qwen", Path: "/tmp/qwen.llamafile"}}
	cfg.Llamafile.Active = "qwen"
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
	if len(cfg.Llamafile.Models) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(cfg.Llamafile.Models))
	}
}

func TestConfigAddOrUpdateEntry_update(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: "alpha", Path: "/tmp/alpha.llamafile"})
	cfg.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: "alpha", Path: "/tmp/alpha-v2.llamafile"})
	if len(cfg.Llamafile.Models) != 1 {
		t.Fatalf("expected 1 entry after update, got %d", len(cfg.Llamafile.Models))
	}
	if cfg.Llamafile.Models[0].Path != "/tmp/alpha-v2.llamafile" {
		t.Errorf("path not updated: %s", cfg.Llamafile.Models[0].Path)
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
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "qwen", Path: "/tmp/q.llamafile", ContextLength: 8192}}
	cfg.Llamafile.Active = "qwen"
	a := NewAgent(cfg, ws)
	if got := a.effectiveContextLimit(); got != 8192 {
		t.Errorf("effectiveContextLimit: got %d want 8192", got)
	}
}

func TestEffectiveContextLimit_llamafileEntryZeroFallsThrough(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "qwen", Path: "/tmp/q.llamafile", ContextLength: 0}}
	cfg.Llamafile.Active = "qwen"
	a := NewAgent(cfg, ws)
	// ContextLength=0 means unknown; should return 0 (no other source available).
	if got := a.effectiveContextLimit(); got != 0 {
		t.Errorf("effectiveContextLimit with ContextLength=0: got %d want 0", got)
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
	cfg.Llamafile.URL = srv.URL
	a := NewAgent(cfg, ws)
	a.In = strings.NewReader("y\n")

	var buf strings.Builder
	if err := adoptExternalServer(a, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Config.Llamafile.Active == "" {
		t.Error("expected LlamafileActive to be set after adoption")
	}
	out := buf.String()
	if !strings.Contains(out, "Qwen3.5-4B-Q5_K_S") {
		t.Errorf("expected model name in output, got: %s", out)
	}
}

// TestAdoptExternalServer_probesContextLength is the regression test for the
// Gemma4-E4B chunking bug: adoptExternalServer registered the adopted model
// with ContextLength left at its zero value, unlike switchLlamafileModel and
// addAndStartLlamafile which both call ProbeLlamafileContextLength. A zero
// ContextLength makes effectiveContextLimit() return 0 for the rest of the
// session, which in turn disables the read_file chunking guard entirely.
func TestAdoptExternalServer_probesContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"Gemma4-E4B-Q4_K_M.gguf","meta":{"n_ctx":8192}}],"object":"list"}`))
	}))
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.URL = srv.URL
	a := NewAgent(cfg, ws)
	a.In = strings.NewReader("y\n")

	var buf strings.Builder
	if err := adoptExternalServer(a, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := a.Config.ActiveLlamafileEntry()
	if entry == nil {
		t.Fatal("expected an active llamafile entry after adoption")
	}
	if entry.ContextLength != 8192 {
		t.Errorf("expected adoptExternalServer to probe and store ContextLength=8192, got %d", entry.ContextLength)
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
	cfg.Llamafile.URL = srv.URL
	a := NewAgent(cfg, ws)
	a.In = strings.NewReader("n\n")

	var buf strings.Builder
	if err := adoptExternalServer(a, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Config.Llamafile.Active != "" {
		t.Error("expected LlamafileActive to remain empty when user declines adoption")
	}
}

// ─── runFirstRunWizard ───────────────────────────────────────────────────────

func TestRunFirstRunWizard_emptyInput(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.ModelsDir = t.TempDir() // empty dir — no .llamafile files
	a := NewAgent(cfg, ws)
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
	cfg := DefaultConfig()
	cfg.Llamafile.ModelsDir = t.TempDir() // empty dir — no .llamafile files
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	// Provide a non-existent path — the add flow should fail with a not-found error.
	err := runFirstRunWizard(a, strings.NewReader("/nonexistent/model.llamafile\n"), &buf)
	if err == nil {
		t.Fatal("expected error for non-existent llamafile path")
	}
}

// TestUseLlamafileEntry_updatesRecorder is the regression test for the bug
// where /llamafile use (and all other code paths through useLlamafileEntry)
// did not call RecordModelSwitch, leaving the recorder showing the old model
// name in all subsequent scene headers and speaker lines.
func TestUseLlamafileEntry_updatesRecorder(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)

	recPath := filepath.Join(t.TempDir(), "session.fountain")
	rec, err := NewRecorder(recPath, "none", ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	a.Recorder = rec

	var buf strings.Builder
	_ = a.useLlamafileEntry("apertus", &buf)
	rec.Close()

	data, _ := os.ReadFile(recPath)
	content := string(data)

	if !strings.Contains(content, "model switch: apertus (llamafile)") {
		t.Errorf("expected model switch note after useLlamafileEntry, got:\n%s", content)
	}
}

// TestUseLlamafileEntry_subsequentTurnUsesNewModel verifies the full chain:
// after useLlamafileEntry, RecordTurnWithStats uses the new model name in the
// scene header and speaker lines — not the stale name from recorder creation.
func TestUseLlamafileEntry_subsequentTurnUsesNewModel(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)

	recPath := filepath.Join(t.TempDir(), "session.fountain")
	rec, _ := NewRecorder(recPath, "none", ws.Root)
	a.Recorder = rec

	var buf strings.Builder
	_ = a.useLlamafileEntry("apertus", &buf)
	_ = rec.RecordTurn("review the file", "Here is my review.")
	rec.Close()

	data, _ := os.ReadFile(recPath)
	content := string(data)

	if !strings.Contains(content, "Model: APERTUS.") {
		t.Errorf("expected APERTUS in scene header after switch, got:\n%s", content)
	}
	if strings.Contains(content, "Forwarding to NONE.") {
		t.Errorf("stale NONE speaker still present after switch:\n%s", content)
	}
	if !strings.Contains(content, "Forwarding to APERTUS.") {
		t.Errorf("expected APERTUS speaker after switch, got:\n%s", content)
	}
}

// TestRunFirstRunWizard_pickerWhenModelsExist verifies that when LlamafileModelsDir
// contains .llamafile files, runFirstRunWizard shows the directory picker instead
// of the generic download-guidance wizard text.
func TestRunFirstRunWizard_pickerWhenModelsExist(t *testing.T) {
	modelsDir := t.TempDir()
	fakePath := filepath.Join(modelsDir, "test-model.llamafile")
	if err := os.WriteFile(fakePath, []byte("not-a-real-llamafile"), 0o755); err != nil {
		t.Fatal(err)
	}

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.ModelsDir = modelsDir
	a := NewAgent(cfg, ws)
	// a.In provides the model name for cmdLlamafileAdd's name prompt.
	// SelectFrom auto-selects the single file, so only the name line is consumed.
	a.In = strings.NewReader("my-model\n")

	var buf strings.Builder
	// An error is expected — the file is not a real llamafile executable.
	_ = runFirstRunWizard(a, strings.NewReader(""), &buf)

	out := buf.String()
	if strings.Contains(out, "Harvey couldn't find a model to connect to") {
		t.Error("wizard download text should NOT be shown when models exist in LlamafileModelsDir")
	}
	if !strings.Contains(out, "Llamafiles found in") {
		t.Errorf("expected directory picker output, got:\n%s", out)
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
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "external", Path: ""}}
	cfg.Llamafile.Active = "external"
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	err := restartActiveLlamafile(a, &buf)
	if err == nil {
		t.Fatal("expected error for adopted server with empty path")
	}
}

// ─── startAndUseLlamafile — stale server adoption ────────────────────────────

// fakeLlamafileServer starts an httptest server that answers /v1/models with
// the given model name. Returns the server and its URL.
func fakeLlamafileServer(t *testing.T, modelName string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"data":[{"id":%q}],"object":"list"}`, modelName)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestStartAndUseLlamafile_staleServerSameModel(t *testing.T) {
	srv := fakeLlamafileServer(t, "qwen-coding")
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.URL = srv.URL
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "qwen-coding", Path: "/tmp/q.llamafile"}}
	a := NewAgent(cfg, ws)

	var buf strings.Builder
	entry := &a.Config.Llamafile.Models[0]
	_ = a.startAndUseLlamafile(entry, &buf)

	out := buf.String()
	// Server matched — should show a connection-feedback line, not a mismatch warning.
	if strings.Contains(out, "not") && strings.Contains(out, "configured") {
		t.Errorf("unexpected mismatch warning for matching model: %s", out)
	}
	if a.Client == nil {
		t.Error("expected Client to be set after stale-server adoption")
	}
}

func TestStartAndUseLlamafile_staleServerDifferentModel(t *testing.T) {
	// Server is running "phi-mini" but we asked for "qwen-coding".
	srv := fakeLlamafileServer(t, "phi-mini")
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.URL = srv.URL
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "qwen-coding", Path: "/tmp/q.llamafile"}}
	a := NewAgent(cfg, ws)

	var buf strings.Builder
	entry := &a.Config.Llamafile.Models[0]
	_ = a.startAndUseLlamafile(entry, &buf)

	out := buf.String()
	// Should warn that the running model differs from the expected one.
	if !strings.Contains(out, "phi-mini") {
		t.Errorf("expected detected model name 'phi-mini' in output, got: %s", out)
	}
	// Client should still be set (we adopt the running model).
	if a.Client == nil {
		t.Error("expected Client to be set even when detected model differs")
	}
}

// ─── selectBackend — connection feedback format ───────────────────────────────

func TestSelectBackend_connectionFeedbackFormat(t *testing.T) {
	// Start a fake llamafile server so ProbeLlamafile returns true.
	srv := fakeLlamafileServer(t, "qwen-coding")
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.URL = srv.URL
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "qwen-coding", Path: "/tmp/q.llamafile"}}
	cfg.Llamafile.Active = "qwen-coding"
	cfg.LlamaCpp.URL = "http://127.0.0.1:1" // unreachable — prevent Case 0 adoption
	a := NewAgent(cfg, ws)

	var buf strings.Builder
	_ = a.selectBackend(newTestBufioReader(""), &buf, "")

	out := buf.String()
	// Should show "Connecting to" feedback rather than the old "Checking llamafile".
	if !strings.Contains(out, "Connecting to") {
		t.Errorf("expected 'Connecting to' in startup feedback, got: %s", out)
	}
}

// TestPickBackend_ListsUnregisteredDiskModels reproduces the startup-picker
// bug report: only 4 of 7 llamafiles in ~/Models showed up because
// pickBackend only listed the registry (a.Config.Llamafile.Models), not a
// live disk scan. Registering one of three on-disk files must not hide the
// other two from the startup picker.
func TestPickBackend_ListsUnregisteredDiskModels(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.llamafile", "bravo.llamafile", "charlie.llamafile"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o755); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.Llamafile.ModelsDir = dir
	cfg.Llamafile.Models = []LlamafileEntry{{Name: "alpha", Path: filepath.Join(dir, "alpha.llamafile")}}
	cfg.Llamafile.URL = "http://127.0.0.1:1" // unreachable — no running server to adopt
	cfg.Ollama.URL = "http://127.0.0.1:1"    // unreachable — keep Ollama out of the list
	a := NewAgent(cfg, ws)

	var buf strings.Builder
	// "0" cancels the picker without starting any model.
	_ = a.pickBackend(newTestBufioReader("0\n"), &buf, "")

	out := buf.String()
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected %q in startup picker output, got:\n%s", name, out)
		}
	}
}

// TestPickBackend_ListsGGUFModels is the regression test for the reported bug
// (TODO.md: "I have both Llamafile and gguf models in ~/Models... but the
// gguf models are not listed as an option"): pickBackend's combined startup
// picker previously only ever considered llamafile and Ollama models, with no
// code path for .gguf/llama.cpp models at all — confirmed by reading the code
// before writing this test, not assumed.
func TestPickBackend_ListsGGUFModels(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"delta.gguf", "echo.gguf"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = dir
	cfg.LlamaCpp.URL = "http://127.0.0.1:1"  // unreachable — no running server to adopt
	cfg.Llamafile.ModelsDir = t.TempDir()    // isolate from any real ~/Models on the test machine
	cfg.Llamafile.URL = "http://127.0.0.1:1" // unreachable — no running server to adopt
	cfg.Ollama.URL = "http://127.0.0.1:1"    // unreachable — keep Ollama out of the list
	a := NewAgent(cfg, ws)

	var buf strings.Builder
	// "0" cancels the picker without starting any model.
	_ = a.pickBackend(newTestBufioReader("0\n"), &buf, "")

	out := buf.String()
	for _, name := range []string{"delta", "echo"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected %q (a .gguf model) in startup picker output, got:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "llamacpp") {
		t.Errorf("expected the .gguf entries to be labelled (llamacpp), got:\n%s", out)
	}
}
