package harvey

import (
	"fmt"
	"strings"
	"testing"
)

// D4 of the observation-drafting plan: drop duplicates in code, without a model
// (design decision 5).

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  Hello,   WORLD!  ":                    "hello world",
		"The build needs Go 1.27.3.":             "the build needs go 1 27 3",
		"tabs\tand\nnewlines":                    "tabs and newlines",
		"kb's recall — now with drafts (0.0.30)": "kb s recall now with drafts 0 0 30",
		"": "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalize_TheOriginLineIsNotPartOfTheText(t *testing.T) {
	a := Normalize("the stream closes\nDrafted from harvey-session-20261030-143612#0042..#0057.")
	b := Normalize("the stream closes")
	if a != b {
		t.Errorf("origin line counted: %q vs %q", a, b)
	}
}

func prop(body string) Proposal { return Proposal{Kind: "finding", Body: body} }

func TestSimilarTo_SameTextAfterNormalizationMatchesEvenWhenShort(t *testing.T) {
	if m, ok := SimilarTo(prop("Go test."), []string{"go   TEST"}); !ok || m != "go   TEST" {
		t.Errorf("got %q, %v", m, ok)
	}
}

func TestSimilarTo_ReorderedWordsMatch(t *testing.T) {
	existing := []string{"hailo closes the stream when the prompt exceeds about 3300 characters"}
	if _, ok := SimilarTo(prop("when the prompt exceeds about 3300 characters hailo closes the stream"), existing); !ok {
		t.Error("the same words in another order should match")
	}
}

func TestSimilarTo_ThresholdIsEightyPercentOfTheShorterTextsWords(t *testing.T) {
	existing := []string{"alpha beta gamma delta epsilon zeta eta theta iota kappa"}
	// 8 of 10 words shared: matches.
	if _, ok := SimilarTo(prop("alpha beta gamma delta epsilon zeta eta theta lambda mu"), existing); !ok {
		t.Error("8 of 10 shared words should match")
	}
	// 7 of 10: does not.
	if m, ok := SimilarTo(prop("alpha beta gamma delta epsilon zeta eta nu lambda mu"), existing); ok {
		t.Errorf("7 of 10 shared words matched %q", m)
	}
}

func TestSimilarTo_AReworderedFindingIsKept(t *testing.T) {
	existing := []string{"hailo-ollama closes the stream when the system prompt is long"}
	if m, ok := SimilarTo(prop("qwen2.5-coder:1.5b accepts the same prompt that llama3.2:3b rejects"), existing); ok {
		t.Errorf("a different finding matched %q", m)
	}
}

func TestSimilarTo_ShortTextsMatchOnlyWhenEqual(t *testing.T) {
	existing := []string{"go test passes in the harvey directory with the new files"}
	if m, ok := SimilarTo(prop("go test passes"), existing); ok {
		t.Errorf("a three-word text matched %q by overlap", m)
	}
}

func TestSimilarTo_AShortTextInsideAMuchLongerOneIsNotADuplicate(t *testing.T) {
	var filler []string
	for i := 0; i < 30; i++ {
		filler = append(filler, fmt.Sprintf("word%d", i))
	}
	long := strings.Join(filler, " ") + " the stream closes past the limit"
	// Every word of the proposal occurs in the long observation, but the long one says much more.
	if m, ok := SimilarTo(prop("the stream closes past the limit"), []string{long}); ok {
		t.Errorf("a short text matched a long one that merely contains it: %q", m[:30])
	}
}

func TestSimilarTo_TheOriginLineNeverCounts(t *testing.T) {
	origin := "\nDrafted from harvey-session-20261101-100000#0001..#0009."
	if _, ok := SimilarTo(prop("alpha beta gamma delta epsilon"+origin), []string{"one two three four five" + origin}); ok {
		t.Error("two different texts matched through a shared origin line")
	}
	if _, ok := SimilarTo(prop("alpha beta gamma delta epsilon\nDrafted from s#0001."), []string{"alpha beta gamma delta epsilon\nDrafted from s#0099..#0120."}); !ok {
		t.Error("the same text with different origin lines should match")
	}
}

func TestDedupAgainst_SkipsWhatKBHoldsAndNamesIt(t *testing.T) {
	existing := []string{"the build needs Go 1.27.3 or later", "kb must be 0.0.30 or newer for drafts"}
	props := []Proposal{
		prop("harvey drafts observations from the session log"),
		prop("the build needs Go 1.27.3 or later"),
		prop("an unrelated finding about the pipeline step threshold"),
	}
	kept, skipped := DedupAgainst(props, existing)
	if len(kept) != 2 || kept[0].Body != props[0].Body || kept[1].Body != props[2].Body {
		t.Fatalf("kept = %+v", kept)
	}
	if len(skipped) != 1 || skipped[0].Proposal.Body != props[1].Body || skipped[0].Matched != existing[0] {
		t.Fatalf("skipped = %+v", skipped)
	}
}

func TestDedupAgainst_TwoProposalsThatMatchKeepTheFirst(t *testing.T) {
	props := []Proposal{
		prop("the tool result role needs checking on the hailo models"),
		prop("check the tool result role on the hailo models"),
		prop("something else entirely about completion"),
	}
	kept, skipped := DedupAgainst(props, nil)
	if len(kept) != 2 || kept[0].Body != props[0].Body || kept[1].Body != props[2].Body {
		t.Fatalf("kept = %+v", kept)
	}
	if len(skipped) != 1 || skipped[0].Matched != props[0].Body {
		t.Fatalf("skipped = %+v; want the second proposal, matched to the first", skipped)
	}
}

func TestDedupAgainst_NothingToCompareKeepsEverything(t *testing.T) {
	props := []Proposal{prop("one two three four five six"), prop("seven eight nine ten eleven twelve")}
	kept, skipped := DedupAgainst(props, nil)
	if len(kept) != 2 || len(skipped) != 0 {
		t.Errorf("kept %d, skipped %d", len(kept), len(skipped))
	}
	if kept, skipped := DedupAgainst(nil, []string{"x"}); len(kept) != 0 || len(skipped) != 0 {
		t.Errorf("no proposals: kept %d, skipped %d", len(kept), len(skipped))
	}
}

func TestSimilarTo_ThreeWordsAgainstAnAlmostIdenticalFourAreNotMatched(t *testing.T) {
	if m, ok := SimilarTo(prop("go test passes"), []string{"go test passes ok"}); ok {
		t.Errorf("below four words only an equal text matches, got %q", m)
	}
}

func TestSimilarTo_NothingMatchesAnEmptyText(t *testing.T) {
	if _, ok := SimilarTo(prop("!!!"), []string{"", "???", "   "}); ok {
		t.Error("texts with no words matched each other")
	}
	if _, ok := SimilarTo(prop("a real finding about the stream"), []string{""}); ok {
		t.Error("a real text matched an empty one")
	}
}
