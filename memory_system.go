package harvey

/** MemorySystem owns the complete lifecycle of Harvey's memory subsystems.
 * Open it once per session via OpenMemory; close it on session exit.
 *
 * Fields:
 *   Store   (*MemoryStore)   — session-scoped memory store; nil if open failed.
 *   Unified (*UnifiedMemory) — retrieval layer over Store; nil if Store is nil.
 *
 * Example:
 *   ms, err := OpenMemory(ws, &cfg.Memory)
 *   defer ms.Close()
 */
type MemorySystem struct {
	Store   *MemoryStore
	Unified *UnifiedMemory
}

/** OpenMemory initializes all memory subsystems in dependency order.
 * Returns a non-nil *MemorySystem even on partial failure — Store and Unified
 * are nil when the store could not be opened, and callers must nil-check them.
 *
 * Parameters:
 *   ws  (*Workspace)   — workspace whose agents/memories directory is used.
 *   cfg (*MemoryConfig) — memory configuration (top-K, budget, RAG settings).
 *
 * Returns:
 *   *MemorySystem — never nil.
 *   error         — non-nil if the store could not be opened.
 *
 * Example:
 *   ms, err := OpenMemory(ws, &cfg.Memory)
 *   if err != nil {
 *       log.Printf("memory unavailable: %v", err)
 *   }
 *   defer ms.Close()
 */
func OpenMemory(ws *Workspace, cfg *MemoryConfig) (*MemorySystem, error) {
	ms := &MemorySystem{}
	store, err := NewMemoryStore(ws)
	if err != nil {
		return ms, err
	}
	ms.Store = store
	ms.Unified = NewUnifiedMemory(store, cfg, ws)
	return ms, nil
}

/** Close shuts down all memory subsystems. Safe to call on a nil receiver or
 * when the store was never successfully opened.
 *
 * Returns:
 *   error — first non-nil error encountered during shutdown.
 *
 * Example:
 *   defer ms.Close()
 */
func (ms *MemorySystem) Close() error {
	if ms == nil || ms.Store == nil {
		return nil
	}
	return ms.Store.Close()
}
