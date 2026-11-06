package harvey

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// assemble_summary.go — the assembler's summaries (plan L7b, DR-0036).
//
// When the conversation nears the limit, the turns the assembler would drop
// next are summarised by the active model and replaced, in the history, by one
// summary. The summary is a compaction event that carries its text and names
// the events it stands for, so what was left out is still in the log. It
// replaces the rolling summary (memory_rolling.go).

// summaryBackoffTurns is how many turns to wait after a failed summary before
// trying again.
const summaryBackoffTurns = 3

// summaryTokenShare is the share of the prompt limit a summary is asked to stay
// within.
const summaryTokenShare = 0.10

/** summarizeForNextTurn runs after a reply. If the conversation has reached
 * the rolling-summary threshold of the prompt limit, it summarises every turn
 * older than the last KeepTurns, together with any earlier summary, with the
 * active model, and replaces them with the new summary. A failed call leaves
 * the history as it was, says so once, and is not retried for a few turns; the
 * assembler then drops the oldest turns as it would have. It does nothing
 * without a session log, a backend, a known limit or the setting enabled, or
 * when the log cannot reproduce the history exactly.
 *
 * Parameters:
 *   ctx (context.Context) — the turn's context.
 *   out (io.Writer)       — destination for the notices.
 *
 * Example:
 *   a.summarizeForNextTurn(ctx, os.Stdout)
 */
func (a *Agent) summarizeForNextTurn(ctx context.Context, out io.Writer) {
	cfg := a.Config.Memory.RollingSummary
	limit, _ := a.assembleLimit()
	if !cfg.Enabled || a.Client == nil || a.SessionLog == nil || limit <= 0 || a.logTurn < a.summaryRetryAfter {
		return
	}
	path := a.SessionLog.Path()
	events, _, err := ReadSessionLog(path)
	if err != nil {
		return
	}
	items := RebuildItems(events, RenderOptions{Blobs: BlobDir(path)})
	if !sameMessages(items, a.History) {
		return
	}
	used := 0
	for _, it := range items {
		used += itemTokens(it.Msg)
	}
	if !ShouldCompress(used, limit, cfg.WarnAtPct) {
		return
	}

	// The turns to summarise: every turn but the last KeepTurns, and the
	// summaries already made, which the new one absorbs.
	last := 0
	for _, it := range items {
		if it.Kind == ItemTurn && it.Group > last {
			last = it.Group
		}
	}
	keep := cfg.KeepTurns
	if keep < 1 {
		keep = 1
	}
	cut := last - keep + 1 // groups below cut are summarised
	var old []PromptItem
	var rest []PromptItem
	for _, it := range items {
		switch {
		case it.Kind == ItemSummary, it.Kind == ItemTurn && it.Group < cut:
			old = append(old, it)
		default:
			rest = append(rest, it)
		}
	}
	turnsOld := 0
	for _, it := range old {
		if it.Kind == ItemTurn {
			turnsOld++
		}
	}
	if turnsOld == 0 {
		return
	}

	text, err := a.writeSummary(ctx, old, int(float64(limit)*summaryTokenShare))
	if err != nil {
		a.summaryRetryAfter = a.logTurn + summaryBackoffTurns
		a.logEvent(KindFault, LevelInfo, map[string]any{
			"source": "summary", "severity": "warning", "message": a.logContent(err.Error()),
		})
		fmt.Fprintf(out, yellow("  ⚠")+" Could not summarise the older turns (%v); the oldest will be left out instead.\n", err)
		return
	}

	var sources []string
	seen := map[string]bool{}
	supersedes := ""
	for _, it := range old {
		if it.Event != "" && !seen[it.Event] {
			seen[it.Event] = true
			sources = append(sources, it.Event)
		}
		if it.Kind == ItemSummary {
			supersedes = it.Event
		}
	}
	summary := PromptItem{Msg: Message{Role: "user", Content: "[Conversation summary]\n\n" + text}, Kind: ItemSummary}
	msgs, _ := Assemble(append([]PromptItem{summary}, rest...), AssembleBudget{})
	before := a.historySnapshot()
	a.History = msgs
	extra := map[string]any{"sources": sources, "summary": a.logContent(summary.Msg.Content)}
	if supersedes != "" {
		extra["supersedes"] = supersedes
	}
	a.logRewriteWith("summary", before, extra)
	a.sessionCompressed = true
	fmt.Fprintln(out, dim(fmt.Sprintf("  [context ~%d%% full — summarised %d older turn(s)]", used*100/limit, turnsOld)))
}

// sameMessages reports whether the items are exactly the history.
func sameMessages(items []PromptItem, h []Message) bool {
	msgs := make([]Message, len(items))
	for i, it := range items {
		msgs[i] = it.Msg
	}
	return promptHash(msgs) == promptHash(h)
}

// writeSummary asks the active model to summarise the items in about budget
// tokens. The request is logged as an ad-hoc call with purpose "summary".
func (a *Agent) writeSummary(ctx context.Context, items []PromptItem, budget int) (string, error) {
	var dialogue strings.Builder
	for _, it := range items {
		m := it.Msg
		switch {
		case m.Content != "":
			fmt.Fprintf(&dialogue, "%s: %s\n\n", m.Role, m.Content)
		case len(m.ToolCalls) > 0:
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&dialogue, "assistant called %s(%s)\n\n", tc.Function.Name, tc.Function.Arguments)
			}
		}
	}
	if budget < 60 {
		budget = 60
	}
	req := []Message{
		{Role: "system", Content: fmt.Sprintf("You are a summariser. Summarise the following conversation in at most %d tokens. Keep decisions made, files changed, errors resolved, constraints the user set and anything still open. An earlier summary may be included; fold it into yours.", budget)},
		{Role: "user", Content: dialogue.String()},
	}
	var buf strings.Builder
	sctx := withCallPurpose(ctx, "summary", map[string]any{"turns": len(items)})
	if _, err := a.Client.Chat(sctx, req, &buf); err != nil {
		return "", err
	}
	text := strings.TrimSpace(buf.String())
	if text == "" {
		return "", fmt.Errorf("the model returned an empty summary")
	}
	return text, nil
}

/** ShouldCompress reports whether the conversation history has grown large
 * enough to trigger rolling summary compression. Returns true when
 * historyTokens >= contextLen * warnAtPct. Returns false when contextLen or
 * warnAtPct is zero or negative.
 *
 * Parameters:
 *   historyTokens (int)     — estimated token count of the full history.
 *   contextLen    (int)     — model's context window size in tokens.
 *   warnAtPct     (float64) — fraction of contextLen at which to compress
 *                             (e.g. 0.80 = 80%).
 *
 * Returns:
 *   bool — true when compression is needed.
 *
 * Example:
 *   if ShouldCompress(1640, 2048, 0.80) {
 *       // summarise all but the last few turns
 *   }
 */
func ShouldCompress(historyTokens, contextLen int, warnAtPct float64) bool {
	if contextLen <= 0 || warnAtPct <= 0 {
		return false
	}
	return float64(historyTokens) >= float64(contextLen)*warnAtPct
}
