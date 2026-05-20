
# Action Items

## Bugs

- [ ] **Model cannot write files to workspace** — When asked to create a file, models respond
  with a markdown code block instead of writing to disk. Two causes: (1) no write-capable
  command in `allowed_commands` (only read-only tools like `ls`, `cat`, `grep` are listed),
  and (2) small local models (e.g. APERTUS-TOOLS) do not reliably invoke tool-call protocol
  for action requests. Debug log confirms zero `tool_call`/`command_exec` events during a
  full session where the user explicitly requested file creation. See
  [dev-notes/file-write-failure-diagnosis-20260520.md](dev-notes/file-write-failure-diagnosis-20260520.md).
  Possible fixes: add a `write_file` built-in tool to the tool registry; add `tee` to
  `allowed_commands`; implement code-block extraction with an interactive write prompt.

## Up Next (features for v0.0.6)

- [ ] Add in built-in tool support dates, times and durations


## Someday, maybe ideas

### Prompt pipeline
Design: [pipeline-design.md](pipeline-design.md) | Plan: [pipeline-plan.md](pipeline-plan.md)

- [ ] `/pipeline <CONFIDENCE%> FILE [FILE ...]` — chain Markdown prompt files through models; each step's response feeds the next; confidence threshold gates progression; @mention routes individual steps to specific models; final response appended to Harvey's active conversation on success

### Remote protocol integration
Design: [remote-protocol-design.md](remote-protocol-design.md) | Plan: [remote-protocol-plan.md](remote-protocol-plan.md)

- [ ] Phase 1: `RemoteReader` abstraction interface + URI scheme factory
- [ ] Phase 2: S3-compatible backend (MinIO Go client; AWS, MinIO, Cloudflare R2)
- [ ] Phase 3: S3 RAG ingest + `/read` + tab completion
- [ ] Phase 4: `filterCommandEnvironment` credential stripping (AWS/SFTP/HTTP vars)
- [ ] Phase 5: HTTP/HTTPS backend (stdlib `net/http`; downgrade protection)
- [ ] Phase 6: SFTP/SCP backend (`golang.org/x/crypto/ssh`; strict host key verification)

