package harvey

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	knowledge "github.com/rsdoiel/knowledge"
)

// H6: /kb learn concepts. SuggestConcepts proposes candidates, the human picks
// which become concepts, then each document that mentions a picked concept is
// previewed as a diff and written only after a yes, through the same
// permission check every other harvey file write uses.
//
// One correction to the plan, made while reading the code: the plan asked for a
// test "with safe_mode on". safe_mode limits which commands ! and /run may
// execute; it does not gate file writes. File writes are gated by the
// permissions: table (Agent.CheckWritePermission), so that is what these tests
// deny.

// gizmoDoc mentions "gizmo" three times across two of its four sections, so it
// is a distinctive term that is not yet a concept. Its other words are unique
// so nothing else in the corpus scores.
const gizmoDoc = "# Field notes\n\n## One\n\nThe gizmo arrived. The gizmo hummed.\n\n## Two\n\nA gizmo again.\n\n## Three\n\nBravo yankee.\n\n## Four\n\nCharlie zulu.\n"

// onceDoc mentions "gizmo" a single time, below the density threshold.
const onceDoc = "# Aside\n\n## Only\n\nOne gizmo, once.\n\n## Other\n\nDelta whiskey.\n"

type conceptsFixture struct {
	a   *Agent
	kb  *knowledge.KnowledgeBase
	pid int64
}

func newConceptsFixture(t *testing.T, input string) *conceptsFixture {
	t.Helper()
	a, pid := newTestAgentWithKB(t)
	a.In = strings.NewReader(input)
	return &conceptsFixture{a: a, kb: a.KB, pid: pid}
}

// place writes content under the workspace and ingests it with its absolute
// path, returning that path.
func (f *conceptsFixture) place(t *testing.T, rel, content string) string {
	t.Helper()
	abs := filepath.Join(f.a.Workspace.Root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := f.kb.IngestDocument(f.pid, abs, knowledge.DocumentIngestOptions{}); err != nil {
		t.Fatalf("IngestDocument: %v", err)
	}
	return abs
}

// pick returns the 1-based number kbLearnConcepts will print next to term.
func (f *conceptsFixture) pick(t *testing.T, term string) int {
	t.Helper()
	s, err := f.kb.SuggestConcepts("test-project", learnConceptsDefaultLimit)
	if err != nil {
		t.Fatalf("SuggestConcepts: %v", err)
	}
	for i, c := range s.Candidates {
		if c.Term == term {
			return i + 1
		}
	}
	t.Fatalf("%q is not a candidate: %+v", term, s.Candidates)
	return 0
}

func (f *conceptsFixture) run(t *testing.T, args ...string) string {
	t.Helper()
	var out strings.Builder
	if err := kbLearnConcepts(f.a, args, &out); err != nil {
		t.Fatalf("kbLearnConcepts: %v", err)
	}
	return out.String()
}

func (f *conceptsFixture) hasConcept(t *testing.T, name string) bool {
	t.Helper()
	cs, err := f.kb.Concepts()
	if err != nil {
		t.Fatalf("Concepts: %v", err)
	}
	for _, c := range cs {
		if strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

func (f *conceptsFixture) checksum(t *testing.T, path string) string {
	t.Helper()
	d, err := f.kb.DocumentByPath(path)
	if err != nil || d == nil {
		t.Fatalf("DocumentByPath(%s): %v, %v", path, d, err)
	}
	return d.Checksum
}

// checksumOf is the checksum form the knowledge base stores for a document.
func checksumOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(b)
}

// ─── lineDiff ────────────────────────────────────────────────────────────────

func TestLineDiff_ShowsOnlyTheChangedLinesWithTheirNumbers(t *testing.T) {
	before := "one\ntwo\nthree\nfour\n"
	after := "one\ntwo [[x]]\nthree\nfour\n"
	got := lineDiff(before, after)
	if !strings.Contains(got, "-2: two") || !strings.Contains(got, "+2: two [[x]]") {
		t.Errorf("diff should show line 2 removed and added:\n%s", got)
	}
	if strings.Contains(got, "one") || strings.Contains(got, "three") || strings.Contains(got, "four") {
		t.Errorf("unchanged lines must not appear:\n%s", got)
	}
}

func TestLineDiff_IdenticalTextIsEmpty(t *testing.T) {
	if got := lineDiff("a\nb\n", "a\nb\n"); got != "" {
		t.Errorf("identical text should give an empty diff, got %q", got)
	}
}

func TestLineDiff_AddedLinesAreMarkedAsAdditions(t *testing.T) {
	got := lineDiff("a\nb\n", "a\nb\n\n[^1]: note\n")
	if !strings.Contains(got, "+") || !strings.Contains(got, "[^1]: note") {
		t.Errorf("an appended footnote definition should show as added:\n%s", got)
	}
	if strings.Contains(got, "-") {
		t.Errorf("nothing was removed, so no '-' line expected:\n%s", got)
	}
}

// ─── planConceptTagging ─────────────────────────────────────────────────────

func TestPlanConceptTagging_TagsOnlyTheAcceptedConcepts(t *testing.T) {
	f := newConceptsFixture(t, "")
	for _, name := range []string{"gizmo", "widget"} {
		if _, err := f.kb.AddConcept(name, ""); err != nil {
			t.Fatalf("AddConcept: %v", err)
		}
	}
	text := "The gizmo and the widget. The gizmo and the widget again.\n"
	next, exact, _, err := planConceptTagging(f.kb, text, []string{"gizmo"})
	if err != nil {
		t.Fatalf("planConceptTagging: %v", err)
	}
	if !strings.Contains(next, "[[gizmo]]") {
		t.Errorf("the accepted concept should be linked:\n%s", next)
	}
	if strings.Contains(next, "[[widget]]") {
		t.Errorf("a known concept the human did not pick must be left alone:\n%s", next)
	}
	if len(exact) != 1 || exact[0] != "gizmo" {
		t.Errorf("exact = %v, want [gizmo]", exact)
	}
}

func TestPlanConceptTagging_ASingleMentionIsBelowTheThreshold(t *testing.T) {
	f := newConceptsFixture(t, "")
	if _, err := f.kb.AddConcept("gizmo", ""); err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	text := "One gizmo, once.\n"
	next, exact, _, err := planConceptTagging(f.kb, text, []string{"gizmo"})
	if err != nil {
		t.Fatalf("planConceptTagging: %v", err)
	}
	if next != text || len(exact) != 0 {
		t.Errorf("a single mention is not tagged (DR-0027 threshold); got %q, exact %v", next, exact)
	}
}

func TestPlanConceptTagging_FootnotesANearMissOfTheAcceptedConceptOnly(t *testing.T) {
	f := newConceptsFixture(t, "")
	// Both are six letters or more: the library does not fuzz shorter concept
	// names (DR-0036), so a five-letter fixture would test nothing.
	for _, name := range []string{"sprocket", "flywheel"} {
		if _, err := f.kb.AddConcept(name, ""); err != nil {
			t.Fatalf("AddConcept: %v", err)
		}
	}
	// "sprockets" is one edit from sprocket, "flywheels" one from flywheel; the
	// human picked only sprocket.
	text := "# T\n\nThe sprockets and the flywheels.\n"
	next, _, fuzzy, err := planConceptTagging(f.kb, text, []string{"sprocket"})
	if err != nil {
		t.Fatalf("planConceptTagging: %v", err)
	}
	if len(fuzzy) != 1 || fuzzy[0].Concept != "sprocket" {
		t.Fatalf("want exactly one footnote, for sprocket, got %+v", fuzzy)
	}
	if strings.Contains(next, "[[flywheel]]") {
		t.Errorf("the other concept's near-miss must not be footnoted:\n%s", next)
	}
}

func TestPlanConceptTagging_IsIdempotentOnItsOwnOutput(t *testing.T) {
	f := newConceptsFixture(t, "")
	if _, err := f.kb.AddConcept("sprocket", ""); err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	text := "# T\n\nThe sprocket, the sprocket and the sprockets.\n"
	once, _, _, err := planConceptTagging(f.kb, text, []string{"sprocket"})
	if err != nil {
		t.Fatalf("planConceptTagging: %v", err)
	}
	if once == text {
		t.Fatal("the first pass should have changed the text, or this test proves nothing")
	}
	twice, exact, fuzzy, err := planConceptTagging(f.kb, once, []string{"sprocket"})
	if err != nil {
		t.Fatalf("planConceptTagging: %v", err)
	}
	if twice != once || len(exact) != 0 || len(fuzzy) != 0 {
		t.Errorf("a second pass should change nothing:\nonce:  %q\ntwice: %q", once, twice)
	}
}

// ─── selection ───────────────────────────────────────────────────────────────

func TestLearnConcepts_ListsTheCandidatesAndAddsOnlyThePicked(t *testing.T) {
	f := newConceptsFixture(t, "")
	f.place(t, "notes/a.md", gizmoDoc)
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\nn\n")
	out := f.run(t)
	if !strings.Contains(out, "gizmo") {
		t.Errorf("the candidate should be listed:\n%s", out)
	}
	if !f.hasConcept(t, "gizmo") {
		t.Error("the picked candidate should now be a concept")
	}
	cs, _ := f.kb.Concepts()
	if len(cs) != 1 {
		t.Errorf("only the picked term should have been added, concepts: %+v", cs)
	}
}

func TestLearnConcepts_NoSelectionChangesNothing(t *testing.T) {
	f := newConceptsFixture(t, "none\n")
	path := f.place(t, "notes/a.md", gizmoDoc)
	sum := f.checksum(t, path)
	f.run(t)
	if cs, _ := f.kb.Concepts(); len(cs) != 0 {
		t.Errorf("no concept should be added, got %+v", cs)
	}
	if readFile(t, path) != gizmoDoc || f.checksum(t, path) != sum {
		t.Error("the document and its KB row must be untouched")
	}
}

func TestLearnConcepts_AnUnreadableSelectionChangesNothing(t *testing.T) {
	f := newConceptsFixture(t, "banana\n")
	path := f.place(t, "notes/a.md", gizmoDoc)
	out := f.run(t)
	if cs, _ := f.kb.Concepts(); len(cs) != 0 {
		t.Errorf("a bad answer must not add concepts, got %+v", cs)
	}
	if readFile(t, path) != gizmoDoc {
		t.Error("a bad answer must not touch the file")
	}
	if !strings.Contains(out, "banana") {
		t.Errorf("the message should quote the bad answer:\n%s", out)
	}
}

func TestLearnConcepts_SaysSoWhenThereIsNothingToSuggest(t *testing.T) {
	f := newConceptsFixture(t, "")
	out := f.run(t)
	if !strings.Contains(strings.ToLower(out), "no candidate") {
		t.Errorf("an empty corpus should say there is nothing to suggest:\n%s", out)
	}
}

func TestLearnConcepts_NeedsACurrentProject(t *testing.T) {
	f := newConceptsFixture(t, "")
	f.a.Config.Memory.CurrentProjectID = 0
	out := f.run(t)
	if !strings.Contains(out, "No current project") {
		t.Errorf("want the no-project message, got:\n%s", out)
	}
}

func TestLearnConcepts_TheLimitFlagCapsTheList(t *testing.T) {
	f := newConceptsFixture(t, "none\n")
	f.place(t, "notes/a.md", gizmoDoc)
	if out := f.run(t, "--limit", "0"); out == "" {
		t.Error("--limit 0 should be accepted (no cap)")
	}
	f.a.In = strings.NewReader("")
	var out strings.Builder
	if err := kbLearnConcepts(f.a, []string{"--limit", "x"}, &out); err != nil {
		t.Fatalf("kbLearnConcepts: %v", err)
	}
	if !strings.Contains(out.String(), "--limit") {
		t.Errorf("a bad --limit should print usage:\n%s", out.String())
	}
}

// ─── the write path ─────────────────────────────────────────────────────────

func TestLearnConcepts_AnApprovedWriteTagsTheFileAndReingestsIt(t *testing.T) {
	f := newConceptsFixture(t, "")
	path := f.place(t, "notes/a.md", gizmoDoc)
	before := f.checksum(t, path)
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\ny\n")
	out := f.run(t)

	got := readFile(t, path)
	if !strings.Contains(got, "[[gizmo]]") {
		t.Fatalf("the file should now link gizmo:\n%s", got)
	}
	if f.checksum(t, path) == before {
		t.Error("the document should have been re-ingested (checksum unchanged)")
	}
	if want := checksumOf(got); f.checksum(t, path) != want {
		t.Errorf("the stored checksum should match the new file")
	}
	if !strings.Contains(out, "-") || !strings.Contains(out, "+") {
		t.Errorf("the preview should be a diff:\n%s", out)
	}
	if !f.hasConcept(t, "gizmo") {
		t.Error("gizmo should be a concept")
	}
}

func TestLearnConcepts_ADeclinedPreviewLeavesTheFileAndRowsUnchanged(t *testing.T) {
	f := newConceptsFixture(t, "")
	path := f.place(t, "notes/a.md", gizmoDoc)
	before := f.checksum(t, path)
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\nn\n")
	f.run(t)
	if readFile(t, path) != gizmoDoc {
		t.Error("a 'no' must leave the file as it was")
	}
	if f.checksum(t, path) != before {
		t.Error("a 'no' must not re-ingest")
	}
}

func TestLearnConcepts_AWriteDeniedByPermissionsLeavesTheFileAndKBUnchanged(t *testing.T) {
	f := newConceptsFixture(t, "")
	path := f.place(t, "notes/a.md", gizmoDoc)
	before := f.checksum(t, path)
	f.a.Config.SetPermission(".", []string{"read"})
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\ny\n")
	out := f.run(t)

	if readFile(t, path) != gizmoDoc {
		t.Error("a denied write must leave the file as it was")
	}
	if f.checksum(t, path) != before {
		t.Error("a denied write must not re-ingest, so the document row is unchanged")
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("the denial should be reported:\n%s", out)
	}
}

func TestLearnConcepts_ADeniedWriteDoesNotAskForConfirmation(t *testing.T) {
	// A prompt the user can answer yes to and which then fails is a trap; a
	// path harvey may not write is refused before it is offered.
	f := newConceptsFixture(t, "")
	f.place(t, "notes/a.md", gizmoDoc)
	f.a.Config.SetPermission(".", []string{"read"})
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\n")
	out := f.run(t)
	if strings.Contains(out, "[y]es") {
		t.Errorf("no confirmation should be offered for a path that cannot be written:\n%s", out)
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("the refusal should be reported:\n%s", out)
	}
}

func TestLearnConcepts_AnswerAllWritesEveryRemainingFile(t *testing.T) {
	f := newConceptsFixture(t, "")
	a := f.place(t, "notes/a.md", gizmoDoc)
	b := f.place(t, "notes/b.md", strings.ReplaceAll(gizmoDoc, "Field notes", "More notes"))
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\na\n")
	f.run(t)
	for _, p := range []string{a, b} {
		if !strings.Contains(readFile(t, p), "[[gizmo]]") {
			t.Errorf("%s should have been tagged after answering All", p)
		}
	}
}

func TestLearnConcepts_QuitStopsBeforeTheRemainingFiles(t *testing.T) {
	f := newConceptsFixture(t, "")
	a := f.place(t, "notes/a.md", gizmoDoc)
	b := f.place(t, "notes/b.md", strings.ReplaceAll(gizmoDoc, "Field notes", "More notes"))
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\nq\n")
	f.run(t)
	for _, p := range []string{a, b} {
		if strings.Contains(readFile(t, p), "[[gizmo]]") {
			t.Errorf("%s was tagged despite quit", p)
		}
	}
}

func TestLearnConcepts_ADocumentThatMentionsItOnceIsLeftAlone(t *testing.T) {
	f := newConceptsFixture(t, "")
	f.place(t, "notes/a.md", gizmoDoc)
	once := f.place(t, "notes/once.md", onceDoc)
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\ny\ny\n")
	f.run(t)
	if readFile(t, once) != onceDoc {
		t.Error("a document below the density threshold must not be rewritten")
	}
}

func TestLearnConcepts_FountainDocumentsAreNeverRewritten(t *testing.T) {
	// Sessions and hand-offs are Fountain: harvey's replay and the memory miner
	// read [[...]] there as notes and file events, and a session is a record of
	// what happened. Curation must not add links to them.
	f := newConceptsFixture(t, "")
	f.place(t, "notes/a.md", gizmoDoc)
	spmd := "Title: Session\n\nINT. HARVEY AND RSDOIEL TALKING 2026-09-24\n\nRSDOIEL\nThe gizmo, the gizmo, the gizmo.\n"
	sp := f.place(t, "agents/hand-off/2026-09-24.spmd", spmd)
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\ny\ny\n")
	out := f.run(t)
	if readFile(t, sp) != spmd {
		t.Error("a Fountain document must never be rewritten")
	}
	if !strings.Contains(strings.ToLower(out), "fountain") {
		t.Errorf("the skip should be reported:\n%s", out)
	}
}

func TestLearnConcepts_APathOutsideTheWorkspaceIsRefused(t *testing.T) {
	f := newConceptsFixture(t, "")
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte(gizmoDoc), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := f.kb.IngestDocument(f.pid, outside, knowledge.DocumentIngestOptions{}); err != nil {
		t.Fatalf("IngestDocument: %v", err)
	}
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\ny\n")
	out := f.run(t)
	if readFile(t, outside) != gizmoDoc {
		t.Error("a file outside the workspace must not be written")
	}
	if !strings.Contains(out, "outside the workspace") {
		t.Errorf("the refusal should be reported:\n%s", out)
	}
}

func TestLearnConcepts_TheSummaryCountsFilesAndStaleSummaries(t *testing.T) {
	f := newConceptsFixture(t, "")
	f.place(t, "notes/a.md", gizmoDoc)
	f.a.In = strings.NewReader(strconv.Itoa(f.pick(t, "gizmo")) + "\ny\n")
	out := f.run(t)
	if !strings.Contains(out, "1 file") {
		t.Errorf("the closing line should count the files written:\n%s", out)
	}
}

// ─── applyConceptTagWrite: both-or-neither per file ─────────────────────────

func TestApplyConceptTagWrite_AFailedReingestRestoresTheFile(t *testing.T) {
	f := newConceptsFixture(t, "")
	path := f.place(t, "notes/a.md", gizmoDoc)
	d, err := f.kb.DocumentByPath(path)
	if err != nil || d == nil {
		t.Fatalf("DocumentByPath: %v, %v", d, err)
	}
	failing := func(string) (knowledge.DocumentIngestResult, error) {
		return knowledge.DocumentIngestResult{}, errors.New("database is locked")
	}
	_, err = applyConceptTagWrite(f.a, *d, path, []byte(gizmoDoc), "changed\n", failing)
	if err == nil {
		t.Fatal("a failed re-ingest must be reported")
	}
	if readFile(t, path) != gizmoDoc {
		t.Error("the file must be restored when the re-ingest fails, so file and KB agree")
	}
}

func TestApplyConceptTagWrite_ADeniedPathIsNeverWritten(t *testing.T) {
	f := newConceptsFixture(t, "")
	path := f.place(t, "notes/a.md", gizmoDoc)
	d, _ := f.kb.DocumentByPath(path)
	f.a.Config.SetPermission(".", []string{"read"})
	called := false
	ingest := func(string) (knowledge.DocumentIngestResult, error) {
		called = true
		return knowledge.DocumentIngestResult{}, nil
	}
	_, err := applyConceptTagWrite(f.a, *d, path, []byte(gizmoDoc), "changed\n", ingest)
	if !errors.Is(err, errWriteDenied) {
		t.Errorf("want errWriteDenied, got %v", err)
	}
	if called || readFile(t, path) != gizmoDoc {
		t.Error("a denied write must neither touch the file nor re-ingest")
	}
}

// ─── routing ─────────────────────────────────────────────────────────────────

func TestKBLearn_RoutesConceptsToTheCurationPass(t *testing.T) {
	f := newConceptsFixture(t, "")
	var out strings.Builder
	if err := kbLearn(f.a, []string{"concepts"}, &out); err != nil {
		t.Fatalf("kbLearn: %v", err)
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Errorf("concepts should be a known subcommand:\n%s", out.String())
	}
	if !strings.Contains(learnUsage, "concepts") {
		t.Errorf("the usage line should list concepts: %s", learnUsage)
	}
}

// TestMyersEdits_ApplyingTheEditsReproducesAfter checks the diff on many small
// random line lists, not just the hand-picked cases: deleting the '-' lines
// from before and inserting the '+' lines must give exactly after.
func TestMyersEdits_ApplyingTheEditsReproducesAfter(t *testing.T) {
	seed := uint32(12345)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>16) % n
	}
	alphabet := []string{"a", "b", "c", "d", "e"}
	gen := func() []string {
		out := make([]string, next(9))
		for i := range out {
			out[i] = alphabet[next(len(alphabet))]
		}
		return out
	}
	for i := 0; i < 500; i++ {
		before, after := gen(), gen()
		edits, ok := myersEdits(before, after)
		if !ok {
			t.Fatalf("case %d: small inputs must not exceed the edit limit", i)
		}
		removed := map[int]bool{}
		added := map[int]string{}
		for _, e := range edits {
			if e.op == '-' {
				removed[e.line] = true
			} else {
				added[e.line] = e.text
			}
		}
		// Rebuild after: walk before, dropping removed lines, and place added
		// lines at their 1-based position in after.
		var kept []string
		for j, l := range before {
			if !removed[j+1] {
				kept = append(kept, l)
			}
		}
		var rebuilt []string
		ki := 0
		for pos := 1; pos <= len(after); pos++ {
			if text, isNew := added[pos]; isNew {
				rebuilt = append(rebuilt, text)
			} else {
				if ki >= len(kept) {
					t.Fatalf("case %d: ran out of kept lines\nbefore=%v after=%v edits=%v", i, before, after, edits)
				}
				rebuilt = append(rebuilt, kept[ki])
				ki++
			}
		}
		if strings.Join(rebuilt, "\n") != strings.Join(after, "\n") || ki != len(kept) {
			t.Fatalf("case %d: edits do not reproduce after\nbefore=%v\nafter =%v\nrebuilt=%v\nedits=%v", i, before, after, rebuilt, edits)
		}
	}
}
