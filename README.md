
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

- version: 0.0.19
- status: active
- released: 2026-10-10

- Behaviour changes, see CHANGES.md upgrade notes: llama.cpp models are looked for in llama.cpp's cache (`$LLAMA_CACHE`, else `~/.cache/llama.cpp`) and `~/Models` is for llamafiles only, so set `llama_cpp.models_dir` to keep the old directory (harvey DR-0031, proposed); `skills.catalog` is off by default (set `skills: catalog: full` to restore it); the command line now beats `harvey.yaml` for `--llamafile-url`, `--llamafile-dir`, `-r`, `--record-file`, `--resume`, `--continue`, `--replay`, `--replay-output` and `--replay-continue` as well as `--ollama` and `-m`, and `HARVEY_LLAMAFILE_DIR` beats `llamafile.models_dir`
- A chat stream that ends before its final chunk is an error (`ErrStreamTruncated`, exit 74) instead of an empty reply recorded as a success
- Hailo (AI HAT+ 2) is an engine beside Ollama (harvey DR-0030): a `hailo:` section in `harvey.yaml`, card and server detected apart, `engine/model` names (a bare name on both engines asks, or is exit 2 unattended), the catalog shown as `not pulled` with `/model pull [--yes] NAME`, and a capability table so harvey stops sending `tools` or asking for embeddings where Hailo has none
- Per-model prompt limits: `/model limit`, the system prompt and history fitted to the limit, a turn over it refused before sending, and a failed turn retried on smaller prompts that teaches the limit; the model cache is keyed by engine and model
- `/memory mine` reads a session too large for the model in parts, records an empty session as `empty`, and remembers a model that failed on a session so auto-mine does not retry it; a RAG retrieval failure is reported once per session instead of silently
- `harvey completion bash|powershell [-install]` for shell completion of verbs, options and help topics
- A write offer no longer treats a command, end of input or arbitrary text as an answer; piping `/exit` used to create a file named `exit`
- Switching to a llama.cpp model mid-session keeps the `--debug` log and records the switch, as the Ollama paths already did
- `make test-models` runs live model tests and skips a missing model system with the reason; `make build` no longer runs `cmt`, `make generate` does
- `knowledge` bumped to the released v0.0.20


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

