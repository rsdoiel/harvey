package harvey

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// D3 of the observation-drafting plan: the prompt, the reading of the reply,
// and the origin line (design decisions 2, 5 and 6).

func onePass(texts ...string) DraftPass {
	var es []TranscriptEntry
	for i, tx := range texts {
		id := "harvey-session-20261101-100000#" + strings.Repeat("0", 3) + string(rune('1'+i))
		es = append(es, TranscriptEntry{First: id, Last: id, Text: tx})
	}
	return DraftPass{First: es[0].First, Last: es[len(es)-1].Last, Entries: es}
}

func TestBuildDraftMessages(t *testing.T) {
	msgs := BuildDraftMessages(onePass("User: why does the stream close?", "Model: hailo drops it past 3300 chars"), "harvey")
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("want a system and a user message, got %+v", msgs)
	}
	sys, user := msgs[0].Content, msgs[1].Content
	for _, kind := range []string{"note", "finding", "decision", "question", "hypothesis"} {
		if !strings.Contains(sys, kind) {
			t.Errorf("instructions do not name the kind %q", kind)
		}
	}
	if !strings.Contains(sys, "NONE") {
		t.Error("instructions do not say how to answer when nothing is worth keeping")
	}
	for _, want := range []string{"harvey", "why does the stream close?", "hailo drops it past 3300 chars"} {
		if !strings.Contains(user, want) {
			t.Errorf("the excerpt message lacks %q", want)
		}
	}
	if strings.Index(user, "why does the stream") > strings.Index(user, "hailo drops") {
		t.Error("entries are out of order in the prompt")
	}
}

func TestParseProposals_NothingWorthKeeping(t *testing.T) {
	for _, reply := range []string{"", "   \n\n", "NONE", "none", "None.", "NONE\n", "```\nNONE\n```"} {
		got, dropped := ParseProposals(reply)
		if len(got) != 0 || dropped != 0 {
			t.Errorf("%q: got %+v, dropped %d; want nothing and no error", reply, got, dropped)
		}
	}
}

func TestParseProposals_ReadsOneObservationPerLine(t *testing.T) {
	reply := "finding: hailo-ollama closes the stream once the prompt passes about 3300 characters\n" +
		"decision: use qwen2.5-coder:1.5b on the Pi for drafting\n" +
		"question: does the tool-result role work on the hailo models?\n"
	got, dropped := ParseProposals(reply)
	if dropped != 0 || len(got) != 3 {
		t.Fatalf("got %+v, dropped %d", got, dropped)
	}
	if got[0].Kind != "finding" || !strings.HasPrefix(got[0].Body, "hailo-ollama closes the stream") {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Kind != "decision" || got[2].Kind != "question" {
		t.Errorf("kinds = %s, %s", got[1].Kind, got[2].Kind)
	}
}

func TestParseProposals_ToleratesWhatSmallModelsWrite(t *testing.T) {
	reply := "Here are the observations worth keeping:\n" +
		"```\n" +
		"1. Finding: the build needs Go 1.27.3\n" +
		"- **Note**: kb must be 0.0.30 or newer\n" +
		"* HYPOTHESIS - the stream closes because of the context window\n" +
		"```\n" +
		"\n"
	got, dropped := ParseProposals(reply)
	if dropped != 0 || len(got) != 3 {
		t.Fatalf("got %+v, dropped %d", got, dropped)
	}
	want := []struct{ kind, body string }{
		{"finding", "the build needs Go 1.27.3"},
		{"note", "kb must be 0.0.30 or newer"},
		{"hypothesis", "the stream closes because of the context window"},
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Body != w.body {
			t.Errorf("[%d] = %+v, want %s: %s", i, got[i], w.kind, w.body)
		}
	}
}

func TestParseProposals_AKindOutsideTheVocabularyIsDroppedAndCounted(t *testing.T) {
	reply := "finding: keep this one\n" +
		"opinion: this kind is not in the vocabulary\n" +
		"tip: neither is this\n" +
		"note: and this one is kept too\n"
	got, dropped := ParseProposals(reply)
	if len(got) != 2 || dropped != 2 {
		t.Fatalf("got %d kept, %d dropped; want 2 and 2: %+v", len(got), dropped, got)
	}
	if got[0].Body != "keep this one" || got[1].Body != "and this one is kept too" {
		t.Errorf("got %+v", got)
	}
}

func TestParseProposals_AnEmptyBodyIsDropped(t *testing.T) {
	got, dropped := ParseProposals("finding:\nnote:   \nnote: real")
	if len(got) != 1 || got[0].Body != "real" || dropped != 0 {
		t.Errorf("got %+v, dropped %d", got, dropped)
	}
}

func TestParseProposals_ALongBodyIsCutAtAWord(t *testing.T) {
	long := strings.Repeat("word ", 400)
	got, _ := ParseProposals("finding: " + long)
	if len(got) != 1 {
		t.Fatal(got)
	}
	if len(got[0].Body) > proposalBodyCap+4 {
		t.Errorf("body is %d bytes, cap %d", len(got[0].Body), proposalBodyCap)
	}
	if !strings.HasSuffix(got[0].Body, "…") || strings.HasSuffix(strings.TrimSuffix(got[0].Body, "…"), "wor") {
		t.Errorf("not cut at a word: …%q", got[0].Body[len(got[0].Body)-12:])
	}
}

func TestFinishBody_NamesTheSessionAndTheEvents(t *testing.T) {
	p := Proposal{Kind: "finding", Body: "the stream closes"}
	pass := DraftPass{First: "harvey-session-20261030-143612#0042", Last: "harvey-session-20261030-143612#0057"}
	got := FinishBody(p, pass)
	want := "the stream closes\nDrafted from harvey-session-20261030-143612#0042..#0057."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	one := DraftPass{First: "s#0007", Last: "s#0007"}
	if got := FinishBody(p, one); !strings.HasSuffix(got, "Drafted from s#0007.") {
		t.Errorf("a single-event pass: %q", got)
	}
}

// A fake Drafter that returns scripted replies and records the purpose of each call.
type scriptDrafter struct {
	replies   []string
	errs      []error
	purposes  []string
	calls     int
	lastUser  string
	afterCall func(n int)
	// during runs at the start of a call with the call's context and its
	// number; an error it returns is the call's error.
	during func(ctx context.Context, n int) error
}

func (f *scriptDrafter) Name() string { return "fake" }
func (f *scriptDrafter) Draft(ctx context.Context, msgs []Message) (string, error) {
	i := f.calls
	f.calls++
	f.purposes = append(f.purposes, callPurposeOf(ctx).name)
	if len(msgs) > 1 {
		f.lastUser = msgs[len(msgs)-1].Content
	}
	if f.afterCall != nil {
		defer f.afterCall(f.calls)
	}
	if f.during != nil {
		if err := f.during(ctx, f.calls); err != nil {
			return "", err
		}
	}
	if i < len(f.errs) && f.errs[i] != nil {
		return "", f.errs[i]
	}
	if i < len(f.replies) {
		return f.replies[i], nil
	}
	return "NONE", nil
}

func TestRunDraftPass_AsksOnceWithThePurposeDraft(t *testing.T) {
	f := &scriptDrafter{replies: []string{"finding: it works\nnote: so does this"}}
	got, dropped, err := RunDraftPass(context.Background(), f, onePass("User: hi"), "harvey", "harvey-session-20261101-100000", 1, 3)
	if err != nil || dropped != 0 || len(got) != 2 {
		t.Fatalf("got %+v, %d, %v", got, dropped, err)
	}
	if f.calls != 1 || f.purposes[0] != "draft" {
		t.Errorf("calls = %d, purposes = %v; want one call with purpose draft", f.calls, f.purposes)
	}
}

func TestRunDraftPass_AModelErrorNamesThePassAndKeepsTheCause(t *testing.T) {
	cause := errors.New("connection refused")
	f := &scriptDrafter{errs: []error{cause}}
	_, _, err := RunDraftPass(context.Background(), f, onePass("User: hi"), "harvey", "s", 2, 3)
	if err == nil || !strings.Contains(err.Error(), "pass 2 of 3") || !errors.Is(err, cause) {
		t.Fatalf("err = %v; want it to name pass 2 of 3 and wrap the cause", err)
	}
}

func TestParseProposals_ADashIsASeparatorOnlyAfterASpace(t *testing.T) {
	got, dropped := ParseProposals("note-taking is a habit worth keeping\nfinding - the dash with spaces works\nnote – so does an en dash")
	if dropped != 0 || len(got) != 2 {
		t.Fatalf("got %+v, dropped %d; the first line has no kind and is ignored", got, dropped)
	}
	if got[0].Body != "the dash with spaces works" || got[1].Body != "so does an en dash" {
		t.Errorf("got %+v", got)
	}
}

func TestParseProposals_NoneWithAnExplanationIsStillNothing(t *testing.T) {
	for _, reply := range []string{"NONE: nothing worth keeping here", "None - the session was routine.", "none. nothing to record"} {
		got, dropped := ParseProposals(reply)
		if len(got) != 0 || dropped != 0 {
			t.Errorf("%q: got %+v, dropped %d", reply, got, dropped)
		}
	}
}

// Real output from qwen2.5:3b (2026-11-01, tmp/d7): it copied the placeholder
// "KIND: finding" literally and put the text on the next line.
func TestParseProposals_AKindLineFollowedByTheText(t *testing.T) {
	real := "KIND: finding\nThe `bat` command is not a standard command and needs to be installed separately for proper functionality."
	got, dropped := ParseProposals(real)
	if dropped != 0 || len(got) != 1 || got[0].Kind != "finding" || !strings.HasPrefix(got[0].Body, "The `bat` command is not a standard command") {
		t.Fatalf("got %+v, dropped %d", got, dropped)
	}
}

func TestParseProposals_AKindAloneOnALineTakesTheNextLine(t *testing.T) {
	cases := map[string]Proposal{
		"finding\nbat is installed as batcat":             {"finding", "bat is installed as batcat"},
		"Kind: note\nText: kb must be 0.0.30":             {"note", "kb must be 0.0.30"},
		"**question**\n\ndoes the role work on hailo?":    {"question", "does the role work on hailo?"},
		"1. KIND: decision\n   use qwen2.5 for drafting":  {"decision", "use qwen2.5 for drafting"},
		"hypothesis:\nthe stream closes on a long prompt": {"hypothesis", "the stream closes on a long prompt"},
	}
	for in, want := range cases {
		got, dropped := ParseProposals(in)
		if dropped != 0 || len(got) != 1 || got[0] != want {
			t.Errorf("%q: got %+v, dropped %d; want %+v", in, got, dropped, want)
		}
	}
}

func TestParseProposals_TwoSplitObservationsAndANormalOne(t *testing.T) {
	in := "KIND: finding\nfirst split one here\n\nKIND: note\nsecond split one here\nquestion: and a normal one"
	got, dropped := ParseProposals(in)
	if dropped != 0 || len(got) != 3 || got[0].Kind != "finding" || got[1].Kind != "note" || got[2].Kind != "question" {
		t.Fatalf("got %+v, dropped %d", got, dropped)
	}
}

func TestParseProposals_AKindWithNothingAfterItIsDropped(t *testing.T) {
	got, dropped := ParseProposals("finding\n")
	if len(got) != 0 || dropped != 0 {
		t.Errorf("got %+v, dropped %d", got, dropped)
	}
	got, _ = ParseProposals("finding\nnote: a real one")
	if len(got) != 1 || got[0].Kind != "note" {
		t.Errorf("a kind line followed by another kind line is not a body: %+v", got)
	}
}

// The instructions give an example a model can copy the shape of, not a
// placeholder it copies literally, and rank the evidence.
func TestBuildDraftMessages_AnExampleAndTheOrderOfTrust(t *testing.T) {
	sys := BuildDraftMessages(onePass("User: hi"), "harvey")[0].Content
	if strings.Contains(sys, "KIND:") {
		t.Error("the instructions still show the KIND: placeholder a small model copies literally")
	}
	example := false
	for _, line := range strings.Split(sys, "\n") {
		for _, k := range draftKinds {
			if strings.HasPrefix(line, k+": ") {
				example = true
			}
		}
	}
	if !example {
		t.Error("the instructions show no example line such as 'finding: ...'")
	}
	lower := strings.ToLower(sys)
	if !strings.Contains(lower, "command") || !strings.Contains(lower, "assistant") || !strings.Contains(lower, "wrong") {
		t.Error("the instructions do not say that what the commands showed outranks what the assistant claimed")
	}
}

func TestParseProposals_APendingKindDoesNotLeakIntoLaterLines(t *testing.T) {
	// A kind alone, then a full line, then an unlabelled line: the last belongs to nobody.
	got, _ := ParseProposals("finding\nnote: the full line\nan unlabelled line after it")
	if len(got) != 1 || got[0].Kind != "note" {
		t.Errorf("got %+v", got)
	}
	// A kind alone, then NONE, then a line: nothing is kept.
	got, _ = ParseProposals("finding\nNONE\na stray line")
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestParseProposals_APlaceholderKindMayEndInAPeriod(t *testing.T) {
	got, dropped := ParseProposals("KIND: finding.\nthe text after it")
	if dropped != 0 || len(got) != 1 || got[0].Kind != "finding" || got[0].Body != "the text after it" {
		t.Errorf("got %+v, dropped %d", got, dropped)
	}
}
