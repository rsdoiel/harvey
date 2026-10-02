package harvey

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// kbUsageSubcommands returns the subcommand names cmdKB advertises in its
// "Unknown kb subcommand" usage line, which is the list of what it dispatches.
// It reads the source because a bare test agent has no knowledge base open.
func kbUsageSubcommands(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("commands_kb.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, `usage: /kb <`) {
			i := strings.Index(line, "<")
			j := strings.Index(line, ">")
			return strings.Split(line[i+1:j], "|")
		}
	}
	t.Fatal("no /kb usage line found in commands_kb.go")
	return nil
}

// TestKBRegistry_ListsEverySubcommandCmdKBDispatches verifies that the /kb
// registry entry (used for tab completion and /help) names every subcommand
// cmdKB accepts, so a subcommand added to the switch cannot go unlisted.
func TestKBRegistry_ListsEverySubcommandCmdKBDispatches(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	cmd := a.commands["kb"]

	want := kbUsageSubcommands(t)
	got := append([]string{}, cmd.Subcommands...)
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("/kb Subcommands = %v, want %v", got, want)
	}
	for _, sub := range want {
		if !strings.Contains(cmd.Usage, sub) {
			t.Errorf("/kb Usage %q does not mention %q", cmd.Usage, sub)
		}
	}
}
