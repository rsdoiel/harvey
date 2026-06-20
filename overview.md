
# Harvey – Natural Language Programming for Scholarly Work

## What Harvey Is

Harvey is an open-source, cross-platform terminal tool for scholarly work using
natural language programming. It connects a local language model system —
running via [llamafile](https://github.com/mozilla-ai/llamafile) or
[Ollama](https://ollama.com) — to your workspace, giving you a programmable
interface for reading files, running commands, searching code, managing
knowledge, and recording your work. Language model systems are commonly called
"AI models" or "AI"; Harvey treats them as a programmable substrate for
deliberate, documented work rather than a chat assistant.

Harvey runs on any platform supported by Go and is designed to work well on
resource-constrained hardware such as a Raspberry Pi, as well as on more
capable desktop and server machines.

## Why You Might Want to Use It

| Feature | Benefit |
|---|---|
| **Llamafile & Ollama backends** | Run language model systems locally with no cloud dependency. Llamafile bundles a model and server into a single executable; Ollama manages a model registry. Switch between them with `/model use`. |
| **Unified command set** (`/read`, `/write`, `/run`, `/attach`, `/search`, `/git`, `/rag`, `/memory`, `/kb`, etc.) | Direct the language model and manage your workspace without leaving the REPL. |
| **RAG integration** | Inject context from local files, PDFs, S3 objects, SFTP/SCP servers, or HTTP/HTTPS URLs into your prompts. Create and query SQLite-backed vector stores with `/rag` commands. |
| **Tool calling** | Built-in tools (`read_file`, `write_file`, `run_command`, etc.) with validated schemas let capable language models act directly on your workspace. |
| **Multi-model routing** | Dispatch prompts to specific models (`@model_name`) or route to remote endpoints (Ollama, Anthropic, DeepSeek, Gemini, Mistral, OpenAI) via `/route add`. |
| **Session recording** | Every session is recorded as a Fountain `.spmd` file — a structured, human-readable transcript you can review, replay against a different model, or mine for reusable memories. |
| **Knowledge base & memory** | Persistent SQLite knowledge base (`agents/knowledge.db`) plus a unified memory system with rolling summaries, typed experience records, and token-budget tracking. |
| **Secure safe mode** | Execution is gated by a command allowlist; workspace permissions give fine-grained read/write/exec/delete control per path prefix; API keys are stripped from every child process. |
| **Extensible skill system** | Load specialised skills via `/skill load <name>` to inject domain-specific instructions and compiled scripts. |
| **Installer scripts** | Pre-built installers for Linux (x86_64/aarch64/armv7l), macOS, and Windows. |

## Getting Started

1. **Install Harvey** — Run the installer script for your OS (`installer.sh` for Linux/macOS, `installer.ps1` for Windows).

2. **Get a model** — Download a llamafile from the [Mozilla AI pre-built llamafiles page](https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles) and place it in `~/Models/`. Harvey finds and connects to it automatically at startup.

3. **Launch Harvey**

   ```bash
   cd $HOME/myproject
   harvey
   ```

4. **Basic commands**

   ```
   harvey > /read src/main.go
   harvey > /run go test ./...
   harvey > /kb observe finding context window at 8 192 tokens is tight for large files
   harvey > /bye
   ```

See [getting_started.md](getting_started.md) for a full session walkthrough.

## Design Principles

**Natural language programming interface** — The REPL is not a chatbot. Every
exchange directs the language model to act on your workspace: read files, run
commands, write output, search code, update knowledge. The goal is a
reproducible, auditable workflow expressed in natural language.

**Scholarly apparatus built in** — The workspace, knowledge base, RAG store,
memory system, and session recording together form a lab notebook: a place to
accumulate findings, record decisions, and retrieve prior context. Harvey is
designed for work that requires documentation and continuity across sessions.

**Tool-first, local-first architecture** — All user actions are exposed as
first-class tools (`read_file`, `write_file`, `run_command`, etc.) with
validated schemas. Language model systems run locally by default; cloud
endpoints are opt-in named routes, never the primary path.

**Layered security** — Safe Mode gates execution to a command allowlist;
workspace permissions give fine-grained read/write/exec/delete control per
path prefix; an audit log records every command and file access; API keys are
stripped from every child process environment.
