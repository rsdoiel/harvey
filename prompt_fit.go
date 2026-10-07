package harvey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	anyllmerrors "github.com/mozilla-ai/any-llm-go/errors"
)

// promptSystemShare is the part of a model's prompt limit the system prompt
// may use, as numerator and denominator; the rest is left for the conversation.
const (
	promptSystemShareNum = 6
	promptSystemShareDen = 10
)

// minProjectChars is the least of HARVEY.md worth keeping: below it the layer
// is dropped, since a stub of a few words only costs tokens.
const minProjectChars = 200

// projectTruncMarker ends a HARVEY.md cut short to fit a prompt limit, so the
// model and the reader can see that the text stops there on purpose.
const projectTruncMarker = "\n[HARVEY.md truncated to fit this model's prompt limit]\n"

// agentPreambleCompact is agentPreamble's rules in a few lines, for models whose
// prompt limit leaves no room for the full preamble: auto-write tagged blocks,
// /run hints, no invented output.
const agentPreambleCompact = `You are Harvey, a terminal coding agent. A fenced code block tagged lang:path
(for example go:cmd/x/main.go) is written to that file after your reply, with
confirmation; never tag a block just to show a file. Suggest shell commands as
a /run <command> hint and wait; never invent their output. /read, /search and
/git give files and repo state.
`

/** systemPromptBudget returns the tokens the system prompt may use under a
 * prompt limit: its share of the limit, leaving the rest for the conversation.
 *
 * Parameters:
 *   limit (int) — the model's prompt limit in estimateTokens units; 0 = unknown.
 *
 * Returns:
 *   int — the system prompt's budget; 0 when limit is unknown.
 *
 * Example:
 *   budget := systemPromptBudget(700) // 420
 */
func systemPromptBudget(limit int) int {
	if limit <= 0 {
		return 0
	}
	return limit * promptSystemShareNum / promptSystemShareDen
}

// truncateAtBoundary cuts s to at most max bytes, at the last paragraph break
// in the second half if there is one, else the last line break, else hard.
func truncateAtBoundary(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for _, sep := range []string{"\n\n", "\n"} {
		if i := strings.LastIndex(cut, sep); i > max/2 {
			return cut[:i]
		}
	}
	return cut
}

/** fitSystemPrompt shrinks a system prompt to a token budget, dropping the
 * least essential layer first: the skills catalog, then the tail of HARVEY.md
 * (cut at a paragraph break, marked as cut), then the full preamble for a
 * compact one, and last all of HARVEY.md. The prompt is the agent preamble,
 * HARVEY.md, and the catalog appended by loadSkills.
 *
 * Parameters:
 *   full    (string) — the whole system prompt.
 *   catalog (string) — the skills catalog block at its end; "" if none.
 *   budget  (int)    — tokens allowed, in estimateTokens units; <= 0 = no limit.
 *
 * Returns:
 *   string   — the prompt, unchanged when it fits.
 *   []string — what was cut, for the user; nil when nothing was.
 *
 * Example:
 *   text, notes := fitSystemPrompt(full, catalog, systemPromptBudget(700))
 */
func fitSystemPrompt(full, catalog string, budget int) (string, []string) {
	if budget <= 0 || estimateTokens(full) <= budget {
		return full, nil
	}
	var notes []string
	base := full
	if catalog != "" && strings.HasSuffix(full, "\n\n"+catalog) {
		base = strings.TrimSuffix(full, "\n\n"+catalog)
		notes = append(notes, "dropped the skills catalog")
		if estimateTokens(base) <= budget {
			return base, notes
		}
	}
	preamble, project := "", base
	if strings.HasPrefix(base, agentPreamble) {
		preamble, project = agentPreamble, base[len(agentPreamble):]
	}
	maxChars := budget*4 + 3 // estimateTokens floors, so this many bytes still fit

	if preamble != "" {
		if room := maxChars - len(preamble) - len(projectTruncMarker); room >= minProjectChars {
			notes = append(notes, "truncated HARVEY.md")
			return preamble + truncateAtBoundary(project, room) + projectTruncMarker, notes
		}
		preamble = agentPreambleCompact
		notes = append(notes, "used the compact preamble")
	}
	if room := maxChars - len(preamble) - len(projectTruncMarker); room >= minProjectChars {
		notes = append(notes, "truncated HARVEY.md")
		return preamble + truncateAtBoundary(project, room) + projectTruncMarker, notes
	}
	if project != "" {
		notes = append(notes, "dropped HARVEY.md")
	}
	return truncateAtBoundary(preamble, maxChars), notes
}

// systemPromptText is the system prompt as it should be sent now: the
// configured prompt with its dynamic sections expanded, shrunk to the active
// model's prompt limit, and what was cut to get there.
func (a *Agent) systemPromptText() (string, []string) {
	full := ExpandDynamicSections(a.Config.SystemPrompt, a.Workspace)
	return fitSystemPrompt(full, a.catalogBlock, systemPromptBudget(a.promptTokenLimit()))
}

/** refreshSystemPrompt rewrites the system message in the history to fit the
 * active model's prompt limit, or back to the full prompt when the model has
 * none. It runs at startup and before every turn, so every way of changing
 * model is covered. It does nothing when Config.SystemPrompt is empty, since
 * the system message is then not Harvey's to rewrite. What was cut is
 * reported once, not every turn.
 *
 * Parameters:
 *   out (io.Writer) — destination for the notice.
 *
 * Example:
 *   a.refreshSystemPrompt(os.Stdout)
 */
func (a *Agent) refreshSystemPrompt(out io.Writer) {
	if a.Config.SystemPrompt == "" {
		return
	}
	text, notes := a.systemPromptText()
	for i := range a.History {
		if a.History[i].Role == "system" {
			a.History[i].Content = text
			break
		}
	}
	note := strings.Join(notes, "; ")
	if note == a.promptFitNote {
		return
	}
	a.promptFitNote = note
	if note != "" {
		name := "this model"
		if ac, ok := a.Client.(*AnyLLMClient); ok {
			name = ac.ModelName()
		}
		fmt.Fprintf(out, yellow("  ⚠")+" To fit %s's prompt limit (%d tokens), Harvey %s.\n", name, a.promptTokenLimit(), note)
	}
}

// isPinnedContext reports whether m is the pinned-context message that
// ClearHistory puts at the start of a conversation, which trimming keeps.
func isPinnedContext(m Message) bool {
	return m.Role == "user" && strings.HasPrefix(m.Content, "[pinned context]")
}

/** trimMessages drops the oldest complete turns from history until its
 * estimated size is under limit, always keeping the system message, any pinned
 * context and the latest turn. A turn runs from a user message up to the next
 * one, so tool calls and their results go with the turn that made them. It
 * works on and returns a new slice; the argument is not changed.
 *
 * Parameters:
 *   history ([]Message) — the conversation.
 *   limit   (int)       — tokens, in estimateTokens units; <= 0 = no limit.
 *
 * Returns:
 *   []Message — the trimmed conversation.
 *   int       — how many messages were dropped.
 *
 * Example:
 *   trimmed, n := trimMessages(a.History, 700)
 */
func trimMessages(history []Message, limit int) ([]Message, int) {
	h := append([]Message(nil), history...)
	if limit <= 0 {
		return h, 0
	}
	dropped := 0
	for estimateTokens(HistoryText(h)) >= limit {
		start := -1
		for i, m := range h {
			if m.Role != "system" && !isPinnedContext(m) {
				start = i
				break
			}
		}
		if start < 0 {
			break
		}
		end := -1
		for j := start + 1; j < len(h); j++ {
			if h[j].Role == "user" && !isPinnedContext(h[j]) {
				end = j
				break
			}
		}
		if end < 0 {
			break // only the latest turn is left
		}
		h = append(h[:start:start], h[end:]...)
		dropped += end - start
	}
	return h, dropped
}

/** trimHistoryToBudget trims the history to the active model's prompt limit
 * (see trimMessages) and says what it dropped. It does nothing when the limit
 * is unknown. If the latest turn alone is still over, the caller's
 * promptBudgetError refuses it.
 *
 * Parameters:
 *   out (io.Writer) — destination for the notice naming what was dropped.
 *
 * Example:
 *   a.trimHistoryToBudget(os.Stdout)
 */
func (a *Agent) trimHistoryToBudget(out io.Writer) {
	limit := a.promptTokenLimit()
	if limit <= 0 {
		return
	}
	trimmed, dropped := trimMessages(a.History, limit)
	if dropped == 0 {
		return
	}
	a.History = trimmed
	fmt.Fprintf(out, yellow("  ⚠")+" Trimmed %d earlier message(s) to fit the model's prompt limit (%d tokens).\n", dropped, limit)
}

// minLearnPromptTokens is the smallest failed prompt worth blaming on size: a
// server that fails on a few lines has some other trouble.
const minLearnPromptTokens = 200

/** isPromptTooLargeFailure reports whether err looks like a server failing on
 * a prompt that is too big: a stream cut off, a context-length error, or a
 * provider HTTP 500 (hailo-ollama's answer to an oversize prompt). A server
 * that is not running, a missing model and a cancelled call are not.
 *
 * Parameters:
 *   err (error) — the error a chat call returned.
 *
 * Returns:
 *   bool — true when retrying on a smaller prompt is worth one attempt.
 *
 * Example:
 *   if isPromptTooLargeFailure(err) { retry() }
 */
func isPromptTooLargeFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, ErrStreamTruncated) {
		return true
	}
	var cl *anyllmerrors.ContextLengthError
	if errors.As(err, &cl) {
		return true
	}
	var pe *anyllmerrors.ProviderError
	if errors.As(err, &pe) {
		msg := err.Error()
		if strings.Contains(msg, "not running") || strings.Contains(msg, "refused") {
			return false
		}
		return strings.Contains(msg, "500") || strings.Contains(msg, "Internal Server Error")
	}
	return false
}

// fitMessagesTo returns a copy of the conversation shrunk to a prompt of about
// target tokens: the system prompt fitted to its share of target, then the
// oldest turns dropped. The conversation itself is not changed.
func (a *Agent) fitMessagesTo(target int) []Message {
	msgs := append([]Message(nil), a.History...)
	if a.Config.SystemPrompt != "" {
		full := ExpandDynamicSections(a.Config.SystemPrompt, a.Workspace)
		text, _ := fitSystemPrompt(full, a.catalogBlock, systemPromptBudget(target))
		for i := range msgs {
			if msgs[i].Role == "system" {
				msgs[i].Content = text
				break
			}
		}
	}
	msgs, _ = trimMessages(msgs, target)
	return msgs
}

// promptRetryPercents are the sizes, as a percentage of the prompt that failed,
// that a failed turn is retried at, largest first. A server that rejects an
// oversize prompt answers fast, so a few smaller attempts are cheap; only a
// success is slow.
var promptRetryPercents = []int{70, 45, 25}

/** retryWithSmallerPrompt gives a failed turn further tries on smaller copies
 * of the conversation, when the failure looks like an oversize prompt. Each try
 * is fitted to the next size in promptRetryPercents. When one succeeds, its
 * smaller conversation replaces the history and its size is recorded as the
 * model's prompt limit, since a success proves the cause. If none does,
 * nothing changes and the caller reports the original error. It does nothing
 * for a prompt too small to blame, an error that is not a size failure, or a
 * conversation that cannot be made smaller.
 *
 * Parameters:
 *   ctx (context.Context)  — the turn's context.
 *   err (error)            — the error the first attempt returned.
 *   buf (*strings.Builder) — receives the reply; reset before each retry.
 *   out (io.Writer)        — destination for the notices.
 *
 * Returns:
 *   ChatStats — the successful retry's stats.
 *   bool      — true when a retry succeeded and the turn can carry on.
 *
 * Example:
 *   if st, ok := a.retryWithSmallerPrompt(ctx, err, &buf, out); ok { stats, err = st, nil }
 */
func (a *Agent) retryWithSmallerPrompt(ctx context.Context, err error, buf *strings.Builder, out io.Writer) (ChatStats, bool) {
	if !isPromptTooLargeFailure(err) {
		return ChatStats{}, false
	}
	failed := estimateTokens(HistoryText(a.History))
	if failed < minLearnPromptTokens {
		return ChatStats{}, false
	}
	tried := failed
	for _, pct := range promptRetryPercents {
		target := failed * pct / 100
		msgs := a.fitMessagesTo(target)
		size := estimateTokens(HistoryText(msgs))
		if size >= tried {
			continue // no smaller than the last attempt
		}
		tried = size
		fmt.Fprintf(out, yellow("  ⚠")+" The server failed on a prompt of about %d tokens; retrying with about %d.\n", failed, size)
		buf.Reset()
		stats, err2 := a.Client.Chat(ctx, msgs, buf)
		if err2 != nil {
			buf.Reset()
			if !isPromptTooLargeFailure(err2) {
				return ChatStats{}, false // a different trouble: report the original
			}
			continue
		}
		a.History = msgs
		a.learnPromptLimit(size, out)
		return stats, true
	}
	return ChatStats{}, false
}

// learnPromptLimit records n as the active model's prompt limit when no limit
// is known or n is lower, and says so.
func (a *Agent) learnPromptLimit(n int, out io.Writer) {
	ac, ok := a.Client.(*AnyLLMClient)
	if !ok || a.ModelCache == nil {
		return
	}
	cap, _ := a.ModelCache.Get(ac.ModelName())
	if cap == nil {
		cap = &ModelCapability{Name: ac.ModelName(), ProbeLevel: "none", ProbedAt: time.Now()}
	}
	if cap.MaxPromptTokens > 0 && cap.MaxPromptTokens <= n {
		return
	}
	cap.MaxPromptTokens = n
	if err := a.ModelCache.Set(cap); err != nil {
		return
	}
	a.promptFitNote = "" // so the next turn reports how the prompt is now fitted
	fmt.Fprintf(out, yellow("  ⚠")+" Recorded a prompt limit of %d tokens for %s; change it with /model limit.\n", n, ac.ModelName())
}
