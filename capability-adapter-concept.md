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

### What Granite Switch suggests

IBM's Mellea layer converts unpredictable text generation into reliable
function signatures by *enforcing a contract before parsing, not after*. The
model is told, via a strict system prompt injection, exactly what its output
must look like. Non-conforming output is caught early and the model is
asked to self-correct.

### Possible design for Harvey

When Harvey enables prose-tool-call mode, inject a **format contract block**
into the system prompt:

```
## Output contract (tool mode)
When invoking a tool, your response MUST contain exactly one JSON block in
the following form and nothing else in that block:

```json
{"tool": "<name>", "arguments": {<key>: <value>, ...}}
```

Any deviation from this schema is an error. Do not add commentary inside
the block. Do not use any other JSON structure.
```

After receiving the response, before any parsing, validate the block against
the schema. If validation fails, inject a correction message:

```
Your last response did not match the required tool-call format.
Please restate only the tool call as a JSON block matching the schema above.
```

Re-prompt once. If the second attempt also fails, fall through to the current
warning path. This adds one extra turn in the failure case but avoids silent
parse failures in the common case.

**Implementation note:** The contract injection should be conditional — only
active when `a.toolsEnabled && !modelSupportsStructuredCalls`. It must not
fire during structured-tool-call sessions where the model is already reliable.

---

## Idea 4 — Context Continuity Across Tool Loops

### Current state

`RunToolLoop` appends tool call/result pairs to `a.History`, so the model
does retain context across turns within a single tool loop. However, when
Harvey exits a tool loop and returns to the prose REPL path, some of that
intermediate context gets compressed or lost under rolling summary pressure.

### What Granite Switch suggests

IBM's aLoRA architecture specifically preserves the context object across
sequential adapter activations using activated LoRAs (not standard LoRAs),
avoiding full KV-cache recalculation between steps. The lesson for Harvey is
not to replicate aLoRA mechanics (that's a model-level concern) but to
recognize that *Harvey's rolling summary is the analogous mechanism* — and to
make sure it is tool-loop-aware.

### Possible design for Harvey

Tag tool-call turns in the rolling summary so they are summarized differently
from conversational turns. Tool call/result pairs carry structured information
(tool name, arguments, result content) that compresses better with a
structured summary than with free-text prose compression.

```
Instead of: "Harvey called a tool and got a result."
Store:       {tool: "read_file", args: {path: "foo.go"}, result_summary: "640 lines, package harvey"}
```

This keeps tool context recoverable even after rolling compression cuts the
raw history, which mirrors the Granite Switch goal of context continuity
across multi-step pipelines.

---

## Sequencing

None of these ideas depend on each other, but they have a natural order:

1. **Strict output enforcement (Idea 3)** — lowest risk, immediate reliability
   gain in prose-tool-call mode. Pure prompt engineering, no new code paths.
2. **Tool-loop-aware rolling summary (Idea 4)** — improves long-session
   context fidelity. Contained change to `memory_rolling.go`.
3. **Capability-based routing (Idea 1)** — medium complexity. Requires adding
   capability metadata to `RouteEntry` and a routing policy to `Config`.
4. **Tool-dispatch adapter tier (Idea 2)** — highest complexity. Requires a
   fine-tuned Ollama model and a new dispatch path in `terminal.go`.

Ideas 3 and 4 are viable near-term work. Ideas 1 and 2 depend on having a
suitable adapter model available and are better treated as medium-term
experiments once Harvey's evaluation corpus (assay) has prompts that exercise
tool-call reliability.
