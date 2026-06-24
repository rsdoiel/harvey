# Harvey Audit Trail Enhancements — Design

**Status (2026-06-24):** Design draft for v0.0.15. See
[audit-trail-plan.md](audit-trail-plan.md) for the phased
implementation plan.

**References:**
- "Tool Use and AI Scientists" — Corin Wagen, *Chemical Weapons
  Avoidance Newsletter* (cwagen.substack.com, 2026). Primary motivating
  article. Argues that tool calls are the primary mechanism for AI
  interpretability, serving as an audit log of agent decisions.

---

## Motivation

Wagen's article identifies four advantages of tool use in AI systems:
domain-specific knowledge integration, external reality checks,
computational efficiency, and — most relevant here — **interpretability
through audit trails**. Tool calls create transparent decision logs that
allow users to understand and verify AI reasoning and rapidly diagnose
failures.

Harvey's Fountain session format records dialogue, file writes, and
shell commands, but has four gaps that prevent sessions from serving as
full audit trails:

1. **Tool calls are unstructured.** Structured tool loop calls appear as
   prose action blocks ("Harvey calls read_file: {args}") rather than
   parseable notes. Tool results are not recorded at all, so there is no
   way to know from the session whether a tool succeeded or failed.

2. **RAG retrieval is invisible.** When `ragAugment` prepends context
   chunks to the user's prompt, no trace appears in the session file.
   If Harvey gives a wrong answer because a stale or irrelevant chunk
   was injected, the session provides no diagnostic evidence.

3. **Memory injection is invisible.** `UnifiedMemory.Recall` fires once
   per session and injects workspace profiles and prior memories into the
   system context. This injection leaves no trace in the Fountain file —
   a reader cannot see what prior knowledge shaped the session.

4. **Multi-model tool calls are unattributed.** When a user invokes
   `@mention` to forward a turn to a routed model (e.g. `@mistral`),
   any tool calls made during that turn are attributed to HARVEY rather
   than to the model that requested them.

---

## Scene model

A Harvey Fountain session file is a **sequence of many scenes**, not a
single scene. Each discrete interaction — a chat turn, a shell command,
a file write group, a skill activation — opens its own scene with a
timestamped heading. `RecordTurnWithStats` writes a new
`INT. HARVEY AND RSDOIEL TALKING TIMESTAMP` heading for **every chat
turn**. A session with ten chat turns, two shell commands, and one file
write contains thirteen or more scenes.

Understanding this is essential to placing the new audit elements
correctly:

- `[[tool: ...]]` and `[[rag: ...]]` notes are **not new scenes**. They
  are Fountain notes written **inside the existing per-turn
  `INT. HARVEY AND RSDOIEL TALKING` scene** for the turn where they
  occurred.
- `INT. CONTEXT RECALL` **is** a new scene type. It appears once at the
  start of a session, before the first chat turn, when
  `UnifiedMemory.Recall` returns non-empty results.

A complete session file with all four new elements looks like this:

```
Title: Harvey Session
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-06-24 10:00:00
Draft date: 2026-06-24

FADE IN:


INT. CONTEXT RECALL 2026-06-24 10:00:01          ← new scene (W3), once only

[[recall: workspace_profile_250928 (workspace_profile) — score 1.00]]
[[recall: tool_use_d55f70 (tool_use) — score 0.75]]


INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:00:05   ← turn 1, no tools, no RAG

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

RSDOIEL
What is 2+2?

HARVEY
Forwarding to LLAMA3.

LLAMA3
4


INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:01:10   ← turn 2, RAG fired (W2)

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

[[rag: 3 chunks from rag_store.db, top score 0.87]]

RSDOIEL
How do I initialise a Go module?

HARVEY
Forwarding to LLAMA3.

LLAMA3
Run: go mod init <module-path>


INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:02:30   ← turn 3, tool loop (W1)

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

RSDOIEL
Does harvey.go compile cleanly?

HARVEY
Forwarding to LLAMA3.

[[tool: read_file({"path":"harvey.go"}) — ok]]
[[tool: run_shell({"cmd":"go build ./..."}) — error: exit 1]]

LLAMA3
There is a compilation error. Line 42 references an undefined variable.


INT. SHELL 2026-06-24 10:03:15                        ← separate scene for shell cmd

RSDOIEL
! go build ./...

SHELL
./harvey.go:42:5: undefined: foo

[[shell: go build ./... — exit 1]]


EXT. MISTRAL AND RSDOIEL 2026-06-24 10:04:00          ← remote route (W4)

Harvey routing to MISTRAL (cloud API). Workspace: <workspace>.

RSDOIEL
@mistral review this fix

HARVEY
Forwarding to MISTRAL.

[[MISTRAL.tool: read_file({"path":"harvey.go"}) — ok]]

MISTRAL
The fix looks correct. The variable should be declared at line 38.


THE END.
```

This illustrates the central rule: **a new scene opens for each
interaction; notes appear inside the scene where they occur**. A
multi-round tool loop (model calls tool, gets result, calls another
tool, gets result, produces final answer) generates multiple
`[[tool: ...]]` notes all within the same `INT. HARVEY AND RSDOIEL
TALKING` scene — not in separate scenes. See the "Alternatives
considered" section for why.

---

## INT./EXT. semantic correction

The existing `FOUNTAIN_FORMAT.md` v1.1 defined `INT.` as "Harvey is
involved" and `EXT.` as "direct model-human, no Harvey" — making `EXT.`
effectively hypothetical and unused. The recorder always wrote `INT.`
for every scene, including remote route dispatches and cloud API calls.

The correct reading of the theatrical metaphor is geographical:
**`INT.` = computation happening on the local machine; `EXT.` =
computation happening on a remote system.** This makes `EXT.` scenes
practically meaningful and frequent:

| Interaction | Old | Corrected |
|---|---|---|
| Local Ollama / Llamafile | INT. | INT. (unchanged) |
| Shell command | INT. | INT. (unchanged) |
| File write / agent action | INT. | INT. (unchanged) |
| Skill activation | INT. | INT. (unchanged) |
| Context recall | INT. | INT. (unchanged) |
| Remote Ollama route (`@pi2`) | INT. (wrong) | **EXT.** |
| Cloud API route (`@mistral`) | INT. (wrong) | **EXT.** |
| Direct model-human (no Harvey) | EXT. (hypothetical) | EXT. (confirmed) |

When Harvey routes to a remote endpoint, HARVEY still appears in the
EXT. scene as the forwarding character. HARVEY is absent only when the
conversation is truly direct (no Harvey involvement at all).

Updating the spec is **W0** — the first step before any code changes —
so that the recorder implementation has a clear spec to target.

---

## Design principles

**Keep `audit.jsonl` separate.** Harvey already has `audit.go`, a
ring-buffer + NDJSON log at `agents/audit.jsonl`. This log targets
machine-readable security auditing (`/audit show`). The new Fountain
notes target human readability and memory-miner parsability within
`.spmd` session files. Bridging the two systems would couple unrelated
concerns and add complexity without clear benefit.

**Status-only for tool results.** Tool output can be very large (e.g.
`read_file` returning a whole source file). Recording the full content
would bloat session files and confuse the memory miner. A status-only
record — `ok` or `error: <first line>` — conveys what matters for audit
purposes without the content.

**Notes inside scenes, not new scenes.** `[[tool:]]` and `[[rag:]]`
elements belong inside the existing per-turn scene, not in separate
scenes of their own. A "turn" from the user's perspective is one
request-response cycle; splitting it across multiple scenes would make
sessions harder to read and harder to mine. `INT. CONTEXT RECALL` is
the only new scene type because memory injection is a distinct
pre-session event, not part of any individual turn.

**Silent when nothing fires.** RAG provenance and context recall notes
are emitted only when something actually happened. A session with RAG
off and no memories should not contain empty `[[rag: 0 chunks]]` or
`INT. CONTEXT RECALL` scenes.

**Fountain v1.2.** The new notation extends the format with four new
`[[...]]` note types and one new scene type. Older parsers that do not
recognise these elements should ignore them gracefully (the Fountain
spec permits unknown elements).

---

## Architecture

### W1 — Structured `[[tool: ...]]` notes

**Current state.** `ToolCallRecord{Name, Args}` captures tool calls from
`toolCallsFromHistory` (which scans assistant-role messages in history).
`formatToolCallAction` emits a prose action block. `RecordTurnWithStats`
calls `writeAction` for each record.

**Change.** Add `Result string` and `Character string` to
`ToolCallRecord`. Extend `toolCallsFromHistory` to also scan tool-role
messages and pair each call with its result status. Rename
`formatToolCallAction` → `formatToolCallNote`; change it to return note
content (without `[[` brackets) for `writeNote`. In `RecordTurnWithStats`,
switch from `writeAction` to `writeNote`.

**Where the notes appear.** Tool call notes are written inside the
`INT. HARVEY AND RSDOIEL TALKING` scene opened by `RecordTurnWithStats`
for that turn, between HARVEY's forwarding line and the model's reply.
Multiple rounds of a tool loop (e.g. model calls two tools before giving
its final answer) produce multiple flat notes in the same scene:

```
INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:02:30

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

RSDOIEL
Does harvey.go compile cleanly?

HARVEY
Forwarding to LLAMA3.

[[tool: read_file({"path":"harvey.go"}) — ok]]
[[tool: run_shell({"cmd":"go build ./..."}) — error: exit 1]]

LLAMA3
There is a compilation error on line 42.
```

**How result status is derived.** Tool-role messages whose `Content`
starts with `"error:"` (the convention set by `ExecuteToolCalls` in
`tool_executor.go`) are recorded as `error: <first line>`. All others
are `ok`. This avoids any changes to the executor.

### W2 — RAG provenance notes

**Current state.** `ragAugment(prompt string) string` returns the
augmented prompt and calls `a.DebugLog.LogRAGInject(...)` with the store
name, chunk count, and top score. That information is not forwarded to
the recorder.

**Change.** Add `RAGAugmentInfo{StoreName, Chunks, TopScore}` (in
`recorder.go` alongside `ToolCallRecord`). Change `ragAugment` to return
`(string, *RAGAugmentInfo)`, returning `nil` when RAG did not fire.
Add `ragInfo *RAGAugmentInfo` to `RecordTurnWithStats`. When non-nil,
emit a `[[rag: ...]]` note inside the turn's scene, immediately after
the scene action block and before the user dialogue.

**Where the note appears.** The `[[rag:]]` note is written inside the
`INT. HARVEY AND RSDOIEL TALKING` scene for the turn where RAG fired.
It appears once per turn where RAG retrieved chunks, before the user's
dialogue line (because RAG context was prepended to the user's prompt
before it was sent):

```
INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:01:10

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

[[rag: 3 chunks from rag_store.db, top score 0.87]]

RSDOIEL
How do I initialise a Go module?

HARVEY
Forwarding to LLAMA3.

LLAMA3
Run: go mod init <module-path>
```

Turns where RAG did not fire have no `[[rag:]]` line at all.

### W3 — `INT. CONTEXT RECALL` scene

**Current state.** `injectMemoryContext` (in `harvey.go`) calls
`um.Recall(query, embedder, budget)` and formats the results as a user
message injected into history. The `Recorder` is on the `Agent` struct
but is not called here.

**Change.** Add `RecordContextRecall(results []UnifiedResult) error` to
`Recorder`. Call it from `injectMemoryContext` after a non-empty
`Recall` result, guarding against `nil` recorder. The method writes a
new `INT. CONTEXT RECALL TIMESTAMP` scene with one `[[recall: ...]]`
Fountain note per result.

**Where the scene appears.** Memory injection fires once per session
(on the first chat turn, before the prompt is sent). The
`INT. CONTEXT RECALL` scene is therefore written before the first
`INT. HARVEY AND RSDOIEL TALKING` scene — it represents the session's
starting knowledge state, not any specific chat turn:

```
FADE IN:


INT. CONTEXT RECALL 2026-06-24 10:00:01

[[recall: workspace_profile_250928 (workspace_profile) — score 1.00]]
[[recall: tool_use_d55f70 (tool_use) — score 0.75]]


INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:00:05

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

RSDOIEL
(first user prompt)
...
```

`UnifiedResult.ID` and `UnifiedResult.Source` are already present; no
changes to the `UnifiedResult` struct are needed.

### W4 — Character-attributed tool calls

**Current state.** All tool call records carry an implicit HARVEY
attribution. When the `@mention` path calls `DispatchToEndpoint` (for
routes) or `RunToolLoop` on a forwarded model, the model that requested
the tool calls is not tracked.

**Scope limit for v0.0.15.** Route dispatch (`DispatchToEndpoint`) is
handled separately and does not use the tool executor loop reviewed
here. Character attribution applies to the local `RunToolLoop` path
when Harvey switches models via `@mention` (local model switch, not
remote route dispatch). Multi-character mixed-model loops (where
different models make different rounds of tool calls within a single
turn) are deferred — v0.0.15 applies one character name per turn.

**Change.** Add `Character string` to `ToolCallRecord` and
`CharacterName string` to `ToolExecutor`. In `terminal.go`, the
`@mention` local-switch path creates a `ToolExecutor` after switching
models; set `ex.CharacterName = strings.ToUpper(mentionName)` there.
Update `toolCallsFromHistory` to accept `charName string` and stamp all
extracted records with it.

**Where the attributed notes appear.** When Harvey routes to a remote
endpoint the scene is `EXT.` (remote computation). Character-attributed
tool notes appear inside that EXT. scene, between HARVEY's forwarding
line and the remote model's reply:

```
EXT. MISTRAL AND RSDOIEL 2026-06-24 10:04:00

Harvey routing to MISTRAL (cloud API). Workspace: <workspace>.

RSDOIEL
@mistral review this fix

HARVEY
Forwarding to MISTRAL.

[[MISTRAL.tool: read_file({"path":"harvey.go"}) — ok]]

MISTRAL
The fix looks correct.
```

---

## Fountain format v1.2 additions

| Syntax | Where it appears | When written |
|---|---|---|
| `[[tool: name(args) — status]]` | Inside `INT. HARVEY AND … TALKING` scene | Each structured tool call in a tool loop |
| `[[CHARACTER.tool: name(args) — status]]` | Inside `INT. HARVEY AND … TALKING` scene | Same, when model attribution is known |
| `[[rag: N chunks from STORE, top score S.SS]]` | Inside `INT. HARVEY AND … TALKING` scene | RAG context was injected for this turn |
| `[[recall: ID (SOURCE) — score S.SS]]` | Inside `INT. CONTEXT RECALL` scene | One item recalled at session start |

New scene type: `INT. CONTEXT RECALL TIMESTAMP` — written once at
session start when `UnifiedMemory.Recall` returns non-empty results.
Never appears mid-session.

---

## Alternatives considered

**Bridge `audit.jsonl` and Fountain.** Routing `AuditBuffer.Add` to
also write a Fountain note would unify the two paths. Rejected: the
audit buffer is initialised before the recorder, and its events
(command execution, file reads, security checks) are lower-level than
what belongs in the session narrative. Coupling them would require the
audit buffer to hold a recorder reference and complicate shutdown order.

**Full tool result content in Fountain.** Recording the full output of
each tool call makes sessions maximally auditable. Rejected for v0.0.15:
`read_file` on a large source file or a broad search result would make
session files unwieldy and degrade memory miner quality. Status-only
achieves the diagnostic goal (did it succeed?) without the cost.

**`[[rag: ...]]` as parenthetical in scene description.** Placing the
RAG note in the scene description line (e.g. alongside "Model: ... .
Workspace: ...") would keep it in a single action block. Rejected: the
scene description is written once at the scene open; RAG fires later in
`runChatTurn`. A separate note written just before user dialogue is
temporally accurate and avoids restructuring `RecordTurnWithStats`.

**`INT. TOOL LOOP` scene per tool-call round.** A structured tool loop
can involve multiple model↔tool rounds (model calls tool, gets result,
calls another tool, gets result, produces final answer). Each round
could be its own `INT. TOOL LOOP TIMESTAMP` scene. Rejected: a "turn"
from the user's perspective is one request-response cycle. Splitting it
across multiple scenes makes the session harder to read (the user's
original question and the model's final answer would be in different
scenes) and harder to mine (memory extraction relies on
question-and-answer locality within a scene). Flat notes inside the
single turn scene preserve both readability and mining quality.

**Per-message character attribution via `Message.Model`.** Accurate
multi-round character attribution requires tagging each `Message` with
the model that produced it. This changes the `Message` struct and
ripples through serialisation, history compaction, and replay. Deferred:
single-character-per-turn covers the real-world case and adds no struct
changes in v0.0.15.
