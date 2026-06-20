
# Action Items

## Bugs

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

- [ ] Unified `/model` command — `/model [list|use NAME|status]` that works regardless of whether the active backend is llamafile or Ollama, removing the mental load of choosing between `/llamafile` and `/ollama` subcommands
- [ ] `/llamafile remove` alias — add `remove` as an alias for the `drop` subcommand; `remove` is more natural English for most users
- [ ] `/llamafile download` stub — a command that prints a curated table of recommended models (name, size, HuggingFace URL, capability note) formatted for copy-pasting into `wget`/`curl`, removing the friction of finding the right file

### Session quality

- [ ] Record active model in session Fountain header — add `## Model: <name> (<backend>)` to the `.spmd` header so session reviews and memory mining have model provenance
- [ ] Health check on `--resume` — before loading the resumed session's context, verify the previously-active model is reachable; if not, prompt to restart it rather than silently continuing with a dead backend

### Command vocabulary consistency

Harvey's resource commands share a core set of verbs — `list`, `add`, `new`,
`use`, `show`, `edit`, `remove`, `rename`, `status` — but coverage is uneven
across command families. Making the vocabulary consistent lowers the learning
curve: knowing any one command family teaches you all the others.

**`remove` as the canonical delete verb (aliases kept for backwards compat):**
- [ ] `/rag remove` — alias for `drop`; make `remove` canonical
- [ ] `/route remove` — alias for `rm`; make `remove` canonical
- [ ] `/llamafile remove` — already planned above; confirm `drop` stays as alias

**Missing `show` subcommands (content/details display, distinct from `status`):**
- [ ] `/llamafile show [NAME]` — display registered model details: path, size, context length, capabilities from model cache
- [ ] `/rag show [NAME]` — display active (or named) store stats: chunk count, embedding model, DB path, last ingest date

**Missing `use` subcommand:**
- [ ] `/route use NAME` — set the named route as the default without requiring `@mention` syntax

**`/session` command expansion:**
- [ ] `/session list` — list available session files from `agents/sessions/` without restarting Harvey
- [ ] `/session show [PATH]` — display session metadata: date, model, backend, turn count, opening prompt
- [ ] `/session use PATH` (or `continue`) — alias `continue` as `use` to match the vocabulary; keep `continue` as alias

**Normalize `new`/`show` in skill commands:**
- [ ] `/skill show NAME` — alias for `info`; make `show` canonical, keep `info` as alias
- [ ] `/skill-set new NAME` — alias for `create`; make `new` canonical, keep `create` as alias
- [ ] `/skill-set show NAME` — alias for `info`; make `show` canonical, keep `info` as alias

**`/model alias` verb normalization:**
- [ ] `/model alias add ALIAS FULLNAME` — alias for `set`; make `add` the preferred verb, keep `set` as alias

**Documentation of the vocabulary:**
- [ ] Add a "Command vocabulary" section to `user_manual.md` and `getting-started.md` explaining the eight core verbs and the `add` vs `new` distinction so users can predict subcommands for any command family

### Documentation

- [ ] Restructure getting-started documentation to lead with Llamafile setup, then present Ollama as an advanced/alternative option
- [ ] Review all `.7.md` man pages and `.md` prose docs for coverage gaps introduced by v0.0.12–v0.0.13 features (spinner status, profile commands, `--resume`, routing feedback, memory enrichment)
- [ ] Audit cross-references: every new command/flag added since v0.0.11 should appear in at least one SEE ALSO section and in the user manual index
- [ ] Update CONFIGURATION.md to document new config fields added in v0.0.14 (LlamafileContextLength, etc.)

