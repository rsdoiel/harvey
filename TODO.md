
# Action Items

## Bugs

## Release Review

- [ ] README.md is stale and needs to be brought up to date for v0.0.14 improvements
- [x] The harvey.1.md `/read` now handles the explicit `/read-pdf` command — `cmdRead` now auto-detects `.pdf` files and extracts text via poppler (consistent with `read_file` tool); `ReadHelpText` documents this; `harvey-read.7.md` regenerated
- [ ] Check for broken links in Markdown pages (example some of the skills links are broken)
- [ ] Identify missing content (example Harvey skills) and review what can be done to fix the problem
- [ ] Some of the diagrams written with UTF-8 corner and line characters had aslignment issues for the virtical bars and corners on the right side

## Next (v0.0.14 release)

### Llamafile as primary model system

- [x] Llamafile becomes primary model system alongside Ollama models
  - [x] In startup process before prompting user, detect what model systems are available — `selectBackend` probes llamafile first, then Ollama
  - [x] If a session is not being continued, present available Llamafiles as the first choice and Ollama models (if available) next — `pickBackend` shows registered llamafiles before Ollama models
  - [ ] Bring Llamafile support into parity for advanced features like pipelines and routing
  - [x] Update documentation to present Llamafile support for basic operation, then include an advanced section for working with Ollama models — getting_started.md and user_manual.md restructured


