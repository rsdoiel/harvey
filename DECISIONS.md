# Harvey — Architecture & UX Decision Log

This file records significant architectural and UX decisions, their rationale, and known trade-offs. New decisions are added at the top. Each entry names the decision, the context that prompted it, the chosen approach, the rejected alternatives, and the consequences.

---

## 2026-06-08 — `/loop` chat iterations use a shared `runChatTurn` helper that skips skill auto-trigger and `autoExecuteReply`

**Context.** The REPL's plain-chat path does more than call the model: it
checks whether the input matches a skill trigger pattern (auto-dispatching to
a different flow entirely), and after the reply, offers to write fenced code
blocks to disk via an interactive Y/n prompt (`autoExecuteReply`). Both make
sense for a human typing one message at a time; both are problematic when the
same prompt is sent N times unattended — a skill could fire on iteration 3
but not iteration 1, and a Y/n prompt would block forever waiting on stdin
that nothing will type.

**Decision.** Factor the REPL's inline chat block (`terminal.go`, roughly
lines 635-820) into a shared `(a *Agent) runChatTurn(ctx, input, out) (reply
string, stats ChatStats, err error)`. It keeps everything that defines "how
Harvey answers a prompt" — RAG augmentation, the tool-loop-or-plain-chat
branch, token/context warnings, stats, Fountain recording — and excludes
skill auto-trigger matching and `autoExecuteReply`, both of which belong to
"how the REPL reacts to a typed line." `/loop` calls this helper directly for
its chat-mode iterations; the REPL becomes a thin wrapper around the same
helper plus its own skill-trigger/`autoExecuteReply` handling.

**Rejected alternatives.**

- *Reuse the REPL's inline chat block as-is* — a looped prompt could silently
  jump to a different skill mid-run, or stall on iteration 1 waiting for a
  keypress that never comes.
- *Duplicate the chat block inside `cmdLoop`* — roughly 150 lines of
  copy-paste that would drift from the REPL's version on the next change to
  the chat path.

**Consequences.**

- `terminal.go`'s plain-chat branch is refactored but behaviourally unchanged
  for normal typed input — verified with `go test -race` after extraction.
- `/loop` behaves predictably: the same prompt produces the same kind of
  exchange every time, with no surprise skill redirects or stalled prompts.
- If `a.Config.ToolsEnabled`, looped prompts can still cause the model to
  write files or run commands via the normal tool loop — `/loop` does not
  suppress this, since doing so would make looped chat behave differently
  from normal chat (see `loop-design.md`, "Safety Considerations").

---

## 2026-06-08 — `/loop` caps iterations at 100 and defaults to 10

**Context.** `/loop` is the first Harvey command that can run LLM calls — and,
with tools enabled, write files or execute shell commands — repeatedly and
unattended. Harvey's existing security posture (safe mode, permission system,
audit log) is built around bounding and surfacing risky actions rather than
trusting the user to always type the right thing.

**Decision.** `/loop` takes an optional `--count N` (following the
`--depth N` convention already established by `/read-dir`), defaulting to 10
and capped at 100. There is no "run forever" option.

**Rejected alternatives.**

- *Unbounded by default* — the one command that could turn a typo
  (`/loop 1s tell me a joke`) into thousands of unattended LLM calls before
  the user notices.
- *Confirmation prompt before starting* — adds a keypress without adding much
  safety; the printed plan summary (`Looping every 5m, up to 10 times: ...`)
  gives the same "last chance to Ctrl+C" moment without an extra interaction
  step, consistent with how `/pipeline` announces its plan before running.

**Consequences.**

- A fully unattended `/loop` run is bounded to at most 100 iterations — e.g.
  roughly 8 hours at a 5-minute interval — which still covers realistic
  "check on this periodically" use cases.
- Users who need more must re-invoke `/loop`, a deliberate speed bump rather
  than an oversight.

---

## 2026-06-08 — `/loop` requires an explicit interval; no self-pacing mode

**Context.** Claude Code's `/loop` can omit the interval and let the agent
self-pace via a wake-scheduling primitive. Harvey has no equivalent — it is a
synchronous CLI process with no persistent scheduler or "wake me up later"
mechanism.

**Decision.** `INTERVAL` is a required first argument to `/loop`, parsed with
the existing `parseDurationString` helper (`config.go:650`, already used for
`run_timeout`/`ollama_timeout` in `harvey.yaml`). There is no self-pacing
mode.

**Rejected alternatives.**

- *Have Harvey "guess" an interval once and run at that fixed cadence* — just
  a worse version of asking the user, with an extra layer of
  unpredictability.
- *Keep the process resident and let it wake itself* — a fundamentally
  different program shape than Harvey's synchronous REPL; far outside the
  scope of adding one command.

**Consequences.**

- `/loop`'s usage string and help text always show `INTERVAL` as required.
- Users coming from Claude Code's `/loop` will notice the difference; the
  help text explains why (no async scheduler in Harvey).

---

## 2026-06-08 — `/loop` runs as a blocking foreground command, not a background goroutine

**Context.** Harvey's REPL (`terminal.go:Run`) is a single-threaded loop that
blocks on each turn, mutating `a.History`, `a.Recorder`, and the shared output
writer with no locking — because nothing has ever run concurrently with it.
Adding a command that repeats a prompt on an interval raises the question of
whether it should run in the background while the user keeps typing, or take
over the REPL until it finishes.

**Decision.** `/loop` runs in the foreground inside its own command handler,
reusing the SIGINT-cancellation pattern already used three times in
`terminal.go` (chat, `!` commands, `@mention` dispatch): one cancellable
context for the whole run, a goroutine watching `os.Signal`, and a
`wasCancelled` check. Any Ctrl+C — mid-iteration or during the
inter-iteration sleep — stops the whole loop and returns to the prompt.

**Rejected alternatives.**

- *Background goroutine* — would require introducing locking around
  `a.History`, `a.Recorder`, and `out`, none of which exist today. The
  concurrency-safety surface this opens is large relative to the value of
  letting the user type while the loop runs.
- *"Ctrl+C cancels the iteration; a second Ctrl+C stops the loop"* — a second
  control surface nothing else in Harvey has; rejected for consistency with
  the existing single-Ctrl+C-aborts convention.

**Consequences.**

- `/loop` blocks the REPL for its duration — communicated up front via a
  printed plan summary before the first iteration runs.
- `/loop status` / `/loop stop` subcommands aren't meaningful (the REPL can't
  read them while blocked) and are not implemented.
- No new synchronization primitives are introduced anywhere in Harvey.

---

## 2026-06-05 — Profile templates and help guides ship embedded in the binary

**Context.** Harvey installs by copying a single executable to `$HOME/bin`. Users on three OS / two CPU architectures should not need to install a separate asset package. Templates and help guides must therefore travel with the binary.

**Decision.** Use Go's `//go:embed` directive (standard library since Go 1.16) to compile a `templates/` directory tree into the binary at build time. A single `EmbeddedTemplates embed.FS` variable in `templates.go` gives the rest of Harvey read access to template and help guide content at runtime. Workspace-local templates in `agents/templates/profiles/` are checked at runtime and merged with the built-in list, allowing organisations to add shared templates without patching Harvey.

**Rejected alternatives.**

- *Separate asset directory alongside the binary* — breaks the single-file install model.
- *Download templates from the internet on first run* — requires network access, adds failure modes, complicates offline use on a Raspberry Pi.
- *Templates in `harvey.yaml`* — templates are multi-line prose documents; embedding them in YAML is unreadable and fragile to edit.

**Consequences.**

- `templates/` directory added to the Harvey source tree; must be maintained alongside code.
- Binary size increases modestly (six `.fountain` files and three Markdown guides are small).
- `templates.go` is the single registration point for all embedded assets.

---

## 2026-06-05 — Initial developer/writer template set; library templates deferred

**Context.** Harvey needs a useful starting set of profile templates but the full range of library staff roles requires domain expertise and UX review that is not yet available.

**Decision.** Ship five developer/writer templates for v1:

| Template | Role |
|----------|------|
| `backend-developer` | Go, Python, TypeScript+Deno, SQL for application work |
| `frontend-developer` | HTML, CSS, TypeScript/JavaScript, Deno bundling |
| `dataset-developer` | Front end plus SQL, dataset CLI, datasetd web service |
| `data-scientist` | Data analysis, SQL for exploration, Python data tooling |
| `technical-writer` | Documentation, man pages, tutorials, Markdown and Fountain |

Library role templates (subject specialist, systems/digital, instruction/data literacy, support staff) are deferred until library staff and a UX colleague can define the categories and content correctly. Placeholder files are named in the plan but not authored.

**Rejected alternatives.**

- *Ship library templates based on external assumptions* — risks producing templates that do not match how library staff actually work, which would undermine trust in the feature.

**Consequences.**

- Library users who try Harvey before the library templates ship will use `blank.fountain` or one of the developer templates as a starting point. Acceptable short-term.

---

## 2026-06-05 — `/profile use` verb; `/profile` top-level alias

**Context.** The profile switching command needed a name consistent with Harvey's existing command vocabulary. Two candidates were considered: `switch` and `use`.

**Decision.** Use `use` as the subcommand verb because it matches the established pattern in Harvey: `/ollama use`, `/rag use`, and `/kb use` all select the active item from a list. Register `/profile` as a top-level alias delegating to `/memory profile`, following the same one-line handler pattern as `/recall` → `/memory recall`.

**Rejected alternatives.**

- *`/profile switch`* — `switch` does not appear elsewhere in Harvey's command vocabulary. `use` is already the selection verb.
- *`/switch-profile` or `/change-profile`* — hyphenated commands are not the Harvey convention.

**Consequences.**

- `commands.go` gains a `"profile"` entry in the top-level command table (identical in structure to `"recall"`).
- `cmdMemoryProfile` gains a `"use"` dispatch case.
- `/memory profile use`, `/profile use`, and `/profile` (showing subcommand help) all work.

---

## 2026-06-05 — Profile switching writes a Fountain handoff document

**Context.** When a user switches profiles mid-session with `/profile use`, the in-progress conversation context would be lost after `ClearHistory()`. The user may need to resume the previous context in a future session.

**Decision.** Before clearing history, `/profile use` writes a `.fountain` summary file to `agents/hand-off/<timestamp>.spmd`. The handoff captures the last N assistant messages as bullet points and lists file paths and open questions from recent turns. No LLM call is required — the handoff is structural, not summarised. Because it is a `.fountain` file, the memory miner can extract facts from it in a later session, migrating context from the old role into the new session's experience memories over time.

The previous `workspace_profile` document is archived (status set to `archived`) rather than deleted, preserving the history of who this workspace has been used as.

**Rejected alternatives.**

- *No handoff* — context is lost on profile switch; acceptable only if profiles are rarely switched.
- *LLM-generated summary* — higher quality but requires a blocking model call during the switch, adding latency and a failure mode.
- *Write handoff to the session file* — session files record conversation turns, not profile transitions; mixing them would complicate the memory miner.

**Consequences.**

- `agents/hand-off/` directory is created at workspace init alongside `agents/sessions/`.
- `writeHandoff()` function added to `harvey.go`.
- Memory miner learns to process files from `agents/hand-off/` as well as `agents/sessions/`.

---

## 2026-06-05 — Help guides for Ollama and PDF tools embedded in binary

**Context.** New users frequently fail to install Ollama or PDF extraction tools before running Harvey. The error messages Harvey currently produces do not explain what is missing or how to fix it. Users on three operating systems need platform-specific install instructions.

**Decision.** Embed short Markdown help guides (`templates/help/ollama.md`, `templates/help/pdf-tools.md`) in the binary using the same `//go:embed` infrastructure as profile templates. Surface them via `/help ollama` and `/help pdf-tools`. Print a one-line pointer to the relevant guide when a detection failure occurs at startup (Ollama unreachable) or during a command (PDF extraction fails). Guides are deliberately short: what it is, how to install on each platform, one troubleshooting line.

**Rejected alternatives.**

- *Link to external documentation only* — requires network access to get help; unhelpful in offline or restricted environments.
- *Inline error messages only* — install instructions for three platforms embedded in Go string literals are unmaintainable; Markdown guides are editable without touching code.

**Consequences.**

- `templates/help/` directory contains three Markdown files maintained alongside the code.
- `helptext.go` gains `OllamaHelpText` and `PDFToolsHelpText` helpers.
- `terminal.go` and `pdf_extract.go` each gain one conditional pointer line.

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
