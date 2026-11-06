package harvey

import (
	"fmt"
	"strings"
	"testing"
)

// D2 of the observation-drafting plan: split a transcript into passes that fit
// the drafter's window (design decision 3).

func entries(n, bytesEach int) []TranscriptEntry {
	var es []TranscriptEntry
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("s#%04d", i)
		es = append(es, TranscriptEntry{First: id, Last: id, Text: fmt.Sprintf("E%d ", i) + strings.Repeat("x", bytesEach)})
	}
	return es
}

func passTokens(p DraftPass) int {
	n := 0
	for _, e := range p.Entries {
		n += estimateTokens(e.Text)
	}
	return n
}

func TestSplitPasses_ATranscriptThatFitsIsOnePass(t *testing.T) {
	es := entries(5, 40)
	got := SplitPasses(es, 1000)
	if len(got) != 1 || len(got[0].Entries) != 5 || got[0].First != "s#0001" || got[0].Last != "s#0005" {
		t.Fatalf("got %+v", got)
	}
}

func TestSplitPasses_ALargerOneIsTheFewestPassesThatFit(t *testing.T) {
	es := entries(10, 400) // about 100 tokens each
	got := SplitPasses(es, 350)
	if len(got) != 4 { // 3+3+3+1
		t.Fatalf("want 4 passes, got %d", len(got))
	}
	var seen []string
	for _, p := range got {
		if passTokens(p) > 350 {
			t.Errorf("pass %s..%s holds %d tokens, over the budget", p.First, p.Last, passTokens(p))
		}
		for _, e := range p.Entries {
			seen = append(seen, e.First)
		}
	}
	if len(seen) != 10 {
		t.Fatalf("entries lost or repeated: %v", seen)
	}
	for i, id := range seen {
		if id != fmt.Sprintf("s#%04d", i+1) {
			t.Errorf("order broken at %d: %v", i, seen)
			break
		}
	}
}

func TestSplitPasses_FirstAndLastAreTheEntrysEventIds(t *testing.T) {
	es := entries(4, 400)
	es[1].Last = "s#0009" // a tool call that ends at a later event
	es[3].Last = "s#0012"
	got := SplitPasses(es, 250)
	if len(got) < 2 {
		t.Fatalf("want a split, got %+v", got)
	}
	if got[0].First != "s#0001" {
		t.Errorf("first pass starts at %s", got[0].First)
	}
	last := got[len(got)-1]
	if last.Last != "s#0012" {
		t.Errorf("last pass ends at %s, want the last entry's Last s#0012", last.Last)
	}
	for _, p := range got {
		if p.First != p.Entries[0].First || p.Last != p.Entries[len(p.Entries)-1].Last {
			t.Errorf("pass span %s..%s does not match its entries", p.First, p.Last)
		}
	}
}

func TestSplitPasses_AnEntryLargerThanTheBudgetIsCutNotDropped(t *testing.T) {
	es := entries(3, 40)
	es[1].Text = "BIG " + strings.Repeat("y", 8000)
	got := SplitPasses(es, 300)
	n := 0
	var bigSeen bool
	for _, p := range got {
		n += len(p.Entries)
		if passTokens(p) > 300 {
			t.Errorf("pass %s..%s is over the budget: %d", p.First, p.Last, passTokens(p))
		}
		for _, e := range p.Entries {
			if strings.HasPrefix(e.Text, "BIG") {
				bigSeen = true
				if !strings.HasSuffix(e.Text, "…") {
					t.Errorf("a cut entry is not marked: …%q", e.Text[len(e.Text)-10:])
				}
			}
		}
	}
	if n != 3 || !bigSeen {
		t.Fatalf("entries = %d, big seen = %v", n, bigSeen)
	}
}

func TestSplitPasses_NothingIsNoPasses(t *testing.T) {
	if got := SplitPasses(nil, 1000); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestSplitPasses_ATinyBudgetStillMakesProgress(t *testing.T) {
	for _, budget := range []int{1, 0, -5} {
		got := SplitPasses(entries(3, 400), budget)
		n := 0
		for _, p := range got {
			n += len(p.Entries)
		}
		if n != 3 || len(got) != 3 {
			t.Fatalf("budget %d: want 3 entries in 3 passes, got %d in %d", budget, n, len(got))
		}
	}
}

func TestDraftBudget(t *testing.T) {
	b, assumed := DraftBudget(8192)
	if assumed || b != 8192-draftReserveTokens {
		t.Errorf("DraftBudget(8192) = %d, %v", b, assumed)
	}
	b, assumed = DraftBudget(0)
	if !assumed || b != draftFallbackWindow-draftReserveTokens {
		t.Errorf("an unknown window = %d, %v; want the fallback window less the reserve, flagged", b, assumed)
	}
	b, _ = DraftBudget(300) // smaller than the reserve
	if b < draftMinBudget {
		t.Errorf("a tiny window gave a budget of %d, below the floor %d", b, draftMinBudget)
	}
}
