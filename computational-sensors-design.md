# Harvey — Computational Guide/Sensor Tools (Direction A) — Design

**Date**: 2026-07-12
**Status**: ✅ Both phases implemented 2026-07-12 — see `DECISIONS.md` and
[computational-sensors-plan.md](computational-sensors-plan.md)
**Related**: [harness-engineering-exploration.md](harness-engineering-exploration.md)
Direction A, [DECISIONS.md](DECISIONS.md) (2026-07-12 sensor-unification
entries this builds on)

---

## What's already there (found during scoping, not assumed)

Before designing anything new, checked what Harvey already has, since the
exploration doc's Direction A was written as an outside audit, not an
implementation plan:

- **`CodeFormatter.Check(content, filePath) (bool, []FormatIssue)`**
  (`language_registry.go:243`) already exists, is implemented for Go via
  `gofmt` (`PipeExternalFormatter`, `code_formatters.go`), and is unit-tested
  (`code_formatters_test.go`). It is **dormant** — grepped every non-test
  call site; nothing calls `.Check()` outside its own tests. Neither
  `/format` (`commands.go:3733`) nor the auto-write path calls it; both only
  call `.Format()`.
- **The "computational guide" half of Direction A already exists**:
  `applyAutoFormat` (`builtin_tools.go:1307`) runs the registered formatter
  automatically after every `write_file`, gated by `a.Config.AutoFormat`
  (default `true`). Its result note (`"formatted"` / `"already formatted"`)
  is appended directly into the tool's own returned string
  (`builtin_tools.go:246-253`: `msg += " (" + note + ")"`), so the **model**
  sees it as part of the tool-result content, not the human via any
  `SensorEvent`. (Correction: an earlier draft of this document assumed a
  separate `edit_file` tool existed and described a gap where
  `applyAutoFormat` wasn't wired to it. Checked — Harvey has no `edit_file`
  tool at all, only whole-file `write_file`. There is no second call site to
  wire; that "gap" doesn't exist.)

So the real gap for Direction A is narrower than "build sensor
infrastructure from scratch": (1) wire the already-built `Check()` into the
same automatic post-write path, and (2) add sensors for what formatting
can't catch — correctness, not style.

## Scope for this increment

Per the exploration doc's own Risks and Limits section ("activating several
rules at once surfaces a wave of findings at once... rules should likely be
introduced incrementally, not as a bulk ruleset dump"), this design covers
exactly two sensors, not the full candidate list from the exploration doc:

1. **`Check()` wiring** — surface the dormant `gofmt` `Check()` result after
   `write_file`, reusing existing, tested infrastructure.
2. **`go vet`** — the first genuinely new sensor. Chosen over
   `staticcheck`/`gocyclo`/unused-param/hunspell because `go vet` only
   reports near-certain bugs (unreachable code, format-string mismatches,
   suspicious struct tags, etc.) by design — it has no style-nit noise floor
   to tune, unlike a general linter. Lower risk to introduce first; the rest
   stay candidates for a follow-on increment once this one's real behavior
   is observed.

Both are **Computational** in the `SensorEvent`/`SensorClass` sense
(`sensor_event.go`) — deterministic, CPU-run, no LLM judgment.

## Visibility decision (2026-07-12)

Confirmed with the user before designing further: findings are
**human-visible by default** (always emit a `SensorEvent` — free, no token
cost, matches this session's overall emphasis on protecting the Pi's tight
context budget) and **model-visible (appended to the tool-result string,
costing tokens) only when the finding is severe enough to warrant it, or
behind a config flag for users who want stronger self-correction at the
cost of tokens.**

Applying that split concretely to the two sensors in scope:

- **`go vet` findings are always also appended to the tool-result string.**
  `go vet`'s own design philosophy is to only report near-certain bugs, not
  style preferences — there is no low-severity tier to gate on, so treating
  every finding as "severe enough" is the honest reading of the decision
  above, not a loophole around it.
- **`Check()`/`gofmt` findings are `SensorEvent`-only, never appended to the
  tool-result string**, gated behind a config flag
  (`Config.SensorInjectFormatFindings`, default `false`) for a user who wants
  it anyway. Rationale: `applyAutoFormat` already runs `Format()` first and
  rewrites the file — for Go specifically, `Check()` should almost never
  find anything left to report after a successful auto-format; the residual
  case (a syntax error `gofmt` can't parse, so `Format()` silently no-ops
  per `applyAutoFormat`'s existing "errors are silently suppressed" comment)
  is rare enough that spending tokens on it by default isn't warranted, but
  visibility to a human debugging that rare case is still cheap and useful.

## Proposed shape

```go
// runPostWriteSensors runs the currently-enabled computational sensors for a
// just-written file and returns (sensorEvents, toolResultAppendix).
// sensorEvents are always reported via the agent's Status (when non-nil);
// toolResultAppendix is empty unless a finding warrants spending tokens on
// model-visibility (see computational-sensors-design.md).
func runPostWriteSensors(a *Agent, relPath, absPath, content string) (events []SensorEvent, toolResultAppendix string)
```

Called from the `write_file` handler (`builtin_tools.go`), after
`applyAutoFormat` — Harvey has no separate `edit_file` tool to also wire.

`go vet` invocation: `go vet <package-dir>` via `os/exec`, scoped to the
package containing the written file — not the whole module — to keep it
fast enough to run synchronously in the write path on Pi-class CPU. Output
parsed line-by-line (`go vet`'s output format is stable: `file:line:col:
message`) into `[]SensorEvent{Kind: "go_vet", Class: Computational}`.

`Check()` invocation: call the already-existing `f.Check(content, absPath)`
directly — no new parsing needed, `FormatIssue` already has the right shape.

## Open questions for the plan

- Should `go vet` run on every `write_file` call, or only for
  `.go` files (obviously only Go, but: only when `go.mod` exists in the
  workspace, to avoid a confusing "go vet failed" note in a non-Go project
  containing incidental `.go` fixture files)?
- Timeout/failure handling: `go vet` requires a compilable package. A
  work-in-progress file that doesn't compile yet will make `go vet` itself
  fail (not "found issues" — outright error). This must not be surfaced as a
  false sensor finding indistinguishable from a real `go vet` warning, and
  must never block the write itself (matching `applyAutoFormat`'s existing
  "errors are silently suppressed" policy) — needs explicit handling in the
  plan, not left implicit.
