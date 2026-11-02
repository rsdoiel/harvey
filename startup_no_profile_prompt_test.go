package harvey

import (
	"os"
	"strings"
	"testing"
)

// The first-run profile prompt ("Select [1-7] or press Enter for Blank") is
// gone: profiles are guesses about what a user needs, harvey stays simple, and
// the prompt took the first line of piped input as its answer (D7, 2026-11-02;
// TODO.md). A fresh workspace starts at the REPL, through the real Run.

func TestRun_FreshWorkspaceAsksNoProfileQuestion(t *testing.T) {
	a, ws := runFixture(t, true)
	a.Config.Memory.Enabled = true
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	a.stdin = r
	// "n" answers "Start Ollama now?", the fixture having no Ollama.
	if _, err := w.WriteString("n\n/status\n/exit\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run = %v", err)
	}
	got := out.String()
	for _, banned := range []string{"Select [1-", "workspace profile", "Workspace profile saved"} {
		if strings.Contains(got, banned) {
			t.Errorf("a fresh workspace was asked about profiles (%q):\n%s", banned, got)
		}
	}
	// The first piped line was a command, not an answer to a prompt.
	if !strings.Contains(got, "Backend:") || !strings.Contains(got, "History:") {
		t.Errorf("/status did not run as the first piped line:\n%s", got)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	if metas, _ := store.List(string(MemoryTypeWorkspaceProfile)); len(metas) != 0 {
		t.Errorf("%d workspace_profile memories were written on startup", len(metas))
	}
}
