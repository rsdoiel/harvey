# Harvey User Manual

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is a terminal-based coding agent backed by a local
[Ollama](https://ollama.com) server (or any provider supported by
`mozilla-ai/any-llm-go`). It provides an interactive REPL where you chat with
a large language model while also reading files, running commands, searching
code, and applying suggested changes, all sandboxed to a single workspace
directory.

---

## Suggested Reading Order

Work through these documents in order to understand Harvey completely.

1. [Overview](README.md) — what Harvey is and the philosophy behind it
2. [Installation](INSTALL.md) — get Harvey installed on your system
3. [Getting Started](getting_started.md) — first session, keyboard shortcuts, slash commands, and security features
4. [System Prompt](HARVEY.md) — the default system prompt and how to customize it
5. [Configuration](CONFIGURATION.md) — workspace configuration in `agents/harvey.yaml`
6. [Security](SECURITY.md) — safe mode, permissions, audit log, and threat model
7. [Skills](SKILLS.md) — extend Harvey with custom skills
8. [Routing](ROUTING.md) — connect to remote model endpoints via @mention
9. [Retrieval-Augmented Generation](Using_RAGs_with_Harvey.md) — retrieval-augmented generation
10. [Knowledge Base](KNOWLEDGE_BASE.md) — SQLite-backed knowledge base
11. [Sessions](SESSIONS.md) — session recording and the Fountain format
12. [Fountain Format](FOUNTAIN_FORMAT.md) — Fountain screenplay format specification
13. [Model Cache](MODEL_CACHE.md) — model capability caching
14. [Architecture](ARCHITECTURE.md) — component map and internal design
15. [Testing](TESTING.md) — writing and running tests

---

## Reference

### Getting Started

- [README.md](README.md) — project overview, features, and philosophy
- [INSTALL.md](INSTALL.md) — installation for all platforms
- [about.md](about.md) — project metadata and author information
- [getting_started.md](getting_started.md) — session walkthrough, keyboard shortcuts, slash commands, security
- [HARVEY.md](HARVEY.md) — default system prompt

### User Guides

- [Using_RAGs_with_Harvey.md](Using_RAGs_with_Harvey.md) — RAG setup, named stores, commands, and configuration
- [SKILLS.md](SKILLS.md) — SKILL.md format, discovery paths, compiled skills, triggers, and skill wizard
- [ROUTING.md](ROUTING.md) — @mention syntax, endpoint types, and configuration
- [KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md) — schema, FTS5 search, CLI commands, and Go API
- [SESSIONS.md](SESSIONS.md) — session recording, file structure, and programmatic access
- [MODEL_CACHE.md](MODEL_CACHE.md) — architecture, database schema, Go API, and probing mechanisms
- [delegate_to_harvey.md](delegate_to_harvey.md) — delegating tasks to Harvey
- [search.md](search.md) — search functionality
- [further_reading.md](further_reading.md) — additional reading and resources

### Configuration and Architecture

- [CONFIGURATION.md](CONFIGURATION.md) — `agents/harvey.yaml`, `agents/routes.json`, and environment variables
- [ARCHITECTURE.md](ARCHITECTURE.md) — component map, core types, security system, and backend implementations
- [FOUNTAIN_FORMAT.md](FOUNTAIN_FORMAT.md) — Fountain screenplay session format specification

### Security

- [SECURITY.md](SECURITY.md) — security model, protections, and known limitations
- [SECURITY_REVIEW.md](SECURITY_REVIEW.md) — pre-release security assessment

### Models

- [models.md](models.md) — models in use and evaluation status
- [model_guide.md](model_guide.md) — model selection guide from `/ollama probe`
- [Llamafile_notes.md](Llamafile_notes.md) — Mozilla AI's single-file runnable models (not integrated with Harvey)

### Development and Design

- [TESTING.md](TESTING.md) — running tests, test architecture, writing tests, and CI
- [model_testing_plan.md](model_testing_plan.md) — model evaluation methodologies
- [RAG_Support_Design.md](RAG_Support_Design.md) — RAG support design document
- [Harvey_Skill-Set_Design.md](Harvey_Skill-Set_Design.md) — `/skill-set` command design
- [improved_tool_handling_with_schemas.md](improved_tool_handling_with_schemas.md) — tool handling with schemas design
- [TODO.md](TODO.md) — current action items and known bugs

### Installation Notes

- [INSTALL_NOTES_macOS.md](INSTALL_NOTES_macOS.md) — macOS-specific notes
- [INSTALL_NOTES_Windows.md](INSTALL_NOTES_Windows.md) — Windows-specific notes

---

## Man Pages

- [harvey.1.md](harvey.1.md) — primary man page
- [harvey.7.md](harvey.7.md) — general concepts
  - [clear](harvey-clear.7.md)
  - [context](harvey-context.7.md)
  - [editing](harvey-editing.7.md)
  - [file-tree](harvey-file-tree.7.md)
  - [files](harvey-files.7.md)
  - [git](harvey-git.7.md)
  - [inspect](harvey-inspect.7.md)
  - [knowledge base](harvey-kb.7.md)
  - [model](harvey-model.7.md)
    - [model aliases](harvey-model-alias.7.md)
  - [ollama](harvey-ollama.7.md)
  - [RAG](harvey-rag.7.md)
  - [read](harvey-read.7.md)
    - [read directory](harvey-read-dir.7.md)
  - [record](harvey-record.7.md)
  - [rename](harvey-rename.7.md)
  - [routing](harvey-routing.7.md)
  - [run](harvey-run.7.md)
  - [search](harvey-search.7.md)
  - [security](harvey-security.7.md)
  - [session](harvey-session.7.md)
  - [skills](harvey-skills.7.md)
    - [skill sets](harvey-skill-set.7.md)
  - [status](harvey-status.7.md)
  - [summarize](harvey-summarize.7.md)
  - [write](harvey-write.7.md)

---

## External

- [GitHub Issues](https://github.com/rsdoiel/harvey/issues) — bug reports and feature requests
