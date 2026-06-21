# CHANGES

## v0.0.14 (2026-06-21)

### New features

- Llamafile is now the primary language model backend: startup sequence shows registered llamafiles first, auto-selects on preferred model match, then falls back to Ollama models
- Explicit connection feedback: "Connecting to NAME (llamafile)… ✓" shown in terminal on backend selection and after `startAndUseLlamafile`
- Stale server adoption: Harvey detects a running llamafile server it did not start, probes its model via `/v1/models`, warns on model mismatch, and adopts it rather than refusing to start
- `/llamafile show [NAME]`: displays name, path, file size, and configured context length for a registered model
- `/rag show [NAME]`: displays store name, database path, embedding model, chunk count, and model map
- Remote RAG ingest extended: `sftp://`, `scp://`, `http://`, and `https://` URIs now supported alongside `s3://`
- `/read` auto-detects `.pdf` files and extracts text via poppler (pdfinfo + pdftotext), consistent with the `read_file` built-in tool
- `/status` backend tag and token-count estimate now work for llamafile backends (was Ollama-only)
- Pipeline context-utilization display now works for llamafile via character estimate (was Ollama-only)
- Context utilization hint `[ctx: N%]` added to spinner label when estimated token usage reaches 50% of the model's context window
- Routing feedback in spinner: shows `@route · model` during routed turns when routing is active
- Model provenance recorded in Fountain session header: `Model:` field now stores `NAME (backend)` (e.g., `QWEN-CODING (llamafile)`) for session replay and audit
- Health check on `--resume`/`--continue`: session model is extracted before backend selection; a mismatch warning is shown when the resumed model differs from the active backend
- `@mention` dispatch: routing is tried first when routing is enabled and a route exists; falls back to `attemptModelSwitch` for local model switching; case-insensitive for both llamafile names and model aliases
- Help system: all 41 help-text constants documented with block comments and reordered into 11 logical groups; `ModelHelpText` dispatch bug fixed (topic was unreachable via `/help model`); `harvey-model.7.md` man page added

### Documentation

- Complete rewrite of `overview.md` with "Natural Language Programming for Scholarly Work" framing
- `getting_started.md` rewritten llamafile-first with correct Mozilla AI model download URL and startup sequence
- `user_manual.md` restructured: llamafile-first Model Management section, corrected links, expanded man page index
- `CONFIGURATION.md` fully updated: all v0.0.14 configuration fields documented (`model_aliases`, `llamafile` with `context_length`, `tools`, `memory` with `rolling_summary` and `knowledge_base`, `rag` nesting, `syntax_highlight`, `auto_format`); SFTP, HTTP, and AWS environment variables added; complete annotated `harvey.yaml` example

## v0.0.13 (2026-06-19)

### New features

- `/profile` command: top-level alias for `/memory profile <list|show|edit|use|rename>` — profile management without the `/memory` prefix
- `--resume` flag: resumes the most recent session in `agents/sessions/` automatically at startup (no path needed); prints a notice if no sessions are found
- Spinner live status: tool calls now show a transient "Calling tool…" status line on the second spinner line while waiting for results; `StatusReporter` interface added to `tool_executor.go` so the spinner can be wired to the executor
- `assay --llamafile PATH`: evaluate a llamafile binary directly; assay starts and stops the process automatically and derives the model name from the binary path
- Added web-developer workspace profile template (`templates/profiles/web-developer.fountain`)
- `HARVEY.md`: documented native file-reading capabilities — PDF (via poppler) and image (vision routes) — so models know to call `read_file` directly without asking the user to convert files

### Bug fixes

- S3 remote: improved not-found detection for missing keys and missing buckets; path-style access now correctly enabled for non-AWS S3-compatible endpoints (MinIO, Cloudflare R2)
- `MostRecentSession` helper added to `sessions_files.go`; `--resume` reliably selects the newest `.spmd`/`.fountain` file by delegating to `ListSessionFiles` (already sorted newest-first)
- Llamafile: `scanLlamafileModels` now discovers binaries by directory scan, fixing cases where auto-discovery missed models not registered via `/llamafile add`

## v0.0.12 (2026-06-17)

### New features

- Memory enrichment: added `kind` field to memory documents classifying why knowledge matters (`pitfall`/`workaround`/`recommendation`/`pattern`)
- Memory enrichment: added `action` field — the imperative step a future agent should take; included in embedding text for better semantic retrieval
- Memory enrichment: added `confidence` field (default 0.5); retrieval scores are weighted multiplicatively (`score = cosine × confidence`)
- `/memory flag <id>`: new command reduces confidence by 0.1 per call; auto-archives when confidence falls to or below 0.2
- `/memory list`: new `--kind` filter; output now shows kind and confidence columns alongside type
- Miner prompt updated to elicit `kind` and `action` for each extracted memory; all five memory types now listed
- `WriteDigest()`: MemoryStore auto-writes `agents/memories/DIGEST.md` on every Save, Archive, and MineAuto — plain Markdown readable by any LLM without a SQLite client
- `agents/skills/harvey-memory/SKILL.md`: new cross-agent skill teaching Vibe and Claude Code when and how to use the memory digest
- Memories database lazily migrated: existing `memories.db` files gain `kind`, `action`, `confidence` columns on first open; FTS5 table rebuilt with new columns
- Added `create_dir` built-in tool so models can create directories without `run_command mkdir`
- Added `/safe` and `/safe_mode` as aliases for `/safemode`
- Unknown slash commands now highlighted in yellow
- Tool result compaction: prior tool-call rounds are compacted in `RunToolLoop` before each new LLM turn, keeping context bounded during multi-step tasks
- `/plan` command: generate a GFM checklist plan, execute each step with fresh bounded context, track progress in `agents/plan.md`
- `multi-file` skill: auto-detects multi-file creation requests and generates a plan via the compiled script path
- Skill dispatch: `HARVEY_API_BASE` env var added to compiled script environment
- Skill dispatch: LLM-fallback skills now trigger an LLM response turn instead of silently continuing
- Plan execution: steps with blocked or failed tool calls are no longer auto-marked complete

### Bug fixes

- Llamafile: fixed exec format error on macOS (APE binaries now launched via `/bin/sh`)
- Llamafile: added `--server` flag for headless mode (llamafile v0.10.3 API change)
- Llamafile: added `-ngl` GPU layer offload support with `gpu_layers` config option (default 99, maximises Metal/CUDA)
- Llamafile: `startup_timeout` config option (default 120s); fast-fail on process exit with stderr surfaced in error
- Llamafile: debug log now wired to new client after `/llamafile use` model switch
- Skill trigger regex: fixed `/pattern/flags` format (trailing flag suffix no longer breaks regex mode)
- Skill dispatch: compilation failure now falls back to LLM context-injection path instead of erroring out

## v0.0.11 (2026-06-11)

### New features

- Added scholarly identifier extraction and normalization for 14 identifier types (DOI, ORCID, ROR, RAiD, ArXiv, FundRef, ISBN, ISSN, ISNI, PMID, PMCID, VIAF, SNAC, LCNAF) via `scholarly_identifiers.go` and `github.com/caltechlibrary/metadatatools`
- Added scholarly-aware PDF ingest: papers are chunked by section (abstract, introduction, methods, results, discussion, conclusion, references) and tagged with the document's own identifiers and any cited works' identifiers
- Extended the knowledge base schema so observations can record a source DOI and concepts can represent scholarly entities (people, papers, institutions, funders) via an identifier type/value pair
- Workspace onboarding now scans `codemeta.json`/`CITATION.cff` for identifiers (e.g. author ORCID iDs, release DOIs) and records them in the project's `project_fact` memory metadata

## v0.0.10 (2026-06-09)

### New features

- Added multi-language code-aware chunking for RAG ingestion (C, C++, Pascal, Oberon, Lisp, Basic)
- Added documentation extraction: comment and docstring association with symbols for C, C++, Pascal, Oberon, Lisp, Basic
- Added ANSI syntax highlighting of code blocks in LLM responses (13 languages: C, C++, Pascal, Oberon, Lisp, Basic, Go, Python, JavaScript, TypeScript, Rust, Shell, SQL); configurable via `syntax_highlight` in harvey.yaml
- Added automatic code formatting on `write_file`: built-in formatters for Pascal, Oberon, Basic; external pipe-mode formatters for Go (gofmt), C/C++ (clang-format), Python (black), Rust (rustfmt), JavaScript/TypeScript (prettier); configurable via `auto_format` in harvey.yaml
- Added `/format FILE [FILE...]` command to manually format workspace source files in-place

## v0.0.9 (2026-06-09)

Added `/loop` command

## v0.0.8 (2026-06-05)

*Initial profile template system*

- Added `/profile use` command for workspace profile management
- Added help guides and `/status profile` display

## v0.0.7 (2026-06-02)

### New features

**`assay` evaluation harness** — a new `bin/assay` tool for running a prompt
corpus against one or more Ollama models and producing a Markdown report plus
JSON results for human review and automated checking.

- Corpus defined in YAML (`agents/assay/corpus.yaml`); each prompt specifies
  category, language, automated checks (`contains`, `not_contains`, `compiles`,
  `go_vet`), and human-review questions
- `--rag-db PATH` — opens a RAG store and injects retrieved context before
  each prompt, enabling RAG-assisted evaluation
- `--rag-compare` — runs each prompt twice (base + RAG) and writes a per-check
  delta table alongside the main report
- `--category NAME` — run only one category of prompts (e.g. `go-crosswalk`)
- Summary table (model × prompts × auto-pass rate × average tok/s) at the top
  of every report

**File attachment commands**

- `/attach FILE` — route-aware file injection: native image (JPEG, PNG, GIF,
  WebP) for vision-capable routes, text extraction for PDFs, plain-text for
  source files; not restricted to the workspace
- `/read-pdf FILE [PAGES]` — extract text from a PDF using poppler utilities
  (`pdfinfo`, `pdftotext`, `pdfimages`) and inject into context; accepts a
  page range (e.g. `40-55`); cap of 20 pages per call; diagram-only pages are
  flagged; not restricted to the workspace

**Knowledge discoverability**

- `/hint` — on-demand improvement suggestions: flags unmined sessions, empty or
  disabled RAG stores, and empty knowledge base
- `/recall QUERY` — alias for `/memory recall`; searches all three knowledge
  silos (RAG, memory store, knowledge base) in one call
- `/help learn` — new unified help topic explaining the three-silo architecture
  and the single decision rule for where to put each type of content
- Session-start memory digest: on REPL startup Harvey prints dim actionable
  hints when sessions are unmined, the active RAG store is empty, or RAG is
  off with chunks present
- Enhanced `/status` — now includes a Memory/RAG summary block (active
  memories injected, unmined session count, active RAG store, chunk count,
  RAG on/off)

**Persistent command history** — the REPL input history is saved to
`agents/harvey_history` on exit (capped at 1000 entries, most recent kept)
and reloaded on the next startup; history is per-workspace to avoid leaking
commands and paths between projects.

### Bug fixes

- Removed unreachable dead code (`cmdRunCtx`, `extractRunSuggestions`) that
  was missing the safe-mode allowlist check and would have bypassed it had
  it ever been wired into an execution path
- Help text overview (`/help`) was missing entries for `/attach`, `/read-pdf`,
  `/hint`, `/memory`, `/recall`, and `/pipeline`; all are now listed under
  their appropriate sections
- `harvey --help attach`, `--help read-pdf`, and `--help learn` now work
  correctly; topic was silently falling through to the unknown-topic error

## v0.0.6 (2026-05-30)

### New features

**Remote protocol integration** — Harvey can now read files from remote
storage over `s3://`, `http://`, `https://`, `sftp://`, and `scp://` URIs
anywhere a local path was accepted (`/read`, `/attach`, RAG ingest).

- `RemoteReader` interface with URI-scheme factory (`NewRemoteReader`)
- S3-compatible backend via MinIO Go client (AWS S3, MinIO, Cloudflare R2)
- HTTP/HTTPS backend (stdlib `net/http`); HTTPS→HTTP downgrade protection
- SFTP/SCP backend (`golang.org/x/crypto/ssh`); strict host-key fingerprint
  verification required — anonymous connections are refused
- S3 RAG ingest: `/rag add s3://bucket/prefix/` ingests objects as RAG docs
- Tab completion extended to remote URI prefixes

**Unified memory system**

- `MemoryConfig` is now the single canonical owner of all RAG and knowledge-base
  configuration; `memory.rag:` and `memory.knowledge_base:` replace top-level
  `rag:` in `harvey.yaml` (old format still loaded for backward compatibility)
- Rolling summary: when conversation history exceeds `memory.budget_pct`
  (default 25 %) of the model context window, Harvey compresses all but the
  last six turns into a ~150-token summary with a warning
- Hybrid retrieval: FTS5 lexical fast-path combined with cosine similarity
  (when an embedder is active); `Recent()` fallback replaced by relevance ranking
- Session stats: `memory_stats` table in `memories.db` tracks token budget,
  injected tokens, compression events, and average tokens/sec per session
- `/memory status` now shows budget utilisation and, after ten sessions,
  prints evidence-based advice to increase or reduce `budget_pct`
- `/memory recall QUERY` — on-demand semantic search across all memory types
- Workspace-profile onboarding: on first run Harvey prompts four questions and
  writes a `workspace_profile` memory document; no longer asks for workspace
  name (derived from the directory)

**Pipeline support**

- `/pipeline` command and implementation for multi-step prompt pipelines
- Pipeline steps can reference workspace files and inject their output into
  subsequent steps

### Security fixes

- **SFTP — OOM prevention**: `sftpReadPacket` now rejects packets larger than
  4 MB; `sftpReadStr` rejects strings larger than 256 KB. A malicious or
  misconfigured SFTP server could previously trigger a ~4 GB allocation.
- **HTTP — response body limit**: `Get` caps the response body at 256 MB via
  `io.LimitReader`; responses that hit the cap return an error instead of
  exhausting process memory.
- **HTTP — client timeout**: `http.Client.Timeout` is now 5 minutes, preventing
  indefinite hangs when a server stalls after accepting the connection.
- **Credential filtering**: `filterCommandEnvironment` extended to strip
  `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`,
  `AWS_SECURITY_TOKEN`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`,
  `SFTP_PASSWORD`, `SFTP_KEY_PATH`, `HTTP_BEARER_TOKEN`, `HTTP_BASIC_PASSWORD`
  from child-process environments.

### Bug fixes

- Fixed tools-loop processing problem where the model's tool-call responses
  were not advancing the conversation correctly
- Fixed `write_file` tool failure: malformed JSON in model responses no longer
  silently drops the write; error is surfaced to the user
- Fixed missing help documentation for several commands added in v0.0.5

## v0.0.5 (2026-05-17)

- Initial public release
- Terminal REPL with Ollama backend
- Tool calling: `read_file`, `write_file`, `list_files`, `run_command`, `git`
- RAG store support (SQLite-backed vector search)
- Knowledge-base integration (`/kb` commands)
- Session recording to Fountain screenplay format (`.spmd`)
- Safe mode with command allowlist
- Multi-model routing (`/route`, `@name` dispatch)
- Skill dispatch system
- Cross-platform installers (Linux x86-64/aarch64/armv7l, macOS, Windows)
