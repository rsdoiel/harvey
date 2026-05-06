
# Action Items

## Bugs

- [X] I don't think the RAG is function correctly, when I ran /rag setup the suggest embed didn't match what we discussed.
- [X] /rag command is missing a help guide in REPL
- [X] /apply command is missing a help guide in REPL
- [X] /clear command is missing a help guide in REPL

## Next Steps

- [X] Change Harvey's directory from "harvey" to "agents" so that everything is shared with other agent tools.
- [X] Make sure all commands have a help guide
- [X] Make sure line editing and $EDITOR configuration have help guides
- [X] Continue Phase 3 & 4 for integrating Go from Mozilla AI GitHub projects — `any-llm-go v0.9.0` is fully integrated; all providers (Ollama, Llamafile, llama.cpp, Mistral, DeepSeek, Gemini, Anthropic, OpenAI) wired in `anyllm_client.go`
- [ ] Explore adding an LSP service using Go lsp module for integrating Harvey with an IDE or text editor

## Upcoming Features

- [ ] **Model aliases** (`prompts/model-aliases.md`): Short friendly names for long Ollama model identifiers (e.g. `qwen-coder` → `qwen2.5-coder:7b`). Store in `agents/harvey.yaml` under `model_aliases:` map. Resolve at model-selection time and record the full name in Fountain session headers.
- [ ] **`/file-tree` command** (`prompts/read_files.md`): Display a directory tree (like Unix `tree`). Needs security review — restrict to workspace root.
- [ ] **`/read-dir` command** (`prompts/read_files.md`): Read all files in a directory into current context. Needs size/depth limits and security review.
- [ ] **Keyboard behaviors** (`prompts/keyboard_behaviors.md`): (a) Ctrl+J / Shift+Enter for multi-line input; (b) `@`-prefix file autocompletion; (c) Ctrl+G to open `$EDITOR`. Items (a) and (b) belong in termlib; (c) may already be partially covered by the Ctrl+X Ctrl+E chord — needs investigation.
- [ ] **`/skill-set` command** (`prompts/skill-sets.md`, design doc `Harvey_Skill-Set_Design.md`): Load/unload named YAML bundles of skills from `agents/skill-sets/`. Validate triggers, calculate token cost via Ollama `/api/tokenize`.
- [ ] **`fountain-session` skill**: Create `agents/skills/fountain-session/SKILL.md` to guide agents (especially Mistral Vibe) on how to *write* a well-formed session handoff summary in Fountain format. Currently only the analysis side (`fountain-analysis`) exists.
- [X] **Remove `/apply`**: Redundant with always-on `autoExecuteReply`; removed from `commands.go`, `helptext.go`, `cmd/harvey/main.go`.
- [X] **`SupportsTaggedBlocks` probe**: Added live test to `ThoroughProbeModel` — sends a minimal `/api/generate` request and checks the response with `findTaggedBlocks`. Column added to `model_cache.db` with migration. Displayed in `/ollama probe` output alongside tools/embed.
- [ ] **Model selection guide**: Document which installed Ollama models are appropriate for which Harvey task types. See `agents/model_guide.md` (to be created).

## Security Tasks

- [x] **Skill sandboxing**: Implemented pure Go partial isolation for skill execution (env filtering via `filterSkillEnvironment()`, temp directory support, 30s timeouts via `exec.CommandContext()`, resource limits via memory-constrained execution). **Note**: Pure Go cannot provide full OS-level sandboxing - Linux namespaces/seccomp would require additional OS support. True isolation requires containers or similar. This limitation is documented in security documentation.
- [x] **Path traversal fix**: Hardened `Workspace.AbsPath()` to use `filepath.Clean()` and proper prefix checking with normalized path comparison
- [x] **Shell injection fix**: Replaced `sh -c` usage in `!` command with direct `exec.Command()` + custom `parseCommandLine()` parser that rejects shell metacharacters
- [x] **Command validation**: Added allowlist and confirmation prompts for `/run` command execution via Safe Mode; environment filtering applied to `/run` and `/git` commands
- [x] **Editor validation**: Added `validateEditorPath()` in skill_wizard.go rejecting shell metacharacters and path traversal; only allows known safe editors
- [x] **API key protection**: Implemented environment filtering throughout (sensitive variables: ANTHROPIC_API_KEY, DEEPSEEK_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY, MISTRAL_API_KEY, OPENAI_API_KEY). Error messages reference variable names only, never values.
- [x] **Information disclosure**: Added file exclusion patterns to workspace file tree exposure; workspace files restricted to workspace root

### Architectural Security Features (Implemented)

- [x] **Safe Mode**: Command allowlist system with `/safemode on|off|status|allow|deny|reset` commands. Default allowlist: ls, cat, grep, head, tail, wc, find, stat, jq, htmlq, bat, batcat
- [x] **Audit Logging**: In-memory ring buffer (1000 events) in `audit.go`. Commands: `/audit show [n]`, `/audit clear`, `/audit status`. Tracks: command, file_read, file_write, file_delete, file_list, skill_run, security events
- [x] **Permissions System**: Path prefix-based permissions stored in `harvey.yaml`. Commands: `/permissions list [PATH]`, `/permissions set PATH PERMS`, `/permissions reset`. Permissions: read, write, exec, delete
- [x] **Security Status**: Unified `/security status` command showing Safe Mode state, Workspace Permissions, Audit Buffer status, and attack surface summary

### Security Documentation

- [x] Created `harvey/agents/sessions/harvey_handoff_security_design.fountain` with complete handoff documentation
- [x] Updated this TODO.md with Security Tasks section

## Documentation Tasks

### Priority 1: Create Missing Documentation Files
- [x] **CONFIGURATION.md** - Document all configuration files and options (harvey.yaml, routes.json)
- [x] **SKILLS.md** - Skills system deep dive (SKILL.md format, discovery paths, compiled skills, triggers, skill wizard)
- [x] **ROUTING.md** - Remote endpoint routing (@mention syntax, endpoint types, configuration)
- [x] **KNOWLEDGE_BASE.md** - KB schema, FTS5 search, projects/observations/concepts model
- [x] **SESSIONS.md** - Session recording and Fountain format specification

### Priority 2: Expand Existing Documentation
- [x] **Using_RAGs_with_Harvey.md** - Expand from brief overview to comprehensive guide (borrow content from RAG_Support_Design.md)
- [x] **Add testing section** to user_manual.md or create TESTING.md (created TESTING.md)
- [x] **Update INSTALL.md** - Fix URLs (Laboratory.github.io → rsdoiel.github.io), remove publicai.co references (will be regenerated by cmt)

### Priority 3: Improve Go Code Documentation
- [x] Add package-level docs to model_cache.go
- [x] Add package-level docs to ollama.go
- [x] Add package-level docs to route_persist.go
- [x] Add package-level docs to skill_compile.go
- [x] Add package-level docs to skill_dispatch.go
- [x] Add package-level docs to skill_wizard.go
- [x] recorder.go — Already has package-level docs
- [x] sessions_files.go — Already has package-level docs
- [x] Add function-level docs to cmdOllama
- [x] Add function-level docs to cmdKB
- [x] Add function-level docs to cmdRag
- [x] Add function-level docs to cmdRoute
- [x] Add function-level docs to cmdRecord
- [x] Add function-level docs to cmdSession
- [x] Add function-level docs to cmdSkill
- [x] Document workspaceFileTree in harvey.go
- [x] Document workspaceGitStatus in harvey.go
- [x] ExpandDynamicSections already has documentation
- [x] LLMClient interface already has examples in documentation
- [x] Embedder interface already has examples in documentation

### Priority 4: Fix Outdated References
- [x] Search/replace `Laboratory.github.io` → `rsdoiel.github.io` across all files
- [x] Remove or disclaim publicai.co references (getting_started.md, user_manual.md, codemeta.json)
- [x] Update `.harvey/` → `agents/` directory references (workspace.go, route_persist.go, skills.go, tier2_test.go, ROUTING.md, CONFIGURATION.md, ARCHITECTURE.md, SESSIONS.md)
- [x] Fix HTML entities in about.md (&gt;&#x3D; → >=) — SKIPPED: cmt-generated file

### Priority 5: Organize Documentation
- [x] Create `docs/` directory to organize documentation
- [x] Create `DOCUMENTATION.md` that lists all docs with descriptions
- [x] Ensure all docs are linked from README.md or user_manual.md
- [x] Add MODEL_CACHE.md - Document capability probing, cache database, model metadata

## Security Follow-Up (2026-05-04)

### Code Fixes

- [x] **Issue 1 — Enforce permissions in file commands**: Added `CheckReadPermission` / `CheckWritePermission` checks to `cmdRead`, `cmdWrite`, and `cmdApply` in `commands.go`. Denied ops are now audit-logged with `StatusDenied`.

- [x] **Issue 2 — Consolidate duplicate env-filter functions**: `filterEnvironment` in `terminal.go` now delegates to `filterCommandEnvironment` in `commands.go`; one canonical implementation.

- [x] **Issue 3 — Persist SafeMode & AllowedCommands across sessions**: Added `safe_mode` and `allowed_commands` to `harveyYAML`; `LoadHarveyYAML` and `SaveRAGConfig` round-trip them. `safeModeOn/Off/Allow/Deny/Reset` all call `SaveRAGConfig`.

- [x] **Issue 4 — Configurable timeouts**: Added `RunTimeout` (default 5 min) and `OllamaTimeout` (default 0 = unlimited) to `Config`. Both are exposed as `run_timeout` / `ollama_timeout` in harvey.yaml and accept Go duration strings ("5m", "1m30s") or plain integer seconds. Critical bug fixed: any-llm-go Ollama provider had a hard-coded 120 s HTTP timeout that killed queries on slow hardware; now removed for local providers (Ollama, Llamafile, llama.cpp).

- [x] **Issue 5 — Atomic global audit buffer**: Replaced `var globalAuditBuffer *AuditBuffer` with `atomic.Pointer[AuditBuffer]` in `audit.go`.

### Documentation Fixes

- [x] **helptext.go — Fixed Agent Skills typos and URL**: "Anthoropic" → "Anthropic", "is document at" → "is documented at", unified URL to `https://agentskills.io/home`. Mirrored fixes in harvey.7.md.

- [x] **helptext.go / harvey.1.md — Added SECURITY section**: Documents `/safemode`, `/permissions`, `/audit`, `/security` commands and notes API key env-var filtering.

- [x] **codemeta.json — Improved releaseNotes**: Replaced vague sentence with four specific bullets describing security hardening and timeout changes.

- [x] **CONFIGURATION.md — Documented security configuration**: Added full Security Configuration subsection covering `safe_mode`, `allowed_commands`, `permissions` map (format, matching rules, example), `run_timeout`, and `ollama_timeout`.


