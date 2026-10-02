
# Harvey

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is an agent REPL written in Go and designed to use Llamafile models or Ollama server to access language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and my personal language model agent.  Many people think of agents only in the context of very big companies. I think small models running on small or tiny computers are an opportunity. Harvey, is a small agent for small and tiny computers and is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.

## Security Note

Harvey is **experimental** — a **working proof of concept**, not production-ready software. Letting a probabilistic model direct command execution is an inherently risky attack surface; Harvey mitigates this with safe mode, workspace sandboxing, permission checks, audit logging, and security reviews with each release, but the risk is never zero. **Don't use Harvey where the risks might endanger your data, people, or planet.** See [SECURITY.md](SECURITY.md) for details.

## Features

### Language Model Support
- **Llamafile** (primary): register and run local model binaries — no server required
- **Ollama**: connect to a local Ollama server for broader model selection
- **Cloud routes**: Anthropic, DeepSeek, Gemini, Mistral, and OpenAI via configured routes
- Multi-model dispatch via `@mention` and model aliasing; routing feedback shown in spinner

### Core Capabilities
- Interactive terminal REPL sandboxed to a workspace directory
- Auto-execute: tagged code blocks in model replies are written to workspace files automatically
- `HARVEY.md` provides a customizable system prompt per workspace
- Context utilization hint `[ctx: N%]` in spinner when approaching the model's context window

### Knowledge, Memory & Sessions
- Three-silo memory: RAG vector stores, session-experience memory with rolling summaries, and a SQLite knowledge base
- Sessions recorded as human-readable Fountain screenplay (`.spmd`) files — replay, continue, or mine them for memories
- Model provenance recorded in session headers for audit and replay accuracy
- Pinned context and conversation summarization to manage the context window

### File & Code Support
- Code-aware RAG chunking, documentation extraction, and ANSI syntax highlighting (13 languages)
- Automatic code formatting on `write_file`: gofmt, clang-format, black, rustfmt, prettier, and built-in Pascal/Oberon/Basic formatters
- PDF text extraction via poppler; image reading via vision-capable model routes
- Remote RAG ingest: `s3://`, `sftp://`, `scp://`, `http://`, `https://` URIs
- File-reference injection: for models that ignore the tools schema, Harvey pre-injects workspace files mentioned in the prompt so they can still work with file content

### Extensibility
- SKILL.md skills, bundled skill sets, and multi-step prompt pipelines
- Git integration and per-workspace profile templates

## Quick Start

1. **Download a llamafile** from the [Mozilla AI pre-built models page](https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles) and make it executable
2. **Install Harvey**: run the installer for your platform
   - Linux/macOS: `./installer.sh`
   - Windows: run `installer.ps1` in PowerShell
3. **Run**: `harvey`
4. **Try it**:
   ```
   harvey > /model use
   harvey > /read LICENSE
   harvey > /help
   ```

See [Getting Started](getting_started.md) and [Installation](INSTALL.md) for detailed instructions.

## Platform Support

Harvey runs on:
- Linux: x86_64, aarch64, armv7l (including Raspberry Pi OS)
- macOS: Intel and Apple Silicon (M1 and above)
- Windows: x86_64

## Documentation

- [User Manual](user_manual.md) — Main documentation index
- [Overview](overview.md) — What Harvey is, why you might use it, and the design philosophy
- [Getting Started](getting_started.md) — First session, keyboard shortcuts, slash commands
- [Configuration Reference](CONFIGURATION.md) — harvey.yaml fields and environment variables
- [Installation](INSTALL.md) — Get Harvey installed on your system
- [Developer Guide](developer_guide.md) — Architecture, conventions, and contributing
- [Vision](vision.md) — Philosophy, motivation, and future direction
- [About](about.md) — Project metadata and version information
- [GitHub Repository](https://github.com/rsdoiel/harvey) — Source code and issues

## Release Notes

- version: 0.0.18
- status: active
- released: 2026-10-02

- Breaking for scripts: every slash command now returns a classed error instead of printing a failure and carrying on, so a non-interactive `harvey` session (piped stdin, or `--replay` without `--replay-continue`) exits with the class of its first failed command, where it used to exit 0 or 70. An unknown command is 2, a missing file or workspace 66, no matches or nothing to act on 1, a path or action refused by the permission rules 77, an unreachable backend 69, a file of the wrong kind 65, a setting that cannot be saved 74. `harvey/CHANGES.md` has the table for each command family. A session at a terminal is unaffected apart from the message now reading `Error: ...`. `/loop`, `/read`, `/format`, `/kb cite` and `/rag ingest` still try every item, then return the first failure
- `/model` and `/security` with an unknown subcommand are now usage errors; `/model NAME` used to show the active model and ignore the name
- Bug fix: `/workspace init FROM_PATH` never imported anything, because the handler read its subcommand from the wrong argument; `/workspace bogus` quietly showed the status
- Bug fix: `/learn` found no hand-offs after the workspace moved them to `agents/projects/<project>/hand-off/`; it now scans each project's `hand-off/` directory and accepts Markdown hand-offs
- `/profile use`'s automatic hand-off is now written as Markdown per `HANDOFF_FORMAT.md` instead of Fountain; `recallKB` retrieves through the knowledge library's `RecallByText`
- Design briefs, plans, decision records and notes moved out of this repository into `agents/projects/harvey/` (harvey DR-0008); 21 earlier decisions from `DECISIONS.md` are now records DR-0009 to DR-0029
- `knowledge` bumped to the released v0.0.15


### Authors

- Doiel, R. S.



## Software Requirements

- Llamafile v0.10 models or Ollama plus Ollama models

### Software Suggestions

- Go >= 1.26.4
- CMTools >= 0.0.45
- Pandoc >= 3.9
- GNU Make >= 3.8



## Getting Help & License

- [GitHub Issues](https://github.com/rsdoiel/harvey/issues) — Bug reports and feature requests
- [AGPL-3.0 License](https://www.gnu.org/licenses/agpl-3.0.txt)

