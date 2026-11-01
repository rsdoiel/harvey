package harvey

import (
	"regexp"
	"strings"
	"unicode"
)

// D4 of the observation-drafting plan (design/observation-drafting-design.md,
// decision 5): duplicates are found in code, without asking a model.

const (
	// dedupMinWords is the fewest unique words the shorter of two texts needs
	// for overlap to count; below it only equal texts match.
	dedupMinWords = 4

	// dedupShare is the share of the shorter text's words that must occur in
	// the other for a match.
	dedupShare = 0.8

	// dedupRatio is the share of the longer text's words that the shared words
	// must also make up, so a short text inside a much longer one is not a
	// duplicate of it.
	dedupRatio = 0.5
)

var originLine = regexp.MustCompile(`(?im)^[ \t]*Drafted from [^\n]*$`)

/** Normalize is the form two texts are compared in: the origin line removed,
 * lower case, every run of characters that are not letters or digits turned
 * into one space, and the ends trimmed.
 *
 * Parameters:
 *   text (string) — an observation's body.
 *
 * Returns:
 *   string — the normalized text.
 *
 * Example:
 *   Normalize("Needs Go 1.27.3.") // "needs go 1 27 3"
 */
func Normalize(text string) string {
	text = originLine.ReplaceAllString(text, "")
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func wordSet(normalized string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(normalized) {
		set[w] = true
	}
	return set
}

// similar reports whether two normalized texts are the same or overlap enough.
func similar(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	wa, wb := wordSet(a), wordSet(b)
	short, long := len(wa), len(wb)
	if short > long {
		short, long = long, short
	}
	if short < dedupMinWords {
		return false
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	return float64(inter)/float64(short) >= dedupShare && float64(inter)/float64(long) >= dedupRatio
}

/** SimilarTo reports whether a proposal repeats one of the texts already held.
 * Two texts match when they are equal after Normalize, or when at least 80% of
 * the shorter one's words (at least four) occur in the other and those words
 * make up at least half of the longer one's. The origin line never counts.
 *
 * Parameters:
 *   p        (Proposal) — the proposal.
 *   existing ([]string) — the bodies already in kb, in any status.
 *
 * Returns:
 *   string — the existing text it matched, as given; empty when none.
 *   bool   — true when it matched one.
 *
 * Example:
 *   if m, dup := SimilarTo(p, bodies); dup { fmt.Println("same as:", m) }
 */
func SimilarTo(p Proposal, existing []string) (string, bool) {
	np := Normalize(p.Body)
	for _, ex := range existing {
		if similar(np, Normalize(ex)) {
			return ex, true
		}
	}
	return "", false
}

/** DraftSkip is a proposal left out because kb, or an earlier proposal of the
 * same run, already says it.
 *
 * Fields:
 *   Proposal (Proposal) — what was left out.
 *   Matched  (string)   — the text it repeats, as held.
 *
 * Example:
 *   // {Proposal: {finding, "the build needs Go 1.27.3"}, Matched: "the build needs Go 1.27.3 or later"}
 */
type DraftSkip struct {
	Proposal Proposal
	Matched  string
}

/** DedupAgainst drops the proposals that repeat something already held, or an
 * earlier proposal of the same run (the first is kept).
 *
 * Parameters:
 *   proposals []Proposal — in the order written.
 *   existing  []string   — the bodies already in kb, in any status.
 *
 * Returns:
 *   []Proposal  — those to write, in order.
 *   []DraftSkip — those left out, each with what it matched.
 *
 * Example:
 *   kept, skipped := DedupAgainst(props, existingBodies)
 */
func DedupAgainst(proposals []Proposal, existing []string) ([]Proposal, []DraftSkip) {
	var kept []Proposal
	var skipped []DraftSkip
	held := append([]string(nil), existing...)
	for _, p := range proposals {
		if m, dup := SimilarTo(p, held); dup {
			skipped = append(skipped, DraftSkip{Proposal: p, Matched: m})
			continue
		}
		kept = append(kept, p)
		held = append(held, p.Body)
	}
	return kept, skipped
}
