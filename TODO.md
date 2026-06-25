
# Action Items

## Bugs

- `/model use` (no arg): prints usage error instead of showing a picker of registered
  llamafile models and aliases. Should call SelectFromStrings(allModelNames(a), ...) when
  len(args) < 2, matching the UX of /ollama use, /llamafile use, and /rag use.

- `/session use` / `/session continue` (no arg): prints usage error instead of showing a
  picker of available session files. Should call ListSessionFiles(a.SessionsDir) and
  SelectFrom when len(args) < 2, matching the UX of /rag use and /llamafile use.

## Release Review

## Next (v0.0.15 release)

### Audit trail enhancements (see [audit-trail-plan.md](audit-trail-plan.md))

- **W0** — Update `FOUNTAIN_FORMAT.md` to v1.2: correct INT./EXT. semantic
  (INT. = local computation, EXT. = remote); add new notation to spec before
  any code changes.

- **W1** — Structured `[[tool: name(args) — status]]` notes: replace prose
  action blocks with parseable Fountain notes; add `Result` and `Character`
  fields to `ToolCallRecord`; extend `toolCallsFromHistory` to pair calls with
  tool-role message results.

- **W2** — RAG provenance notes: change `ragAugment` to return
  `(string, *RAGAugmentInfo)`; emit `[[rag: N chunks from STORE, top score
  S.SS]]` note inside the turn's scene before user dialogue.

- **W3** — `INT. CONTEXT RECALL` scene: add `RecordContextRecall` to
  `Recorder`; call from `injectMemoryContext` when `UnifiedMemory.Recall`
  returns non-empty results; write one `[[recall: ID (SOURCE) — score S.SS]]`
  note per recalled item.

- **W4** — EXT. scenes + character-attributed tool calls: add
  `RecordExteriorTurn` to `Recorder` for remote route dispatches; pass
  `charName` through `runChatTurn` → `toolCallsFromHistory` for local
  `@mention` switches; add `CharacterName` to `ToolExecutor`.

### Scholarly provenance (see [scholarly-provenance-plan.md](scholarly-provenance-plan.md))

- **S1** — RAG chunk provenance schema: add `indexed_at`, `content_hash`,
  `source_url`, `source_doi`, `source_title`, `source_version`, `rights`,
  `retracted`, `retraction_note` columns to `chunks` table via idempotent
  `ALTER TABLE` migration in `RagStore.Open`; compute SHA-256 content hash on
  ingest for change detection; add `--doi`, `--url`, `--title`, `--version`,
  `--rights` flags to `/rag ingest`.

- **S2** — Source registry in `knowledge.db`: add `sources` authority table
  and `observation_sources` join table; migrate existing `source_doi` values
  from `observations`; add `/kb source {add,list,show,remove}` and
  `/kb retract` commands.

- **S3** — Source-level Fountain notes: extend `RAGAugmentInfo` with
  `Sources []RAGChunkRef`; emit deduplicated `[[rag-source: path[:lines]
  (title, doi:DOI)]]` notes after the existing `[[rag: ...]]` aggregate note.
  Depends on S1.

- **S4** — Observation attribution: add `lastRAGInfo *RAGAugmentInfo` to
  `Agent`; prompt in `/kb observe` to auto-link retrieved sources; add
  `/kb cite SOURCE_ID` for manual linking; show source attribution with
  retraction warnings in `/kb show`. Depends on S2 and S3.

- **HARVEY.md** — Add a Provenance section instructing the model to attribute
  retrieved content at the point of use, not post-hoc.

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
