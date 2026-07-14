
# Action Items

## Update next


## Bugs

- [x] Remove the prompt to remove previous session at startup (we have a `-resume` and `/resume` option if needed) —
  already fixed in commit `9e3e13b` (2026-07-12, bundled into an earlier "Quick Save" commit, not checked off at
  the time). `pickSession`'s interactive "Resume a prior session? [y/N]" prompt was removed from `Run()`
  (`terminal.go`); `--continue`/`--resume` CLI flags (`cmd/harvey/main.go`) and `/resume`, `/session
  use|continue|replay` (`commands.go`) are the confirmed, working replacement.

- [x] I have both Llamafile and gguf models in ~/Models on my Mac, bit the gguf models are not listed as an option
  (llama.cpp is installed) — fixed 2026-07-13. Root cause: `pickBackend` (`backend_startup.go`), the combined
  startup picker used whenever any llamafile is registered, built its options list from registered llamafiles +
  disk-scanned unregistered llamafiles + live Ollama models — with no code path for `.gguf`/llama.cpp models at
  all. `/model list`/`/model use` (`aggregateModels`) already handled all three backends correctly; the startup
  flow had never been brought in line with that later unification. Fixed by adding a disk-scan branch (mirroring
  the existing llamafile one) plus a `"llamacpp"` option kind that starts the model via the already-existing
  `startLlamaCppModelPath`. See DECISIONS.md 2026-07-13 entry. Test: `TestPickBackend_ListsGGUFModels`.

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

- [x] Known remaining gap: `startAndUseLlamafile` (`backend_startup.go`) adopts
  an already-running server under a detected model name without registering/
  probing a matching `LlamafileEntry` when that name differs from the
  configured active entry — same class of bug as the fixed `adoptExternalServer`
  case, narrower scope. Fixed 2026-07-13: `useLlamafileEntry`'s result is now
  checked, and when no `LlamafileEntry` exists yet for the adopted name, one is
  registered (empty `Path`, matching `adoptExternalServer`'s own precedent —
  the adopted server's actual model file path is unknown) with a probed
  `ContextLength`. See DECISIONS.md 2026-07-13 entry. Test:
  `TestStartAndUseLlamafile_AdoptedDifferentName_RegistersEntry`.

- [x] `/read-chunks` doesn't fail fast when the llamafile backend is
  unreachable (e.g. the server died after cancelling a prior prompt). Found
  2026-07-06 via `agents/logs/harvey-20260706-172458.jsonl`: every chunk in
  the map phase fired its own "connection refused" to `localhost:8080` and
  was recorded as a per-chunk failure (by design — a chunk failure doesn't
  abort the map phase), then the run only actually errored out at the
  synthesis call. On a multi-chunk document this burns through the whole
  file before surfacing what is really a single root-cause problem. Fixed
  2026-07-13: a new `probeClientReachable` helper (`chunk_analyzer.go`)
  dispatches on the client's `ProviderName()` (ollama/llamafile/llamacpp use
  their existing local health probes; cloud providers and test doubles are
  skipped, `checked=false`) and is called once at the top of
  `RunChunkedAnalysis` — fixing all three chunk-analysis call sites
  (`cmdReadChunks`, `injectOrChunk`, `read_file`'s guard) through their
  existing error-handling paths, no per-call-site change needed. See
  DECISIONS.md 2026-07-13 entry. Test:
  `TestRunChunkedAnalysis_FailsFastWhenBackendUnreachable`.

- [x] Debug log records each chunk's LLM call twice during `/read-chunks` — fixed 2026-07-13.
  Root cause confirmed as described: `RunChunkedAnalysis` (`chunk_analyzer.go`) logged every chunk/synthesis call
  itself while `AnyLLMClient.chatInternal` (`anyllm_client.go`) already logs the same call internally. Fix was not
  a simple "drop the caller-side calls," though: `chatInternal`'s own `DebugLog` field is nil for any freshly-
  constructed client (`resolveDispatchTarget`'s route-registry and local-switch branches both build fresh
  `*AnyLLMClient`s that were never wired), so naively removing the caller-side logging would have silently dropped
  logging entirely for `@mention`-dispatched chunk analysis instead of fixing a duplicate. Fixed in two parts:
  (1) `resolveDispatchTarget` (`dispatch_target.go`) now wires `DebugLog` onto whatever client it resolves, for
  all three of its branches; (2) the now-redundant `dbg *DebugLog` parameter was removed entirely from
  `RunChunkedAnalysis`, along with all its internal logging calls. See DECISIONS.md 2026-07-13 entry. Tests:
  `TestResolveDispatchTarget_RouteEndpoint_WiresDebugLog`, `TestResolveDispatchTarget_LocalSwitch_WiresDebugLog`.
  **Related finding, fixed separately 2026-07-13:** `builtin_tools.go`'s `read_file` pre-read chunking guard had
  the same cosmetic-only `@mention` bug already fixed elsewhere for `/read-chunks`/`injectOrChunk` during Direction
  D (Bug 1 in `subagent-dispatch-design.md`) — it parsed `@mention` to relabel `ChunkAnalysisParams.Model` but
  always dispatched via `a.Client`, never the mentioned model. Missed during that earlier work (only two of the
  three chunk-analysis call sites were found at the time); now fixed the same way, via `resolveDispatchTarget`.
  See DECISIONS.md 2026-07-13 entry. Test: `TestReadFile_MentionDispatchesToNamedModel`.

- [ ] `Qwen3.5-4B-Q5_K_S`'s `/read-chunks` chunk 1 ran 54+ minutes (interrupted
  by user 2026-07-06, still pegged at ~389% CPU when stopped — genuinely
  computing, not hung) versus the ~10 min/chunk baseline already measured for
  `gemma-4-E4B-it-Q5_K_M`. The likely difference: this entry's
  `harvey.yaml` `context_length` is 180224, vs. 16384–65536 for the other
  registered models — a much larger configured context can inflate CPU-only
  KV-cache setup/compute cost regardless of actual chunk size. Unconfirmed;
  needs a controlled retest with the Qwen entry's context_length temporarily
  lowered (e.g. to 16384) to see if chunk time drops accordingly before
  folding Qwen into the per-model timing table above.

