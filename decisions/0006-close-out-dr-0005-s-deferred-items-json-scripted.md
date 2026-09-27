---
id: "0006"
title: "Close out DR-0005's deferred items: --json, scripted exit codes, ThoroughProbeModel, termlib"
date: "2026-09-27"
status: accepted
kind: decision
trigger: request
project: harvey
phase: ""
supersedes: []
superseded_by: []
relates_to: []
initiative: ""
session: ""
decisions: []
tags: []
uuid: "01a0e346-8ede-77d5-82b5-b5c5d6a35b85"
origin_host: "wren"
---

**Context.**

DR-0005 adopted the workspace exit-code convention for `harvey` and `assay` but
explicitly left two things out of scope: a `--json` error mode, and an exit status for a
scripted session's individual `/` commands (`echo /read x | harvey` stayed exit 0
regardless). `exit-codes-plan.md`'s "Order and risk" section named both as later items.
Separately, `TODO.md` had two long-standing loose ends: `github.com/rsdoiel/termlib` had
no `v0.0.10` tag though `main` was a commit past `v0.0.9`, and `ThoroughProbeModel`
(`ollama.go`) had no caller anywhere (DR-0004 noted this as a known, deliberate gap, not
a bug). RSDOIEL asked for all four to be closed out together.

**Decision.**

1. **termlib**: `go.mod` now requires the pseudo-version at `354195d` (the commit past
   `v0.0.9`, a wide/multi-row prompt fix that does not affect harvey's one-row prompt).
   Cutting an actual `termlib v0.0.10` tag is left as a separate, optional step — a
   pseudo-version needs no release on another repo to unblock this one.
2. **`ThoroughProbeModel`**: wired into `/rag setup`'s embedder auto-pick only
   (`confirmEmbedder` in `commands_rag.go`), not into every Ollama model selection. The
   keyword-based guess `/rag setup` already made is now confirmed — or, if it is wrong,
   corrected — with one live `/api/embed` call before the store is committed to it; a
   probe failure (server unreachable, nothing confirms) never blocks setup, the same rule
   `FastProbeModel`'s own failures already follow. `setOllamaModel`-wide wiring was
   rejected (below).
3. **`--json`**: `harvey.PrintJSONError` and `harvey.ExtractJSONFlag` (`exitcode.go`) give
   both binaries the same `{"error","class","code"}` envelope `kb --json` already uses.
   `-json`/`--json` is recognised wherever it sits on the command line, before either
   binary's own argument parsing, so it still applies when a later flag — or the `flag`
   package's own parse, for `assay` — is what fails. Scope is startup/flag-parsing errors
   (and, for `harvey`, a non-interactive session's own final result, since that already
   flows through the same return path — see item 4); a REPL's interactive output, and
   `assay`'s per-prompt/report output, stay text-only.
4. **Scripted exit codes**: a non-interactive `harvey` session (`interactiveSession` false
   — stdin not a terminal, or `--replay` without `--replay-continue`) now tracks the first
   slash-command or chat-turn error and returns it once the session ends normally
   (`keepFirstFailure` in `terminal.go`), instead of always exiting 0. An interactive
   session is unaffected — a typo at a terminal is something the person just tries again,
   not a reason to change the exit status. `plan_cmd.go`'s two unclassified plain errors
   (`"no backend connected"`, `"no plan found"`) were reclassified (`Unavailablef`,
   `Negativef`) as the first case this makes visible.

**Rationale.**

`--json` mirroring `kb`'s exact envelope shape means a script that already parses one
tool's `--json` errors parses all three (`kb`, `harvey`, `assay`) the same way. Detecting
`-json`/`--json` before each binary's own parsing — rather than registering it as an
ordinary flag — is what lets it apply even when the flag that actually fails comes later
on the line, or when `assay`'s `flag.FlagSet` itself is what rejects the line. Scoping the
scripted-exit-code work to plumbing plus one command family (`/plan`) rather than
auditing every handler keeps this record honest about what it delivers: many command
handlers still print a failure and return `nil`, or return an unclassified `fmt.Errorf`,
and neither is fixed by this record.

**Rejected alternatives.**

- *Tag `termlib v0.0.10` now.* A tag is a public release on a repository this record's
  author does not own the decision to publish; the pseudo-version needs nothing from
  `termlib` to unblock `harvey`, so tagging is left for RSDOIEL, on their own schedule.
- *Wire `ThoroughProbeModel` into every `setOllamaModel` selection*, replacing
  `FastProbeModel` outright. Rejected in the same AskUserQuestion turn this record
  reports: it adds a live `/api/embed` request to every model switch, including the vast
  majority that never touch RAG or embedding at all, for a signal only `/rag setup`
  currently consumes (`commands_rag.go:404`).
- *Give `--json` a role in `assay`'s per-prompt/report output*, not just startup errors.
  Rejected as broader than DR-0005's own scope note intended; `report.md`/`results.json`
  already are `assay`'s machine-readable output.
- *Exit on the status of the last scripted command only*, not the first failure. Rejected
  to match the bulk-command rule the workspace convention already states and `assay`
  already follows: run everything, then exit with the class of the first failure, so a
  later command's silent success cannot paper over an earlier one's failure.
- *Audit and reclassify every command handler's error return in this pass.* Real
  candidates (`/read`, `/read-dir`, an unknown command name, most of `commands_rag.go`/
  `commands_skill.go`) still print-and-swallow or return an unclassified error. Out of
  scope here — named as a follow-up, not silently left implied-done.

**Consequences.**

A scripted session's exit code is now only as informative as the command it ran: `/plan`
gives real signal (69/1/65 instead of always 0 or an unclassified 70); most other command
families do not yet, and will keep exiting 0 on a printed failure, or 70 on a returned-
but-unclassified one, until each is worked through the same way. `MODEL_CACHE.md`'s
"the thorough probe is not wired in" line no longer holds without qualification — it is
wired in exactly one place. `--json`'s `harvey` scope also covers a non-interactive
session's own final result "for free," since `mainRun` never distinguishes a
flag-parsing error from `Agent.Run`'s return value — this was not separately requested
but follows directly from item 4 landing in the same session and sharing `mainRun`'s one
return path. Tests: `exitcode_test.go` (`PrintJSONError`, `ExtractJSONFlag`),
`cmd/harvey/main_test.go` and `cmd/assay/main_test.go` (`--json` end to end),
`terminal_scripted_test.go` (`keepFirstFailure`, the interactive/non-interactive split),
`commands_rag_thorough_test.go` (`confirmEmbedder`).

Status `proposed`: promotion is the author's call.
