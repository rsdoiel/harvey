# Harvey — Cold-Start Latency Findings (input to design session)

**Status (2026-07-04):** Design/decision session held. Two items decided and
implemented; two more decided but not yet implemented (see "Decisions and
implementation" below).

---

## Background

`TODO.md` had an open research question: "How to improve cold starts with
models (reference Bonsia model slow warmup processing system prompt)." That
question grew out of live-testing `bonsai-8b` (see
`llamacpp-cpu-tuning-design.md`, Addendum 2), where a single cold-start turn
took 16m4.277s. The initial read was that this was a Q1_0-specific problem
(Bonsai's 1-bit kernel is unusually slow at prompt processing). The user
pushed back: is this only Q1_0, or does it happen with other models too? If
it's general, it needs fixing, not just documenting. This document reports
the test that answers that question.

## Method

Built one `.spmd` replay file with a single short turn ("Tell me in one
sentence what model you are and what base model you were built from. Then
compute 12 times 7 and show your work briefly."). Ran it live through
`harvey --replay` (not raw `llama-bench` — the real system prompt, the real
`--replay` code path) against four different llamafiles, each from a
genuinely cold server (no prior warm process on the port — confirmed by
process inspection each time, after one run was accidentally contaminated by
a leftover warm server from a previous test and had to be redone). Same
hardware throughout: the Raspberry Pi 500 (BCM2712, 4x Cortex-A76, 16GB)
used for all of today's benchmarking.

## Results

| Model | Params | Quantization | Cold-start (single turn) |
|---|---|---|---|
| Bonsai-8B | 8B | Q1_0 (1-bit) | 16m4.277s |
| Apertus-8B-Instruct-2509 | 8B | standard (not Q1_0) | 9m34.05s |
| Qwen3.5-4B-Q5_K_S | 4B | Q5_K_S | 6m25.229s |
| OpenELM-3B-Instruct | 3B | Q4_K_M | **failed** — context overflow |

The OpenELM failure: `request (3372 tokens) exceeds the available context
size (2048 tokens)`. OpenELM's native context (2048, an architecture limit,
not a config choice — see `henry/models/openelm-3b-instruct.yaml`) is
smaller than Harvey's own system prompt plus a single question. It never
got the chance to be slow; it couldn't run at all.

## Findings

1. **Not Q1_0-specific.** Cold-start time scales with parameter count across
   every model class tested (8B/8B/4B → 16m/9.5m/6.5m), consistent with
   Q1_0 just being unusually slow *on top of* a problem that exists
   regardless of quantization. Even the smallest model that could actually
   complete the test — Qwen3.5-4B, half the size of the two 8B models —
   still took 6.5 minutes. That is not an edge case confined to one exotic
   quantization; it's every size class tested being impractically slow on a
   first turn.

2. **The system prompt is 3372 tokens** (`agentPreamble` in `config.go` plus
   `HARVEY.md`), larger than an earlier rough estimate of ~1500-2000 tokens
   made from a chars/4 approximation. This is a second, independent problem
   layered on top of the speed one: any model with a native context at or
   below ~3372 tokens cannot run Harvey **at all**, regardless of how fast
   it is. OpenELM-3B (2048 ctx) is a concrete example already in this
   workspace's model library, not a hypothetical.

3. **This is strictly a cold-start cost, not a per-turn one** — already
   established in `llamacpp-cpu-tuning-design.md` Addendum 2 and
   reconfirmed today: llama-server's slot-based prompt cache persists the
   processed system-prompt prefix across separate Harvey invocations as
   long as the underlying server process stays warm. The numbers above are
   worst-case "first thing after selecting a model" costs, not a tax paid
   on every message.

4. **Secondary, non-speed observation:** of the three models that could
   complete the test, Qwen3.5-4B gave the most coherent self-identification
   answer ("I'm Harvey... not an AI language model built from any base
   model" — correctly playing the assigned role) versus Bonsai-8B and
   Apertus-8B both hallucinating specific wrong lineages ("built from the
   Ollama model", "built from a base model like Llama2"). Not central to
   the cold-start question, but worth keeping in view — it suggests this
   isn't a universal small/local-model failure mode, some models handle the
   system-prompt framing better than others.

## Candidate directions (not decided — for the design session)

None of these are committed; they're the shape of the solution space as of
this write-up.

1. **Trim the system prompt** (`agentPreamble` + review of `HARVEY.md`
   length). Addresses both the cold-start cost *and* the small-context
   compatibility wall (finding 2) in one move. Tradeoff: `agentPreamble`'s
   repetition of the auto-write-file rule (stated three times) may be
   deliberate reinforcement for weak models — trimming needs to preserve
   whatever compliance benefit that repetition buys, not just shrink token
   count blindly.

2. **Background pre-warm.** Fire a lightweight priming request to the
   backend immediately after model selection/connection, before the user
   finishes typing their first real message, so the system-prompt
   processing happens during idle "thinking" time instead of blocking the
   first real turn. Relies on finding 3 (cache persistence) already being
   true. Doesn't help the OpenELM-class compatibility wall at all — a model
   that can't hold the prompt still can't hold it, warm or cold.

3. **Investigate prompt-processing-specific performance knobs**, separate
   from the decode/generation tuning already shipped today (thread count,
   `PinCPU`, BLAS backend choice — all `tg`-focused). `pp` (prompt
   processing) is the actual bottleneck for cold start, and it wasn't the
   target of any tuning done so far this session. Candidates: batch size
   (`--batch-size`, default 2048), `ubatch` size, whether `-c` (context
   ceiling) itself adds allocation/bookkeeping overhead independent of
   actual prompt token count.

## Open questions for the design session

- What's an acceptable target cold-start time for Harvey on this class of
  hardware (Pi 500 / similar 4-core ARM boards)? Without a target, "trim
  the preamble" has no stopping point.
- Should Harvey detect up front that a model's native context can't hold
  the system prompt, and fail with a clear message before attempting the
  request — rather than the current raw `400 exceed_context_size_error`
  surfaced straight from `llama-server`?
- Is there a way to separate "pure model/weights loading time" from "system
  prompt processing time" in these numbers? All four measurements above
  bundle both; knowing the split would sharpen which candidate direction
  matters most.
- Does trimming `agentPreamble` risk regressing tool-call compliance on the
  weak/small models it was seemingly written for? Needs a compliance check,
  not just a token-count check, before cutting content.

## Addendum (2026-07-04): system-prompt composition audit

Follow-up analysis, done before the design session, to find out exactly what
the ~3372 measured tokens are made of — not just estimate it. Read the actual
code paths rather than assuming.

### Composition

The system prompt is built from three pieces, concatenated in this order:

1. **`agentPreamble`** (const in `config.go`) — 1087 chars.
2. **`HARVEY.md`** — loaded by `LoadHarveyMD()` in `config.go`, which does a
   bare `os.ReadFile("HARVEY.md")`. Size depends on which file is found (see
   below): the workspace-root `HARVEY.md` is 4005 chars; `harvey/HARVEY.md`
   is 1899 chars.
3. **Skills catalog** (`CatalogSystemPromptBlock` in `skills.go`, appended in
   `terminal.go` after `LoadHarveyMD()`) — scans `agents/skills/*/SKILL.md`
   and inlines every skill's full frontmatter `description:` plus its
   absolute filesystem path as XML. Currently 11 skills, 6212 chars — **the
   single largest of the three**, bigger than `agentPreamble` + `HARVEY.md`
   combined.

Chars/4 across all three (11304 chars) gives ~2826 tokens, undercounting the
measured 3372 — Harvey's own chars/4 heuristic (used elsewhere for the
`[ctx: %]` hint) is optimistic for this kind of dense, hyphenated/XML-tagged
technical text. At a more realistic ~3.3 chars/token, 11304/3.3 ≈ 3425,
consistent with the measured value. No unaccounted remainder.

**Ruled out:** the memory silo's always-injected `workspace_profile`/
`project_fact` records do not contribute — `memory.enabled` is not set in
`agents/harvey.yaml`, so `injectMemoryContext` (`harvey.go`) returns early
before ever calling `UnifiedMemory.Recall`.

### Bug: `HARVEY.md` loading bypasses the workspace boundary entirely

`cmd/harvey/main.go` calls `cfg.SystemPrompt = harvey.LoadHarveyMD()` at line
144, **before** `harvey.NewWorkspace(cfg.WorkDir)` runs at line 145.
`LoadHarveyMD()` never consults `cfg.WorkDir`/`-w` — it reads `"HARVEY.md"`
relative to the process's raw cwd, with no containment check, before the
`Workspace` struct (which is where Harvey's actual "operations stay inside
the root" security invariant lives, per its own doc comment) has even been
constructed.

Confirmed with the user this is *not* simply "should default to cwd" — that
part is correct and intentional (launching in `~/Laboratory` should load its
`HARVEY.md`; launching inside `~/Laboratory/henry` without `-w` should not).
The actual gap is narrower and more concrete:

- When `-w` is passed explicitly, there is currently **no check** that the
  process's cwd is contained within the given root. Passing `-w
  ~/Laboratory` while standing in an unrelated tree (e.g. `~/WorkLab/henry`)
  should fail fast with a clear error — it currently doesn't; `NewWorkspace`
  will happily set `Root` to whatever path it's given.
- Independent of that check, `LoadHarveyMD()` doesn't use `ws` at all, so
  even when `-w` *is* valid and honored for everything else (`agents/`,
  sessions, config), it has zero effect on which `HARVEY.md` loads. That's
  inconsistent with the rest of the workspace model.

**Consequence for this document's own numbers:** the "Method" section above
assumed the workspace-root `HARVEY.md` (4005 chars) was the one loaded during
the four cold-start test runs (`harvey -w /home/rsdoiel/Laboratory
--llamafile ...`). That was an assumption, not a verified fact — the actual
file loaded depends on the shell's cwd at invocation time, which was not
recorded. If cwd was `harvey/` (plausible; that's where the binary lives),
`harvey/HARVEY.md` (1899 chars) loaded instead. This is itself evidence of
the bug: the same command line can silently load different guidance
depending on invocation directory.

### Skills catalog: breadth is deliberate, per-entry cost is not

Raised the question directly with the user: given small-model context
constraints, should Harvey load fewer skills into the prompt? Answer: no —
`agents/` (skills, `knowledge.db`, memories) is deliberately designed to be a
shared, filesystem-legible substrate so *other* model/tooling systems can
also discover project skills and knowledge, not just whichever model Harvey
is currently running. Reducing which skills appear would work against that
goal, and the filesystem itself (any tool can read `agents/skills/*/SKILL.md`
directly) already satisfies cross-system discoverability without needing
full injection into any one session's live prompt.

The waste is representational, not coverage: each catalog entry inlines a
multi-sentence description **and** a full absolute path
(`/home/rsdoiel/Laboratory/agents/skills/<name>/SKILL.md`) that the LLM has
no use for — it only ever needs to type `` `/skill load <name>` ``. A
shorter per-entry form (name + one-line purpose, no path) would cut this
block substantially while still surfacing all 11 skills every session.

**Revised implication for the design session:** trimming `agentPreamble`
alone (candidate direction #1) only touches ~10% of the total. The
higher-leverage, and only currently *growing*, target is the skills catalog
— fix its representation (shorter entries, drop the path), not its breadth.

## Decisions and implementation (2026-07-04 design session)

1. **Workspace-boundary fix — decided and implemented.** Confirmed with the
   user that cwd-as-default-workspace is intentional; the actual gap was
   `-w`/`--workdir` having no containment check and `HARVEY.md` loading
   bypassing the `Workspace` boundary entirely (see Addendum above).
   Implemented: `Workspace.LoadHarveyMD()` (replaces the old package-level
   `LoadHarveyMD()`, now anchored to `ws.Root` via `ws.ReadFile`) and
   `RequireCWDInRoot(cwd, root)`, wired into `cmd/harvey/main.go` via
   `checkWorkDir()` — exits with a clear error when `-w` is passed explicitly
   and cwd isn't inside the declared root. `main()` reordered so the
   workspace is constructed before the system prompt loads. TDD-first;
   `TestWorkspaceLoadHarveyMD_*` and `TestRequireCWDInRoot_*` in
   `workspace_test.go`.

2. **Preflight context-size check — decided and implemented.** Detects when
   the system prompt alone leaves no room in the active model's context
   (the OpenELM-3B failure mode) and fails with an actionable message instead
   of the raw `400 exceed_context_size_error`. Implemented as
   `systemPromptTokenEstimate()` (pads the chars/4 heuristic 20%, per the
   composition-audit finding above that it undercounts this kind of text) and
   `systemPromptExceedsContext()` (pure decision function) in
   `context_estimator.go`, wired into `terminal.go`'s `Run()` right after
   backend selection. Tests: `TestSystemPromptTokenEstimate_*`,
   `TestSystemPromptExceedsContext_*`.

3. **Skills catalog format — decided and implemented.** Auto-truncate each
   entry's frontmatter `description:` to its first sentence (or ~80 chars)
   for the injected catalog block, and drop the absolute path entirely — the
   LLM only needs `` `/skill load <name>` ``. No `SKILL.md` edits required;
   breadth (all skills, every session) is preserved deliberately for
   cross-model/tooling discoverability via the filesystem. Implemented as
   `summarizeSkillDescription()` in `skills.go`, used by
   `CatalogSystemPromptBlock` in place of the raw description, with the
   `<location>` line removed. TDD-first; `TestSummarizeSkillDescription_*`
   and `TestCatalogSystemPromptBlock_noLocation` /
   `_truncatesLongDescription` in `skills_test.go`. Measured effect on the
   real 11-skill catalog: 6212 → 2024 chars (~1882 → ~613 estimated tokens at
   ~3.3 chars/token) — a ~1270-token cut while still listing all 11 skills
   every session.

4. **agentPreamble / HARVEY.md trim — decided and implemented.** Full
   rewrite for density (not just deduping the repeated auto-write rule).
   `agentPreamble` (`config.go`) folded its ~4 repeated mentions of the
   tag-to-write rule down to one mechanism sentence + one rules-list
   reminder; all five pre-existing `TestAgentPreamble_*` invariant tests
   (non-empty, no-fake-output phrasing, slash-command mentions, auto/tagged
   mentions, fence example) still pass unmodified against the new text —
   confirming the rewrite preserved every compliance-critical rule the
   tests guard. Both `HARVEY.md` files were rewritten the same way,
   dropping content that only duplicated `agentPreamble` (the tagged-block
   mechanism was fully re-explained in both, with two extra example fence
   blocks in the `harvey/` one) while keeping everything workspace-specific
   (PDF/image reading, `/kb` workflow, Fountain conventions, build/test
   commands).

   **Correction (2026-07-04):** the Addendum above states
   `harvey/HARVEY.md` was 1899 chars — that number was never actually
   measured, just estimated by eye, and was wrong. The real pre-rewrite
   size (`git show HEAD:HARVEY.md | wc -c`) was **4927 chars**, not 1899.
   Measured before/after for all three pieces:

   | File | Before | After | Change |
   |---|---|---|---|
   | `agentPreamble` (`config.go`) | 1087 | 1050 | −37 chars (−3%) |
   | `HARVEY.md` (Laboratory root) | 4033 | 2816 | −1217 chars (−30%) |
   | `HARVEY.md` (`harvey/`) | 4927 | 2069 | −2858 chars (−58%) |

   `agentPreamble`'s small delta confirms it was already fairly tight —
   most of its content is compliance-critical rules, not filler; the real
   density win was in the two `HARVEY.md` files, which had accumulated a
   full duplicate explanation of `agentPreamble`'s own mechanism.

All four items from the 2026-07-04 design session are now decided and
implemented.

## Re-run with the trimmed prompt (2026-07-04)

Re-ran the same single-turn cold-start test (see Reproduction below) against
all four models, using the rebuilt `bin/harvey` with all four fixes above
applied, to confirm the trim actually helped rather than assuming it from
the composition audit alone.

**Confirm no leftover server before every run.** Two of six runs in this
sweep were contaminated by a still-running llamafile server from the
immediately-prior test (same class of bug noted in the original Method
section) and had to be discarded and rerun:

- Qwen3.5-4B's first attempt actually ran on a leftover warm OpenELM-3B
  server (`"Server at http://localhost:8080 is serving
  'openelm-3b-instruct-Q4_K_M' (configured: 'Qwen3.5-4B-Q5_K_S') — adopting
  detected model"`) — caught from the log line, discarded, OpenELM process
  killed, Qwen rerun clean.
- Bonsai-8B's first attempt likewise ran on a leftover warm Apertus-8B
  server — same pattern, same fix.

**First clean Qwen3.5-4B result was itself an anomaly.** That rerun came
back at 14m28.434s — slower than yesterday's *pre-trim* 6m25.229s, which
doesn't fit: a smaller prompt cannot increase prompt-processing cost by any
mechanism Harvey controls. Checked for causes before drawing any
conclusion — no swap in use, no competing processes, temperature normal
(46°C) — found nothing conclusive. Re-ran a second time from a fully clean
state (no processes, load average settling) and got 4m33.972s, in line with
the other three models. Treating the 14m28s run as an unexplained one-off
(candidate causes: cold filesystem page-cache for the ~3GB model file,
general Pi run-to-run variance) rather than a real effect — flagging it
here instead of silently dropping it, per this doc's own no-silent-rewrite
convention.

**Final clean results:**

| Model | Before (2026-07-03) | After (2026-07-04, trimmed) | Change |
|---|---|---|---|
| Bonsai-8B (Q1_0) | 16m4.277s | 8m5.715s | −50% |
| Apertus-8B | 9m34.05s | 5m6.256s | −47% |
| Qwen3.5-4B | 6m25.229s | 4m33.972s | −29% |
| OpenELM-3B-Instruct | failed — context overflow, couldn't start | 3m20.517s, completes | now runs at all |

All four models improved. The three that could already run before are all
roughly 30-50% faster, consistent with the smaller prompt needing less
prefill/prompt-processing work. OpenELM-3B is the clearest single result:
it could not run *at all* before (3372 tokens > its 2048 native context),
and now completes, because the trimmed prompt (agentPreamble + skills
catalog cuts) brought total tokens under its limit. Its actual reply is
incoherent (garbled HTML-ish text) — a separate, pre-existing model-quality
issue for this weak/small model under Harvey's system-prompt framing, not a
sizing problem; noted here for completeness, not investigated further.

This closes the loop opened by finding 1 in "Findings" above (cold-start
time scales with parameter count, not just Q1_0/Bonsai) — the fix (prompt
trim) reduced that cost for every model class tested, and separately fixed
the compatibility wall from finding 2 for at least one real model in this
workspace's library.

### Assay as a future reference point (deferred)

Considered using `assay` (`cmd/assay/`) to A/B-test system-prompt variants
(full vs. trimmed) for tool-call/convention compliance before making changes.
Found it currently has no system-prompt injection at all — it sends raw
corpus prompts with no system message, so it can't yet answer that question.
Decided to **defer** building this out: trim the prompt first, let real
usage surface which compliance failures actually happen, and let those
concrete failure modes drive what `assay` needs (a `--system-prompt` flag,
a `harvey-conventions.yaml` corpus, a `--system-compare` delta report) rather
than guessing at checks upfront. Revisit once the trimming work has produced
real failure examples to design against.

## Reproduction

Test turn used for all four runs:

```
Tell me in one sentence what model you are and what base model you were
built from. Then compute 12 times 7 and show your work briefly.
```

Commands (per model):

```bash
harvey -w /home/rsdoiel/Laboratory \
  --llamafile <path-to-llamafile> \
  --replay <turn-file.spmd> \
  --replay-output <out.spmd>
```

Confirm no leftover warm server before each cold-start run — `ps aux | grep
llamafile`; a stale server on the configured URL gets silently adopted
instead of a fresh one starting, which happened once during this
investigation and had to be redone.
