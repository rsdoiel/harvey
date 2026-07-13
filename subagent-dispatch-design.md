# Harvey — One Subagent-Dispatch Primitive (Direction D) — Design

**Date**: 2026-07-13
**Status**: ✅ All four phases implemented 2026-07-13 — see `DECISIONS.md` and
[subagent-dispatch-plan.md](subagent-dispatch-plan.md)
**Related**: [harness-engineering-exploration.md](harness-engineering-exploration.md)
Direction D, [chunked-analysis-design.md](chunked-analysis-design.md),
`DECISIONS.md` (2026-07-05 `/read-chunks` entry, 2026-07-12 prerequisite-
refactor entries this follows the same audit discipline as)

---

## What's already there (found during scoping, not assumed)

Three call sites build a bounded context, run it against a model, and
(mostly) fold the result back as a summary rather than a full transcript.
Read all three in full before proposing anything.

| | `/plan next` (`plan_cmd.go:114`) | `/read-chunks` / `injectOrChunk`'s auto-chunk path (`commands.go:1957`, `file_inject.go:117`, `chunk_analyzer.go:78`) | `@mention` registry dispatch (`terminal.go:796`, `routing.go:174`) |
|---|---|---|---|
| **Context built** | Fresh, empty history: system prompt + plan state + step instruction. No prior conversation at all. | Per chunk: `chunkSystemPrompt` (hardcoded, not `a.Config.SystemPrompt`) + chunk content + instruction. Repeated N times (map), then once more over the partial results (reduce). | `recentHistory(history, 10)` (last 10 non-system messages) + prompt. |
| **Model resolution** | `step.Model` annotation → `attemptModelSwitch` — a **real, persistent swap** of `a.Client`/`a.Backend` (kills/relaunches a llamafile server for local models). | `@mention` parsed out of the instruction string, but **only used to label `ChunkAnalysisParams.Model`** (recording only, per that field's own doc comment) — the actual call always uses `a.Client`, whatever's currently active. | `a.Routes.Lookup(name)` → `clientForEndpoint` builds an **independent `LLMClient`**, never touching `a.Client` — or, if not a registered route, falls through to `attemptModelSwitch` (same persistent swap as `/plan next`). |
| **Tool support** | Yes — `ToolExecutor.RunToolLoop` when tools enabled, else plain `client.Chat`. | None at all — always plain `client.Chat`, both map and reduce. | Yes, when `ep.Tools` — same `RunToolLoop`/`client.Chat` branch as `/plan next`, duplicated. |
| **Fold-back into `a.History`** | **None.** The step's reply is only printed to `out`; nothing is added to history. | `a.AddMessage("user", "[/read-chunks ...]"); a.AddMessage("assistant", synthesis)`. | `a.AddMessage("user", input); a.AddMessage("assistant", reply)`. |
| **Restore after dispatch** | Attempts to restore the pre-step model — **see Bug 2 below, this is broken.** | N/A (never actually switches). | N/A for route dispatch (independent client, nothing to restore); N/A for local-switch fallthrough (intentionally persistent, not restored — see "Not actually part of this pattern" below). |

### Two real bugs found while reading the code (not hypothetical)

**Bug 1 — `@model` in a chunk-analysis instruction is silently non-functional.**
`chunked-analysis-design.md` and `DECISIONS.md` (2026-07-05) both state the
design intent explicitly: *"If the user includes `@model` in the chunk
prompt, Harvey's existing `@mention` routing infrastructure routes each
chunk analysis call to the named model."* The shipped code does not do
this. `cmdReadChunks` (`commands.go:2063`) and `injectOrChunk`
(`file_inject.go:229`) both parse the mention and reassign a local `model`
string, but that string only flows into `ChunkAnalysisParams.Model` —
consumed solely for `Recorder`/`DebugLog` labeling. `RunChunkedAnalysis`
(`chunk_analyzer.go:78`) always executes against whatever `client` its
caller passed in, which both callers hard-code as `a.Client`. Writing
`@granite summarize this` in a `/read-chunks` instruction never dispatches
to granite — it only mislabels the session recording as if it had. No test
exercises the actual dispatched-model behavior (only `ParseAtMention`'s own
parsing is tested), which is presumably how this went unnoticed.

**Bug 2 — `/plan next`'s post-step model restore reads a field the switch
already clobbered.** `cmdPlanNext` (`plan_cmd.go:138,189-192`):

```go
defaultModel := activeModelLabel(a) // label to restore after the step
...
if step.Model != "" && activeModelLabel(a) != defaultModel {
    if _, err := attemptModelSwitch(a, a.Config.Llamafile.Active, out); err != nil {
```

`defaultModel` is captured but never used for restoration — only for the
`!=` comparison. The restore instead reads `a.Config.Llamafile.Active`
live, at restore time. But `switchLlamafileModel` (`llamafile.go:220`,
called from `attemptModelSwitch`'s llamafile-registry branch) **sets**
`a.Config.Llamafile.Active = name` as part of *performing* the step's
switch. So by the time restore runs, that field has already been
overwritten to the step's own model name — restore effectively tries to
"switch to" the model that's already active, not the one that was active
before the step. Confirmed by tracing the exact mutation, not inferred.
Two further problems layered on top: `activeModelLabel`'s return value
(`"name (backend)"`) isn't a valid `attemptModelSwitch` lookup key even if
it were passed through; and the restore assumes the pre-step model was
itself a llamafile registry entry, silently wrong whenever the user was on
an Ollama model or a non-llamafile alias when the step started.

**Confirmed with the user (2026-07-13): both are in scope to fix as part of
Direction D**, rather than filed as separate, unrelated bugfixes — both
live in exactly the model-dispatch-resolution code this direction is
already consolidating, and a single correct resolver used consistently by
all three call sites fixes both without writing either fix twice.

### Not actually part of this pattern

`@mention`'s **local-model-switch** fallthrough (`terminal.go:853-869`, when
the name doesn't match a registered route but does match a llamafile
entry/alias) is a *different* mechanism from the other three: it mutates
`a.Client` **persistently** (no restore — that's the point, the user is
deliberately changing their active model for the rest of the session) and,
when there's a trailing prompt, falls through to the **normal chat path
with full history**, not a bounded context. It only shares surface syntax
(`@name`) with the dispatch pattern, not its shape. **Out of scope** — left
exactly as it is.

---

## Scope for this increment

Per the audit above, forcing all three into one universal
"build-context-and-dispatch-and-fold-back" function would be wrong: each
caller's context construction is genuinely different for good reason (a
plan step needs zero prior history; a chunk needs exactly its own content;
a route dispatch needs recent conversational continuity). Unifying that
part would either lose a real distinction or produce a function with three
near-mutually-exclusive modes — not a simplification.

What *is* genuinely duplicated (or, in the chunk-analysis case, silently
broken) is narrower, and that's what this increment targets:

1. **`resolveDispatchTarget`** — one function that, given a name, returns a
   client to dispatch to and a `restore` function, correctly branching on
   whether the target is a registered route (independent client, no
   `a.Client` mutation, trivial restore) or a local llamafile/alias (real
   persistent swap, restore must reconstruct the actual pre-switch state,
   not round-trip through a display label). Fixes Bug 2 by construction —
   there is no longer a code path that reads a post-switch field expecting
   a pre-switch value. Used by `/plan next` (replacing its broken restore),
   and by `/read-chunks`/`injectOrChunk` (fixing Bug 1 — the resolved
   client is what actually gets passed to `RunChunkedAnalysis`, not always
   `a.Client`).
2. **A shared tools-vs-plain-chat execution helper** — `/plan next` and
   `@mention` registry dispatch duplicate the same
   `if toolsEnabled { RunToolLoop } else { Chat }` branch. Chunk analysis
   is **deliberately** tool-free (each chunk is read-only text synthesis;
   giving it side-effecting tools would be a scope expansion, not a
   duplication fix) — not part of this helper.
3. **A shared fold-back-as-summary helper** — `/read-chunks` and `@mention`
   registry dispatch both do the same two-call
   `a.AddMessage("user", ...); a.AddMessage("assistant", ...)` pattern.
   `/plan next` deliberately does **not** fold back into history (a plan
   step is a side-effecting action, not a chat turn) — not touched.

Confirmed with the user (2026-07-13): all three sub-primitives, not just
the model-resolution one.

---

## Proposed shapes

### 1. `resolveDispatchTarget`

```go
// DispatchTarget is a resolved model to run one bounded turn against, plus
// however this Agent needs to clean up afterward.
type DispatchTarget struct {
    Client  LLMClient
    Restore func() // no-op for independent (route) clients; real swap-back for local
}

// resolveDispatchTarget resolves name (an @mention-style identifier) to a
// dispatch target. Checks the route registry first (independent client, no
// Agent mutation), then the llamafile registry / model aliases (a real,
// persistent swap of a.Client/a.Backend — costs minutes on Pi-class
// hardware per the Hardware Reality Check in harness-engineering-
// exploration.md). Returns ok=false when name matches neither.
func resolveDispatchTarget(a *Agent, name string, out io.Writer) (target DispatchTarget, ok bool, err error)
```

For the local-swap branch, `Restore` cannot be a cheap reference-swap back
to the previous `a.Backend`/`a.Client` — traced `switchLlamafileModel`
(`llamafile.go:207-210`): it **stops** the previous backend process before
starting the new one (`if a.Backend != nil && a.Backend.StartedByHarvey()
{ ...; a.Backend.Stop() }`). By the time a step finishes, the pre-step
process is already dead; reassigning the old `a.Backend` reference would
hand back a handle to a killed process. Restore must instead **capture the
pre-step identifying name(s) before switching**, then genuinely relaunch
through the same mechanism — paying a second real cold-start cost, not a
cheap swap:

```go
prevLlamafileActive := a.Config.Llamafile.Active // captured BEFORE the step's switch
prevOllamaModel := a.Config.Ollama.Model
```

`Restore` then: if `prevLlamafileActive != ""`, call
`attemptModelSwitch(a, prevLlamafileActive, out)` to relaunch that exact
entry; otherwise (the pre-step model was plain Ollama, not a llamafile
registry entry) stop any backend the step started and rebuild the Ollama
client from `prevOllamaModel` directly. This is strictly more expensive
than today's (broken) restore attempt, but it's the honest cost of a
correct one — consistent with the Hardware Reality Check's framing that a
model swap costs minutes each way, not something this design can make
cheaper, only correct.

### 2. Shared tool-vs-plain execution helper

```go
// runBoundedTurn runs messages against client — through registry's
// ToolExecutor when useTools is true and registry is non-nil, otherwise a
// plain client.Chat — streaming to w. Returns the (possibly tool-extended)
// history when tools ran, or nil when the plain-chat path was taken (no
// history to inspect for the plan-step "did a tool error?" check).
func runBoundedTurn(ctx context.Context, client LLMClient, registry *ToolRegistry, cfg *Config, useTools bool, messages []Message, dbg *DebugLog, w io.Writer) (updatedHistory []Message, stats ChatStats, err error)
```

`cmdPlanNext` keeps its own `planStepHadErrors(updatedHistory)` call using
the returned history; `DispatchToEndpoint` just discards it. Both stop
duplicating the `NewToolExecutor(...)` / `RunToolLoop` vs. `Chat` branch
itself.

### 3. Shared fold-back helper

```go
// foldBackTurn appends a user/assistant turn to a.History, the shared
// pattern behind /read-chunks and @mention registry dispatch summarizing a
// bounded sub-dispatch back into the main conversation.
func foldBackTurn(a *Agent, userContent, assistantContent string)
```

Trivial — two `a.AddMessage` calls — but removes the duplication and gives
future callers (e.g. a future `/plan next` mode that *does* want fold-back)
one place to change if the pattern ever needs to grow (timestamps, a
distinguishing marker, etc.).

---

## Decisions confirmed (2026-07-13)

1. **Both bugs are in scope**, fixed as a natural consequence of the
   consolidation rather than filed separately.
2. **All three sub-primitives** (`resolveDispatchTarget`, the tool-vs-plain
   helper, the fold-back helper) are in scope for this increment — not just
   the model-resolution one.
3. **No universal single "dispatch primitive" function** — the audit shows
   that would force together three genuinely different context-construction
   strategies. The three sub-primitives above are the actual, honest scope
   of "consolidation" here.

## Open questions for the plan

- Should `/plan next` adopt `resolveDispatchTarget` even for the case where
  `step.Model` names something already active (no-op switch avoided today
  via the `activeModelLabel(a) != defaultModel` check) — worth confirming
  the no-op-avoidance behavior is preserved, not just the restore-correctness.
- Test strategy for Bug 1's fix: the existing `/read-chunks`/`injectOrChunk`
  tests never assert on which model actually ran a chunk (only `a.Client`'s
  mock replies are checked) — the plan needs a test double capable of
  distinguishing "route A's client was called" from "route B's client was
  called" to prove the fix, not just that parsing succeeded.
- The Ollama-daemon-swap vs. llamafile-relaunch benchmark
  (`harness-engineering-exploration.md`'s Hardware Reality Check, "not yet
  run") remains open and is not resolved by this design — `Restore`'s cost
  is real either way; this document doesn't change that cost, only fixes
  correctness of the restore itself.
