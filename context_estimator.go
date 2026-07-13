package harvey

import (
	"context"
	"fmt"
	"os"
)

// estimateTokens returns a fast token count estimate using the 4-bytes-per-token
// heuristic. Returns at least 1 so callers can safely divide by the result.
func estimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 {
		n = 1
	}
	return n
}

/** contextUsage returns the agent's current context-window usage: the token
 * count already in history and the model's effective context limit. It is
 * the single accessor shared by the turn-time warning (runChatTurn) and
 * /status (cmdStatus), replacing three previously independent copies of this
 * calculation.
 *
 * limit always comes from effectiveContextLimit(), including its
 * llamafile-entry and ModelCache fallbacks — not a direct read of
 * a.Config.Ollama.ContextLength — so every caller agrees on what "the limit"
 * is regardless of whether harvey.yaml sets ollama.context_length explicitly
 * (2026-07-12 decision).
 *
 * used is an exact, server-reported token count when the active client is
 * Ollama (via CountTokens' /api/tokenize call); otherwise it is the chars/4
 * heuristic applied to the whole concatenated history in one call, matching
 * /status's pre-existing approach rather than summing estimateTokens per
 * message (2026-07-12 decision — per-message summation over-counts whenever
 * history has many short/compacted messages, since estimateTokens floors at
 * a minimum of 1 token per call).
 *
 * Returns:
 *   used  (int)  — tokens already in history.
 *   limit (int)  — effective context window size; 0 when unknown.
 *   exact (bool) — true when used came from a real tokenizer call.
 *
 * Example:
 *   used, limit, exact := agent.contextUsage()
 *   if limit > 0 && used*100/limit >= 80 { fmt.Println("context getting full") }
 */
func (a *Agent) contextUsage() (used, limit int, exact bool) {
	limit = a.effectiveContextLimit()
	if ac, ok := a.Client.(*AnyLLMClient); ok && ac.ProviderName() == "ollama" {
		n, ex := CountTokens(context.Background(), ac.BackendURL(), ac.ModelName(), HistoryText(a.History))
		return n, limit, ex
	}
	return estimateTokens(HistoryText(a.History)), limit, false
}

// contextTier classifies token usage relative to the effective context
// limit, used to decide whether/how loudly to warn.
type contextTier int

const (
	contextOK contextTier = iota
	contextWarn
	contextFull
)

/** formatContextUsage computes a usage tier and, for the warn/full tiers,
 * the warning line to print — the single place the ≥80%/≥100% thresholds
 * and their wording exist, shared by every caller of contextUsage().
 *
 * Parameters:
 *   used  (int)  — tokens already in history.
 *   limit (int)  — effective context window size; <= 0 means unknown.
 *   exact (bool) — true when used is a real tokenizer count, not an estimate.
 *
 * Returns:
 *   contextTier — contextOK, contextWarn, or contextFull.
 *   string      — warning line for contextWarn/contextFull; "" for contextOK.
 *
 * Example:
 *   tier, msg := formatContextUsage(used, limit, exact)
 *   if tier != contextOK { fmt.Fprintln(out, msg) }
 */
func formatContextUsage(used, limit int, exact bool) (contextTier, string) {
	if limit <= 0 {
		return contextOK, ""
	}
	qualifier := "~"
	if exact {
		qualifier = ""
	}
	pct := used * 100 / limit
	switch {
	case pct >= 100:
		return contextFull, fmt.Sprintf(
			"Context full: %s%d / %d tokens (%d%%) — reply may be truncated; try /clear or switch to a model with larger context",
			qualifier, used, limit, pct)
	case pct >= 80:
		return contextWarn, fmt.Sprintf("Context %d%% full: %s%d / %d tokens", pct, qualifier, used, limit)
	default:
		return contextOK, ""
	}
}

/** remainingContext returns the estimated number of tokens available for new
 * content given the agent's current state. It subtracts the token cost of all
 * messages in history (which includes the system prompt when set) and a 10%
 * safety margin from the model's effective context limit.
 *
 * Returns 0 when the effective context limit is unknown or when the estimated
 * usage already meets or exceeds the limit.
 *
 * Parameters:
 *   a (*Agent) — the agent whose history and config are inspected.
 *
 * Returns:
 *   int — estimated tokens remaining; 0 when limit is unknown or exhausted.
 *
 * Example:
 *   rem := remainingContext(agent)
 *   if rem < 2000 { fmt.Println("context running low") }
 */
func remainingContext(a *Agent) int {
	limit := a.effectiveContextLimit()
	if limit <= 0 {
		return 0
	}
	safetyMargin := limit / 10
	used := 0
	for _, m := range a.History {
		used += estimateTokens(m.Content)
	}
	remaining := limit - used - safetyMargin
	if remaining < 0 {
		return 0
	}
	return remaining
}

/** stmWarnNudge returns a brief reminder string when the agent's remaining
 * context falls below the configured STMWarnPct fraction of the total limit.
 * It returns an empty string when the limit is unknown, STMWarnPct is zero,
 * or context is still ample. The returned string is intended to be appended
 * to the current user message so the model sees it as a meta-instruction.
 *
 * Parameters:
 *   a (*Agent) — the agent whose history and config are inspected.
 *
 * Returns:
 *   string — nudge text, or "" when no nudge is needed.
 *
 * Example:
 *   augmented += stmWarnNudge(a)
 */
func stmWarnNudge(a *Agent) string {
	pct := a.Config.Chunking.STMWarnPct
	if pct <= 0 {
		return ""
	}
	limit := a.effectiveContextLimit()
	if limit <= 0 {
		return ""
	}
	rem := remainingContext(a)
	if rem <= 0 || rem >= int(float64(limit)*pct) {
		return ""
	}
	return fmt.Sprintf(
		"\n\n[Harvey: context is nearly full — approximately %d tokens remaining (<%d%% of limit). "+
			"If the summary_context tool is available, invoke it now to compress conversation history.]",
		rem, int(pct*100))
}

/** fileExceedsBudget reports whether a file's estimated token cost exceeds the
 * given budget. It uses os.Stat to read the file size without opening the
 * file. The token estimate is size/4 (same heuristic as estimateTokens).
 *
 * Parameters:
 *   path   (string) — path to the file to check.
 *   budget (int)    — token budget to compare against.
 *
 * Returns:
 *   bool  — true if the estimated token cost exceeds budget.
 *   int64 — raw file size in bytes (0 on error).
 *   error — os.Stat error, or nil.
 *
 * Example:
 *   over, size, err := fileExceedsBudget("report.md", remainingContext(agent))
 *   if over { fmt.Printf("file is %d bytes, too large\n", size) }
 */
func fileExceedsBudget(path string, budget int) (bool, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0, err
	}
	size := info.Size()
	estimated := int(size / 4)
	return estimated > budget, size, nil
}

/** systemPromptTokenEstimate returns an estimated token count for the
 * current system-prompt message in history, padded 20% over the raw
 * chars/4 heuristic. The pad reflects a measured undercount: an audit of
 * Harvey's actual system prompt (agentPreamble + HARVEY.md + skills
 * catalog) found chars/4 gave ~2826 tokens against a real, server-measured
 * 3372 for the same text (see cold-start-latency-findings.md Addendum,
 * 2026-07-04) — dense, hyphenated/XML-tagged technical text tokenizes worse
 * than the heuristic assumes. Padding trades a slightly pessimistic
 * estimate for fewer missed overflows.
 *
 * Returns:
 *   int — padded estimated token count; 0 when no system message is set.
 *
 * Example:
 *   n := agent.systemPromptTokenEstimate()
 */
func (a *Agent) systemPromptTokenEstimate() int {
	for _, m := range a.History {
		if m.Role == "system" {
			return estimateTokens(m.Content) * 6 / 5
		}
	}
	return 0
}

/** systemPromptExceedsContext reports whether an estimated system-prompt
 * token count leaves no room in a model's context window, returning an
 * actionable error describing the mismatch and how to resolve it. A prompt
 * that exactly equals the limit is treated as exceeding it, since that
 * leaves zero tokens for any reply.
 *
 * Parameters:
 *   modelName (string) — name shown in the error message.
 *   n         (int)    — estimated system-prompt token count.
 *   limit     (int)    — model's context window size in tokens; <= 0 means unknown.
 *
 * Returns:
 *   error — describes the mismatch and remedies; nil when it fits or limit is unknown.
 *
 * Example:
 *   if err := systemPromptExceedsContext("OpenELM-3B", 3372, 2048); err != nil {
 *       fmt.Println(err)
 *   }
 */
func systemPromptExceedsContext(modelName string, n, limit int) error {
	if limit <= 0 || n < limit {
		return nil
	}
	return fmt.Errorf("system prompt (~%d tokens) exceeds %s's context window (%d tokens) — "+
		"switch to a model with a larger context, or shorten HARVEY.md / reduce the number "+
		"of registered skills in agents/skills/", n, modelName, limit)
}
