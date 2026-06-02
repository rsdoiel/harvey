# CHANGES

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

## v0.0.5c (2026-05-30)

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
