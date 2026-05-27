
# Action Items

## Bugs

- [ ] The Onboarding for Harvey asks the name of the workspace. The workspace name is the  name of the directory where Harvey is running. No need to ask for the name.
- [ ] When I use the Qwen and LLama small models I am getting embedded JSON back not the functions in TypeScript or CSS I ask for

## Next Release

### Remote protocol integration

Design: [remote-protocol-design.md](remote-protocol-design.md) | Plan: [remote-protocol-plan.md](remote-protocol-plan.md)

- [ ] Phase 1: `RemoteReader` abstraction interface + URI scheme factory
- [ ] Phase 2: S3-compatible backend (MinIO Go client; AWS, MinIO, Cloudflare R2)
- [ ] Phase 3: S3 RAG ingest + `/read` + tab completion
- [ ] Phase 4: `filterCommandEnvironment` credential stripping (AWS/SFTP/HTTP vars)
- [ ] Phase 5: HTTP/HTTPS backend (stdlib `net/http`; downgrade protection)
- [ ] Phase 6: SFTP/SCP backend (`golang.org/x/crypto/ssh`; strict host key verification)

