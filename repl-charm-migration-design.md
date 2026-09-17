# REPL line-editor migration: termlib → Charm — Design Brief

> Phase 1 (Discussion/Design) of `DISCUSS_REVIEW_PLAN_IMPLEMENT.md`. Nothing
> is agreed by this document; it exists to be reviewed and turned into
> decision records in `harvey/decisions/` (new — see the "Decisions
> realignment" thread in `TODO.md` and
> `knowledge-learning-mode-feature-request.md` item 6).

## Problem statement

`harvey`'s next release drops the `github.com/rsdoiel/termlib` dependency
(user request 2026-09-15, gating `make release`). The user has decided the
replacement should adopt the Charm ecosystem (`bubbletea`/`bubbles`), which
`knowledge`'s `cmd/kb` already uses (the only Charm dependency anywhere in
the Laboratory so far). This document scopes *how much* of harvey adopts
Charm, since "migrate to Charm" could mean anything from swapping one
component to rearchitecting the whole REPL.

## What termlib actually provides today

`terminal.go` uses exactly this surface (confirmed by grep, not recall):
`termlib.NewLineEditor`, `.Completer` field, `.Prompt(prompt string)
(string, error)`, `.AppendHistory`, `.SetHistory`/`.History`,
`termlib.ErrInterrupted`. `lineeditor.go` (661 lines) implements, per its
own doc comment:

- Left/Right/Home/End cursor movement, Backspace
- Up/Down arrow history navigation (single-line only)
- Tab word-completion via `Completer func(line string) []string` — first
  Tab lists matches + fills longest common prefix, subsequent Tabs cycle
- Ctrl+A/E/J/K, Ctrl+C → `ErrInterrupted`, Ctrl+D → EOF-or-delete
- Multi-line input (Ctrl+J inserts a newline, Enter submits the whole
  buffer; history disabled once a newline is present)
- Ctrl+X Ctrl+E → shells out to `$EDITOR`/`$VISUAL`/vi to compose the line
- Raw-mode via `golang.org/x/term`, with a plain-line fallback when stdin
  isn't a TTY (tests, piped input)

`terminal.go`'s `Run()` loop calls `le.Prompt(a.prompt())` **once per
top-level turn**, blocking, inside a large ordinary `for {}` loop that then
synchronously does everything else — `/` command dispatch (which itself
does further prompting, e.g. `promptAction`'s Y/n boxes and `ui.go`'s
`SelectFrom`/`SelectItem` pickers), `!` shell commands with live streamed
output, `@mention` model switching, and the LLM chat/tool-call turn itself.
None of that surrounding logic is event-driven today.

## What Charm actually offers (verified against the locally cached modules,
`bubbles@v1.0.0`, `bubbletea@v1.3.10` — not assumed)

- `bubbletea.NewProgram` runs inline by default; `tea.WithAltScreen()` is
  opt-in (`options.go`). A Bubble Tea program does **not** have to take
  over the whole screen — it can behave like termlib does today, printing
  into the normal scrollback.
- `bubbles/textinput` has built-in suggestion/autocomplete support
  (`SetSuggestions`, Tab-accept, Up/Down or Ctrl+N/Ctrl+P to cycle
  matches) — close to termlib's Tab-completion, but suggestions match
  against the *whole current value*, not termlib's "complete the current
  word up to the cursor" semantics; harvey's `Completer` (slash-command
  args) would need adapting, and Up/Down's default suggestion-cycling
  binding collides with termlib's Up/Down-for-history — the keymap needs
  deliberate remapping.
- `bubbles/textinput` has **no built-in history** — would need the same
  kind of manual slice-and-swap termlib implements by hand.
- `bubbles/textarea` handles multi-line editing; `bubbles/textinput` does
  not. Termlib's single widget does both (mode-switches on Ctrl+J) — a
  Charm version needs either two components with an explicit switch, or
  building multi-line on `textarea` alone and dropping `textinput`.
- `tea.ExecProcess` (`exec.go`) exists specifically to suspend a running
  Bubble Tea program, run an external command (raw terminal access), and
  resume on return — the correct primitive for `$EDITOR` handoff, and
  already the idiom other Charm-based tools use for this.
- There is no Charm equivalent of a single blocking `Prompt() (string,
  error)` call — Bubble Tea's unit of work is a `Model`/`Update`/`View`
  program that runs until it quits. Adopting it means *some* code has to
  become event-driven; the open question is how much.

## Scope options

**A. Widget-level swap (recommended for this release).** Keep
`terminal.go`'s outer loop exactly as it is. Replace `termlib.LineEditor`
with a small package (`replinput.go` or similar) that wraps a
`bubbletea.NewProgram` (no alt-screen) around `textinput`+`textarea`
internally, exposing the *same* narrow surface `terminal.go` already calls:
`Prompt(prompt string) (string, error)`, `AppendHistory`, `SetHistory`/
`History`, and a sentinel `ErrInterrupted`. Each call to `Prompt` starts,
runs, and quits its own Bubble Tea program synchronously, returning the
final line the same way termlib does today. History, Tab-completion
adapted to word-boundary matching, multi-line mode-switching, and
`$EDITOR` handoff (via `tea.ExecProcess`) all get reimplemented inside this
wrapper — genuinely equivalent behavior, Charm underneath, zero change to
the rest of `terminal.go`, `commands.go`, `ui.go`, or the tool-call paths.

**B. Full REPL rearchitecture.** Turn `terminal.go`'s `Run()` loop itself
into a single long-running Bubble Tea program — `/` dispatch, `!` shell
streaming, `@mention` switching, LLM streaming output, `promptAction`
Y/n boxes, and `ui.go`'s pickers all become `Update`/`View`/`Cmd` states
instead of synchronous function calls. This is the "real" Charm-native
harvey, and would let `ui.go`'s `SelectFrom`/`SelectItem` (already flagged
in `DECISIONS.md` as "promotion to termlib deferred until a clean
generalisation is proven") become genuine Bubble Tea components instead of
hand-rolled pickers. It is also a rewrite of most of the interactive
surface of a working tool, touching streaming output, session recording
hooks (`RecordTurnWithStats`, `RecordShellCommand`), and every place that
currently assumes synchronous, blocking I/O.

**Recommendation:** Option A for this release. It satisfies the actual
ask (drop `termlib`, land on Charm, matching `knowledge`'s precedent)
without combining a REPL rearchitecture with the other two release
workstreams (bug fixes, knowledge integration). Option B is real and
worth naming as a deliberate non-goal now rather than an oversight —
revisit it as its own effort once Option A has been live for a while.

## Constraints

- No local `replace` directive should be introduced for any `charmbracelet/*`
  module — same lesson as the stale `termlib` replace found 2026-09-15.
- `bubbles`/`bubbletea`/`lipgloss` versions should match what `knowledge`
  already pins (`bubbles v1.0.0`, `bubbletea v1.3.10`, `lipgloss v1.1.0`)
  unless a reason to diverge turns up — one Charm version across the
  workspace, not two.
- TDD per this repo's convention: `terminal_test.go`/a new
  `replinput_test.go` gets the failing tests first. Bubble Tea programs are
  normally tested via `teatest` (`github.com/charmbracelet/x/exp/teatest`)
  or by driving `Update` directly with synthetic `tea.KeyMsg`s — needs a
  quick spike to confirm which fits harvey's non-TTY test fallback path
  (termlib's own "stdin not a TTY → plain line reading" behavior, which
  harvey's tests currently rely on, needs an equivalent in the wrapper).
- Every exported symbol needs the `/** ... */` doc convention per
  `CLAUDE.md`.

## Open questions for Review

1. Confirm Option A (widget-level swap) as the release scope; Option B
   deferred explicitly, not silently dropped.
2. Package boundary: new file in the `harvey` package (matching how
   `ui.go`'s `SelectFrom` lives there today), or a small internal
   sub-package? `DECISIONS.md`'s prior note ("promotion to termlib is
   deferred until a clean generalisation is proven") argued against
   over-abstracting early — same logic likely applies here: keep it in
   `harvey` for now, no separate module.
3. Tab-completion semantics: reimplement termlib's word-boundary
   `Completer` behavior on top of `textinput`'s whole-value
   `SetSuggestions`, or accept a behavior change (completing the whole
   line instead of the current word)? The current `Completer` signature
   (`func(line string) []string`) is word-boundary-aware; preserving it
   means computing suggestions from the substring up to the cursor and
   only inserting the completed word, not calling `SetSuggestions`
   naively.
4. Does the multi-line mode-switch (Ctrl+J) live inside one wrapper
   `Model` that swaps between an internal `textinput`/`textarea`, or does
   `Prompt` itself dispatch to two different sub-programs depending on
   whether the buffer has gained a newline? The former keeps a single
   Bubble Tea program alive per call (matches termlib's continuous-editing
   feel); the latter is simpler but would lose in-place mode switching.
