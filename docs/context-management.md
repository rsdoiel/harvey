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

| Turn | Messages | Elapsed |
|------|----------|---------|
| 1 | 8 | 57 s |
| 2 | 10 | 506 s |
| 3 | 14 | 89 s |

The existing rolling summary fires at 80% of the context window — too late for agentic bursts,
and it treats tool messages as opaque text, losing the correlation structure.

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

`RunToolLoop` in `tool_executor.go`. The loop tracks `prevRoundStart` (the history index
where the previous round began). At the top of each iteration it calls `compactToolRound`
on that range before sending history to the LLM.

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

Or at runtime (takes effect next session):

```
/set tools.tool_result_compaction off
```

---

## Plan-execute pattern (Phase 2 — not yet implemented)

For very long multi-file tasks, even compacted tool messages accumulate. The plan-execute
pattern (`/plan` command, `harvey/plan.go`) addresses this by:

1. **Planning phase**: the model produces a compact checklist stored in `agents/plan.md`
2. **Execution phase**: each step runs in a **fresh bounded context**:
   - system prompt
   - current plan state (one line per step, ✓ or ○)
   - step instruction
   
   Prior conversation history is **not included**. Context is O(plan_size) regardless of
   how many steps have completed.

See `/Users/rsdoiel/.claude/plans/velvety-pondering-waffle.md` for the full design.

---

## Model selection guidance

| Model | Tool use | Context sensitivity | Recommended for |
|-------|----------|---------------------|-----------------|
| Qwen 4B | One tool call per turn; reliable with clean context | Fast (7–30 s/turn) | Conversation, explanation, single-file tasks |
| Qwen 9B | Batches multiple tool calls per turn | Slow, highly sensitive to context size | Multi-file tasks with compaction on |

With compaction enabled, the 9B model's turn times should stay roughly constant across a
multi-file task instead of growing with each completed step.

---

## Rolling summary

The existing rolling summary (`memory_rolling.go`) compresses older turns into a single
synthetic message when history exceeds `rolling_summary.warn_at_pct` (default 80%) of the
context window. It is complementary to compaction:

- **Compaction** handles tool-heavy agentic turns (fires eagerly, per-round)
- **Rolling summary** handles long conversations (fires late, token-threshold triggered)

Both can be active simultaneously. If compaction is working correctly, the rolling summary
should rarely need to fire during agentic sessions.

Configuration:

```yaml
memory:
  rolling_summary:
    enabled: true
    warn_at_pct: 0.80   # compress when history exceeds 80% of context window
    keep_turns: 6       # keep last 6 turns verbatim; summarise the rest
```
