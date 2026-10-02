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
	err := cmdSkill(a, []string{"bogus-subcommand"}, &out)
	if ExitCodeFor(err) != ClassUsage || !strings.Contains(err.Error(), "suggest") {
		t.Errorf("expected a usage error listing 'suggest', got: %v", err)
	}
}
