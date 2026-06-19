# Harvey Spinner UX & Status Messages — Design

**Status (2026-06-18):** Design settled. See
[spinner-ux-plan.md](spinner-ux-plan.md) for the implementation plan.

This document covers two related UX improvements:
1. Dynamic status messages in the spinner (what Harvey is doing right now).
2. Tab completion for slash commands (noted but deferred — see end of document).

---

## Motivation

Harvey's spinner shows Edward Lear quotes and a timer while waiting. Users
watching a long LLM request have no way to tell whether Harvey is:
- Embedding a query for RAG retrieval
- Calling a built-in tool
- Waiting for the model to respond
- Injecting memory context

Claude Code and similar tools update a status line as work progresses. Users
familiar with those tools expect to see live feedback. The Lear quotes are
distinctive and add personality, but they do not help the user decide whether
to wait or cancel.

---

## Design Principles

1. **Lear messages are the default.** When Harvey has nothing specific to
   report, the spinner shows a Lear quote as today. Status messages are
   additions, not replacements.

2. **Status messages are transient.** A status like "Calling read_file…"
   appears on the message line briefly. When the operation completes or the
   next Lear tick fires, the line reverts to a Lear quote. This prevents
   stale status messages from persisting after the operation ends.

3. **Non-blocking updates.** Callers send status updates without waiting for
   the spinner goroutine. If a status is sent faster than the ticker can
   display it, only the most recent is shown.

4. **No structural change to spinner layout.** The three-line block (label /
   message / frame+timer) is preserved. Status messages occupy the message
   line (line 2), rendered in dim green to distinguish them visually from
   the colored Lear quotes.

---

## Spinner API Changes

### New field: `StatusCh chan string`

```go
type Spinner struct {
    out      io.Writer
    estimate time.Duration
    label    string
    done     chan struct{}
    stopped  chan struct{}
    StatusCh chan string  // new: receives status update strings
}
```

`StatusCh` is a buffered channel with capacity 1, created in `newSpinner`.
The goroutine drains it on each frame tick.

### New method: `UpdateStatus(msg string)`

```go
func (s *Spinner) UpdateStatus(msg string) {
    select {
    case s.StatusCh <- msg:
    default: // drop if channel is full; next tick will read the latest
    }
}
```

Non-blocking. If the channel is full (a previous update hasn't been read
yet), the new update replaces it — only the latest matters.

### Goroutine changes

The `run()` goroutine adds a `lastStatus string` local variable.

On each frame tick:
1. Drain `StatusCh` into `lastStatus` (non-blocking read).
2. If `lastStatus != ""`, render line 2 as `dim("  ⎿") + " " + dimGreen(lastStatus)`.
3. On the next message tick (`msgTick.C`), clear `lastStatus` (revert to
   Lear). This means a status message is shown for at most 6 seconds
   (the existing Lear rotation interval) before Lear resumes.

No change to the timer or frame animation.

### Visual result

```
Ollama (qwen2.5-coder:7b)
  ⎿ Calling read_file…          ← dim green; replaces Lear quote
     ⎿ ⠸ [3s / ~8s]
```

After the Lear rotation fires (or the tool call completes and no new status
arrives):
```
Ollama (qwen2.5-coder:7b)
  ⎿ There was an old man with a beard   ← Lear quote resumes
     ⎿ ⠼ [9s / ~8s]
```

---

## Status Message Injection Points

The following call sites in `terminal.go` send status updates:

| Event | Status string |
|---|---|
| RAG embedding start | `"Searching knowledge base…"` |
| RAG injection complete | `"Found N relevant chunks"` (or empty if 0) |
| Memory context injection | `"Injecting memory context…"` |
| Tool call start | `"Calling <tool-name>…"` |
| Tool call complete (success) | `"<tool-name> done"` |
| Tool call complete (error) | `"<tool-name> failed"` |
| Model switched mid-session | `"Switching to <model>…"` |

The spinner is only running during LLM requests (between the spinner start
and stop calls). Tool calls that happen *inside* a `RunToolLoop` iteration
fire while the spinner is live; updates are safe because `UpdateStatus` is
non-blocking.

The spinner is not active during slash command execution (no LLM request is
in flight), so status updates during `/rag ingest` or `/memory mine` are
out of scope. Those operations already print their own progress lines.

---

## Tab Completion (Deferred)

The TODO also requests better command tab completion to save typing. This is
a more significant change:

- Harvey's `termlib.LineEditor` handles raw terminal input. Tab completion
  requires the editor to detect a `\t` keypress and query the command table
  for completions.
- Prefix matching is straightforward for top-level commands. Subcommand
  completion (e.g., `/memory <tab>` → `mine list show flag forget…`) needs
  the command registry to expose a completion interface.
- Path completion (for commands that take file arguments like `/read FILE`)
  requires filesystem scanning integrated with the line editor.

This is a separate project affecting `termlib/lineeditor.go` and
`commands.go` (or a new `completions.go`). It is out of scope for this work
item. It should be designed as its own document when prioritized.

---

## Out of Scope

- **Tab completion** — separate design effort, noted above.
- **Status messages during `/rag ingest` or `/memory mine`** — these
  operations run their own progress output; spinner integration is not needed.
- **Persistent status log** — status updates are ephemeral; they are not
  written to the debug log. The debug log already records tool call events
  via `LogToolCall`.
- **Color configuration** — status messages are always dim green. No
  user-configurable color theming.
