# Harvey Context Management

## Why context grows during agentic tasks

When Harvey executes multi-step tasks — creating directories, writing files, running commands —
each completed tool call adds two messages to the conversation history:

1. An **assistant message** carrying the tool call requests (`Role="assistant"`, `ToolCalls=[...]`)
2. One or more **tool messages** carrying the results (`Role="tool"`, `Content=<output>`,
   `ToolCallID=<id>`)

These messages remain in history for every subsequent LLM turn. A task that writes ten files
accumulates 20+ messages the model must re-process on every turn, even though the output of
"wrote 512 bytes to demo/styles.css" carries no actionable information after the LLM has
seen it once.

Measured impact in production (Qwen 9B via llamafile on Apple M1 12 GB):

| Turn | Messages | Without compaction | With compaction |
|------|----------|--------------------|-----------------|
| 2 | 4–5 | 7 s | 19 s |
| 3 | 6–7 | 116 s | 114 s |
| 4 | 8–9 | 280 s | 150 s |
| 5 | 10–11 | 314 s | **5 s** |
| 6 | 12–13 | 55 s | **14 s** |

The existing rolling summary fires at 80% of the context window — too late for agentic bursts,
and it treats tool messages as opaque text, losing the correlation structure.

Two complementary mechanisms address this: **tool result compaction** (automatic, always-on)
and the **`/plan` command** (opt-in, for long multi-file tasks).

---

## Tool result compaction

### What it does

After the LLM produces a plain-text response (consuming the previous round of tool results),
Harvey compacts those results in place before the next LLM call:

- **Assistant message**: `ToolCalls` is set to `nil`; `Content` becomes
  `[called: tool_name, ...]`
- **Tool messages**: `Content` becomes `[done]`; `ToolCallID` is preserved so the
  LLM can still correlate if it refers back

Only the immediately preceding round is compacted; the current round's full results are
always delivered verbatim so the LLM can use them.

### Where it happens

`compactToolRound` in `tool_executor.go`, called from `RunToolLoop`. The loop tracks
`prevRoundStart` (the history index where the previous round began). At the top of each
iteration it compacts that range before sending history to the LLM.

### What is preserved

- `ToolCallID` on every tool message — required for protocol correctness
- Full tool content in the **current** round — the LLM needs it to decide next steps
- Fountain session recording — `RecordAgentAction` is called before compaction, so the
  session transcript retains the complete tool output

### Configuration

```yaml
tools:
  tool_result_compaction: false   # set to disable; default is on
```

---

## `/plan` command — bounded context execution

For very long multi-file tasks, even compacted tool messages accumulate. The `/plan` command
(`plan.go`, `plan_cmd.go`) eliminates context growth entirely by executing each step with a
**fresh bounded context** containing only the system prompt and current plan state.

### Workflow

```
/plan create a web component demo with JS, CSS, HTML
```

1. Harvey sends the task to the model with a planning prompt.
2. The model responds with a GFM checklist (`- [ ] step`).
3. The checklist is saved to `agents/plan.md` and persists across `/clear` and restarts.

```
/plan next     ← repeat until done
```

Each call:
1. Loads `agents/plan.md` and finds the first unchecked step.
2. Builds a **fresh history**: `[system prompt] + [plan content + step instruction]`.
   Prior conversation history is **not included**.
3. Runs `RunToolLoop` on the fresh history — the model calls the appropriate tool.
4. Marks the step done, saves the updated plan.

Context per step is O(plan size), not O(steps completed). Turn times stay flat.

### Subcommands

| Command | Effect |
|---------|--------|
| `/plan TASK` | Generate checklist, save to `agents/plan.md` |
| `/plan next` | Execute next unchecked step with bounded context |
| `/plan status` | Show ✓/○ checklist and progress count |
| `/plan show` | Print raw `agents/plan.md` |
| `/plan clear` | Delete `agents/plan.md` |

### Plan file format (`agents/plan.md`)

```markdown
# Plan: Create a web component demo

<!-- created: 2026-06-17T18:44:40Z -->
<!-- updated: 2026-06-17T19:02:11Z -->

- [x] Create demo/ directory
- [x] Write demo/styles.css
- [ ] Write demo/app.js
- [ ] Write demo/index.html
```

The file is human-editable. Steps can be added, reordered, or pre-checked manually.

---

## Model selection guidance

| Model | Tool use | Context sensitivity | Recommended for |
|-------|----------|---------------------|-----------------|
| Qwen 4B | One tool call per turn; reliable with clean context | Fast (7–30 s/turn) | Conversation, explanation, `/plan next` steps |
| Qwen 9B | Batches multiple tool calls per turn | Moderate with compaction on | Multi-file tasks, complex reasoning |

With compaction enabled the 9B model's turn times stay roughly constant instead of growing
with each completed step. For very long tasks (10+ files), `/plan` keeps times flat regardless
of model size.

---

## Rolling summary

The existing rolling summary (`memory_rolling.go`) compresses older turns into a single
synthetic message when history exceeds `rolling_summary.warn_at_pct` (default 80%) of the
context window. It is complementary to compaction:

- **Compaction** handles tool-heavy agentic turns (fires eagerly, per-round)
- **Rolling summary** handles long conversations (fires late, token-threshold triggered)

Both can be active simultaneously. If compaction is working correctly, the rolling summary
should rarely need to fire during agentic sessions.

```yaml
memory:
  rolling_summary:
    enabled: true
    warn_at_pct: 0.80   # compress when history exceeds 80% of context window
    keep_turns: 6       # keep last 6 turns verbatim; summarise the rest
```

---

## Key source files

| File | Purpose |
|------|---------|
| `tool_executor.go` | `RunToolLoop`, `compactToolRound` |
| `config.go` | `ToolResultCompaction` field and YAML wiring |
| `plan.go` | `Plan`, `PlanStep`, load/save/parse/format |
| `plan_cmd.go` | `/plan` command handler |
| `memory_rolling.go` | `ShouldCompress`, `CompressHistory` |
| `docs/design/context-management-plan.md` | Problem analysis, design decisions, Phase 3 design |
