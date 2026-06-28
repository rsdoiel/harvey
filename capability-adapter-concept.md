# Capability-Based Routing and Adapter Tiers — Concept

**Source:** IBM Research, "Granite Libraries and Project Granite Switch"
(Luis Lastras et al.)
https://research.ibm.com/blog/granite-libraries-project-switch

This document explores what Harvey might look like if the architectural ideas
in that work were applied to its routing, tool-call, and output-enforcement
subsystems. Nothing here is a commitment; it is a design space to reason from.

---

## Background

Project Granite Switch introduces a *switching layer* — an additional
transformer mechanism that dynamically activates specialized adapter components
(aLoRAs) based on control tokens in the prompt. The key properties:

- Adapters are independently trained, composable, and replaceable without
  retraining the base model.
- The switching layer preserves context across sequential adapter activations.
- Adapter selection is driven by *capability need*, not by a static endpoint
  name.
- Output can be made deterministic by enforcing strict format contracts
  (IBM calls this the "Mellea" layer).

Harvey already expresses several of these ideas in nascent form. The question
is whether making them explicit and first-class improves reliability, reduces
friction, and opens new capabilities.

---

## Idea 1 — Capability-Based Routing

### Current state

`routing.go` maintains a `RouteRegistry` of named remote endpoints. Dispatch
happens via `@mention` in the user's message: `@openai explain this` sends the
prompt to the `openai` endpoint. The user names the endpoint explicitly.

### What Granite Switch suggests

Instead of (or alongside) naming endpoints, Harvey could route by *capability
declaration*. The router would match a required capability against what each
registered endpoint is known to be good at:

```
Capability          Preferred endpoint
──────────────────  ──────────────────────────────
structured-tools    a model with reliable JSON tool calls
long-context        a model with large context window
code-generation     a model fine-tuned on code
summarization       a model optimized for compression tasks
```

`ModelCache` already stores capability data per installed Ollama model. The
gap is that routing today ignores that cache entirely.

### Possible design

Add a `Capability` field to `RouteEntry` in `route_persist.go`. Add a
`RoutingPolicy` to `Config` with a default of `"explicit"` (current behavior)
and an optional `"capability"` mode. In capability mode, Harvey inspects the
active tool registry, the size of the context, and the task type inferred from
the user's message, then selects the best available endpoint automatically.

The user can still override with `@mention`. Capability routing is the
fallback when no explicit mention is given.

**Open question:** Task-type inference from a user message is itself an LLM
call. A lightweight local classifier (or even a fast keyword heuristic) may
be more appropriate than a full chat round-trip.

---

## Idea 2 — A Third Tool-Call Tier

### Current state

Harvey has two execution paths for tool calls:

| Tier | Trigger | Mechanism | Reliability |
|---|---|---|---|
| Structured | Model returns `tool_calls` in API response | `RunToolLoop` (multi-turn) | High |
| Prose | Small model emits JSON in a fenced block | `tryExecuteProseToolCalls` (single-turn) | Fragile |

The prose path exists because small local models (common Ollama targets) do
not reliably emit structured `tool_calls`. The workaround is best-effort text
parsing, which breaks on minor format deviations.

### What Granite Switch suggests

IBM's aLoRA adapters sit between "full generalist model" and "fragile text
parsing": a small model fine-tuned *specifically* for tool dispatch. It learns
one narrow behavior (emit valid JSON tool calls) rather than general reasoning.
This is cheaper than requiring a large structured-call-capable model and more
reliable than prose parsing.

### Possible design for Harvey

Introduce a **tier 2.5**: a dedicated tool-dispatch adapter model registered
as a capability-endpoint. Harvey would route tool calls through this model
when the primary model is in prose mode:

```
User message → Primary model (reasoning, answer generation)
                    ↓
             Tool dispatch needed?
                    ↓  yes
             Tool-dispatch adapter (JSON emit only)
                    ↓
             ToolRegistry.Dispatch()
                    ↓
             Result injected into history
                    ↓
             Primary model (continue generation)
```

The adapter model never sees the full conversation — it receives only a compact
tool-call instruction schema and the primary model's intent. This mirrors how
Granite Switch keeps the context object alive while activating the right adapter
for each sub-task.

In practice this could be an Ollama model fine-tuned on Harvey's tool schemas,
registered as a named route with `capability: tool-dispatch`. The primary prose
path remains as an ultimate fallback.

---

## Idea 3 — Strict Output Enforcement (Mellea Analogue)

### Current state

When prose-tool-call mode is active, Harvey parses the assistant's raw text
looking for JSON fenced blocks. If the model drifts from the expected format,
parsing silently fails or produces garbage. The user sees an unhelpful "try
/tools off or a larger model" warning.

The partial mitigation already in Harvey: when `unknownNames` is non-empty
after a parse attempt, `tryExecuteProseToolCalls` injects available tool names
into history. This is a correction *after* a partial parse failure — not a
contract that prevents the failure in the first place.

### What Granite Switch suggests

IBM's Mellea layer converts unpredictable text generation into reliable
function signatures by *enforcing a contract before parsing, not after*. The
model is told, via a strict system prompt injection, exactly what its output
must look like. Non-conforming output is caught early and the model is
asked to self-correct.

### Why this matters beyond tool calls

Idea 3 is the structural foundation for IVR (see [plan-ivr-design.md](plan-ivr-design.md)).
IVR is the same pattern applied at a higher level of abstraction:

| Level | Instruct | Validate | Repair |
|-------|----------|----------|--------|
| Format (Idea 3) | System prompt contract | JSON schema check | Correction message, re-prompt |
| Behavioral (IVR) | Step annotation (`[validate: type:value]`) | Deterministic check (command, file, regex) | Repair prompt with feedback |

Implementing Idea 3 first proves the I→V→R loop works at the format level,
reveals implementation patterns that IVR can reuse, and resolves IVR's
core question: "what does validation mean?" Format validation is unambiguous
(is the JSON conformant?); behavioral validation can be modeled the same way
once the plumbing is established.

### Design for Harvey

#### When to activate

Inject the format contract at session start, conditionally:

```go
if a.Config.ToolsEnabled && a.modelToolMode() == ToolModeProse {
    a.systemPrompt += formatContractBlock(a.Tools)
}
```

This fires once per session, not per turn. `formatContractBlock` generates
the contract from the actual registered tool schemas so the model sees real
tool names and argument shapes.

#### The contract block

```
## Tool-call format contract

You are operating in prose tool-call mode. When you need to invoke a tool,
your response MUST contain exactly one fenced JSON block with this structure
and nothing else inside the block:

` + "```json" + `
{"tool": "<name>", "arguments": {"<param>": <value>, ...}}
` + "```" + `

Valid tool names: read_file, write_file, run_command, ...
Any deviation from this structure is an error. Do not wrap the block in
prose. Do not emit multiple JSON blocks. Do not add commentary inside the
block.
```

The tool name list is generated from `a.Tools.Names()` so it is always
accurate and does not go stale.

#### Validation before parsing

In `tryExecuteProseToolCalls`, before attempting JSON parsing, run a
lightweight structural check:

```go
func validateToolCallFormat(response string) (json.RawMessage, error) {
    // 1. Exactly one fenced ```json block present?
    // 2. Block parses as JSON object?
    // 3. Object has "tool" (string) and "arguments" (object) keys?
    // Returns the raw JSON if valid, error otherwise.
}
```

If `validateToolCallFormat` returns an error, inject the correction message
and re-prompt once before falling through to the current warning path:

```go
correctionMsg := "Your last response did not match the required tool-call format. " +
    "Please restate only the tool call as a single ```json block " +
    "with {\"tool\": \"<name>\", \"arguments\": {...}}."
```

#### Tracking

Add a `proseToolCallRepairs int` counter to `ChatStats` so the assay corpus
can measure how often the correction fires and whether it actually helps.
This is the metric that will tell us whether Idea 3 is worth keeping or
whether it needs further tuning.

#### Acceptance criteria

- With a model in prose tool-call mode, a response missing the JSON block
  triggers exactly one correction re-prompt.
- A response that is correct on the second attempt is parsed and dispatched
  normally.
- A response that is wrong twice falls through to the existing "try a larger
  model" warning.
- The contract block is NOT injected when `modelToolMode() == ToolModeStructured`.
- `go test -race` passes.

#### Connection to IVR

Once Idea 3 is working, the I→V→R pattern in Harvey has a proven runtime.
The IVR work in `/plan` can then:
1. Reuse `validateToolCallFormat` as a model for "validate step output against
   a contract" — just with a different contract and validator.
2. Reuse the correction-message injection pattern for repair prompts.
3. Inherit the `ChatStats` counter approach for plan-level repair metrics.

---

## Idea 4 — Context Continuity Across Tool Loops

### Current state

`RunToolLoop` appends tool call/result pairs to `a.History`, so the model
retains context across turns within a single tool loop. However, when
Harvey exits a tool loop and returns to the prose REPL path, the rolling
summary treats tool-call/result message pairs the same as conversational
turns — compressing them into prose that loses the structured information
(tool name, arguments, which file was read, what the result was).

Concrete symptom: after a long session where Harvey read several files, the
model may not know which files it already read because the `read_file` tool
calls were compressed to something like "Harvey reviewed some files earlier."

### What Granite Switch suggests

IBM's aLoRA architecture preserves the context object across adapter
activations. The Harvey analogue is the rolling summary — the key insight
is that the rolling summary should be *tool-loop-aware*, compressing tool
turns differently from prose turns.

### Design for Harvey — `ToolTurnSummary`

**This is the most immediately implementable idea in this document.**

Define a structured type for compressed tool-call turns:

```go
// ToolTurnSummary is the compressed representation of a tool call/result
// pair after the rolling summary fires. It replaces the raw message pair
// in compressed history and is included in the summary injected at context
// window start.
type ToolTurnSummary struct {
    Turn   int            // turn number in the session
    Tool   string         // tool name
    Args   map[string]any // tool arguments
    Result string         // result content, truncated to ResultMaxTokens
}

// ResultMaxTokens is the maximum token estimate for a ToolTurnSummary
// result. Keeps compressed tool history from consuming too much budget.
const ToolTurnSummaryMaxTokens = 100
```

#### In `memory_rolling.go`

When the rolling summary fires and processes a turn that contains tool
call/result messages:

1. Extract each tool call/result pair.
2. For each pair, build a `ToolTurnSummary`: tool name and args from the
   tool call message; truncated result from the tool result message.
3. Format the summary as a compact structured line in the compressed
   history block:

```
[turn 7] read_file: path="harvey.go" → 1240 lines, package harvey
[turn 7] write_file: path="context_estimator.go" → ok
[turn 8] run_command: cmd="go test ./..." → PASS 47 tests (exit 0)
```

4. For conversational (non-tool) turns, use the existing prose compression.

#### Injection into context

The existing rolling summary injects a `[Session summary: ...]` block at
the beginning of the retained history window. Extend this block with a
`Tool calls (compressed):` section listing all `ToolTurnSummary` entries
from compressed turns.

#### Why this is more immediate than Ideas 1 and 2

- It is a contained change to `memory_rolling.go` with no new dependencies.
- It has a clear, testable outcome: after rolling compression, the model
  should be able to answer "which files did I read earlier?" from the
  structured summary rather than guessing from prose.
- It improves long-session reliability for all users, regardless of model
  size or backend.
- It requires no new infrastructure — `ToolTurnSummary` is a pure data type
  extracted from existing `Message` fields.

#### Tests

- `TestRollingSummary_PreservesToolCalls` — after compression, the injected
  summary contains structured tool turn lines for any tool-call turns that
  were compressed out of raw history.
- `TestToolTurnSummary_Truncation` — result content longer than
  `ToolTurnSummaryMaxTokens` is truncated and marked `[truncated]`.
- `TestRollingSummary_MixedTurns` — a session with both conversational and
  tool-call turns compresses each correctly; conversational turns get prose
  compression, tool turns get structured summary.

---

## Sequencing

### Near-term (current release cycle)

1. **Tool-loop-aware rolling summary (Idea 4)** — contained change to
   `memory_rolling.go`, no new dependencies, immediate long-session reliability
   benefit. Design and acceptance criteria are complete above.

2. **Strict output enforcement (Idea 3)** — format contract injection + one
   re-prompt on failure. This is the precursor to full IVR: once the I→V→R
   loop works at the format level, IVR can layer behavioral validation on top.
   The design above is complete enough to plan implementation.

### Medium-term (after assay corpus measures baseline tool-call reliability)

3. **Capability-based routing (Idea 1)** — requires adding capability metadata
   to `RouteEntry` and a routing policy to `Config`. The keyword-heuristic
   approach (route by message content patterns) is sufficient for Harvey;
   avoid LLM-based task classification.

4. **Tool-dispatch adapter tier (Idea 2)** — requires a fine-tuned <3B model
   for Pi feasibility. Depends on assay showing that the prose-tool-call path
   has measurable failure rate after Idea 3 is applied.

### Relationship to other work

- **IVR ([plan-ivr-design.md](plan-ivr-design.md))**: Idea 3 is the precursor.
  Do not re-open IVR design until Idea 3's I→V→R loop is implemented and
  validated. The patterns from Idea 3 directly answer IVR's unresolved questions.
- **Chunked analysis / budget management**: See [small-model-budget-design.md](small-model-budget-design.md)
  for how Ideas 3 and 4 fit into a broader resource-aware prompt management system.
