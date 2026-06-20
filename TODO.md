
# Action Items

## Bugs

## Release Review

- [ ] Overview.md still reflects a Ollama first approach, needs to cover Llamafile support before talking about Ollama model support.
- [ ] The helptext.go help guides (help text) seem to still be oriented as Ollama first, needs to be LLamafile primary while still supporting Ollama models. See `./bin/harvery --help getting-started` output as an example
- [ ] Review the helptext.go file, review the help text constants to ensure they align with the current implementation. 
- [ ] Each exported constant needs a brief comment identifying it's purpose as well as related man page output. 
- [ ] Order the help texts in helptext.go to make it easy for humans to review. Example the helptext that is used to generate harvey.1 man page, should be at the top. The other help text gguides should be grouped around what they cover.

## Next (v0.0.14 release)

### Llamafile as primary model system

- [ ] Llamafile becomes primary model system alongside Ollama models
  - [ ] In startup process before prompting user, detect what model systems are available
  - [ ] If a session is not being continued, present available Llamafiles as the first choice and Ollama models (if available) next
  - [ ] Bring Llamafile support into parity for advanced features like pipelines and routing
  - [ ] Update documentation to present Llamafile support for basic operation, then include an advanced section for working with Ollama models

### Startup & connection

- [ ] Explicit connection feedback — show "Connecting to `<model>` (llamafile)… ✓" during startup instead of silent connection; important for slower hardware where users can't tell if Harvey is waiting on the model or hung
- [ ] First-run onboarding when no model is found — if neither a llamafile nor Ollama is reachable, run a guided mini-wizard: print the HuggingFace Mozilla model list URL and guide the user into a `/llamafile add` flow rather than dropping to an error
- [ ] Stale external server adoption — when `/llamafile add` finds a server already running that Harvey didn't start, probe its `/v1/models` endpoint, identify which model it's serving, and offer to adopt it (register as active) rather than just warning and bailing

### Mid-session awareness

- [ ] Auto-reconnect on dropped llamafile — detect when the llamafile process dies mid-session (crash, OOM), and on the next prompt offer to restart it with the same model rather than presenting an opaque API error
- [ ] Context utilization hint — show a subtle `[ctx: 72%]` indicator in the status line or spinner for smaller llamafiles (4B–8B) with tight context windows, so users know when to `/clear`
- [ ] Routing feedback in spinner — when multi-model routing is active, show which model handled the turn (e.g. `routed → coding-model`) in the transient spinner status to make routing transparent and easier to tune
- [ ] At-mention model switch — if the command prompt starts with `@modelname`, treat it as a model switch while preserving existing context in the environment

### Model management ergonomics

- [x] Unified `/model` command — `/model [list|use NAME|show [NAME]|status]` implemented; works regardless of backend (llamafile.go + commands.go:415)
- [x] `/llamafile remove` alias — `case "drop", "remove":` in llamafile.go:192; `drop` kept as alias
- [x] `/llamafile download` stub — `case "download":` in llamafile.go:194; `LlamafileDownloadText` in helptext.go

### Session quality

- [ ] Record active model in session Fountain header — add `## Model: <name> (<backend>)` to the `.spmd` header so session reviews and memory mining have model provenance
- [ ] Health check on `--resume` — before loading the resumed session's context, verify the previously-active model is reachable; if not, prompt to restart it rather than silently continuing with a dead backend

### Command vocabulary consistency

Harvey's resource commands share a core set of verbs — `list`, `add`, `new`,
`use`, `show`, `edit`, `remove`, `rename`, `status` — but coverage is uneven
across command families. Making the vocabulary consistent lowers the learning
curve: knowing any one command family teaches you all the others.

**`remove` as the canonical delete verb (aliases kept for backwards compat):**
- [x] `/rag remove` — implemented; `case "drop", "remove":` at commands.go:4810
- [x] `/route remove` — implemented; in subcommand list and handler at commands.go:966
- [x] `/llamafile remove` — implemented; see Model management ergonomics above

**Missing `show` subcommands (content/details display, distinct from `status`):**
- [x] `/llamafile show [NAME]` — implemented in llamafile.go:cmdLlamafileShow; shows path, size, context length
- [x] `/rag show [NAME]` — implemented in commands.go:ragShow; shows db path, embed model, chunk count, model map

**Missing `use` subcommand:**
- [x] `/route use NAME` — implemented; `case "use":` at commands.go:1006; also clears sticky route when NAME omitted

**`/session` command expansion:**
- [x] `/session list` — implemented; lists .spmd files from agents/sessions/ with timestamps
- [x] `/session show [PATH]` — implemented at commands.go:3792; shows date, model, turn count, opening prompt
- [x] `/session use PATH` (or `continue`) — implemented; `case "use", "continue":` at commands.go:3823

**Normalize `new`/`show` in skill commands:**
- [x] `/skill show NAME` — implemented; `case "info", "show":` at commands.go:3932
- [x] `/skill-set new NAME` — implemented; `case "create", "new":` at commands.go:4179
- [x] `/skill-set show NAME` — implemented; `case "info", "show":` at commands.go:4173

**`/model alias` verb normalization:**
- [x] `/model alias add ALIAS FULLNAME` — implemented; `add` is preferred verb, `set` kept as alias; documented in ModelAliasHelpText

**Documentation of the vocabulary:**
- [x] "Command vocabulary" section added to both `user_manual.md` and `getting_started.md` with eight-verb table and `add` vs `new` distinction

### Documentation

- [ ] Restructure getting-started documentation to lead with Llamafile setup, then present Ollama as an advanced/alternative option
- [ ] Review all `.7.md` man pages and `.md` prose docs for coverage gaps introduced by v0.0.12–v0.0.13 features (spinner status, profile commands, `--resume`, routing feedback, memory enrichment)
- [ ] Audit cross-references: every new command/flag added since v0.0.11 should appear in at least one SEE ALSO section and in the user manual index
- [ ] Update CONFIGURATION.md to document new config fields added in v0.0.14 (LlamafileContextLength, etc.)

