# Harvey User Manual

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is a terminal-based coding agent backed by a local [Ollama](https://ollama.com) server (or any provider supported by `mozilla-ai/any-llm-go`). It provides an interactive REPL where you chat with a large language model while also reading files, running commands, searching code, and applying suggested changes, all sandboxed to a single workspace directory.

---

## 🚀 Quick Start Guide
*For users who want to get Harvey running immediately*

### 1. Installation
[INSTALL.md](INSTALL.md) — Step-by-step instructions to install Harvey on Linux, macOS, or Windows

### 2. First Session
[getting_started.md](getting_started.md) — Launch Harvey, connect to Ollama, and run your first commands

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

### Security Features

- **[Security Guide](SECURITY.md)** — Safe mode, command allowlists, workspace permissions, audit logging, and threat model
- **[Security Review](SECURITY_REVIEW.md)** — Pre-release security assessment and known limitations

---

## 🎯 Advanced Features

### Retrieval-Augmented Generation

- **[RAG Setup & Usage](Using_RAGs_with_Harvey.md)** — Complete guide to retrieval-augmented generation, including named stores, ingest commands, and configuration
- **[RAG Commands](harvey-rag.7.md)** — Detailed reference for all RAG-related slash commands

### Knowledge Management

- **[Knowledge Base](KNOWLEDGE_BASE.md)** — SQLite-backed knowledge base schema, FTS5 search, CLI commands, and Go API
- **[Memory System](memory-unified-design.md)** — Unified memory architecture with rolling summaries and token budget tracking

### Model Management

- **[Model Guide](model_guide.md)** — Model selection guide based on `/ollama probe` results
- **[Model Cache](MODEL_CACHE.md)** — Model capability caching architecture, database schema, and probing mechanisms
- **[Routing](ROUTING.md)** — Connect to remote model endpoints (Anthropic, DeepSeek, Gemini, Mistral, OpenAI) via @mention syntax
- **[Llamafile Notes](Llamafile_notes.md)** — Mozilla AI's single-file runnable models (note: not currently integrated with Harvey)

### Sessions & Recording

- **[Sessions](SESSIONS.md)** — Session recording, file structure, replay functionality, and programmatic access
- **[Fountain Format](FOUNTAIN_FORMAT.md)** — Fountain screenplay format specification for conversation recordings

---

## 🔧 Extending Harvey

### Skills System

- **[Skills Overview](SKILLS.md)** — Extend Harvey with custom skills, including SKILL.md format, discovery paths, compiled skills, triggers, and the skill wizard
- **[Skill Sets](harvey-skill-set.7.md)** — Manage named bundles of skills for specific workflows

---

## 📖 What's New

- See **[CHANGES.md](CHANGES.md)** for version history and new features in each release

---

## 🔍 Can't Find What You Need?

- Use `/help` inside Harvey for a live command list
- Use `/help <command>` for specific command help
- See **[Developer Guide](developer_guide.md)** for development and contribution information
- See **[Reference](reference.md)** for complete command and configuration reference
