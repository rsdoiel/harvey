package harvey

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// D5 of the observation-drafting plan: resolve the project, read what kb holds,
// and write drafts through the installed kb. These run against a scratch
// database and are skipped when kb is missing or older than kbMinVersion.

func scratchKB(t *testing.T, wsName string, projects ...string) (root, db string) {
	t.Helper()
	if _, err := lookKB(); err != nil {
		t.Skipf("no usable kb: %v", err)
	}
	root = filepath.Join(t.TempDir(), wsName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	db = filepath.Join(root, "k.db")
	for _, p := range projects {
		scratchRun(t, db, "project", "add", p, p+" project")
	}
	return root, db
}

func scratchRun(t *testing.T, db string, args ...string) string {
	t.Helper()
	out, err := exec.Command("kb", append([]string{"-db", db}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("kb %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestDraftBy(t *testing.T) {
	cases := map[string]string{
		"ollama (qwen2.5:3b)": "harvey qwen2.5:3b",
		"hailo (llama3.2:1b)": "harvey llama3.2:1b",
		"granite":             "harvey granite",
		"":                    "harvey",
	}
	for in, want := range cases {
		if got := DraftBy(in); got != want {
			t.Errorf("DraftBy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveDraftProject_Order(t *testing.T) {
	root, db := scratchKB(t, "alpha", "alpha", "beta", "gamma")
	cases := []struct{ name, flag, configured, want string }{
		{"flag", "beta", "", "beta"},
		{"flag over config", "beta", "gamma", "beta"},
		{"config", "", "gamma", "gamma"},
		{"directory name", "", "", "alpha"},
		{"config over directory", "", "beta", "beta"},
	}
	for _, c := range cases {
		got, err := ResolveDraftProject(root, db, c.flag, c.configured)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestResolveDraftProject_NothingToGoOnStopsAndListsTheProjects(t *testing.T) {
	root, db := scratchKB(t, "Laboratory", "alpha", "beta")
	_, err := ResolveDraftProject(root, db, "", "")
	if err == nil || ExitCodeFor(err) != ClassUsage {
		t.Fatalf("want a usage error (exit 2), got %v", err)
	}
	for _, want := range []string{"alpha", "beta", "--project"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

func TestResolveDraftProject_AnUnknownProjectIsExit1AndListsTheProjects(t *testing.T) {
	root, db := scratchKB(t, "ws", "alpha")
	for _, c := range []struct{ flag, configured string }{{"nosuch", ""}, {"", "nosuch"}} {
		_, err := ResolveDraftProject(root, db, c.flag, c.configured)
		if err == nil || ExitCodeFor(err) != ClassNegative || !strings.Contains(err.Error(), "nosuch") || !strings.Contains(err.Error(), "alpha") {
			t.Errorf("%+v: got %v, class %v", c, err, ExitCodeFor(err))
		}
	}
}

func TestExistingObservationBodies_EveryStatus(t *testing.T) {
	root, db := scratchKB(t, "ws", "demo")
	scratchRun(t, db, "observation", "add", "--project", "demo", "finding", "a reviewed finding")
	scratchRun(t, db, "observation", "add", "--project", "demo", "--draft", "--by", "harvey test", "note", "a drafted note")
	got, err := ExistingObservationBodies(root, db, "demo")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v; want both the reviewed and the drafted body", got, err)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "a reviewed finding") || !strings.Contains(joined, "a drafted note") {
		t.Errorf("got %v", got)
	}
	if _, err := ExistingObservationBodies(root, db, "nosuch"); err == nil || ExitCodeFor(err) != ClassNegative {
		t.Errorf("an unknown project: %v", err)
	}
}

func TestWriteDraft_WritesADraftThatIsHiddenUntilPromoted(t *testing.T) {
	root, db := scratchKB(t, "ws", "demo")
	body := "-a body that starts with a dash\nDrafted from s#0001..#0009."
	id, err := WriteDraft(root, db, "demo", "harvey qwen2.5:3b", Proposal{Kind: "finding", Body: body})
	if err != nil || id == 0 {
		t.Fatalf("WriteDraft = %d, %v", id, err)
	}
	// kb search exits 1 on no results, which is what a hidden draft gives.
	found, _ := exec.Command("kb", "-db", db, "search", "dash").CombinedOutput()
	if strings.Contains(string(found), "starts with a dash") {
		t.Errorf("the draft is searchable: %s", found)
	}
	queue := scratchRun(t, db, "observation", "review", "list")
	if !strings.Contains(queue, "harvey qwen2.5:3b") || !strings.Contains(queue, "starts with a dash") {
		t.Errorf("review list = %s", queue)
	}
	show := scratchRun(t, db, "observation", "show", "1")
	if !strings.Contains(show, "Drafted from s#0001..#0009.") || !strings.Contains(show, "drafted") {
		t.Errorf("show = %s", show)
	}
}

func TestWriteDraft_AnUnknownProjectIsExit1AndWritesNothing(t *testing.T) {
	root, db := scratchKB(t, "ws", "demo")
	_, err := WriteDraft(root, db, "nosuch", "harvey x", Proposal{Kind: "note", Body: "text"})
	if err == nil || ExitCodeFor(err) != ClassNegative {
		t.Fatalf("got %v", err)
	}
	if out := scratchRun(t, db, "observation", "review", "list"); strings.Contains(out, "text") {
		t.Errorf("something was written: %s", out)
	}
}

func TestWriteDraft_ARefusedByOrKindIsNotRetriedAndIsAnError(t *testing.T) {
	root, db := scratchKB(t, "ws", "demo")
	if _, err := WriteDraft(root, db, "demo", " ", Proposal{Kind: "note", Body: "text"}); err == nil || ExitCodeFor(err) != ClassUsage {
		t.Errorf("a blank --by: %v", err)
	}
	if _, err := WriteDraft(root, db, "demo", "harvey x", Proposal{Kind: "bogus", Body: "text"}); err == nil || ExitCodeFor(err) != ClassUsage {
		t.Errorf("a bad kind: %v", err)
	}
}

// A kb that predates drafts is not run at all: the version check comes first.
func TestDraftWrite_AnOldKBIsUnavailableAndNothingRuns(t *testing.T) {
	dir := fakeKBWithVersion(t, "0.0.29")
	for name, call := range map[string]func() error{
		"projects": func() error { _, e := KBProjectNames("/tmp", "/tmp/x.db"); return e },
		"existing": func() error { _, e := ExistingObservationBodies("/tmp", "/tmp/x.db", "p"); return e },
		"write": func() error {
			_, e := WriteDraft("/tmp", "/tmp/x.db", "p", "harvey x", Proposal{Kind: "note", Body: "t"})
			return e
		},
	} {
		if err := call(); err == nil || ExitCodeFor(err) != ClassUnavailable {
			t.Errorf("%s: got %v, want unavailable (69)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "calls")); err == nil {
		t.Error("an unsupported kb was run")
	}
}

// With a fake kb that records its arguments: every write is a draft, signed,
// and nothing ever asks kb to promote.
func TestDraftWrite_EveryCallIsADraftAndNothingPromotes(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nd=$(dirname \"$0\")\n" +
		"if [ \"$1\" = \"-version\" ]; then echo 'kb " + kbMinVersion + " abc1234'; exit 0; fi\n" +
		"echo \"$@\" >> \"$d/calls\"\n" +
		"case \"$*\" in *\"observation add\"*) echo '{\"id\": 7, \"status\": \"drafted\"}';; *) echo '[]';; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "kb"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := KBProjectNames(dir, filepath.Join(dir, "k.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := ExistingObservationBodies(dir, filepath.Join(dir, "k.db"), "demo"); err != nil {
		t.Fatal(err)
	}
	id, err := WriteDraft(dir, filepath.Join(dir, "k.db"), "demo", "harvey x", Proposal{Kind: "finding", Body: "text"})
	if err != nil || id != 7 {
		t.Fatalf("WriteDraft = %d, %v", id, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "calls"))
	calls := string(raw)
	if strings.Contains(calls, "promote") {
		t.Errorf("harvey asked kb to promote:\n%s", calls)
	}
	var adds int
	for _, line := range strings.Split(strings.TrimSpace(calls), "\n") {
		if strings.Contains(line, "observation add") {
			adds++
			if !strings.Contains(line, "--draft") || !strings.Contains(line, "--by harvey x") {
				t.Errorf("an add that is not a signed draft: %s", line)
			}
		}
	}
	if adds != 1 {
		t.Errorf("%d adds, want 1:\n%s", adds, calls)
	}
}

// A kb that answers with something other than the id is a data error, not a
// silent zero.
func TestWriteDraft_UnreadableOutputFromKBIsADataError(t *testing.T) {
	for _, reply := range []string{"wrote it", `{"status": "drafted"}`, `{"id": 0}`} {
		dir := t.TempDir()
		script := "#!/bin/sh\nif [ \"$1\" = \"-version\" ]; then echo 'kb " + kbMinVersion + " abc1234'; exit 0; fi\n" +
			"cat <<'EOF'\n" + reply + "\nEOF\n"
		if err := os.WriteFile(filepath.Join(dir, "kb"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		id, err := WriteDraft(dir, filepath.Join(dir, "k.db"), "demo", "harvey x", Proposal{Kind: "note", Body: "t"})
		if err == nil || ExitCodeFor(err) != ClassData || id != 0 {
			t.Errorf("%q: got %d, %v (class %v), want a data error", reply, id, err, ExitCodeFor(err))
		}
	}
}
