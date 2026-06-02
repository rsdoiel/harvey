# Harvey — Architecture & UX Decision Log

This file records significant architectural and UX decisions, their rationale, and known trade-offs. New decisions are added at the top. Each entry names the decision, the context that prompted it, the chosen approach, the rejected alternatives, and the consequences.

---

## 2026-06-02 — Persistent command history across sessions

**Context.** Harvey's `termlib.LineEditor` supports Up/Down arrow history navigation within a session, but the history is in-memory only and lost on exit. Users must retype slash commands, `!` shell commands, and prompts from prior sessions, which breaks flow — especially for repeated workflows like `/rag ingest`, `/memory mine`, or iterating on a prompt.

**Decision.** Persist the input history to `agents/harvey_history` inside the workspace (one entry per line, plain text). On startup Harvey loads this file and seeds the `LineEditor` before entering the REPL. On clean exit the in-memory history is written back, capped at **1000 entries** (most recent kept). Consecutive duplicate suppression is already handled by `AppendHistory`; no further deduplication is applied at write time.

The implementation requires two changes:

1. **`termlib` (`lineeditor.go`)** — add two methods to `LineEditor`:
   - `SetHistory(lines []string)` — replaces the in-memory history slice wholesale (used at startup).
   - `History() []string` — returns a copy of the current history slice (used at exit to write back).

2. **Harvey (`terminal.go`)** — add `loadCmdHistory(ws, le)` called after `le` is created (line ~225), and `saveCmdHistory(ws, le)` called in the REPL exit path. Both functions resolve the path as `ws.AbsPath("agents/harvey_history")`. `saveCmdHistory` truncates to the last 1000 entries before writing.

The history file path is not configurable in this iteration; `agents/` is Harvey's conventional home for all runtime state (`harvey.yaml`, `sessions/`, `memories/`, `rag/`, `knowledge.db`).

**Rejected alternatives.**

- *Global `~/.harvey_history`* — shares history across workspaces, which leaks commands and paths between projects. Harvey's workspace-boundary model makes per-workspace the correct scope.
- *Storing history in `agents/harvey.yaml`* — would pollute the config file with ephemeral runtime data and complicate config schema evolution.
- *Parsing `.spmd` session files for history* — session recordings are conversation transcripts, not command logs; extraction would be fragile and slow.

**Consequences.**

- `termlib/lineeditor.go` gains `SetHistory` and `History` methods.
- `harvey/terminal.go` gains `loadCmdHistory` and `saveCmdHistory` helper functions wired into the REPL startup and exit.
- No changes to `harvey.yaml` schema, `Config`, or any other subsystem.
- Concurrent Harvey sessions in the same workspace will silently overwrite each other's history on exit (last-writer-wins), consistent with bash's behaviour without `HISTFILE` locking.

---

## 2026-06-02 — UX nudge system for memory discoverability

**Context.** Users who understand the three storage silos (RAG / Memory / Knowledge Base) can get significantly better results, but the ingestion decision ("where does this go?") breaks flow. No built-in mechanism surfaced actionable hints about pending mining, empty RAG stores, or RAG being disabled.

**Decision.** Implement a four-part nudge system:

1. **Session-start digest** — a `sessionMemoryDigest()` function called after the ready line that prints dim hints only when a condition is actionable:
   - Unmined sessions pending → suggest `/memory mine`
   - Active RAG store is empty → suggest `/rag ingest`
   - RAG off but chunks exist → suggest `/rag on`
   No output is printed when everything looks healthy.

2. **Enhanced `/status`** — extend `cmdStatus` with a Memory/RAG summary block (active memories, unmined sessions, active store, chunk count, RAG on/off). Keeps the one-stop status view complete.

3. **New `/hint` command** — on-demand improvement suggestions that aggregate all three silos and explain the decision rule. Verbose version of the session digest with context about *why* each suggestion matters.

4. **`/help learn` topic** — a unified "How Harvey learns" help page with a three-column table (what to ingest → which command → where it goes) and the single decision rule:
   - Have a text file or document? → `/rag ingest`
   - Something useful happened in a session? → `/memory mine`
   - Making an observation about an experiment? → `/kb observe`

5. **`/recall` alias** — routes to `/memory recall` to make the unified retrieval interface the obvious entry point.

**Rejected alternatives.**
- *Single storage silo* — would reduce configuration but lose retrieval precision for small models. Topic-scoped RAG stores (e.g., `deno_typescript`, `go`) give better recall than one large mixed store.
- *Always-on verbose status* — printing all memory info on every startup is too noisy. Only surface hints when actionable.
- *Merging `/rag on` + `/memory recall` into a single toggle* — the per-prompt RAG injection (`ragAugment`) and session-start injection (`UnifiedMemory.Recall`) are different channels. A single toggle would require auditing whether `UnifiedMemory` already includes RAG chunks. Deferred to a future audit.

**Consequences.**
- `terminal.go` gains a `sessionMemoryDigest()` call after the ready line.
- `commands.go` gains `cmdHint`, enhanced `cmdStatus`, and a `/recall` registration.
- `helptext.go` gains `LearnHelpText`.
- `cmdHelp` dispatches `"learn"` and `"memory-overview"` to `LearnHelpText`.
- `help` topic list is updated to include `learn`.

---

## 2026-06-02 — model_map in RAG stores (deferred simplification)

**Context.** Each RAG store entry in `harvey.yaml` has a `model_map` field that maps generation models to embedding models. In practice every store uses `nomic-embed-text` for all generation models, making the map redundant.

**Decision.** Deferred. Do not remove `model_map` now. The code is already correct and operational. Remove it when there is a concrete reason to simplify the config schema (e.g., adding a new embedder type that makes the override meaningful).

**Consequences.** `model_map` remains in the config and `ragAugment` continues to honour it. No user-visible change.

---

## 2026-06-02 — Dual RAG injection audit (deferred)

**Context.** Harvey has two RAG injection paths that run independently:
1. Per-prompt via `ragAugment()` in `terminal.go` (when `a.RagOn`)
2. Session-start via `UnifiedMemory.Recall()` which also queries the RAG store

A user with both `memory.enabled` and `rag.enabled` may receive RAG content twice per turn — once in the system prompt injection and once prepended to each prompt. This wastes context tokens and may confuse small models.

**Decision.** Deferred. Audit and fix when a user observes noticeably degraded context efficiency. The fix would be to either: (a) skip RAG chunks in `UnifiedMemory.Recall()` when `a.RagOn` is true, or (b) make `ragAugment` a no-op when `UnifiedMemory` already injected from the same store.

**Consequences.** Known overlap. No immediate action required.

---

## 2026-05-31 — prose tool call correction injection

**Context.** Small models emit tool calls as JSON fenced blocks rather than structured API responses. The original `tryExecuteProseToolCalls` returned `bool` and could not distinguish "dispatched successfully" from "dispatched but every call errored". When models hallucinated tool names the warning was suppressed because `len(results) > 0` was always true.

**Decision.** Change `tryExecuteProseToolCalls` to return `(dispatched bool, unknownNames []string)`. Track a `succeeded` counter internally; set `dispatched = true` only when ≥1 call succeeded. When `unknownNames` is non-empty, inject a correction message into history *after* `a.AddMessage("assistant", ...)` so history ordering is: user → assistant → correction-user. This gives the model a chance to retry with the correct tool names.

**Consequences.** The `noToolCalls` guard also gates `autoExecuteReply` to prevent directory-tree code blocks from being offered as files to write after successful tool-call turns.

---

## 2026-05-31 — histLenBeforeChat pattern for noToolCalls guard

**Context.** Harvey needs to know whether a chat turn resulted in structured tool calls (via `RunToolLoop`) so it can skip `autoExecuteReply` when tool calls already handled file writing. The check `len(a.History) == histLenBeforeChat` correctly detects no tool calls only when captured before `a.AddMessage`.

**Decision.** Capture `histLenBeforeChat := len(a.History)` before the `Chat/RunToolLoop` call. Compute `noToolCalls := len(a.History) == histLenBeforeChat` *before* `a.AddMessage`. This invariant must be preserved: any refactor that moves `a.AddMessage` before the `noToolCalls` check will silently break the guard.

**Consequences.** Documented as a key invariant in `CLAUDE.md`.

---

## 2026-05-28 — Three-silo memory architecture

**Context.** Harvey needs to accumulate knowledge across sessions without polluting the LLM context window on every turn. Three distinct content types require different ingestion and retrieval strategies: (1) external documents, (2) session experience, (3) research observations.

**Decision.** Three independent silos unified at retrieval time by `UnifiedMemory.Recall()`:

| Silo | Ingestion | Retrieval |
|---|---|---|
| RAG store | `/rag ingest` (explicit) | Per-prompt via `ragAugment()` |
| Memory store | `/memory mine` or auto-mine on exit | Session-start via `UnifiedMemory` |
| Knowledge base | `/kb observe` (explicit) | On-demand via `UnifiedMemory` |

**Consequences.** Each silo has its own command namespace (`/rag`, `/memory`, `/kb`). The unified retrieval via `/memory recall` is the recommended entry point. All three silos share a token budget enforced at injection time.
