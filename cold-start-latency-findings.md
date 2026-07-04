# Harvey — Cold-Start Latency Findings (input to design session)

**Status (2026-07-03):** Investigation complete for this round. Findings
only — design, decision, and implementation plan deferred to a follow-up
session. This document is the input to that session, not the output of it.

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
