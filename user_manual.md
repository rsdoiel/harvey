# Harvey User Manual

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is a tool for scholarly work via natural language programming. It provides an interactive REPL backed by a local language model system — running via [llamafile](https://github.com/mozilla-ai/llamafile) or [Ollama](https://ollama.com) — where you direct the model to read files, run commands, search code, and apply changes inside a sandboxed workspace. Individual prompts can be routed to cloud providers via named routes. Language model systems are commonly called "AI models" or "AI"; Harvey treats them as a programmable interface for deliberate, documented work rather than a chat assistant.

---

## 🚀 Quick Start Guide
*For users who want to get Harvey running immediately*

### 1. Installation
[INSTALL.md](INSTALL.md) — Step-by-step instructions to install Harvey on Linux, macOS, or Windows

### 2. First Session
[getting_started.md](getting_started.md) — Launch Harvey, connect to a local model, and run your first commands

### 3. System Prompt Customization
[HARVEY.md](HARVEY.md) — The default system prompt and how to customize it for your project

---

## 📚 Core User Guide
*For daily Harvey usage*

### Working with Harvey

- **[Overview](overview.md)** — What Harvey is, its philosophy, and core features
- **[Configuration](CONFIGURATION.md)** — Workspace configuration in `agents/harvey.yaml`, including permissions, timeouts, and memory settings

### Commands and Workflows

- **[Command Reference](harvey.1.md)** — Primary man page with all available commands and flags
- **[Getting Started](getting_started.md)** — Detailed session walkthrough, keyboard shortcuts, and slash commands

#### Command vocabulary

Every Harvey resource command responds to the same eight verbs:

| Verb | Meaning |
|---|---|
| `list` | Enumerate all registered items |
| `add` | Register an existing external resource (file, URL) |
| `new` | Create a fresh internal item (database, skill, plan) |
| `use [NAME]` | Activate an item; picker when NAME is omitted |
| `show [NAME]` | Display item content or details |
| `edit [NAME]` | Open in `$EDITOR` |
| `remove [NAME]` | Delete or unregister; picker when NAME is omitted |
| `rename OLD NEW` | Rename |

Backend service commands additionally support `start`, `stop`, and `status`
(connection health — distinct from `show` which displays content).

The `add` vs `new` distinction: `add` registers something you already have;
`new` creates something Harvey manages from scratch.

Learning this vocabulary for one command teaches you all the others:
`/llamafile`, `/rag`, `/route`, `/session`, `/skill`, `/skill-set`, and
`/memory profile` all follow the same pattern.

---

## 🎯 Advanced Features

### Retrieval-Augmented Generation

- **[RAG Setup & Usage](Using_RAGs_with_Harvey.md)** — Complete guide to retrieval-augmented generation, including named stores, ingest commands, and configuration
- **[RAG Commands](harvey-rag.7.md)** — Detailed reference for all RAG-related slash commands

### Knowledge Management

- **[Knowledge Base](KNOWLEDGE_BASE.md)** — SQLite-backed knowledge base schema, FTS5 search, CLI commands, and Go API
- **[KB Commands](harvey-kb.7.md)** — `/kb` slash command reference: projects, observations, concepts
- **[Memory System](memory-unified-design.md)** — Unified memory architecture with rolling summaries and token budget tracking
- **[Memory Commands](harvey-memory.7.md)** — `/memory` slash command reference: mine, list, show, flag, forget, profile
- **[Learn — Memory Overview](harvey-learn.7.md)** — When to use RAG vs memory vs KB; unified recall

### Model Management

- **[Llamafile Commands](harvey-llamafile.7.md)** — Primary local backend: add, use, show, list, start, download, remove
- **[Unified /model Command](harvey-model.7.md)** — `/model list|use|show|status` works across llamafile and Ollama
- **[Model & Alias Commands](harvey-model-alias.7.md)** — `@mention` inline model switching and `/model alias` short names
- **[Ollama Commands](harvey-ollama.7.md)** — Alternative local backend: service control and model management
- **[Routing](ROUTING.md)** — Connect to remote endpoints (Anthropic, DeepSeek, Gemini, Mistral, OpenAI, remote Ollama) via @mention syntax
- **[Routing Commands](harvey-routing.7.md)** — `/route` slash command reference
- **[Model Guide](model_guide.md)** — Model selection guide based on capability probing results
- **[Model Cache](MODEL_CACHE.md)** — Model capability caching architecture, database schema, and probing mechanisms

### Sessions & Recording

- **[Sessions](SESSIONS.md)** — Session recording, file structure, replay functionality, and programmatic access
- **[Session Commands](harvey-session.7.md)** — `/session list|show|use|continue|replay` reference
- **[Record Commands](harvey-record.7.md)** — `/record start|stop|status` reference
- **[Fountain Format](FOUNTAIN_FORMAT.md)** — Fountain screenplay format specification for conversation recordings

### File & Workspace Operations

- **[Read](harvey-read.7.md)** — Inject workspace files into conversation context
- **[Read Directory](harvey-read-dir.7.md)** — Read all files in a directory tree into context
- **[Read PDF](harvey-read-pdf.7.md)** — Extract and inject PDF text (requires poppler)
- **[Attach](harvey-attach.7.md)** — Attach images, PDFs, or text; auto-selects best representation
- **[Write](harvey-write.7.md)** — Save the last reply or first code block to a file
- **[Format](harvey-format.7.md)** — Auto-format source files using language-appropriate tools
- **[Search](harvey-search.7.md)** — Regex search across workspace files
- **[Files](harvey-files.7.md)** — List workspace directory contents
- **[File Tree](harvey-file-tree.7.md)** — Display a recursive directory tree

### Automation & Pipelines

- **[Pipeline](harvey-pipeline.7.md)** — Chain Markdown prompt files with confidence gating
- **[Plan](harvey-plan.7.md)** — Generate and execute step-by-step task plans with bounded context
- **[Loop](harvey-loop.7.md)** — Repeat a prompt or command on a fixed interval

### Status & Diagnostics

- **[Status](harvey-status.7.md)** — Show backend, token usage, routing, recording, and debug state
- **[Hint](harvey-hint.7.md)** — Actionable suggestions for improving results (RAG, memory, KB)
- **[Inspect](harvey-inspect.7.md)** — Detailed model information (Ollama)
- **[Summarize](harvey-summarize.7.md)** — Condense history to free context window space (`/compact` alias)

---

## 🔧 Extending Harvey

### Skills System

- **[Skills Overview](SKILLS.md)** — Extend Harvey with custom skills, including SKILL.md format, discovery paths, compiled skills, triggers, and the skill wizard
- **[Skills Commands](harvey-skills.7.md)** — `/skill` slash command reference
- **[Skill Sets](harvey-skill-set.7.md)** — Manage named bundles of skills for specific workflows

### Security

- **[Security Guide](SECURITY.md)** — Safe mode, command allowlists, workspace permissions, audit logging, and threat model
- **[Security Commands](harvey-security.7.md)** — `/safemode`, `/permissions`, `/audit`, `/security` reference
- **[Security Review](SECURITY_REVIEW.md)** — Pre-release security assessment and known limitations

---

## 📖 What's New

- See **[CHANGES.md](CHANGES.md)** for version history and new features in each release

---

## 🔍 Can't Find What You Need?

- Use `/help` inside Harvey for a live command list
- Use `/help <command>` for specific command help
- See **[Developer Guide](developer_guide.md)** for development and contribution information
- See **[Reference](reference.md)** for complete command and configuration reference
