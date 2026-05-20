
# Action Items

## Bugs

## Next Steps (upcoming features, v0.0.5)

- [x] The file tree built-in command is missing
- [x] The `/rag ingest` method should be able to read the documents in the folder and ingest them one by one reporting progress. If the documents are large (> 100K), the ingest command to show the list of documents to be read and confirm before starting through the list

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

