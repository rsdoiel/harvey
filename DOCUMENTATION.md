# Harvey Documentation Index

## Overview

This document serves as the **central hub** for all Harvey documentation.
It organizes all available guides, references, and specifications by category
to help you find the information you need quickly.

Harvey's documentation covers five main areas:

- **Getting Started** — Installation, setup, and basic usage
- **User Guides** — Practical instructions for using Harvey features
- **Configuration & Architecture** — Technical details and customization
- **Development** — Extending Harvey, testing, and contributing
- **Reference** — Technical specifications and API documentation

## Quick Navigation

| What you need | Start here |
|---------------|------------|
| First time with Harvey | [Getting Started](getting_started.md) |
| Install Harvey | [Installation](INSTALL.md) |
| Use slash commands | [User Manual](user_manual.md) or [Getting Started](getting_started.md#slash-commands) |
| Set up RAG | [Using RAGs with Harvey](Using_RAGs_with_Harvey.md) |
| Configure Harvey | [Configuration Reference](CONFIGURATION.md) |
| Understand session format | [Fountain Format](FOUNTAIN_FORMAT.md) |
| Learn about skills | [Skills System](SKILLS.md) |
| Set up routing | [Routing](ROUTING.md) |
| Use knowledge base | [Knowledge Base](KNOWLEDGE_BASE.md) |
| Understand architecture | [Architecture](ARCHITECTURE.md) |
| Write tests | [Testing](TESTING.md) |
| Model cache details | [Model Cache](MODEL_CACHE.md) |


## Documentation by Category

### 📚 Getting Started

These documents help you install and begin using Harvey.

| Document | Description | Audience |
|----------|-------------|----------|
| [README.md](README.md) | Project overview, features, motivation, and philosophy | New users |
| [INSTALL.md](INSTALL.md) | Installation instructions for all platforms | New users |
| [about.md](about.md) | Project metadata and author information | All |
| [getting_started.md](getting_started.md) | Comprehensive introduction with session walkthrough, keyboard shortcuts, slash commands, and security features (safe mode, permissions, audit log) | New users |
| [user_manual.md](user_manual.md) | Concise user manual with command reference | All users |
| [HARVEY.md](HARVEY.md) | Default system prompt for Harvey | Reference |

### 🎯 User Guides

Practical guides for using Harvey's features effectively.

| Document | Description | Audience |
|----------|-------------|----------|
| [Using_RAGs_with_Harvey.md](Using_RAGs_with_Harvey.md) | Complete guide to Retrieval-Augmented Generation with Harvey. Covers setup, named stores, commands, configuration, usage patterns, and advanced topics. | Users, Developers |
| [SKILLS.md](SKILLS.md) | Deep dive into Harvey's skills system. Includes SKILL.md format, discovery paths, compiled skills, triggers, and the skill wizard. | Users, Developers |
| [ROUTING.md](ROUTING.md) | Guide to remote endpoint routing. Covers @mention syntax, endpoint types (Ollama, cloud APIs), configuration, and usage. | Users, Developers |
| [KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md) | Complete reference for the SQLite-backed knowledge base. Includes schema, FTS5 search, projects/observations/concepts model, CLI commands, Go API, and migration guide. | Users, Developers |
| [SESSIONS.md](SESSIONS.md) | Guide to session recording and the Fountain format specification. Covers file structure, character model, scene types, @mention routing, commands, and programmatic access. | Users, Developers |
| [MODEL_CACHE.md](MODEL_CACHE.md) | Comprehensive guide to model capability caching. Covers architecture, database schema, Go API, probing mechanisms, usage patterns, configuration, best practices, and troubleshooting. | Developers |
| [TESTING.md](TESTING.md) | Complete testing guide. Covers running tests, test architecture, testing strategies, writing tests, mocking, tiered testing, debugging, CI, and contributing. | Developers |

### ⚙️ Configuration & Architecture

Technical details about Harvey's internals and customization options.

| Document | Description | Audience |
|----------|-------------|----------|
| [CONFIGURATION.md](CONFIGURATION.md) | Complete guide to Harvey's workspace-local configuration. Covers agents/harvey.yaml (including security settings: safe mode, permissions, timeouts), agents/routes.json, HARVEY.md system prompt, and environment variables. | Users, Developers |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Detailed technical documentation. Includes component map, core types (Agent, LLMClient, Message, Workspace, KnowledgeBase, Recorder), security system (audit log, permissions, safe mode, env filtering), backend implementations, test coverage, and feature roadmap. | Developers |
| [models.md](models.md) | Information about models being used and evaluated | Reference |
| [model_testing_plan.md](model_testing_plan.md) | Comprehensive plan for testing and evaluating models | Developers |

### 🛠️ Development

Documents for developers extending, testing, or contributing to Harvey.

| Document | Description | Audience |
|----------|-------------|----------|
| [TESTING.md](TESTING.md) | Complete testing guide. See User Guides section above. | Developers |
| [MODEL_CACHE.md](MODEL_CACHE.md) | Model capability caching. See User Guides section above. | Developers |
| [harvey.1.md](harvey.1.md) | Man page source (generated from `harvey -help`) | Reference |
| [harvey.7.md](harvey.7.md) | Additional man page documentation | Reference |

### 📋 Reference & Specifications

Technical specifications and reference materials.

| Document | Description | Audience |
|----------|-------------|----------|
| [FOUNTAIN_FORMAT.md](FOUNTAIN_FORMAT.md) | Harvey's Fountain screenplay format specification. Covers multi-model character attribution, file extensions (.spmd, .fountain), character model (Human, HARVEY, ROUTE_NAME, MODEL_NAME), character identity rules, scene types (INT., EXT.), @mention syntax for routing, and forwarding patterns. | Developers, Advanced Users |
| [RAG_Support_Design.md](RAG_Support_Design.md) | Design document for RAG support in Harvey | Developers |
| [delegate_to_harvey.md](delegate_to_harvey.md) | Guide for delegating tasks to Harvey | Users |
| [further_reading.md](further_reading.md) | Additional reading materials and resources | All |
| [search.md](search.md) | Search functionality documentation | Users |

### 📋 Installation Notes

Platform-specific installation instructions.

| Document | Description | Audience |
|----------|-------------|----------|
| [INSTALL_NOTES_macOS.md](INSTALL_NOTES_macOS.md) | macOS-specific installation notes | macOS Users |
| [INSTALL_NOTES_Windows.md](INSTALL_NOTES_Windows.md) | Windows-specific installation notes | Windows Users |


## Documentation Map

```
harvey/
├── README.md                    # Project overview
├── INSTALL.md                   # Installation guide
├── about.md                     # Project metadata
├── user_manual.md               # User manual
├── getting_started.md           # Getting started guide
├── HARVEY.md                    # System prompt
│
├── CONFIGURATION.md             # Configuration reference
├── ARCHITECTURE.md              # Technical architecture
├── FOUNTAIN_FORMAT.md           # Fountain format spec
├── SKILLS.md                    # Skills system
├── ROUTING.md                   # Routing guide
├── KNOWLEDGE_BASE.md            # Knowledge base reference
├── SESSIONS.md                  # Session recording & Fountain
├── MODEL_CACHE.md               # Model capability caching
├── Using_RAGs_with_Harvey.md    # RAG comprehensive guide
├── TESTING.md                   # Testing guide
│
├── RAG_Support_Design.md         # RAG design document
├── delegate_to_harvey.md        # Delegation guide
├── model_testing_plan.md         # Model testing plan
├── models.md                    # Models reference
├── further_reading.md           # Additional resources
├── search.md                    # Search documentation
│
├── harvey.1.md                  # Man page
├── harvey.7.md                  # Additional man page
├── INSTALL_NOTES_macOS.md        # macOS install notes
└── INSTALL_NOTES_Windows.md      # Windows install notes
```


## Recommended Reading Paths

### For New Users

1. **[README.md](README.md)** — Understand what Harvey is and its philosophy
2. **[INSTALL.md](INSTALL.md)** — Get Harvey installed on your system
3. **[getting_started.md](getting_started.md)** — Learn the basics through a walkthrough
4. **[user_manual.md](user_manual.md)** — Reference for all slash commands

### For Power Users

1. **[CONFIGURATION.md](CONFIGURATION.md)** — Customize Harvey to your needs
2. **[SKILLS.md](SKILLS.md)** — Extend Harvey with custom skills
3. **[ROUTING.md](ROUTING.md)** — Set up remote endpoints
4. **[Using_RAGs_with_Harvey.md](Using_RAGs_with_Harvey.md)** — Use RAG for enhanced responses
5. **[KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md)** — Build and query your knowledge base

### For Developers

1. **[ARCHITECTURE.md](ARCHITECTURE.md)** — Understand Harvey's internal structure
2. **[FOUNTAIN_FORMAT.md](FOUNTAIN_FORMAT.md)** — Learn the session format
3. **[TESTING.md](TESTING.md)** — Write and run tests
4. **[MODEL_CACHE.md](MODEL_CACHE.md)** — Understand model capability caching
5. **[RAG_Support_Design.md](RAG_Support_Design.md)** — RAG implementation details

### For Advanced Use Cases

1. **[SESSIONS.md](SESSIONS.md)** — Advanced session recording and replay
2. **[MODEL_CACHE.md](MODEL_CACHE.md)** — Model metadata and capability probing
3. **[model_testing_plan.md](model_testing_plan.md)** — Model evaluation methodologies


## Document Statistics

| Category | Count | Total Size |
|----------|-------|------------|
| Getting Started | 6 | ~45 KB |
| User Guides | 7 | ~170 KB |
| Configuration & Architecture | 3 | ~45 KB |
| Development | 4 | ~65 KB |
| Reference & Specifications | 6 | ~70 KB |
| Installation Notes | 2 | ~5 KB |
| **Total** | **28** | **~400 KB** |


## Maintenance Notes

### Documentation Standards

All Harvey documentation follows these conventions:

- **Version tag** in header: `*Version X.Y — description*`
- **Overview section** with bullet points for quick scanning
- **Detailed sections** with tables for complex information
- **Examples section** with code snippets where applicable
- **Best Practices** section for practical advice
- **Troubleshooting** section with common issues and solutions
- **See Also** section with links to related documents
- **Generation note** at bottom where applicable

### Document Naming Convention

- Lowercase with hyphens only (e.g., `Using_RAGs_with_Harvey.md`)
- `.md` extension for all Markdown files
- `.spmd` extension for Harvey session recordings (Fountain format)

### Auto-generated Files

The following files are auto-generated and should not be manually edited:

- `INSTALL.md` — Generated from codemeta.json using cmt tool
- `about.md` — Generated from codemeta.json using cmt tool
- `harvey.1.md` — Generated from `harvey -help` output
- Any `*.html` files — Generated from Markdown sources

### Directory Structure

Currently all documentation files are located in the `harvey/` directory root.
For future organization, consider creating a `harvey/docs/` subdirectory with
symlinks or a structured hierarchy.


## See Also

- [README.md](README.md) — Project overview
- [ARCHITECTURE.md](ARCHITECTURE.md) — Technical architecture
- [CONFIGURATION.md](CONFIGURATION.md) — Configuration reference
- [GitHub Repository](https://github.com/rsdoiel/harvey) — Source code and issues


*Generated from Harvey source code and documentation analysis*
