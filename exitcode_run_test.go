package harvey

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// H3 of exit-codes-plan.md, the library half: what Agent.Run and the code under
// it return, by class. The command-line half is in cmd/harvey/main_test.go.

// runFixture returns an agent in a temp workspace with no backend reachable:
// HOME is empty (no ~/Models), Ollama points at a closed port, and stdin is
// /dev/null. terminal says whether stdin is to be treated as a terminal.
func runFixture(t *testing.T, terminal bool) (*Agent, *Workspace) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HARVEY_LLAMAFILE_DIR", "")
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Run builds its workspace from cfg.WorkDir ("." by default) and writes
	// sessions, databases and logs under it, so both must be the temp
	// directory or a test writes into the real checkout.
	t.Chdir(ws.Root)
	cfg := DefaultConfig()
	cfg.WorkDir = ws.Root
	cfg.Ollama.URL = "http://127.0.0.1:1"
	cfg.Llamafile.ModelsDir = t.TempDir()
	cfg.Security.SafeMode = true
	a := NewAgent(cfg, ws)
	a.hailoDevicePath = filepath.Join(t.TempDir(), "no-hailo0") // not this machine's card
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devnull.Close() })
	a.stdin = devnull
	old := isTerminal
	isTerminal = func(*os.File) bool { return terminal }
	t.Cleanup(func() { isTerminal = old })
	return a, ws
}

func writeBadYAML(t *testing.T, ws *Workspace) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws.HarveyDir(), "harvey.yaml"), []byte("rag: [broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A session with nobody at a terminal and no model to talk to cannot do its
// job: 69, not a silent exit 0.
func TestRun_NoBackendWhenNotInteractiveIsUnavailable(t *testing.T) {
	a, _ := runFixture(t, false)
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassUnavailable {
		t.Fatalf("Run with no backend and no terminal = %v (%v), want unavailable", got, err)
	}
}

// At a terminal the same state is the first-run guide, and the user can pick a
// model with /model use; the session starts and ends 0.
func TestRun_NoBackendAtATerminalStillStarts(t *testing.T) {
	a, _ := runFixture(t, true)
	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run at a terminal with no backend = %v, want a normal session", err)
	}
	if !strings.Contains(out.String(), "/model use") {
		t.Errorf("no pointer to /model use in the output:\n%s", out.String())
	}
}

// A harvey.yaml that does not parse is a warning where the user can fix it in
// the session, and 78 where nobody can.
func TestRun_MalformedHarveyYAMLIsAWarningAtATerminal(t *testing.T) {
	a, ws := runFixture(t, true)
	writeBadYAML(t, ws)
	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run at a terminal with a bad harvey.yaml = %v, want a warning and a session", err)
	}
	if !strings.Contains(out.String(), "harvey.yaml") {
		t.Errorf("the warning was dropped:\n%s", out.String())
	}
}

func TestRun_MalformedHarveyYAMLWhenNotInteractiveIsConfig(t *testing.T) {
	a, ws := runFixture(t, false)
	writeBadYAML(t, ws)
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassConfig {
		t.Fatalf("Run with a bad harvey.yaml and no terminal = %v (%v), want config", got, err)
	}
}

// --replay without --replay-continue never reaches the REPL, so it is
// non-interactive even at a terminal.
func TestRun_ReplayOnlyCountsAsNotInteractive(t *testing.T) {
	a, ws := runFixture(t, true)
	writeBadYAML(t, ws)
	a.Config.Session.ReplayPath = filepath.Join(t.TempDir(), "x.spmd")
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassConfig {
		t.Fatalf("Run --replay at a terminal with a bad harvey.yaml = %v (%v), want config", got, err)
	}
	a2, ws2 := runFixture(t, true)
	writeBadYAML(t, ws2)
	a2.Config.Session.ReplayPath = filepath.Join(t.TempDir(), "x.spmd")
	a2.Config.Session.ReplayContinue = true
	if got := ExitCodeFor(a2.Run(io.Discard)); got == ClassConfig {
		t.Error("--replay-continue is interactive, so a bad harvey.yaml is only a warning")
	}
}

func TestLoadHarveyYAML_ParseErrorIsConfig(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	writeBadYAML(t, ws)
	err := LoadHarveyYAML(ws, DefaultConfig())
	if err == nil {
		t.Fatal("a malformed harvey.yaml loaded without an error")
	}
	if got := ExitCodeFor(err); got != ClassConfig {
		t.Errorf("class %v, want config", got)
	}
}

// The replay file is checked before the backend: a missing file is 66 whether
// or not a model is connected, and with a good file and no model it is 69.
func TestReplayFromFountain_ChecksTheFileBeforeTheBackend(t *testing.T) {
	a := newTestAgent(t)
	err := a.ReplayFromFountain(t.Context(), filepath.Join(t.TempDir(), "missing.spmd"), "", io.Discard)
	if got := ExitCodeFor(err); got != ClassNoInput {
		t.Errorf("missing replay file, no backend = %v (%v), want no_input", got, err)
	}

	good := filepath.Join(t.TempDir(), "s.spmd")
	if err := os.WriteFile(good, []byte("Title: t\n\nINT. SESSION - NOW\n\nHARVEY\nhello\n\nUSER\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = a.ReplayFromFountain(t.Context(), good, "", io.Discard)
	if got := ExitCodeFor(err); got != ClassUnavailable {
		t.Errorf("good replay file, no backend = %v (%v), want unavailable", got, err)
	}
}

// A system prompt that cannot fit the model's context is a problem with the
// content the user supplied (HARVEY.md), not with the tool.
func TestSystemPromptExceedsContext_IsData(t *testing.T) {
	err := systemPromptExceedsContext("tiny", 5000, 1000)
	if err == nil {
		t.Fatal("expected an error for a prompt larger than the context")
	}
	if got := ExitCodeFor(err); got != ClassData {
		t.Errorf("class %v, want data", got)
	}
}

func TestStartLlamafileService_Classes(t *testing.T) {
	// The wrong kind of file is a bad input, not an unavailable service.
	_, err := StartLlamafileService("/bin/sh", "http://127.0.0.1:1", "", time.Second, -1, 0, nil)
	if got := ExitCodeFor(err); got != ClassNoInput {
		t.Errorf("wrong extension = %v (%v), want no_input", got, err)
	}
	// A llamafile that will not serve is an unavailable service.
	_, err = StartLlamafileService("/nonexistent/x.llamafile", "http://127.0.0.1:1", "", 5*time.Second, -1, 0, nil)
	if got := ExitCodeFor(err); got != ClassUnavailable {
		t.Errorf("launch that fails = %v (%v), want unavailable", got, err)
	}
}
