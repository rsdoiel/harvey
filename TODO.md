
# Action Items

## Bugs

- [ ] The keyboard/line edit handling does not appear as part of help guide suggested by `/help` command.

## Up Next (features for v0.0.6)

- [ ] Add built-in tool support for "whoami", this will be helpful in writing and project or writing review
- [ ] Increase the `/rag ingest` kilo byte limit that triggers a user prompt from 100K to 1000K
- [ ] There is an alias of `/rag setup` for `/rag new`, we can drop the alias.

## Someday, maybe ideas

### Remote protocol integration

Design: [remote-protocol-design.md](remote-protocol-design.md) | Plan: [remote-protocol-plan.md](remote-protocol-plan.md)

- [ ] Phase 1: `RemoteReader` abstraction interface + URI scheme factory
- [ ] Phase 2: S3-compatible backend (MinIO Go client; AWS, MinIO, Cloudflare R2)
- [ ] Phase 3: S3 RAG ingest + `/read` + tab completion
- [ ] Phase 4: `filterCommandEnvironment` credential stripping (AWS/SFTP/HTTP vars)
- [ ] Phase 5: HTTP/HTTPS backend (stdlib `net/http`; downgrade protection)
- [ ] Phase 6: SFTP/SCP backend (`golang.org/x/crypto/ssh`; strict host key verification)

