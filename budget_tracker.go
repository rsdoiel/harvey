package harvey

// budget_tracker.go — a shared, per-turn token pool threaded through
// ragAugment and injectOrChunk so both consume from (and are aware of) the
// same remaining-context budget, rather than three independent,
// uncoordinated checks. See feedforward-budget-design.md (Direction B,
// Phase A).

/** BudgetTracker is a shared, per-turn token pool. Callers call Reserve
 * before adding content to the turn's prompt; a failed Reserve means "this
 * doesn't fit — skip it and note that it was skipped," not an error.
 *
 * A nil *BudgetTracker is treated by its callers (ragAugment,
 * injectOrChunk) as "unconstrained" — this keeps every call site that
 * doesn't care about budget cutoff behavior unchanged.
 *
 * Fields:
 *   Total (int) — total token budget for this turn.
 *   Used  (int) — tokens reserved so far.
 *
 * Example:
 *   tracker := NewBudgetTracker(remainingContext(a))
 *   augmented, info := a.ragAugment(input, tracker)
 *   augmented = a.injectOrChunk(ctx, augmented, out, tracker)
 */
type BudgetTracker struct {
	Total int
	Used  int
}

/** NewBudgetTracker returns a BudgetTracker with the given total token
 * budget and nothing yet reserved.
 *
 * Parameters:
 *   total (int) — total token budget for this turn.
 *
 * Returns:
 *   *BudgetTracker — a tracker ready for Reserve calls.
 *
 * Example:
 *   tracker := NewBudgetTracker(4096)
 */
func NewBudgetTracker(total int) *BudgetTracker {
	return &BudgetTracker{Total: total}
}

/** Reserve attempts to reserve tokens tokens from the tracker's remaining
 * budget. It succeeds (and increments Used) only when doing so would not
 * exceed Total; a rejected reservation leaves Used unchanged.
 *
 * Parameters:
 *   tokens (int) — number of tokens to reserve.
 *
 * Returns:
 *   bool — true when the reservation fit and was applied; false otherwise.
 *
 * Example:
 *   if !tracker.Reserve(estimateTokens(chunk.Content)) {
 *       // doesn't fit — skip this chunk
 *   }
 */
func (b *BudgetTracker) Reserve(tokens int) bool {
	if b.Used+tokens > b.Total {
		return false
	}
	b.Used += tokens
	return true
}

/** Remaining returns the number of tokens still available in the tracker's
 * budget.
 *
 * Returns:
 *   int — Total minus Used.
 *
 * Example:
 *   rem := tracker.Remaining() // tokens still available
 */
func (b *BudgetTracker) Remaining() int {
	return b.Total - b.Used
}
