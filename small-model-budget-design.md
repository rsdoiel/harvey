# Harvey Small-Model Budget Management — Design Exploration

**Date**: 2026-06-28  
**Status**: Exploration — not yet approved for implementation  
**Related**: [chunked-analysis-design.md](chunked-analysis-design.md),
[capability-adapter-concept.md](capability-adapter-concept.md),
[plan-ivr-design.md](plan-ivr-design.md)

---

## The Unifying Problem

Harvey runs on an 8B model on a Raspberry Pi with 15.8 GiB RAM, CPU-only.
The nominal context window may be 8K–32K tokens, but at CPU speed on Pi,
long contexts cause two distinct failure modes:

- **Memory exhaustion (OOM)**: the KV cache for a large context does not fit
  in available RAM. The process dies or stalls. This is what happened when
  `natural_language_programming.md` (30KB, ~7,500 tokens) was injected into a
  session that already had history and memories loaded.
- **Quality degradation**: even within the memory limit, small models produce
  significantly worse output as context fills. The "effective" context for
  reliable inference is often 30–50% of the nominal window.

Harvey has several mechanisms that each address one slice of this problem:

| Mechanism | What it handles | Where |
|-----------|----------------|-------|
| Chunked analysis | File content too large for one turn | `builtin_tools.go`, `chunk_analyzer.go` |
| Rolling summary | History growing too long | `memory_rolling.go` |
| RAG score threshold | Too many RAG chunks retrieved | `terminal.go` (`ragMinScore`) |
| `injectFileContext` cap | File injection capped at 64KB | `file_inject.go` |
| `capOutput` | Tool result output capped | `builtin_tools.go` |

These mechanisms were designed independently. None of them coordinate.
A file might pass the 64KB `injectFileContext` cap (e.g., a 30KB file) but
still overflow context when combined with 4K tokens of existing history, a
2K system prompt, and 1K of injected memories.

**The hypothesis:** there is a general relationship between large file handling
and complex prompt handling — both are instances of the same resource constraint.
A unified budget-aware approach would handle both more reliably than the current
collection of independent caps.

---

## The Context Budget Model

Before each turn, the available context budget can be computed:

```
budget = model.ContextWindow - safety_margin
       - len(system_prompt_tokens)
       - len(history_tokens)         // all prior turns
       - len(memory_tokens)          // injected memories and RAG
       - response_headroom           // tokens reserved for the model's reply
```

`remainingContext(a)` in `context_estimator.go` already computes this
(subtracting history, system prompt, and a 10% safety margin). What is
missing is the per-component accounting and the allocation policy that
decides what to do when a component exceeds its share.

### Budget components in priority order

| Priority | Component | Current behavior when tight |
|----------|-----------|----------------------------|
| 1 | System prompt + workspace profile | Always included; never trimmed |
| 2 | Response headroom (~1K tokens) | Implicit; not explicitly reserved |
| 3 | Current user input | Always included; can be arbitrarily large |
| 4 | Memories + KB context | Included if budget allows (approximate) |
| 5 | RAG context | Capped by `ragMinScore` threshold |
| 6 | File content (injected) | Capped at 64KB, not budget-aware |
| 7 | Prior history | Compressed by rolling summary when full |

The problem is that items 5 and 6 are not aware of each other or of items
3 and 4. When a large file is injected (item 6) and RAG is on (item 5) and
the session has 20 turns of history (item 7), the model receives more than
it can handle, and fails.

---

## The Relationship Between Large Files and Complex Prompts

A large file is one specific case of "input that exceeds the remaining budget."
A complex prompt is another. Both share the same response: **decompose the
work into budget-sized pieces**.

### Large file → chunked analysis (already designed)

When `read_file` would deliver more content than `remainingContext() × threshold`,
chunked analysis breaks the file into paragraphs, processes each with a
map call, and synthesizes a combined result.

### Complex prompt → plan decomposition (partially implemented)

When a user's request requires more steps than can fit in one turn with
reliable output, `/plan` breaks the request into bounded-context steps.
Today the user triggers this explicitly (`/plan TASK`). There is no
automatic detection that a prompt is "too complex for one turn."

### Multi-file request → neither mechanism handles this

When a user says "review these three files: A.go, B.go, C.go" and each
file is 8KB:
- `injectFileContext` injects all three (total 24KB, ~6K tokens)
- No budget check is run across the aggregate
- With history + memories, this likely overflows

The chunked analysis feature handles one large file. It does not handle
multiple medium files whose aggregate exceeds budget.

---

## Design Directions

These are exploratory directions, not implementation plans. Each needs
refinement before it can be planned.

### Direction 1 — Budget-aware `injectFileContext`

The most impactful near-term change. Make `injectFileContext` a method on
`Agent` so it can access the budget:

```go
func (a *Agent) injectFileContext(prompt string) string
```

For each file candidate extracted from the prompt:
1. Estimate its token cost (`fileExceedsBudget`).
2. Track a running total against `remainingContext(a)`.
3. When the running total would exceed, say, 40% of the budget:
   - Stop injecting files.
   - Append a note: `[Note: N additional files exceeded context budget and were not injected: X, Y, Z]`

This is a purely defensive improvement. It does not add chunked analysis
to the injection path (too complex for a synchronous path), but it prevents
the OOM case that triggered the original bug.

A more aggressive version: when a single file exceeds budget, instead of
injecting it, route it to chunked analysis synchronously before the turn
begins. This requires making the injection path async-capable, which is
a larger change.

### Direction 2 — Explicit budget allocation for RAG and memories

Currently `ragAugment` and `UnifiedMemory.Recall` each consume tokens
without knowing what the other took. Add a shared budget tracker:

```go
type BudgetTracker struct {
    Total    int
    Used     int
}

func (b *BudgetTracker) Reserve(tokens int) bool {
    if b.Used+tokens > b.Total { return false }
    b.Used += tokens
    return true
}
```

Pass a `BudgetTracker` to both `ragAugment` and `UnifiedMemory.Recall`.
Each consumes from the same pool. When the pool is exhausted, further
injections are skipped rather than causing overflow.

This also resolves the dual RAG injection audit (TODO.md): with a shared
tracker, double-injection is harmless because the second injection is
budget-checked against what the first already consumed.

### Direction 3 — Complexity detection for automatic plan decomposition

When the user submits a prompt, estimate its complexity before sending:

```go
func estimatePromptComplexity(prompt string, history []Message) ComplexityLevel {
    // heuristics:
    // - number of distinct file references
    // - presence of multi-step keywords ("then", "after that", "finally")
    // - estimated turn count from known patterns
}
```

If complexity exceeds a threshold and `remainingContext` is below a second
threshold, offer (don't force) a plan decomposition:

```
  ⚠ This request involves multiple steps and the context is 75% full.
  Decompose into a plan? [Y/n]:
```

This is the most speculative direction. Complexity estimation from text is
imprecise, and false positives (offering /plan when it's not needed) are
annoying. Leave for a later exploration once Directions 1 and 2 are in place.

### Direction 4 — Effective context window calibration

Instead of using the model's nominal context window, calibrate an
"effective" window for reliable inference on Pi:

```yaml
# harvey.yaml
models:
  llama3.2:
    context_window: 8192
    effective_context: 4096   # actual reliable window on this hardware
```

`remainingContext(a)` would use `effective_context` when set, falling back
to `context_window`. Users can tune this per-model based on observed quality.

This is a simple, low-risk change with high practical impact because it
makes the budget math more honest without requiring any algorithmic changes.

---

## Relationship to Other Features

### Chunked analysis

Chunked analysis is the right answer for large single files read via `read_file`.
The bug fix (Direction 1 above) addresses the gap where files reach the model
via `injectFileContext` instead of `read_file`.

The two mechanisms are complementary, not competing:
- `injectFileContext` → budget-aware, cap or skip files that exceed budget
- `read_file` + chunked analysis → decompose large files into LLM sub-calls

### IVR repair loops

An IVR repair attempt is itself a turn that consumes context. Under the current
budget model, if a step fills 90% of context, there is no room for a repair
turn. A budget-aware system would refuse to offer repair when the remaining
budget cannot support it, rather than launching a repair that itself OOMs.

This is a concrete prerequisite for IVR on small models: IVR must be budget-aware,
not just repair-count-aware. The `RepairBudgetTotal` field in the deferred IVR
design (`plan-ivr-design.md`) is the right instinct but needs the budget tracker
from Direction 2 to be meaningful.

### Idea 3 (strict output enforcement)

Format correction re-prompts (one extra turn per correction) also consume
context. The correction turn is small (~100 tokens) but the model's prior
response (which is in history) is not. Budget-awareness would prevent
attempting correction when the context is already too full for a useful response.

### Idea 4 (tool-loop-aware rolling summary)

Structured `ToolTurnSummary` entries are more space-efficient than prose
compression of the same information. A tool call with a large result compresses
to a single short line rather than a multi-sentence prose summary. This
directly increases the effective budget available per turn by reclaiming
space in compressed history.

---

## Immediate Actions

Before any of the above directions are implemented, two low-risk changes
should happen:

1. **Lower `maxInjectFileBytes` in `file_inject.go`** from 64KB to 16KB.
   This immediately reduces the worst-case injection from ~16K tokens to
   ~4K tokens, making OOM much less likely without any algorithmic changes.
   It is a one-line fix with a clear rationale.

2. **Add `effective_context` to the model cache** (Direction 4 above).
   This is a config-only change that lets the Pi user tell Harvey "this model
   reliably handles 4K tokens, not 8K" without code changes.

These two changes close the immediate safety gap while the larger design
directions are explored.
