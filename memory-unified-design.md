# Harvey Unified Memory — Design

**Status (2026-05-26):** Design draft. See
[memory-unified-plan.md](memory-unified-plan.md) for the phased
implementation plan.

**References:**
- "Memory in the Age of AI Agents: A Survey — Forms, Functions and
  Dynamics" (arXiv 2512.13564v2) — primary taxonomy reference.
  `Reference/papers/2512.13564v2.pdf`
- "Is Grep All You Need? How Agent Harnesses Reshape Agentic Search"
  (arXiv 2605.15184v1) — hybrid lexical/semantic retrieval strategy.
  `Reference/papers/2605.15184v1.pdf`
- "Real-time and offline LLMs on edge devices: a systematic review"
  (PeerJ CS 3769) — real-time vs. offline access patterns.
  `Reference/papers/peerj-cs-3769.pdf`
- "Performance analysis of localised LLMs in resource-constrained edge"
  (JEC 1047) — Pi throughput benchmarks; temperature guidance.
  `Reference/papers/JEC_1047_Ray_Pradhan.pdf`
- "iGenOrch: Intelligent Orchestration for Multi-Model LLM Inference
  on Edge Platforms" (3746467) — latency-aware context allocation.
  `Reference/papers/3746467.3801501.pdf`

---

## Motivation

Harvey currently has three disconnected memory silos:

| Silo | Location | What it holds |
|------|----------|---------------|
| `MemoryStore` | `agents/memories/` | Agent experiences (tool_use, workflow, user_preference) |
| RAG stores | `agents/rag/*.db` | Ingested documents for retrieval |
| Knowledge Base | `agents/knowledge.db` | Project observations and concepts |

Each silo has its own retrieval path. At session start, only
`MemoryStore` is wired to automatic injection; RAG and KB require
manual commands (`/rag`, `/kb inject`). A query such as "how do I
work in this project?" cannot draw from all three without separate
steps.

A second problem is resource pressure. Small models running on a
Raspberry Pi 500+ have context windows of roughly 2 K–8 K tokens.
Memory injection today has no token budget: `top_k` is the only
governor, and it is model-agnostic. On a 2 K context window, five
verbose memory documents can exhaust the budget before the user's
first question is in scope.

The goal of this redesign is to:

1. Unify retrieval across all three silos into a single ranked result
   set injected at session start.
2. Govern injection by a **token budget** proportional to the model's
   context window.
3. Add **workspace_profile** and **project_fact** memory types so the
   model always has stable workspace context without consuming
   retrieval budget.
4. Restructure `harvey.yaml` so all memory-related configuration
   lives under a single `memory:` key.
5. Add a **rolling summary** for in-session working memory so long
   conversations remain usable on small context models.

---

## Principles

**Workspace-centric.** Harvey knows nothing outside the workspace.
Every memory document, every index, every configuration file lives
under `agents/` in the workspace root. There is no global profile,
no cross-workspace state.

**Budget-first.** Memory injection is useful only when it fits in
the context window alongside the user's input and a meaningful
response. Token budget enforcement is not optional.

**Unified but manageable.** The three silos keep their separate
management commands (`/memory`, `/rag`, `/kb`) because they serve
different authoring workflows. The unification is in the *retrieval
and injection* path, not the storage path.

**Same model, separate call.** Harvey runs on hardware where only
one model is loaded at a time. Operations that require an LLM (rolling
summary compression, memory mining) use the current chat model in a
separate call, not a second model.

---

## Memory Taxonomy (paper → Harvey mapping)

The survey organises agent memory along three axes.

### Forms (what carries memory)

| Form | Harvey | Notes |
|------|--------|-------|
| Token-level Flat (1D) | `.spmd` files + SQLite/FTS5/cosine | Core storage; keep as-is |
| Token-level Planar (2D) | — | Out of scope for this design |
| Parametric | — | Not applicable; Harvey does not fine-tune |
| Latent | — | Not applicable; no KV-cache control |

Harvey operates entirely in token-level flat memory. This is the right
choice for a local, resource-constrained agent: it is transparent,
editable, and requires no infrastructure beyond SQLite.

### Functions (why memory is needed)

| Function | Types | Injection priority |
|----------|-------|--------------------|
| Factual — workspace profile | `workspace_profile` | 1 (always, highest) |
| Factual — project facts | `project_fact` | 2 (always, high) |
| Experiential — tool use | `tool_use` | 3 (semantic retrieval) |
| Experiential — workflows | `workflow` | 3 (semantic retrieval) |
| Experiential — preferences | `user_preference` | 4 (semantic retrieval) |
| RAG knowledge | RAG store chunks | 5 (if tokens remain) |
| KB observations | Knowledge base | 6 (if tokens remain) |

Factual types are always injected first because they are stable,
compact, and high-value: a model that does not know who it is working
with or what project it is in will produce lower-quality output
regardless of how good its experiential retrieval is.

### Dynamics (how memory operates)

| Dynamic | Current state | Change |
|---------|---------------|--------|
| Formation — post-hoc mining | `/memory mine` (manual) | Add auto-mine on session end |
| Formation — real-time | None | Out of scope |
| Evolution — supersedes/archive | Implemented | Keep |
| Evolution — consolidation | None | Out of scope (future) |
| Retrieval — at session start | `injectMemoryContext` (Memory only) | Replace with unified retrieval |
| Retrieval — per-turn | None | Out of scope |
| Working memory — compression | None | Add rolling summary |

---

## New Memory Types

```
workspace_profile   Stable facts about who works in this workspace and their
                    role. Created once by the onboarding flow. Updated manually
                    via /memory profile update or by replacing the document.

project_fact        Workspace-specific technical facts: language, build system,
                    key conventions, entry points. Auto-extracted from
                    codemeta.json / go.mod / package.json / .git/config at first
                    use; user-editable thereafter.
```

Both types live in `agents/memories/<type>/` alongside existing types.
Both are included in standard memory management commands (`/memory list`,
`/memory show`, `/memory forget`).

The existing types (`tool_use`, `workflow`, `user_preference`) are
unchanged. No type is removed.

---

## Config Restructuring

### Goal

Move `RagStores`, `RagActive`, `RagEnabled`, `KnowledgeDB`, and
`CurrentProjectID` from the top-level `Config` struct into `MemoryConfig`.
Update `harvey.yaml` serialisation to place all of these under
`memory:`. Keep reading the old top-level `rag:` and `knowledge_db:`
keys for backward compatibility, but never write them.

### New `MemoryConfig` (Go)

```go
type MemoryConfig struct {
    // Master switch and injection behaviour
    Enabled       bool
    TopK          int
    InjectOnStart bool
    BudgetPct     float64  // fraction of model context window (default 0.25)

    // RAG stores (moved from Config)
    RagStores []RagStoreEntry
    RagActive string
    RagEnabled bool

    // Knowledge base (moved from Config)
    KnowledgeDB      string
    CurrentProjectID int64

    // Rolling summary
    RollingSummary RollingSummaryConfig
}

type RollingSummaryConfig struct {
    Enabled   bool     // default true
    WarnAtPct float64  // warn at this fraction of context window (default 0.80)
    KeepTurns int      // keep last N turns verbatim during compression (default 6)
}
```

Accessor methods currently on `Config` (`ActiveRagStore`,
`RagStoreByName`, `AddOrUpdateRagStore`, `RemoveRagStore`) move to
`MemoryConfig`. `SaveRAGConfig` is renamed `SaveMemoryConfig` and
writes the full `memory:` section.

### New `harvey.yaml` shape

```yaml
memory:
  enabled: true
  inject_on_start: true
  budget_pct: 0.25
  top_k: 5
  rolling_summary:
    enabled: true
    warn_at_pct: 0.80
    keep_turns: 6
  rag:
    active: harvey
    enabled: true
    stores:
      - name: harvey
        db_path: agents/rag/harvey.db
        embedding_model: nomic-embed-text
  knowledge_base:
    db_path: agents/knowledge.db
    current_project: ""
```

Old top-level keys (`rag:`, `knowledge_db:`) are read silently for
backward compat and migrated into `memory:` on the next save.

---

## Unified Retrieval

### `UnifiedMemory` (`memory_unified.go`)

```go
type UnifiedResult struct {
    Source  string   // "workspace_profile", "project_fact",
                     // "experiential", "rag", "kb"
    ID      string   // memory ID, RAG chunk ID, or KB observation ID
    Content string   // formatted text ready for context injection
    Score   float64  // cosine similarity or keyword score; 1.0 for factual types
    Tokens  int      // estimated token count of Content
}

type UnifiedMemory struct {
    store  *MemoryStore
    cfg    *MemoryConfig
    wsRoot string
}

// Recall queries all silos and returns a budget-constrained result list.
// Factual types (workspace_profile, project_fact) are always returned first,
// regardless of score. Remaining slots are filled by scored results from
// experiential memories, RAG, and KB until budgetTokens is exhausted.
func (u *UnifiedMemory) Recall(
    query    string,
    embedder Embedder,
    budget   int,
) ([]UnifiedResult, error)
```

### Hybrid retrieval strategy

Research on agentic search (arXiv 2605.15184v1) shows that lexical
(exact-match) search is competitive with semantic (vector) search in
agent harnesses, and substantially cheaper. On a Pi, each embedding
call is a round-trip to Ollama; FTS5 is free.

`Recall` uses a two-path strategy:

**Fast path — always runs:**
FTS5 text search against description, summary, and tags in
`memories.db`. This is the primary path when no embedder is
configured or available. Results are scored by FTS5 rank.

**Slow path — runs when embedder is available:**
Cosine similarity against stored embedding vectors, same as the
current `MemoryStore.Query` implementation.

The two result sets are merged by score, deduped, and truncated to
`budget`. The fast path replaces the current `Recent()` fallback —
"most recent" is a poor proxy for relevance.

**Implication for the REPL:** the `/memory recall` display command
always uses the slow path (embedder required) because it is
interactive and quality matters more than latency. Session-start
injection uses whichever path is available.

### Token budget computation

```
budget = OllamaContextLength * Memory.BudgetPct
```

`OllamaContextLength` comes from `Config.OllamaContextLength`, which
is set by `/inspect` or populated from the Ollama model info at
startup. When it is zero (unknown), fall back to a conservative
default of 512 tokens.

### Real-time vs. offline retrieval paths

Edge LLM research (peerj-cs-3769) distinguishes two access patterns
that map directly to Harvey's two retrieval modes:

| Mode | Trigger | Path | Latency target |
|------|---------|------|----------------|
| Real-time | Session start injection | Fast (FTS5 first, semantic if available) | < 200 ms total |
| Offline | `/memory recall`, background mining | Slow (full semantic + reranking) | seconds acceptable |

Memory formation (mining, auto-mine) and stats recording always run
on the offline path, after the session or in the background.

### Context injection format

The injected message groups results by source with minimal markup so
small models can parse it without confusion:

```
[memory context]

[workspace profile]
<content of workspace_profile docs>

[project facts]
<content of project_fact docs>

[relevant experience]
tool_use: <description>. <summary>
workflow: <description>. <summary>

[knowledge]
<rag chunk or kb observation>
```

---

## Workspace Profile Onboarding

At `Agent.Reset()` (session start), check whether
`agents/memories/workspace_profile/` contains any `.spmd` files.
If not, and if the session is interactive (not replay), run the
onboarding flow before injecting memory context:

```
Harvey: I don't have a workspace profile yet. A few quick questions:

> What should I call you in this workspace? _
> What is your role here? (developer / researcher / writer / other) _
> Primary language(s) or tools? e.g. Go, TypeScript, Python _
> Anything else I should know about this project? (Enter to skip) _
```

Immediately after, attempt to auto-extract `project_fact` from the
workspace in this order:

1. `codemeta.json` — name, description, programmingLanguage,
   developmentStatus
2. `go.mod` — module name
3. `package.json` — name, description
4. `.git/config` `remote.origin.url`
5. Ask if nothing was found

Both documents are written as `.spmd` memory files and saved to
the store. Onboarding is detected purely by file presence; no config
flag is needed.

The onboarding flow is a separate function (`RunOnboarding`) in
`memory_onboarding.go` so it can be unit-tested independently.

---

## Rolling Summary (Working Memory)

### Trigger

After every LLM reply, immediately before the REPL prompt is
re-displayed:

```go
if cfg.Memory.RollingSummary.Enabled &&
   cfg.OllamaContextLength > 0 &&
   float64(CountTokens(History)) / float64(cfg.OllamaContextLength) >=
       cfg.Memory.RollingSummary.WarnAtPct {
    compressHistory(agent, cfg.Memory.RollingSummary.KeepTurns, out)
}
```

### Compression

`compressHistory` takes all turns except the last `KeepTurns` and
asks the current model in a **separate, non-recorded call**:

```
System: You are a summariser. Summarise the following conversation
        history in at most 150 tokens. Focus on decisions made,
        files changed, errors resolved, and context the user provided.

User:   <older turns as plain text>
```

The reply replaces the older turns with a single synthetic user
message:

```
[Session history compressed — summary: <reply>]
```

The last `KeepTurns` turns are kept verbatim. The overall history is
now short enough that the model can continue without confusion.

### Notification

Harvey prints one line before compressing:

```
[context ~82% full — compressing older turns]
```

No confirmation is required. The user can always `/session save` to
keep the full uncompressed history on disk before it is lost.

---

## `/memory recall` Command

New subcommand under `/memory`:

```
/memory recall <query text>
```

Calls `UnifiedMemory.Recall(query, embedder, maxResults=10)` with a
generous token budget (no cap; this is a display command, not
injection). Displays results grouped by source with scores:

```
[workspace profile]  workspace_profile_a1b2c3
  R. S. Doiel — systems developer — Go, TypeScript

[project fact]       project_fact_d4e5f6
  Harvey: terminal coding agent; Go module; AGPL-3.0

[experience 0.91]    tool_use_a3f891
  Run git init when git reports 'not a repository'

[rag 0.87]           harvey/FOUNTAIN_FORMAT.md (chunk 2)
  The scene heading format is INT. MEMORY <TIMESTAMP>...

[kb 0.74]
  Finding: small models benefit from explicit project context
```

---

## `/memory` Command Surface (unchanged)

Existing subcommands are not renamed or removed. `recall` is additive:

| Subcommand | Purpose |
|------------|---------|
| `mine` | Post-hoc extraction from session files |
| `list [type]` | List active memories, optionally filtered |
| `show <id>` | Display full memory document |
| `forget <id>` | Archive a memory |
| `status` | Counts, manifest summary, store path |
| `recall <query>` | **NEW** — unified search across all silos |

---

## Adaptive Budget Tuning

`BudgetPct` defaults to `0.25` — a conservative starting point for
small models. Over time Harvey accumulates enough session data to
suggest better values.

### What is tracked

Each session records two counters into a new `memory_stats` table in
`agents/memories/memories.db`:

| Column | Meaning |
|--------|---------|
| `session_id` | Filename of the session `.spmd` file |
| `budget_tokens` | Tokens allocated (`OllamaContextLength * BudgetPct`) |
| `injected_tokens` | Tokens actually injected (sum of `UnifiedResult.Tokens`) |
| `compressed` | 1 if rolling summary fired at least once during the session |
| `recorded_at` | UTC timestamp |

The row is written at session end (same time as auto-mine, Phase 5).

### What is tracked (extended)

Edge benchmarking research (JEC_1047_Ray_Pradhan.pdf) confirms that
throughput (`tokens_per_sec`) is a reliable real-time indicator of
model stress on Pi hardware. Harvey already captures this in
`ChatStats.TokensPerSec` after every LLM call. Recording the session
average alongside budget stats enables a third signal.

Extended `memory_stats` row:

| Column | Meaning |
|--------|---------|
| `avg_tokens_per_sec` | Average `ChatStats.TokensPerSec` across all turns in the session |

### When to suggest

At `/memory status`, if `memory_stats` has >= 10 rows, Harvey
computes three signals:

1. **Budget saturation** — `avg(injected_tokens / budget_tokens)`:
   if > 0.90, the budget is being maxed out and relevant memories are
   likely cut off. Suggestion: increase `budget_pct`.
2. **Compression frequency** — fraction of sessions where
   `compressed = 1`: if > 0.50, rolling summary fires often. This is
   informational — compression is working as intended.
3. **Throughput trend** — if `avg_tokens_per_sec` is low (< 2 tok/s
   on this hardware) AND budget saturation is high, the model is
   struggling with a large context. Suggestion: *reduce* `budget_pct`
   and rely more on lexical retrieval. This is the opposite of signal
   1 and the signals are evaluated together.

### Output format (in `/memory status`)

```
Memory budget:  25% of context (512 tokens on granite3.3:2b)
Budget advice:  avg utilisation 93% over last 12 sessions —
                consider increasing memory.budget_pct to 0.35
```

Or, if things look healthy:

```
Memory budget:  25% of context (512 tokens on granite3.3:2b)
Budget advice:  avg utilisation 61% — current setting looks good
```

Harvey never changes `budget_pct` automatically; it only surfaces the
suggestion. The user edits `harvey.yaml` to act on it.

### Schema addition

```sql
CREATE TABLE IF NOT EXISTS memory_stats (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT    NOT NULL,
    budget_tokens  INTEGER NOT NULL DEFAULT 0,
    injected_tokens INTEGER NOT NULL DEFAULT 0,
    compressed   INTEGER NOT NULL DEFAULT 0,  -- bool
    recorded_at  TEXT    NOT NULL
);
```

This table is added to `memoriesSchema` in `memory_store.go` and
populated at session end by a new `RecordSessionStats` method on
`MemoryStore`.

---

## Out of Scope

The following are noted but deferred:

- **Planar / hierarchical memory (2D/3D):** graph or tree structure
  between memory documents. Useful for concept mapping but adds
  retrieval complexity not justified at current scale.
- **Per-turn retrieval:** querying memory on every model turn, not
  just at session start. Doubles LLM calls on Pi hardware.
- **Consolidation / forgetting policies:** merging near-duplicate
  memories or decaying low-access ones. Useful future work.
- **Parametric / latent memory:** fine-tuning or KV-cache
  manipulation. Requires infrastructure not available on Pi.
- **Auto-mine on session end (v1):** desirable but lower priority than
  unified retrieval and budget management. Deferred to a follow-on
  plan item.
