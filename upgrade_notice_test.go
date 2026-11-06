package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An upgrade notice replaces the per-process deprecation notices: it says what
// changed since the version this workspace last ran, once, and only on stderr,
// so it cannot reach the REPL's input and bring back the swallowed-first-line
// bug that removing the profile prompt fixed (2026-11-02).

var testNotices = []upgradeNotice{
	{"0.0.21", "profiles are gone"},
	{"0.0.22", "second change"},
	{"0.0.23", "third change"},
}

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.0.20", "0.0.21", true},
		{"0.0.21", "0.0.20", false},
		{"0.0.9", "0.0.10", true}, // numeric, not text
		{"0.1.0", "0.0.99", false},
		{"v0.0.20", "0.0.21", true},
		{"0.0.21", "0.0.21", false},
		{"0.0.21-dev", "0.0.21", false}, // a suffix does not make it older
		{"1.0.0", "0.9.9", false},
	} {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func lastVersionFile(a *Agent) string { return filepath.Join(a.Workspace.HarveyDir(), "last_version") }

func upgradedAgent(t *testing.T, used bool) *Agent {
	t.Helper()
	a := newTestAgent(t)
	if used { // a workspace that has been run before: it has a session on disk
		dir := filepath.Join(a.Workspace.HarveyDir(), "sessions")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "harvey-session-20261001-100000.jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestAnnounceUpgrade_aBrandNewWorkspaceSaysNothingAndRecordsTheVersion(t *testing.T) {
	a := upgradedAgent(t, false)
	var errOut strings.Builder
	a.announceUpgrade(&errOut, "0.0.21", testNotices)
	if errOut.Len() != 0 {
		t.Errorf("a new workspace was told about changes it never lived through:\n%s", errOut.String())
	}
	if b, _ := os.ReadFile(lastVersionFile(a)); strings.TrimSpace(string(b)) != "0.0.21" {
		t.Errorf("last_version = %q, want 0.0.21", b)
	}
}

func TestAnnounceUpgrade_anExistingWorkspaceWithNoRecordCountsAsComingFromTheBaseline(t *testing.T) {
	a := upgradedAgent(t, true)
	var errOut strings.Builder
	a.announceUpgrade(&errOut, "0.0.22", testNotices)
	got := errOut.String()
	for _, want := range []string{"0.0.20", "0.0.22", "profiles are gone", "second change"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "third change") {
		t.Errorf("a notice for a later version was shown:\n%s", got)
	}
}

func TestAnnounceUpgrade_showsOnlyWhatIsNewerThanTheRecordedVersion(t *testing.T) {
	a := upgradedAgent(t, true)
	os.WriteFile(lastVersionFile(a), []byte("0.0.21\n"), 0o644)
	var errOut strings.Builder
	a.announceUpgrade(&errOut, "0.0.23", testNotices)
	got := errOut.String()
	if strings.Contains(got, "profiles are gone") {
		t.Errorf("a change already announced was repeated:\n%s", got)
	}
	if !strings.Contains(got, "second change") || !strings.Contains(got, "third change") {
		t.Errorf("notice lacks the newer changes:\n%s", got)
	}
}

func TestAnnounceUpgrade_isShownOnceAndThenSilent(t *testing.T) {
	a := upgradedAgent(t, true)
	var first, second strings.Builder
	a.announceUpgrade(&first, "0.0.21", testNotices)
	a.announceUpgrade(&second, "0.0.21", testNotices)
	if first.Len() == 0 {
		t.Fatal("the first run after an upgrade said nothing")
	}
	if second.Len() != 0 {
		t.Errorf("the second run repeated the notice:\n%s", second.String())
	}
}

func TestAnnounceUpgrade_aVersionWithNoEntrySaysNothingButIsRecorded(t *testing.T) {
	a := upgradedAgent(t, true)
	os.WriteFile(lastVersionFile(a), []byte("0.0.21\n"), 0o644)
	var errOut strings.Builder
	a.announceUpgrade(&errOut, "0.0.24", testNotices[:1])
	if errOut.Len() != 0 {
		t.Errorf("a release with no entry printed:\n%s", errOut.String())
	}
	if b, _ := os.ReadFile(lastVersionFile(a)); strings.TrimSpace(string(b)) != "0.0.24" {
		t.Errorf("last_version = %q, want 0.0.24", b)
	}
}

func TestAnnounceUpgrade_aDowngradeSaysNothingAndKeepsTheRecord(t *testing.T) {
	a := upgradedAgent(t, true)
	os.WriteFile(lastVersionFile(a), []byte("0.0.23\n"), 0o644)
	var errOut strings.Builder
	a.announceUpgrade(&errOut, "0.0.21", testNotices)
	if errOut.Len() != 0 {
		t.Errorf("a downgrade printed:\n%s", errOut.String())
	}
	if b, _ := os.ReadFile(lastVersionFile(a)); strings.TrimSpace(string(b)) != "0.0.23" {
		t.Errorf("last_version = %q, want it left at 0.0.23", b)
	}
}

// Through the real Run: the notice goes to stderr, not to the session's output,
// and the first piped line is still the first line the REPL reads.
func TestRun_theUpgradeNoticeGoesToStderrAndLeavesTheInputAlone(t *testing.T) {
	oldT, oldV := upgradeNoticeTable, runningVersion
	upgradeNoticeTable, runningVersion = testNotices[:1], "0.0.21"
	t.Cleanup(func() { upgradeNoticeTable, runningVersion = oldT, oldV })

	a, ws := runFixture(t, true)
	sess := filepath.Join(ws.HarveyDir(), "sessions")
	os.MkdirAll(sess, 0o755)
	os.WriteFile(filepath.Join(sess, "harvey-session-20261001-100000.jsonl"), []byte("{}\n"), 0o644)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	a.stdin = r
	w.WriteString("n\n/status\n/exit\n") // n answers "Start Ollama now?"
	w.Close()
	var errOut strings.Builder
	a.stderr = &errOut

	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !strings.Contains(errOut.String(), "profiles are gone") {
		t.Errorf("stderr lacks the notice:\n%s", errOut.String())
	}
	if strings.Contains(out.String(), "profiles are gone") {
		t.Errorf("the notice reached the session output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Backend:") {
		t.Errorf("/status did not run as the first piped line:\n%s", out.String())
	}
}
