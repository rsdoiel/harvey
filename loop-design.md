# Harvey `/loop` — Design

## Overview

`/loop` repeats a prompt or slash command on a fixed interval, for a bounded
number of iterations, entirely within the existing REPL. It exists for
workflows like "check the build every 5 minutes and tell me if it breaks" or
"run /git status every 30 seconds while I refactor."

Harvey's REPL (`terminal.go:Run`) is a single-threaded loop that blocks on
each turn; there is no async wake-up mechanism comparable to Claude Code's
session scheduler. `/loop` is therefore a **blocking, foreground** command: it
takes over the REPL for its duration, runs its iterations in-process, and
returns control when it finishes, is cancelled, or hits its iteration cap.

## Command Syntax

```
/loop INTERVAL [--count N] PROMPT
/loop INTERVAL [--count N] /COMMAND [ARGS...]
```

| Argument | Format | Example |
|----------|--------|---------|
| `INTERVAL` | Required, parsed by `parseDurationString` | `30s`, `5m`, `1h`, `300` |
| `--count N` | Optional, integer, default 10, max 100 | `--count 5` |
| `PROMPT` | Free text sent as a chat turn each iteration | `Summarize new log entries` |
| `/COMMAND` | A slash command dispatched each iteration | `/git status`, `/run make test` |

If the trailing text begins with `/`, `/loop` dispatches it as a command via
`a.dispatch()`; otherwise it is sent as a chat prompt. This mirrors the
existing distinction in the REPL between `/commands`, `!shell`, `@mentions`,
and plain chat — `/loop` does not invent a new input grammar, it picks the
existing path that matches the trailing text.

`--count` follows the `--depth N` convention already used by `/read-dir`.

## Why a Required Interval, No Self-Pacing

Claude Code's `/loop` can omit the interval and let the agent self-pace via a
wake-scheduling primitive. Harvey has no equivalent: it is a synchronous CLI
process with no persistent scheduler, so "self-pacing" would have to mean
either (a) Harvey guesses an interval once and runs at a fixed cadence anyway
— which is just a worse version of asking the user — or (b) the process stays
resident and wakes itself, a different program shape than Harvey is today.
Requiring an explicit `INTERVAL` is the honest mapping of the feature onto
Harvey's architecture: the user picks the cadence, Harvey executes it
reliably.

## Execution Model

`cmdLoop` runs its own loop inside the command handler:

```
print plan summary ("Looping every 5m, up to 10 times: <prompt>")
for i := 1; i <= count; i++ {
    run one iteration (chat turn or command dispatch)
    if cancelled { break }
    if i < count {
        sleep(interval), interruptible by Ctrl+C
    }
}
print summary ("Loop finished after N iteration(s)" | "Loop cancelled after N iteration(s)")
```

### Cancellation

Every long-running operation in `terminal.go` (chat, `!` commands, `@mention`
dispatch) uses the same pattern: a cancellable `context.Context`, a goroutine
watching `os.Signal` for `SIGINT`, and a `wasCancelled` flag checked after the
operation returns. `/loop` builds **one** such context/watcher for the whole
run — not one per iteration — so cancellation during either an iteration or
the inter-iteration sleep (`time.NewTimer` raced against `ctx.Done()` in a
`select`) surfaces the same way.

**Any `Ctrl+C` stops the loop**, whether it lands mid-iteration or during the
sleep — it does not just cancel the current iteration and continue. This
matches the existing convention ("Cancelled." returns to the prompt) and
avoids a second control surface (e.g. "press Ctrl+C twice to fully stop")
that nothing else in Harvey has.

## Iteration Cap

`--count` defaults to 10 and is capped at 100. An unbounded `/loop` would be
the first Harvey command that can run LLM calls — and, if tools are enabled,
write files or execute shell commands — indefinitely without supervision.
Bounding it by default, with a ceiling that still allows long unattended runs
(100 × 5m ≈ 8 hours) but blocks `--count 999999` typos, is a deliberate safety
choice consistent with Harvey's existing safe-mode/permissions/audit posture.

## Chat-Mode Iteration Flow

A chat-mode iteration performs the *same model call* a normal chat turn would
— same RAG augmentation, same tool-loop-or-plain-chat branch, same recording —
so that looping a prompt behaves predictably and consistently with typing it
by hand repeatedly. To get this without duplicating ~150 lines of REPL loop
body, the existing inline chat block in `terminal.go` (roughly lines 635-820)
is factored into a shared helper:

```go
func (a *Agent) runChatTurn(ctx context.Context, input string, out io.Writer) (reply string, stats ChatStats, err error)
```

`/loop` calls this helper directly. It deliberately **excludes** two things
that the REPL's inline block does around the same call:

| Excluded behaviour | Why excluded from `/loop` |
|---|---|
| Skill auto-trigger dispatch (`MatchesTrigger`) | Could silently redirect a looped prompt to a different skill on some iteration but not others — surprising in an unattended run |
| `autoExecuteReply` / code-block-to-file offers | These are interactive Y/n prompts; they would block an unattended loop waiting on stdin that nothing will type |

(The one-time memory-context injection is also skipped, but only because it
already fired once per session before `/loop` could run — not a deliberate
exclusion.)

Everything else — RAG augmentation, the tool-loop vs. plain-chat branch,
token/context warnings, stats recording, Fountain recording — is identical to
a normal turn, because those are properties of "how Harvey answers a prompt,"
not properties of the REPL's input-reading loop.

## Command-Mode Iteration Flow

When the trailing text starts with `/`, each iteration is simply:

```go
_, err := a.dispatch(commandLine, out)
```

This is intentionally the *only* thing `/loop` does in command mode — the
dispatched command already handles its own recording, audit logging, and
safe-mode checks (e.g. `/run` checks `a.Config.IsCommandAllowed`). `/loop`
adds no new execution surface; it just calls the same dispatcher the REPL
calls, repeatedly.

If the dispatched command would itself exit Harvey (`/exit`, `/quit`,
`/bye`), `/loop` recognises this and stops the loop instead of forwarding the
exit — exiting mid-loop from a scripted command would be surprising and hard
to recover from.

## Session Recording

No changes to `recorder.go`. Chat-mode iterations are recorded via the same
`RecordTurnWithStats` call a normal turn makes (through `runChatTurn`);
command-mode iterations are recorded by whatever the dispatched command
already does. The loop's start/stop/cancellation is visible in the live
terminal output but is **not** specially marked in the Fountain transcript —
a reader sees N consecutive ordinary turns or command invocations, which is
an accurate (if slightly repetitive) record of what happened. A dedicated
loop-boundary marker is left as a future enhancement if transcript
readability turns out to matter in practice (see "Out of Scope").

## Safety Considerations

- **Tool calls**: if `a.Config.ToolsEnabled`, looped prompts go through
  `RunToolLoop` exactly as normal chat does, including any file writes or
  shell execution the model decides to perform. `/loop` does not suppress
  this — doing so would make looped chat behave differently from normal chat,
  which is more confusing than the (documented) risk of repetition. The
  printed plan summary before the first iteration is the user's chance to
  Ctrl+C before anything runs.
- **Safe mode / audit**: command-mode iterations go through `a.dispatch`,
  which already enforces `a.Config.SafeMode` / `IsCommandAllowed` and writes
  to `a.AuditBuffer`. No new checks are needed in `/loop` — it inherits them
  by reusing the dispatcher.
- **Iteration cap**: see above.

## Error Conditions

| Condition | Message | Behaviour |
|---|---|---|
| Missing/invalid interval | `loop: invalid interval %q — use e.g. 30s, 5m, 1h` | Print usage, no iterations run |
| Interval ≤ 0 | `loop: interval must be positive` | Print usage, no iterations run |
| `--count` not an integer, ≤ 0, or > 100 | `loop: --count must be an integer between 1 and 100` | Print usage, no iterations run |
| Empty prompt/command | `Usage: /loop INTERVAL [--count N] PROMPT` | Print usage |
| `/exit`\|`/quit`\|`/bye` dispatched mid-loop | `loop: stopping — %q would exit Harvey` | Stop loop, do not exit |
| Ctrl+C | `Cancelled. Loop stopped after N/M iteration(s).` | Stop loop, return to prompt |
| Iteration error (chat or dispatch) | printed inline by the iteration itself, exactly as it would be outside a loop | Continue to the next iteration |

The last row is deliberate: a transient error in one iteration (e.g. a model
timeout) does not stop the loop — only the user (Ctrl+C) or the count does.
This matches "check on something periodically," where an occasional miss is
expected and shouldn't end the monitoring.

## Rejected Alternatives

- **Background goroutine loop** — would require locking around `a.History`,
  `a.Recorder`, and the shared output writer, none of which exist today
  because Harvey has never needed them. The added concurrency-safety surface
  is large for a feature whose blocking version works fine.
- **Self-pacing (omit interval)** — no wake-scheduling primitive exists in
  Harvey; faking it would either be a fixed interval in disguise or a
  different program shape. Honest mapping: require the interval.
- **Model-driven stop sentinel** (e.g. reply contains `LOOP_DONE`) — adds
  prompt-engineering fragility (what if the model says it conversationally?)
  for a v1 feature whose primary controls are already the count and Ctrl+C.
  Could be layered on later behind an opt-in flag.
- **Confirmation prompt before starting** — adds friction without much safety
  benefit; the user already typed the interval and count explicitly, and the
  printed plan summary plus immediate Ctrl+C give the same "are you sure"
  window without an extra keypress.
- **Unbounded iterations by default** — rejected for the reasons in
  "Iteration Cap" above.

## Out of Scope (Future Enhancements)

- Dedicated Fountain recorder markers for loop start/stop/cancellation.
- Model-driven early termination (sentinel-based or confidence-based, akin to
  `/pipeline`'s confidence gating).
- `/loop status` / `/loop stop` as separate commands — not reachable while
  the loop blocks the REPL; would only make sense if `/loop` ever became
  backgroundable, which this design deliberately avoids.
- Auto-`/clear` or auto-`/summarize` between iterations to bound history
  growth on short intervals — left to the user's judgement on interval choice
  for v1; worth revisiting if long, tight-interval loops prove to blow context
  windows in practice.
