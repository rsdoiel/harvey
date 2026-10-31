package harvey

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
