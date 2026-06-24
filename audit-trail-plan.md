# Harvey Audit Trail Enhancements — Implementation Plan

See [audit-trail-design.md](audit-trail-design.md) for the full design
rationale and architecture decisions, including the scene model that
explains where each new element appears within a multi-scene session
file.

Target version: **v0.0.15**

Work items are ordered W1 → W2 → W3 → W4. Each is independent and
compiles, passes tests, and provides standalone value on its own. W1
establishes the `writeNote` pattern reused by W2; W4 builds on the
`Character` field added in W1.

**Key placement rule:** `[[tool:]]` and `[[rag:]]` notes are written
*inside* the scene that `RecordTurnWithStats` (INT.) or
`RecordExteriorTurn` (EXT.) opens for each chat turn. `INT. CONTEXT
RECALL` is the only new scene type; it is written once before the first
chat turn.

---

## W0 — Update `FOUNTAIN_FORMAT.md` to v1.2

**Goal:** Correct the INT./EXT. semantic and document all new notation
before any code changes, so the implementation has a clear spec to
target. This is a documentation-only step; no Go files change.

**INT./EXT. correction.** The v1.1 spec incorrectly defined `EXT.` as
"no Harvey involvement" (hypothetical). The correct reading is
geographical: `INT.` = local machine, `EXT.` = remote system. Route
dispatches and cloud API calls are EXT. even though Harvey routes them.

### Changes to `FOUNTAIN_FORMAT.md`

| Section | Change |
|---------|--------|
| Scene Types — INT. definition | "Harvey involved" → "computation runs locally" |
| Scene Types — INT. use cases | Remove remote routing and cloud @mention (now EXT.) |
| Scene Types — EXT. definition | "No Harvey" → "computation runs remotely"; add routing sub-case showing HARVEY in dialogue |
| Scene Types — EXT. use cases | Add remote Ollama routes and cloud API routes |
| Scenarios 2 & 3 | Change from INT. to EXT. scene headings and descriptions |
| Scenario 4 | Update note: HARVEY absent only when truly direct (no routing) |
| Scenario 6 | Add note: remote @mention routes each create their own EXT. scene |
| Scene Types Reference | Add EXT. routing example alongside existing EXT. direct example |
| Best Practices | Update INT./EXT. guidance to match new semantic |
| Changelog | Add v1.2 entry |

### Acceptance criteria

- `FOUNTAIN_FORMAT.md` version is 1.2 in the Changelog.
- INT. definition no longer mentions routing or cloud models.
- EXT. definition covers both routing (HARVEY in dialogue) and direct (no HARVEY) sub-cases.
- Scenarios 2 and 3 use `EXT.` headings.

---

## W1 — Structured `[[tool: ...]]` notes

**Goal:** Replace prose action blocks for tool calls with parseable
Fountain notes that also record result status, inside the existing
per-turn scene.

### Files to modify

| File | Change |
|------|--------|
| `recorder.go` | Add `Result string`, `Character string` to `ToolCallRecord`. Rename `formatToolCallAction` → `formatToolCallNote`; change return to note content for `writeNote`. In `RecordTurnWithStats`, switch `writeAction(formatToolCallAction(tc))` → `writeNote(formatToolCallNote(tc))`. |
| `terminal.go` | Extend `toolCallsFromHistory` to build a `map[ToolCallID]string` from `Role=="tool"` messages, then set each `ToolCallRecord.Result` to `"ok"` or `"error: <first line>"`. Add `charName string` parameter (pass `""` from existing call site). |
| `FOUNTAIN_FORMAT.md` | Add `[[tool: name(args) — status]]` to Special Syntax section. Begin v1.2 changelog entry. |

### `formatToolCallNote` logic

```go
func formatToolCallNote(tc ToolCallRecord) string {
    nameArgs := tc.Name + "()"
    if tc.Args != "" && tc.Args != "{}" && tc.Args != "null" {
        nameArgs = tc.Name + "(" + tc.Args + ")"
    }
    result := tc.Result
    if result == "" {
        result = "ok"
    }
    if tc.Character != "" {
        return tc.Character + ".tool: " + nameArgs + " — " + result
    }
    return "tool: " + nameArgs + " — " + result
}
```

### `toolCallsFromHistory` extension

```go
func toolCallsFromHistory(msgs []Message, charName string) []ToolCallRecord {
    // Build result map from tool-role messages.
    resultByID := make(map[string]string)
    for _, m := range msgs {
        if m.Role == "tool" && m.ToolCallID != "" {
            content := strings.TrimSpace(m.Content)
            first := strings.SplitN(content, "\n", 2)[0]
            if strings.HasPrefix(first, "error:") {
                resultByID[m.ToolCallID] = first
            } else {
                resultByID[m.ToolCallID] = "ok"
            }
        }
    }
    // Extract calls from assistant messages.
    var out []ToolCallRecord
    for _, m := range msgs {
        if m.Role != "assistant" || len(m.ToolCalls) == 0 {
            continue
        }
        for _, tc := range m.ToolCalls {
            result := resultByID[tc.ID]
            if result == "" {
                result = "ok"
            }
            out = append(out, ToolCallRecord{
                Name:      tc.Function.Name,
                Args:      tc.Function.Arguments,
                Result:    result,
                Character: charName,
            })
        }
    }
    return out
}
```

Update the existing call site in `terminal.go` to pass `""` as
`charName`. W4 will change this to the @mention model name.

### Expected scene output

Tool notes appear between HARVEY's forwarding line and the model's
reply, inside the existing `INT. HARVEY AND … TALKING` scene:

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

Multiple tool-call rounds within a single turn (multi-step tool loop)
produce multiple flat notes in the same scene — no new scene is opened
between rounds.

### Tests to add (in existing `recorder_test.go` or new file)

- `TestFormatToolCallNote_NoArgs` — `{Name:"list_files", Args:"", Result:"ok"}` → `"tool: list_files() — ok"`
- `TestFormatToolCallNote_WithArgs` — args present, `result="error: exit 1"` → correct note string
- `TestFormatToolCallNote_WithCharacter` — `Character:"MISTRAL"` → `"MISTRAL.tool: ..."`
- `TestToolCallsFromHistory_ResultExtraction` — history slice with assistant + tool messages; verify `Result` field populated correctly

### Acceptance criteria

- `go test ./...` passes.
- Session files contain `[[tool: ...]]` notes between HARVEY's forwarding line and the model reply, inside the existing `INT. HARVEY AND … TALKING` scene.
- Tool results from `tool`-role messages are correctly decoded as `ok` or `error: ...`.
- Turns with no tool calls produce no `[[tool:]]` lines.

---

## W2 — RAG provenance notes

**Goal:** Record which RAG store was queried, how many chunks were
returned, and the top similarity score, as a Fountain note inside the
turn's scene, before each turn where RAG fired.

### Files to modify

| File | Change |
|------|--------|
| `recorder.go` | Add `RAGAugmentInfo struct { StoreName string; Chunks int; TopScore float64 }`. Add `ragInfo *RAGAugmentInfo` as last parameter to `RecordTurnWithStats`. When non-nil, emit `[[rag: N chunks from STORE, top score S.SS]]` note after scene action block, before user dialogue. Update `RecordTurn` wrapper to pass `nil`. |
| `terminal.go` | Change `ragAugment(prompt string) string` → `ragAugment(prompt string) (string, *RAGAugmentInfo)`. Return `nil` when RAG does not fire; return `&RAGAugmentInfo{...}` using values already computed for `DebugLog.LogRAGInject`. Update call site at line 1058. Pass `ragInfo` to `RecordTurnWithStats` at line 1239. |
| `replay.go` | Update `RecordTurnWithStats` call at line 230 to pass `nil` for `ragInfo`. |
| `terminal_test.go`, `recorder_test.go` | Update `RecordTurnWithStats` calls to pass `nil`. |
| `FOUNTAIN_FORMAT.md` | Add `[[rag: N chunks from STORE, top score S.SS]]` to Special Syntax section. |

### `ragAugment` return change

The function already computes `topScore` and `entry.Name` before calling
`DebugLog.LogRAGInject`. Return `&RAGAugmentInfo{StoreName: entry.Name, Chunks: len(relevant), TopScore: topScore}` on the successful path; `nil` on all early-return paths.

### Note placement in `RecordTurnWithStats`

The `[[rag:]]` note is the first element written inside the scene after
the action block (scene description), before the user's dialogue. This
placement is accurate to the execution order: RAG context was prepended
to the user's prompt before the turn began.

```go
// After r.writeAction(...) for the scene description:
if ragInfo != nil {
    r.writeNote(fmt.Sprintf("rag: %d chunks from %s, top score %.2f",
        ragInfo.Chunks, ragInfo.StoreName, ragInfo.TopScore))
}
// Then: r.writeDialogue(r.userName, ...)
```

### Expected scene output

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

Turns where RAG did not fire have no `[[rag:]]` line. The note does not
appear in shell scenes, agent mode scenes, or any scene other than the
chat turn where RAG retrieved chunks.

### Tests to add

- `TestRecordTurnWithStats_RAGNote` — pass non-nil `RAGAugmentInfo`; verify `[[rag: ...]]` note appears in output before user dialogue, inside the scene.
- `TestRecordTurnWithStats_NoRAGNote` — pass `nil` ragInfo; verify no `[[rag:]]` line in output.
- `TestRAGAugmentReturnsInfo` — use a mock RAG store; verify `ragAugment` returns non-nil `*RAGAugmentInfo` when chunks are found, `nil` when RAG is off.

### Acceptance criteria

- `go test ./...` passes.
- Session files contain `[[rag: ...]]` before the user dialogue in turns where RAG fired.
- Sessions with RAG off or no matching chunks contain no `[[rag:]]` lines.

---

## W3 — `INT. CONTEXT RECALL` scene

**Goal:** Write a dedicated scene at session start listing every memory
item injected by `UnifiedMemory.Recall`, so the session's starting
knowledge state is auditable.

This is the only work item that adds a **new scene type**. All other
new elements (W1, W2, W4) are notes inside existing scenes.

### Files to modify

| File | Change |
|------|--------|
| `recorder.go` | Add `RecordContextRecall(results []UnifiedResult) error` method. Writes `INT. CONTEXT RECALL TIMESTAMP` scene heading, then one `[[recall: ID (SOURCE) — score S.SS]]` note per result. No-op when `len(results) == 0`. |
| `harvey.go` | In `injectMemoryContext` (line 324), after `um.Recall` returns non-empty `results` and before `a.AddMessage("user", FormatContext(results))`, add: `if a.Recorder != nil { _ = a.Recorder.RecordContextRecall(results) }` |
| `FOUNTAIN_FORMAT.md` | Add `INT. CONTEXT RECALL` to Scene Types Reference table. Note: written once at session start, never mid-session. |

### `RecordContextRecall` implementation

```go
func (r *Recorder) RecordContextRecall(results []UnifiedResult) error {
    if len(results) == 0 {
        return nil
    }
    ts := time.Now().Format("2006-01-02 15:04:05")
    r.writeSceneHeading(fmt.Sprintf("INT. CONTEXT RECALL %s", ts))
    for _, res := range results {
        r.writeNote(fmt.Sprintf("recall: %s (%s) — score %.2f",
            res.ID, res.Source, res.Score))
    }
    return nil
}
```

### Expected scene output

The `INT. CONTEXT RECALL` scene appears after `FADE IN:` and before
the first `INT. HARVEY AND … TALKING` scene:

```
FADE IN:


INT. CONTEXT RECALL 2026-06-24 10:00:01

[[recall: workspace_profile_250928 (workspace_profile) — score 1.00]]
[[recall: tool_use_d55f70 (tool_use) — score 0.75]]


INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:00:05

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

RSDOIEL
(first user prompt of the session)
...
```

Sessions with memory injection disabled or no recalled items skip the
`INT. CONTEXT RECALL` scene entirely — the first scene is the first
chat turn.

### Tests to add

- `TestRecordContextRecall_NonEmpty` — pass two `UnifiedResult` values; verify scene heading and two `[[recall: ...]]` notes appear in output, in document order (scene before first chat turn).
- `TestRecordContextRecall_Empty` — pass empty slice; verify nothing is written.

### Acceptance criteria

- `go test ./...` passes.
- Sessions where memories were recalled begin with an `INT. CONTEXT RECALL` scene before the first chat scene.
- Sessions with memory injection off or no recalled items open directly with the first `INT. HARVEY AND … TALKING` scene.

---

## W4 — EXT. scenes for remote routes + character-attributed tool calls

**Goal:** Two related changes that both stem from the INT./EXT.
correction in W0:

1. **`RecordExteriorTurn`** — a new `Recorder` method that writes an
   `EXT.` scene for remote route dispatches, replacing the current
   `RecordTurn` (which writes INT.) in the route dispatch path.
2. **Character-attributed tool notes** — when a remote route uses tools,
   label the notes with the route/model name using the `CHARACTER.tool:`
   prefix inside the EXT. scene.

**Scope.** Remote route dispatch goes through `DispatchToEndpoint` in
`terminal.go`. Local `@mention` model switches (where `attemptModelSwitch`
succeeds) continue to use `runChatTurn` and produce INT. scenes — those
are not remote computation. The `Character` field on `ToolCallRecord`
(added in W1) handles the note prefix once the character name is known.

### Files to modify

| File | Change |
|------|--------|
| `recorder.go` | Add `RecordExteriorTurn(endpoint, userInput, reply string) error` method. Writes `EXT. {ENDPOINT} AND {USER} {TIMESTAMP}` scene with HARVEY as forwarding character. `Character string` on `ToolCallRecord` already handles attributed note prefix (W1). |
| `terminal.go` | In the route dispatch path (around line 837–841), replace `a.Recorder.RecordTurn(input, reply)` with `a.Recorder.RecordExteriorTurn(strings.ToUpper(name), input, reply)`. Also pass `charName = strings.ToUpper(name)` through `runChatTurn` → `toolCallsFromHistory` for the local @mention switch path. |
| `tool_executor.go` | Add `CharacterName string` field to `ToolExecutor`. |

### `RecordExteriorTurn` implementation

```go
func (r *Recorder) RecordExteriorTurn(endpoint, userInput, reply string) error {
    ts := time.Now().Format("2006-01-02 15:04:05")
    r.writeSceneHeading(fmt.Sprintf("EXT. %s AND %s %s", endpoint, r.userName, ts))
    r.writeAction(fmt.Sprintf(
        "Harvey routing to %s. Workspace: %s.", endpoint, r.workspace,
    ))
    r.writeDialogue(r.userName, "", userInput)
    r.writeDialogue("HARVEY", "", fmt.Sprintf("Forwarding to %s.", endpoint))
    r.writeDialogue(endpoint, "", reply)
    return nil
}
```

Note: `RecordExteriorTurn` does not accept tool calls or RAG info for
v0.0.15. RAG does not fire on the route dispatch path (RAG augmentation
runs inside `runChatTurn`, which is not called for remote route
dispatches). Tool calls from remote routes are not yet captured in the
tool loop (that would require `DispatchToEndpoint` to return them).

### Character attribution for local @mention switch

For the local model-switch path (`attemptModelSwitch` success → falls
through to `runChatTurn`), pass `charName string` through `runChatTurn`
→ `toolCallsFromHistory`. Set `ex.CharacterName = charName` before
`RunToolLoop`. In all normal callers pass `""`.

### Expected scene output (remote route)

```
EXT. MISTRAL AND RSDOIEL 2026-06-24 10:04:00

Harvey routing to MISTRAL. Workspace: <workspace>.

RSDOIEL
@mistral review this fix

HARVEY
Forwarding to MISTRAL.

MISTRAL
The fix looks correct. The variable should be declared at line 38.
```

### Expected scene output (local @mention switch with tools)

```
INT. HARVEY AND RSDOIEL TALKING 2026-06-24 10:04:00

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

RSDOIEL
@llama3 review this fix

HARVEY
Forwarding to LLAMA3.

[[LLAMA3.tool: read_file({"path":"harvey.go"}) — ok]]

LLAMA3
The fix looks correct.
```

### Tests to add

- `TestRecordExteriorTurn` — verify EXT. scene heading, HARVEY forwarding line, endpoint reply in output.
- `TestToolCallsFromHistory_WithCharName` — pass `charName="LLAMA3"`; verify all records have `Character="LLAMA3"`.

### Acceptance criteria

- `go test ./...` and `go test -race` pass.
- Remote route dispatches produce `EXT. ENDPOINT AND USER TIMESTAMP` scenes.
- Local @mention switches continue to produce `INT. HARVEY AND … TALKING` scenes.
- Tool notes in EXT. scenes carry `ENDPOINT.tool:` prefix.
- Tool notes in local @mention INT. scenes carry the switched model name as prefix.

---

## Fountain format changelog entry

Add to `FOUNTAIN_FORMAT.md` Changelog table:

```
| 1.2 | 2026-06-24 | [[tool:]], [[CHARACTER.tool:]], [[rag:]], [[recall:]] notes; INT. CONTEXT RECALL scene |
```

---

## Full test run (after all four work items)

```bash
cd harvey
go test ./...
go test -race
go build -o bin/harvey cmd/harvey/*.go
```

Manual smoke test: start Harvey with tools enabled and RAG on, run two
chat turns (one with tool calls, one with RAG retrieval), then inspect
the `.spmd` session file to confirm:
- `INT. CONTEXT RECALL` scene (if memories exist) appears before the first chat scene
- `[[rag: ...]]` note appears inside the RAG turn's scene, before user dialogue
- `[[tool: ...]]` notes appear inside the tool turn's scene, between HARVEY's forwarding line and the model reply
- No extra scenes were created for tool loop rounds or RAG retrieval
