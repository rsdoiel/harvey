
# Action Items

## Bugs

- [ ] When using a route the response is getting written into the status message rather then being buffered and returned when complete. ```harvey > /route list

  ✗  @francis           ollama      http://localhost:8080/api/v1/chat  [(default)]
  ✓  @wren              ollama      http://wren.local:11434  [qwen2.5-coder:7b]

harvey > @wren write a Hello World program in Oberon langauge.
  → dispatching to @wren

@wren · working
  ⎿ The Owl and the Pussycat sail by the light of thought...
     ⎿ ⠴ [11s]:
  ⎿ The Jumblies have gone to sea in a sieve to fetch your answer...
     ⎿ ⠙ [13s]
     ⎿ ⠸ [14s];

     ⎿ ⠼ [15s];

     ⎿ ⠏ [17s];
  ⎿ The Dong with the luminous nose searches through the dark...
     ⎿ ⠇ [21s]!");
     ⎿ ⠋ [22s];

     ⎿ ⠙ [23s].
     ⎿ ⠏ [24s]`
  ⎿ The Nutcrackers and the Sugar-Tongs are in conference...
     ⎿ ⠏ [36s].
  ⎿ The runcible spoon stirs the pot of possibilities...
     ⎿ ⠹ [46s]:

  ⎿ Far and few, far and few, the thoughts are gathering...
     ⎿ ⠙ [49s]
     ⎿ ⠇ [50s]`

     ⎿ ⠼ [53s]:

  ⎿ The Bong-tree sways as your answer takes its shape...
     ⎿ ⠹ [55s]

  @wren
harvey > ```

- [X] I don't think the RAG is function correctly, when I ran /rag setup the suggest embed didn't match what we discussed.
- [X] /rag command is missing a help guide in REPL
- [X] /apply command is missing a help guide in REPL
- [X] /clear command is missing a help guide in REPL
- [X] Ollama Alias doesn't work: ````harvey > /ollama alias apertus abb-decide/apertus-tools:8b-instruct-2509-q4_k_m
Unknown ollama subcommand: alias```


## Next Steps (upcoming features)

- [X] Add a 'debug' option when starting Ollama server for better diagnositics
- [X] The syntax of slash commands needs to be more unified, `/rag switch` should be more like  `/ollama use`
- [X] Revise how the Ollama models are listed by features. Example: the embedding only models should be grouped together and the tools/tagged models in a group
- [X] **`/read-dir` command** (`prompts/read_files.md`): Read all files in a directory into current context. Needs size/depth limits and security review.
- [X] **Keyboard behaviors — Ctrl+J** (`prompts/keyboard_behaviors.md`): Ctrl+J inserts a newline for multi-line input in termlib LineEditor; Enter submits. Backspace merges lines. History navigation disabled while in multi-line mode. Items still open: (b) `@`-prefix file autocompletion; (c) Ctrl+G (Ctrl+X Ctrl+E already covers editor launch).
- [X] **`/skill-set` command** (`prompts/skill-sets.md`, design doc `Harvey_Skill-Set_Design.md`): Load/unload named YAML bundles of skills from `agents/skill-sets/`. Validates skill names against catalog, counts tokens via Ollama `/api/tokenize` (heuristic fallback), warns at >50% context and blocks at >100%. Sample `agents/skill-sets/fountain.yaml` included.
- [X] **`fountain-session` skill**: `agents/skills/fountain-session/SKILL.md` guides agents on writing session handoff summaries in Fountain format. v1.1: cleaner `<PLACEHOLDER>` syntax, correct Harvey save workflow (auto-record + /rename or tagged code block), cross-reference to FOUNTAIN_FORMAT.md, optional knowledge-base step.
- [X] **Model selection guide**: `agents/model_guide.md` updated with current installed models from model_cache.db — tier structure, tools/tagged-block capability columns, task rubric, hardware constraints (M1 vs Pi 500+), and suggested model aliases.
- [ ] Explore adding an LSP service using Go lsp module for integrating Harvey with an IDE or text editor
- [X] **`/file-tree` command** (`prompts/read_files.md`): Display a directory tree (like Unix `tree`). Needs security review — restrict to workspace root.
- [X] **Remove `/apply`**: Redundant with always-on `autoExecuteReply`; removed from `commands.go`, `helptext.go`, `cmd/harvey/main.go`.
- [X] **`SupportsTaggedBlocks` probe**: Added live test to `ThoroughProbeModel` — sends a minimal `/api/generate` request and checks the response with `findTaggedBlocks`. Column added to `model_cache.db` with migration. Displayed in `/ollama probe` output alongside tools/embed.
- [X] Improve tool support with tool schema that can be communicated to the language model
- [X] Add a `/rename` command so I can rename the session files in Harvey like I do in Claude Code.
- [X] Change Harvey's directory from "harvey" to "agents" so that everything is shared with other agent tools.
- [X] Make sure all commands have a help guide
- [X] Make sure line editing and $EDITOR configuration have help guides
- [X] Continue Phase 3 & 4 for integrating Go from Mozilla AI GitHub projects — `any-llm-go v0.9.0` is fully integrated; all providers (Ollama, Llamafile, llama.cpp, Mistral, DeepSeek, Gemini, Anthropic, OpenAI) wired in `anyllm_client.go`
- [X] **Model aliases** (`prompts/model-aliases.md`): Short friendly names for long Ollama model identifiers (e.g. `qwen-coder` → `qwen2.5-coder:7b`). Store in `agents/harvey.yaml` under `model_aliases:` map. Resolve at model-selection time and record the full name in Fountain session headers.

## Security Tasks

- [X] **Skill sandboxing**: Implemented pure Go partial isolation for skill execution (env filtering via `filterSkillEnvironment()`, temp directory support, 30s timeouts via `exec.CommandContext()`, resource limits via memory-constrained execution). **Note**: Pure Go cannot provide full OS-level sandboxing - Linux namespaces/seccomp would require additional OS support. True isolation requires containers or similar. This limitation is documented in security documentation.
- [X] **Path traversal fix**: Hardened `Workspace.AbsPath()` to use `filepath.Clean()` and proper prefix checking with normalized path comparison
- [X] **Shell injection fix**: Replaced `sh -c` usage in `!` command with direct `exec.Command()` + custom `parseCommandLine()` parser that rejects shell metacharacters
- [X] **Command validation**: Added allowlist and confirmation prompts for `/run` command execution via Safe Mode; environment filtering applied to `/run` and `/git` commands
- [X] **Editor validation**: Added `validateEditorPath()` in skill_wizard.go rejecting shell metacharacters and path traversal; only allows known safe editors
- [X] **API key protection**: Implemented environment filtering throughout (sensitive variables: ANTHROPIC_API_KEY, DEEPSEEK_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY, MISTRAL_API_KEY, OPENAI_API_KEY). Error messages reference variable names only, never values.
- [X] **Information disclosure**: Added file exclusion patterns to workspace file tree exposure; workspace files restricted to workspace root

### Architectural Security Features (Implemented)

- [X] **Safe Mode**: Command allowlist system with `/safemode on|off|status|allow|deny|reset` commands. Default allowlist: ls, cat, grep, head, tail, wc, find, stat, jq, htmlq, bat, batcat
- [X] **Audit Logging**: In-memory ring buffer (1000 events) in `audit.go`. Commands: `/audit show [n]`, `/audit clear`, `/audit status`. Tracks: command, file_read, file_write, file_delete, file_list, skill_run, security events
- [X] **Permissions System**: Path prefix-based permissions stored in `harvey.yaml`. Commands: `/permissions list [PATH]`, `/permissions set PATH PERMS`, `/permissions reset`. Permissions: read, write, exec, delete
- [X] **Security Status**: Unified `/security status` command showing Safe Mode state, Workspace Permissions, Audit Buffer status, and attack surface summary

### Security Documentation

- [X] Created `harvey/agents/sessions/harvey_handoff_security_design.fountain` with complete handoff documentation
- [X] Updated this TODO.md with Security Tasks section

## Documentation Tasks

### Priority 1: Create Missing Documentation Files

- [X] **CONFIGURATION.md** - Document all configuration files and options (harvey.yaml, routes.json)
- [X] **SKILLS.md** - Skills system deep dive (SKILL.md format, discovery paths, compiled skills, triggers, skill wizard)
- [X] **ROUTING.md** - Remote endpoint routing (@mention syntax, endpoint types, configuration)
- [X] **KNOWLEDGE_BASE.md** - KB schema, FTS5 search, projects/observations/concepts model
- [X] **SESSIONS.md** - Session recording and Fountain format specification

### Priority 2: Expand Existing Documentation

- [X] **Using_RAGs_with_Harvey.md** - Expand from brief overview to comprehensive guide (borrow content from RAG_Support_Design.md)
- [X] **Add testing section** to user_manual.md or create TESTING.md (created TESTING.md)
- [X] **Update INSTALL.md** - Fix URLs (Laboratory.github.io → rsdoiel.github.io), remove publicai.co references (will be regenerated by cmt)

### Priority 3: Improve Go Code Documentation

- [X] Add package-level docs to model_cache.go
- [X] Add package-level docs to ollama.go
- [X] Add package-level docs to route_persist.go
- [X] Add package-level docs to skill_compile.go
- [X] Add package-level docs to skill_dispatch.go
- [X] Add package-level docs to skill_wizard.go
- [X] recorder.go — Already has package-level docs
- [X] sessions_files.go — Already has package-level docs
- [X] Add function-level docs to cmdOllama
- [X] Add function-level docs to cmdKB
- [X] Add function-level docs to cmdRag
- [X] Add function-level docs to cmdRoute
- [X] Add function-level docs to cmdRecord
- [X] Add function-level docs to cmdSession
- [X] Add function-level docs to cmdSkill
- [X] Document workspaceFileTree in harvey.go
- [X] Document workspaceGitStatus in harvey.go
- [X] ExpandDynamicSections already has documentation
- [X] LLMClient interface already has examples in documentation
- [X] Embedder interface already has examples in documentation

### Priority 4: Fix Outdated References

- [X] Search/replace `Laboratory.github.io` → `rsdoiel.github.io` across all files
- [X] Remove or disclaim publicai.co references (getting_started.md, user_manual.md, codemeta.json)
- [X] Update `.harvey/` → `agents/` directory references (workspace.go, route_persist.go, skills.go, tier2_test.go, ROUTING.md, CONFIGURATION.md, ARCHITECTURE.md, SESSIONS.md)
- [X] Fix HTML entities in about.md (&gt;&#x3D; → >=) — SKIPPED: cmt-generated file

### Priority 5: Organize Documentation

- [X] Create `docs/` directory to organize documentation
- [X] Create `DOCUMENTATION.md` that lists all docs with descriptions
- [X] Ensure all docs are linked from README.md or user_manual.md
- [X] Add MODEL_CACHE.md - Document capability probing, cache database, model metadata

## Security Follow-Up (2026-05-04)

### Code Fixes

- [X] **Issue 1 — Enforce permissions in file commands**: Added `CheckReadPermission` / `CheckWritePermission` checks to `cmdRead`, `cmdWrite`, and `cmdApply` in `commands.go`. Denied ops are now audit-logged with `StatusDenied`.

- [X] **Issue 2 — Consolidate duplicate env-filter functions**: `filterEnvironment` in `terminal.go` now delegates to `filterCommandEnvironment` in `commands.go`; one canonical implementation.

- [X] **Issue 3 — Persist SafeMode & AllowedCommands across sessions**: Added `safe_mode` and `allowed_commands` to `harveyYAML`; `LoadHarveyYAML` and `SaveRAGConfig` round-trip them. `safeModeOn/Off/Allow/Deny/Reset` all call `SaveRAGConfig`.

- [X] **Issue 4 — Configurable timeouts**: Added `RunTimeout` (default 5 min) and `OllamaTimeout` (default 0 = unlimited) to `Config`. Both are exposed as `run_timeout` / `ollama_timeout` in harvey.yaml and accept Go duration strings ("5m", "1m30s") or plain integer seconds. Critical bug fixed: any-llm-go Ollama provider had a hard-coded 120 s HTTP timeout that killed queries on slow hardware; now removed for local providers (Ollama, Llamafile, llama.cpp).

- [X] **Issue 5 — Atomic global audit buffer**: Replaced `var globalAuditBuffer *AuditBuffer` with `atomic.Pointer[AuditBuffer]` in `audit.go`.

### Documentation Fixes

- [X] **helptext.go — Fixed Agent Skills typos and URL**: "Anthoropic" → "Anthropic", "is document at" → "is documented at", unified URL to `https://agentskills.io/home`. Mirrored fixes in harvey.7.md.

- [X] **helptext.go / harvey.1.md — Added SECURITY section**: Documents `/safemode`, `/permissions`, `/audit`, `/security` commands and notes API key env-var filtering.

- [X] **codemeta.json — Improved releaseNotes**: Replaced vague sentence with four specific bullets describing security hardening and timeout changes.

- [X] **CONFIGURATION.md — Documented security configuration**: Added full Security Configuration subsection covering `safe_mode`, `allowed_commands`, `permissions` map (format, matching rules, example), `run_timeout`, and `ollama_timeout`.

## Pre-Release Checklist (2026-05-12)

### Security Hardening (v0.0.3 Release)

- [X] **Critical — `/run` command injection fix**: `cmdRun` already uses `parseCommandLine()` for argument validation
- [X] **High — Expand API key filtering**: `COHERE_API_KEY`, `GROQ_API_KEY`, `PERPLEXITY_API_KEY` already in both `skill_dispatch.go` and `commands.go`
- [X] **High — Add SSH key blocking**: `id_rsa`, `id_ed25519`, `authorized_keys` already in `sensitiveDenyPatterns` in `tools.go`
- [X] **Medium — TLS verification for routes**: Add warning when remote routes use HTTP (not HTTPS) for cloud providers in `routing.go`
- [X] **Medium — Install script security**: `release.bash` and `release.ps1` now generate and upload SHA256 checksums alongside release zips. INSTALL.md user-facing verification docs still pending (cmt-generated; needs codemeta.json update).
- [X] **Medium — Symlink hardening**: `resolveWorkspacePath` now rejects any path where `EvalSymlinks` changes the result — symbolic links are not followed inside the workspace
- [X] **Low — Tool input limits**: `builtin_tools.go` already has `maxInputPath`, `maxInputContent`, `maxInputPattern`, `maxInputCommand` constants with validation
- [X] **Low — Rate limiting**: `MaxToolCallsPerTurn` already enforced in `ToolExecutor`
- [X] **Documentation — Create SECURITY.md**: Document Harvey's security model, threats, and mitigations for users


