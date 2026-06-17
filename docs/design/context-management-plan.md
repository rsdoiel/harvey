# Design: Context-Bounded Agentic Task Execution

**Status:** All three phases implemented and shipped.

---

## Problem

When Harvey executes multi-step tasks, each tool call adds two messages to history:
an `assistant` message (with `ToolCalls`) and a `tool` message (with the result). After
several files are written, the model re-processes the entire accumulated chain on every
subsequent turn.

Measured: Qwen 9B second turn took 506 s; Qwen 4B climbed from 7 s → 116 s → 280 s → 314 s
across five turns of a six-file task. The rolling summary only fires at 80% context
capacity — far too late for agentic bursts.

---

## Design Decisions

**D1 — Tool result compaction in `RunToolLoop`, not `terminal.go`**
`RunToolLoop` knows exactly when a tool result has been "consumed" (immediately after the
next LLM response). Compacting there keeps the logic local to tool execution. `terminal.go`
only knows about turns, not individual tool rounds.

**D2 — Compact in-place; do not maintain a separate ContextHistory slice**
Rejected splitting `History` into full + context slices: it doubles the complexity of every
history-touching path (recording, memory mining, compression, routing). Instead: compact
the content of already-consumed tool messages. Fountain recording captures tool calls via
`RecordAgentAction` before compaction, so the session transcript is unaffected.

**D3 — Compaction replaces Content, preserves ToolCallID**
The LLM needs `ToolCallID` to correlate results with requests. `Content` is replaced with
`[called: tool_name, ...]` / `[done]`. The assistant's `ToolCalls` field is cleared to nil
since the model doesn't benefit from re-reading its own prior tool request JSON.

**D4 — Compaction is opt-out via config, not opt-in**
Default: on. Context growth harms all users of local models; the benefit is universal.
Opt out with `tools: tool_result_compaction: false` in harvey.yaml.

**D5 — Plan command, not a skill**
Skills inject LLM instructions into context. Plans require Harvey-side orchestration:
parsing structured output, persisting state, building bounded context per step. Implemented
as `/plan` (slash command) backed by `plan.go` + `plan_cmd.go`.

**D6 — Plan stored in workspace file, not in-memory**
`agents/plan.md` persists across `/clear` and session restarts. A plan started in one
session can be continued in another. The file is also human-readable documentation of
what was done and what remains.

**D7 — Plan execution uses a fresh bounded context per step**
Each `/plan next` call sends only: system prompt + current plan state + step instruction.
Prior conversation history is excluded. Context is O(plan_size) regardless of step count.
This is the key property that eliminates context growth for plan-driven work.

**D8 — Multi-file skill is Phase 3 (deferred)**
A skill can auto-invoke the plan pattern for common requests. Deferred until Phase 1 and
Phase 2 are validated in production.

---

## Implementation

### Phase 1 — Tool result compaction ✓

| File | Change |
|------|--------|
| `tool_executor.go` | `compactToolRound` helper; `prevRoundStart` tracking in `RunToolLoop`; `ToolResultCompaction` field on `ToolExecutor` |
| `config.go` | `ToolResultCompaction bool` (default true); `tool_result_compaction` YAML field; load/save via `SaveMemoryConfig` |
| `tool_executor_test.go` | 4 unit tests: basic, multiple tools, round isolation, bad-index noop |

**Measured result:** turn times with compaction on:
19 s → 114 s → 150 s → **5 s** → **14 s** vs 7 s → 116 s → 280 s → 314 s → 55 s without.
The dramatic improvement at turns 5–6 is compaction removing 3 rounds of full tool output
from re-processing.

### Phase 2 — `/plan` command ✓

| File | Change |
|------|--------|
| `plan.go` | `Plan`, `PlanStep` structs; `LoadPlan`, `SavePlan`, `PlanFromLLMResponse`, `PrintPlan`, `NextStep`, `MarkDone`, `AllDone`, `Summary` |
| `plan_cmd.go` | `/plan TASK`, `/plan next`, `/plan status`, `/plan show`, `/plan clear` |
| `commands.go` | Register `"plan"` entry |
| `plan_test.go` | 9 unit tests: GFM checklist parsing, checked steps, numbered fallback, fallback goal, step navigation, completion, mark-done, summary, round-trip save/load |

Plan file format (`agents/plan.md`):
```markdown
# Plan: <goal>

<!-- created: <RFC3339> -->
<!-- updated: <RFC3339> -->

- [ ] step one
- [x] step two (done)
- [ ] step three
```

`/plan next` builds a fresh history and runs `RunToolLoop` — no prior conversation included.

### Phase 3 — Multi-file skill ✓

**File:** `agents/skills/multi-file/SKILL.md`

Trigger regex (case-insensitive):
```
/creat.*\b(demo|component|module|package|project|app|site|page|api)\b|write.*\b(files?|components?|modules?)\b|build.*\b(demo|component|project|app)\b/i
```

**LLM-fallback path (no compiled script yet):**
1. Harvey detects the trigger and calls `DispatchSkill`, which injects the skill body as context
2. `DispatchSkill` returns `llmNeeded=true` (LLM-fallback path)
3. `terminal.go` falls through to `runChatTurn` instead of `continue`
4. Model responds with a `# Plan:` GFM checklist per the skill body instructions
5. `terminal.go` detects the plan in the reply, calls `SavePlan`, and shows the user `/plan next`

**Compiled script path (future):**
SKILL.md includes guidance for generating `scripts/compiled.bash`. The script uses
`HARVEY_API_BASE` (now injected into all skill environments) to call the LLM API directly,
writes `agents/plan.md`, and exits — no interactive turn needed.

**Infrastructure changes shipped alongside the skill:**
- `DispatchSkill` returns `(bool, error)`: `true` when LLM follow-up is needed
- `terminal.go` uses the bool to fall through vs `continue` after skill dispatch
- `runChatTurn` reply is now captured; if `skillWantsLLM` and reply contains a plan,
  `SavePlan` is called automatically
- `HARVEY_API_BASE` added to compiled-script environment (derived from active backend config)

---

## Verification checklist

- [x] `go test ./...` passes with all new tests
- [x] Phase 1 validated: debug log shows flat turn times for multi-file task
- [ ] Phase 2 validated: `/plan next` × N keeps turn times flat (pending user test)
- [x] Phase 3 implemented and validated
