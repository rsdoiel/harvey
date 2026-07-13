# Harvey — Computational Guide/Sensor Tools — Implementation Plan

See [computational-sensors-design.md](computational-sensors-design.md) for
the full rationale and the 2026-07-12 visibility decision. See
[DECISIONS.md](DECISIONS.md) for the finalized decision entries as each
phase lands.

Resolved here, as implementation-level judgment calls rather than
user-facing policy decisions (the design doc's two "open questions"):

- **`go vet` only runs when the workspace has a `go.mod`.** Trivial
  correctness requirement — avoids a confusing "go vet" note appearing in a
  non-Go project that happens to contain an incidental `.go` fixture file.
- **Distinguishing a real `go vet` finding from `go vet` itself failing to
  run** (e.g. the package doesn't compile yet, mid-edit): `go vet`'s output
  for a build failure includes a header line starting with `"# "` (package
  path), followed by raw compiler diagnostics, distinct from its own
  per-analyzer findings (which are plain `file:line:col: message` with no
  `"# "` header). Treat any output containing a `"# "`-prefixed line as a
  non-actionable build failure — suppress entirely, matching
  `applyAutoFormat`'s existing "errors are silently suppressed" policy —
  rather than attempting to surface it as a sensor finding.

---

## Phase A — Wire `Check()` into the post-write path ✅ Complete 2026-07-12

See `DECISIONS.md` (2026-07-12 entry) for the final shape and test list. The
plan below is kept as-written for the historical record.

**Goal.** Surface the existing, tested, dormant `gofmt` `Check()` result as
a `SensorEvent` after `write_file`, with no token cost by
default (per the design doc's visibility decision — `Check()` findings are
`SensorEvent`-only unless `Config.SensorInjectFormatFindings` is set).

### Files to modify

| File | Change |
|---|---|
| `config.go` | Add `SensorInjectFormatFindings bool` (default `false`) to `Config`; surface in `config_yaml.go` as `sensors.inject_format_findings` |
| `builtin_tools.go` | New `runPostWriteSensors(a *Agent, relPath string) (appendix string)`; called from the `write_file` handler, after `applyAutoFormat`; reports via `a.ActiveStatus` directly rather than returning events |
| `builtin_tools.go` | `runPostWriteSensors`'s `Check()` half: call `f.Check(content, absPath)` (reusing the existing formatter already resolved for `applyAutoFormat`); build a `SensorEvent{Kind: "format_check", Message: ..., Class: Computational}` per `FormatIssue`; report each via `a`'s active `StatusReporter` if one is reachable from the tool handler (see note below), or return them for the caller to report — **the tool-handler signature (`func(ctx, args) (string, error)`) has no `io.Writer`/`StatusReporter` parameter today; this phase must resolve how a tool handler reaches the same reporting path `ExecuteToolCalls` already has (`e.Status`)** — likely by giving `Agent` a settable "current status reporter" field the handler closures can read, since `ToolRegistry.RegisterTool` closures already capture `a *Agent` |
| `builtin_tools_test.go` | Tests for `runPostWriteSensors`'s `Check()` half: a file with a real gofmt issue produces a `SensorEvent`, not a tool-result appendix (default config); with `SensorInjectFormatFindings: true`, the appendix is non-empty too |

### Approach

1. **Resolved mechanism for a tool handler to reach a `StatusReporter`:**
   every builtin tool handler is a closure created in
   `RegisterBuiltinTools(r *ToolRegistry, a *Agent)`
   (`builtin_tools.go:41`), so it already captures `a` — but `a` has no
   reference to the turn's live `StatusReporter` today (that lives only on
   `ToolExecutor.Status`, which the handler closures never see;
   `ToolExecutor` itself has no `*Agent` reference either — the two are
   wired together only via `terminal.go`). Adding a `ToolHandler` signature
   change to thread a reporter through would touch every registered tool.
   Instead: add `Agent.ActiveStatus StatusReporter` (`harvey.go`), set it
   alongside the two existing `ex.Status = sp` / `ex.Status = retrySp`
   assignments in `terminal.go` (`runChatTurn`'s main call and its
   cannot-read-file retry), and clear it back to `nil` once each spinner
   stops. A handler closure reports via `a.ActiveStatus.ReportSensor(ev)`
   with the existing `if ... != nil` nil-check pattern already used
   elsewhere for optional reporters. This keeps `ToolHandler`'s signature
   unchanged and only touches two call sites in `terminal.go` plus one new
   field.
2. Write tests for `runPostWriteSensors`'s `Check()` half first (TDD),
   confirm red, implement, confirm green.
3. Wire into `write_file` (Harvey's only file-write tool).

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] Writing an intentionally-unformatted `.go` file produces a
      human-visible `SensorEvent` (default config) with no token cost added
      to the tool result.
- [ ] With `sensors.inject_format_findings: true`, the same write also
      appends the finding to the tool-result string.

---

## Phase B — Add `go vet` as the first new sensor ✅ Complete 2026-07-12

See `DECISIONS.md` (2026-07-12 entry, second of that date) for the final
shape and one deviation from this plan: the timeout uses a dedicated 15s
constant rather than `Config.Security.RunTimeout` as originally suggested
below — reusing the 5-minute `run_command` timeout would let a hung `go vet`
block every single file write for far too long. The plan below is kept
as-written for the historical record.

**Goal.** A genuinely new computational sensor: run `go vet` on the package
containing a just-written `.go` file, report findings as `SensorEvent`s, and
always append findings to the tool-result string (per the design doc's
visibility decision — `go vet` has no low-severity tier to gate on).

### Files to modify

| File | Change |
|---|---|
| `builtin_tools.go` | `runPostWriteSensors`'s second half: when the workspace has `go.mod` and the written file is `.go`, run `go vet <package-dir>` via `os/exec` with a timeout; parse output; on any `"# "`-prefixed line, treat the whole result as a non-actionable build failure and suppress (see resolved judgment call above) |
| `builtin_tools_test.go` | Tests: a file with a real `go vet`-detectable issue (e.g. a `Printf` call with a format/argument mismatch) produces a `SensorEvent` **and** a tool-result appendix; a file with a genuine compile error (undefined symbol) produces neither — confirms the `"# "`-prefix suppression logic; a workspace without `go.mod` never invokes `go vet` at all |

### Approach

1. Write the three characterization/new-behavior tests above first, confirm
   red (function doesn't exist / doesn't yet distinguish the compile-failure
   case), implement, confirm green.
2. Timeout: reuse whatever timeout convention `run_command`
   (`builtin_tools.go`) already uses for external processes, rather than
   inventing a new one.

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] A real `go vet` finding (e.g. bad `Printf` format string) is both
      human-visible (`SensorEvent`) and model-visible (tool-result
      appendix).
- [ ] A mid-edit compile error never surfaces as a false sensor finding and
      never blocks the write.
- [ ] No `go.mod` in the workspace → `go vet` is never invoked (no wasted
      process spawn, no confusing output).

---

## Deferred to a follow-on increment

`staticcheck`/`gocyclo` (complexity), unused-parameter check, a
changed-file-without-changed-test heuristic, and `hunspell`/grammar checking
for the scholarly-prose use case — all named as candidates in
`harness-engineering-exploration.md` Direction A, deliberately not part of
this plan. Per that document's own Risks and Limits section, introduce one
sensor at a time and observe real behavior before adding the next, rather
than activating a bulk ruleset in one pass.
