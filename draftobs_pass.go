package harvey

// D2 of the observation-drafting plan (design/observation-drafting-design.md,
// decision 3): a session larger than the drafter's window is read in passes.

const (
	/** draftReserveTokens is what a drafting pass keeps back from the drafter's
	 * window for the instructions and the model's reply.
	 */
	draftReserveTokens = 700

	/** draftFallbackWindow is the window a pass assumes when the drafter's is
	 * not known, in tokens. The report says it was assumed.
	 */
	draftFallbackWindow = 2048

	/** draftMinBudget is the least a pass will carry, in tokens, however small
	 * the window.
	 */
	draftMinBudget = 256
)

/** DraftPass is one model call's worth of transcript.
 *
 * Fields:
 *   First   (string)            — id of the first event in the pass.
 *   Last    (string)            — id of the last event in the pass.
 *   Entries ([]TranscriptEntry) — the entries, in log order.
 *
 * Example:
 *   // {First: "s#0001", Last: "s#0014", Entries: [...]}
 */
type DraftPass struct {
	First   string
	Last    string
	Entries []TranscriptEntry
}

/** DraftBudget is how many tokens of transcript a pass may carry for a drafter
 * whose context window is window tokens: the window less draftReserveTokens,
 * never below draftMinBudget.
 *
 * Parameters:
 *   window (int) — the drafter's window in tokens, or 0 or less when unknown.
 *
 * Returns:
 *   int  — the budget in tokens.
 *   bool — true when the window was unknown and draftFallbackWindow was used.
 *
 * Example:
 *   budget, assumed := DraftBudget(a.effectiveContextLimit())
 */
func DraftBudget(window int) (int, bool) {
	assumed := false
	if window <= 0 {
		window, assumed = draftFallbackWindow, true
	}
	b := window - draftReserveTokens
	if b < draftMinBudget {
		b = draftMinBudget
	}
	return b, assumed
}

/** SplitPasses splits a transcript into the fewest passes that each fit
 * budget tokens, keeping log order and never splitting an entry. An entry
 * larger than the budget is cut to fit and marked, not dropped.
 *
 * Parameters:
 *   entries []TranscriptEntry — from DraftTranscript.
 *   budget  int               — tokens one pass may carry (see DraftBudget).
 *
 * Returns:
 *   []DraftPass — in order; empty when there are no entries.
 *
 * Example:
 *   budget, _ := DraftBudget(4096)
 *   for _, p := range SplitPasses(DraftTranscript(events, nil), budget) { ... }
 */
func SplitPasses(entries []TranscriptEntry, budget int) []DraftPass {
	var passes []DraftPass
	var cur []TranscriptEntry
	used := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		passes = append(passes, DraftPass{First: cur[0].First, Last: cur[len(cur)-1].Last, Entries: cur})
		cur, used = nil, 0
	}
	for _, e := range entries {
		cost := estimateTokens(e.Text)
		if cost > budget {
			// estimateTokens is bytes/4, so budget*4 bytes fit; leave room for the marker.
			max := budget*4 - 4
			if max < 0 {
				max = 0
			}
			e.Text = cutMarked(e.Text, max)
			cost = estimateTokens(e.Text)
		}
		if used+cost > budget {
			flush()
		}
		cur = append(cur, e)
		used += cost
	}
	flush()
	return passes
}
