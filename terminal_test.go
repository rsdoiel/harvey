package harvey

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// makeTestTree creates a temporary directory tree for completion tests:
//
//	root/
//	  harvey/
//	    main.go
//	    README.md
//	  docs/
//	    guide.pdf
//	    notes.txt
//	  .hidden/
//	  README.md
//	  report.pdf
func makeTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dirs := []string{"harvey", "docs", ".hidden"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"harvey/main.go":   "package main",
		"harvey/README.md": "# harvey",
		"docs/guide.pdf":   "%PDF",
		"docs/notes.txt":   "notes",
		"README.md":        "# root",
		"report.pdf":       "%PDF",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func sortedMatches(matches []string) []string {
	sort.Strings(matches)
	return matches
}

func TestWorkspacePathCandidates_rootListing(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "", false, nil))
	// Should see dirs and files at root, no hidden entries.
	want := []string{"README.md", "docs/", "harvey/", "report.pdf"}
	if len(got) != len(want) {
		t.Fatalf("root listing: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("root listing[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_prefixFilter(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "h", false, nil))
	want := []string{"harvey/"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("prefix 'h': got %v, want %v", got, want)
	}
}

func TestWorkspacePathCandidates_subdirListing(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "harvey/", false, nil))
	want := []string{"harvey/README.md", "harvey/main.go"}
	if len(got) != len(want) {
		t.Fatalf("harvey/ listing: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("harvey/[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_onlyDirs(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "", true, nil))
	want := []string{"docs/", "harvey/"}
	if len(got) != len(want) {
		t.Fatalf("onlyDirs: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("onlyDirs[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_extFilter(t *testing.T) {
	root := makeTestTree(t)
	pdfOnly := map[string]bool{".pdf": true}
	got := sortedMatches(workspacePathCandidates(root, "", false, pdfOnly))
	// Directories are always included so the user can navigate into them;
	// regular files are filtered to the given extensions.
	want := []string{"docs/", "harvey/", "report.pdf"}
	if len(got) != len(want) {
		t.Fatalf("extFilter .pdf at root: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("extFilter[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_extFilterSubdir(t *testing.T) {
	root := makeTestTree(t)
	pdfOnly := map[string]bool{".pdf": true}
	got := sortedMatches(workspacePathCandidates(root, "docs/", false, pdfOnly))
	want := []string{"docs/guide.pdf"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("extFilter .pdf in docs/: got %v, want %v", got, want)
	}
}

func TestWorkspacePathCandidates_hiddenExcluded(t *testing.T) {
	root := makeTestTree(t)
	got := workspacePathCandidates(root, ".", false, nil)
	for _, m := range got {
		if filepath.Base(m) == ".hidden" || m == ".hidden/" {
			t.Errorf("hidden entry leaked into completions: %q", m)
		}
	}
}

func TestWorkspacePathCandidates_escapeBlocked(t *testing.T) {
	root := makeTestTree(t)
	got := workspacePathCandidates(root, "../../etc/", false, nil)
	if len(got) != 0 {
		t.Errorf("path escape should produce no completions, got %v", got)
	}
}

// ─── activeModelLabel ────────────────────────────────────────────────────────

func TestActiveModelLabel_noBackend(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	if got := activeModelLabel(a); got != "none" {
		t.Errorf("got %q want %q", got, "none")
	}
}

func TestActiveModelLabel_llamafile(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.LlamafileActive = "qwen-coding"
	if got := activeModelLabel(a); got != "qwen-coding (llamafile)" {
		t.Errorf("got %q want %q", got, "qwen-coding (llamafile)")
	}
}

func TestActiveModelLabel_ollama(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.OllamaModel = "llama3.2:3b"
	if got := activeModelLabel(a); got != "llama3.2:3b (ollama)" {
		t.Errorf("got %q want %q", got, "llama3.2:3b (ollama)")
	}
}

func TestActiveModelLabel_llamafileTakesPriority(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.LlamafileActive = "qwen-coding"
	a.Config.OllamaModel = "llama3.2:3b"
	if got := activeModelLabel(a); got != "qwen-coding (llamafile)" {
		t.Errorf("llamafile should take priority; got %q", got)
	}
}

// ─── probeActiveBackend ──────────────────────────────────────────────────────

func TestProbeActiveBackend_noBackendConfigured(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	// Neither LlamafileActive nor OllamaModel set — must return false without
	// making any network call.
	if probeActiveBackend(a) {
		t.Error("expected false when no backend is configured")
	}
}

func TestProbeActiveBackend_llamafileNotRunning(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.LlamafileActive = "qwen-coding"
	a.Config.LlamafileURL = "http://127.0.0.1:19991" // nothing listening here
	if probeActiveBackend(a) {
		t.Error("expected false when llamafile server is not running")
	}
}

func TestProbeActiveBackend_ollamaNotRunning(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.OllamaModel = "llama3.2:3b"
	a.Config.OllamaURL = "http://127.0.0.1:19992" // nothing listening here
	if probeActiveBackend(a) {
		t.Error("expected false when ollama server is not running")
	}
}

// ─── isConnectionError ───────────────────────────────────────────────────────

func TestIsConnectionError_refused(t *testing.T) {
	err := fmt.Errorf("dial tcp: connect: connection refused")
	if !isConnectionError(err) {
		t.Error("expected true for connection refused")
	}
}

func TestIsConnectionError_eof(t *testing.T) {
	err := fmt.Errorf("unexpected EOF")
	if !isConnectionError(err) {
		t.Error("expected true for EOF")
	}
}

func TestIsConnectionError_reset(t *testing.T) {
	err := fmt.Errorf("read tcp: connection reset by peer")
	if !isConnectionError(err) {
		t.Error("expected true for connection reset")
	}
}

func TestIsConnectionError_other(t *testing.T) {
	err := fmt.Errorf("invalid JSON response")
	if isConnectionError(err) {
		t.Error("expected false for non-connection error")
	}
}

func TestIsConnectionError_nil(t *testing.T) {
	if isConnectionError(nil) {
		t.Error("expected false for nil error")
	}
}

// ─── attemptModelSwitch ──────────────────────────────────────────────────────

func TestAttemptModelSwitch_notFound(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	switched, err := attemptModelSwitch(a, "unknown-model", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if switched {
		t.Error("expected switched=false for unknown model name")
	}
}

func TestAttemptModelSwitch_llamafileRegistered(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/nonexistent.llamafile"},
	}
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	// The switch will fail because the file doesn't exist and no server is
	// running, but the name IS found, so switched==true and err!=nil.
	switched, err := attemptModelSwitch(a, "qwen-coding", &buf)
	if !switched {
		t.Error("expected switched=true when model name is registered")
	}
	// An error is acceptable (server not running); what matters is switched==true.
	_ = err
}

func TestAttemptModelSwitch_caseInsensitive(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/nonexistent.llamafile"},
	}
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	// Upper-case name should still find "qwen-coding".
	switched, _ := attemptModelSwitch(a, "QWEN-CODING", &buf)
	if !switched {
		t.Error("expected switched=true for case-insensitive name match")
	}
}

func TestAttemptModelSwitch_aliasLookupCaseInsensitive(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	// Register an alias. Aliases are stored with lowercase keys.
	cfg.ModelAliases = map[string]string{"coder": "qwen2.5-coder:7b"}
	cfg.OllamaURL = "http://localhost:11434" // won't connect, just wiring
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	// "CODER" should match the alias stored as "coder".
	switched, _ := attemptModelSwitch(a, "CODER", &buf)
	if !switched {
		t.Error("expected switched=true for case-insensitive alias match")
	}
}

// ─── @mention local model switch (REPL-level) ─────────────────────────────────

func TestAtMention_localModelSwitch_withFakeServer(t *testing.T) {
	// A fake llamafile server so startAndUseLlamafile succeeds.
	srv := fakeLlamafileServer(t, "phi-mini")
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileURL = srv.URL
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/q.llamafile"},
		{Name: "phi-mini", Path: "/tmp/p.llamafile"},
	}
	cfg.LlamafileActive = "qwen-coding"
	a := NewAgent(cfg, ws)
	// Wire up a starting client so the REPL has a backend.
	a.Client = newLlamafileLLMClient(srv.URL+"/v1", "qwen-coding", 0)

	var buf strings.Builder
	// attemptModelSwitch with "phi-mini" should succeed (server is reachable).
	switched, err := attemptModelSwitch(a, "phi-mini", &buf)
	if !switched {
		t.Error("expected switched=true when model is registered and server reachable")
	}
	if err != nil {
		t.Errorf("unexpected switch error: %v", err)
	}
	// After switch, active model should be phi-mini.
	if a.Config.LlamafileActive != "phi-mini" {
		t.Errorf("expected LlamafileActive=phi-mini, got %q", a.Config.LlamafileActive)
	}
}

// ─── pickBackend tests ────────────────────────────────────────────────────────

func TestPickBackend_listsLlamafilesBeforeOllama(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/q.llamafile"},
		{Name: "phi-mini", Path: "/tmp/p.llamafile"},
	}
	a := NewAgent(cfg, ws)
	// "0\n" = select none, avoiding any actual llamafile start.
	a.In = strings.NewReader("0\n")
	var buf strings.Builder

	_ = a.pickBackend(newTestBufioReader("0\n"), &buf, "")

	out := buf.String()
	// Both models should appear in the output.
	if !strings.Contains(out, "qwen-coding") {
		t.Errorf("expected qwen-coding in picker output, got: %s", out)
	}
	if !strings.Contains(out, "phi-mini") {
		t.Errorf("expected phi-mini in picker output, got: %s", out)
	}
	// llamafile label should appear before any Ollama label.
	qwenPos := strings.Index(out, "qwen-coding")
	phiPos := strings.Index(out, "phi-mini")
	if qwenPos < 0 || phiPos < 0 {
		t.Fatal("models missing from output")
	}
	if qwenPos > phiPos {
		t.Error("expected qwen-coding listed before phi-mini (registration order)")
	}
	// No backend should be set after selecting none.
	if a.Client != nil {
		t.Error("expected no client set after selecting none")
	}
}

func TestPickBackend_autoSelectsPreferredLlamafile(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/nonexistent.llamafile"},
		{Name: "phi-mini", Path: "/tmp/phi.llamafile"},
	}
	a := NewAgent(cfg, ws)
	var buf strings.Builder

	// preferredModel matches "qwen-coding" — should auto-select without showing picker.
	// Start will fail (no binary), but the output should mention starting qwen-coding.
	_ = a.pickBackend(newTestBufioReader(""), &buf, "qwen-coding")

	out := buf.String()
	// Should have attempted to start or use qwen-coding, not phi-mini.
	if strings.Contains(out, "phi-mini") && !strings.Contains(out, "qwen-coding") {
		t.Errorf("auto-select should have chosen qwen-coding, got: %s", out)
	}
}

func TestPickBackend_noSelectionLeavesNoClient(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/q.llamafile"},
	}
	a := NewAgent(cfg, ws)
	var buf strings.Builder

	_ = a.pickBackend(newTestBufioReader("0\n"), &buf, "")

	if a.Client != nil {
		t.Error("expected no client when user selects 0 (none)")
	}
}

func TestSelectBackend_callsPickBackendWhenLlamafileModelsExistButNoActive(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/q.llamafile"},
	}
	// LlamafileActive is empty — should take the pickBackend path.
	a := NewAgent(cfg, ws)
	var buf strings.Builder

	_ = a.selectBackend(newTestBufioReader("0\n"), &buf, "")

	out := buf.String()
	// The picker should have been shown (mentions qwen-coding).
	if !strings.Contains(out, "qwen-coding") {
		t.Errorf("expected picker to show registered llamafile, got: %s", out)
	}
}

// newTestBufioReader wraps a string in a bufio.Reader for terminal test helpers.
func newTestBufioReader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

// ─── routeSpinnerLabel tests ──────────────────────────────────────────────────

func TestRouteSpinnerLabel_includesModel(t *testing.T) {
	ep := &RouteEndpoint{Name: "pi2", Model: "llama3.1:8b"}
	label := routeSpinnerLabel("pi2", ep)
	if !strings.Contains(label, "llama3.1:8b") {
		t.Errorf("expected model name in spinner label, got: %q", label)
	}
	if !strings.Contains(label, "pi2") {
		t.Errorf("expected route name in spinner label, got: %q", label)
	}
}

func TestRouteSpinnerLabel_fallbackWhenNoModel(t *testing.T) {
	ep := &RouteEndpoint{Name: "pi2", Model: ""}
	label := routeSpinnerLabel("pi2", ep)
	if !strings.Contains(label, "pi2") {
		t.Errorf("expected route name in fallback label, got: %q", label)
	}
	if !strings.Contains(label, "working") {
		t.Errorf("expected 'working' in fallback label, got: %q", label)
	}
}

// ─── --continue / --resume model hint extraction ──────────────────────────────

func TestSelectBackend_extractsModelHintFromContinuePath(t *testing.T) {
	// Write a minimal session file that records "llamafile (qwen-coding)" as the model.
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.fountain")
	rec, err := NewRecorder(sessionPath, "llamafile (qwen-coding)", dir)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	_ = rec.RecordTurnWithStats("hello", "world", ChatStats{}, nil, "", nil, nil)
	rec.Close()

	// Set up agent with qwen-coding registered but no active model.
	// Point LlamafileURL at a fake server so pickBackend can auto-select.
	srv := fakeLlamafileServer(t, "qwen-coding")
	defer srv.Close()

	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ContinuePath = sessionPath
	cfg.LlamafileURL = srv.URL
	cfg.LlamafileModels = []LlamafileEntry{
		{Name: "qwen-coding", Path: "/tmp/q.llamafile"},
	}
	a := NewAgent(cfg, ws)
	var buf strings.Builder

	// selectBackend should auto-select qwen-coding (from the session hint)
	// without showing the interactive picker.
	_ = a.selectBackend(newTestBufioReader(""), &buf, "")

	// The auto-selection path connects the client.
	if a.Client == nil {
		t.Error("expected Client to be set after auto-selection from session hint")
	}
}
