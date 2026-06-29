
# Action Items

## Feature ideas

- [ ] Add support for using Llama.cpp to run models
- [ ] Plan for adding support to run ONNX models
- [ ] Assay needs to work across model systems, example I should be able to use with Llamafiles or Llama.cpp
- [x] Chunked document analysis for small models — designed and planned.
  See [chunked-analysis-design.md](chunked-analysis-design.md) and
  [chunked-analysis-plan.md](chunked-analysis-plan.md). Work items W0–W5
  are in the v0.0.16 cycle below.

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
- [ ] **R7-B** — Group `Config` fields into `OllamaConfig`, `LlamafileConfig`, `SecurityConfig`, `SessionConfig` sub-structs (defer until unified backend design finalised; YAML migration required)

---

## Bugs

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
