package harvey

import (
	"context"
	"fmt"
	"io"
	"strings"
)

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
 *       CompressHistory(agent, 6, os.Stdout)
 *   }
 */
func ShouldCompress(historyTokens, contextLen int, warnAtPct float64) bool {
	if contextLen <= 0 || warnAtPct <= 0 {
		return false
	}
	return float64(historyTokens) >= float64(contextLen)*warnAtPct
}

/** CompressHistory replaces all but the last keepTurns conversation turns with
 * a single synthetic summary message produced by the current LLM. The system
 * prompt (if present) is preserved at the front. The compression call is not
 * recorded in the session file; existing recording entries are unaffected.
 *
 * Parameters:
 *   a         (*Agent)   — the Harvey agent whose History is compressed.
 *   keepTurns (int)      — number of recent turns to keep verbatim.
 *   out       (io.Writer) — status output (unused in current implementation;
 *                           callers print the warning line before calling).
 *
 * Returns:
 *   error — if the LLM summarisation call fails.
 *
 * Example:
 *   if ShouldCompress(tokens, limit, 0.80) {
 *       fmt.Fprintln(out, "[context ~82% full — compressing older turns]")
 *       CompressHistory(agent, 6, out)
 *   }
 */
func CompressHistory(a *Agent, keepTurns int, out io.Writer) error {
	if a.Client == nil {
		return nil
	}

	// Separate system prompt from conversation turns.
	var systemMsg *Message
	turns := a.History
	if len(turns) > 0 && turns[0].Role == "system" {
		msg := turns[0]
		systemMsg = &msg
		turns = turns[1:]
	}

	if len(turns) <= keepTurns {
		return nil
	}

	olderTurns := turns[:len(turns)-keepTurns]
	recentTurns := turns[len(turns)-keepTurns:]

	// Format older turns as plain dialogue for the summariser.
	var olderText strings.Builder
	for _, m := range olderTurns {
		if m.Content != "" {
			fmt.Fprintf(&olderText, "%s: %s\n\n", m.Role, m.Content)
		}
	}
	if olderText.Len() == 0 {
		return nil
	}

	// Non-recorded LLM call: summarise the older turns.
	summaryMessages := []Message{
		{
			Role:    "system",
			Content: "You are a summariser. Summarise the following conversation history in at most 150 tokens. Focus on decisions made, files changed, errors resolved, and context the user provided.",
		},
		{
			Role:    "user",
			Content: olderText.String(),
		},
	}

	var buf strings.Builder
	if _, err := a.Client.Chat(context.Background(), summaryMessages, &buf); err != nil {
		return fmt.Errorf("compress: summarise: %w", err)
	}
	summary := strings.TrimSpace(buf.String())
	if summary == "" {
		return nil
	}

	// Rebuild history: [system?] + [compressed summary] + [recent turns].
	synth := Message{
		Role:    "user",
		Content: "[Session history compressed — summary: " + summary + "]",
	}
	newHistory := make([]Message, 0, 1+1+len(recentTurns))
	if systemMsg != nil {
		newHistory = append(newHistory, *systemMsg)
	}
	newHistory = append(newHistory, synth)
	newHistory = append(newHistory, recentTurns...)
	a.History = newHistory
	return nil
}
