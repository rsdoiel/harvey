# Harvey User Manual

Harvey is a terminal-based coding agent backed by a local
[Ollama](https://ollama.com) server or [publicai.co](https://publicai.co).
It provides an interactive REPL where you chat with a large language model
while also being able to read files, run commands, search code, and apply
suggested changes — all sandboxed to a single workspace directory.

---

## Contents

1. [Installation](INSTALL.md)
2. [Getting started](getting_started.md)
3. [The HARVEY.md system prompt](getting_started.md#the-harveymd-system-prompt)
4. [Session walkthrough](getting_started.md#session-walkthrough)
5. [Keyboard shortcuts](getting_started.md#keyboard-shortcuts)
6. [Slash commands](getting_started.md#slash-commands)
   - [Session](getting_started.md#session-commands)
   - [Backends](getting_started.md#backend-commands)
   - [File operations](getting_started.md#file-operations-tier-1)
   - [Code assistance](getting_started.md#code-assistance-tier-2)
   - [Session quality](getting_started.md#session-quality-tier-3)
   - [Knowledge base](getting_started.md#knowledge-base-commands)
   - [Recording](getting_started.md#recording-commands)
7. [Typical workflows](getting_started.md#typical-workflows)
8. [Further reading](getting_started.md#further-reading)
9. [Model evaluation](models.md)


## Further reading

- [ARCHITECTURE.md](ARCHITECTURE.md) — detailed technical documentation:
  component map, core types, conversation model, backend implementations,
  test coverage, and the full feature roadmap.
- [harvey.1.md](harvey.1.md) — man page source (generated from `harvey -help`).
- [README.md](README.md) — project overview and quick-start.
- [INSTALL.md](INSTALL.md) — installation instructions for all platforms.
- [about.md](about.md) — project metadata.
- [GitHub Issues](https://github.com/rsdoiel/harvey/issues) — bug reports and
  feature requests.
