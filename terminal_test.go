package harvey

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestActiveModelLabel_ollama(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.Ollama.Model = "llama3.2:3b"
	if got := activeModelLabel(a); got != "llama3.2:3b (ollama)" {
		t.Errorf("got %q want %q", got, "llama3.2:3b (ollama)")
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

func TestProbeActiveBackend_ollamaNotRunning(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.Ollama.Model = "llama3.2:3b"
	a.Config.Ollama.URL = "http://127.0.0.1:19992" // nothing listening here
	if probeActiveBackend(a) {
		t.Error("expected false when ollama server is not running")
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

func TestAttemptModelSwitch_aliasLookupCaseInsensitive(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	// Register an alias. Aliases are stored with lowercase keys.
	cfg.ModelAliases = map[string]ModelAlias{"coder": {Model: "qwen2.5-coder:7b"}}
	cfg.Ollama.URL = "http://localhost:11434" // won't connect, just wiring
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	// "CODER" should match the alias stored as "coder".
	switched, _ := attemptModelSwitch(a, "CODER", &buf)
	if !switched {
		t.Error("expected switched=true for case-insensitive alias match")
	}
}

// ─── @mention local model switch (REPL-level) ─────────────────────────────────

// TestAtMention_tagRouting_resolvesTaggedAlias verifies that @code switches to
// the model whose alias is tagged "code", even though no alias is literally
// named "code". This exercises the Step 3 tag-lookup path in the REPL.
func TestAtMention_tagRouting_resolvesTaggedAlias(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{
		"granite": {Model: "granite3.3:8b", Tags: []string{"code", "instruct"}},
	}
	a := NewAgent(cfg, ws)
	a.Client = newOllamaLLMClient("http://127.0.0.1:1", "granite3.3:8b", 0)

	// @code should not match any alias by name, but should resolve via tag "code"
	// → granite alias → granite3.3:8b → attemptModelSwitch succeeds.
	tagModel, ok := resolveTagAlias(a.Config, "code")
	if !ok {
		t.Fatal("resolveTagAlias(code): expected ok=true")
	}
	if tagModel != "granite3.3:8b" {
		t.Errorf("resolveTagAlias(code) = %q, want granite3.3:8b", tagModel)
	}

	var buf strings.Builder
	switched, err := attemptModelSwitch(a, tagModel, &buf)
	if !switched {
		t.Errorf("attemptModelSwitch(%q): expected switched=true", tagModel)
	}
	if err != nil {
		t.Errorf("attemptModelSwitch: %v", err)
	}
}

// TestAtMention_tagRouting_noTagMatch verifies that resolveTagAlias returns
// ("", false) when no alias carries the requested tag — preserving the
// existing "not found" behaviour.
func TestAtMention_tagRouting_noTagMatch(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{
		"qwen": {Model: "qwen3:8b", Tags: []string{"reasoning"}},
	}
	a := NewAgent(cfg, ws)

	_, ok := resolveTagAlias(a.Config, "code")
	if ok {
		t.Error("expected resolveTagAlias(code) = false when no alias carries 'code' tag")
	}
}

// TestAtMention_tagRouting_exactAliasPrecedence verifies that @granite uses
// the exact alias (Step 2) before tag lookup (Step 3)..
func TestAtMention_tagRouting_exactAliasPrecedence(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{
		"granite": {Model: "granite3.3:8b", Tags: []string{"code", "instruct"}},
	}
	a := NewAgent(cfg, ws)
	a.Client = newOllamaLLMClient("http://127.0.0.1:1", "granite3.3:8b", 0)

	// @granite → exact alias hit via ResolveModelAlias before tag lookup runs.
	resolved := a.Config.ResolveModelAlias("granite")
	if resolved != "granite3.3:8b" {
		t.Errorf("exact alias resolution: got %q, want granite3.3:8b", resolved)
	}

	var buf strings.Builder
	switched, err := attemptModelSwitch(a, "granite", &buf)
	if !switched {
		t.Errorf("attemptModelSwitch(granite): expected switched=true via alias")
	}
	if err != nil {
		t.Errorf("attemptModelSwitch: %v", err)
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

// ─── ragAugment ───────────────────────────────────────────────────────────────

// fakeOllamaEmbedServer starts an httptest server that responds to POST
// /api/embed with a fixed embedding vector, mimicking the Ollama embed API.
func fakeOllamaEmbedServer(t *testing.T, vec []float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		type resp struct {
			Embeddings [][]float64 `json:"embeddings"`
		}
		_ = json.NewEncoder(w).Encode(resp{Embeddings: [][]float64{vec}})
	}))
}
