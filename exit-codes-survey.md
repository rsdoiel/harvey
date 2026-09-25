# Harvey and assay exit codes — survey

Phase 1 of adopting the workspace exit-code convention (workspace DR-0003; the `kb`
implementation is knowledge DR-0047 to DR-0049) in `cmd/harvey` and `cmd/assay`.
Nothing here is decided. It is the survey the plan will rest on, plus the questions
that need an answer before the plan is written.

Measured 2026-09-25 with the v0.0.16 tree (`harvey 0.0.16 df39cfe`), each command
run in a scratch workspace with an isolated `HOME`, stdin from `/dev/null`, and
Ollama pointed at a closed port. The probe script is in the session scratchpad
(`hx/run.sh`), not in the repository.

## What exists today

| | `cmd/harvey` | `cmd/assay` |
|---|---|---|
| Source | `cmd/harvey/main.go`, 187 lines | `cmd/assay/main.go`, 1150 lines |
| Argument parsing | hand-written loop in `main` | standard `flag` package, then `flag.Parse` |
| `os.Exit` sites | 17 (12 `Exit(1)`, 5 `Exit(0)` for help, version, license and init) | 18 (16 `Exit(1)`, 2 `Exit(0)` for help and version) |
| Exit codes possible | 0 and 1 | 0, 1, and 2 (only from `flag` on an unknown flag) |
| Testable | no: everything is in `main` | no: same |
| `EXIT STATUS` in the manual | none | none |
| Library (`package harvey`) | 436 `fmt.Errorf`/`errors.New`, 2 sentinels (`ErrAutoArchived`, `ErrToolLoopExceeded`), no exit calls | shared |

Everything in the library returns a plain error to `main`, which prints it and exits 1.
There is no classification anywhere. Both binaries print human text only; there is no
`--json` mode, so the "class name beside the code" rule has nothing to attach to yet
(`assay`'s `results.json` is a report, not an error channel).

`harvey` is an interactive REPL. Slash commands inside it (`/read`, `/model use`, …)
have no exit status and are out of scope here. See "Not covered" below.

## Baseline: every case probed

"Now" is the measured exit code. "DR-0003" is the class the convention gives the
condition as described. Where the convention leaves a real choice, the case is marked
with the question number from the next section.

### `harvey`

| Case | Now | Convention | Note |
|---|---|---|---|
| `--version`, `--help`, `help`, `-l` | 0 | 0 | |
| `help nosuchtopic` | 1 | 2 | bad value on the command line |
| `--bogus` | 1 | 2 | usage |
| `-m` with no argument | 1 | 2 | usage |
| `init` with no argument | 1 | 2 | usage |
| `init /nonexistent` | 1 | 66 | named input missing |
| `init bad.yaml` (malformed) | 1 | 65 | content the tool reads is wrong |
| `-w /nonexistent` | 1 | 66 | today the message is about the working directory, not the path |
| `-w DIR` where cwd is not inside DIR | 1 | 2 | the workspace-boundary check; a bad `-w` value |
| `-w FILE` (a file, not a directory) | 1 | 2 or 66 | same message as the two above |
| `--replay /nonexistent` | 1 | 66 | today: "replay: no backend connected", the file is never checked |
| `--replay` a malformed file, no backend | 1 | 65 or 69 | same message; the backend is checked first |
| `--continue /nonexistent` | **0** | 66 | prints a warning and starts the session (Q3) |
| `--llamafile /nonexistent` | 1 | 66 | today the shell's error is embedded in "exited before server became ready" |
| `--record-file /proc/nope/x` | **0** | 73 | prints `✗ Auto-record failed: recorder: cannot create …` and carries on (Q3) |
| `harvey.yaml` malformed | **0** | 78 | prints `✗ harvey.yaml: yaml: …` and carries on with defaults (Q3) |
| No backend reachable, stdin closed | **0** | 69 | Q3 |
| `-m nosuch`, no server | **0** | 69 | Q3 |
| Session ends normally (`/exit`, Ctrl-D) | 0 | 0 | |
| A runtime error out of `Agent.Run` | 1 | by cause | see below |

`Agent.Run` returns an error from seven places: `initWorkspace` (wraps a workspace
error), `selectBackend`, `systemPromptExceedsContext`, `ReplayFromFountain` (twice)
and the prompt reader. Those need classifying at the source (Go rule in the
convention: sentinels or wrapped standard-library errors), not by matching text.

### `assay`

| Case | Now | Convention | Note |
|---|---|---|---|
| `--version`, `--help` | 0 | 0 | |
| `--bogus` | 2 | 2 | already right, by accident of `flag` |
| `--rag-compare` without `--rag-db` | 1 | 2 | four flag-combination checks, all usage |
| `--guide-compare` without `--guide-file` | 1 | 2 | |
| `--guide-compare` with `--rag-compare` | 1 | 2 | |
| `--llamafile` with `--llamacpp` | 1 | 2 | |
| `--corpus` missing | 1 | 66 | |
| `--corpus` malformed | 1 | 65 | |
| `--corpus` with no prompts | 1 | 1 or 65 | Q2 |
| `--category` matching nothing | 1 | 1 | a query that finds nothing; unchanged |
| Ollama unreachable while listing models | 1 | 69 | |
| `--llamacpp URL` unreachable | 1 | 69 | |
| Ollama up but no models | 1 | 1 | |
| `--llamafile` file missing | 1 | 66 | the same shell-error message as `harvey` |
| `--rag-db` cannot be opened | 1 | 66, 77 or 74 | depends on the cause; the driver error carries it |
| `--guide-file` missing | 1 | 66 | |
| `--output` cannot be created | 1 | 73 | `/proc/nope`, a file, and a read-only parent all give 1 |
| Every model call fails (server down) | **0** | 69 | each is recorded as an `ERROR` result; the run reports success (Q2) |
| A prompt's automatic checks fail | 0 | 0 or 1 | Q2 |
| `report.md` or `results.json` cannot be written | **0** | 74 | the error is printed, then the run ends 0 |
| `mkdir extracted` fails | **0** | 73 | printed, ignored |

## Findings that are bugs, not classification

1. **Closed stdin answers "yes".** `askYesNo` (`terminal.go`) ignores the read error, so
   end of input returns the default, and all five callers default to yes:
   `Start Ollama now?`, `Restart MODEL?`, and the three skill prompts `Compile now?`,
   `Run now?`, `Recompile?`. Observed live: with stdin closed, Harvey printed
   `Start Ollama now? [Y/n]  Starting Ollama...  ✓ Ollama started`. It is the same
   defect the `promptAction` fix of 2026-09-24 closed for the write prompt, in a second
   function. `TestDispatchSkill_RunNowIsNotAnsweredYesByEndOfInput` showed the worst case:
   with the input ending after "Compile now?", the freshly compiled script ran.
   **Fixed 2026-09-25** (Q4): end of input with no answer is now no, whatever the default;
   `ask_yes_no_test.go`, mutation-checked; the live closed-stdin run now declines to start
   Ollama.
2. **"Ollama started" may be reported when nothing new started.** The probe above used a
   closed port while an `ollama serve` was already running on the default port (started
   hours earlier, so not by the probe); Harvey still printed success. I did not trace why,
   only that the message does not tell you whether a server is now reachable at the URL
   you gave. Not an exit-code matter, but it is what a
   script would read.
3. **`--replay` checks the backend before the file**, so a missing or malformed replay file
   is reported as "no backend connected".
4. **`assay` reports success when every call failed** and when its own report could not be
   written. Both violate "do not exit 0 with failures".

## Not covered

- **Slash commands.** `echo '/read nosuchfile' | harvey` exits 0. A scripted session has
  no way to learn that a command failed. The convention is about the process's exit code;
  giving a piped session a status is a design question of its own (an exit status for the
  last failed command? for any?), and I propose leaving it out of this work.
- **A session that ends because the user interrupted it** (Ctrl-C at the prompt) exits 0
  today. The convention has no "interrupted" class; 130 is the shell's number for SIGINT
  and is above the range the convention allows tools to use. I propose leaving it 0.

## Questions to settle before a plan

**Q1. Where does the classifier live?** `kb` keeps it in `cmd/kb` because there is one
binary. Harvey has two that share the library. Proposal: an `exitcode.go` in the root
`harvey` package exporting the class type, one function mapping an error to its class
and code, and the sentinels the library needs (`ErrInvalid`, `ErrNotFound` first; more
only when a case demands one); each `main` only prints and exits. Alternative: two copies
in `cmd/`, which is what "one small type per tool" in the convention's Go paragraph reads
as, at the cost of drift between two binaries in one repo.

**Q2. What does `assay` count as failure?**
(a) A model call that errors is an operational failure; today it is recorded and the run
exits 0. Proposal: finish the run, write the report, then exit with the class of the first
failure (69 when the server was unreachable), per the bulk-command rule.
(b) A prompt whose automatic checks fail is a *result*, and a corpus run with failing
prompts is normal work. Proposal: exit 0, with a `--strict` flag later if wanted, not now.
(c) A corpus with no prompts: 1 (nothing to run, like the category case) or 65 (a corpus
that is empty is wrong content). Proposal: 65, since `--category` is the only thing that
makes "matches nothing" a query.

**Q3. Fail fast or warn and carry on, for inputs the interactive session can survive?**
Five cases today print a problem and start anyway: `--continue` missing, `--record-file`
uncreatable, `harvey.yaml` malformed, no backend reachable, and `-m` naming a model that
cannot be reached. In a terminal the user sees the warning and can fix it in-session, which
is why the code was written that way. In a script the exit status is the only signal.
Proposal: an input the user *named on the command line* (`--continue`, `--replay`,
`--record-file`, `--llamafile`) fails fast with 66 or 73 before the session starts. A
malformed `harvey.yaml` and an unreachable backend stay warnings in an interactive
session, and become 78 and 69 when the session is not interactive (stdin is not a
terminal, or `--replay` without `--replay-continue`). That keeps the tool usable at a
terminal and honest in a pipeline, at the cost of a rule with a condition in it.

**Q4. The `askYesNo` defect (finding 1): fix it first?** It is a bug independent of exit
codes and changes what a piped session can do. Proposal: fix it as its own first step,
red first, the way `promptAction` was, before any exit-code work touches `terminal.go`.

**Q5. Testability.** Neither `main` can be tested. `kb` solved this with `mainRun(args, out,
errOut) int`. Proposal: the same for both, so the enforcement test (every flag path with a
bogus flag, a missing argument, a surplus argument, and each documented class, never 70)
can run in-process. `harvey`'s hand-written argument loop is the larger change; it should
also start refusing a surplus positional argument, which it currently reports as
"Unknown flag".

## Decisions (RSDOIEL, 2026-09-25)

- **Q1:** the classifier lives in the library (`exitcode.go` in package `harvey`); both
  `main`s only print and exit.
- **Q2:** `assay` finishes the run and writes the report, then exits with the class of
  the first failed model call (69 when the server was unreachable). Failing automatic
  checks are results and exit 0; `--strict` is a later option, not part of this work. An
  empty corpus is 65; a `--category` matching nothing stays 1.
- **Q3:** an input named on the command line (`--continue`, `--replay`, `--record-file`,
  `--llamafile`) fails fast with 66 or 73 before the session starts. A malformed
  `harvey.yaml` and an unreachable backend stay warnings at a terminal and become 78 and
  69 when the session is not interactive (stdin not a terminal, or `--replay` without
  `--replay-continue`).
- **Q4:** the `askYesNo` defect is fixed first, on its own (done).
- **Q5** (not asked, taken as proposed unless you object): `mainRun(args, out, errOut) int`
  for both binaries, and a surplus positional argument to `harvey` becomes a usage error.

## Proposed shape of the plan

Each item red first, one commit per item, made only on request:

| Item | What |
|---|---|
| H0 | **Done.** `askYesNo` treats end of input as no (Q4) |
| H1 | Classifier and sentinels in the library (Q1); unclassified is 70 |
| H2 | `mainRun` for both binaries; `harvey` argument loop; usage sites |
| H3 | `harvey`: input sites (init, `-w`, replay, continue, record-file, llamafile), `Agent.Run` errors, Q3 |
| H4 | `assay`: usage, corpus, availability, output, bulk exit (Q2) |
| H5 | Enforcement test over every path; `EXIT STATUS` in both manuals; harvey `CLAUDE.md` |
| H6 | Old-versus-new run of both binaries on the v0.0.16 baseline (the `kb` harness is the model); harvey DR-0005 adopting workspace DR-0003, proposed |

Risk worth naming: H3 changes exit statuses that today are 0 (five cases above) and 1
(most of the rest). Anyone wrapping `harvey` in a script sees the change; the upgrade
notes need the same table the `kb` release carried.
