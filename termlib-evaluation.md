# termlib evaluation (gate on tagging v0.0.16)

Written 2026-09-24. `TODO.md` asks for an *evaluation* of whether `github.com/rsdoiel/termlib`
should be replaced or kept, before v0.0.16 is tagged. This records the facts and the options. It
does not decide.

**A correction to `repl-charm-migration-design.md`.** That brief says the user "has decided" that
v0.0.16 drops termlib for Charm. `TODO.md` says the opposite question is open ("determine whether
termlib's surface can be replaced ... or should stay"). The brief overstated it, and its Option A
was written as a recommendation to review, not an agreed scope.

## Facts (each checked in the code or module, 2026-09-24)

- **Harvey's whole use of termlib is 6 lines in `terminal.go`**: `NewLineEditor(os.Stdin, out)`,
  `le.Prompt(a.prompt())` once per turn, `ErrInterrupted`, `Completer`, and
  `AppendHistory`/`SetHistory`/`History` in `loadCmdHistory`/`saveCmdHistory`. Nothing else imports it.
- **termlib is the author's own module**, `~/Laboratory/termlib`, ~2650 lines including tests, one
  dependency (`golang.org/x/term`). Harvey is its only consumer anywhere in the Laboratory.
- **Tagged `v0.0.9`; `main` is one commit ahead** (`354195d`, "fix prompt and input bug": a prompt
  wider than the terminal, or containing a newline, corrupted the redraw). Harvey's prompt is one
  short row (`harvey [unsafe] > `), so this fix does not affect it today.
- **The trigger was not a functional problem.** The 2026-09-15 request followed the discovery of a
  stale `replace github.com/rsdoiel/termlib => ../termlib` in `go.mod`. That is fixed (`go.mod`
  requires `v0.0.9` cleanly, the build passes).
- **Charm is not in Harvey's build today.** Harvey's binary imports `x/term` only. Adopting
  `bubbletea`/`bubbles`/`lipgloss` adds about **30 third-party packages** (measured as the packages
  `knowledge`'s `kb` imports that Harvey does not: `charmbracelet/{bubbles,x}`, `muesli/{ansi,termenv,
  cancelreader}`, `mattn/go-runewidth`, `rivo/uniseg`, `sahilm/fuzzy`, `xo/terminfo`). `kb` already
  takes that cost, so it is a precedent, not a new kind of dependency for the Laboratory.
- **Charm has no equivalent of a blocking `Prompt() (string, error)`.** History, word-boundary Tab
  completion, Ctrl+J multi-line and the `$EDITOR` handoff would be rebuilt inside a wrapper
  (`repl-charm-migration-design.md`, Option A). Harvey's tests rely on termlib's non-TTY fallback
  (plain line reading); the wrapper would need its own.
- **Not measured:** how the two feel to use. No behavior comparison was run.

## Options

1. **Keep termlib for v0.0.16.** Tag `termlib` v0.0.10 (it has the unreleased fix), bump the
   `go.mod` require, and treat Charm as its own later effort. Nothing changes in the REPL.
   Cost: none now. Risk: none new. Leaves the "one input stack across the Laboratory" question open.
2. **Vendor termlib's line editor into Harvey** (copy ~660 lines into the `harvey` package, keep
   `x/term`). Removes the separate module and any release coupling, changes no behavior. Cost: Harvey
   owns the code and termlib loses its only consumer.
3. **Widget-level swap to Charm** (the brief's Option A). Replaces `termlib.LineEditor` with a
   wrapper around `textinput`/`textarea`, same narrow surface. Cost: ~30 new packages and a rebuild
   of history, completion, multi-line and editor handoff, on the one component every keystroke goes
   through. Risk: input regressions in the release that also ships the learning mode.
4. **Full REPL rearchitecture on Bubble Tea** (the brief's Option B). A rewrite of most of the
   interactive surface: streaming, `!` output, recording hooks, every blocking prompt. A separate
   project, not a release item.

## Recommendation

**Option 1 for v0.0.16**, and take Charm as a deliberate follow-up once the learning mode has been
in use. The evaluation the gate asks for has a clear answer: termlib is small, owned, tested, has
no functional defect affecting Harvey, and the only reason it was questioned (the stale `replace`)
is fixed. Replacing the input layer in the same release as the largest feature since v0.0.15 puts the
risk where a regression is most visible and hardest to unit-test. If the goal is one input stack
across the Laboratory, that is a good reason to do Option 3, but it is a reason about `kb`/Harvey
consistency, and it can wait for its own release.

Whichever is chosen, the decision belongs in `decisions/` as a record (DR-0003), authored `proposed`.
