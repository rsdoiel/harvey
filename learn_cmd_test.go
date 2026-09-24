package harvey

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	knowledge "github.com/rsdoiel/knowledge"
)

// H5 commands: /kb learn draft, /kb learn review, and bare /kb learn.
// Drafting model precedence (RSDOIEL, 2026-09-24): an @name on the command,
// then learn_model in harvey.yaml, then the active model.

type learnCmdFixture struct {
	a         *Agent
	kb        *knowledge.KnowledgeBase
	pid       int64
	drafter   *fakeDrafter
	requested []string // names the override was asked to resolve
	restored  int
}

func newLearnCmd(t *testing.T, input string) *learnCmdFixture {
	t.Helper()
	a, pid := newTestAgentWithKB(t)
	a.In = strings.NewReader(input)
	f := &learnCmdFixture{a: a, kb: a.KB, pid: pid, drafter: &fakeDrafter{name: "granite", reply: fixedReply("A draft naming chunking.")}}
	a.learnDrafterOverride = func(name string, out io.Writer) (Drafter, func(), error) {
		f.requested = append(f.requested, name)
		return f.drafter, func() { f.restored++ }, nil
	}
	return f
}

func (f *learnCmdFixture) doc(t *testing.T, name, content string) (int64, map[string]int64) {
	t.Helper()
	return ingestDoc(t, f.kb, f.pid, name, content)
}

func (f *learnCmdFixture) count(t *testing.T, status string) int {
	t.Helper()
	items, err := f.kb.DocumentReviewQueue(f.pid, status)
	if err != nil {
		t.Fatalf("DocumentReviewQueue: %v", err)
	}
	return len(items)
}

func runLearn(t *testing.T, f *learnCmdFixture, args ...string) string {
	t.Helper()
	var out strings.Builder
	if err := kbLearn(f.a, args, &out); err != nil {
		t.Fatalf("kbLearn %v: %v", args, err)
	}
	return out.String()
}

// ─── model resolution ────────────────────────────────────────────────────────

func TestLearnDraft_ModelPrecedenceMentionThenConfigThenActive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string
		args    []string
		want    string
		comment string
	}{
		{"mention wins over config", "cfgmodel", []string{"draft", "@granite"}, "granite", "an @name overrides learn_model"},
		{"config used when no mention", "cfgmodel", []string{"draft"}, "cfgmodel", "learn_model is the default"},
		{"active model when neither", "", []string{"draft"}, "", "empty means the active model"},
		{"mention without config", "", []string{"draft", "@small"}, "small", "an @name works alone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLearnCmd(t, "")
			f.a.Config.LearnModel = tc.config
			f.doc(t, "d.md", learnDoc)
			runLearn(t, f, tc.args...)
			if len(f.requested) != 1 || f.requested[0] != tc.want {
				t.Errorf("resolved %v, want [%q]: %s", f.requested, tc.want, tc.comment)
			}
		})
	}
}

func TestLearnDraft_TheMentionMayComeBeforeOrAfterTheFlags(t *testing.T) {
	f := newLearnCmd(t, "")
	f.doc(t, "d.md", learnDoc)
	runLearn(t, f, "draft", "--limit", "1", "@granite")
	runLearn(t, f, "draft", "@other", "--limit", "1")
	if len(f.requested) != 2 || f.requested[0] != "granite" || f.requested[1] != "other" {
		t.Errorf("resolved %v, want granite then other", f.requested)
	}
}

func TestLearnDraft_TheModelIsRestoredWhenTheRunEnds(t *testing.T) {
	f := newLearnCmd(t, "")
	f.doc(t, "d.md", learnDoc)
	runLearn(t, f, "draft", "@granite")
	if f.restored != 1 {
		t.Errorf("restore called %d times, want once: a local model switch must be undone", f.restored)
	}
}

func TestLearnDraft_AModelThatCannotBeResolvedIsReportedNotFatal(t *testing.T) {
	f := newLearnCmd(t, "")
	f.a.learnDrafterOverride = func(string, io.Writer) (Drafter, func(), error) {
		return nil, nil, io.ErrUnexpectedEOF
	}
	f.doc(t, "d.md", learnDoc)
	out := runLearn(t, f, "draft", "@nonesuch")
	if !strings.Contains(out, "nonesuch") {
		t.Errorf("output = %q, want the unresolvable model named", out)
	}
	if n := f.count(t, "drafted"); n != 0 {
		t.Errorf("%d items drafted with no model", n)
	}
}

// ─── draft ───────────────────────────────────────────────────────────────────

func TestLearnDraft_DraftsTheQueueAndDoesNotPromote(t *testing.T) {
	f := newLearnCmd(t, "")
	f.doc(t, "d.md", learnDoc)
	out := runLearn(t, f, "draft")
	if f.count(t, "drafted") == 0 {
		t.Errorf("nothing was drafted; output:\n%s", out)
	}
	if f.count(t, "reviewed") != 0 {
		t.Error("a draft run promoted something to reviewed")
	}
	for _, want := range []string{"granite", "drafted", "/kb learn review"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
}

func TestLearnDraft_TheDefaultLimitIs25AndLimitZeroMeansAll(t *testing.T) {
	f := newLearnCmd(t, "")
	var body strings.Builder
	body.WriteString("# Big\n\n")
	for i := 0; i < 30; i++ {
		body.WriteString("## Part " + string(rune('A'+i%26)) + string(rune('a'+i/26)) + "\n\ntext for this part.\n\n")
	}
	f.doc(t, "big.md", body.String())

	out := runLearn(t, f, "draft")
	if len(f.drafter.calls) != 25 {
		t.Errorf("default run made %d model calls, want 25", len(f.drafter.calls))
	}
	if !strings.Contains(out, "25") {
		t.Errorf("output = %q, want the limit stated", out)
	}
	f.drafter.calls = nil
	runLearn(t, f, "draft", "--limit", "0")
	if len(f.drafter.calls) < 5 {
		t.Errorf("--limit 0 made %d further calls, want the rest of the queue drafted", len(f.drafter.calls))
	}
}

func TestLearnDraft_DryRunCountsAndCallsNoModel(t *testing.T) {
	f := newLearnCmd(t, "")
	f.doc(t, "d.md", learnDoc)
	out := runLearn(t, f, "draft", "--dry-run")
	if len(f.drafter.calls) != 0 || len(f.requested) != 0 {
		t.Errorf("a dry run resolved a model or called it: %v %v", f.requested, len(f.drafter.calls))
	}
	if !strings.Contains(strings.ToLower(out), "would draft") {
		t.Errorf("output = %q, want a preview of what would be drafted", out)
	}
	if f.count(t, "drafted") != 0 {
		t.Error("a dry run drafted something")
	}
}

func TestLearnDraft_NothingToDraftSaysSo(t *testing.T) {
	f := newLearnCmd(t, "")
	out := runLearn(t, f, "draft")
	if !strings.Contains(strings.ToLower(out), "nothing") {
		t.Errorf("output = %q, want a message that there is nothing to draft", out)
	}
}

func TestLearnDraft_RequiresACurrentProjectAndBadFlagsGiveUsage(t *testing.T) {
	f := newLearnCmd(t, "")
	f.a.Config.Memory.CurrentProjectID = 0
	if out := runLearn(t, f, "draft"); !strings.Contains(out, "/kb project use") {
		t.Errorf("output = %q, want how to pick a project", out)
	}
	f.a.Config.Memory.CurrentProjectID = f.pid
	for _, args := range [][]string{{"draft", "--limit"}, {"draft", "--limit", "x"}, {"draft", "--bogus"}} {
		if out := runLearn(t, f, args...); !strings.Contains(out, "Usage") && !strings.Contains(out, "not a number") && !strings.Contains(out, "needs") {
			t.Errorf("%v: output = %q, want usage guidance", args, out)
		}
	}
}

// ─── review ──────────────────────────────────────────────────────────────────

func draftedFixture(t *testing.T, input string) (*learnCmdFixture, int64, map[string]int64) {
	t.Helper()
	f := newLearnCmd(t, "")
	docID, ids := f.doc(t, "d.md", learnDoc)
	s := &LearnSession{KB: f.kb, ProjectID: f.pid, Drafter: f.drafter}
	if _, err := s.DraftPending(t.Context(), 0, nil); err != nil {
		t.Fatalf("DraftPending: %v", err)
	}
	f.a.In = strings.NewReader(input)
	return f, docID, ids
}

func TestLearnReview_AcceptPromotesOnlyThatItem(t *testing.T) {
	f, _, _ := draftedFixture(t, "a\nq\n")
	before := f.count(t, "drafted")
	out := runLearn(t, f, "review")
	if got := f.count(t, "reviewed"); got != 1 {
		t.Errorf("reviewed = %d, want exactly the one accepted; output:\n%s", got, out)
	}
	if f.count(t, "drafted") != before-1 {
		t.Errorf("drafted = %d, want %d", f.count(t, "drafted"), before-1)
	}
}

func TestLearnReview_SkipQuitEnterAndEndOfInputNeverPromote(t *testing.T) {
	for _, input := range []string{"s\ns\ns\ns\ns\n", "q\n", "\n\n\n\n\n\n", "", "x\ny\nz\n"} {
		f, _, _ := draftedFixture(t, input)
		runLearn(t, f, "review")
		if got := f.count(t, "reviewed"); got != 0 {
			t.Errorf("input %q promoted %d item(s): only an explicit accept may", input, got)
		}
	}
}

func TestLearnReview_AnUnknownKeyReprompsTheSameItem(t *testing.T) {
	f, _, _ := draftedFixture(t, "x\na\nq\n")
	out := runLearn(t, f, "review")
	if !strings.Contains(out, "[a]ccept") {
		t.Errorf("output = %q, want the key legend", out)
	}
	if got := f.count(t, "reviewed"); got != 1 {
		t.Errorf("reviewed = %d, want 1: the bad key must not consume the item", got)
	}
}

func TestLearnReview_ShowsTheSignalsTheHumanNeedsToDecide(t *testing.T) {
	f, _, _ := draftedFixture(t, "q\n")
	out := runLearn(t, f, "review")
	for _, want := range []string{"granite", "confidence", "A draft naming chunking.", "Alpha", "talks about"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to show %q", out, want)
		}
	}
}

func TestLearnReview_EditGoesThroughTheEditorAndIsThenAcceptedExplicitly(t *testing.T) {
	f, docID, ids := draftedFixture(t, "e\na\nq\n")
	fakeEditor(t, `printf 'The human rewrote this.\n' > "$1"`)
	runLearn(t, f, "review")
	got := sectionState(t, f.kb, docID, firstDraftedID(t, f, ids))
	_ = got
	items, _ := f.kb.DocumentReviewQueue(f.pid, "reviewed")
	if len(items) != 1 {
		t.Fatalf("reviewed = %d, want the edited item accepted", len(items))
	}
	if items[0].SummaryBody != "The human rewrote this." || items[0].GeneratedBy != "human" || items[0].Confidence != nil {
		t.Errorf("accepted item = %+v, want the human's text, generated_by human, no confidence", items[0])
	}
}

func firstDraftedID(t *testing.T, f *learnCmdFixture, ids map[string]int64) int64 {
	t.Helper()
	items, _ := f.kb.DocumentReviewQueue(f.pid, "drafted")
	if len(items) == 0 {
		items, _ = f.kb.DocumentReviewQueue(f.pid, "reviewed")
	}
	return items[0].ID
}

func TestLearnReview_EditThatChangesNothingKeepsTheDraft(t *testing.T) {
	f, _, _ := draftedFixture(t, "e\nq\n")
	truePath, _ := os.LookupEnv("PATH")
	_ = truePath
	fakeEditor(t, `true`)
	runLearn(t, f, "review")
	items, _ := f.kb.DocumentReviewQueue(f.pid, "drafted")
	for _, it := range items {
		if it.GeneratedBy == "human" {
			t.Errorf("item %d became a human edit although the editor changed nothing", it.ID)
		}
	}
}

func TestLearnReview_AFailingEditorIsReportedAndTheItemStaysAvailable(t *testing.T) {
	f, _, _ := draftedFixture(t, "e\na\nq\n")
	fakeEditor(t, `exit 1`)
	out := runLearn(t, f, "review")
	if !strings.Contains(strings.ToLower(out), "editor") {
		t.Errorf("output = %q, want the editor failure reported", out)
	}
	if f.count(t, "reviewed") != 1 {
		t.Error("after a failed edit the same item should still be there to accept")
	}
}

func TestLearnReview_RedraftAsksTheModelAgainAndKeepsTheItem(t *testing.T) {
	f, _, _ := draftedFixture(t, "r\nq\n")
	f.drafter.name = "second-model"
	f.drafter.reply = fixedReply("A fresh attempt about chunking.")
	out := runLearn(t, f, "review")
	items, _ := f.kb.DocumentReviewQueue(f.pid, "drafted")
	redrafted := 0
	for _, it := range items {
		if it.SummaryBody == "A fresh attempt about chunking." && it.GeneratedBy == "second-model" {
			redrafted++
		}
	}
	if redrafted != 1 {
		t.Errorf("redrafted items = %d, want exactly the one; output:\n%s", redrafted, out)
	}
	if f.count(t, "reviewed") != 0 {
		t.Error("a redraft promoted something")
	}
}

func TestLearnReview_NothingDraftedSaysSo(t *testing.T) {
	f := newLearnCmd(t, "")
	out := runLearn(t, f, "review")
	if !strings.Contains(strings.ToLower(out), "nothing") {
		t.Errorf("output = %q, want a message that there is nothing to review", out)
	}
}

// ─── bare /kb learn ──────────────────────────────────────────────────────────

func TestLearnBare_DraftsWhatIsMissingThenReviews(t *testing.T) {
	f := newLearnCmd(t, "a\nq\n")
	f.doc(t, "d.md", learnDoc)
	runLearn(t, f)
	if len(f.drafter.calls) == 0 {
		t.Error("bare /kb learn did not draft the unsummarized items")
	}
	if got := f.count(t, "reviewed"); got != 1 {
		t.Errorf("reviewed = %d, want the one item the user accepted", got)
	}
}

func TestKBLearn_UsageListsAllThreeSubcommands(t *testing.T) {
	f := newLearnCmd(t, "")
	out := runLearn(t, f, "nonesuch")
	for _, want := range []string{"ingest", "draft", "review"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage = %q, want it to name %q", out, want)
		}
	}
	_ = filepath.Join
}

func TestLearnReview_AGistShowsNoMisleadingWordCount(t *testing.T) {
	f, _, _ := draftedFixture(t, "s\ns\ns\n")
	out := runLearn(t, f, "review")
	i := strings.Index(out, "whole document")
	if i < 0 {
		t.Fatalf("no gist shown:\n%s", out)
	}
	line := out[i:]
	line = line[:strings.Index(line, "\n   source:")]
	if strings.Contains(line, "0 words") {
		t.Errorf("gist header = %q, want no \"0 words\": a gist has no source size", line)
	}
}
