package harvey

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// "Scripting harvey exit codes" (harvey/decisions/0006-*.md): a non-interactive
// session (piped stdin, or a script) exits with the class of the first slash
// command or chat turn that failed, instead of always 0 — the "not covered"
// gap exit-codes-survey.md left open.

// keepFirstFailure is the ordering rule in isolation: the first non-nil error
// wins, and a later one never overwrites it.
func TestKeepFirstFailure_TheFirstNonNilWins(t *testing.T) {
	e1 := errors.New("first")
	e2 := errors.New("second")
	if got := keepFirstFailure(nil, e1); got != e1 {
		t.Errorf("keepFirstFailure(nil, e1) = %v, want e1", got)
	}
	if got := keepFirstFailure(e1, e2); got != e1 {
		t.Errorf("keepFirstFailure(e1, e2) = %v, want e1 (the first one)", got)
	}
	if got := keepFirstFailure(nil, nil); got != nil {
		t.Errorf("keepFirstFailure(nil, nil) = %v, want nil", got)
	}
}

// scriptedFixture is runFixture, plus a fake Ollama server reachable enough
// for backend selection to succeed (ProbeOllama only needs 200 on /api/tags,
// and pickOllamaModel takes cfg.Ollama.Model unconditionally, so the REPL
// loop is reached without a real model server) and stdin piped from a real
// file (not /dev/null) so the REPL has scripted commands to dispatch.
func scriptedFixture(t *testing.T, terminal bool, script string) *Agent {
	t.Helper()
	a, _ := runFixture(t, terminal)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	a.Config.Ollama.URL = srv.URL
	a.Config.Ollama.Model = "fixture-model"
	// Workspace-profile onboarding otherwise reads the first line of the
	// script as its own picker answer, before the REPL ever sees it.
	a.Config.Memory.Enabled = false

	f, err := os.CreateTemp(t.TempDir(), "script")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(script); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	a.stdin = f
	return a
}

func TestRun_NonInteractive_FailedScriptedCommandDecidesExitClass(t *testing.T) {
	a := scriptedFixture(t, false, "/plan next\n")
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassNegative {
		t.Fatalf("Run() = %v (%v), want negative (no plan found)", got, err)
	}
}

func TestRun_Interactive_FailedScriptedCommandStillExitsZero(t *testing.T) {
	a := scriptedFixture(t, true, "/plan next\n")
	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run() at a terminal = %v, want nil even though /plan next failed", err)
	}
}

func TestRun_NonInteractive_OnlySuccessfulCommandsExitZero(t *testing.T) {
	a := scriptedFixture(t, false, "/help\n")
	if err := a.Run(io.Discard); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}
