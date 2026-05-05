
# Harvey Configuration Reference

## Overview

Harvey uses a **workspace-local configuration system**. All configuration
files live inside the workspace — the directory where Harvey is launched.
A HARVEY.md file in the workspace root provides the system prompt, and
environment variables supply cloud provider API keys.

### Configuration Sources

```
┌─────────────────────────────────────────────────────────────┐
│                    CONFIGURATION SOURCES                    │
├─────────────────────────────────────────────────────────────┤
│  1. Workspace Config         <workspace>/agents/harvey.yaml │
│  2. System Prompt            <workspace>/HARVEY.md          │
│  3. Environment Variables    Shell environment              │
└─────────────────────────────────────────────────────────────┘
```

**Precedence:** Workspace config > System prompt > Environment variables

## Configuration Files

### 1. Configuration (`<workspace>/agents/harvey.yaml`)

**Purpose:** Workspace-level configuration for Harvey's subsystems: knowledge
base, sessions, skills, RAG stores, and model cache.

**Locations:** Workspace: `<workspace>/agents/harvey.yaml`

**Format:** YAML

**Example:**
```yaml
# Knowledge base
knowledge_db: "agents/knowledge.db"

# Session recordings
sessions_dir: "agents/sessions"

# Skills/agents directory
agents_dir: "agents"

# Auto-record setting (null = use default)
auto_record: true

# Model capability cache
model_cache_db: "agents/model_cache.db"

# RAG configuration
rag:
  enabled: true
  active: "default"
  stores:
    - name: "default"
      db_path: "agents/rag/default.db"
      embedding_model: "nomic-embed-text"
      model_map:
        "llama3.1:latest": "nomic-embed-text"
        "granite-code:3b": "nomic-embed-text"
        "mistral:latest": "nomic-embed-text"
    - name: "deno_typescript"
      db_path: "agents/rag/deno_typescript.db"
      embedding_model: "nomic-embed-text"
      model_map:
        "llama3.1:latest": "nomic-embed-text"
```

**Top-Level Fields:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `knowledge_db` | string | No | `"agents/knowledge.db"` | Path to knowledge base SQLite file |
| `sessions_dir` | string | No | `"agents/sessions"` | Directory for session recordings |
| `agents_dir` | string | No | `"agents"` | Directory for skills/agents tree |
| `auto_record` | boolean/null | No | `true` | Auto-record sessions (null = keep default) |
| `model_cache_db` | string | No | `"agents/model_cache.db"` | Path to model capability cache |
| `rag` | object | No | See below | RAG configuration |

**RAG Configuration (`rag:`):**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean | No | `false` | Whether RAG is enabled |
| `active` | string | No | `""` | Name of active RAG store |
| `stores` | array | No | `[]` | Array of named RAG stores |

**RAG Store Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique store identifier |
| `db_path` | string | Yes | Path to SQLite database file |
| `embedding_model` | string | Yes | Embedding model name (e.g., `nomic-embed-text`) |
| `model_map` | object | No | Mapping of generation model → embedding model |
| `embedder_kind` | string | No | `"ollama"` | Either `"ollama"` or `"encoderfile"` |
| `embedder_url` | string | No | Base URL for encoderfile embedder |

**Important Notes:**
- Paths are relative to the workspace root
- The `model_map` ensures each generation model uses the correct embedding model
- Embedding model binding is enforced: you cannot mix embeddings from different models in the same store

### 2. Route configuration (`<workspace>/agents/routes.json`)

**Purpose:** Persists routing endpoints and routing state across Harvey
sessions.

**Location:** `<workspace>/agents/routes.json`

**Format:** JSON

**Example:**
```json
{
  "enabled": true,
  "endpoints": [
    {
      "name": "pi2",
      "url": "ollama://192.168.1.12:11434",
      "model": "llama3.1:8b",
      "kind": "ollama"
    },
    {
      "name": "claude",
      "url": "anthropic://",
      "model": "claude-3-haiku",
      "kind": "anthropic"
    },
    {
      "name": "pi3",
      "url": "ollama://192.168.1.13:11434",
      "model": "mistral:latest",
      "kind": "ollama"
    }
  ]
}
```

**Fields:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean | No | `false` | Whether routing is enabled globally |
| `endpoints` | array | No | `[]` | List of registered remote endpoints |

**Endpoint Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for the endpoint |
| `url` | string | Yes | URL including scheme (e.g., `ollama://host:port`) |
| `model` | string | No | Default model to use with this endpoint |
| `kind` | string | No | Inferred from URL scheme | One of: `ollama`, `llamafile`, `llamacpp`, `anthropic`, `deepseek`, `gemini`, `mistral`, `openai` |

**URL Schemes:**

| Scheme | Kind | Backend Type | Notes |
|--------|------|---------------|-------|
| `ollama://host:port` | ollama | Ollama server | Local or remote |
| `http://host:port` | ollama | Ollama server | HTTP (insecure) |
| `https://host:port` | ollama | Ollama server | HTTPS (secure) |
| `llamafile://host:port` | llamafile | Llamafile server | OpenAI-compatible |
| `llamacpp://host:port` | llamacpp | llama.cpp server | OpenAI-compatible |
| `anthropic://` | anthropic | Anthropic Claude | Uses `ANTHROPIC_API_KEY` |
| `deepseek://` | deepseek | DeepSeek API | Uses `DEEPSEEK_API_KEY` |
| `gemini://` | gemini | Google Gemini | Uses `GEMINI_API_KEY` or `GOOGLE_API_KEY` |
| `mistral://` | mistral | Mistral API | Uses `MISTRAL_API_KEY` |
| `openai://` | openai | OpenAI API | Uses `OPENAI_API_KEY` |

**Commands:**
- `/route add NAME URL [MODEL]` — Register a new endpoint
- `/route rm NAME` — Remove an endpoint
- `/route list` — List all endpoints with status
- `/route on` — Enable routing globally
- `/route off` — Disable routing globally
- `/route status` — Show routing state

### 3. System Prompt (`<workspace>/HARVEY.md`)

**Purpose:** Provides the system prompt for Harvey's LLM interactions. This is
the primary way to give the model context about your project, conventions, and
guidelines.

**Location:** `<workspace>/HARVEY.md` (loaded from the workspace root at startup)

**Format:** Markdown with optional dynamic markers

**Example:**
```markdown
You are Harvey, a terminal coding agent. You are working in a Go project
located at /home/user/myproject.

## Project Context

Today: <!-- @date -->

Current git status:
<!-- @git-status -->

Workspace files:
<!-- @files -->

## Conventions

- Use Go 1.26+ features
- Prefer stdlib over third-party packages
- All exported symbols need doc comments
- Use `t.TempDir()` in tests

## Available Tools

You have access to the following slash commands: /read, /write, /run, /search,
/git, /apply, /clear, /summarize, /context, /kb, /rag, /ollama, /route, /skill.

## Rules

1. Tag code blocks with file paths for auto-apply
2. Use /run to suggest shell commands
3. Never narrate fake command output
```

**Dynamic Markers:** Harvey expands these before injecting the system prompt:

| Marker | Replaced With | Example |
|--------|---------------|---------|
| `<!-- @date -->` | Current date (YYYY-MM-DD) | `2026-05-04` |
| `<!-- @files -->` | Workspace file tree (hidden dirs excluded) | Tree listing |
| `<!-- @git-status -->` | Output of `git status --short` | `M README.md` |

**Notes:**
- The prompt is re-injected after `/clear`
- Pinned context (from `/context add`) is injected after the system prompt
- Skills catalog can be injected into the prompt via configuration


## Environment Variables

Harvey reads environment variables for cloud provider API keys. These are
optional and only needed when using the corresponding route types.

| Variable | Provider | Route Scheme | Purpose |
|----------|----------|--------------|---------|
| `ANTHROPIC_API_KEY` | Anthropic | `anthropic://` | Claude API key |
| `DEEPSEEK_API_KEY` | DeepSeek | `deepseek://` | DeepSeek API key |
| `GEMINI_API_KEY` | Google | `gemini://` | Gemini API key (primary) |
| `GOOGLE_API_KEY` | Google | `gemini://` | Gemini API key (fallback) |
| `MISTRAL_API_KEY` | Mistral | `mistral://` | Mistral API key |
| `OPENAI_API_KEY` | OpenAI | `openai://` | OpenAI API key |
| `OLLAMA_HOST` | Ollama | `ollama://` | Ollama server host (default: `localhost`) |
| `OLLAMA_ORIGINS` | Ollama | `ollama://` | CORS origins for Ollama |
| `OLLAMA_KEEP_ALIVE` | Ollama | `ollama://` | Keep-alive interval (default: `24h`) |

**Note:** For local Ollama, Harvey uses the `ollama` command-line tool's
configuration by default. The `OLLAMA_HOST` environment variable can override
the default `http://localhost:11434`.


## Configuration Workflow

### Typical Setup Process

```bash
# 1. Start Harvey for the first time (creates defaults)
harvey

# 2. Register remote endpoints
harvey
> /route add pi2 ollama://192.168.1.12:11434 llama3.1:8b
harvey
> /route on

# 3. Configure RAG (optional)
harvey
> /rag new golang
harvey
> /rag switch golang
harvey
> /rag on

# 4. Create workspace-specific config
mkdir -p myproject/agents
echo 'knowledge_db: myproject/agents/knowledge.db' > myproject/agents/harvey.yaml

# 5. Run Harvey in the project
harvey -w myproject
```

### Configuration File Precedence Example

```
Workspace (myproject/agents/harvey.yaml):
  rag:
    enabled: true
    active: golang
    stores:
      - name: golang
        db_path: myproject/agents/rag/golang.db
        embedding_model: nomic-embed-text

Result: In myproject/, the golang store is active.
```

## Troubleshooting

### Common Issues

**"No routes configured"**
- Run `/route add NAME URL` to register endpoints
- Run `/route on` to enable routing
- Check `<workspace>/agents/routes.json` exists

**"Knowledge base not found"**
- Harvey creates it automatically at first use
- Check `agents/knowledge.db` exists
- Verify path in `harvey.yaml` or create the directory

**"Model cache not found"**
- Harvey creates it automatically
- Check `agents/model_cache.db` exists
- Run `/ollama probe` to populate it

**"RAG store not found"**
- Run `/rag new NAME` to create a store
- Run `/rag switch NAME` to activate it
- Verify the database file exists at the specified path

### Verification Commands

```bash
# Check workspace routes
harvey --help routing  # or: cat agents/routes.json

# Check workspace config
cat agents/harvey.yaml

# Check knowledge base
sqlite3 agents/knowledge.db ".tables"

# Check model cache
sqlite3 agents/model_cache.db "SELECT count(*) FROM models;"

# Check RAG stores
ls -la agents/rag/*.db
```

## Reference: All Configuration Options

### Routes (`<workspace>/agents/routes.json`)

```json
{
  "enabled": true/false,
  "endpoints": [
    {
      "name": "string (required)",
      "url": "string (required)",
      "model": "string (optional)",
      "kind": "string (optional, auto-detected)"
    }
  ]
}
```

### Workspace Agents (`<workspace>/agents/harvey.yaml`)

```yaml
# Paths
knowledge_db: "string (optional, default: agents/knowledge.db)"
sessions_dir: "string (optional, default: agents/sessions)"
agents_dir: "string (optional, default: agents)"
model_cache_db: "string (optional, default: agents/model_cache.db)"

# Behavior
auto_record: true/false/null  # null = use default (true)

# RAG Configuration
rag:
  enabled: true/false
  active: "string (store name)"
  stores:
    - name: "string"
      db_path: "string"
      embedding_model: "string"
      model_map:
        "generation-model": "embedding-model"
      embedder_kind: "ollama"  # or "encoderfile"
      embedder_url: "http://host:port"
```

*For more details on specific subsystems, see:*
- [ROUTING.md](ROUTING.md) — Remote endpoint routing
- [KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md) — Knowledge base schema
- [RAG_Support_Design.md](RAG_Support_Design.md) — RAG implementation details
- [SESSIONS.md](SESSIONS.md) — Session recording format
