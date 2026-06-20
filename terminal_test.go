package harvey

import (
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
