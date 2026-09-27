# scripts

## compare-exit-codes.py

Compares two builds of `harvey` and `assay`, an older release and the tree under test,
on the baseline table `exit-codes-survey.md` measured. It is H6 of `exit-codes-plan.md`,
the check `harvey/decisions/0005-*.md` cites: a release that changes what `harvey`/`assay`
say to a script should be run against the last release, with a wide set of failures,
before it is tagged. Modelled on `knowledge`'s `scripts/compare-exit-codes.py`
(DR-0040/DR-0049), simplified because there are two fixed-flag binaries here instead of
one binary with a verb `SYNOPSIS` to walk.

```bash
go build -o bin/harvey ./cmd/harvey && go build -o bin/assay ./cmd/assay
# an older release, however you have it (a release zip, or built from a tag):
uv run scripts/compare-exit-codes.py --base ~/tmp/harvey-v0.0.16 --new bin
```

What it does, and what to read in its report:

- Every command runs against both `--base` and `--new`, in a fresh copy of a scratch
  workspace per run: an isolated `HOME`, stdin from `/dev/null`, and Ollama/llama.cpp
  pointed at a closed local port, so no run can reach a real backend and no run sees
  another's changes.
- The command list is `exit-codes-survey.md`'s baseline table (`### harvey`, `### assay`)
  translated into arguments and fixtures — not a synopsis walk, since neither binary has
  one worth generating from.
- The report lists each command whose exit code changed (old to new), then differences in
  stdout/stderr at an unchanged code, then any exit 70 (an error nothing classified),
  timeout, or missing binary in the new build. It exits 1 on any of those, so it can gate
  a release.
- A short "not covered" footer lists the handful of survey rows that need a live model
  response or a running Ollama with zero registered models — not reproducible without a
  real backend, so left for the reviewer to check by reading the code (the exact lines are
  named in the script).
- **Reading the changed codes against `harvey/decisions/0005-*.md` is your job.** Nothing
  here decides a change is right. Expect every "was 0, now non-zero" row from the survey
  and DR-0005; anything else needs an explanation.

Options: `--work DIR` keeps the scratch files (the Laboratory convention is
`~/Laboratory/tmp`), `--timeout N` changes the per-command limit (default 15s). Standard
library only; run it with `uv run`.

**The one trap this harness exists to avoid**, the same one `knowledge`'s copy names: a
bad flag or an unclosed stdin must never start a real `ollama serve` or `llama-server`.
The port handed to `--ollama`/`--llamacpp` is opened and immediately closed, so the
connection is always refused; the run checks `pgrep -x ollama` before and after and warns
if the process set changed.
