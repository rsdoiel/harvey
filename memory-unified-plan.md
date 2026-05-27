# Harvey Unified Memory — Implementation Plan

See [memory-unified-design.md](memory-unified-design.md) for the full
design rationale and architecture decisions.

Phases are ordered so each one compiles, passes tests, and provides
standalone value. No phase depends on a later phase being started.

---

## Phase 1 — Config Restructure (split into 5 sub-phases)

**Goal:** Move RAG and KB configuration under `MemoryConfig` in both
the Go structs and `harvey.yaml`. All functionality stays identical;
this is a pure restructure with no behaviour change.

Each sub-phase compiles, passes all tests, and leaves Harvey fully
functional. Sub-phases are designed to be completed in separate
sessions.

---

### Phase 1a — Additive: new `MemoryConfig` fields

**Scope:** `config.go` and `config_test.go` only. No call sites
change. Nothing is removed.

**Files to modify:**

| File | Change |
|------|--------|
| `config.go` | Add `BudgetPct float64` and `RollingSummary RollingSummaryConfig` to `MemoryConfig`. Add `RollingSummaryConfig` type (`Enabled bool`, `WarnAtPct float64`, `KeepTurns int`). Set defaults in `DefaultConfig`: `BudgetPct: 0.25`, `RollingSummary: {Enabled: true, WarnAtPct: 0.80, KeepTurns: 6}`. Add `rolling_summary:` sub-struct to `memoryYAML` and wire it in `LoadHarveyYAML`. |
| `config_test.go` | Add test: `DefaultConfig().Memory.BudgetPct == 0.25`. Add round-trip test: marshal then unmarshal `memoryYAML` with rolling_summary fields; verify values survive. |

**Acceptance criteria:**
- `go build ./...` and `go test ./...` pass.
- Harvey behaviour is unchanged.

---

### Phase 1b — Mirror RAG config into `MemoryConfig`

**Scope:** `config.go` and `config_test.go` only. All existing
`Config.Rag*` fields remain; they are kept in sync with
`Config.Memory.Rag*` during load and save. No call sites change yet.

**Files to modify:**

| File | Change |
|------|--------|
| `config.go` | Add `RagStores []RagStoreEntry`, `RagActive string`, `RagEnabled bool` to `MemoryConfig`. Add `memory.rag` sub-struct to `memoryYAML` (reuse `ragYAML` type). In `LoadHarveyYAML`, after populating `cfg.RagStores/Active/Enabled` from old `rag:` or new `memory.rag:`, also copy those values into `cfg.Memory.Rag*`. Add `ActiveRagStore()`, `RagStoreByName()`, `AddOrUpdateRagStore()`, `RemoveRagStore()` methods to `MemoryConfig` (identical logic to the same methods on `Config`). Add `SaveMemoryConfig(ws, cfg)` that writes the full `memory:` section including `memory.rag`. Update `SaveRAGConfig` to call `SaveMemoryConfig` (becomes a thin wrapper). |
| `config_test.go` | Add backward-compat test: parse YAML with old top-level `rag:` section; verify `cfg.Memory.RagActive` and `cfg.RagActive` both have the expected value. Add round-trip test for `memory.rag` YAML path. |

**Acceptance criteria:**
- `go build ./...` and `go test ./...` pass.
- An existing workspace with old-format `harvey.yaml` loads without
  error. Running `/rag status` works normally.
- `SaveRAGConfig` still writes a valid file; its output now includes
  `memory.rag:` alongside the old `rag:` section.

---

### Phase 1c — Migrate RAG call sites

**Scope:** `commands.go` and `harvey.go`. Switch all RAG field
accesses from `cfg.Rag*` / `cfg.ActiveRagStore()` etc. to
`cfg.Memory.Rag*` / `cfg.Memory.ActiveRagStore()` etc. Switch
`SaveRAGConfig` → `SaveMemoryConfig`.

**Files to modify:**

| File | Change |
|------|--------|
| `commands.go` | Replace `a.Config.RagStores`, `a.Config.RagActive`, `a.Config.RagEnabled`, `a.Config.ActiveRagStore()`, `a.Config.RagStoreByName()`, `a.Config.AddOrUpdateRagStore()`, `a.Config.RemoveRagStore()` with `a.Config.Memory.*` equivalents throughout. Replace all `SaveRAGConfig(...)` calls with `SaveMemoryConfig(...)`. |
| `harvey.go` | Same substitutions for any RAG field accesses. |

**Acceptance criteria:**
- `go build ./...` and `go test ./...` pass.
- All `/rag` subcommands work correctly end-to-end.
- Old `Config.Rag*` fields on the `Config` struct still exist (not
  yet removed — that is Phase 1e). Harvey is not broken if any
  unvisited call site still uses the old path.

---

### Phase 1d — Mirror KB into `MemoryConfig` and migrate call sites

**Scope:** `config.go`, `commands.go`, `knowledge.go`,
`knowledge_test.go`. Moves `KnowledgeDB` and `CurrentProjectID` into
`MemoryConfig` and migrates all call sites in one step (the KB
footprint is much smaller than RAG's).

**Files to modify:**

| File | Change |
|------|--------|
| `config.go` | Add `KnowledgeDB string` and `CurrentProjectID int64` to `MemoryConfig`. Add `knowledge_base:` sub-struct to `memoryYAML` (`db_path string`, `current_project string`). In `LoadHarveyYAML`, populate both `cfg.KnowledgeDB`/`cfg.CurrentProjectID` and `cfg.Memory.KnowledgeDB`/`cfg.Memory.CurrentProjectID` from whichever YAML location is present. |
| `commands.go` | Replace `a.Config.KnowledgeDB` and `a.Config.CurrentProjectID` with `a.Config.Memory.*` throughout. |
| `knowledge.go` | Same substitutions. |
| `knowledge_test.go` | Update field references. |

**Acceptance criteria:**
- `go build ./...` and `go test ./...` pass.
- All `/kb` subcommands work correctly end-to-end.

---

### Phase 1e — Final cleanup: remove old top-level fields

**Scope:** `config.go` and any remaining call sites. Remove
`Config.RagStores`, `Config.RagActive`, `Config.RagEnabled`,
`Config.KnowledgeDB`, `Config.CurrentProjectID` and the duplicate
methods on `Config`. Remove the old top-level `rag:` write path from
`SaveMemoryConfig`. Remove `SaveRAGConfig`.

**Files to modify:**

| File | Change |
|------|--------|
| `config.go` | Delete the five moved fields from `Config`. Delete `ActiveRagStore()`, `RagStoreByName()`, `AddOrUpdateRagStore()`, `RemoveRagStore()` from `Config`. Delete `SaveRAGConfig`. Update `LoadHarveyYAML` to only copy `memory.rag` values into `Config.Memory.*` (still reads old `rag:` for compat but no longer copies to now-absent `Config.Rag*`). Update `SaveMemoryConfig` to omit the legacy `rag:` top-level write. |
| `config_test.go` | Remove any tests that reference the deleted `Config.Rag*` fields directly. |

**Acceptance criteria:**
- `go build ./...` and `go test ./...` pass with no errors.
- `harvey.yaml` written by Harvey no longer contains a top-level
  `rag:` section; all RAG config is under `memory.rag:`.
- Old `harvey.yaml` files with top-level `rag:` still load correctly
  (backward-compat read path remains).

---

## Phase 2 — New Memory Types + Unified Retrieval + Token Budget

**Goal:** Add `workspace_profile` and `project_fact` types. Create
`UnifiedMemory`. Replace `injectMemoryContext` with budget-aware
unified injection. Add `/memory recall`.

### Files to create

| File | Purpose |
|------|---------|
| `memory_unified.go` | `UnifiedResult` struct, `UnifiedMemory` struct and `Recall` method, token budget allocation logic, context injection formatting |
| `memory_unified_test.go` | Unit tests for `Recall` (all silos empty, factual-only, mixed, budget truncation) |

### Files to modify

| File | Change |
|------|--------|
| `memory.go` | Add `MemoryTypeWorkspaceProfile = "workspace_profile"` and `MemoryTypeProjectFact = "project_fact"` constants. Add both to `ValidMemoryTypes`. |
| `memory_store.go` | Add `workspace_profile` and `project_fact` to the `subdirs` slice in `NewMemoryStore` so the directories are created on startup. |
| `harvey.go` | Replace `injectMemoryContext` body: instantiate `UnifiedMemory`, compute `budgetTokens` from `OllamaContextLength * BudgetPct` (fallback 512), call `Recall`, format and inject. Keep the function signature unchanged so no callers need updating. |
| `commands.go` | Add `"recall"` to the `cmdMemory` dispatch switch. Implement `cmdMemoryRecall`: opens store, calls `UnifiedMemory.Recall` with a display budget (no cap), formats grouped output. |
| `config.go` | Set `BudgetPct` default to `0.25` in `DefaultConfig`. |
| `helptext.go` | Add `recall` to the `/memory` help text. |

### Notes

- `UnifiedMemory.Recall` scores factual types at 1.0 (always
  included, always first). Experiential types are scored by cosine
  similarity against the session query. RAG chunks and KB observations
  are scored by their respective retrieval methods and appended if
  budget remains.
- Token estimation uses `len(content)/4` as an approximation. The
  exact `CountTokens` API call is too expensive to make once per
  result; the approximation keeps injection within ±20% of budget.
- The `query` passed to `Recall` at session start is the system
  prompt text (same as the previous `injectMemoryContext` behaviour).
- **Hybrid retrieval:** `Recall` runs FTS5 text search first (fast,
  no Ollama round-trip). If an embedder is available it also runs
  cosine similarity and merges the two result sets. The `Recent()`
  fallback is removed — FTS5 is always a better proxy for relevance
  than recency.
- **Temperature for internal LLM calls:** Rolling summary compression
  and memory mining calls should use a low temperature (0.1–0.2) for
  deterministic, high-fidelity output. Pass temperature via a new
  optional field on the internal call rather than using the user's
  current chat temperature setting.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- Starting a session injects a `[memory context]` block containing
  only factual docs when no experiential memories exist.
- Starting a session with a large experiential store respects
  `budget_pct`: the injected block does not exceed
  `OllamaContextLength * BudgetPct` tokens (estimated).
- `/memory recall "git error"` returns results from all populated
  silos in grouped format.
- Setting `memory.enabled: false` in harvey.yaml disables injection
  and recall.

---

## Phase 3 — Workspace Profile Onboarding

**Goal:** On first use in a workspace, Harvey asks a few questions and
creates `workspace_profile` and `project_fact` memory documents.

### Files to create

| File | Purpose |
|------|---------|
| `memory_onboarding.go` | `NeedsOnboarding(store *MemoryStore) bool`, `RunOnboarding(agent *Agent, store *MemoryStore, embedder Embedder, out io.Writer, in io.Reader) error`, `extractProjectFact(wsRoot string) string` |
| `memory_onboarding_test.go` | Unit tests for `NeedsOnboarding`, `extractProjectFact` (workspace with codemeta.json, go.mod, bare workspace) |

### Files to modify

| File | Change |
|------|--------|
| `harvey.go` | In `Agent.Reset()` (or at REPL startup, after the store is opened), call `NeedsOnboarding`; if true, call `RunOnboarding` before `injectUnifiedContext`. |
| `commands.go` | Add `"profile"` subcommand to `/memory` for manual profile management: `profile show` (list workspace_profile docs), `profile update` (open most recent in $EDITOR). |
| `helptext.go` | Document the `profile` subcommand in `/memory` help. |

### `extractProjectFact` logic

Checks in order; stops at first success:

1. Parse `codemeta.json` in workspace root → extract `name`,
   `description`, `programmingLanguage`, `developmentStatus`.
2. Parse `go.mod` → module name.
3. Parse `package.json` → name, description.
4. Read `.git/config` → `remote.origin.url`.
5. Return empty string (caller will ask the user).

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- In a workspace with no `workspace_profile` memories, starting
  Harvey triggers the onboarding questions; after answering, memories
  are written and subsequent starts skip onboarding.
- In a workspace with `codemeta.json`, project_fact is populated
  automatically without questions.
- `/memory profile show` lists the workspace_profile documents.
- `/memory list workspace_profile` also works (uses the existing list
  command).

---

## Phase 4 — Rolling Summary (Working Memory Compression)

**Goal:** When conversation history approaches the context window
limit, warn and compress older turns so long sessions remain usable
on small models.

### Files to create

| File | Purpose |
|------|---------|
| `memory_rolling.go` | `ShouldCompress(historyTokens, contextLen int, warnAtPct float64) bool`, `CompressHistory(agent *Agent, keepTurns int, out io.Writer) error` |
| `memory_rolling_test.go` | Unit tests for `ShouldCompress`; integration test stub for `CompressHistory` |

### Files to modify

| File | Change |
|------|--------|
| `harvey.go` (or `commands.go` REPL loop) | After writing each assistant reply to history, call `ShouldCompress`; if true, print the warning line and call `CompressHistory`. |
| `config.go` | Set `RollingSummary` defaults in `DefaultConfig`: `Enabled: true`, `WarnAtPct: 0.80`, `KeepTurns: 6`. Add `rolling_summary:` sub-struct to `memoryYAML` and load it in `LoadHarveyYAML`. |
| `helptext.go` | Document rolling summary in the `/context` or `/compact` help sections, or add a dedicated note in `/help memory`. |

### `CompressHistory` logic

1. Split `agent.History` into `older` (all but last `KeepTurns` turns)
   and `recent` (last `KeepTurns` turns).
2. Format `older` as plain dialogue text.
3. Call the current model with the summariser prompt (see design doc).
   This call is **not recorded** (bypasses the session recorder).
4. Replace `agent.History` with `[summary message] + recent`.
5. Print `[context ~N% full — compressing older turns]` before the
   model call.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `ShouldCompress` returns false below threshold, true at or above.
- A session that runs long enough to hit 80% of the context window
  prints the warning message and continues normally afterward.
- Setting `rolling_summary.enabled: false` in harvey.yaml disables
  the feature; no compression occurs.
- The session `.spmd` recording on disk contains the original full
  history up to the point of compression; after compression only the
  summary + recent turns appear in new recording entries.

---

## Phase 2b — Adaptive Budget Tuning

**Goal:** Track per-session memory usage statistics and surface
tuning suggestions in `/memory status`. Implemented alongside or
immediately after Phase 2.

### Files to modify

| File | Change |
|------|--------|
| `memory_store.go` | Add `memory_stats` table to `memoriesSchema` (columns: `session_id`, `budget_tokens`, `injected_tokens`, `compressed`, `avg_tokens_per_sec`, `recorded_at`). Add `RecordSessionStats(sessionID string, budgetTokens, injectedTokens int, compressed bool, avgToksPerSec float64) error` method to `MemoryStore`. |
| `harvey.go` | At session end (before REPL exits), call `store.RecordSessionStats(...)` with values accumulated during the session. Track `injectedTokens`, `compressed`, and a running average of `ChatStats.TokensPerSec` as session-scoped variables. |
| `commands.go` | In `cmdMemoryStatus`, query `memory_stats` for the last 10+ rows; compute budget saturation, compression frequency, and throughput trend; print the `Budget advice:` line (see design doc for all three signals). |
| `memory_store.go` | Add `BudgetStats(n int) (avgSaturation, compressionRate, avgToksPerSec float64, err error)` query method. |

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- After 10+ sessions, `/memory status` includes a `Budget advice:`
  line.
- Fewer than 10 sessions: the advice line is omitted silently.
- Stats table survives a `memories.db` rebuild (it is empty after
  rebuild; that is acceptable — stats accumulate again from next use).

---

## Phase 5 — Auto-mine on Session End

**Goal:** When the user exits or clears history in a session that had
enough turns to be worth mining, automatically propose memory
extraction without requiring a manual `/memory mine` run.

**Trigger:** Session ends (user types `exit`/`quit`/`/clear`) AND the
session has >= 10 user turns AND the session file is not already in
the manifest.

**Behaviour:** Harvey prints a single line:

```
[auto-mining session for memories — use /memory mine to review manually]
```

Then runs the non-interactive path of `Miner.Mine` (accept all
proposals without interactive review, score >= 0.70 threshold,
skip near-duplicates). The user can review or revise afterward with
`/memory list` and `/memory forget`.

### Files to modify

| File | Change |
|------|--------|
| `harvey.go` or REPL exit path | After recording the session to disk, check turn count and manifest; call auto-mine if criteria met. |
| `memory_miner.go` | Add `MineAuto(ctx, sessionPath, agent, embedder, out) error` that runs extraction + saves all proposals above a confidence threshold without interactive review. |
| `commands.go` | Ensure `/clear` triggers the same check as exit (it already saves the session). |

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- Exiting a 10+ turn session triggers auto-mine; a new session
  starting in the same workspace shows the mined memories via
  `/memory list`.
- Exiting a short (< 10 turn) session does not trigger auto-mine.
- Running `/memory mine` on an already-auto-mined session reports
  "already mined" (manifest entry exists).

---

## Dependency Graph

```
Phase 1a (additive config fields)
    └─► Phase 1b (mirror RAG into MemoryConfig)
            └─► Phase 1c (migrate RAG call sites)
                    └─► Phase 1d (mirror + migrate KB)
                            └─► Phase 1e (remove old fields)
                                    └─► Phase 2 (Unified Retrieval)
                                            ├─► Phase 2b (Adaptive Budget Tuning)
                                            ├─► Phase 3 (Onboarding)
                                            └─► Phase 4 (Rolling Summary)
                                                    └─► Phase 5 (Auto-mine)
```

Phase 1 sub-phases are strictly sequential — each one must compile
and pass tests before starting the next. Phases 2b, 3, 4, and 5 each
depend on Phase 2 but are independent of each other and can be
developed in any order after Phase 2 is complete.

---

## New Module Dependencies

None. All implementation uses packages already imported by Harvey:
`database/sql`, `gopkg.in/yaml.v3`, `github.com/glebarez/go-sqlite`,
and the Go standard library.

---

## Open Questions

None outstanding. Design decisions were settled during the planning
session on 2026-05-26. Record any new decisions or reversals here
as implementation proceeds.
