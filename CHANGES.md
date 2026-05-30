# CHANGES

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
