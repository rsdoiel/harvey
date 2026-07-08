
# Action Items

## Update Next

- [ ] Remove the prompt to remove previous session at startup (we have a `-resume` and `/resume` option if needed)

## Bugs

- [ ] I have both Llamafile and gguf models in ~/Models on my Mac, bit the gguf models are not listed as an option (llama.cpp is installed)

- [x] Chunk prompt never triggered for Gemma4-E4B — root cause found and fixed
  2026-07-05, see [DECISIONS.md](DECISIONS.md) (2026-07-05 — Chunking guard fix).
  Two bugs: `remainingContext()` returning 0 for "unknown limit" was treated the
  same as "skip the guard" in `builtin_tools.go`'s `read_file` (now falls back
  to a 4096-token budget, matching `file_inject.go`); and `adoptExternalServer`
  never probed context length for llamafile models adopted from an
  already-running server, so `effectiveContextLimit()` stayed 0 for the whole
  session. Tests: `TestReadFile_ChunkingEnabledContextLimitUnknown`,
  `TestAdoptExternalServer_probesContextLength`.

- [x] Llamafile GPULayers defaulted to 99 (maximise GPU) on every platform,
  including Raspberry Pi hardware with no usable GPU-compute backend. This is
  the actual explanation for the `bonsai-8b` (Q1_0) retest below appearing to
  hang for 20+ minutes — the underlying `llama-server` process was still
  running after 2+ hours of CPU time. Fixed 2026-07-05: default changed to 0
  (CPU-only), matching `LlamaCppConfig.GPULayers`'s existing default. See
  DECISIONS.md 2026-07-05 entry. Tests:
  `TestDefaultConfig_LlamafileGPULayersDefaultsToZero`,
  `TestSaveLlamafileConfig_DoesNotPersistDefaultGPULayers`,
  `TestSaveLlamafileConfig_PersistsCustomGPULayers`.

- [x] Added `/resume [FILE]` slash command as a thin alias for `/session use`
  (same picker/load behavior, more discoverable name matching `--resume`).
  See DECISIONS.md 2026-07-05 entry. Tests: `TestCmdResume_aliasForSessionUse`,
  `TestCmdResume_noArgsShowsPicker`.

- [x] Chunk-quality retest against the actual Gemma4-E4B model — RESOLVED
  2026-07-05. Downloaded `gemma-4-E4B-it-Q5_K_M.llamafile` (7.4GB) from
  huggingface.co/mozilla-ai/llamafile_0.10 (no longer dependent on the
  `henry` build pipeline). Ran `/read-chunks natural_language_programming.md
  --chunk-size 800 --max-chunks 20 [topic-drift instruction]` — 23 chunks,
  stopped after 4 completed (user time constraints). All 4 chunks were
  coherent, on-topic, and did genuinely useful paragraph-level drift
  analysis — a stark contrast to the original garbled-token bug report.
  **Conclusion: the map-reduce chunking approach itself is sound.** The
  original hallucination was entirely explained by the chunk-prompt guard
  never firing (TODO items above), not model coherence collapse under the
  chunking prompt. Per-chunk pace: ~10 min/chunk at 800-byte chunks,
  CPU-only (`-ngl 0`, confirmed via `ps`), 377–385% CPU utilization — genuinely
  computing, not hung. 23 chunks would extrapolate to ~4 hours total,
  consistent with an overnight/unattended run being the intended use case.
  Full per-chunk output is preserved in
  `agents/sessions/harvey-session-20260705-205110.spmd` (chunks 1-4) even
  though the run was killed before synthesis.

- [ ] Benchmark per-chunk timing across candidate models, now that GPULayers
  defaults to 0. No other model has been timed with the GPU-layers fix in
  place — the earlier `bonsai-8b` 20+ min "hang" was confounded by the
  GPULayers=99 bug and is not valid timing data. Use `/read-chunks PATH
  --chunk-size 800 --max-chunks 2` (or 3) on the same test document across
  models to get a fast, comparable per-chunk time without committing to a
  full run. Candidates on disk in `~/Models/` as of 2026-07-05:
  `OpenELM-3B-Instruct-Q4_K_M`, `Qwen3.5-4B-Q5_K_S`, `gemma-4-E2B-it-Q5_K_M`
  (smaller Gemma4 sibling, needs `chmod +x`), `Bonsai-8B-Q1_0` (retest —
  previous timing invalid), `Apertus-8B-Instruct-2509`,
  `granite-4.1-8b-source-Q4_K_M`, plus `gemma-4-E4B-it-Q5_K_M` (~10 min/chunk
  baseline from today). Goal: build a real per-model-per-chunk timing table
  to answer "which model fits a given overnight/unattended time budget on a
  Pi 500."

- [x] Added `/read-chunks PATH [--chunk-size N] [--max-chunks N] [--overlap
  paragraph|sentence|none] [INSTRUCTION...]` — runs the chunked map-reduce
  pipeline directly, with no overflow-threshold check, and lets chunk-size/
  overlap/max-chunks be swept per-invocation independent of harvey.yaml.
  See DECISIONS.md 2026-07-05 entry. Tests in `read_chunks_cmd_test.go`.

- [ ] Known remaining gap: `startAndUseLlamafile` (`backend_startup.go`) adopts
  an already-running server under a detected model name without registering/
  probing a matching `LlamafileEntry` when that name differs from the
  configured active entry — same class of bug as the fixed `adoptExternalServer`
  case, narrower scope. See DECISIONS.md 2026-07-05 entry.

- [x] (Original bug report — superseded by the entry above.) Console log of the
  garbled Gemma4-E4B-Q4_K_M output that prompted this investigation is preserved
  in git history (see this file's blame for 2026-06-30). The chunk *isolation*
  mechanism itself was already confirmed working
  (`TestRunChunkedAnalysis_ChunkMessagesAreIsolated`); the actual cause was the
  chunking guard never firing, not model coherence collapse — see the resolved
  entry above and `chunked-analysis-design.md` for the overall approach.

## Research Question

- [x] The following URLs are from IBM's website about their approaches to improving
      language model behavior. Please review this webpages for insights that might
      prove useful for Harvey — reviewed 2026-07-08, see
      [DECISIONS.md](DECISIONS.md) (2026-07-08 — IBM generative computing /
      Granite Switch / Mellea research review). No immediate adoption; follow-ups
      for Henry tracked in `~/Laboratory/henry/continue_next_time.txt`.
      - https://research.ibm.com/blog/inference-friendly-aloras-lora
      - https://research.ibm.com/blog/generative-computing-mellea
      - https://research.ibm.com/blog/granite-libraries-project-switch
- [x] Review this GitHub repo, https://github.com/generative-computing/granite-switch, for
      implications for Harvey, Henry and Mable — reviewed 2026-07-08, see
      [DECISIONS.md](DECISIONS.md) (2026-07-08 entry, same as above).
- [x] How to improve cold starts with models. Investigated 2026-07-03,
  designed/decided/implemented 2026-07-04 — see `cold-start-latency-findings.md`.
  Not Q1_0/Bonsai-specific: cold-start time scales with parameter count
  across every model tested. Shipped: workspace-boundary fix + preflight
  context-size check (fails clearly instead of a raw 400 when the system
  prompt alone can't fit), skills catalog reformatted (-1270 tokens on the
  real 11-skill catalog), and agentPreamble/both HARVEY.md files rewritten
  for density (-30% to -58%). Not pursued: raw prompt-processing speed
  tuning (`--batch-size`/`--ubatch-size`) — re-measure cold-start with the
  trimmed prompt first; revisit only if still too slow.

## Dual RAG injection audit

See [DECISIONS.md](DECISIONS.md) (2026-06-02 — Dual RAG injection audit, deferred).
M6 (`rag.per_prompt: bool`, shipped) is the preferred resolution for models
with tool support.

- [ ] (Legacy option) Users with both `memory.enabled` and `rag.enabled` receive RAG content
  twice per turn: once via `UnifiedMemory.Recall()` at session start and once via `ragAugment()`
  per prompt. M6 above is the preferred resolution; this item tracks the fallback for models
  without tool support.
