# Harvey — Pre-Harness-Engineering Refactor — Design

**Date**: 2026-07-12
**Status**: Design — decisions logging in `DECISIONS.md` as each item lands;
see [harness-prerequisite-refactor-plan.md](harness-prerequisite-refactor-plan.md)
for the phased implementation plan.
**Related**: [harness-engineering-exploration.md](harness-engineering-exploration.md),
[small-model-budget-design.md](small-model-budget-design.md)

---

## Motivation

Before starting the guide/sensor tooling work in
`harness-engineering-exploration.md`, stabilize the specific code paths that
work will build on. This is deliberately scoped narrow: four items already
surfaced with concrete file:line references while auditing Harvey against
Böckeler's framework, each one a direct dependency of a specific harness-
engineering direction. This is not a general debt-reduction sweep.

**Explicitly out of scope for this cycle:** `commands.go` (3877 lines, 71
functions). It's real debt but doesn't block any harness-engineering
direction — decided out during scoping (2026-07-12) so this cycle stays
small and low-risk. Track separately if/when it becomes a blocker.

Each item below follows the same shape: current state, the problem, a
proposed direction, and what it would take to verify the refactor didn't
change behavior. None of these are decided yet — that's the next step, to
be logged in `DECISIONS.md` once agreed.

---

## Item 1 — Unify token-budget/context-percentage reporting ✅ Implemented 2026-07-12

See `DECISIONS.md` (2026-07-12 entry) and `harness-prerequisite-refactor-plan.md`
Phase B for what was actually found and decided. The proposal below is kept
as-written for the historical record.

**Current state.** The same "estimate tokens used, compute %, pick a
warning tier" logic is written three times:

- `terminal.go:1112-1129` — Ollama path (exact count via `CountTokens`)
- `terminal.go:1130-1143` — llamafile/llama.cpp path (estimated via
  `estimateTokens`)
- `commands.go:640-664` — `/status`, a near-identical third copy of both,
  branching on provider again

**Problem.** Three call sites means three places to update if the warning
thresholds (`80%`, `100%`) or the qualifier logic (`~` for estimated vs.
exact) ever change, and it's already happened once without all three being
touched consistently — worth confirming they still agree before refactoring.

**Proposed direction.** One method, `Agent.contextUsage() (used int, limit
int, exact bool)`, used by all three call sites; a second small function
formats the `%`/qualifier/warning-tier text from that tuple. `runChatTurn`
and `cmdStatus` both call the formatter; the only difference between them is
whether the result is gated behind a threshold (turn warnings only print at
≥80%) or always shown (`/status`).

**Verification.** Existing behavior for both providers must be
byte-identical for the same inputs — this is a pure Extract Method refactor.
Add table-driven tests for `contextUsage()`/the formatter directly (currently
this logic has no direct unit tests; it's only exercised indirectly through
whatever integration tests hit `runChatTurn`/`cmdStatus`).

---

## Item 2 — Unify tool-call dispatch-and-report ✅ Implemented 2026-07-12

See `DECISIONS.md` (2026-07-12 entry) for the final decision and test list.
The proposal below is kept as-written for the historical record; the
"unknown tool" asymmetry it flags was resolved by making both paths print
the warning, per that decision entry.

**Current state.** `tryExecuteApertusToolCalls` (`tool_executor.go:252`) and
`tryExecuteProseToolCalls` (`tool_executor.go:291`) are near-byte-identical:
same `NewToolExecutor` setup, same `ExecuteToolCalls` call, same per-result
loop printing `dim("  [name]")` + content and collecting `unknownNames`. They
differ only in which parser produces `calls` (`ParseApertusToolCalls` vs.
`ParseProseToolCalls`).

**Problem.** Any change to how tool results are reported/dispatched (e.g.
Direction C's `SensorEvent` routing, or Direction A's tool tier needing a
different report format) has to be made twice, and already risks drifting —
`tryExecuteProseToolCalls` has the extra "Unknown tool(s)... Available
tools:" block (`tool_executor.go:316-324`) that `tryExecuteApertusToolCalls`
does not.

**Proposed direction.** One function, `executeAndReportToolCalls(a *Agent,
calls []anyllm.ToolCall, out io.Writer) (dispatched bool, unknownNames
[]string)`, parameterized by nothing extra — both callers already produce
`[]anyllm.ToolCall` from their respective parsers before calling in. Each of
today's two functions becomes: `calls := ParseXToolCalls(...); if
len(calls)==0 {return false,nil}; return executeAndReportToolCalls(a, calls,
out)`. The "unknown tool" block currently only in the prose path should
almost certainly apply to both — worth confirming that's an oversight, not
an intentional difference, before merging.

**Verification.** `tool_executor_test.go` likely already covers both paths
separately; after the merge, both existing test sets should pass unmodified
against the shared function (same behavior, less code). If the "unknown
tool" block turns out to be deliberately prose-only, that becomes an
explicit parameter instead of a silent behavior change.

---

## Item 3 — Unify sensor/status reporting into one channel ✅ Implemented 2026-07-12

See `DECISIONS.md` (2026-07-12 entry) and `harness-prerequisite-refactor-plan.md`
Phase C. This was the last item of this refactor cycle — Item 4 remains
owned by `small-model-budget-design.md`. The proposal below is kept
as-written for the historical record.

**Current state.** Three disconnected reporting mechanisms for what are all,
conceptually, sensor signals:

- `tool_executor.go:118-133` — `e.Status.UpdateStatus(...)`, transient,
  erased by the spinner's next Lear-quote tick (`spinner.go:155-159`)
- `terminal.go:1265` — `groundingCheck` result, printed as a bare warning
  line after the spinner has already stopped
- `terminal.go:1298` — prose-tool-call "only tool-call syntax" warning,
  another independent print

**Problem.** This is the direct prerequisite for
`harness-engineering-exploration.md` Direction C (the sensor-sidecar
pattern) and Direction A (new computational sensors need somewhere to
report). Building either on top of three uncoordinated paths means the new
work has to integrate with three different call shapes instead of one.

**Proposed direction.** Introduce the `SensorEvent{Kind, Message, Class}`
type sketched in Direction C now, as pure plumbing — no new sensors yet,
just rerouting the three existing signals through it. Concretely: `Status
StatusReporter` becomes (or gains) a method taking a `SensorEvent`;
`groundingCheck` and the prose-tool-call warning call the same method
instead of `fmt.Fprintln` directly. The spinner's rendering behavior does
not need to change yet — this item is about giving all three signals one
shape and one destination, not redesigning the UI. Actual UI changes (a
trailing status log, the human/agent two-view split) stay in Direction C's
scope, to be designed after this plumbing exists.

**Verification.** Since this changes user-visible output shape (even if only
internally routed the same way), this needs before/after manual comparison
of the printed output for a grounding-check warning and an unknown-tool
warning, plus whatever tests currently assert on the exact printed text of
either — those assertions may need updating to match the new (hopefully
equivalent) output.

---

## Item 4 — Budget-gate feedforward assembly before it's added to history

**Current state.** Already tracked in detail in
`small-model-budget-design.md` (Directions 1 and 2, `BudgetTracker`); not
re-designed here. Restating only the sequencing point: `runChatTurn`
(`terminal.go:1091-1109`) runs memory injection → `ragAugment` → file
injection → *then* appends to history → *then* computes the token-budget
warning (`terminal.go:1112-1143`). The budget check runs after the content
it should be gating is already committed.

**Proposed direction.** Defer entirely to `small-model-budget-design.md`'s
existing Direction 1 (`Agent.injectFileContext`, budget-aware, skip/note
files that don't fit) and Direction 2 (`BudgetTracker` shared between
`ragAugment` and `UnifiedMemory.Recall`). Listed here only so this refactor
cycle's four items are the complete prerequisite set for
`harness-engineering-exploration.md`'s Directions A-C in one place — the
actual design work for this item already exists and doesn't need repeating.

**Verification.** Whatever `small-model-budget-design.md` specifies when
that design is decided; likely the two "Immediate Actions" it already lists
(lower `maxInjectFileBytes`, add `effective_context`) plus tests for
`BudgetTracker.Reserve`.

---

## Sequencing

Items 1 and 2 are small, mechanical, independently shippable, and touch
disjoint code — either order, or in parallel. Item 3 (sensor-channel
unification) is easiest once Item 2 exists, since both tool-call dispatch
functions currently print directly rather than through any shared reporting
call — merging them first means Item 3 only has to redirect one call site's
output instead of two. Item 4 is already scoped in its own document and can
proceed independently of 1-3 on whatever timeline that design is decided.

Recommended order: **Item 2 → Item 1 → Item 3**, with Item 4 proceeding in
parallel under its own document.

## Next step

See [harness-prerequisite-refactor-plan.md](harness-prerequisite-refactor-plan.md)
for the phased implementation plan (added 2026-07-12 — this design doc
originally skipped a plan artifact given the small size of each item, but
the user's requested cycle is design → decisions → planning, all three, so
the plan doc was added after Item 2 was already implemented). Each phase in
that plan logs its decision in `DECISIONS.md` following the existing
Context/Decision/Rejected/Consequences format as it lands.
