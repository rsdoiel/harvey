# Harvey Reference Manual

This document provides a comprehensive reference for all Harvey commands, configuration options, and platform-specific information.

---

## 📖 Command Reference

All Harvey commands are organized into logical categories. Each command has its own man page with detailed usage information.

### Core Commands

- **[harvey.1.md](harvey.1.md)** — Primary man page: name, synopsis, description, options, environment variables, and all available slash commands
- **[overview.md](overview.md)** — General concepts and overview of Harvey's design philosophy

### File Operations

- **[harvey-read.7.md](harvey-read.7.md)** — Read file contents into the conversation context
- **[harvey-write.7.md](harvey-write.7.md)** — Write assistant output to a file
- **[harvey-attach.7.md](harvey-attach.7.md)** — Attach a file (image, PDF, or text) to the next turn
- **[harvey-files.7.md](harvey-files.7.md)** — List directory contents in the workspace
- **[harvey-file-tree.7.md](harvey-file-tree.7.md)** — Display a recursive directory tree
- **[harvey-read-dir.7.md](harvey-read-dir.7.md)** — Read all eligible files in a directory tree into context
- **[harvey-read-pdf.7.md](harvey-read-pdf.7.md)** — Extract text from a PDF using poppler utilities
- **[harvey-editing.7.md](harvey-editing.7.md)** — Edit files using the configured editor
- **[harvey-format.7.md](harvey-format.7.md)** — Format workspace source files in-place

### Context and Search

- **[harvey-search.7.md](harvey-search.7.md)** — Regex search across workspace files
- **[harvey-context.7.md](harvey-context.7.md)** — Manage pinned context that survives /clear
- **[harvey-git.7.md](harvey-git.7.md)** — Read-only git commands (status, diff, log, show, blame)

### RAG (Retrieval-Augmented Generation)

- **[harvey-rag.7.md](harvey-rag.7.md)** — Manage retrieval-augmented generation stores
- **[harvey-learn.7.md](harvey-learn.7.md)** — Learn from context and build knowledge
- **[harvey-inspect.7.md](harvey-inspect.7.md)** — Inspect model capabilities and metadata

### Session Management

- **[harvey-session.7.md](harvey-session.7.md)** — Session management commands
- **[harvey-record.7.md](harvey-record.7.md)** — Start, stop, and manage session recordings
- **[harvey-rename.7.md](harvey-rename.7.md)** — Rename the active session file
- **[harvey-summarize.7.md](harvey-summarize.7.md)** — Condense conversation history to free context window space
- **[harvey-clear.7.md](harvey-clear.7.md)** — Reset conversation history
- **[harvey-loop.7.md](harvey-loop.7.md)** — Manage the REPL loop

### Memory System

- **[harvey-memory.7.md](harvey-memory.7.md)** — Manage the session-experience memory store

### Knowledge Base

- **[harvey-kb.7.md](harvey-kb.7.md)** — Query and update the SQLite knowledge base

### Llamafile Integration

- **[harvey-llamafile.7.md](harvey-llamafile.7.md)** — Manage llamafile backends: `/llamafile add|use|list|start|status|remove|download`

### Ollama Integration

- **[harvey-ollama.7.md](harvey-ollama.7.md)** — Manage the local Ollama server and installed models

### Unified Model Management

- **[harvey-model-alias.7.md](harvey-model-alias.7.md)** — `@NAME` inline model switching and `/model alias` short-name definitions
- `/model list|use NAME|show|status` — backend-agnostic model commands (see harvey-model-alias.7.md)

### Routing

- **[harvey-routing.7.md](harvey-routing.7.md)** — Manage named remote LLM endpoints for @mention routing

### Skills

- **[harvey-skills.7.md](harvey-skills.7.md)** — Discover and load SKILL.md-format skills into context
- **[harvey-skill-set.7.md](harvey-skill-set.7.md)** — Load and manage named bundles of skills

### Pipelines

- **[harvey-pipeline.7.md](harvey-pipeline.7.md)** — Chain Markdown prompt files as discrete steps

### Security

- **[harvey-security.7.md](harvey-security.7.md)** — Unified security posture overview

### Execution

- **[harvey-run.7.md](harvey-run.7.md)** — Run shell commands safely within the workspace

### System Status

- **[harvey-status.7.md](harvey-status.7.md)** — Show active backend, token usage, routing, and debug state
- **[harvey-hint.7.md](harvey-hint.7.md)** — Show actionable suggestions for improving results (RAG, memory, KB)

---

## 🔧 Configuration Reference

### Main Configuration
- **[CONFIGURATION.md](CONFIGURATION.md)** — Complete workspace configuration reference for `agents/harvey.yaml`, `agents/routes.json`, and environment variables

### Workspace Configuration Files
- `agents/harvey.yaml` — Main configuration file
- `agents/routes.json` — Named route definitions for multi-backend support
- `agents/knowledge.db` — SQLite knowledge base
- `HARVEY.md` — System prompt for the workspace

### Environment Variables
See **[harvey.1.md](harvey.1.md#environment)** for complete list:
- `ANTHROPIC_API_KEY` — API key for Anthropic Claude
- `DEEPSEEK_API_KEY` — API key for DeepSeek
- `GEMINI_API_KEY` — API key for Google Gemini (also accepts `GOOGLE_API_KEY`)
- `MISTRAL_API_KEY` — API key for Mistral
- `OPENAI_API_KEY` — API key for OpenAI

*Note: All API key variables are filtered from child process environments*

---

## 🌍 Platform-Specific Information

### Installation Notes

- **[INSTALL.md](INSTALL.md)** — Main installation guide for all platforms
- **[INSTALL_NOTES_macOS.md](INSTALL_NOTES_macOS.md)** — macOS-specific installation notes and requirements
- **[INSTALL_NOTES_Windows.md](INSTALL_NOTES_Windows.md)** — Windows-specific installation notes and requirements

### Supported Architectures
- Linux: x86_64, aarch64, armv7l
- macOS: x86_64, arm64
- Windows: x86_64

---

## 📚 Additional Reference

### Models

- **[models.md](models.md)** — Models in use and evaluation status
- **[model_guide.md](model_guide.md)** — Model selection guide based on `/ollama probe` results
- **[Llamafile_notes.md](Llamafile_notes.md)** — Mozilla AI's single-file runnable models (note: not currently integrated with Harvey)

### Development

- **[TESTING.md](TESTING.md)** — Running tests, test architecture, and CI/CD setup
- **[CHANGES.md](CHANGES.md)** — Version history and release notes
- **[TODO.md](TODO.md)** — Current action items, known bugs, and planned features
- **[further_reading.md](further_reading.md)** — Additional reading and resources

### Formatting

- **[page.tmpl](page.tmpl)** — Template for HTML page generation

---

## 🎯 Quick Command Lookup

### Most Common Commands

| Task | Command |
|------|---------|
| Start Harvey | `harvey` |
| Start with specific model | `harvey -m <model>` |
| Start with recording | `harvey --record` |
| Read a file | `/read <path>` |
| Write to file | `/write <path>` |
| Run shell command | `/run <command>` |
| Search files | `/search <pattern>` |
| Git status | `/git status` |
| List files | `/files [path]` |
| Attach file | `/attach <path>` |
| Show help | `/help` |
| Show status | `/status` |
| Clear history | `/clear` |
| Exit | `/exit`, `/quit`, or `/bye` |

### Model Management

| Task | Command |
|------|---------|
| List models | `/ollama list` |
| Pull a model | `/ollama pull <model>` |
| Use a model | `/ollama use <model>` |
| Show model info | `/ollama show <model>` |
| Start Ollama | `/ollama start` |
| Check Ollama status | `/ollama status` |

### RAG Operations

| Task | Command |
|------|---------|
| Create RAG store | `/rag new <name>` |
| List RAG stores | `/rag list` |
| Use RAG store | `/rag use <name>` |
| Ingest files | `/rag ingest <path>` |
| Enable RAG | `/rag on` |
| Disable RAG | `/rag off` |
| Query RAG | `/rag query <text>` |

### Knowledge Base

| Task | Command |
|------|---------|
| Show status | `/kb status` |
| Search | `/kb search <text>` |
| List projects | `/kb project list` |
| Add project | `/kb project add <name> [desc]` |
| Record observation | `/kb observe <kind> <text>` |
| List concepts | `/kb concept list` |
| Add concept | `/kb concept add <name> [desc]` |

### Security

| Task | Command |
|------|---------|
| Toggle safe mode | `/safemode on\|off` |
| Safe mode status | `/safemode status` |
| Allow command | `/safemode allow <cmd>` |
| Set permissions | `/permissions set <path> <perms>` |
| Show permissions | `/permissions list` |
| View audit log | `/audit show [n]` |
| Security overview | `/security status` |
