package harvey

import (
	"fmt"
	"io"
	"strings"
)

// assemble.go — the context assembler (plan L7b, DR-0036, DR-0048).
//
// The prompt is a view of the session log, not a replay of a.History. The log
// is rebuilt into items (promptrebuild.go), and Assemble chooses which of them
// the model sees: the system prompt, the pinned items, the summaries, the
// recalled knowledge, and a tail of whole turns, in that order, stable first
// so a server's prefix cache survives. It is a pure function of its input.

/** ItemKind says what part an item plays in the assembled prompt.
 *
 * Example:
 *   if it.Kind == ItemPin { keep(it) }
 */
type ItemKind string

// The kinds of prompt item.
const (
	ItemSystem  ItemKind = "system"  // the system prompt
	ItemPin     ItemKind = "pin"     // pinned context the author asked to keep
	ItemSummary ItemKind = "summary" // a summary that stands for earlier turns
	ItemKB      ItemKind = "kb"      // recalled knowledge or an injected kb row
	ItemTurn    ItemKind = "turn"    // dialog, tool rounds and the files read into a turn
)

/** PromptItem is one message of the history with what the assembler needs to
 * know about it.
 *
 * Fields:
 *   Msg   (Message)  — the message as it is sent.
 *   Kind  (ItemKind) — its part in the prompt.
 *   Group (int)      — the turn it belongs to; what the author added before
 *                      typing a line belongs to that line's turn.
 *   Event (string)   — the id of the log event that added it.
 *
 * Example:
 *   items := RebuildItems(events, opts)
 */
type PromptItem struct {
	Msg   Message
	Kind  ItemKind
	Group int
	Event string
}

// classifyItem decides an item's kind from the message and what added it.
func classifyItem(m Message, via string) ItemKind {
	switch {
	case m.Role == "system" && !strings.HasPrefix(m.Content, "[Summary]"):
		return ItemSystem
	case via == "pinned" || strings.HasPrefix(m.Content, "[pinned context]"):
		return ItemPin
	case via == "summary" || strings.HasPrefix(m.Content, "[Conversation summary]") ||
		strings.HasPrefix(m.Content, "[Summary]") || strings.HasPrefix(m.Content, "[Session history compressed"):
		return ItemSummary
	case via == "memory" || via == "kb-inject":
		return ItemKB
	}
	return ItemTurn
}

/** AssembleBudget is what the assembler may spend.
 *
 * Fields:
 *   Limit   (int)     — the most tokens the prompt may be, in estimateTokens
 *                       units; 0 or less means no limit.
 *   KBShare (float64) — the largest share of Limit the recalled knowledge may
 *                       take; 0 means the default, a quarter.
 *
 * Example:
 *   b := AssembleBudget{Limit: 3000}
 */
type AssembleBudget struct {
	Limit   int
	KBShare float64
}

const defaultKBShare = 0.25

/** PromptManifest says what an assembled prompt is made of, by event id, so the log
 * can show what the model saw without storing the prompt (DR-0039).
 *
 * Fields:
 *   System    (string)   — the event that set the system prompt.
 *   Pinned    ([]string) — events of the pinned items kept.
 *   Summaries ([]string) — events of the summaries kept.
 *   KB        ([]string) — events of the recalled knowledge kept.
 *   Tail      (TailSpan) — the turns kept verbatim.
 *   Dropped   ([]string) — events of the items left out.
 *   UsedEst   (int)      — estimated tokens of the assembled prompt.
 *   Limit     (int)      — the budget's limit.
 *
 * Example:
 *   _, m := Assemble(items, AssembleBudget{Limit: 3000})
 *   fmt.Println(len(m.Dropped), "left out")
 */
type PromptManifest struct {
	System    string
	Pinned    []string
	Summaries []string
	KB        []string
	Tail      TailSpan
	Dropped   []string
	UsedEst   int
	Limit     int
	// LimitSource says where Limit came from: "prompt_limit" (the model's
	// learned or seeded cap on the whole prompt) or "window" (a share of its
	// context window). Set by assembleForTurn.
	LimitSource string
}

/** TailSpan is the run of turns kept verbatim: the first and last event of the
 * tail and how many turns it holds.
 *
 * Example:
 *   fmt.Println(m.Tail.Turns)
 */
type TailSpan struct {
	From, To string
	Turns    int
}

// itemTokens estimates the tokens of one message.
func itemTokens(m Message) int {
	n := estimateTokens(m.Content)
	for _, tc := range m.ToolCalls {
		n += estimateTokens(tc.Function.Name + tc.Function.Arguments)
	}
	return n
}

/** Assemble chooses the prompt: the system prompt, the pins, the summaries,
 * the recalled knowledge and a tail of whole turns, in that order. Over the
 * limit it drops the oldest recalled knowledge beyond its share, then the rest
 * of the recalled knowledge, then the oldest turns (a turn is never split, and
 * the latest is always kept). System prompt, pins
 * and summaries are never dropped here; a prompt that still does not fit is the
 * caller's to refuse. The result is deterministic.
 *
 * Parameters:
 *   items  ([]PromptItem)  — the history, oldest first.
 *   budget (AssembleBudget) — what the prompt may cost.
 *
 * Returns:
 *   []Message — the prompt.
 *   PromptManifest  — what it is made of and what was left out.
 *
 * Example:
 *   msgs, manifest := Assemble(items, AssembleBudget{Limit: 3000})
 */
func Assemble(items []PromptItem, budget AssembleBudget) ([]Message, PromptManifest) {
	var system, pins, summaries, kb, tail []PromptItem
	for _, it := range items {
		switch it.Kind {
		case ItemSystem:
			system = append(system, it)
		case ItemPin:
			pins = append(pins, it)
		case ItemSummary:
			summaries = append(summaries, it)
		case ItemKB:
			kb = append(kb, it)
		default:
			tail = append(tail, it)
		}
	}
	var dropped []string
	drop := func(it PromptItem) { dropped = append(dropped, it.Event) }
	tokens := func(list []PromptItem) int {
		n := 0
		for _, it := range list {
			n += itemTokens(it.Msg)
		}
		return n
	}
	total := func() int { return tokens(system) + tokens(pins) + tokens(summaries) + tokens(kb) + tokens(tail) }

	if budget.Limit > 0 {
		share := budget.KBShare
		if share <= 0 {
			share = defaultKBShare
		}
		for len(kb) > 0 && float64(tokens(kb)) > share*float64(budget.Limit) {
			drop(kb[0])
			kb = kb[1:]
		}
		for total() > budget.Limit {
			if len(kb) > 0 { // recalled knowledge goes before any dialog
				drop(kb[0])
				kb = kb[1:]
				continue
			}
			if len(tail) == 0 || tail[len(tail)-1].Group == tail[0].Group {
				break // only the latest turn is left
			}
			g := tail[0].Group
			for len(tail) > 0 && tail[0].Group == g {
				drop(tail[0])
				tail = tail[1:]
			}
		}
	}

	var out []Message
	m := PromptManifest{Dropped: dropped, Limit: budget.Limit}
	add := func(list []PromptItem, ids *[]string) {
		for _, it := range list {
			out = append(out, it.Msg)
			if ids != nil {
				*ids = append(*ids, it.Event)
			}
		}
	}
	add(system, nil)
	if len(system) > 0 {
		m.System = system[0].Event
	}
	add(pins, &m.Pinned)
	add(summaries, &m.Summaries)
	add(kb, &m.KB)
	add(tail, nil)
	if len(tail) > 0 {
		m.Tail.From, m.Tail.To = tail[0].Event, tail[len(tail)-1].Event
		seen := map[int]bool{}
		for _, it := range tail {
			seen[it.Group] = true
		}
		m.Tail.Turns = len(seen)
	}
	m.UsedEst = tokens(system) + tokens(pins) + tokens(summaries) + tokens(kb) + tokens(tail)
	return out, m
}

// windowShare is the share of the context window a prompt may fill when the
// model's prompt limit is not known; the rest is left for the reply. To be
// tuned on the Pi (DR-0039 left the reserve to be measured).
const windowShare = 0.85

/** assembleLimit returns the most tokens the prompt may be for the active
 * model, and where that figure came from: the model's prompt limit when it is
 * known, otherwise a share of its context window, otherwise nothing.
 *
 * Returns:
 *   int    — tokens, 0 when neither is known.
 *   string — "prompt_limit", "window" or "".
 *
 * Example:
 *   limit, source := a.assembleLimit()
 */
func (a *Agent) assembleLimit() (int, string) {
	if n := a.promptTokenLimit(); n > 0 {
		return n, "prompt_limit"
	}
	if w := a.effectiveContextLimit(); w > 0 {
		return int(float64(w) * windowShare), "window"
	}
	return 0, ""
}

/** assembleForTurn chooses the prompt for the turn that is starting, from the
 * session log, and makes it the history. It is what trimHistoryToBudget was:
 * with the active model's prompt limit known and the history over it, the
 * oldest whole turns are left out; now it is also ordered system, pins,
 * summaries, knowledge, tail, and the manifest of what it chose is kept for the
 * request that follows. Whenever the log cannot reproduce the history exactly
 * (no log, a prompt that held a redacted secret, a read error) or the limit is
 * unknown it does what trimHistoryToBudget does.
 *
 * Parameters:
 *   out (io.Writer) — destination for the notice naming what was left out.
 *
 * Example:
 *   a.assembleForTurn(os.Stdout)
 */
func (a *Agent) assembleForTurn(out io.Writer) {
	a.lastManifest = nil
	limit, source := a.assembleLimit()
	if limit <= 0 || a.SessionLog == nil {
		a.trimHistoryToBudget(out)
		return
	}
	path := a.SessionLog.Path()
	events, _, err := ReadSessionLog(path)
	if err != nil {
		a.trimHistoryToBudget(out)
		return
	}
	items := RebuildItems(events, RenderOptions{Blobs: BlobDir(path)})
	msgs := make([]Message, len(items))
	for i, it := range items {
		msgs[i] = it.Msg
	}
	if promptHash(msgs) != promptHash(a.History) {
		a.trimHistoryToBudget(out)
		return
	}
	assembled, man := Assemble(items, AssembleBudget{Limit: limit})
	man.LimitSource = source
	a.lastManifest = &man
	if promptHash(assembled) == promptHash(a.History) {
		return
	}
	before := a.historySnapshot()
	a.History = assembled
	a.logRewrite("assemble", before)
	if n := len(man.Dropped); n > 0 {
		fmt.Fprintf(out, yellow("  ⚠")+" Trimmed %d earlier message(s) to fit the model's prompt limit (%d tokens).\n", n, limit)
	}
}
