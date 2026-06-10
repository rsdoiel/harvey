# harvey

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is a terminal-based coding agent written in Go. It connects to an Ollama server (or other supported providers) to provide an interactive REPL for working with language models, with all operations sandboxed to a workspace directory. The name comes from Mary Chase's play about a Púca — Harvey is a small, local agent for small computers. See [Vision](vision.md) for the philosophy behind the project.

## Security Note

Harvey is **experimental** — a **working proof of concept**, not production-ready software. Letting a probabilistic model direct command execution is an inherently risky attack surface; Harvey mitigates this with safe mode, workspace sandboxing, permission checks, audit logging, and security reviews with each release, but the risk is never zero. **Don't use Harvey where the risks might endanger your data, people, or planet.** See [SECURITY.md](SECURITY.md) for details.

## Features

### Core Capabilities
- Interactive terminal REPL for language models, sandboxed to a workspace directory
- Auto-execute: tagged code blocks in model replies are written to files automatically
- `HARVEY.md` provides a customizable system prompt per workspace

### Knowledge, Memory & Sessions
- Three-silo memory: RAG vector stores, session-experience memory with rolling summaries, and a SQLite knowledge base
- Sessions recorded as human-readable Fountain screenplay (`.spmd`) files — replay, continue, or mine them for memories
- Pinned context and conversation summarization to manage the context window

### Multi-Model & Routing
- Ollama integration (primary), with cloud routes for Anthropic, DeepSeek, Gemini, Mistral, and OpenAI
- Multi-model routing via `@mention` dispatch and model aliasing

### Security
- Safe mode with a command allowlist and fine-grained workspace permissions
- API key filtering from child processes and an in-memory audit log

### File & Code Support
- Code-aware RAG chunking, doc extraction, and ANSI syntax highlighting (13 languages)
- Automatic code formatting on `write_file` (gofmt, clang-format, black, rustfmt, prettier, and built-in Pascal/Oberon/Basic formatters)
- Git integration and PDF text extraction

### Extensibility
- SKILL.md skills, bundled skill sets, and multi-step prompt pipelines

## Quick Start

1. **Install**: Run the installer for your platform
   - Linux/macOS: `./installer.sh`
   - Windows: Run `installer.ps1` in PowerShell

2. **Run**: `harvey`

3. **Try it**:
   ```
harvey > /read LICENSE
harvey > /help
   ```

See [Installation](INSTALL.md) for detailed instructions.

## Platform Support

Harvey runs on:
- Linux: x86_64, aarch64, armv7l
- macOS: Intel and Apple Silicon (M1 and above)
- Windows: x86_64
- Raspberry Pi OS

## Software Requirements

- Go >= 1.26.3
- Ollama (recommended)

### Software Suggestions

For building Harvey and documentation from source:
- CMTools >= 0.0.45b
- Pandoc >= 3.9
- GNU Make >= 3.8

## Documentation

- [GitHub Repository](https://github.com/rsdoiel/harvey) — Source code
- [User Manual](user_manual.md) — Main documentation index
- [Overview](overview.md) — What Harvey is, why you might use it, and how to get started
- [Developer Guide](developer_guide.md) — Architecture, conventions, and contributing
- [Reference Manual](reference.md) — Command and configuration reference
- [Installation](INSTALL.md) — Get Harvey installed on your system
- [Getting Started](getting_started.md) — First session, keyboard shortcuts, slash commands
- [About](about.md) — Project metadata and version information
- [Vision](vision.md) — Philosophy, motivation, and future direction

## Release Notes

- version: 0.0.10
- status: active
- released: 2026-06-09

- Added multi-language code-aware chunking for RAG ingestion (C, C++, Pascal, Oberon, Lisp, Basic)
- Added documentation extraction: comment and docstring association with symbols for C, C++, Pascal, Oberon, Lisp, Basic
- Added ANSI syntax highlighting of code blocks in LLM responses (13 languages: C, C++, Pascal, Oberon, Lisp, Basic, Go, Python, JavaScript, TypeScript, Rust, Shell, SQL); configurable via `syntax_highlight` in harvey.yaml
- Added automatic code formatting on `write_file`: built-in formatters for Pascal, Oberon, Basic; external pipe-mode formatters for Go (gofmt), C/C++ (clang-format), Python (black), Rust (rustfmt), JavaScript/TypeScript (prettier); configurable via `auto_format` in harvey.yaml
- Added `/format FILE [FILE...]` command to manually format workspace source files in-place

See [CHANGES.md](CHANGES.md) for detailed release history.

### Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)

## License

[AGPL-3.0](https://www.gnu.org/licenses/agpl-3.0.txt)

## Getting Help

- [GitHub Issues](https://github.com/rsdoiel/harvey/issues) — Bug reports and feature requests
