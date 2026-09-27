---
id: "0005"
title: "Adopt the workspace exit-code convention for harvey and assay"
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
uuid: "01a0e31b-b180-7511-9b12-8204bf9897e3"
origin_host: "wren"
---

**Context.**

`cmd/harvey` and `cmd/assay` exited only 0 or 1. Workspace DR-0003 (Laboratory root
`CLAUDE.md`, "Exit codes for command-line tools") sets a shared table — 0 ok, 1 negative,
2 usage, 65 data, 66 no_input, 69 unavailable, 70 internal, 73 cant_create, 74 io, 75
temp_fail, 77 no_permission, 78 config — so a script or an embedding tool can tell what
went wrong from the number alone, the way `kb` already does (knowledge DR-0047). `exit-codes-survey.md`
(41 baseline rows, two binaries sharing one library) and `exit-codes-plan.md` (H0-H6) did
the design work; this record adopts the decisions that survey settled on 2026-09-25.

**Decision.**

1. **Classifier lives in the library**, not per-binary: `exitcode.go` in package `harvey`
   exports the class type, constructors (`Usagef`, `NoInputf`, `Dataf`, ...) and sentinels
   (`ErrNotFound`, `ErrInvalid`); `ExitCodeFor(err)` maps an error to its class and code.
   Each `main` is a thin `os.Exit(mainRun(args, out, errOut))` — neither binary decides its
   own exit status inline. An error nothing classified is 70, never 1.
2. **`assay` finishes its run and writes the report**, then exits with the class of the
   first failed model call (69 when the server was unreachable) — never exits 0 with
   failures. A failing automatic check on a prompt is a *result*, not a tool failure, and
   exits 0; `--strict` is a later option, out of this work. An empty corpus is 65 (wrong
   content); a `--category` that matches nothing stays 1 (a query that finds nothing).
3. **Non-interactive sessions fail fast on named inputs.** An input the user names on the
   command line (`--continue`, `--replay`, `--record-file`, `--llamafile`) fails before the
   session starts, with 66 or 73. A malformed `harvey.yaml` and an unreachable backend stay
   warnings at an interactive terminal (the user can fix them in-session) and become
   failures — 78 and 69 — when the session is non-interactive: stdin is not a terminal, or
   `--replay` is given without `--replay-continue`.
4. **`askYesNo` treats end of input as no**, fixed first and independently of the rest
   (H0), because it changes what a piped/non-interactive session can do and every other
   item depends on non-interactive behaviour being honest.
5. **`flagSpecs` (harvey) and `defineAssayFlags` (assay)** are each binary's single list of
   options; `flags_test.go` compares them against the manual's OPTIONS section, and the
   `EXIT STATUS` sections of `HelpText`/`AssayHelpText` list the codes each tool can
   actually return, both enforced by test. A surplus positional argument to `harvey`
   (previously "Unknown flag") is now a usage error (2).
6. **Verified old-versus-new** against the v0.0.16 baseline (`df39cfe`) with the same
   harness pattern as `kb`'s X5: both trees built, run against the survey's 41 rows plus
   the enforcement test's paths, in a scratch workspace with an isolated `HOME`, stdin from
   `/dev/null`, and Ollama on a closed port — checking `pgrep ollama` before and after so
   the harness itself never starts a real server. Every changed exit code is accounted for
   and nothing exits 70.

**Rationale.**

One classifier shared by two binaries in one repo avoids drift between two copies of the
same table. Fail-fast on named inputs keeps a script honest without breaking interactive
use, where the existing warn-and-continue behaviour is what the code was written for.
Finishing `assay`'s run before reporting failure preserves the report as a record of what
was attempted, per the workspace convention's bulk-command rule ("finish everything, then
exit with the class of the first failure").

**Rejected alternatives.**

- *Two independent classifiers, one per `cmd/`.* Matches "one small type per tool" in the
  convention's Go paragraph literally, at the cost of drift between `harvey` and `assay`
  as codes are added — rejected since both share the same library and the same table.
- *Fail fast on `harvey.yaml`/backend problems even at an interactive terminal.* Breaks the
  case the warn-and-continue code was written for (a user who can fix it in-session);
  rejected in favour of the interactive/non-interactive split.
- *`assay` exits 0 whenever the report is written, regardless of call failures.* What the
  tool did before this record; rejected because it violates "do not exit 0 with failures"
  and hides a fully-down backend behind a green exit code.
- *A `--strict` flag for `assay`'s failing checks now, not later.* Deferred: a corpus run
  with failing prompts is normal evaluation work, and the flag has no design yet.

**Consequences.**

Anyone wrapping `harvey` or `assay` in a script sees exit codes change: five cases that
exited 0 before (closed stdin at a yes/no prompt; a bad `--continue`, `--replay`,
`--record-file`, or `--llamafile` argument; a malformed `harvey.yaml` or unreachable
backend in a non-interactive run) now exit non-zero, and most of the remaining 1s split
across the fuller table. `CHANGES.md`'s `## Unreleased` section carries the upgrade table.
Testing `Agent.Run` paths needed a backend-less agent and a function-variable seam for the
`--llamafile` start call, mirroring the existing seam for `/model use`'s `listLocalModels`.
Out of scope, left for later: a status for a scripted session's individual `/` commands
(`echo /read x | harvey` still exits 0), and a `--json` error mode.

Status `proposed`: promotion is the author's call.
