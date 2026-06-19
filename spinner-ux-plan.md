# Harvey Spinner UX — Implementation Plan

See [spinner-ux-design.md](spinner-ux-design.md) for the full design
rationale and decisions.

---

## Phase A — Dynamic Status Messages in Spinner

**Goal:** Add `UpdateStatus(msg string)` to `Spinner` and wire it into the
key operation call sites in `terminal.go`.

### Files to modify

| File | Change |
|------|--------|
| `spinner.go` | Add `StatusCh chan string` field, `UpdateStatus` method, `lastStatus` tracking in `run()` |
| `spinner_test.go` | Add test: `UpdateStatus` causes the message line to show the status string |
| `terminal.go` | Call `spin.UpdateStatus(...)` at RAG embed, tool call start/end, memory injection |

---

### `spinner.go` changes

#### Struct

```go
type Spinner struct {
    out      io.Writer
    estimate time.Duration
    label    string
    done     chan struct{}
    stopped  chan struct{}
    StatusCh chan string // receives transient status updates
}
```

#### `newSpinner` update

```go
func newSpinner(out io.Writer, estimate time.Duration, label string) *Spinner {
    s := &Spinner{
        out:      out,
        estimate: estimate,
        label:    label,
        done:     make(chan struct{}),
        stopped:  make(chan struct{}),
        StatusCh: make(chan string, 1), // buffered: drop if full
    }
    go s.run()
    return s
}
```

#### `UpdateStatus` method

```go
/** UpdateStatus sends a transient status message to the spinner's message
 * line. Non-blocking: if a previous status has not been consumed yet, the
 * new message replaces it. Calling UpdateStatus on a stopped spinner is safe
 * (the send is discarded).
 *
 * Parameters:
 *   msg (string) — status string to display, e.g. "Calling read_file…"
 *
 * Example:
 *   spin.UpdateStatus("Searching knowledge base…")
 */
func (s *Spinner) UpdateStatus(msg string) {
    select {
    case s.StatusCh <- msg:
    default:
        // Channel full — drain and replace with latest.
        select {
        case <-s.StatusCh:
        default:
        }
        select {
        case s.StatusCh <- msg:
        default:
        }
    }
}
```

#### `run()` goroutine changes

Add `lastStatus string` as a local variable. On each `frameTick.C` event:

```go
case <-frameTick.C:
    // Drain status channel.
    select {
    case s := <-s.StatusCh:
        lastStatus = s
    default:
    }

    frameIdx = (frameIdx + 1) % len(spinnerFrames)
    elapsed := time.Since(start)

    var msgLine string
    if lastStatus != "" {
        msgLine = dim("  ⎿") + " " + dimGreen(lastStatus)
    } else {
        msgLine = line2(msgIdx)
    }
    // Redraw line 2 and line 3.
    fmt.Fprintf(s.out, "\033[1A\r%s\033[K\r\n%s\033[K",
        msgLine,
        line3(frameIdx, elapsed),
    )
```

On each `msgTick.C` event (Lear rotation):
```go
case <-msgTick.C:
    lastStatus = "" // clear status; revert to Lear
    msgIdx = (msgIdx + 1) % len(LearMessages)
    // existing redraw logic
```

**Note on redraw frequency:** The current implementation separates fast
(frame) and slow (msg) redraws. Currently `frameTick.C` only redraws line 3.
To show status updates promptly, we need to redraw line 2 on status arrival.
The simplest change: on `frameTick.C`, always redraw both lines 2 and 3 when
`lastStatus` has changed since the last frame. Track `renderedStatus string`
alongside `lastStatus` to detect changes.

This increases the write frequency slightly but keeps the existing split
between frame (100ms) and message (6s) ticks.

#### `dimGreen` helper

If not already defined in `terminal.go` or a color helpers file:

```go
func dimGreen(s string) string {
    return "\033[2;32m" + s + "\033[0m"
}
```

Check whether Harvey already has a dim+green combination; reuse if so.

---

### `terminal.go` call sites

Add `spin.UpdateStatus(...)` calls at these points (search for existing
`spin` variable usage to find exact line numbers):

```go
// Before RAG embedding:
spin.UpdateStatus("Searching knowledge base…")

// After RAG injection (N chunks found):
if n > 0 {
    spin.UpdateStatus(fmt.Sprintf("Found %d relevant chunk(s)", n))
}

// Before memory context injection:
spin.UpdateStatus("Injecting memory context…")

// At tool call start (inside RunToolLoop or tryExecuteProseToolCalls):
spin.UpdateStatus(fmt.Sprintf("Calling %s…", toolName))

// At tool call completion:
if err != nil {
    spin.UpdateStatus(fmt.Sprintf("%s failed", toolName))
} else {
    spin.UpdateStatus(fmt.Sprintf("%s done", toolName))
}
```

The spinner is not available inside `RunToolLoop` directly (the spinner is
started in the REPL's chat path in `terminal.go`). The cleanest approach:
pass the spinner reference to `RunToolLoop` as an optional interface:

```go
type StatusReporter interface {
    UpdateStatus(msg string)
}
```

`RunToolLoop` accepts an optional `StatusReporter` (nil if no spinner is
active). The REPL passes `spin` when calling `RunToolLoop`; tests and
non-interactive paths pass `nil`.

Alternatively, attach `StatusReporter` to `Agent` temporarily for the
duration of the LLM call. This avoids a signature change to `RunToolLoop`.
Choose whichever is less invasive given the current signature.

---

### `spinner_test.go` changes

Add a test that:
1. Creates a spinner with a `bytes.Buffer` as output.
2. Calls `spin.UpdateStatus("hello")`.
3. Waits 150ms (long enough for one frame tick).
4. Stops the spinner.
5. Asserts the buffer contains `"hello"` in the output.

The test must handle the ANSI escape sequences in the output (search for
`"hello"` as a substring, not an exact match).

---

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- During an LLM call, the spinner message line shows transient status strings
  at the injection points listed above.
- After 6 seconds without a status update, the Lear message resumes.
- `UpdateStatus` on a stopped spinner does not panic or block.
- The spinner test asserts `"hello"` appears in output after `UpdateStatus`.

---

## Tab Completion (Out of Scope — Notes Only)

Tab completion is mentioned in the TODO as a separate improvement. It is not
part of this plan. Key design questions to resolve when that work begins:

1. **Where does completion live?** Options:
   - In `termlib/lineeditor.go` as a pluggable `CompletionFunc`.
   - In `commands.go` as a static completion table.
   - Combination: `LineEditor` calls a registered `CompletionFunc`; Harvey
     registers the function at startup.

2. **What completes?**
   - Top-level commands (`/`, then prefix matching).
   - Subcommands (e.g., `/memory <tab>` → `mine list show flag forget…`).
   - File arguments (OS path completion for `/read FILE`, `/attach FILE`).
   - Model names for `/model`, `/ollama use`, `/llamafile use`.

3. **Edge cases:**
   - Multiple matches: show a completion menu or cycle on repeated Tab.
   - Path completion with spaces: quoting conventions.
   - Non-interactive terminals: Tab is a literal character; no completion.

Design that work as a separate `tab-completion-design.md` when prioritized.

---

## Dependency Graph

```
Phase A (spinner status) — independent; no dependencies on other open items
```

---

## Open Questions

- What is the exact signature of `RunToolLoop` today? Determine whether to
  pass `StatusReporter` as a parameter or attach it to `Agent`. Check
  `tool_executor.go` before implementing.
- Does Harvey already define a `dimGreen` color function? Check color helpers
  in `terminal.go` or a separate colors file.
