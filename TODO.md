
# Action Items

## Feature ideas

- [x] Chunked document analysis for small models — designed and planned.
  See [chunked-analysis-design.md](chunked-analysis-design.md) and
  [chunked-analysis-plan.md](chunked-analysis-plan.md). Work items W0–W5
  are in the v0.0.16 cycle below.

## Bugs

- [x] Context limits hit even when file appears smaller than context window —
  root cause identified (Harvey compares against raw window, not remaining
  context after history). Fixed by W1 in
  [chunked-analysis-plan.md](chunked-analysis-plan.md).

## v0.0.16 development cycle

- [ ] Make sure LLamafile support is at parity with Ollama support, example creation and use of RAG with Llamafiles

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

- [ ] The current retry in `terminal.go` calls `Client.Chat` directly and rolls back the full
  history by one message. When `RunToolLoop` added intermediate tool-call/tool-result messages
  before the refusal, those are silently dropped. Implement a surgical rollback that only removes
  the assistant refusal message and re-adds the augmented user message, preserving prior tool
  loop history.

### `/model mode MODEL auto` — reset to auto
See [audit-trail-plan.md](audit-trail-plan.md) option 3 (`/model mode` command, `model_cache.go`).

- [ ] `/model mode` currently accepts `structured`, `prose`, `inject`, and `none` but has no
  `auto` value to clear an explicit override and return the model to capability-detected defaults.
  Add `auto` as a valid mode that sets `tool_mode = ''` (empty) in `model_capabilities`, restoring
  `modelToolMode()` fallback to `CapabilityStatus`.

### Retraction monitoring service
See [scholarly-provenance-plan.md](scholarly-provenance-plan.md) S2 and
[scholarly-provenance-design.md](scholarly-provenance-design.md).

- [ ] The `sources` table in `knowledge.db` already has `retracted INTEGER` and `retraction_note TEXT`
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

### Dual RAG injection audit
See [DECISIONS.md](DECISIONS.md) (2026-06-02 — Dual RAG injection audit, deferred).

- [ ] Users with both `memory.enabled` and `rag.enabled` receive RAG content twice per turn:
  once via `UnifiedMemory.Recall()` at session start and once via `ragAugment()` per prompt.
  Audit the overlap and either (a) skip RAG chunks in `UnifiedMemory.Recall()` when `a.RagOn`
  is true, or (b) make `ragAugment` a no-op when `UnifiedMemory` already injected from the same
  store this session.
