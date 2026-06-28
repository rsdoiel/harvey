package harvey

import "os"

// estimateTokens returns a fast token count estimate using the 4-bytes-per-token
// heuristic. Returns at least 1 so callers can safely divide by the result.
func estimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 {
		n = 1
	}
	return n
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
