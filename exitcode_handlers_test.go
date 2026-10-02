package harvey

import (
	"io"
	"strings"
	"testing"
)

// Reclassifying the command handlers' errors (the follow-up named in
// harvey/decisions/0006-*.md): a failure a slash command used to print and
// swallow now comes back as a classed error, so a non-interactive session
// exits with its class. Phase 1 is the dispatcher itself.

// An unknown command name is a usage error: the command line is wrong and
// nothing was attempted.
func TestDispatch_UnknownCommandIsUsageError(t *testing.T) {
	a, _ := runFixture(t, false)
	a.registerCommands()
	var out strings.Builder
	exit, err := a.dispatch("/nosuchcommand", &out)
	if exit {
		t.Errorf("dispatch asked to exit on an unknown command")
	}
	if err == nil {
		t.Fatalf("dispatch(/nosuchcommand) = nil, want a usage error")
	}
	if got := ExitCodeFor(err); got != ClassUsage {
		t.Errorf("class = %v (%v), want usage", got, err)
	}
	for _, want := range []string{"/nosuchcommand", "/help"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Known commands, and the exit words, are not affected.
func TestDispatch_KnownCommandsStillSucceed(t *testing.T) {
	a, _ := runFixture(t, false)
	a.registerCommands()
	if _, err := a.dispatch("/help", io.Discard); err != nil {
		t.Errorf("dispatch(/help) = %v, want nil", err)
	}
	if exit, err := a.dispatch("/exit", io.Discard); !exit || err != nil {
		t.Errorf("dispatch(/exit) = (%v, %v), want (true, nil)", exit, err)
	}
}

// A scripted session that names an unknown command exits usage (2).
func TestRun_NonInteractive_UnknownCommandExitsUsage(t *testing.T) {
	a := scriptedFixture(t, false, "/nosuchcommand\n")
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassUsage {
		t.Fatalf("Run() = %v (%v), want usage", got, err)
	}
}

// At a terminal a typo is something the person just tries again: the session
// still ends cleanly, and the message is shown.
func TestRun_Interactive_UnknownCommandStillExitsZero(t *testing.T) {
	a := scriptedFixture(t, true, "/nosuchcommand\n")
	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run() at a terminal = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "/nosuchcommand") {
		t.Errorf("output does not name the unknown command:\n%s", out.String())
	}
}

// /loop stops on the first failing iteration, so a mistyped command inside a
// loop is reported once instead of running the whole interval count.
func TestRunLoopIteration_UnknownCommandReturnsUsageError(t *testing.T) {
	a, _ := runFixture(t, false)
	a.registerCommands()
	_, err := runLoopIteration(t.Context(), a, "/nosuchcommand", io.Discard)
	if got := ExitCodeFor(err); got != ClassUsage {
		t.Fatalf("runLoopIteration = %v (%v), want usage", got, err)
	}
}
