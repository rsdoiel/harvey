
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
- [ ] Explore adding an LSP service suing Go lsp module for intergrating Harvey with an IDE or text editor
- [ ] Continue Phase 3 & 4 for integrating Go from Mozilla AI GitHub priojects

## Documentation Tasks

### Priority 1: Create Missing Documentation Files
- [ ] **CONFIGURATION.md** - Document all configuration files and options (harvey.yaml, routes.json)
- [ ] **SKILLS.md** - Skills system deep dive (SKILL.md format, discovery paths, compiled skills, triggers, skill wizard)
- [ ] **ROUTING.md** - Remote endpoint routing (@mention syntax, endpoint types, configuration)
- [ ] **KNOWLEDGE_BASE.md** - KB schema, FTS5 search, projects/observations/concepts model
- [ ] **SESSIONS.md** - Session recording and Fountain format specification

### Priority 2: Expand Existing Documentation
- [ ] **Using_RAGs_with_Harvey.md** - Expand from brief overview to comprehensive guide (borrow content from RAG_Support_Design.md)
- [ ] **Add testing section** to user_manual.md or create TESTING.md
- [ ] **Update INSTALL.md** - Fix URLs (Laboratory.github.io → rsdoiel.github.io), remove publicai.co references

### Priority 3: Improve Go Code Documentation
- [ ] Add package-level docs to files missing them: learner_messages.go, model_cache.go, ollama.go, ollama_probe.go, recorder.go, route_persist.go, skill_compile.go, skill_dispatch.go, skill_wizard.go, sessions_files.go
- [ ] Add function-level docs to command handlers in commands.go (cmdOllama, cmdKB, cmdRag, cmdRoute, cmdRecord, cmdSession, cmdSkill)
- [ ] Document internal helper functions in harvey.go (workspaceFileTree, workspaceGitStatus)
- [ ] Add examples to interface documentation (LLMClient, Embedder, etc.)

### Priority 4: Fix Outdated References
- [ ] Search/replace `Laboratory.github.io` → `rsdoiel.github.io` across all files
- [ ] Remove or disclaim publicai.co references
- [ ] Update `.harvey/` → `harvey/` or `agents/` directory references
- [ ] Fix HTML entities in about.md (&gt;&#x3D; → >=)

### Priority 5: Organize Documentation
- [ ] Create `docs/` directory to organize documentation
- [ ] Create `DOCUMENTATION.md` that lists all docs with descriptions
- [ ] Ensure all docs are linked from README.md or user_manual.md
- [ ] Add MODEL_CACHE.md - Document capability probing, cache database, model metadata


