package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	knowledge "github.com/rsdoiel/knowledge"
)

// H4 (knowledge-learning-mode-plan.md): `/kb learn ingest` offers harvey's own
// session recordings and hand-off notes to the knowledge base as documents.
// RSDOIEL's choice, 2026-09-24: hand-offs are always offered; a session only
// if it has at least 200 words. Real data drove that: 31 of 34 sessions are
// under 200 words (median 24) and none reaches the miner's 10 chat turns.

// learnFixture is a temp workspace with agents/sessions and agents/hand-off,
// with the process working directory set to its root, as `kb` would be run.
type learnFixture struct {
	root string
	kb   *knowledge.KnowledgeBase
	pid  int64
}

func newLearnFixture(t *testing.T) *learnFixture {
	t.Helper()
	a, pid := newTestAgentWithKB(t)
	t.Chdir(a.Workspace.Root)
	return &learnFixture{root: a.Workspace.Root, kb: a.KB, pid: pid}
}

// fountainWithWords returns a small valid Fountain document of about n words.
func fountainWithWords(n int) string {
	return "Title: Test\nAuthor: RSDOIEL\n\nFADE IN:\n\nINT. TEST SCENE\n\n" + strings.Repeat("word ", n) + "\n"
}

func (f *learnFixture) write(t *testing.T, rel, content string) string {
	t.Helper()
	path := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func candidateByName(cs []LearnCandidate, name string) *LearnCandidate {
	for i := range cs {
		if filepath.Base(cs[i].Path) == name {
			return &cs[i]
		}
	}
	return nil
}

func TestLearnCandidates_HandOffsAreAlwaysOffered(t *testing.T) {
	f := newLearnFixture(t)
	f.write(t, "agents/hand-off/tiny.spmd", fountainWithWords(3))
	cs, err := learnCandidates(f.kb, f.root, 200)
	if err != nil {
		t.Fatalf("learnCandidates: %v", err)
	}
	c := candidateByName(cs, "tiny.spmd")
	if c == nil || c.Kind != "hand-off" {
		t.Errorf("candidates = %+v, want the tiny hand-off offered as a hand-off, whatever its size", cs)
	}
}

func TestLearnCandidates_ASessionNeedsAtLeastTheMinimumWords(t *testing.T) {
	f := newLearnFixture(t)
	// The fixture's fixed header adds a known number of words, so build the
	// files around the count learnCandidates itself reports.
	f.write(t, "agents/sessions/probe.spmd", fountainWithWords(0))
	probe, _ := learnCandidates(f.kb, f.root, 0)
	p := candidateByName(probe, "probe.spmd")
	if p == nil {
		t.Fatalf("probe session not offered with the gate off: %+v", probe)
	}
	base := p.Words
	os.Remove(filepath.Join(f.root, "agents/sessions/probe.spmd"))

	f.write(t, "agents/sessions/just-under.spmd", fountainWithWords(200-base-1))
	f.write(t, "agents/sessions/exactly.spmd", fountainWithWords(200-base))
	cs, _ := learnCandidates(f.kb, f.root, 200)
	if candidateByName(cs, "just-under.spmd") != nil {
		t.Error("a session of 199 words was offered, want it hidden by the 200-word gate")
	}
	if c := candidateByName(cs, "exactly.spmd"); c == nil || c.Words != 200 || c.Kind != "session" {
		t.Errorf("exactly.spmd = %+v, want offered at exactly 200 words as a session", c)
	}
}

func TestLearnCandidates_MinWordsZeroOffersEverySession(t *testing.T) {
	f := newLearnFixture(t)
	f.write(t, "agents/sessions/stub.spmd", fountainWithWords(0))
	cs, _ := learnCandidates(f.kb, f.root, 0)
	if candidateByName(cs, "stub.spmd") == nil {
		t.Errorf("candidates = %+v, want the stub offered when the gate is off", cs)
	}
}

func TestLearnCandidates_AnAlreadyIngestedFileIsNotOfferedAgain(t *testing.T) {
	f := newLearnFixture(t)
	f.write(t, "agents/hand-off/done.spmd", fountainWithWords(50))
	f.write(t, "agents/hand-off/todo.spmd", fountainWithWords(50))
	// Ingested the way `kb document ingest agents/hand-off/done.spmd` would
	// from the workspace root: stored relative, as given.
	if _, err := f.kb.IngestDocument(f.pid, "agents/hand-off/done.spmd", knowledge.DocumentIngestOptions{}); err != nil {
		t.Fatalf("IngestDocument: %v", err)
	}
	cs, _ := learnCandidates(f.kb, f.root, 0)
	if candidateByName(cs, "done.spmd") != nil {
		t.Error("done.spmd was offered although it is already in the knowledge base")
	}
	if candidateByName(cs, "todo.spmd") == nil {
		t.Error("todo.spmd was not offered")
	}
}

func TestLearnCandidates_AFileIngestedUnderItsAbsolutePathIsAlsoRecognised(t *testing.T) {
	// Another tool may have stored the absolute form. Offering it again would
	// create a second document for the same file.
	f := newLearnFixture(t)
	abs := f.write(t, "agents/hand-off/abs.spmd", fountainWithWords(50))
	if _, err := f.kb.IngestDocument(f.pid, abs, knowledge.DocumentIngestOptions{}); err != nil {
		t.Fatalf("IngestDocument: %v", err)
	}
	cs, _ := learnCandidates(f.kb, f.root, 0)
	if candidateByName(cs, "abs.spmd") != nil {
		t.Error("abs.spmd was offered although it is already stored under its absolute path")
	}
}

func TestLearnCandidates_StoredPathIsRelativeToTheWorkingDirectoryWithForwardSlashes(t *testing.T) {
	f := newLearnFixture(t)
	f.write(t, "agents/hand-off/p.spmd", fountainWithWords(5))
	f.write(t, "agents/sessions/s.spmd", fountainWithWords(300))
	cs, _ := learnCandidates(f.kb, f.root, 200)
	if c := candidateByName(cs, "p.spmd"); c == nil || c.StoredPath != "agents/hand-off/p.spmd" {
		t.Errorf("hand-off = %+v, want StoredPath agents/hand-off/p.spmd, what `kb document ingest` stores from the root", c)
	}
	if c := candidateByName(cs, "s.spmd"); c == nil || c.StoredPath != "agents/sessions/s.spmd" {
		t.Errorf("session = %+v, want StoredPath agents/sessions/s.spmd", c)
	}
}

func TestLearnCandidates_NewestFirst(t *testing.T) {
	f := newLearnFixture(t)
	old := f.write(t, "agents/hand-off/old.spmd", fountainWithWords(5))
	mid := f.write(t, "agents/sessions/mid.spmd", fountainWithWords(300))
	newest := f.write(t, "agents/hand-off/new.spmd", fountainWithWords(5))
	now := time.Now()
	os.Chtimes(old, now.Add(-48*time.Hour), now.Add(-48*time.Hour))
	os.Chtimes(mid, now.Add(-24*time.Hour), now.Add(-24*time.Hour))
	os.Chtimes(newest, now, now)
	cs, _ := learnCandidates(f.kb, f.root, 200)
	var names []string
	for _, c := range cs {
		names = append(names, filepath.Base(c.Path))
	}
	if strings.Join(names, ",") != "new.spmd,mid.spmd,old.spmd" {
		t.Errorf("order = %v, want newest first across both kinds", names)
	}
}

func TestLearnCandidates_OnlyFountainFilesAreConsidered(t *testing.T) {
	f := newLearnFixture(t)
	f.write(t, "agents/hand-off/notes.md", "# not a hand-off\n")
	f.write(t, "agents/hand-off/a.spmd", fountainWithWords(5))
	f.write(t, "agents/hand-off/b.fountain", fountainWithWords(5))
	os.MkdirAll(filepath.Join(f.root, "agents/hand-off/sub.spmd"), 0o755)
	cs, _ := learnCandidates(f.kb, f.root, 0)
	if len(cs) != 2 {
		t.Errorf("candidates = %+v, want only a.spmd and b.fountain (no .md, no directory)", cs)
	}
}

func TestLearnCandidates_MissingDirectoriesAreNotAnError(t *testing.T) {
	f := newLearnFixture(t)
	cs, err := learnCandidates(f.kb, f.root, 200)
	if err != nil || len(cs) != 0 {
		t.Errorf("got %+v, %v; want no candidates and no error for a workspace with neither directory", cs, err)
	}
}

func TestLearnCandidates_CountsChatTurnsForInformation(t *testing.T) {
	f := newLearnFixture(t)
	body := "Title: S\n\n"
	for i := 0; i < 3; i++ {
		body += "INT. HARVEY AND RSDOIEL TALKING 2026-09-24 10:0" + string(rune('0'+i)) + ":00\n\nHarvey chats.\n\n"
	}
	body += strings.Repeat("word ", 250)
	f.write(t, "agents/sessions/chat.spmd", body)
	cs, _ := learnCandidates(f.kb, f.root, 200)
	if c := candidateByName(cs, "chat.spmd"); c == nil || c.Turns != 3 {
		t.Errorf("chat.spmd = %+v, want 3 chat turns counted", c)
	}
}

// ─── selection parsing ───────────────────────────────────────────────────────

func TestParseSelection(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want []int
	}{
		{"all", 4, []int{1, 2, 3, 4}},
		{"ALL", 2, []int{1, 2}},
		{"", 4, nil},
		{"none", 4, nil},
		{"2", 4, []int{2}},
		{"1,3", 4, []int{1, 3}},
		{"2-4", 5, []int{2, 3, 4}},
		{"1, 3-4", 5, []int{1, 3, 4}},
		{"3,1,3", 5, []int{1, 3}},
		{" 2 ", 3, []int{2}},
	} {
		got, err := parseSelection(tc.in, tc.n)
		if err != nil {
			t.Errorf("parseSelection(%q, %d): %v", tc.in, tc.n, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseSelection(%q, %d) = %v, want %v", tc.in, tc.n, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseSelection(%q, %d) = %v, want %v", tc.in, tc.n, got, tc.want)
			}
		}
	}
}

func TestParseSelection_RejectsWhatItCannotMean(t *testing.T) {
	for _, in := range []string{"0", "5", "-1", "4-2", "1-9", "a", "1,x", "1..3", "2-"} {
		if got, err := parseSelection(in, 4); err == nil {
			t.Errorf("parseSelection(%q, 4) = %v, nil; want an error rather than a guess", in, got)
		}
	}
}

// ─── the /kb learn ingest command ────────────────────────────────────────────

func learnAgent(t *testing.T, input string) (*Agent, *learnFixture) {
	t.Helper()
	a, pid := newTestAgentWithKB(t)
	t.Chdir(a.Workspace.Root)
	a.In = strings.NewReader(input)
	return a, &learnFixture{root: a.Workspace.Root, kb: a.KB, pid: pid}
}

func TestKBLearnIngest_OffersTheCandidatesAndIngestsTheSelection(t *testing.T) {
	a, f := learnAgent(t, "1\n")
	old := f.write(t, "agents/hand-off/older.spmd", fountainWithWords(40))
	f.write(t, "agents/hand-off/newer.spmd", fountainWithWords(40))
	now := time.Now()
	os.Chtimes(old, now.Add(-time.Hour), now.Add(-time.Hour))

	var out strings.Builder
	if err := kbLearn(a, []string{"ingest"}, &out); err != nil {
		t.Fatalf("kbLearn ingest: %v", err)
	}
	for _, want := range []string{"hand-off", "agents/hand-off/newer.spmd", "agents/hand-off/older.spmd", "Ingest which"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to contain %q", out.String(), want)
		}
	}
	if d, _ := a.KB.DocumentByPath("agents/hand-off/newer.spmd"); d == nil || d.ProjectID != f.pid {
		t.Errorf("newer.spmd (candidate 1) = %+v, want ingested into the current project", d)
	}
	if d, _ := a.KB.DocumentByPath("agents/hand-off/older.spmd"); d != nil {
		t.Error("older.spmd was ingested although only candidate 1 was chosen")
	}
}

func TestKBLearnIngest_APromptAnswerOfNothingIngestsNothing(t *testing.T) {
	a, f := learnAgent(t, "\n")
	f.write(t, "agents/hand-off/a.spmd", fountainWithWords(40))
	var out strings.Builder
	if err := kbLearn(a, []string{"ingest"}, &out); err != nil {
		t.Fatalf("kbLearn ingest: %v", err)
	}
	if d, _ := a.KB.DocumentByPath("agents/hand-off/a.spmd"); d != nil {
		t.Error("a file was ingested after an empty answer")
	}
}

func TestKBLearnIngest_AllFlagIngestsEverythingWithoutPrompting(t *testing.T) {
	a, f := learnAgent(t, "") // no input available: any prompt would get EOF
	f.write(t, "agents/hand-off/a.spmd", fountainWithWords(40))
	f.write(t, "agents/hand-off/b.spmd", fountainWithWords(40))
	var out strings.Builder
	if err := kbLearn(a, []string{"ingest", "--all"}, &out); err != nil {
		t.Fatalf("kbLearn ingest --all: %v", err)
	}
	for _, p := range []string{"agents/hand-off/a.spmd", "agents/hand-off/b.spmd"} {
		if d, _ := a.KB.DocumentByPath(p); d == nil {
			t.Errorf("%s was not ingested by --all", p)
		}
	}
}

func TestKBLearnIngest_DryRunListsAndChangesNothing(t *testing.T) {
	a, f := learnAgent(t, "all\n")
	f.write(t, "agents/hand-off/a.spmd", fountainWithWords(40))
	var out strings.Builder
	if err := kbLearn(a, []string{"ingest", "--dry-run"}, &out); err != nil {
		t.Fatalf("kbLearn ingest --dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "agents/hand-off/a.spmd") {
		t.Errorf("output = %q, want the candidate listed", out.String())
	}
	if d, _ := a.KB.DocumentByPath("agents/hand-off/a.spmd"); d != nil {
		t.Error("a dry run ingested a file")
	}
}

func TestKBLearnIngest_MinWordsFlagOverridesTheSessionGate(t *testing.T) {
	a, f := learnAgent(t, "")
	f.write(t, "agents/sessions/small.spmd", fountainWithWords(20))
	var hidden, shown strings.Builder
	kbLearn(a, []string{"ingest", "--dry-run"}, &hidden)
	kbLearn(a, []string{"ingest", "--dry-run", "--min-words", "10"}, &shown)
	if strings.Contains(hidden.String(), "small.spmd") {
		t.Error("a 20-word session was offered under the default 200-word gate")
	}
	if !strings.Contains(shown.String(), "small.spmd") {
		t.Errorf("output = %q, want the session offered with --min-words 10", shown.String())
	}
}

func TestKBLearnIngest_NothingToOfferSaysSo(t *testing.T) {
	a, _ := learnAgent(t, "")
	var out strings.Builder
	if err := kbLearn(a, []string{"ingest"}, &out); err != nil {
		t.Fatalf("kbLearn ingest: %v", err)
	}
	if !strings.Contains(strings.ToLower(out.String()), "nothing") {
		t.Errorf("output = %q, want a message that there is nothing to ingest", out.String())
	}
}

func TestKBLearnIngest_RequiresACurrentProject(t *testing.T) {
	a, f := learnAgent(t, "all\n")
	a.Config.Memory.CurrentProjectID = 0
	f.write(t, "agents/hand-off/a.spmd", fountainWithWords(40))
	var out strings.Builder
	if err := kbLearn(a, []string{"ingest", "--all"}, &out); err != nil {
		t.Fatalf("kbLearn ingest: %v", err)
	}
	if !strings.Contains(out.String(), "/kb project use") {
		t.Errorf("output = %q, want it to say how to pick a project", out.String())
	}
	if d, _ := a.KB.DocumentByPath("agents/hand-off/a.spmd"); d != nil {
		t.Error("a file was ingested with no current project")
	}
}

func TestKBLearnIngest_OneBadFileDoesNotStopTheOthers(t *testing.T) {
	a, f := learnAgent(t, "all\n")
	bad := f.write(t, "agents/hand-off/aaa-bad.spmd", fountainWithWords(40))
	f.write(t, "agents/hand-off/zzz-good.spmd", fountainWithWords(40))
	// The candidate list is built before ingest; make the first one unreadable
	// only afterwards, by racing nothing: remove it once it has been listed.
	a.In = &removeOnRead{path: bad, then: strings.NewReader("all\n")}
	var out strings.Builder
	if err := kbLearn(a, []string{"ingest"}, &out); err != nil {
		t.Fatalf("kbLearn ingest: %v", err)
	}
	if d, _ := a.KB.DocumentByPath("agents/hand-off/zzz-good.spmd"); d == nil {
		t.Errorf("the good file was not ingested after the bad one failed; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "failed") {
		t.Errorf("output = %q, want the failure reported", out.String())
	}
}

// removeOnRead deletes path the first time it is read, which is the moment the
// user answers the prompt: after the candidate list was built, before ingest.
type removeOnRead struct {
	path string
	then *strings.Reader
	done bool
}

func (r *removeOnRead) Read(p []byte) (int, error) {
	if !r.done {
		os.Remove(r.path)
		r.done = true
	}
	return r.then.Read(p)
}

func TestKBLearn_UnknownSubcommandGivesUsage(t *testing.T) {
	a, _ := learnAgent(t, "")
	var out strings.Builder
	if err := kbLearn(a, []string{"nonesuch"}, &out); err != nil {
		t.Fatalf("kbLearn: %v", err)
	}
	if !strings.Contains(out.String(), "Usage") || !strings.Contains(out.String(), "ingest") {
		t.Errorf("output = %q, want a usage line naming ingest", out.String())
	}
}

func TestCmdKB_RoutesLearnAndListsItInTheUsage(t *testing.T) {
	a, _ := learnAgent(t, "")
	var out strings.Builder
	if err := cmdKB(a, []string{"learn", "ingest", "--dry-run"}, &out); err != nil {
		t.Fatalf("cmdKB learn: %v", err)
	}
	if strings.Contains(out.String(), "Unknown kb subcommand") {
		t.Errorf("output = %q, want /kb learn routed", out.String())
	}
	var usage strings.Builder
	cmdKB(a, []string{"nonesuch"}, &usage)
	if !strings.Contains(usage.String(), "learn") {
		t.Errorf("usage = %q, want it to list learn", usage.String())
	}
}
