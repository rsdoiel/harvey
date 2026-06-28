# Agentic Memory Tools — Implementation Plan

## Source design

See `agentic-memory-design.md` and arXiv:2601.01885v2 (`memory-models/2601.01885v2.pdf`).

## Work items

### M0 — Proactive STM warning threshold

**Effort:** ~1 hour  
**Files:** `terminal.go`, `config.go`, `config_yaml.go`

Add a check in `runChatTurn` (or immediately before the LLM call): if `remainingContext(a)` is below `cfg.Chunking.STMWarnPct` of the effective context limit, inject a brief `system` message into the turn prompt advising the model to call `summary_context` before proceeding.

- Add `STMWarnPct float64` to `ChunkConfig` (default 0.20).
- Add `stm_warn_pct` to `chunkingYAML`.
- No new tools required; works even when STM tools are not registered.
- Tests: inject a long history until threshold fires; verify system message appears.

**Dependency:** none.

---

### M1 — `summary_context` tool

**Effort:** ~2 hours  
**Files:** `builtin_tools.go`, `builtin_tools_test.go`

Register a new builtin tool `summary_context(span string)`.

- `span`: `"all"` or an integer string `"N"` for the last N turns.
- Calls the existing summarisation logic (reuse the prompt from `cmdSummarize` / `rollingSummaryPrompt`). Sends the selected history slice to the active LLM client; replaces those messages in `a.History` with a single `{"role":"system","content":"[Summary] ..."}` entry.
- Returns a confirmation string: `"Summarised N turns into 1 entry (~K tokens saved)"`.
- Requires `a.Client != nil`; returns an error string if no client.
- Safe-mode guard: in safe mode, return a description of what would be summarised and ask for confirmation before modifying history.
- Recorder: `RecordAgentAction` with `[[summary_context: N turns compressed]]`.
- Tests: summary of 5 messages collapses to 1; "all" summarises everything except system messages; span > history length summarises what's available.

**Dependency:** M0 (optional — M1 is independently useful).

---

### M2 — `filter_context` tool

**Effort:** ~3 hours  
**Files:** `builtin_tools.go`, `builtin_tools_test.go`

Register a new builtin tool `filter_context(criteria string)`.

- Embeds `criteria` using `NewEmbedderForEntry` (active RAG store embedder) or the Ollama embedder at `cfg.OllamaURL`. Falls back to case-insensitive keyword matching in `criteria` against message content when no embedder is available.
- Scores each non-system `History` message using cosine similarity. Removes messages scoring **above** threshold θ_f = 0.6 (i.e. messages *similar* to the filter criteria are removed — this is the AgeMem FILTER semantics: "remove content matching the unwanted criterion").
- Returns: `"Filtered N messages matching '<criteria>'"`.
- System messages are never filtered.
- Safe-mode guard: in safe mode, list what would be removed and require confirmation.
- Tests: filter removes messages containing the criteria keyword; system messages survive; empty history no-ops cleanly.

**Dependency:** M1 (establishes the pattern for history-mutating tools).

---

### M3 — `retrieve_memory` tool

**Effort:** ~1 hour  
**Files:** `builtin_tools.go`, `builtin_tools_test.go`

Register a new builtin tool `retrieve_memory(query string, top_k int)`.

- `top_k` defaults to 3 when 0 or omitted.
- Calls `a.Memory.Unified.Recall(query, embedder, budget)` where budget is `min(top_k * 256, 2048)` tokens.
- Prepends results as a `system` message in `a.History` so subsequent turns can reference them.
- Returns a summary: `"Retrieved N memory entries for query '<query>'"`.
- Requires `a.Memory != nil && a.Memory.Unified != nil`; returns informative error string if unavailable.
- Tests: recall with real MemoryStore entries; graceful no-op when store is empty.

**Dependency:** none (wraps existing `UnifiedMemory.Recall`).

---

### M4 — `add_memory` tool

**Effort:** ~2 hours  
**Files:** `builtin_tools.go`, `builtin_tools_test.go`

Register a new builtin tool `add_memory(content string, memory_type string, tags []string)`.

- Validates `memory_type` against `ValidMemoryTypes`; returns error string on invalid type.
- Constructs a `MemoryDoc` via `NewMemoryDoc(autoID, MemoryType(memory_type), content, content, tags)` where `autoID` is generated from a short hash of content + timestamp.
- Calls `a.Memory.Store.Save(doc, nil)`.
- Returns: `"Memory saved: <id>"`.
- Requires `a.Memory.Store != nil` and safe-mode check (confirm before writing in safe mode).
- Recorder: `RecordAgentAction` with `[[add_memory: <id> — <memory_type>]]`.
- Tests: round-trip save + list; invalid type rejected; safe-mode prompts confirmation.

**Dependency:** M3 (the model typically retrieve-then-adds).

---

### M5 — `update_memory` and `delete_memory` tools

**Effort:** ~2 hours  
**Files:** `builtin_tools.go`, `builtin_tools_test.go`

**`update_memory(id string, content string)`**
- Loads existing `MemoryDoc` by ID from `a.Memory.Store`. Returns error string if not found.
- Updates the Fountain body with new content and a new timestamp.
- Re-saves via `a.Memory.Store.Save(doc, nil)`.
- Returns: `"Memory updated: <id>"`.

**`delete_memory(id string)`**
- Sets the memory's confidence to 0.0 (archive), matching `/memory forget` semantics.
- Returns: `"Memory archived: <id>"`.
- Safe-mode guard: confirm before archiving.

- Tests: update changes content; delete reduces confidence to 0; unknown ID returns clear error.

**Dependency:** M4 (the model typically add-then-update in the same session).

---

### M6 — Dual RAG audit (supersedes the deferred item in DECISIONS.md)

**Effort:** ~2 hours  
**Files:** `config.go`, `config_yaml.go`, `rag_support.go`, `terminal.go`

After M3 (`retrieve_memory`) is available, the per-prompt `ragAugment()` call is redundant when the model is capable of requesting retrieval itself.

- Add `PerPrompt bool` (default `true`) to `RagStoreEntry` and YAML (`per_prompt`).
- In `ragAugment()`: early-return when `!entry.PerPrompt`.
- Document in `CONFIGURATION.md`: for sessions with reliable tools + M3 registered, set `per_prompt: false` on the active store.
- Tests: per_prompt=false skips ragAugment; per_prompt=true (default) unchanged.

**Dependency:** M3.

---

## Ordering

```
M0 (threshold)          — independent, do first (low risk, immediate value)
M3 (retrieve_memory)    — independent of M1/M2, wraps existing code
M1 (summary_context)    — depends on nothing, establishes history-mutation pattern
M2 (filter_context)     — after M1 (similar pattern, more complex)
M4 (add_memory)         — after M3 (model uses retrieve before add)
M5 (update+delete)      — after M4
M6 (dual RAG audit)     — after M3 (per_prompt flag)
```

Session budget: M0+M3 in one session (~2h), M1+M2 in one session (~5h), M4+M5 in one session (~4h), M6 standalone (~2h).

## Testing strategy

Following project TDD convention: write `_test.go` stubs first, confirm red, then implement. All tools use the `newToolAgent` helper from `builtin_tools_test.go`. LTM tools require a real `MemorySystem` (call `OpenMemory` in test setup). STM tools use a `mockLLMClient` for the summarisation call.

## Non-goals for this cycle

- GRPO / RL training loop
- Automatic θ_f threshold tuning for filter_context
- Embedding History messages at write time (filter uses on-the-fly embeddings)
- Changing the startup-time `UnifiedMemory.Recall()` call — it stays as-is
