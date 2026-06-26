
# Action Items

## Bugs

- ~~`/model use` (no arg): prints usage error instead of showing a picker of registered
  llamafile models and aliases.~~ **Fixed** — picker implemented, tests passing.

- ~~`/session use` / `/session continue` (no arg): prints usage error instead of showing a
  picker of available session files.~~ **Fixed** — picker implemented, tests passing.

## Release Review

## Next (v0.0.15 release)

### Audit trail enhancements (see [audit-trail-plan.md](audit-trail-plan.md))

- ~~**W0** — Update `FOUNTAIN_FORMAT.md` to v1.2: correct INT./EXT. semantic
  (INT. = local computation, EXT. = remote); add new notation to spec before
  any code changes.~~ **Done** — version header updated to 1.2; [[tool:]], [[rag:]], [[recall:]] documented in Special Syntax; INT. CONTEXT RECALL in Scene Types Reference; parsing regexes added; validation rule for HARVEY in EXT. corrected.

- ~~**W1** — Structured `[[tool: name(args) — status]]` notes: replace prose
  action blocks with parseable Fountain notes; add `Result` and `Character`
  fields to `ToolCallRecord`; extend `toolCallsFromHistory` to pair calls with
  tool-role message results.~~ **Done** — code already implemented; 7 tests added and passing.

- ~~**W2** — RAG provenance notes: change `ragAugment` to return
  `(string, *RAGAugmentInfo)`; emit `[[rag: N chunks from STORE, top score
  S.SS]]` note inside the turn's scene before user dialogue.~~ **Done** — code already implemented; 5 tests added and passing (incl. full round-trip with fake Ollama embed server).

- ~~**W3** — `INT. CONTEXT RECALL` scene: add `RecordContextRecall` to
  `Recorder`; call from `injectMemoryContext` when `UnifiedMemory.Recall`
  returns non-empty results; write one `[[recall: ID (SOURCE) — score S.SS]]`
  note per recalled item.~~ **Done** — code already implemented; 2 tests added and passing.

- ~~**W4** — EXT. scenes + character-attributed tool calls: add
  `RecordExteriorTurn` to `Recorder` for remote route dispatches; pass
  `charName` through `runChatTurn` → `toolCallsFromHistory` for local
  `@mention` switches; add `CharacterName` to `ToolExecutor`.~~ **Done** — code already implemented; `TestRecordExteriorTurn` added and passing; `go test -race ./...` clean.

### Scholarly provenance (see [scholarly-provenance-plan.md](scholarly-provenance-plan.md))

- ~~**S1** — RAG chunk provenance schema: add `indexed_at`, `content_hash`,
  `source_url`, `source_doi`, `source_title`, `source_version`, `rights`,
  `retracted`, `retraction_note` columns to `chunks` table via idempotent
  `ALTER TABLE` migration in `RagStore.Open`; compute SHA-256 content hash on
  ingest for change detection; add `--doi`, `--url`, `--title`, `--version`,
  `--rights` flags to `/rag ingest`.~~ **Done** — provenance columns added via idempotent ALTER; SHA-256 dedup with stale-chunk cleanup; flags wired to `/rag ingest`; 3 new tests pass; `TestRagIngestPDF_integration` assertion relaxed for dedup.

- ~~**S2** — Source registry in `knowledge.db`: add `sources` authority table
  and `observation_sources` join table; migrate existing `source_doi` values
  from `observations`; add `/kb source {add,list,show,remove}` and
  `/kb retract` commands.~~ **Done** — `sourcesSchema` with UNIQUE index; one-time DOI migration in `OpenKnowledgeBase`; `AddSource`, `ListSources`, `ShowSource`, `RemoveSource`, `RetractSource`, `LinkObservationSource`, `FindOrCreateSource` implemented; `/kb source` and `/kb retract` commands wired; 9 tests pass.

- ~~**S3** — Source-level Fountain notes: extend `RAGAugmentInfo` with
  `Sources []RAGChunkRef`; emit deduplicated `[[rag-source: path[:lines]
  (title, doi:DOI)]]` notes after the existing `[[rag: ...]]` aggregate note.
  Depends on S1.~~ **Done** — `RAGChunkRef` struct added to `recorder.go`; `Sources []RAGChunkRef` added to `RAGAugmentInfo`; `Query` now fetches `source_url`, `source_doi`, `source_title` from DB; `ragAugment` deduplicates by source path and populates `Sources`; `RecordTurnWithStats` emits per-source `[[rag-source:]]` notes with dedup guard; 3 tests pass.

- ~~**S4** — Observation attribution: add `lastRAGInfo *RAGAugmentInfo` to
  `Agent`; prompt in `/kb observe` to auto-link retrieved sources; add
  `/kb cite SOURCE_ID` for manual linking; show source attribution with
  retraction warnings in `/kb show`. Depends on S2 and S3.~~ **Done** — `LastRAGInfo` and `LastObservationID` added to `Agent`; cleared by `ClearHistory`; set after each RAG-augmented chat turn; `kbObserve` hints available sources; `/kb cite` links sources to last observation; `/kb show` displays sources with retraction warnings; `ObservationSources` query added to `knowledge.go`; 8 tests pass; `go test -race ./...` clean.

- **HARVEY.md** — Add a Provenance section instructing the model to attribute
  retrieved content at the point of use, not post-hoc.

## Small model tool-use mitigations

These address the general problem that small models (Phi4, Llama3, Apertus, etc.)
don't reliably fire structured tool calls even when the schema is provided.

- ~~**Option 1** — Pre-resolve file paths: when `toolsReliable()` is false, scan the
  user prompt for path-like tokens, read matching files within the workspace, and
  prepend their content before sending. Fixes the "I don't have the ability to read
  files" class of failure.~~ **Done** — `injectFileContext` + `toolsReliable()` in
  `file_inject.go`; hooked in `runChatTurn`; 15 tests pass; `go test ./...` clean.

- ~~**Option 2** — "Can't read" retry detection: after a response that matches patterns
  like *"I don't have the capability"* or *"please provide the file"*, auto-retry the
  prompt with file content pre-loaded. Reactive safety net complementing option 1.~~
  **Done** — `looksLikeCantReadFile` + `toolsReliableOverride` hook in `file_inject.go`;
  retry block in `runChatTurn` between error handling and display; `injectFileContext`
  made idempotent (skips `### File: tok` already present); 6 new tests pass;
  `go test -race ./...` clean.

- **Option 3** — Per-model `ToolMode` in `harvey.yaml`: add a `tool_mode` field
  (`structured | prose | inject | none`) so the model probe (or user override) can
  set the injection strategy explicitly per model.

## llama.cpp server-side tool call parsing for Apertus

Apertus 4B uses a native tool call format (`<SPECIAL_71>[{"tool_name": args}]<SPECIAL_72>`)
that llama.cpp's server does not recognise as structured tool calls. Apertus tool calls
currently travel as raw text in the API content field and are parsed client-side by
Harvey's prose fallback (`tryExecuteApertusToolCalls` in `terminal.go`).

When llama.cpp adds support for registering a custom tool-call token pattern (or adds
built-in Apertus support), update `templates/apertus-4b-toolcall.jinja` in the henry
project to register the token pair so the server converts them to OpenAI API `tool_calls`
in the response. That would enable full multi-turn structured tool use instead of the
current single-turn prose fallback.

Track: https://github.com/ggml-org/llama.cpp/issues — search "custom tool call format"
or "Apertus".
