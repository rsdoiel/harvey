
# Action Items

## Bugs

## Release Review

- [x] the user_manual.md is stale, still referring to Ollama first approach — fixed; llamafile-first framing throughout
- [x] Overview.md still reflects a Ollama first approach — rewritten with natural language programming / scholarly work framing
- [x] The helptext.go help guides (help text) seem to still be oriented as Ollama first — HelpText description updated; getting_started guide restructured
- [x] Review the helptext.go file, review the help text constants to ensure they align with the current implementation — reviewed and updated throughout this release
- [x] Each exported constant needs a brief comment identifying it's purpose as well as related man page output — all 41 constants commented
- [x] Order the help texts in helptext.go to make it easy for humans to review — reordered into 11 logical groups; HelpText (harvey.1) is now first

## Next (v0.0.14 release)

### Llamafile as primary model system

- [x] Llamafile becomes primary model system alongside Ollama models
  - [x] In startup process before prompting user, detect what model systems are available — `selectBackend` probes llamafile first, then Ollama
  - [x] If a session is not being continued, present available Llamafiles as the first choice and Ollama models (if available) next — `pickBackend` shows registered llamafiles before Ollama models
  - [ ] Bring Llamafile support into parity for advanced features like pipelines and routing
  - [x] Update documentation to present Llamafile support for basic operation, then include an advanced section for working with Ollama models — getting_started.md and user_manual.md restructured

### Startup & connection

- [x] Explicit connection feedback — "Connecting to `<model>` (llamafile)… ✓" in `selectBackend` and `startAndUseLlamafile`; dots from `StartLlamafileService` during wait; "✓ Ready" when server becomes available
- [x] First-run onboarding when no model is found — `runFirstRunWizard` fires when `pickBackend` finds neither llamafiles nor Ollama; guides user to `/llamafile add`
- [x] Stale external server adoption — `startAndUseLlamafile` calls `probeRunningLlamafileName`; warns and adopts the detected model when it differs from the configured entry; `adoptExternalServer` handles `/llamafile add` case

### Mid-session awareness

- [x] Auto-reconnect on dropped llamafile — `isConnectionError` detects transport failures; REPL loop offers restart via `restartActiveLlamafile` and retries the turn; implemented at terminal.go:917
- [x] Context utilization hint — `spinnerLabel()` in harvey.go shows `[ctx: N%]` when estimated usage ≥ 50% of `effectiveContextLimit()`
- [x] Routing feedback in spinner — `routeSpinnerLabel` shows `"@name · model"` in spinner label; `UpdateStatus("routed → name")` on line 2
- [ ] At-mention model switch — if the command prompt starts with `@modelname`, treat it as a model switch while preserving existing context in the environment

### Model management ergonomics

- [x] Unified `/model` command — `/model [list|use NAME|show [NAME]|status]` implemented; works regardless of backend (llamafile.go + commands.go:415)
- [x] `/llamafile remove` alias — `case "drop", "remove":` in llamafile.go:192; `drop` kept as alias
- [x] `/llamafile download` stub — `case "download":` in llamafile.go:194; `LlamafileDownloadText` in helptext.go

### Session quality

- [x] Record active model in session Fountain header — `Model:` title page field now stores `"NAME (backend)"` (e.g. `QWEN-CODING (llamafile)`); `parseFountainSession` strips the suffix for auto-selection; implemented in recorder.go and replay.go
- [x] Health check on `--resume` — session model extracted from `ContinuePath` before `selectBackend` so the right backend is auto-selected; mismatch warning shown after connect if models differ

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

- [x] Restructure getting-started documentation to lead with Llamafile setup, then present Ollama as an advanced/alternative option — getting_started.md completely restructured
- [x] Review all `.7.md` man pages and `.md` prose docs for coverage gaps — 4 gaps fixed (--resume, /profile, /safe alias, spinner tool-call status)
- [x] Audit cross-references: every new command/flag added since v0.0.11 should appear in at least one SEE ALSO section and in the user manual index — 12 SEE ALSO sections added/updated; 20 links added to user_manual.md
- [x] Update CONFIGURATION.md to document new config fields added in v0.0.14 — model_aliases, llamafile, tools, memory, rolling_summary, syntax_highlight, auto_format all documented

