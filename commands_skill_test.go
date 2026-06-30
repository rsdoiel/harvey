package harvey

import (
	"strings"
	"testing"
)

// TestCmdSkill_SuggestNoWorkspace verifies that /skill suggest returns an
// error when the agent has no workspace open, rather than panicking.
func TestCmdSkill_SuggestNoWorkspace(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace = nil

	var out strings.Builder
	err := cmdSkill(a, []string{"suggest"}, &out)
	if err == nil {
		t.Fatal("expected an error when workspace is nil, got nil")
	}
}

// TestCmdSkill_SuggestUnknownSubcommandListed verifies that the usage message
// shown for an unrecognised subcommand includes "suggest".
func TestCmdSkill_SuggestUnknownSubcommandListed(t *testing.T) {
	a := newTestAgent(t)

	var out strings.Builder
	_ = cmdSkill(a, []string{"bogus-subcommand"}, &out)
	if !strings.Contains(out.String(), "suggest") {
		t.Errorf("expected 'suggest' in usage message, got: %q", out.String())
	}
}
