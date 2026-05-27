
# Action Items

## Bugs

- [x] The keyboard/line edit handling does not appear as part of help guide suggested by `/help` command.

## Up Next (features for v0.0.6)

- [x] Add built-in tool support for "whoami", this will be helpful in writing and project or writing review
- [x] Increase the `/rag ingest` kilo byte limit that triggers a user prompt from 100K to 1000K
- [x] There is an alias of `/rag setup` for `/rag new`, we can drop the alias.

## Unified Memory System

Design: [memory-unified-design.md](memory-unified-design.md) | Plan: [memory-unified-plan.md](memory-unified-plan.md)

### Phase 1 — Config restructure (split into safe sub-phases)
- [x] Phase 1a: Add `BudgetPct` and `RollingSummaryConfig` to `MemoryConfig`; update defaults
- [x] Phase 1b: Mirror RAG stores into `MemoryConfig`; add `memory.rag` YAML; add `SaveMemoryConfig`
- [x] Phase 1c: Migrate RAG call sites in `commands.go` / `harvey.go` to `cfg.Memory.*`
- [x] Phase 1d: Mirror KB (`KnowledgeDB`, `CurrentProjectID`) into `MemoryConfig`; migrate call sites
- [x] Phase 1e: Remove old top-level `Config.Rag*` / `Config.KnowledgeDB` fields; final cleanup

### Phase 2 — New types, unified retrieval, token budget
- [x] Add `workspace_profile` and `project_fact` `MemoryType` constants
- [x] Create `memory_unified.go`: `UnifiedResult`, `UnifiedMemory`, `Recall` with token budget
- [x] Replace `injectMemoryContext` with budget-aware unified injection
- [x] Add `/memory recall <query>` subcommand

### Phase 2b — Adaptive budget tuning
- [x] Add `memory_stats` table to `memories.db`; add `RecordSessionStats` to `MemoryStore`
- [x] Surface budget utilisation and compression rate in `/memory status`

### Phase 3 — Workspace profile onboarding
- [x] Create `memory_onboarding.go`: first-use detection, interview flow, `extractProjectFact`
- [x] Wire onboarding into session start (`Agent.Reset`)
- [x] Add `/memory profile show|update` subcommand

### Phase 4 — Rolling summary (working memory)
- [x] Create `memory_rolling.go`: `ShouldCompress`, `CompressHistory`
- [x] Wire post-reply compression check into REPL loop
- [x] Add `rolling_summary:` to YAML load/save

### Phase 5 — Auto-mine on session end
- [x] Add `MineAuto` to `memory_miner.go`
- [x] Trigger auto-mine on exit when session has >= 10 turns

---

## Someday, maybe ideas

### Remote protocol integration

Design: [remote-protocol-design.md](remote-protocol-design.md) | Plan: [remote-protocol-plan.md](remote-protocol-plan.md)

- [ ] Phase 1: `RemoteReader` abstraction interface + URI scheme factory
- [ ] Phase 2: S3-compatible backend (MinIO Go client; AWS, MinIO, Cloudflare R2)
- [ ] Phase 3: S3 RAG ingest + `/read` + tab completion
- [ ] Phase 4: `filterCommandEnvironment` credential stripping (AWS/SFTP/HTTP vars)
- [ ] Phase 5: HTTP/HTTPS backend (stdlib `net/http`; downgrade protection)
- [ ] Phase 6: SFTP/SCP backend (`golang.org/x/crypto/ssh`; strict host key verification)

