package harvey

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	knowledge "github.com/rsdoiel/knowledge"
)

// H5 (knowledge-learning-mode-plan.md): LearnSession is the terminal-free core
// of learning mode. It drafts summaries for the review queue through a Drafter
// and promotes one only from Accept, the human keypath. The tests use real
// knowledge databases and really ingested documents, so the review lifecycle
// under test is the real one, with only the model faked.

// fakeDrafter is a Drafter whose replies a test scripts.
type fakeDrafter struct {
	name  string
	reply func(msgs []Message) (string, error)
	calls [][]Message
}

func (f *fakeDrafter) Name() string { return f.name }
func (f *fakeDrafter) Draft(_ context.Context, msgs []Message) (string, error) {
	f.calls = append(f.calls, msgs)
	if f.reply == nil {
		return "A short summary.", nil
	}
	return f.reply(msgs)
}

func fixedReply(s string) func([]Message) (string, error) {
	return func([]Message) (string, error) { return s, nil }
}

func promptText(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func learnTestKB(t *testing.T) (*knowledge.KnowledgeBase, int64) {
	t.Helper()
	a, pid := newTestAgentWithKB(t)
	return a.KB, pid
}

// ingestDoc ingests a Markdown document, returning its id and its section ids
// by heading ("" for the gist).
func ingestDoc(t *testing.T, kb *knowledge.KnowledgeBase, pid int64, name, content string) (int64, map[string]int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := kb.IngestDocument(pid, path, knowledge.DocumentIngestOptions{}); err != nil {
		t.Fatalf("IngestDocument: %v", err)
	}
	d, err := kb.DocumentByPath(path)
	if err != nil || d == nil {
		t.Fatalf("DocumentByPath: %v, %v", d, err)
	}
	secs, err := kb.DocumentSections(d.ID)
	if err != nil {
		t.Fatalf("DocumentSections: %v", err)
	}
	ids := map[string]int64{}
	for _, s := range secs {
		if s.Level == "gist" {
			ids[""] = s.ID
		} else {
			ids[s.Heading] = s.ID
		}
	}
	return d.ID, ids
}

const learnDoc = "# Notes\n\n## Alpha\n\nAlpha talks about [[chunking]] and [[retrieval]] at length.\n\n## Beta\n\nBeta covers [[madr]] and little else.\n"

func sectionState(t *testing.T, kb *knowledge.KnowledgeBase, docID, sectionID int64) knowledge.DocumentSection {
	t.Helper()
	secs, err := kb.DocumentSections(docID)
	if err != nil {
		t.Fatalf("DocumentSections: %v", err)
	}
	for _, s := range secs {
		if s.ID == sectionID {
			return s
		}
	}
	t.Fatalf("section %d not found", sectionID)
	return knowledge.DocumentSection{}
}

// ─── the confidence heuristic: worked by hand before the code (H5 plan) ─────
//
// First version (the plan): the fraction of the section's LINKED concept tags
// that the draft names. A live run against the real vocabulary showed it is
// nil almost everywhere: hand-off sections mention known concepts (tag density
// 1 to 4) but link almost none (9 of 12 sections had no linked tag), because a
// link needs a [[wikilink]] or more than one mention. So the basis is now the
// concepts MENTIONED in the source text, which is what tag_density counts:
//
//	T = distinct known concepts mentioned in the source (case-insensitive)
//	confidence = |T that the draft also names| / |T|, nil when T is empty
//
// With the concepts also given to the model as hints, this measures concept
// COVERAGE (did the draft use the vocabulary), not faithfulness.
//
//	source names {chunking, retrieval, madr}; draft "explains chunking and how retrieval works" -> 2/3 = 0.6667
//	source names {Go, chunking}; draft "We wrote it in go."                                     -> 1/2 = 0.5
//	source names chunking twice and Chunking once; draft "...chunking..."                        -> 1 distinct, present = 1.0
//	source names {chunking}; draft "...chunkings..." (plural is not the concept)                 -> 0/1 = 0.0
//	source names no known concept                                                                -> nil
//	source names {knowledge base}; draft "the knowledge base is..."                              -> 1.0
//	source names {C++}; draft "written in C++."                                                  -> 1.0
//	source names {chunking}, and an unknown word "zebra"; the unknown word is ignored            -> based on chunking only

func confidenceFor(t *testing.T, known []string, source, draft string) *float64 {
	t.Helper()
	kb, _ := learnTestKB(t)
	for _, k := range known {
		if _, err := kb.AddConcept(k, ""); err != nil {
			t.Fatalf("AddConcept(%q): %v", k, err)
		}
	}
	got, err := learnConfidence(kb, source, draft)
	if err != nil {
		t.Fatalf("learnConfidence: %v", err)
	}
	return got
}

func wantConfidence(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("confidence = nil, want %v", want)
	}
	if math.Abs(*got-want) > 0.001 {
		t.Errorf("confidence = %v, want %v", *got, want)
	}
}

func TestLearnConfidence_FractionOfTheConceptsInTheSourceNamedInTheDraft(t *testing.T) {
	got := confidenceFor(t, []string{"chunking", "retrieval", "madr", "unrelated"},
		"The source discusses chunking, retrieval and madr.", "This section explains chunking and how retrieval works.")
	wantConfidence(t, got, 2.0/3.0)
}

func TestLearnConfidence_ALinkedTagIsNotRequiredAMentionIsEnough(t *testing.T) {
	// The real-data finding: a concept mentioned once is not linked, yet it is
	// exactly what the draft should cover.
	wantConfidence(t, confidenceFor(t, []string{"chunking"}, "Chunking is mentioned once.", "About chunking."), 1.0)
}

func TestLearnConfidence_IsCaseInsensitive(t *testing.T) {
	wantConfidence(t, confidenceFor(t, []string{"Go", "chunking"}, "We use Go with chunking.", "We wrote it in go."), 0.5)
}

func TestLearnConfidence_CountsAConceptOnceHoweverOftenItIsMentioned(t *testing.T) {
	wantConfidence(t, confidenceFor(t, []string{"chunking"}, "chunking, Chunking and more chunking.", "All about chunking."), 1.0)
}

func TestLearnConfidence_APluralIsNotTheConceptAndZeroIsARealAnswer(t *testing.T) {
	got := confidenceFor(t, []string{"chunking"}, "The source names chunking.", "Several chunkings are discussed.")
	if got == nil {
		t.Fatal("confidence = nil, want 0.0: a draft that ignores the source's concepts is a real, low signal")
	}
	wantConfidence(t, got, 0.0)
}

func TestLearnConfidence_NoKnownConceptInTheSourceMeansUnknownNotZero(t *testing.T) {
	if got := confidenceFor(t, []string{"chunking"}, "This source names nothing known.", "Anything at all."); got != nil {
		t.Errorf("confidence = %v, want nil: there is nothing to measure", *got)
	}
}

func TestLearnConfidence_WordsThatAreNotConceptsAreIgnored(t *testing.T) {
	wantConfidence(t, confidenceFor(t, []string{"chunking"}, "Chunking and a zebra.", "Chunking only."), 1.0)
}

func TestLearnConfidence_MultiWordAndPunctuationConcepts(t *testing.T) {
	wantConfidence(t, confidenceFor(t, []string{"knowledge base"}, "The knowledge base holds it.", "The knowledge base is queried."), 1.0)
	wantConfidence(t, confidenceFor(t, []string{"C++"}, "It is written in C++.", "It was written in C++."), 1.0)
}

// ─── drafting ────────────────────────────────────────────────────────────────

func newLearnSession(t *testing.T, d Drafter) (*LearnSession, *knowledge.KnowledgeBase, int64) {
	t.Helper()
	kb, pid := learnTestKB(t)
	return &LearnSession{KB: kb, ProjectID: pid, Drafter: d}, kb, pid
}

func TestDraftPending_DraftsEverySectionAndRecordsWhoAndHowSure(t *testing.T) {
	fd := &fakeDrafter{name: "granite", reply: fixedReply("Covers chunking and retrieval in depth.")}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)

	rep, err := s.DraftPending(context.Background(), 0, nil)
	if err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	alpha := sectionState(t, kb, docID, ids["Alpha"])
	if alpha.SummaryStatus != "drafted" || alpha.SummaryBody != "Covers chunking and retrieval in depth." || alpha.GeneratedBy != "granite" {
		t.Errorf("Alpha = %+v, want drafted by granite with the model's text", alpha)
	}
	if alpha.Confidence == nil || math.Abs(*alpha.Confidence-1.0) > 0.001 {
		t.Errorf("Alpha confidence = %v, want 1.0 (both its tags are named)", alpha.Confidence)
	}
	beta := sectionState(t, kb, docID, ids["Beta"])
	if beta.Confidence == nil || *beta.Confidence != 0.0 {
		t.Errorf("Beta confidence = %v, want 0.0 (the draft never names madr)", beta.Confidence)
	}
	if rep.Drafted < 2 {
		t.Errorf("report = %+v, want at least the two sections drafted", rep)
	}
}

func TestDraftPending_NeverPromotesAnything(t *testing.T) {
	fd := &fakeDrafter{name: "granite"}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	if _, err := s.DraftPending(context.Background(), 0, nil); err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	for heading, id := range ids {
		if st := sectionState(t, kb, docID, id).SummaryStatus; st == "reviewed" {
			t.Errorf("section %q is reviewed after drafting alone: a model may write, only a human accepts", heading)
		}
	}
	if items, _ := kb.DocumentReviewQueue(pid, "reviewed"); len(items) != 0 {
		t.Errorf("reviewed items = %d, want none", len(items))
	}
}

// "A model may write but not accept" is enforced by construction: exactly one
// non-test call to PromoteDocumentSummary exists, in learn.go's Accept.
func TestPromoteDocumentSummary_IsCalledFromTheAcceptKeypathOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var callers []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, _ := os.ReadFile(f)
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			if strings.Contains(line, ".PromoteDocumentSummary(") {
				callers = append(callers, f)
			}
		}
	}
	if len(callers) != 1 || callers[0] != "learn.go" {
		t.Errorf("PromoteDocumentSummary is called from %v, want exactly one call site, in learn.go's Accept", callers)
	}
}

func TestDraftPending_HonoursTheLimit(t *testing.T) {
	fd := &fakeDrafter{name: "m"}
	s, kb, pid := newLearnSession(t, fd)
	ingestDoc(t, kb, pid, "d.md", "# T\n\n## A\n\none.\n\n## B\n\ntwo.\n\n## C\n\nthree.\n\n## D\n\nfour.\n")
	if _, err := s.DraftPending(context.Background(), 2, nil); err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	if len(fd.calls) != 2 {
		t.Errorf("the drafter was called %d times, want 2 for --limit 2", len(fd.calls))
	}
}

func TestDraftPending_StripsAThinkBlockAndTrims(t *testing.T) {
	fd := &fakeDrafter{name: "qwen", reply: fixedReply("<think>Let me reason about this.\nMore thought.</think>\n\n  The real summary.  \n")}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	s.DraftPending(context.Background(), 0, nil)
	if got := sectionState(t, kb, docID, ids["Alpha"]).SummaryBody; got != "The real summary." {
		t.Errorf("stored summary = %q, want the model's reasoning stripped and the text trimmed", got)
	}
}

func TestDraftPending_AnEmptyReplyIsAFailureAndSavesNothing(t *testing.T) {
	fd := &fakeDrafter{name: "m", reply: fixedReply("   \n<think>only thinking</think>\n")}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	rep, err := s.DraftPending(context.Background(), 0, nil)
	if err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	if rep.Failed == 0 {
		t.Errorf("report = %+v, want the empty replies counted as failures", rep)
	}
	if st := sectionState(t, kb, docID, ids["Alpha"]).SummaryStatus; st != "unsummarized" {
		t.Errorf("Alpha status = %q, want unsummarized: an empty draft must not be saved", st)
	}
}

func TestDraftPending_OneFailureDoesNotStopTheRest(t *testing.T) {
	calls := 0
	fd := &fakeDrafter{name: "m", reply: func([]Message) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("backend hiccup")
		}
		return "Fine.", nil
	}}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	rep, err := s.DraftPending(context.Background(), 0, nil)
	if err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	if rep.Failed != 1 {
		t.Errorf("report = %+v, want exactly one failure", rep)
	}
	drafted := 0
	for _, id := range []int64{ids["Alpha"], ids["Beta"]} {
		if sectionState(t, kb, docID, id).SummaryStatus == "drafted" {
			drafted++
		}
	}
	if drafted != 1 {
		t.Errorf("%d of the two sections were drafted, want the one that did not fail", drafted)
	}
}

func TestDraftPending_LeavesAlreadyDraftedAndReviewedSectionsAlone(t *testing.T) {
	fd := &fakeDrafter{name: "m", reply: fixedReply("New text.")}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	if err := kb.DraftDocumentSummary(ids["Alpha"], "Human wrote this.", "human", nil); err != nil {
		t.Fatalf("DraftDocumentSummary: %v", err)
	}
	s.DraftPending(context.Background(), 0, nil)
	if got := sectionState(t, kb, docID, ids["Alpha"]); got.SummaryBody != "Human wrote this." || got.GeneratedBy != "human" {
		t.Errorf("Alpha = %+v, want the existing draft untouched by a batch run", got)
	}
}

func TestDraftPending_OnlyTouchesTheCurrentProject(t *testing.T) {
	fd := &fakeDrafter{name: "m"}
	s, kb, pid := newLearnSession(t, fd)
	other, _ := kb.AddProject("other", "")
	docID, ids := ingestDoc(t, kb, other, "o.md", learnDoc)
	ingestDoc(t, kb, pid, "mine.md", "# T\n\n## Mine\n\ntext.\n")
	s.DraftPending(context.Background(), 0, nil)
	if st := sectionState(t, kb, docID, ids["Alpha"]).SummaryStatus; st != "unsummarized" {
		t.Errorf("another project's section is %q, want it left alone", st)
	}
}

func TestDraftPending_ThePromptCarriesTheTitleHeadingTagsAndText(t *testing.T) {
	fd := &fakeDrafter{name: "m"}
	s, kb, pid := newLearnSession(t, fd)
	ingestDoc(t, kb, pid, "d.md", learnDoc)
	s.DraftPending(context.Background(), 1, nil)
	if len(fd.calls) == 0 {
		t.Fatal("the drafter was never called")
	}
	if len(fd.calls) == 0 {
		t.Fatal("the drafter was never called")
	}
	p := promptText(fd.calls[0])
	for _, want := range []string{"Notes", "Alpha", "chunking", "retrieval", "talks about"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt = %q, want it to contain %q", p, want)
		}
	}
}

func TestDraftPending_TruncatesAnOversizedSectionAndSaysSo(t *testing.T) {
	fd := &fakeDrafter{name: "m"}
	s, kb, pid := newLearnSession(t, fd)
	s.MaxSourceBytes = 200
	ingestDoc(t, kb, pid, "d.md", "# T\n\n## Long\n\n"+strings.Repeat("filler words go here. ", 100)+"\n")
	s.DraftPending(context.Background(), 1, nil)
	if len(fd.calls) == 0 {
		t.Fatal("the drafter was never called")
	}
	p := promptText(fd.calls[0])
	if !strings.Contains(p, "[text truncated]") {
		t.Errorf("prompt does not mark the truncation")
	}
	if len(p) > 200+1500 {
		t.Errorf("prompt is %d bytes, want the source cut near the 200-byte cap", len(p))
	}
}

func TestDraftPending_TheGistIsDraftedFromTheSectionSummariesOnceTheyExist(t *testing.T) {
	fd := &fakeDrafter{name: "m", reply: func(msgs []Message) (string, error) {
		if strings.Contains(promptText(msgs), "section summaries") {
			return "Whole-document gist.", nil
		}
		return "Summary of one section.", nil
	}}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	if _, err := s.DraftPending(context.Background(), 0, nil); err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	gist := sectionState(t, kb, docID, ids[""])
	if gist.SummaryStatus != "drafted" || gist.SummaryBody != "Whole-document gist." {
		t.Errorf("gist = %+v, want it drafted after its sections", gist)
	}
	if len(fd.calls) == 0 {
		t.Fatal("the drafter was never called")
	}
	last := promptText(fd.calls[len(fd.calls)-1])
	if !strings.Contains(last, "Summary of one section.") {
		t.Errorf("gist prompt = %q, want it built from the section summaries", last)
	}
}

func TestDraftPending_TheGistWaitsWhenASectionCouldNotBeSummarized(t *testing.T) {
	fd := &fakeDrafter{name: "m", reply: func(msgs []Message) (string, error) {
		if strings.Contains(promptText(msgs), "Beta") {
			return "", errors.New("no")
		}
		return "Ok.", nil
	}}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	s.DraftPending(context.Background(), 0, nil)
	if st := sectionState(t, kb, docID, ids[""]).SummaryStatus; st != "unsummarized" {
		t.Errorf("gist status = %q, want it to wait until every section has a summary", st)
	}
}

// ─── the human keypath: accept, edit, redraft ───────────────────────────────

func draftedSection(t *testing.T) (*LearnSession, *knowledge.KnowledgeBase, int64, int64, *fakeDrafter) {
	t.Helper()
	fd := &fakeDrafter{name: "granite", reply: fixedReply("Machine draft.")}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	s.DraftPending(context.Background(), 0, nil)
	return s, kb, docID, ids["Alpha"], fd
}

func TestAccept_PromotesADraftedSummaryToReviewed(t *testing.T) {
	s, kb, docID, alpha, _ := draftedSection(t)
	if err := s.Accept(alpha); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if st := sectionState(t, kb, docID, alpha).SummaryStatus; st != "reviewed" {
		t.Errorf("status = %q, want reviewed", st)
	}
}

func TestAccept_RefusesASectionThatWasNeverDrafted(t *testing.T) {
	fd := &fakeDrafter{name: "m"}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	if err := s.Accept(ids["Alpha"]); err == nil {
		t.Error("Accept of an unsummarized section = nil error, want one")
	}
	if st := sectionState(t, kb, docID, ids["Alpha"]).SummaryStatus; st == "reviewed" {
		t.Error("an unsummarized section was promoted")
	}
}

func TestEdit_RecordsTheHumanAsTheAuthorWithNoConfidenceAndDoesNotPromote(t *testing.T) {
	s, kb, docID, alpha, _ := draftedSection(t)
	if err := s.Edit(alpha, "My own wording."); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	got := sectionState(t, kb, docID, alpha)
	if got.SummaryBody != "My own wording." || got.GeneratedBy != "human" || got.Confidence != nil {
		t.Errorf("section = %+v, want the human's text, generated_by human, no confidence", got)
	}
	if got.SummaryStatus != "drafted" {
		t.Errorf("status = %q, want drafted: editing is not accepting", got.SummaryStatus)
	}
}

func TestEdit_RefusesEmptyText(t *testing.T) {
	s, kb, docID, alpha, _ := draftedSection(t)
	if err := s.Edit(alpha, "   \n"); err == nil {
		t.Error("Edit with empty text = nil error, want one")
	}
	if got := sectionState(t, kb, docID, alpha).SummaryBody; got != "Machine draft." {
		t.Errorf("summary = %q, want the earlier draft kept", got)
	}
}

func TestRedraft_ReplacesTheDraftWithANewOneAndStaysDrafted(t *testing.T) {
	s, kb, docID, alpha, fd := draftedSection(t)
	fd.name = "other-model"
	fd.reply = fixedReply("Second attempt at chunking and retrieval.")
	if err := s.Redraft(context.Background(), alpha); err != nil {
		t.Fatalf("Redraft: %v", err)
	}
	got := sectionState(t, kb, docID, alpha)
	if got.SummaryBody != "Second attempt at chunking and retrieval." || got.GeneratedBy != "other-model" || got.SummaryStatus != "drafted" {
		t.Errorf("section = %+v, want the new draft by the new model, still drafted", got)
	}
}

func TestDraftedItems_ListsTheHumanReviewQueue(t *testing.T) {
	s, _, _, alpha, _ := draftedSection(t)
	items, err := s.DraftedItems()
	if err != nil {
		t.Fatalf("DraftedItems: %v", err)
	}
	found := false
	for _, it := range items {
		if it.SummaryStatus != "drafted" {
			t.Errorf("item %d is %q, want only drafted items", it.ID, it.SummaryStatus)
		}
		if it.ID == alpha {
			found = true
		}
	}
	if !found || len(items) == 0 {
		t.Errorf("items = %d, want Alpha among them", len(items))
	}
}

// A Markdown H1 becomes a section with an empty body (found while writing the
// tests above: the first queue item was "Notes" with no text). Asking a model
// to summarize nothing invites a made-up summary, and such a section can never
// get a real one, so it must not be drafted and must not block its gist.

func TestDraftPending_NeverAsksTheModelToSummarizeAnEmptySection(t *testing.T) {
	fd := &fakeDrafter{name: "m"}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	s.DraftPending(context.Background(), 0, nil)
	for _, call := range fd.calls {
		p := promptText(call)
		if strings.Contains(p, "Section: Notes\n") {
			t.Errorf("the model was asked to summarize the empty heading-only section: %q", p)
		}
	}
	if notes, ok := ids["Notes"]; ok {
		if st := sectionState(t, kb, docID, notes).SummaryStatus; st != "unsummarized" {
			t.Errorf("empty section status = %q, want it left unsummarized", st)
		}
	}
}

func TestDraftPending_AnEmptySectionDoesNotCountTowardTheLimitOrBlockTheGist(t *testing.T) {
	fd := &fakeDrafter{name: "m", reply: fixedReply("Ok.")}
	s, kb, pid := newLearnSession(t, fd)
	docID, ids := ingestDoc(t, kb, pid, "d.md", learnDoc)
	rep, err := s.DraftPending(context.Background(), 2, nil)
	if err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	if rep.Drafted != 2 {
		t.Errorf("report = %+v, want the limit of 2 spent on the two real sections", rep)
	}
	// A second run drafts the gist: the empty section must not hold it back.
	if _, err := s.DraftPending(context.Background(), 0, nil); err != nil {
		t.Fatalf("second DraftPending: %v", err)
	}
	if st := sectionState(t, kb, docID, ids[""]).SummaryStatus; st != "drafted" {
		t.Errorf("gist status = %q, want drafted: an empty section can never have a summary and must not block it", st)
	}
}

// A gist is drafted from the section summaries, so it is reviewed after them:
// a human who edits a section first can then redraft the gist to match.
func TestDraftedItems_SectionsComeBeforeTheirGist(t *testing.T) {
	s, _, _, _, _ := draftedSection(t)
	items, err := s.DraftedItems()
	if err != nil {
		t.Fatalf("DraftedItems: %v", err)
	}
	seenGist := false
	for _, it := range items {
		if it.Level == "gist" {
			seenGist = true
		} else if seenGist {
			t.Errorf("section %q comes after a gist: sections must be reviewed first", it.Heading)
		}
	}
	if !seenGist {
		t.Fatal("no gist in the drafted items; the test would prove nothing")
	}
}
