# Harvey — One Subagent-Dispatch Primitive (Direction D) — Implementation Plan

See [subagent-dispatch-design.md](subagent-dispatch-design.md) for the full
audit, the two confirmed bugs, and the 2026-07-13 decisions. See
[DECISIONS.md](DECISIONS.md) for the finalized decision entries as each
phase lands.

Resolved here, as implementation-level judgment calls rather than
user-facing policy decisions:

- **Testability seam for the local-swap `Restore` path.** `attemptModelSwitch`
  spawns a real OS process (`switchLlamafileModel` → `StartLlamafileService`)
  — confirmed by reading `model_switch_test.go`, whose existing tests
  simulate switches by directly setting `a.Config.Llamafile.Active`/`a.Backend`
  rather than invoking the real switch, precisely because it isn't practical
  to exercise in a unit test. `TestCmdPlanNext_modelAnnotationSwitches`
  (`plan_test.go:211`) is the concrete proof this gap already causes real
  problems: it only asserts "doesn't panic," explicitly noting the switch
  is *expected* to fail since no server is running — meaning no existing
  test has ever verified restore behavior, which is exactly how Bug 2 went
  uncaught. Add `Agent.attemptModelSwitchOverride func(name string, out
  io.Writer) (bool, error)` (nil-checked, same pattern as the existing
  `Agent.toolsReliableOverride`, `harvey.go:179`) — `resolveDispatchTarget`'s
  local-swap branch calls it instead of `attemptModelSwitch` directly when
  non-nil, letting tests substitute a fast, no-process-spawn fake while
  still exercising the real capture-before/restore-after logic.
- **Restore's Ollama-fallback path** rebuilds the client exactly the way
  `attemptModelSwitch`'s own Ollama branch already does:
  `newOllamaLLMClient(a.Config.Ollama.URL, prevOllamaModel,
  a.Config.Ollama.Timeout)` — no new construction path invented.
- **Genuine no-op avoidance, added as part of the fix, not preserved from
  before** — re-auditing `cmdPlanNext` found the original
  `defaultModel := activeModelLabel(a)` capture was *only* used to decide
  whether to attempt a restore, never to skip the initial switch when
  `step.Model` already matched the active model. There was no real no-op
  avoidance to preserve. `resolveDispatchTarget`'s local-swap branch adds
  a case-insensitive check against the current active name before
  switching at all — skips the swap (and therefore the later restore)
  entirely when the step's declared model is already active.
- **`runBoundedTurn`'s returned history is `nil` on the plain-chat path** —
  matches the only caller that inspects it (`cmdPlanNext`'s
  `planStepHadErrors`), which already treats "no tool calls happened" as
  "nothing to check."
- **`RunChunkedAnalysis`'s signature is unchanged.** It already takes a
  `client LLMClient` parameter directly (`chunk_analyzer.go:78`) — Bug 1's
  fix is entirely at the two call sites (`cmdReadChunks`, `injectOrChunk`):
  resolve the target first, pass `target.Client` instead of always
  `a.Client`, `defer target.Restore()` around the whole map+reduce call
  (one swap for the whole operation, not per chunk — matches the
  exploration doc's "only worth invoking for coarse-grained tasks"
  framing).

---

## Phase A — `resolveDispatchTarget` (fixes Bug 2) ✅ Complete 2026-07-13

See `DECISIONS.md` (2026-07-13 entry) for the final shape and two findings
made while implementing: a correct restore can't be a cheap reference-swap
(the pre-step process is already stopped by the time restore runs), and
the original `TestCmdPlanNext_modelAnnotationSwitches` never actually
exercised the switch path at all, for an unrelated reason (a plan-file
serialization gap between `Model` and `Title`'s `[model: ...]` marker). The
plan below is kept as-written for the historical record.

**Goal.** One correctly-restoring model-resolution primitive, wired into
`/plan next` first since that's where Bug 2 lives.

### Files to modify

| File | Change |
|---|---|
| `dispatch_target.go` (new) | `DispatchTarget{Client LLMClient, Restore func()}`; `resolveDispatchTarget(a *Agent, name string, out io.Writer) (target DispatchTarget, ok bool, err error)` — checks `a.Routes.Lookup(name)` first (route branch: `clientForEndpoint`, `Restore` is a no-op, `a.Client`/`a.Backend` untouched); else checks whether `name` already matches the active model (no-op case: `ok=true`, `target.Client = a.Client`, `Restore` no-op); else attempts a local switch via `a.attemptModelSwitchOverride` if set, else `attemptModelSwitch` — capturing `prevLlamafileActive := a.Config.Llamafile.Active` and `prevOllamaModel := a.Config.Ollama.Model` **before** switching, so `Restore` can relaunch the correct pre-step name afterward |
| `harvey.go` | Add `attemptModelSwitchOverride func(name string, out io.Writer) (bool, error)` field to `Agent` (nil by default), same pattern as `toolsReliableOverride` |
| `dispatch_target_test.go` (new) | `TestResolveDispatchTarget_RouteEndpoint_NoAgentMutation` (route branch never touches `a.Client`/`a.Backend`); `TestResolveDispatchTarget_AlreadyActive_SkipsSwitch` (name matches current active model → no switch attempted, `Restore` is effectively a no-op); `TestResolveDispatchTarget_LocalSwitch_RestoreReturnsToOriginal` (using `attemptModelSwitchOverride` as the fast fake — switch to B, call `Restore()`, assert the override was called a second time with the *original* pre-switch name, not the post-switch one — this is the direct regression test for Bug 2's exact failure mode); `TestResolveDispatchTarget_UnknownName_ReturnsNotFound` |
| `plan_cmd.go` | `cmdPlanNext`: replace the `attemptModelSwitch`/manual-restore block with `resolveDispatchTarget(a, step.Model, out)`; use `target.Client` for the step's `Chat`/`RunToolLoop` call; `defer`-style call `target.Restore()` after the step completes (success or failure) instead of the current post-hoc `if step.Model != "" && activeModelLabel(a) != defaultModel` check |
| `plan_test.go` | Replace `TestCmdPlanNext_modelAnnotationSwitches` (currently only asserts "doesn't panic") with a version using `a.attemptModelSwitchOverride` to make the switch actually succeed in-test, then assert the step ran against the switched client **and** that after the step, `a.attemptModelSwitchOverride` was invoked again to restore the *original* model — turning a previously-untestable path into a real regression test for Bug 2 |

### Approach

1. Write `dispatch_target_test.go` first (TDD), confirm red, implement
   `dispatch_target.go`, confirm green. The route-endpoint and
   already-active branches need no process-spawning seam at all; only the
   local-switch branch needs `attemptModelSwitchOverride`.
2. Rewrite `TestCmdPlanNext_modelAnnotationSwitches` to actually exercise
   restore via the new override — confirm it fails against the *old*
   `cmdPlanNext` code first (proving it would have caught Bug 2), then wire
   `cmdPlanNext` to use `resolveDispatchTarget` and confirm green.

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] `TestResolveDispatchTarget_LocalSwitch_RestoreReturnsToOriginal`
      fails against the pre-Phase-A `cmdPlanNext` restore logic and passes
      after the fix (proves the fix, not just new code existing).
- [ ] A route-registered endpoint used as a plan step's model never
      mutates `a.Client`/`a.Backend` (falls out of the resolver design, not
      previously possible for `/plan next` at all).

---

## Phase B — Wire `resolveDispatchTarget` into `/read-chunks` and `injectOrChunk` (fixes Bug 1) ✅ Complete 2026-07-13

See `DECISIONS.md` (2026-07-13 entry) for the final shape and one deviation
from this plan: `injectOrChunk` calls `restore()` explicitly right after
each loop iteration's `RunChunkedAnalysis` call rather than using `defer`
(considered, but `defer` inside a loop over potentially multiple mentioned
files would queue every restore until the whole function returns, leaving
an earlier iteration's switched model active during later iterations). The
plan below is kept as-written for the historical record.

**Goal.** `@model` in a chunk-analysis instruction actually dispatches to
that model, matching the documented design intent from
`chunked-analysis-design.md` / `DECISIONS.md` (2026-07-05).

### Files to modify

| File | Change |
|---|---|
| `commands.go` (`cmdReadChunks`) | After `ParseAtMention(instruction)` resolves a `mentionName`: call `resolveDispatchTarget(a, mentionName, out)`; on `!ok`, print the existing "not found" warning and keep using `a.Client`; on success, use `target.Client` for the `RunChunkedAnalysis` call and `defer target.Restore()` around it |
| `file_inject.go` (`injectOrChunk`) | Same change, mirrored — this is the auto-chunk (context-overflow) path, currently duplicating the identical cosmetic-only bug |
| `read_chunks_cmd_test.go` | New test: register a second route endpoint (or use `attemptModelSwitchOverride` for a local name), run `/read-chunks file.md @other summarize`, assert the *other* client actually received the chunk/synthesis calls (not `a.Client`) — this requires a test double that can distinguish "which client was called," since the existing tests only check `a.Client`'s mock replies and would pass even under the old, broken behavior |
| `file_inject_test.go` | Same new-test treatment for `injectOrChunk`'s auto-chunk path |

### Approach

1. Write the two new tests first using a client double that records which
   instance was invoked (e.g. two distinct `mockLLMClient` instances with
   different canned replies, asserting the *returned synthesis* came from
   the mentioned model's reply, not the default client's) — confirm red
   against current code (proving Bug 1 is real, not just theorized).
2. Wire both call sites, confirm green.
3. Manual check (per this repo's `verify` practice): run `/read-chunks` on
   a real file with an `@registered-route` mention against a live Ollama
   instance and confirm the debug log / recording shows the request
   actually went to that endpoint's URL, not the locally active model's.

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] New tests fail against pre-Phase-B code and pass after — proves the
      fix.
- [ ] An unrecognized `@name` in a chunk instruction falls back to the
      current active model with a visible warning, matching existing
      unknown-route/unknown-model warning conventions elsewhere in the
      codebase (not a silent fallback).

---

## Phase C — Shared tool-vs-plain execution helper ✅ Complete 2026-07-13

See `DECISIONS.md` (2026-07-13 entry) for the final shape. No deviations
from this plan. The plan below is kept as-written for the historical record.

**Goal.** Remove the duplicated `if useTools { RunToolLoop } else { Chat }`
branch between `cmdPlanNext` and `DispatchToEndpoint`.

### Files to modify

| File | Change |
|---|---|
| `tool_executor.go` (or a new `bounded_turn.go`) | `runBoundedTurn(ctx context.Context, client LLMClient, registry *ToolRegistry, cfg *Config, useTools bool, messages []Message, dbg *DebugLog, w io.Writer) (updatedHistory []Message, stats ChatStats, err error)` |
| `plan_cmd.go` | `cmdPlanNext` calls `runBoundedTurn` instead of its own inline branch; keeps its own `planStepHadErrors(updatedHistory)` call using the returned history |
| `routing.go` | `DispatchToEndpoint` calls `runBoundedTurn` instead of its own inline branch; discards the returned history (unchanged behavior) |
| new/existing test files | `TestRunBoundedTurn_UsesToolLoopWhenEnabled`, `TestRunBoundedTurn_PlainChatWhenToolsDisabled`, `TestRunBoundedTurn_PlainChatReturnsNilHistory` |

### Approach

1. TDD-first for `runBoundedTurn` in isolation.
2. Swap both call sites one at a time, re-running each file's existing
   test suite after each swap to confirm no behavior change (both callers
   already have test coverage for their tool/no-tool branches — those
   tests should pass unmodified, proving the extraction is behavior-preserving).

### Acceptance criteria

- [ ] `go test ./...` green, including every pre-existing `/plan next` and
      `@mention` route-dispatch test, unmodified.
- [ ] `chunk_analyzer.go` is untouched — chunk analysis stays deliberately
      tool-free, per the design doc's scope decision.

---

## Phase D — Shared fold-back helper ✅ Complete 2026-07-13

See `DECISIONS.md` (2026-07-13 entry) for the final shape — a pure
mechanical extraction, no deviations from this plan. **This completes
Direction D** (all four phases). The plan below is kept as-written for the
historical record.

**Goal.** Remove the duplicated two-call `a.AddMessage("user",
...); a.AddMessage("assistant", ...)` pattern between `/read-chunks` and
`@mention` route dispatch.

### Files to modify

| File | Change |
|---|---|
| `harvey.go` (or alongside `AddMessage`) | `func (a *Agent) foldBackTurn(userContent, assistantContent string)` — two `AddMessage` calls |
| `commands.go` (`cmdReadChunks`) | Replace its two `a.AddMessage` calls with `a.foldBackTurn(...)` |
| `terminal.go` (`@mention` route-dispatch branch) | Replace its two `a.AddMessage` calls with `a.foldBackTurn(...)` |
| existing tests | No new tests needed — this is a pure refactor of already-tested call sites; existing `/read-chunks` and `@mention` tests that assert on `a.History` content after dispatch must continue passing unmodified |

### Approach

1. Single mechanical extraction — write `foldBackTurn`, swap both call
   sites, confirm every existing test that inspects `a.History` after a
   `/read-chunks` or `@mention` dispatch still passes unmodified.

### Acceptance criteria

- [ ] `go test ./...` green, no test changes required at either call site.
- [ ] `/plan next` remains untouched — it deliberately does not fold back
      into history.

---

## Deferred / out of scope

- **`@mention`'s local-model-switch fallthrough** (`terminal.go:853-869`) —
  a genuinely different mechanism (persistent switch, full history, no
  restore by design), not part of this consolidation; see design doc.
- **The Ollama-daemon-swap vs. llamafile-relaunch benchmark** — not run by
  this plan; `Restore`'s cost is real either way and unaffected by making
  it correct.
- **Extending tool support to chunk analysis** — chunk analysis stays
  deliberately tool-free; not a duplication-removal candidate.
