# Adopting the workspace exit codes in `harvey` and `assay` — Implementation Plan

Implements the workspace convention (workspace DR-0003, `accepted`) in `cmd/harvey` and
`cmd/assay`. The facts and the decisions this plan rests on are in `exit-codes-survey.md`
(Q1 to Q5, taken 2026-09-25). `knowledge/exit-codes-plan.md` is the worked example for
the method; `knowledge/cmd/kb/exitcode.go` is the model for the classifier.

Work items H0 → H6. TDD-first: each item's tests are written and confirmed red before the
code changes. One commit per item, made only when RSDOIEL asks. Every new exported symbol
gets the `/** … */` block (description, parameters, returns, example). H0 is done.

The codes: 0 ok, 1 negative, 2 usage, 65 data, 66 no_input, 69 unavailable, 70 internal,
73 cant_create, 74 io, 75 temp_fail, 77 no_permission, 78 config. Unclassified is 70.

Neither binary has a `--json` mode, so the class name is not printed; the number is the
contract. If a machine-readable mode is added later, the class type already carries the
name.

---

## H0 — Closed stdin is not "yes" (done)

`askYesNo` returns false at end of input. `ask_yes_no_test.go`; committed `a50c2ca`.

---

## H1 — The classifier and the sentinels

**Add** `exitcode.go` in package `harvey`:

- `ExitClass{Name string; Code int}` and the twelve class values, exported
  (`ClassOK` … `ClassConfig`), because two packages under `cmd/` use them.
- `ClassedError`, wrapping an error with a class. `Error` and `Unwrap` forward, so a
  message reads as it did and `errors.Is`/`As` reach the cause.
- Constructors: `Usagef`, `Negativef`, `NotFoundf`, `Dataf`, `NoInputf`, `Unavailablef`,
  `CantCreatef`, `IOf`, `Configf`, and `ClassedAs(class, err)` for reclassifying what a
  helper returned (the outermost class wins).
- `ExitClassOf(err) (ExitClass, bool)` and `ExitCodeFor(err) ExitClass` (the class carries
  the code): `nil` is 0; a `*ClassedError` gives its
  class; then the standard-library rules from the convention (`fs.ErrNotExist` 66,
  `fs.ErrPermission` 77, `fs.ErrExist` 73, other `*fs.PathError` 74, `net.Error` and
  `*url.Error` 69 (checked by concrete type *before* the file errors, because a refused
  connection wraps a `*os.SyscallError`, and not by the `net.Error` interface first,
  because a bare `syscall.Errno` satisfies it), `context.DeadlineExceeded` 69, a SQLite busy or locked error 75, other
  SQLite constraint and corruption codes 65); anything else is `ClassInternal`.
- Library sentinels only where a case needs one: `ErrInvalid` (bad value, usage when it
  came from an argument, data when it came from a file) and `ErrNotFound`. More only when
  a caller must tell them apart.

**Not yet:** the knowledge library's own markers (`knowledge.ErrInvalid`, `ErrNotFound`,
`ErrInUse`, `ErrConflict`) exist from v0.0.14, and `go.mod` pins v0.0.13. Until the bump,
an error the knowledge library raises itself (as opposed to a SQLite or file error, which
classify by the rules above) reaches the classifier as a plain error and exits 70. The
startup paths that open the knowledge base fail on file and SQLite errors, so this
affects little; it goes in the bump's checklist.

**Tests, first:** `exitcode_test.go`. A table of error to class and code for every
standard-library rule; `Unwrap` and message preservation; `nil`; the outermost-class
rule; wrapping with `%w` still classifies; an unclassified plain error is 70 (this is the
mutation check: change the fallback to 1 and the test fails). No production caller yet.

**Commit boundary:** `exitcode.go`, `exitcode_test.go`.

---

## H2 — `mainRun`, the argument parsers, and the usage sites

Neither `main` can be tested, and every later item needs to run them. Split each into
`func mainRun(args []string, in io.Reader, out, errOut io.Writer) int` in the `cmd/`
package, with `main` calling it and `os.Exit`; `harvey`'s `--version`, `--help` and `help`
already write to a writer or stdout, so they move over unchanged.

**`cmd/harvey`:** the hand-written loop keeps its flags. Usage errors become
`Usagef`: an unknown flag; a flag missing its argument; `init` without a source;
`help TOPIC` with an unknown topic. A surplus positional argument (anything that is not
`init` or `help` and does not start with `-`) becomes "unexpected argument", not
"Unknown flag". `checkWorkDir` stops calling `os.Exit` and returns an error.

**`cmd/assay`:** it uses `flag`. Build a `flag.NewFlagSet` with `ContinueOnError` and
route its output to `errOut`, so `-bogus` returns an error `mainRun` maps to 2 instead of
`flag` exiting. The four flag-combination checks become `Usagef`. A surplus positional
argument (assay takes none) is refused.

**Tests, first:** `cmd/harvey/main_test.go` and `cmd/assay/main_test.go`. For each binary:
`--version` and `--help` exit 0 with output on `out`; a bogus flag, a missing flag
argument, a surplus argument, and (assay) each flag conflict exit 2 with nothing on `out`
and a message on `errOut`. These run in-process, so they need no build and no Ollama.

**Commit boundary:** the two `main.go` refactors and their tests. Behaviour outside the
usage cases must not change in this item; the tests above are the only new assertions.

---

## H3 — `harvey`: inputs, the session, and non-interactive runs

Classify at the source, then let `mainRun` map:

| Site | Class |
|---|---|
| `init` source path missing | `NoInputf` (66) |
| `init` source is malformed YAML | `Dataf` (65) |
| `-w` nonexistent or not a directory | `NoInputf` (66) |
| `-w` with the working directory outside it (`RequireCWDInRoot`) | `Usagef` (2) |
| `--replay FILE` missing | `NoInputf` (66), **checked before the backend** |
| `--replay FILE` malformed Fountain | `Dataf` (65) |
| `--continue FILE` missing | `NoInputf` (66), before the session starts |
| `--record-file PATH` cannot be created | `CantCreatef` (73), before the session starts |
| `--llamafile PATH` missing | `NoInputf` (66) |
| `--llamafile` present but the server will not start | `Unavailablef` (69) |
| Backend selection finds no model (`selectBackend`) | `Unavailablef` (69) |
| System prompt larger than the model's context (`systemPromptExceedsContext`) | `Dataf` (65) |
| `initWorkspace` errors | by cause through `fs` rules (66, 73, 74, 77) |

**Non-interactive** means stdin is not a terminal, or `--replay` without
`--replay-continue`. Interactivity comes from one function, `interactive(in)`, using
`golang.org/x/term`, so tests can inject it. Per Q3:

- A malformed `harvey.yaml` stays a printed warning at a terminal. When non-interactive,
  it is `Configf` (78) and the run does not start.
- No reachable backend stays "start anyway, offer `/model use`" at a terminal. When
  non-interactive it is 69.

A session that ends by `/exit`, Ctrl-D or Ctrl-C is 0.

`Agent.Run` keeps returning `error`; `mainRun` calls `ExitCodeFor`. The seven return
sites in `Run` get a class by cause as above; nothing matches message text.

**Tests, first:** in `cmd/harvey/main_test.go`, one case per row above, run through
`mainRun` in a temp workspace with Ollama pointed at a closed port and the `interactive`
function replaced. Plus: a normal `/exit` session ends 0; `--replay` of a missing file
exits 66 and does not print "no backend connected" (the ordering bug from the survey).
Mutation check: swap the `--replay` file check and backend check back and the test fails.

**Commit boundary:** the library sites and `cmd/harvey/main.go`. This is the item that
changes today's 0 to non-zero (five cases); the upgrade notes cover it.

---

## H4 — `assay`: inputs, availability, outputs, the bulk rule

| Site | Class |
|---|---|
| `--corpus` missing / malformed / no prompts | 66 / 65 / 65 |
| `--category` matching nothing | 1 (unchanged) |
| Ollama or llama.cpp unreachable while listing models | 69 |
| Ollama up, no models | 1 (unchanged) |
| `--llamafile` missing / will not start | 66 / 69 |
| `--rag-db` cannot be opened | by cause: 66, 77, 74 |
| `--guide-file` missing | 66 |
| `--output` cannot be created; `extracted/` cannot be created | 73 |
| `report.md` or `results.json` cannot be written | 74 |

**The bulk rule (Q2).** The run does everything, then exits with the class of the *first*
failure in a fixed order (in the order the failures occurred). A model call that errors is
recorded as an `ERROR` result as it is now, and remembered; if the server was unreachable
it is 69. A report that cannot be written is 74 and does not stop the JSON attempt. Failing
automatic checks are results and never change the exit code. The final line prints the
count of failed calls, so an exit of 69 is explained.

**Tests, first:** in `cmd/assay/main_test.go`, against a fake model server (an
`httptest` server speaking the endpoint `assay` uses): all calls fail, so the run
completes, writes both files, and exits 69; some calls fail, so the class of the first
one; a failing check with working calls exits 0 (mutation: make a failed check set the
exit and the test fails); an unwritable output directory exits 73; a report that cannot be
written exits 74 while the other file is still attempted.

**Commit boundary:** `cmd/assay/main.go` and its tests.

---

## H5 — Enforcement, and the manuals

**Enforcement.** Modelled on `kb`'s `verbcoverage_test.go`: derive the flag and
subcommand list from the help text or the flag set, and run every path with a bogus flag,
a missing argument and a surplus argument, asserting 2; assert that no probed
invocation exits 70. For `harvey` the paths are its flags plus `init` and `help`; for
`assay`, its flags. The test derives the list rather than restating it, so a new flag
that skips classification fails it.

**Manuals.** An `EXIT STATUS` section in `HelpText` (`harvey(1)`) and `AssayHelpText`
(`assay(1)`), in `helptext.go`, generated to `harvey.1.md` and `assay.1.md` the way the
other pages are. Each documents the codes the tool can return, the non-interactive rule
for `harvey`, and the bulk rule for `assay`. The existing `help_dispatch_test.go` pattern
checks the section exists.

**Docs.** `harvey/CLAUDE.md` gets a short "Exit codes" paragraph pointing at the
workspace convention and at `exitcode.go` as the worked example.

**Commit boundary:** the tests, `helptext.go`, the two generated man pages, `CLAUDE.md`.

---

## H6 — Old versus new, and the decision record

**Comparison.** As in `kb`'s X5: build the v0.0.16 tree (`df39cfe`) as the baseline and
the new tree, run the same commands against each in a fresh scratch workspace with an
isolated `HOME`, stdin from `/dev/null` and Ollama on a closed port, and diff exit code,
stdout and stderr. The survey's 41 rows are the seed set; add the enforcement test's
paths. Every changed exit code must be a row of this plan, and nothing may exit 70. The
harness lives in a new `scripts/` directory this time (kb learned that the scratch copy
is lost), with a README, and refuses to run from a directory not named as the workspace only if that turns
out to matter here. One trap from `kb`: the harness must not start a real `ollama serve`;
the closed-port setting and the fixed `askYesNo` keep it from doing so, and the run
checks `pgrep ollama` before and after.

**Record.** `harvey/decisions/0005-…`: adopts workspace DR-0003 for both binaries, with
the Q1 to Q3 choices and the non-interactive rule as its decisions; `proposed`, for you to
accept. It cites the survey and this plan rather than restating them.

**Upgrade notes** in `CHANGES.md` under an `## Unreleased` heading, with the table of
changed exit codes (the five 0-to-non-zero cases first). Version and dates are the
release's business, with the four codemeta fields.

---

## Order and risk

H1 → H2 → H3 → H4 → H5 → H6, with H3 and H4 independent of each other after H2.

- **H2 is the largest mechanical change**: `harvey`'s argument loop and `assay`'s `main`
  both move into functions. It changes no behaviour except the usage exit code, and the
  tests written first are what say so.
- **H3 is the risk to users.** Five cases that exit 0 today stop doing so when
  non-interactive or when a named input is bad. Anyone wrapping `harvey` sees it.
- **Testing `Agent.Run` paths** needs a backend-less agent and, for the `--llamafile`
  start failure, either a fake binary or a seam. If a seam is needed it is the smaller
  change: a function variable for the start call, as `listLocalModels` is for `/model use`.
- **Out of scope, and worth a later item:** a status for scripted sessions (`echo /read x |
  harvey`), and a `--json` error mode. Both are in the survey's "Not covered".
- **The knowledge bump** (v0.0.14) adds its markers to the classifier's table; do it after
  H1 and before H3 if the tag exists by then, otherwise as a follow-up.
