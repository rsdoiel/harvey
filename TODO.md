
# Action Items

## Update next

- [ ] Cross-machine `knowledge.db` sync — resume here. Step 1 (UUID migration) and step 2 (merge tool) are both
  DONE and verified on `macmini-rd.local`, per `uuid-migration-design.md`/`-plan.md`,
  `merge-tool-design.md`/`-plan.md`, and the 2026-07-26 entries in `DECISIONS.md`. **Update 2026-07-27 (on
  `wren`):** the UUID migration has also already run here — confirmed all 3 projects / 84 observations / 17
  concepts / 6 sources have non-empty `uuid`s, and 4 observations already show `origin_host='wren'` (new rows
  written after migration), so this machine picked up the lazy, idempotent migration automatically on a prior
  `OpenKnowledgeBase` call, with no action needed. **Next concrete action:** both machines are now migrated, but
  a real cross-machine `bin/kbmerge` run still hasn't happened — it needs an actual copy of the other machine's
  `agents/knowledge.db` transferred onto one of them; everything verified so far used two copies of the same
  machine's file as a stand-in, not two genuinely divergent databases. After that: knowledge-base module
  extraction (own repo/`go.mod`) is step 3, JSON-L export (deferred) is step 4 — full sequencing in
  `../knowledge_db_merge_design.md`. Full resume context also recorded as a `note` observation in
  `agents/knowledge.db` (project `harvey`, concept `cross-machine-sync`, most recent entry).

## Bugs

- [x] 24 of 101 `observation_concepts` rows in `agents/knowledge.db` (this machine) reference `observation_id`
  values that no longer exist in `observations` — dangling links, found 2026-07-26 while manually verifying the
  merge tool (see `DECISIONS.md`, same date, "Cross-machine `knowledge.db` merge tool" entry). Likely a historical
  delete that ran on a connection without `PRAGMA foreign_keys=ON` active (SQLite enforces FKs per-connection, not
  persistently in the file) — e.g. a raw `sqlite3` CLI session, since `OpenKnowledgeBase` itself pins a single
  connection (`db.SetMaxOpenConns(1)`) and sets the pragma on every open, so the current Go code path was never
  the source. Not caused by, and didn't affect the correctness of, the UUID migration or merge tool —
  `MergeKnowledgeBases`'s join-based copy already excludes unresolvable links rather than propagating them.
  **Fixed 2026-07-27 (on `wren`):** backed up the live file to
  `agents/knowledge.db.pre-cleanup-20260727`, then ran `DELETE FROM observation_concepts WHERE observation_id NOT
  IN (SELECT id FROM observations)` — removed exactly 24 rows. Also checked the other five parent/join
  combinations (`observation_concepts`→`concepts`, `project_concepts`→`projects`/`concepts`,
  `observation_sources`→`observations`/`sources`) — all were already clean, so no further cleanup was needed.
  This machine's `agents/knowledge.db` only; `macmini-rd.local`'s copy was not touched and should be checked
  separately.

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
  `gemma-4-E4B-it-Q5_K_M`. Original theory: this entry's `harvey.yaml`
  `context_length` was 180224 at the time, vs. 16384–65536 for the other
  registered models — a much larger configured context can inflate CPU-only
  KV-cache setup/compute cost regardless of actual chunk size.

  **Update 2026-07-25:** confirmed via `gguf-dump` against the GGUF embedded
  in `Qwen3.5-4B-Q5_K_S.llamafile` (extracted with `unzip`, since llamafile is
  a zip-appended APE binary, not a raw GGUF) that the model's own *trained*
  context length is `qwen35.context_length = 262144` — even larger than the
  180224 this item suspected, and far above the 16384–65536 range of every
  other registered model. This makes the KV-cache-allocation theory more
  plausible, not less: llama.cpp/llamafile allocate KV cache proportional to
  the configured `-c` value at server startup (`ActiveLlamafileContextLength()`
  → `StartLlamafileService`, see `backend_llamafile.go`), independent of
  actual prompt/chunk size, so a `-c` anywhere near this model's native max
  would explain slow, CPU-bound startup cost.

  Also confirmed: `agents/harvey.yaml`'s registered entry for
  `Qwen3.5-4B-Q5_K_S` **already reads `context_length: 16384`** — the exact
  value this item proposed testing. No record exists of when or why it was
  changed (no matching git history in this repo — `git log -p -S 180224`
  returns nothing), so this may already be applied but never re-benchmarked.
  Remaining step: run `/read-chunks PATH --chunk-size 800 --max-chunks 2`
  against the current (16384) config to confirm chunk time now falls in line
  with the ~10 min/chunk baseline, then fold into the per-model timing table
  above. Not run yet — this is a multi-minute, CPU-heavy operation and
  deserves an explicit go-ahead rather than running unattended.

