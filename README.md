
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

- version: 0.0.17
- status: active
- released: 2026-09-27

- Breaking: `harvey` and `assay` now use the workspace exit-code convention (workspace DR-0003; harvey DR-0005/DR-0006) instead of exiting only 0 or 1. Five cases that exited 0 now fail (`--continue`/`--record-file` naming a bad path, a malformed `harvey.yaml` or no reachable backend in a non-interactive session, and `assay` when every model call fails); every other case exits a more specific code than a bare 1. See `harvey/CHANGES.md`'s v0.0.17 section for the full upgrade table
- `-json`/`--json` on both binaries: a failing startup/flag-parsing error (or, for `harvey`, a failed non-interactive session) prints as `{"error","class","code"}` JSON on stderr instead of text, matching `kb`'s shape
- A non-interactive `harvey` session (piped stdin, or `--replay` without `--replay-continue`) now exits with the class of its first failed slash command or chat turn, instead of always 0; an interactive session at a terminal is unaffected. `/plan`'s errors are the first command family reclassified to give real signal here
- `/rag setup`'s embedding-model auto-pick now confirms its keyword-based guess with one live `/api/embed` call before committing to it, falling back to another candidate (or warning) when the guess is wrong — `ThoroughProbeModel`'s first caller
- Ollama models are now probed on every selection, not just when a new alias is saved, so `/model use NAME` and the startup picker no longer silently degrade to file-injection instead of tool calls; a `/model mode` override survives re-probing (previously erased)
- Removed roughly 30 stale `/ollama`/`/llamafile` references across runtime messages, help text and generated docs; both commands were replaced by `/model` months ago. `stale_commands_test.go` guards against regressions
- Bug fix: closed stdin (Ctrl-D, a closed pipe) at a yes/no prompt no longer defaults to "yes" — affected `Start Ollama now?`, `Restart MODEL?`, and three skill prompts
- Bug fix: a failed memory-embed no longer leaves an orphaned `.fountain` file; the error now says to run `ollama pull MODEL`
- Bug fix: `/model use NAME` now matches any registered model by exact name or unique prefix, not just the picker's list
- Bug fix: the "1-byte" line reader actually buffered 16 bytes (`bufio`'s minimum) and could swallow piped multi-line input after a prompt
- `knowledge` bumped to the released v0.0.14; `termlib` bumped to the released v0.0.10 (a display-corrupting bug fix for prompts wider than the terminal or containing an embedded newline)


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

