# Harvey Refactoring Plan

**Date**: 2026-06-28  
**Status**: Approved for scheduling  
**Source**: Code audit conducted 2026-06-28

This plan addresses the technical debt identified in the June 2026 audit.
Work items are labeled R0 → R9, ordered by risk (lowest first). Each item
can be done in isolation; earlier items reduce the risk of later ones.

**Ground rules for all refactoring work:**

- Every item must pass `go test ./...` and `go test -race` before and after.
- No behavior changes. No new features. No opportunistic cleanup beyond the
  stated scope.
- Commit after each item, not after the whole phase.
- If a move touches a function called from many places, use `grep` to find
  all callers before moving.

---

## Audit summary

| Metric | Finding |
|--------|---------|
| Largest file | `commands.go` — 7026 lines, 145 functions, 8 embedded subsystems |
| Second largest | `helptext.go` — 3864 lines, all inline string literals |
| God file | `terminal.go` — 2543 lines, 6+ distinct concerns |
| Duplicated token heuristic | 4 locations (`routing.go`, `terminal.go`, `commands.go`, `pipeline.go`) |
| `NewMemoryStore` opens | 11 separate opens across 5 files; no session-scoped handle |
| Agent struct fields | 35 fields; no sub-struct grouping |
| Config struct fields | ~45 fields; 3 separate places where defaults are set |
| Files with no test | `builtin_tools.go` (933 lines) |
| Orphan test files | ~9 test files named for removed phases, not current source files |

---

## R0 — Quick wins: isolated one-function fixes

**Risk: very low. Each change is a rename or a function move with no logic change.**

Do these in any order. Each is independently committable.

### R0-A — Consolidate `estimateTokens`

**Problem:** `estimateTokens` is documented as living in `routing.go` but
`context_estimator.go` exists precisely to own token accounting. Three other
files inline the same `len(s)/4` heuristic without calling it.

**Fix:**
1. Move `estimateTokens` from `routing.go` to `context_estimator.go`.
2. In `terminal.go` (~line 1352), `commands.go` (~line 661), and
   `pipeline.go` (~line 284): replace the inline `len(...) / 4` with
   a call to `estimateTokens(...)`.
3. Update the comment in `context_estimator.go` that says "defined in
   routing.go".

### R0-B — Delete `filterEnvironment` alias

**Problem:** `terminal.go:73` defines a one-line alias for
`filterCommandEnvironment` in `commands.go:92`.

**Fix:**
1. Delete the `filterEnvironment` function from `terminal.go`.
2. At `terminal.go:754` (the one call site), call `filterCommandEnvironment`
   directly.

### R0-C — Move `ragAugment` to `rag_support.go`

**Problem:** `ragAugment` (77 lines, `terminal.go:2332`) operates on `RagStore`
and `Agent.Rag` — it belongs next to `RagStore` in `rag_support.go`, not
in the terminal REPL file.

**Fix:**
1. Cut `ragAugment` from `terminal.go`, paste into `rag_support.go`.
2. It is called only from `terminal.go`; keep the call there, no callers change.
3. Confirm `go build` still passes (`ragAugment` references `Agent` fields
   that are visible from `rag_support.go` since both are package `harvey`).

### R0-D — Move `ragChunk` to `rag_support.go`

**Problem:** `ragChunk` (~40 lines, `commands.go:6238`) is a RAG-ingestion
helper buried 6238 lines into `commands.go`. It has no `Agent` dependency.

**Fix:**
1. Cut `ragChunk` from `commands.go`, paste into `rag_support.go`.
2. Its callers are in `commands.go` — no imports change (same package).

### R0-E — Move `resolveLlamafilePath` to `llamafile.go`

**Problem:** `resolveLlamafilePath` is defined in `terminal.go:1573` but
called 5 times from `llamafile.go` and only 3 times from `terminal.go`.
The calling file should not own a function it doesn't dominate.

**Fix:**
1. Cut `resolveLlamafilePath` from `terminal.go`, paste into `llamafile.go`.
2. Confirm callers in `terminal.go` still compile (same package — they will).

### R0-F — Merge `ollamaFormatBytes` into `formatBytes`

**Problem:** `commands.go` has two `formatBytes` variants 566 lines apart.
`ollamaFormatBytes` adds only a `zeroDash` behavior (returns `"—"` for zero).

**Fix:**
1. Add a `zeroDash bool` parameter to `formatBytes`, or inline the `"—"`
   check at the one `ollamaFormatBytes` call site and delete the function.
2. Delete `ollamaFormatBytes`.

### R0-G — Rename orphan test files

**Problem:** ~9 test files are named for removed development phases
(`phase_d_test.go`, `phase_e_test.go`, `tier2_test.go`, `tier3_test.go`,
`routing_phase4_test.go`, etc.) rather than for the source files they test.
This makes navigation confusing.

**Fix:** Identify what each file actually tests (grep for the tested
functions), then rename to match the source file (`pdf_extract_test.go`,
`tool_executor_test.go`, etc.). No code changes — rename only.

### R0-H — Remove duplicate `LlamafileEntry` definition

**Problem:** `config.go` contains two definitions of `LlamafileEntry` —
one at approximately line 80–85 and a second at approximately line
1090–1104. Duplicate type definitions are a compilation hazard and a
maintenance trap: edits to one copy are silently missed in the other.

**Fix:**
1. Confirm which definition is canonical (check which one is referenced
   by `LlamafileConfig.Models []LlamafileEntry` and called from
   `llamafile.go`).
2. Delete the duplicate. Verify with `go build` that only one definition
   remains and all callers still compile.

---

## R1 — Move prose and Apertus tool-call logic to `tool_executor.go`

**Risk: low. Pure move, no logic change.**

**Problem:** `tryExecuteProseToolCalls` and `tryExecuteApertusToolCalls`
(~110 lines total, currently in `terminal.go`) operate on `CodeBlock` and
`ToolRegistry` — the same types `tool_executor.go` already owns. They
were placed in `terminal.go` because they're called from `runChatTurn`,
but they have no terminal-specific dependencies.

**Fix:**
1. Cut both functions from `terminal.go`.
2. Paste into `tool_executor.go`.
3. Call sites in `terminal.go` remain unchanged (same package).

**Acceptance:** `go test -race ./...` passes. `terminal.go` shrinks by ~110 lines.

---

## R2 — Extract `commands_rag.go` from `commands.go`

**Risk: medium. Largest single extraction; most callers to verify.**

**Problem:** The `/rag` command handler and the entire RAG ingestion pipeline
(`ragIngestFile`, `ragIngestRemotePrefix`, `ragIngestS3Prefix`, `ragIngestHTTP`,
`ragChunk`) account for roughly 1400 lines inside `commands.go`. After R0-D,
`ragChunk` is already in `rag_support.go`. The remaining ingest functions
have no dependency on the other commands in `commands.go`.

**Fix:**
1. Create `commands_rag.go`.
2. Move `cmdRag` and all `ragIngest*` functions into it.
3. Move the `/rag` help text from `helptext.go` into `commands_rag.go` as
   a package-level `const` (or keep in `helptext.go` — either is fine as
   long as it's consistent).
4. Verify no import cycle: `commands_rag.go` will import nothing that
   `commands.go` doesn't already import.

**Acceptance:** `go test -race ./...` passes. `commands.go` shrinks by ~1400 lines.

---

## R3 — Extract `commands_memory.go` from `commands.go`

**Risk: medium.**

**Problem:** `/memory` commands and workspace-profile management (~640 lines)
are an independent subsystem inside `commands.go`. They call into
`memory_store.go` and `memory_miner.go` — same-package files — but do not
interact with other commands.

**Fix:**
1. Create `commands_memory.go`.
2. Move `cmdMemory` and its sub-handlers (`cmdMemoryList`, `cmdMemoryShow`,
   `cmdMemoryForget`, `cmdMemoryMine`, etc.) into it.
3. Move profile-management helpers that are only called from memory commands.

**Acceptance:** `go test -race ./...` passes. `commands.go` shrinks by ~640 lines.

---

## R4 — Extract `commands_kb.go`, `commands_skill.go`, `commands_route.go`

**Risk: low-medium. Three smaller extractions that can be done together or separately.**

| New file | Source | Lines approx |
|----------|--------|-------------|
| `commands_kb.go` | `/kb` handlers | ~500 |
| `commands_skill.go` | `/skill`, `/skillset` handlers | ~400 |
| `commands_route.go` | `/route` handlers | ~250 |

**Fix for each:** Same pattern as R2 and R3 — create file, move handlers,
verify `go test -race ./...`.

After R2, R3, and R4, `commands.go` should be under 2000 lines, containing
`registerCommands`, `dispatch`, `Command` type, utility functions (`isBinary`,
`looksLikePath`, `formatBytes`), and any commands that don't belong to a
named subsystem.

---

## R5 — Extract `backend_startup.go` from `terminal.go`

**Risk: medium. Prepares for the `ManagedBackend` interface (unified backend design).**

**Problem:** Six backend-selection functions currently live in `terminal.go`
despite having no terminal-REPL logic:

- `selectBackend` (~line 1625)
- `pickBackend` (~line 1707)
- `startAndUseLlamafile` (~line 1797)
- `pickOllamaModel` (~line 1841)
- `useLlamafileEntry` (~line 1593) — also called from `llamafile.go`
- `probeActiveBackend` (~line 238)

These are I/O-heavy (subprocess launch, HTTP probe, user prompt), but they
are not part of the REPL loop. They belong next to the backend code they manage.

**Fix:**
1. Create `backend_startup.go`.
2. Move the six functions listed above into it.
3. `resolveLlamafilePath` (already moved to `llamafile.go` in R0-E) is a
   dependency; verify it is still accessible.
4. `Run()` in `terminal.go` calls these functions at startup; the calls
   remain, only the definitions move.

**Acceptance:** `go test -race ./...` passes. `terminal.go` shrinks by ~400 lines.
This file also establishes the natural location for future `ManagedBackend`
lifecycle functions.

---

## R6 — Introduce `MemorySystem` aggregate; open once per session

**Risk: medium-high. Touches 11 call sites; must not change behavior.**

**Problem:** `NewMemoryStore` is called 11 times across 5 files, once per
operation rather than once per session. This causes file I/O on every memory
read, prevents consistent session-scoped state, and is likely the root of
reported "memory feeling broken" behavior (two opens in the same session
may see stale state if writes happened between them).

### Target shape

Introduce a `MemorySystem` aggregate type that owns the complete lifecycle
of all memory subsystems. This gives every component a single open/close
path and eliminates the scattered `New*` calls:

```go
// MemorySystem owns the complete lifecycle of Harvey's memory subsystems.
// Open it once per session via OpenMemory; close it on session exit.
type MemorySystem struct {
    Store    *MemoryStore
    Unified  *UnifiedMemory
    Miner    *Miner
    Manifest *Manifest
    Rolling  *RollingSummary
}

// OpenMemory initializes all memory subsystems in dependency order.
// Returns a non-nil *MemorySystem even on partial failure — components
// that could not be opened are nil and callers must nil-check them.
func OpenMemory(ws *Workspace, cfg *MemoryConfig) (*MemorySystem, error)

// Close shuts down all memory subsystems in reverse dependency order.
func (m *MemorySystem) Close() error
```

Add `Memory *MemorySystem` to `Agent` in `harvey.go`. All other memory
fields on `Agent` that are subsumed by `MemorySystem` are removed.

### Fix steps

1. Define `MemorySystem`, `OpenMemory`, and `Close` in a new file
   `memory_system.go`.
2. In `terminal.go:Run()`, call `OpenMemory` once after the workspace is
   initialized (alongside `initRag`). Assign to `a.Memory`. Log a warning,
   not a fatal, if opening fails.
3. At each of the 11 `NewMemoryStore` call sites, replace with `a.Memory.Store`
   (or receive `a.Memory` as a parameter where `Agent` is not in scope).
4. Replace direct calls to `NewUnifiedMemory`, `NewMiner`, `NewManifest`
   at their scattered call sites with references to the corresponding
   `a.Memory.*` field.
5. On session exit, call `a.Memory.Close()`.

**Call sites to update (after R2/R3 move commands to new files, line numbers
will shift — grep before editing):**
- `harvey.go:336`
- `terminal.go:611`, `1002`, `1020`, `2289`
- `commands.go:695`, `988`, `6339` (or their new locations in `commands_memory.go` etc.)
- `completion_candidates.go:127`

**Acceptance:** `go test -race ./...` passes. Memory operations that previously
required re-opening the store now reuse the session-scoped handle. Verify
auto-mining on session exit uses the same `Store` that saw the session's turns.

---

## R7 — Config sub-structs and single source of defaults

**Risk: high. Breaking YAML key change; requires migration shim.**

**Problem:** `Config` has ~45 flat fields. Defaults are set in three places
(`DefaultConfig`, `LoadHarveyYAML`, `DefaultChunkConfig`). YAML parsing uses
~12 intermediate `*YAML` adapter structs mixed into `config.go`.

**Fix (in two sub-steps):**

### R7-A — Separate YAML adapter types into `config_yaml.go`

Move all `*YAML` structs and the `harveyYAML` type from `config.go` into a
new `config_yaml.go`. `config.go` retains `Config`, `DefaultConfig`,
`LoadHarveyYAML`, and config methods. No behavior change; the boundary is
purely organizational.

While extracting the YAML types, audit the top-level `harveyYAML` struct for
legacy fields that are no longer populated by the parser:
- `KnowledgeDB` (approximately `config.go:627`) — superseded by nested
  `knowledge_base.db` field; verify no active YAML files set this key before
  removing.
- `RAG` (approximately `config.go:632`) — superseded by the `memory.rag`
  stanza; same verification step.

If confirmed unused, remove the fields and add a migration note to
`CONFIGURATION.md` so users with old `harvey.yaml` files know the key is gone.

### R7-B — Group related Config fields into sub-structs

Proposed groupings (YAML keys change — requires migration shim for one release):

```go
type OllamaConfig struct {
    URL           string
    Model         string
    ContextLength int
    Timeout       time.Duration
}

type LlamafileConfig struct {
    URL            string
    ModelsDir      string
    StartupTimeout time.Duration
    GPULayers      int
    MaxTokens      int
    Models         []LlamafileEntry
    Active         string
}

type SecurityConfig struct {
    SafeMode        bool
    AllowedCommands []string
    Permissions     PermissionsConfig
    RunTimeout      time.Duration
}

type SessionConfig struct {
    AutoRecord       bool
    RecordPath       string
    ContinuePath     string
    ResumeLatest     bool
    ReplayPath       string
    ReplayOutputPath string
    ReplayContinue   bool
}
```

`MemoryConfig` currently mixes RAG config, knowledge base config, and
rolling summary config in a single flat struct. Split it into focused
sub-structs that match the three-silo architecture:

```go
type MemoryConfig struct {
    Enabled       bool
    TopK          int
    InjectOnStart bool
    BudgetPct     float64

    RollingSummary RollingSummaryConfig
    Rag            RagConfig        // replaces RagStores, RagActive, RagEnabled
    Knowledge      KnowledgeConfig  // replaces KnowledgeDB, CurrentProjectID
}

type RagConfig struct {
    Enabled bool
    Active  string
    Stores  []RagStoreEntry
}

type KnowledgeConfig struct {
    DBPath           string
    CurrentProjectID int64
}
```

This `MemoryConfig` restructure is independent of the backend changes and
can be done at the same time as the `OllamaConfig`/`LlamafileConfig` grouping.

There is only one user (`harvey.yaml` is a personal workspace file, not
distributed). No migration shim is needed — update `harvey.yaml` directly
when the config struct changes and document the new key names in
`CONFIGURATION.md`.

**Defer R7-B until the unified backend design (unified-model-backend-design.md)
is finalized**, since that design changes `OllamaConfig` and adds `LlamaCppConfig`.
Do R7-A (the file split) immediately as it has no behavior risk.

---

## R8 — Direct test file for `builtin_tools.go`

**Risk: low. Adding tests never breaks behavior.**

**Problem:** `builtin_tools.go` (933 lines) has no dedicated test file.
The `write_file` auto-format path, PDF extraction error path, chunking
guard branches, and permission-checking paths are untested at the unit level.

**Fix:**
1. Create `builtin_tools_test.go`.
2. Write tests for:
   - `read_file` with an over-budget file and `chunking.enabled: false` (should read normally)
   - `read_file` with an over-budget file and `chunking.enabled: true`, user cancels (should return "cancelled")
   - `write_file` with an auto-formattable Go file (should apply gofmt)
   - `write_file` with a file the workspace denies (should return permission error)
   - `list_files` with a nested directory
   - Any error paths in PDF extraction that can be exercised without `pdftotext`

Use the existing `mockLLMClient` from `tier3_test.go` (or its renamed successor
after R0-G) as the test double for any paths that call the LLM.

---

## R9 — Lower `maxInjectFileBytes` (safety fix, not refactoring)

**Risk: very low. One constant change.**

**Problem:** `file_inject.go:20` sets `maxInjectFileBytes = 64 * 1024`.
A 30KB file injected inline alongside existing history and memories causes
OOM on Pi (observed 2026-06-28). At 4 bytes/token, 64KB ≈ 16K tokens —
far more than an 8B model can handle reliably on CPU.

**Fix:**
```go
const maxInjectFileBytes = 16 * 1024  // ~4K tokens; Pi-safe default
```

Add a comment explaining the rationale. This is tracked in TODO.md as part
of the W5 bug; including it here because it belongs to the structural
audit as much as the feature work.

---

## Work order and schedule

### Do now (before any new features)

| Item | Files touched | Estimated effort |
|------|--------------|-----------------|
| R9 | `file_inject.go` | 5 minutes |
| R0-B | `terminal.go` | 10 minutes |
| R0-A | `routing.go`, `context_estimator.go`, `terminal.go`, `commands.go`, `pipeline.go` | 30 minutes |
| R0-F | `commands.go` | 15 minutes |
| R0-E | `terminal.go`, `llamafile.go` | 15 minutes |
| R0-C | `terminal.go`, `rag_support.go` | 20 minutes |
| R0-D | `commands.go`, `rag_support.go` | 20 minutes |
| R0-G | 9 test files | 30 minutes (find + rename) |
| R0-H | `config.go` | 15 minutes |
| R1 | `terminal.go`, `tool_executor.go` | 45 minutes |

### After R0 and R1 stabilize (one commit per item, tests pass)

| Item | Dependencies | Estimated effort |
|------|-------------|-----------------|
| R7-A | None | 1 hour |
| R5 | R0-E | 2 hours |
| R2 | R0-D | 3 hours |
| R3 | None | 2 hours |
| R4 | None | 2 hours |

### After R2–R5 stabilize

| Item | Dependencies | Estimated effort |
|------|-------------|-----------------|
| R6 | R2, R3 (so call sites are in final locations) | 3 hours |
| R8 | R0-G (so test files have stable names) | 2 hours |
| R7-B | Unified backend design finalized | 4 hours + migration |

---

## Expected outcomes

After R0–R5:

- `commands.go`: ~7026 → ~1800 lines
- `terminal.go`: ~2543 → ~1900 lines
- Token heuristic: 4 implementations → 1 canonical function
- `NewMemoryStore` opens: 11 → will reduce further in R6

After R6:

- `MemorySystem` aggregate type owns all memory subsystem lifecycle
- Memory operations consistent across a session; single open/close path
- `NewMemoryStore` opens: 11 → 1 (via `OpenMemory`)
- "Memory feels broken" reports should decrease

After R7-A:

- `config.go`: ~1281 → ~850 lines (YAML adapters extracted)

After R8:

- `builtin_tools.go` has test coverage
- The W5 chunking guard can be regression-tested without a live LLM
