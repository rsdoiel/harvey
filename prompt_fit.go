package harvey

import (
	"fmt"
	"io"
	"strings"
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

/** trimHistoryToBudget drops the oldest complete turns from the history until
 * the prompt is under the active model's limit, always keeping the system
 * message, any pinned context and the latest turn. A turn runs from a user
 * message up to the next one, so tool calls and their results go with the
 * turn that made them. It does nothing when the limit is unknown. If the
 * latest turn alone is still over, the caller's promptBudgetError refuses it.
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
	dropped := 0
	for estimateTokens(HistoryText(a.History)) >= limit {
		start := -1
		for i, m := range a.History {
			if m.Role != "system" && !isPinnedContext(m) {
				start = i
				break
			}
		}
		if start < 0 {
			break
		}
		end := -1
		for j := start + 1; j < len(a.History); j++ {
			if a.History[j].Role == "user" && !isPinnedContext(a.History[j]) {
				end = j
				break
			}
		}
		if end < 0 {
			break // only the latest turn is left
		}
		kept := make([]Message, 0, len(a.History)-(end-start))
		kept = append(kept, a.History[:start]...)
		a.History = append(kept, a.History[end:]...)
		dropped += end - start
	}
	if dropped > 0 {
		fmt.Fprintf(out, yellow("  ⚠")+" Trimmed %d earlier message(s) to fit the model's prompt limit (%d tokens).\n", dropped, limit)
	}
}
