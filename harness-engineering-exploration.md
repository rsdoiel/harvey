# Harvey and Harness Engineering — Design Exploration

**Date**: 2026-07-12
**Status**: Exploration — not yet approved for implementation
**Related**: [harness-prerequisite-refactor-design.md](harness-prerequisite-refactor-design.md)
(scoped refactor cycle to run before implementing the directions below),
[small-model-budget-design.md](small-model-budget-design.md),
[cold-start-latency-findings.md](cold-start-latency-findings.md),
[llamacpp-cpu-tuning-design.md](llamacpp-cpu-tuning-design.md),
[llamafile-primary-design.md](llamafile-primary-design.md),
[chunked-analysis-design.md](chunked-analysis-design.md),
[plan-ivr-design.md](plan-ivr-design.md)

**Source material**: Birgitta Böckeler (Thoughtworks), "Viability of local
models for coding" (`../Viability-of-local-models-for-coding.txt`, Martin
Fowler site, 07 July 2026); "Harness Engineering for coding agent users"
(martinfowler.com/articles/harness-engineering.html); AI Native Dev
conference talk (`../coding-harnesses-talk-AI-Native-Dev.txt`); "Harness
engineering beyond skills: Using sensors to keep your coding agent in check"
— video deep-dive with Chris Ford (`../harness-engineering-beyond-skills.txt`);
user's own notes (`../notes-coding-harness.md`).

---

## Motivation

Böckeler's recent writing gives a vocabulary for something Harvey has been
doing piecemeal for a while without a shared name for it: **Agent = Model +
Harness**, where the harness splits into

- **Guides (feedforward)** — steer the agent *before* it acts: system
  prompts, `HARVEY.md`, skills, coding conventions, reference docs.
- **Sensors (feedback)** — let the agent observe consequences *after* it
  acts, ideally triggering self-correction before a human looks:
  linters, test suites, review agents, logs, browser screenshots.

Both sides further split into:

- **Inferential** — an LLM judging (guide: none of note; sensor: a code
  review agent). Runs on the GPU, probabilistic.
- **Computational** — a deterministic tool (guide: codemods, e.g.
  OpenRewrite; sensor: static analysis, linters). Runs on the CPU,
  reproducible.

Her strategic-placement principle: run sensors as cheaply and early as
possible (in-session, before commit); never let CI's pass/fail depend on an
*inferential* signal; batch expensive inferential sweeps (security,
modularity, dependency-freshness reviews) as periodic "garbage collection"
rather than every-turn checks.

The follow-up video deep-dive (with Chris Ford) adds a third axis, orthogonal
to guide/sensor and computational/inferential: **informational vs.
normative/judgment-embedded**. A sensor can report a fact without ruling on
it ("this dependency is 14 months old") or render a verdict itself
("dependency age exceeds threshold — fail"). Her own dependency-freshness
example combined both: a computational script gathers ages (informational),
then an LLM call applies judgment about what's actually worth upgrading
(inferential, normative). The distinction matters because it's the
judgment-embedded sensors that are safe to gate CI on; purely informational
ones need a human or an agent to interpret them before they mean anything.

This document audits Harvey against that framework, separates what already
fits from genuine gaps, and lays out exploratory directions — informed by
Harvey's actual hardware ceiling (Raspberry Pi 500, 16GB RAM, no dedicated
GPU), which changes the priority ordering from Böckeler's M3 Max / M5 Pro
context considerably: on that hardware, feedforward token cost is a
quality/attention tradeoff; on the Pi, it's directly minutes of wall-clock
time (see Hardware Reality Check below).

---

## Harvey audit against the framework

### Already fits the framework, working as intended

| Harvey mechanism | Böckeler category | Where |
|---|---|---|
| `HARVEY.md`, skills catalog, `agentPreamble` | Guide, informational, static | `config.go`, `skills.go` |
| RAG store, memory silos | Guide, informational, dynamic | `rag_support.go`, `memory_unified.go` |
| `groundingCheck` (ungrounded-quote detector) | Sensor, computational | `grounding.go`, called `terminal.go:1265` |
| `systemPromptExceedsContext` | **Meta-lint on the guide side** — see below | `context_estimator.go:158` |
| Memory auto-archive at confidence ≤ 0.2 | Sensor, computational, scheduled ("garbage collection") | `agents/memories/`, per `CLAUDE.md` |
| `/plan next` bounded-context step execution | Subagent-shaped feedforward budgeting | `plan_cmd.go:114` |
| `/read-chunks` map-reduce | Feedback-side content decomposition | `commands.go:1969`, `chunk_analyzer.go` |
| `@mention` routing with `recentHistory` window | Bounded-context remote dispatch | `routing.go:118` |

Two of these are worth calling out explicitly because they're easy to
undervalue:

- `systemPromptExceedsContext` is already "a linting approach on the
  feedforward side" — a deterministic rule run *before* submission,
  checking a known failure pattern (prompt too large for context) and
  failing fast with an actionable message rather than letting the model
  choke on a raw 400. It just isn't named that way anywhere in the
  codebase.
- The memory-confidence auto-archive is already Böckeler's "garbage
  collection" pattern — continuous rather than the weekly-batch cadence she
  describes, but structurally the same idea (periodic drift detection, not
  every-turn).

### Gaps identified

1. **Sensor signals report through three disconnected channels.**
   `e.Status.UpdateStatus()` (`tool_executor.go:118-133`) flashes on the
   spinner's line 2 and is erased by the next Lear-quote tick
   (`spinner.go:155-159`); `groundingCheck` (`terminal.go:1265`) prints a
   bare warning line after the spinner has already stopped; the
   prose-tool-call "unknown tool" warning (`terminal.go:1298`) is a third,
   separate print. None persist past the current redraw, and none
   distinguish computational (deterministic, high-confidence) from
   inferential (probabilistic) sensor findings — a distinction Böckeler is
   explicit shouldn't be conflated.

2. **Feedforward assembly is unconditional; the budget check happens after
   the fact.** `runChatTurn` (`terminal.go:1091-1109`) runs memory
   injection → `ragAugment` (always top-5 chunks, `rag_support.go:626`) →
   file injection → *then* appends to history → *then* computes the
   token-budget warning. `small-model-budget-design.md` already tracks this
   precisely (Direction 1/2, `BudgetTracker`) as a coordination gap between
   `ragMinScore`, `injectFileContext`'s byte cap, and history — this
   document doesn't re-litigate that, it reframes it: the fix is the same
   `BudgetTracker`, but it's also literally "extend the existing
   feedforward meta-lint (`systemPromptExceedsContext`) from static content
   to dynamic per-turn content," which gives the eventual implementation a
   single unifying principle rather than two independent efforts.

3. **Token-budget duplication.** The same percentage-and-warning logic is
   written three times: `terminal.go:1112-1129` (Ollama), `terminal.go:1130-1143`
   (llamafile), `commands.go:640-664` (`/status`, near-identical third copy).

4. **Tool-call dispatch triplication.** `tryExecuteApertusToolCalls`
   (`tool_executor.go:252`) and `tryExecuteProseToolCalls`
   (`tool_executor.go:291`) are near-byte-identical, differing only in the
   parse function.

5. **No computational guide/sensor tool tier at all.** Checked
   `builtin_tools.go` and `tool_registry.go`: no lint, formatter, codemod,
   spell-checker, or grammar tool is wired in as a first-class tool.
   Everything feedforward is markdown/skills (informational only);
   everything feedback is either the model's own judgment or the two
   computational sensors listed above. This is the largest structural gap
   relative to the framework, and — per the hardware numbers below —
   probably the highest-leverage one to close first.

---

## Hardware reality check

Two existing benchmark documents already answer questions this exploration
would otherwise have to guess at.

**Cold start, Pi 500 (BCM2712, 4×Cortex-A76, 16GB), post-prompt-trim**
(`cold-start-latency-findings.md`):

| Model | Cold start |
|---|---|
| OpenELM-3B | 3m20s |
| Qwen3.5-4B | 4m34s |
| Apertus-8B | 5m6s |
| Bonsai-8B (Q1_0) | 8m6s |

A prior session's memory note isolated *why*: a cold Bonsai-8B turn took
16m4s; the same warm server, second invocation, took 9.989s — a 96×
difference. The cost is `llama-server`'s prompt-processing of Harvey's
system prompt against the model, cached at the server-slot level as long as
the process stays warm and the prefix matches. It is not a llamafile-format
(APE/cosmopolitan) cost — llamafile and Harvey's own `backend_llamacpp.go`
path both ultimately run `llama-server` underneath, so the physics are
identical either way.

**Steady-state generation** (`llamacpp-cpu-tuning-design.md`): ~4.7 tok/s for
a 3B model at the correct thread count (cores − 1); BLAS backend choice
(OpenBLAS/BLIS/none) made no measurable difference at this model scale —
only thread-count discipline did (~15% win, already adopted).

**Implication for everything in this document:** on Böckeler's M3 Max/M5
Pro, an extra 500 tokens of feedforward content is a quality/attention
tradeoff. On the Pi, it's directly minutes of wall-clock prompt-processing
time on a not-yet-warm server. This reorders priority: budget-gating
feedforward content (gap 2 above) matters *more* here than in Böckeler's own
write-up, and a subagent handoff that uses a different, shorter system
prompt will not hit the calling agent's warm-prefix cache — each handoff
pays a fresh multi-minute prompt-processing tax on this hardware, regardless
of backend choice.

**Llamafile vs. llama.cpp/Ollama:** `llamafile-primary-design.md` already
made the strategic call — llamafile primary for zero-daemon-setup simplicity
(explicitly including Raspberry Pi), Ollama as the advanced/persistent-server
path. Nothing here revisits that. What's still open and falsifiable: for the
*subagent-swap* operation specifically (kill/relaunch vs. same-daemon
model-swap), does Ollama's in-process swap avoid the process-spawn overhead
on top of the (unavoidable) prompt-processing cost? Worth a small, dedicated
benchmark rather than a platform-wide decision either way.

---

## New exploratory directions

### Direction A — Computational guide/sensor tool tier

Add lint/format/spellcheck/codemod tools as builtin tools, not model
capabilities. Candidates already available on this workspace's toolchain:
`gofmt`/`go vet` (code), `hunspell` (spelling), a grammar/style checker —
LibreOffice reportedly ships one — for the scholarly-prose use case Harvey
also serves. These run in milliseconds on CPU and cost zero tokens — exactly
what small models are worst at holding onto reliably (consistent style,
spelling, import-boundary rules) and exactly what a deterministic tool does
without fail. Given the Pi's token economics, this is likely the single
highest-leverage addition in this document: it substitutes CPU-cheap
correctness for GPU-expensive (and unreliable) correctness.

The sensors deep-dive names specific failure modes she found AI models
reliably produce, which is a better prioritization signal than guessing:
max function arguments, cyclomatic complexity, max file/method length,
missing tests on a changed file, "TDD theater" (claims tests-first, never
actually runs them), and high coverage with no real assertions (mutation
testing surfaces this, but see Risks and Limits below — not viable in-session
on this hardware). Go equivalents worth prioritizing for Harvey specifically:
`go vet`, `staticcheck` or `gocyclo` (complexity), an unused-parameter check,
and a changed-file-without-changed-test heuristic (diff the file list against
`_test.go` coverage for the same package). She also notes curated ESLint rule
sets exist specifically targeting AI-generated-code failure modes, separate
from standard recommended presets — worth checking whether an equivalent
exists for Go/`golangci-lint` before hand-rolling rules.

Open question: expose these as tools the model calls voluntarily (requires
the model to remember they exist and choose to call them — the same
tool-calling-reliability problem the source article already flagged as weak
on small models), or run them unconditionally around every file write as a
harness-level sensor the model never has to invoke? The latter fits
Böckeler's placement principle ("wherever it's cheap to run a sensor, do
so") better and doesn't depend on small-model tool-calling reliability at
all.

### Direction B — Linting the feedforward side

Two distinct mechanisms, both extending existing code rather than
introducing new machinery:

1. **Content-aware guide selection.** Run a cheap static scan (e.g. an
   import-graph check) over the specific file(s) a request touches, *before*
   the prompt is built, to decide whether a given skill/convention is even
   relevant this turn — and if the file already violates the rule, inject
   the specific finding instead of the generic prose reminder. Example from
   the user's own notes: the backend-layers skill
   (`services -> clients + domain`) only matters for files under
   `./service/`; the same import scan that would power a
   `clients-no-services` sensor, run proactively, tells you whether to send
   that guide at all.
2. **Meta-lint on the constructed prompt itself.** Generalize
   `systemPromptExceedsContext` (currently static-content-only) to gate
   `ragAugment` and `injectOrChunk` output the same way, using
   `remainingContext()` *before* assembly rather than warning after. This is
   the same change as `small-model-budget-design.md` Direction 1/2's
   `BudgetTracker`, described here as one instance of a general principle:
   the harness should lint its own outgoing payload with the same rigor a
   linter applies to code, not just react to what didn't fit.
3. **Sensor messages as embedded micro-guides.** The sensors deep-dive's
   most reusable idea: a custom lint message doesn't just name the
   violation, it carries the self-correction instruction and the escape
   hatch at the point of failure — "if you judge this file doesn't need a
   type, suppress the warning and document why" or "you may raise this
   file's max-lines threshold, with justification, if it's only slightly
   over." This is more token-efficient than Direction A's plain tool output
   because the guidance is delivered exactly once, exactly when relevant,
   instead of sitting in the always-on system prompt on every turn "in case"
   it's needed. For Harvey this means: when Direction A's tools ship, their
   error strings should be written as actionable micro-guides (with an
   explicit override mechanism and a place to record the justification), not
   bare "rule X violated" text — the same discipline
   `systemPromptExceedsContext`'s error message already applies
   (`context_estimator.go:158`, "switch to a model with larger context, or
   shorten HARVEY.md...").

### Direction C — Unify the sensor-reporting channel

Introduce one small type — sketch, not a commitment:

```go
type SensorEvent struct {
    Kind     string // "tool_result", "grounding", "unknown_tool", ...
    Message  string
    Class    SensorClass // Computational | Inferential
}
```

Route `tool_executor.go`'s status updates, `groundingCheck`, and the
prose-tool-call warning through it, with the spinner (or a short trailing
status log beneath it) rendering `Computational` findings as higher-
confidence than `Inferential` ones. This directly answers the original
"reflect sensor state changes in the spinner" ask, and gives Direction A's
tools a natural place to report into once they exist.

The sensors deep-dive's "sidecar" pattern gives this a concrete shape worth
adopting directly: **two views of the same sensor state, not one.** She ran
her sensors continuously in parallel with (a) a human-facing dashboard
showing every sensor, its full state, and deltas over time, and (b) a
separate agent-optimized view — one command, returning *only the current
failures*, kept deliberately lean. Harvey has fragments of both today —
`/status` (`commands.go:619`) is closer to (a) but static (a snapshot on
request, no history/deltas); the spinner's `StatusReporter` is closer to
(b) but transient (nothing to query on demand, only a live stream). Neither
is quite either view. Once Direction A's tools exist, the natural design is:
a `SensorEvent` log the agent can query with one call ("what's currently
failing") entirely separate from whatever the human sees turn-to-turn — this
avoids ever putting full sensor output in the model's context by default,
which matters more on the Pi's token budget than it did in her experiment.

### Direction D — One subagent-dispatch primitive

`/plan next`'s bounded-context construction (`plan_cmd.go:114`),
`/read-chunks`'s map-reduce chunking, and `@mention`'s bounded
`recentHistory` + endpoint resolution (`routing.go:118`,
`routing.go:161`) are three independent implementations of the same idea:
build a small bounded context, run it against a (possibly different) model,
fold the result back in as a summary rather than a full transcript.
Consolidating them into one callable primitive is a simplification in its
own right and de-risks a general local-model-swap subagent mechanism, since
two of the three pieces already work in production.

Given the hardware numbers above, this primitive is only worth invoking for
coarse-grained tasks — a swap costs minutes, not seconds, so it needs an
explicit granularity judgment (matching Böckeler's own "reflect on task
complexity" point from the talk, sharpened here into a hard latency gate
rather than a quality nicety). And whatever UI represents the wait must
narrate it as such (see Direction C) — `/plan next` already prints
"Switching to X for this step...", but given real swap costs, it should say
something like "expect ~5 minutes" rather than leaving a bare spinner
running.

### Direction E — Test whether guides are still earning their tokens

The sensors deep-dive poses this as an open question rather than an answer,
and it's directly testable: **once a computational sensor reliably catches a
violation, does the corresponding always-on guide prose still need to be in
the system prompt every turn?** Her framing — "if the model gets it right
60% of the time without the guide, and the sensor catches the other 40%,
that combination might beat the guide alone" — is more consequential for
Harvey than for her, because every always-on guide token is a direct
addition to the cold-start prompt-processing cost documented above (minutes,
not a quality nicety). `cmd/assay/` already runs A/B corpus comparisons
(`--rag-compare`, per `CLAUDE.md`); the same harness could run a
`--guide-compare` variant — same corpus, once with a guide's prose included
in the system prompt and once with it stripped but the corresponding sensor
active — and measure whether outcomes actually differ. This gives Direction
A's tools a concrete payoff metric (tokens saved per guide successfully
replaced by a sensor) instead of assuming the substitution is free.

### Direction F — Harness-template skills

The deep-dive's closing idea: bundle guides *and* sensor scaffolding
together into an invokable unit per project archetype (a data-dashboard app
needs different sensors than a CRUD service or an event-processing module),
rather than skills being guide-prose-only. Harvey's skill system
(`skills.go`, `skill_wizard.go`) already has the invocation and discovery
mechanics; what's missing is a skill category that, on load, can also
provision sensor configuration (a lint ruleset, an import-boundary check)
into the workspace, not just inject markdown into the system prompt. This is
a natural extension once Direction A's tool tier exists — a template has
nothing to scaffold before then — so it's sequenced last, not because it's
low-value, but because it's downstream of the others.

---

## Risks and limits (from the sensors deep-dive)

Worth carrying into any implementation of Directions A-C, not just noting
here:

- **Signal-to-noise fatigue.** Activating several rules at once surfaces a
  wave of findings at once; some are valuable, some are noise that makes the
  code harder to read for no real benefit. Rules should likely be
  introduced incrementally, not as a bulk ruleset dump, so it's possible to
  tell which findings are worth keeping.
- **Contradictory sensors cause overcorrection.** Her own example: a
  max-lines sensor pushed the agent to over-fragment a file into many tiny
  subcomponents chasing the rule rather than improving the design. A sensor
  tier needs the same "is this actually useful" judgment a human would apply
  to a lint rule, not blind enforcement.
- **Illusion of quality.** An all-green sensor dashboard is not proof of
  correct behavior — coverage and lint passing doesn't mean the tests assert
  the right things, or that the feature does what was actually needed
  (a sensor cannot catch a wrong specification; that's a feedforward
  problem, not a feedback one).
- **Mutation testing is real signal but real expense.** It reliably finds
  "tests with no assertions" (high coverage, zero actual verification) that
  coverage alone misses — but works by repeatedly mutating code and rerunning
  the suite, which is heavy CPU work. Given the Pi's compute ceiling (already
  fully committed to model inference per the Hardware Reality Check above),
  this is a candidate for the periodic/scheduled "garbage collection" tier
  at most, never an in-session or per-turn sensor on this hardware.

### Ruled out — Go-level memory footprint tuning

Checked `backend_llamafile.go`, `backend_ollama.go`, `backend_llamacpp.go`,
`encoderfile_embedder.go`: Harvey never loads model weights in its own
process; every backend is a subprocess/daemon reached over HTTP, no cgo
bindings anywhere. Harvey's own Go process RSS is almost certainly tens of
MB against a multi-GB model process. `GOMEMLIMIT`/`GOGC` tuning would
optimize a cost that isn't meaningfully present. The actual memory lever is
what Harvey *chooses to hold* in `a.History` across a session (unbounded
file injections, RAG chunks) — which is Direction B / the existing
`small-model-budget-design.md` work, not a runtime-tuning initiative. Not
pursuing this separately.

---

## Open questions

- Direction A: tool-invoked-by-model vs. always-on harness-level sensor —
  needs a decision before implementation, not just a design.
- Direction B: does content-aware guide selection (item 1) risk *removing*
  a guide the model needed for an unrelated reason (e.g., a cross-cutting
  convention that isn't file-path-scoped)? Needs a concrete false-negative
  check before rollout.
- Direction D: the Ollama-daemon-swap vs. llamafile-relaunch benchmark
  described in the Hardware Reality Check section — not yet run.
- Should `SensorEvent` (Direction C) be introduced before or after Direction
  A ships, given Direction A is the first source of genuinely new sensor
  kinds? Sequencing affects how much of Direction C is speculative interface
  vs. driven by real cases.
- Direction E: what's the smallest `assay` change that lets a
  `--guide-compare` run happen without a large corpus-format rework? Worth
  scoping before committing to the idea.
- Which guides currently in `HARVEY.md`/skills are the best first candidates
  to test for removal under Direction E, once Direction A gives them a
  sensor counterpart?

## Suggested next step

Direction A (computational guide/sensor tools) has the best ROI-to-risk
ratio: zero token cost, no coordination with in-flight budget work, and
directly addresses the correctness gap small/local models are worst at on
this hardware. Recommend starting there, informed by whichever answer
Direction B needs, since a `run_linter` tool's output is exactly the kind of
finding Direction B's "specific violation instead of generic guide" idea
needs to exist. Direction E (the guide-deletion experiment) is the natural
follow-on once A exists — it turns "guides vs. sensors" from a design
opinion into a measured result specific to Harvey's own `HARVEY.md` and
skills.
