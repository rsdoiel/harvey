
# Action Items

## Feature ideas

- [x] The chunked reading with prompt seems to work but when I've tried the models seem prone to hullicinations, but that could be that balance of model size and main model context isn't being fully addressed. How do we ensure that each chunk cycle only see the prompt and the chunk rather than the full context and the chunk prompt?
  Confirmed isolated: each chunk LLM call receives exactly 2 messages (system + user) — no conversation history. `TestRunChunkedAnalysis_ChunkMessagesAreIsolated` verifies this. To reduce hallucination from models drawing on training data, added `chunkSystemPrompt` ("Analyse ONLY the text provided in this message…") prepended to every map and synthesis call.
- [x] The `/model download` command just doesn't make sense to me — removed; `LlamafileDownloadText` constant deleted; help text updated to point users directly to HuggingFace
- [x] The sticky last model used isn't useful since the behavior seems idiocractic between Llamafile, Ollama and Llama.cpp models. Let's drop the "active" model concept if not restarting a previous session
- [x] `/llamafile` command fully removed — confirmed absent from command table; `LlamafileHelpText` updated to redirect to `/model`; `/help llamafile` and `/help ollama` now print concise redirect messages
- [x] `/model drop` removed — model files are managed on disk (rm ~/Models/foo.llamafile) or via the ollama CLI; `cmdModelDrop`, `llamafileNameCandidates`, `ollamaListTable`, `modelSwitch` deleted; all help text updated
- [x] While Harvey should support using Ollama models (including starting/stopping Ollama service if necessary), managing the Ollama models available to fall to the Ollama cli this will give us better alignment with all the models and minimize further the Ollama specific behavior — `/ollama` command removed; `ollama` CLI handles model management; Harvey aggregates Ollama models via `/api/tags` for `/model list`
  - [x] When an alias is setup for an Ollama model via `/model use`, it should do a probe to get the model's features this will let us drop the `/ollama probe` and `/ollama probe-all` commands — auto-probe fires in `promptLazyRegister` immediately after alias creation
- [x] Once `/memory profile` is enabled there is no way to turn it off
  Already implemented: `/memory profile off` sets `Config.Memory.InjectOnStart=false` and persists to harvey.yaml. `/memory profile on` re-enables. Both are tested in `commands_memory_test.go`. Help text updated 2026-06-30 to document `on`/`off` subcommands.
- [x] Add support for using Llama.cpp to run models
- [ ] Assay needs to work across model systems, example I should be able to use with Llamafiles or Llama.cpp
  Design settled 2026-06-30 — see [assay-llamacpp-design.md](assay-llamacpp-design.md) and [assay-llamacpp-plan.md](assay-llamacpp-plan.md). Implementation pending (W0–W3).
- [x] Chunked document analysis for small models — designed and planned.
  See [chunked-analysis-design.md](chunked-analysis-design.md) and
  [chunked-analysis-plan.md](chunked-analysis-plan.md). Work items W0–W5
  are in the v0.0.16 cycle below.
- [x] Model picker is showing Ollama files, when I picked Bonsai 8B (available as both Llamafile and under Ollama), it skipped setting up the alias. — Fixed: `ModelAlias` now carries `Engine`; `promptLazyRegister` matches on both model name and engine so same-named cross-backend models get separate aliases.
- [x] Need tests to confirm we can switch between Llamafile and Llama.cpp for models that have both a .llamafile version and a .gguf version — `model_switch_test.go` covers stem consistency and engine-labelled aggregation.


## Refactoring

See [refactoring-plan.md](refactoring-plan.md) for rationale and full work item specs.

### Do now — before new features

- [x] **R9** — Lower `maxInjectFileBytes` from 64KB to 16KB in `file_inject.go` (OOM safety; 5 min)
- [x] **R0-B** — Delete `filterEnvironment` alias in `terminal.go`; call `filterCommandEnvironment` directly
- [x] **R0-A** — Consolidate `estimateTokens` into `context_estimator.go`; remove 3 inline copies
- [x] **R0-F** — Merge `ollamaFormatBytes` into `formatBytes` in `commands.go`
- [x] **R0-E** — Move `resolveLlamafilePath` from `terminal.go` to `llamafile.go`
- [x] **R0-C** — Move `ragAugment` from `terminal.go` to `rag_support.go`
- [x] **R0-D** — Move `ragChunk` from `commands.go` to `rag_support.go`
- [x] **R0-G** — Rename ~9 orphan test files to match the source files they actually test
- [x] **R0-H** — Remove duplicate `LlamafileEntry` definition from `config.go` (two definitions at ~line 80 and ~line 1090)
- [x] **R1** — Move `tryExecuteProseToolCalls` + `tryExecuteApertusToolCalls` from `terminal.go` to `tool_executor.go`

### After R0+R1 stabilize

- [x] **R7-A** — Extract YAML adapter structs from `config.go` into `config_yaml.go`
- [x] **R5** — Extract 6 backend-startup functions from `terminal.go` into `backend_startup.go`
- [x] **R2** — Extract `/rag` command + ingest pipeline from `commands.go` into `commands_rag.go` (~1400 lines)
- [x] **R3** — Extract `/memory` commands from `commands.go` into `commands_memory.go` (~640 lines)
- [x] **R4** — Extract `/kb`, `/skill`, `/route` commands from `commands.go` into `commands_kb.go`, `commands_skill.go`, `commands_route.go`

### After R2–R5 stabilize

- [x] **R6** — Introduce `MemorySystem` aggregate (`memory_system.go`); `OpenMemory`/`Close`; replace 11 separate `NewMemoryStore` opens; add `Memory *MemorySystem` to `Agent`
- [x] **R8** — Create `builtin_tools_test.go` with coverage for chunking guard, write_file auto-format, and permission paths
- [x] **R7-B** — Group `Config` fields into `OllamaConfig`, `LlamafileConfig`, `SecurityConfig`, `SessionConfig` sub-structs (defer until unified backend design finalised; YAML migration required)

---

## Bugs

- [x] **`@alias` switching broken for llama.cpp models.**
  Fixed 2026-06-30: `attemptModelSwitch` now checks `ModelAlias.Engine` before
  dispatching. `Engine="llamacpp"` calls `resolveLlamaCppModelPath` (scans ModelsDir
  for the matching `.gguf`) then `startLlamaCppModelPath`. `Engine="ollama"` goes
  direct to an Ollama client. `Engine=""` or `"llamafile"` retains the previous
  `switchLlamafileModel`-then-Ollama-fallback behaviour for legacy aliases.
  Note: `/route add` registered endpoints already worked for all three backends.

- [x] **SmolLM3 via llama.cpp "unexpected end of JSON input".**
  llama-server occasionally returns a truncated or malformed JSON body on
  inference requests. Root cause: llama-server closes the connection mid-stream
  under load; Go's JSON decoder surfaces this as "unexpected end of JSON input".
  Mitigated 2026-06-30: `isConnectionError` now catches this string so the
  auto-reconnect path fires instead of surfacing the raw error. Full fix
  (preventing the server close) requires upstream llama-server work.

- [x] **/status showed "llamafile" for llamacpp backend.**
  LlamaCppBackend.NewClient() called newLlamafileLLMClient instead of
  newLlamaCppLLMClient. Fixed 2026-06-29 (commit ec79ad8).
  Regression test: TestLlamaCppBackend_NewClient_ProviderIsLlamacpp.

- [x] **Auto-reconnect restart used wrong engine/function for llamacpp.**
  Crash message said "llamafile server"; restart always called
  restartActiveLlamafile even for llamacpp. Fixed 2026-06-29 (commit f17608c).
  Added LlamaCppBackend.ModelPath() and restartActiveLlamaCpp().

- [x] **/exit at chunk prompt processed as chunk instruction.**
  promptChunkInstruction treated "/exit" as the analysis instruction.
  Fixed 2026-06-29 (commit 76d46bd) — now cancels on "no", "cancel",
  "q", "/exit", "/quit". Regression test: TestPromptChunkInstruction_ExitCancels.

- [x] **Chunk LLM calls invisible in debug log.**
  RunChunkedAnalysis called client.Chat() with no DebugLog instrumentation.
  Fixed 2026-06-29 (commit faf5eb1) — added *DebugLog nil-safe parameter.

- [x] **Llamafile capability probe missing — tool support assumed, not detected.**
  `ProbeLlamafileProps` reads `/props`, checks `chat_template` for tool markers
  (`<tool_call>`, `[TOOL_CALLS]`, `<|python_tag|>`, `<|tool_call|>`), and writes
  `SupportsTools` + `ToolMode` into `model_cache.db`. Wired for llamafile via
  `useLlamafileEntry` and for llama.cpp via `startLlamaCppModelPath`; both share
  the `probeLlamaCppAndCache` helper in `backend_llamacpp.go`.

- [x] **Chunked analysis not triggered for large files (W5 wiring incomplete).**
  Root cause: the chunking guard only existed in the `read_file` tool handler
  (structured tool call path). The inject path (`injectOrChunk`, called from
  `runChatTurn` when `!toolsReliable()`) silently skipped files > 16KB with no
  chunking fallback. Fixed by replacing `injectFileContext` with `injectOrChunk`
  in `runChatTurn`, which runs the interactive `promptChunkInstruction` →
  `RunChunkedAnalysis` flow for large files when chunking is enabled.

- [x] This ollama model list in harvey's YAML config can become stale over time as I add and remove models, it needs to get cleaned up so it doesn't list models that are not available.
  Fixed via two mechanisms: (1) `/ollama rm` now immediately prunes `model_aliases` values and
  `model_map` keys referencing the removed model; (2) new `/ollama clean` subcommand queries the
  live Ollama model list and prunes all stale references from harvey.yaml in one pass.

## v0.0.16 development cycle

- [x] Make sure LLamafile support is at parity with Ollama support, example creation and use of RAG with Llamafiles

### Chunked document analysis
See [chunked-analysis-design.md](chunked-analysis-design.md) and
[chunked-analysis-plan.md](chunked-analysis-plan.md).

- [x] W0 — Update `FOUNTAIN_FORMAT.md` to v1.3: `INT. CHUNK ANALYSIS` scene,
  `[[chunk:]]`, `[[chunk-result:]]`, `[[synthesis:]]` notes (doc only, no Go changes)
- [x] W1 — `context_estimator.go`: `remainingContext`, `fileExceedsBudget`
  (standalone accounting bug fix; `estimateTokens` reused from `routing.go`)
- [x] W2 — `chunker.go`: `ChunkDocument`, `DetectDocType`, `ChunkConfig`,
  `DocType` (paragraph/block splitting with overlap)
- [x] W3 — `recorder.go`: `RecordChunkAnalysisStart`, `RecordChunkResult`,
  `RecordChunkSynthesis` methods
- [x] W4 — `chunk_analyzer.go`: `RunChunkedAnalysis` map-reduce engine
- [x] W5 — `terminal.go` wiring: pre-read guard, `promptChunkInstruction` alert
  UX, `@mention` routing, `harvey.yaml` `chunking:` stanza,
  `CONFIGURATION.md` update


### Option 2 reactive retry — surgical rollback
See [audit-trail-plan.md](audit-trail-plan.md) W1 and the small-model tool-use mitigation work
(file_inject.go option 2).

- [x] The current retry in `terminal.go` calls `Client.Chat` directly and rolls back the full
  history by one message. When `RunToolLoop` added intermediate tool-call/tool-result messages
  before the refusal, those are silently dropped. Implement a surgical rollback that only removes
  the assistant refusal message and re-adds the augmented user message, preserving prior tool
  loop history.

### `/model mode MODEL auto` — reset to auto
See [audit-trail-plan.md](audit-trail-plan.md) option 3 (`/model mode` command, `model_cache.go`).

- [x] `/model mode` currently accepts `structured`, `prose`, `inject`, and `none` but has no
  `auto` value to clear an explicit override and return the model to capability-detected defaults.
  Added `auto` as a valid mode that sets `tool_mode = ''` (ToolModeAuto) in `model_capabilities`,
  restoring `toolsReliable()` fallback to `CapabilityStatus`.

### Retraction monitoring service
See [scholarly-provenance-plan.md](scholarly-provenance-plan.md) S2 and
[scholarly-provenance-design.md](scholarly-provenance-design.md).

- [x] The `sources` table in `knowledge.db` already has `retracted INTEGER` and `retraction_note TEXT`
  columns for manual marking (via `/kb retract`). Add a periodic background check against the
  Retraction Watch API (`retractionwatch.com`) that flags retracted DOIs automatically. A
  `/kb check-retractions` command (or scheduled task) should query registered sources with
  `identifier_type = 'doi'` and update `retracted`/`retraction_note` on hits.

### llama.cpp Apertus tool-call format
See `henry` project (`henry-handoff-20260622-llamafile-factory.spmd`).

- [ ] When llama.cpp gains stable custom token support, update
  `templates/apertus-4b-toolcall.jinja` in the `henry` project to use structured tool-call
  tokens instead of the current prose JSON fence workaround. Retest with Apertus 4B via
  `bin/assay --llamafile`.

### Agentic memory tools
See [agentic-memory-design.md](agentic-memory-design.md) and [agentic-memory-plan.md](agentic-memory-plan.md).
Inspired by AgeMem (Yu et al., 2025; arXiv:2601.01885v2 — `memory-models/2601.01885v2.pdf`).

- [x] **M0** — Proactive STM warning: inject system nudge when `remainingContext < stm_warn_pct`
  (default 20%). Add `STMWarnPct float64` to `ChunkConfig`; check in `runChatTurn`.
  **Effort:** ~1h. No tool registration required.

- [x] **M3** — `retrieve_memory(query, top_k)` builtin tool. Wraps `UnifiedMemory.Recall()`;
  returns formatted context as tool result. On-demand mid-session LTM retrieval.
  **Effort:** ~1h.

- [x] **M1** — `summary_context(span)` builtin tool. Compresses N turns (or "all") into a
  single summary entry using the active LLM; replaces covered messages in `a.History`.
  **Effort:** ~2h.

- [x] **M2** — `filter_context(criteria)` builtin tool. Embeds criteria; removes history
  messages scoring above θ_f = 0.6 cosine similarity. Falls back to keyword match when
  no embedder is configured. **Effort:** ~3h.

- [x] **M4** — `add_memory(content, memory_type, tags)` builtin tool. Wraps `MemoryStore.Save()`;
  auto-generates ID; safe-mode confirmation; recorder call.
  **Effort:** ~2h.

- [x] **M5** — `update_memory(id, content)` and `delete_memory(id)` builtin tools.
  Update re-saves with new content; delete archives by zeroing confidence.
  **Effort:** ~2h.

### Dual RAG injection audit
See [DECISIONS.md](DECISIONS.md) (2026-06-02 — Dual RAG injection audit, deferred).
Superseded in part by M6 in [agentic-memory-plan.md](agentic-memory-plan.md): once `retrieve_memory`
(M3) lands, add `per_prompt: bool` to RAG store config so capable models can drive retrieval themselves.

- [x] **M6** — `rag.per_prompt: bool` config flag (default `true`). When false, `ragAugment()`
  is a no-op; the model uses `retrieve_memory` instead. Resolves dual-injection for capable models.
  **Effort:** ~2h. **Dependency:** M3.

- [ ] (Legacy option) Users with both `memory.enabled` and `rag.enabled` receive RAG content
  twice per turn: once via `UnifiedMemory.Recall()` at session start and once via `ragAugment()`
  per prompt. M6 above is the preferred resolution; this item tracks the fallback for models
  without tool support.
