
# Action Items

## Bugs

- [ ] Make sure memory and pipeline commands have help guilds
  - Example output, ```wren:~/Laboratory/harvey $ harvey --help memory
Unknown help topic "memory".
Available topics: audit, clear, compact, context, editing, file-tree, files, git, inspect, kb, ollama, permissions, rag, read, read-dir, record, rename, routing, run, safemode, search, security, session, skill-set, skills, status, summarize, write
wren:~/Laboratory/harvey $ harvey --version
harvey 0.0.5b (released 2026-05-25, fc4ded8)
rsdoiel@wren:~/Laboratory/harvey $ harvey --help pipeline
Unknown help topic "pipeline".
Available topics: audit, clear, compact, context, editing, file-tree, files, git, inspect, kb, ollama, permissions, rag, read, read-dir, record, rename, routing, run, safemode, search, security, session, skill-set, skills, status, summarize, write
```

## Up Next (features for v0.0.6)

### Remote protocol integration

Design: [remote-protocol-design.md](remote-protocol-design.md) | Plan: [remote-protocol-plan.md](remote-protocol-plan.md)

- [ ] Phase 1: `RemoteReader` abstraction interface + URI scheme factory
- [ ] Phase 2: S3-compatible backend (MinIO Go client; AWS, MinIO, Cloudflare R2)
- [ ] Phase 3: S3 RAG ingest + `/read` + tab completion
- [ ] Phase 4: `filterCommandEnvironment` credential stripping (AWS/SFTP/HTTP vars)
- [ ] Phase 5: HTTP/HTTPS backend (stdlib `net/http`; downgrade protection)
- [ ] Phase 6: SFTP/SCP backend (`golang.org/x/crypto/ssh`; strict host key verification)

