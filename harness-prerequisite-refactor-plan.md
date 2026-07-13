# Harvey — Pre-Harness-Engineering Refactor — Implementation Plan

See [harness-prerequisite-refactor-design.md](harness-prerequisite-refactor-design.md)
for the full design rationale and scoping decisions. See
[DECISIONS.md](DECISIONS.md) for the finalized decision entries as each phase
lands.

Phases below map 1:1 to the design doc's four items, in the recommended
order (Item 2 → Item 1 → Item 3; Item 4 deferred to
[small-model-budget-design.md](small-model-budget-design.md), which owns its
own plan). `commands.go` restructuring is explicitly out of scope for this
plan — see the design doc's scoping decision.

---

## Phase A — Unify tool-call dispatch-and-report (Item 2) ✅ Complete 2026-07-12

**Goal.** Replace `tryExecuteApertusToolCalls` and `tryExecuteProseToolCalls`
— near-duplicate execute-and-report loops differing only in their parser —
with one shared function, and resolve the discovered "Unknown tool(s)"
warning asymmetry between the two paths.

### Files modified

| File | Change |
|---|---|
| `tool_executor.go` | Added `executeAndReportToolCalls(a, calls, out) (dispatched, unknownNames)` and `availableToolNames(a) []string`; both `tryExecuteApertusToolCalls`/`tryExecuteProseToolCalls` reduced to parse-then-delegate |
| `terminal.go` | History-injection correction message (`~line 1367`) now calls `availableToolNames(a)` instead of its own inline loop |
| `tool_executor_test.go` | Added `TestExecuteAndReportToolCalls_dispatchesKnownTool`, `TestExecuteAndReportToolCalls_unknownToolWarnsWithAvailableList`, `TestTryExecuteProseToolCalls_toolsDisabled`, `TestTryExecuteProseToolCalls_unknownToolWarns`, `TestTryExecuteApertusToolCalls_dispatchesKnownTool`, `TestTryExecuteApertusToolCalls_unknownToolWarns` |

### Sequencing actually followed

1. Read both functions and their two call sites in `terminal.go` to confirm
   the exact contract (`(dispatched bool, unknownNames []string)`) and find
   the behavioral asymmetry (Apertus path silently omitted the terminal
   warning the prose path printed).
2. Confirmed neither function had direct test coverage — only their parsers
   did (`codeblock_test.go`).
3. Asked the user whether to fix the asymmetry (print the warning for both)
   or preserve it behind a flag — decided: fix it, both print.
4. Wrote characterization/new-behavior tests against the *decided* target
   behavior, confirmed red (`executeAndReportToolCalls` undefined; compile
   failure).
5. Implemented the shared function and thin wrappers; confirmed green.
6. Ran full package test suite; one unrelated pre-existing failure noted
   (`TestCmdModelList_ShowsLlamafileEntries`, tied to already-uncommitted
   llamafile-discovery WIP predating this session) and explicitly not
   touched.
7. Logged the decision in `DECISIONS.md` (2026-07-12 entry).

### Acceptance criteria

- [x] `go test ./...` green except the pre-existing, unrelated failure.
- [x] Both Apertus and prose unknown-tool-call paths print the same
      "Unknown tool(s): ... Available tools: ..." warning.
- [x] `availableToolNames` has exactly one implementation, used by both the
      terminal warning and the history-injection correction message.
- [x] Decision logged in `DECISIONS.md`.

---

## Phase B — Unify token-budget/context-percentage reporting (Item 1) ✅ Complete 2026-07-12

See `DECISIONS.md` (2026-07-12 entry) for the three discrepancies found and
resolved, and the final test list. The plan below is kept as-written for the
historical record.

**Goal.** Replace three independent copies of "estimate tokens used, compute
%, pick a warning tier" with one accessor and one formatter.

### Files to modify

| File | Change |
|---|---|
| `context_estimator.go` | Add `Agent.contextUsage() (used, limit int, exact bool)` — wraps the existing Ollama-exact-count vs. estimated-count branching currently duplicated in `terminal.go` |
| `context_estimator.go` | Add a formatter, e.g. `formatContextUsage(used, limit int, exact bool) (line string, tier WarningTier)`, producing the `%`/qualifier text and a tier (`ok`/`warn80`/`full100`) so callers decide independently whether/how to print |
| `terminal.go` | Replace the two inline blocks (`~1112-1129` Ollama, `~1130-1143` llamafile/llama.cpp) with a single call to `contextUsage()` + `formatContextUsage()`, gated on tier as today (only print at ≥80%) |
| `commands.go` | `cmdStatus` (`~640-664`) calls the same two functions instead of its own third copy; `/status` always prints the line regardless of tier |
| `context_estimator_test.go` | New table-driven tests for `contextUsage()` and `formatContextUsage()` directly — this logic currently has no direct unit tests, only indirect coverage through whatever exercises `runChatTurn`/`cmdStatus` |

### Approach

1. Write characterization tests first: capture today's exact output strings
   for representative cases (Ollama exact count at 50%/80%/100%+, llamafile
   estimated count at the same tiers) by calling the *existing* inline logic
   indirectly (via `runChatTurn`/`cmdStatus` if that's the only current
   entry point) — confirms a known-good baseline before refactoring.
2. Extract `contextUsage()`/`formatContextUsage()`, write direct unit tests
   against them, confirm they reproduce the same strings as step 1's
   baseline.
3. Swap all three call sites to the shared functions.
4. Re-run the characterization tests from step 1 — output must be
   byte-identical (pure Extract Method refactor, no behavior change).

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] Ollama and llamafile/llama.cpp turn-warning output unchanged
      byte-for-byte for the same inputs.
- [ ] `/status` output unchanged byte-for-byte.
- [ ] `contextUsage()`/`formatContextUsage()` have direct unit tests
      covering both providers and all three warning tiers.

---

## Phase C — Unify sensor/status reporting (Item 3) ✅ Complete 2026-07-12

See `DECISIONS.md` (2026-07-12 entry, third of that date) for the final
shape (`SensorEvent`/`SensorClass` in `sensor_event.go`, `ReportSensor` on
`StatusReporter`/`*Spinner`) and an honest note on one byte-level (not
visual) formatting discrepancy between the two terminal.go call sites that
couldn't both be preserved exactly by a single shared formatter. This
completes all three items of this refactor cycle. The plan below is kept
as-written for the historical record.

**Goal.** Introduce `SensorEvent` plumbing (design sketched in
[harness-engineering-exploration.md](harness-engineering-exploration.md)
Direction C) as pure routing — reroute the three existing signals through
one shape and one destination, with no UI redesign yet.

### Files to modify

| File | Change |
|---|---|
| `spinner.go` | Extend `StatusReporter` interface (or add a sibling method) to accept a `SensorEvent{Kind, Message, Class}` rather than only a bare string; existing `UpdateStatus(string)` can remain as a convenience wrapper that constructs a default-`Class` event, so `tool_executor.go`'s existing calls don't all need to change in this phase |
| `grounding.go` / `terminal.go` (`~1265`) | `groundingCheck`'s result routes through the same reporting call instead of a direct `fmt.Fprintln` |
| `terminal.go` (`~1298`) | The "only tool-call syntax" prose warning routes through the same call |
| `tool_executor_test.go`, `grounding_test.go` | Tests asserting on exact printed text for either warning will need updating to match the new routing — expected to be equivalent output, not a behavior change, but needs explicit before/after comparison since this is user-visible text |

### Approach

1. Define `SensorEvent` and `SensorClass` (`Computational`/`Inferential`)
   as a new, minimal type — no behavior yet, just the shape.
2. Add the routing method to `Spinner` (or wherever `StatusReporter` is
   satisfied), defaulting its rendering to today's exact behavior (no visible
   change) so this phase is additive plumbing, not a UI change.
3. Reroute `groundingCheck` and the prose "only tool-call syntax" warning
   through it one at a time, confirming output is unchanged after each.
4. This phase deliberately stops here — the two-view sensor-sidecar pattern
   (human dashboard vs. agent-only-failures query) from the exploration
   doc's Direction C is follow-on design work, not part of this plan.

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] `groundingCheck` and prose-tool-call warning output unchanged
      byte-for-byte.
- [ ] All three signals (tool status, grounding, prose-tool-call) construct
      a `SensorEvent`, even if rendering hasn't changed yet.

---

## Deferred — Item 4 (feedforward budget-gating)

Not planned here. Owned entirely by
[small-model-budget-design.md](small-model-budget-design.md) — its own
"Immediate Actions" section is the operative plan for this item.
