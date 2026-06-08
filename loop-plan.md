# Harvey `/loop` — Implementation Plan

See [loop-design.md](loop-design.md) for the full design rationale.

## New Module Dependencies

None. Implemented with existing Harvey packages, `time`, and the standard
library signal-handling pattern already used three times in `terminal.go`.

## Files to Create

| File | Purpose |
|------|---------|
| `harvey/loop.go` | `cmdLoop`, argument parsing, the iteration loop, sleep-with-cancellation |
| `harvey/loop_test.go` | Unit tests for parsing and the iteration loop (with `mockLLMClient`) |

## Files to Modify

| File | Change |
|------|--------|
| `harvey/terminal.go` | Factor the inline chat block (~lines 635-820) into `(a *Agent) runChatTurn(ctx, input, out) (reply string, stats ChatStats, err error)`; REPL loop becomes a thin wrapper around it |
| `harvey/commands.go` | Register `"loop"` in `registerCommands()`; add a `"loop"` case to `cmdHelp`'s topic switch and the two topic-list strings (commands.go:566 and 596-600) |
| `harvey/helptext.go` | Add `LoopHelpText` constant |

## Implementation Phases

### Phase 1 — Extract `runChatTurn` from the REPL chat block

Pull the body of the REPL's plain-chat branch (RAG augmentation through
`RecordTurnWithStats`, roughly `terminal.go:681-830`) into:

```go
func (a *Agent) runChatTurn(ctx context.Context, input string, out io.Writer) (reply string, stats ChatStats, err error)
```

- Takes an already-built, cancellable `ctx`. Each call site (REPL, `/loop`)
  keeps owning its own SIGINT setup and "Cancelled."/history-rollback framing
  — `runChatTurn` just makes the model call and returns.
- Returns the assembled reply text and stats.
- The REPL's plain-chat branch becomes a thin wrapper: build ctx/signal
  watcher → `runChatTurn` → existing display/skill-trigger/`autoExecuteReply`
  logic, unchanged.
- This phase is a pure refactor. Run `go test -race` in isolation afterward —
  no existing test's behaviour should change.

### Phase 2 — Argument Parsing

```go
func parseLoopArgs(args []string) (interval time.Duration, count int, rest string, err error)
```

- `args[0]` → `parseDurationString` (config.go:650); error if parse fails or
  result ≤ 0
- Optional `--count N`: if `args[1] == "--count"`, parse `args[2]` as an
  integer in `[1, 100]`; default 10 when the flag is absent
- Remaining tokens joined with spaces → `rest`; error if empty
- No workspace path validation here — `rest` is either free text or a command
  line that `a.dispatch` validates itself

### Phase 3 — Iteration Dispatch

```go
func runLoopIteration(ctx context.Context, a *Agent, rest string, out io.Writer) (exitRequested bool, err error)
```

- If `strings.HasPrefix(rest, "/")`: extract the command name; if it is
  `exit`/`quit`/`bye`, return `(true, nil)` without dispatching; otherwise
  `a.dispatch(rest, out)`
- Else: `a.runChatTurn(ctx, rest, out)`, print the reply and stat line —
  same shape as the REPL's display, minus the skill-trigger/`autoExecuteReply`
  exclusions documented in the design

### Phase 4 — Sleep With Cancellation

```go
func sleepInterruptible(ctx context.Context, d time.Duration) (cancelled bool)
```

- `select` on `time.NewTimer(d).C` and `ctx.Done()`
- Reuses the same SIGINT-watcher-goroutine pattern as the rest of
  `terminal.go` — the orchestrator owns one cancellable context for the whole
  run and passes it down, so cancellation during either the iteration or the
  sleep surfaces identically

### Phase 5 — Orchestrator

```go
func cmdLoop(a *Agent, args []string, out io.Writer) error
```

1. `parseLoopArgs` — on error, print usage and return `nil` (matches other
   commands' "print usage, don't error" convention, e.g. `/pipeline` with no
   threshold)
2. Print plan summary: `Looping every %s, up to %d time(s): %s`
3. Build one cancellable `context.Context` + SIGINT watcher for the whole run
4. Loop `i := 1..count`:
   - `runLoopIteration`
   - if it reports `exitRequested`, print `loop: stopping — %q would exit
     Harvey` and break
   - if cancelled, break
   - if `i < count`, `sleepInterruptible`; if it reports cancellation, break
5. Print summary: `Loop finished after %d/%d iteration(s)` or
   `Loop cancelled after %d/%d iteration(s)`

### Phase 6 — Help Text & Registration

- `LoopHelpText` in `helptext.go`, following the existing
  `%{app_name}(7) user manual...` template (see `RunHelpText` for a
  similarly single-purpose command's guide)
- `registerCommands()`:
  ```go
  "loop": {
      Usage:       "/loop INTERVAL [--count N] PROMPT|/COMMAND",
      Description: "Repeat a prompt or command on an interval, up to N times",
      Handler:     cmdLoop,
  },
  ```
- `cmdHelp`: add `case "loop": fmt.Fprint(out, FmtHelp(LoopHelpText, ...))`
  and add `loop` to both topic-listing strings

### Phase 7 — Tests (`loop_test.go`)

| Test | Covers |
|------|--------|
| `TestParseLoopArgs_valid` | `5m`, `30s`, `300`, with and without `--count` |
| `TestParseLoopArgs_invalid` | Bad duration, zero/negative interval, `--count` out of range or non-numeric, empty prompt |
| `TestCmdLoop_chatMode` | N iterations of a plain prompt against `mockLLMClient`; asserts `a.History` grows by 2×N messages |
| `TestCmdLoop_commandMode` | Loops a harmless built-in (`/status`) N times; asserts no chat call is made |
| `TestCmdLoop_exitSentinel` | Looping `/exit` stops the loop without exiting Harvey |
| `TestCmdLoop_countCap` | `--count 0` and `--count 101` rejected with usage messages |
| `TestSleepInterruptible_cancel` | Cancelling the context returns promptly without waiting the full duration |
| `TestRunChatTurn_*` | Coverage for the extracted helper — add minimal tests in Phase 1 if the inline block had none |

## Acceptance Criteria

- [ ] `/loop 5m Check the build` sends the prompt every 5 minutes, up to the
      default 10 times, displaying replies exactly as normal chat would
- [ ] `/loop 30s --count 3 /git status` dispatches `/git status` three times,
      30 seconds apart
- [ ] Ctrl+C during an iteration or during the sleep stops the loop and
      prints a count-aware "Cancelled" message
- [ ] `--count` outside `[1, 100]` is rejected with a usage message before any
      iteration runs
- [ ] Looping `/exit`/`/quit`/`/bye` stops the loop without exiting Harvey
- [ ] `a.Recorder` (when active) records every iteration exactly as it would
      record the equivalent typed-by-hand turn or command
- [ ] `/help loop` prints `LoopHelpText`; `/loop` appears in `/help`'s command
      table and topic list
- [ ] `go test ./...` and `go test -race` pass, including the Phase 1 refactor
      of the REPL chat block
