# Harvey User Manual

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is a terminal-based coding agent backed by a local
[Ollama](https://ollama.com) server.
It provides an interactive REPL where you chat with a large language model
while also being able to read files, run commands, search code, and apply
suggested changes — all sandboxed to a single workspace directory.

> **Note:** Harvey focuses on local models via Ollama or Ollama servers
> running on the local network. It has limited support for remote endpoints (Anthropic, DeepSeek, Gemini, Mistral, OpenAI).

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
7. [Security](getting_started.md#security) — safe mode, permissions, audit log, API key filtering
8. [Typical workflows](getting_started.md#typical-workflows)
9. [Further reading](getting_started.md#further-reading)
10. [Models I'm using](models.md)
11. [Model evaluations](model_testing_plan.md)
12. [Using RAGS with Harvey](Using_RAGs_with_Harvey.md)


## Further reading

- [Llamafile Notes](llamafile_notes.md) - A short note on Llamafiles and Harvey planned integration
- [DOCUMENTATION.md](DOCUMENTATION.md) — master index of all Harvey documentation
- [CONFIGURATION.md](CONFIGURATION.md) — all harvey.yaml keys including security
  settings (safe mode, permissions, timeouts).
- [ARCHITECTURE.md](ARCHITECTURE.md) — detailed technical documentation:
  component map, core types, conversation model, backend implementations,
  security system, test coverage, and the full feature roadmap.
- [harvey.1.md](harvey.1.md) — man page source (generated from `harvey -help`).
- [GitHub Issues](https://github.com/rsdoiel/harvey/issues) — bug reports and
  feature requests.
