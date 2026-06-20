
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

> **Version control note:** If your workspace is a git repository, add `agents/`
> to `.gitignore`. The directory contains configuration, databases, and session
> recordings that should not be committed to version control.

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
| `syntax_highlight` | boolean/null | No | `true` | ANSI colour highlighting of code blocks |
| `auto_format` | boolean/null | No | `true` | Auto-format files after `write_file` tool calls |
| `run_timeout` | string | No | `"5m"` | Timeout for `!` and `/run` commands |
| `ollama_timeout` | string | No | `""` (none) | HTTP timeout for Ollama; empty = no timeout |
| `safe_mode` | boolean/null | No | `true` | Restrict commands to allowlist |
| `allowed_commands` | string list | No | See Security section | Commands allowed when `safe_mode` is true |
| `permissions` | object | No | Full access | Path-prefix permission map |
| `model_aliases` | object | No | `{}` | Short name → full model identifier map |
| `llamafile` | object | No | See below | Llamafile backend configuration |
| `tools` | object | No | See below | Tool-calling behaviour |
| `memory` | object | No | See below | Memory system and RAG configuration |

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

**Llamafile Backend (`llamafile:`):**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `models_dir` | string | No | `~/Models` | Directory scanned for `.llamafile` executables |
| `active` | string | No | `""` | Name of the registered model to connect at startup |
| `url` | string | No | `http://localhost:8080` | API base URL for the llamafile server |
| `gpu_layers` | integer | No | `99` | Transformer layers to offload via `-ngl`; `99` = maximise GPU, `-1` = CPU only |
| `startup_timeout` | string | No | `120s` | How long to wait for the server to become ready |
| `models` | array | No | `[]` | Registered llamafile model entries |

**Llamafile Model Entry Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Short identifier used by `/llamafile use` and `@mention` |
| `path` | string | Yes | Absolute or `~/`-relative path to the `.llamafile` executable |
| `context_length` | integer | No | Context window in tokens; probed from the server when omitted |

`context_length` is used for the rolling-summary trigger and context-utilisation hints.
When omitted Harvey probes the server's `/v1/models` endpoint at startup and caches
the result in memory (not persisted to `harvey.yaml`). Set it explicitly when the
probe is unavailable or you want a fixed value.

```yaml
llamafile:
  models_dir: ~/Models
  active: qwen-coding
  url: http://localhost:8080
  gpu_layers: 99
  startup_timeout: 120s
  models:
    - name: qwen-coding
      path: ~/Models/Qwen2.5-Coder-7B-Q5_K_S.llamafile
      context_length: 32768
    - name: phi-mini
      path: ~/Models/Phi-3.5-mini-instruct-Q4_K_M.llamafile
```

**Security Configuration:**

Security fields are persisted by the `/safemode`, `/permissions`, and related commands.

**Default `allowed_commands`:** `ls`, `cat`, `grep`, `head`, `tail`, `wc`,
`find`, `stat`, `jq`, `htmlq`, `bat`, `batcat`

> **Security note:** `safe_mode` defaults to `true`. The default allowlist covers
> read-only inspection commands. Add commands with `/safemode allow CMD` as your
> workflow requires. Setting `safe_mode: false` permits the language model to run
> any command in `$PATH` — only do this in isolated environments you control.

**Timeout format** (`run_timeout`, `ollama_timeout`): Go duration string or plain
integer seconds. Examples: `"5m"`, `"300"`, `"1m30s"`. `ollama_timeout` should be
left unset for local hardware where inference can take several minutes.

**`permissions` map** — keys are path prefixes relative to the workspace root;
values are lists of allowed actions (`read`, `write`, `exec`, `delete`).
Harvey checks the most specific matching prefix. Default is full access to `.`.

```yaml
permissions:
  ".":           [read, write, exec, delete]   # workspace root — full access
  "docs/":       [read]                         # docs tree — read-only
  "scripts/":    [read, exec]                   # scripts — run but not modify
```

Permission rules:
- Prefix `"."` matches all paths (used as the catch-all default).
- A more specific prefix (longer string) takes priority over a shorter one.
- If no prefix matches, access is **denied** (secure by default).

**Model Aliases (`model_aliases:`):**

Short names that expand to full model identifiers in `@mention` switching and
`/model use`. Aliases are case-insensitive. Persisted by `/model alias add`.

```yaml
model_aliases:
  coder: qwen2.5-coder:7b
  phi: phi-mini          # registered llamafile name
  claude: claude-sonnet-4-6
```

**Tool Settings (`tools:`):**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean/null | No | `true` | Send tool schemas to models that support tool calling |
| `max_tool_calls_per_turn` | integer | No | `10` | Hard limit on tool-call rounds per user turn |
| `max_output_bytes` | integer | No | `65536` | Cap on tool output injected into context (64 KiB) |
| `tool_result_compaction` | boolean/null | No | `true` | Compact prior tool-call rounds before each new model call |

```yaml
tools:
  enabled: true
  max_tool_calls_per_turn: 10
  max_output_bytes: 65536
  tool_result_compaction: true
```

**Memory System (`memory:`):**

Controls Harvey's three-silo memory architecture: experience memories, RAG
retrieval, and knowledge base. The `memory:` block in `harvey.yaml` also houses
RAG store configuration (previously at the top level as `rag:`; old single-store
`rag:` configs are automatically migrated on first load).

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean/null | No | `true` | Enable the memory system at session start |
| `top_k` | integer | No | `5` | Number of experience memories retrieved at session start |
| `inject_on_start` | boolean/null | No | `true` | Inject memory context block into history on start |
| `budget_pct` | float | No | `0.25` | Fraction of context window reserved for memory injection |
| `rolling_summary` | object | No | See below | Automatic working-memory compression settings |
| `rag` | object | No | See RAG section | RAG stores (nested here in v0.0.14+) |
| `knowledge_base.path` | string | No | `"agents/knowledge.db"` | Path to the SQLite knowledge base |

**Rolling Summary (`memory.rolling_summary:`):**

When history token count exceeds `warn_at_pct` of the model's context window,
Harvey compresses all but the last `keep_turns` turns into a short summary.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean/null | No | `true` | Enable automatic working-memory compression |
| `warn_at_pct` | float | No | `0.80` | Fraction of context window that triggers compression |
| `keep_turns` | integer | No | `6` | Number of recent turns preserved verbatim after compression |

```yaml
memory:
  enabled: true
  top_k: 5
  inject_on_start: true
  budget_pct: 0.25
  rolling_summary:
    enabled: true
    warn_at_pct: 0.80
    keep_turns: 6
  knowledge_base:
    path: agents/knowledge.db
  rag:
    enabled: true
    active: golang
    stores:
      - name: golang
        db_path: agents/rag/golang.db
        embedding_model: nomic-embed-text
```

**Behaviour Fields (`syntax_highlight`, `auto_format`):**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `syntax_highlight` | boolean/null | No | `true` | ANSI colour highlighting of code blocks in responses |
| `auto_format` | boolean/null | No | `true` | Run the registered language formatter after `write_file` writes a source file |

```yaml
syntax_highlight: true
auto_format: true
```

**Complete harvey.yaml example:**

```yaml
# Paths
sessions_dir: agents/sessions
agents_dir: agents
model_cache_db: agents/model_cache.db
auto_record: true

# Llamafile backend (primary)
llamafile:
  models_dir: ~/Models
  active: qwen-coding
  startup_timeout: 120s
  gpu_layers: 99
  models:
    - name: qwen-coding
      path: ~/Models/Qwen2.5-Coder-7B-Q5_K_S.llamafile
      context_length: 32768

# Model short-name aliases
model_aliases:
  coder: qwen-coding
  phi: phi-mini

# Behaviour
syntax_highlight: true
auto_format: true

# Security
safe_mode: true
allowed_commands: [ls, cat, grep, head, tail, wc, find, stat, git, go, make]
run_timeout: "5m"
ollama_timeout: ""

permissions:
  ".":      [read, write, exec, delete]
  "docs/":  [read]

# Tool calling
tools:
  enabled: true
  max_tool_calls_per_turn: 10

# Memory system
memory:
  enabled: true
  top_k: 5
  budget_pct: 0.25
  rolling_summary:
    enabled: true
    warn_at_pct: 0.80
    keep_turns: 6
  rag:
    enabled: true
    active: golang
    stores:
      - name: golang
        db_path: agents/rag/golang.db
        embedding_model: nomic-embed-text
```

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
      "url": "ollama://192.0.2.12:11434",
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
      "url": "ollama://192.0.2.13:11434",
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
- `/route remove NAME` — Remove an endpoint (alias: `rm`)
- `/route use [NAME]` — Set NAME as the sticky route; omit to clear
- `/route list` — List all endpoints with status
- `/route on` — Enable routing globally
- `/route off` — Disable routing globally
- `/route status` — Show routing state

### 3. System Prompt (`<workspace>/HARVEY.md`)

**Purpose:** Provides the system prompt for Harvey's language model interactions. This is
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
- **Cloud privacy:** When a cloud route (`@claude`, `@openrouter`, etc.) is
  active, HARVEY.md is sent to the remote provider verbatim. Avoid hardcoding
  absolute local paths in HARVEY.md. Use the `<!-- @files -->` dynamic marker
  instead — it injects a workspace-relative file tree without exposing your
  home directory layout.


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
| `HARVEY_LLAMAFILE_DIR` | Llamafile | — | Override the llamafile discovery directory (takes precedence over `llamafile.models_dir` in `harvey.yaml`) |
| `SFTP_PASSWORD` | SFTP/SCP | `sftp://` `scp://` | Password for SFTP/SCP remote ingest (`/rag ingest sftp://…`) |
| `SFTP_KEY_PATH` | SFTP/SCP | `sftp://` `scp://` | Path to SSH private key for SFTP/SCP remote ingest |
| `HTTP_BEARER_TOKEN` | HTTP/HTTPS | `http://` `https://` | Bearer token for authenticated HTTP remote ingest |
| `HTTP_BASIC_USER` | HTTP/HTTPS | `http://` `https://` | Username for HTTP Basic Auth remote ingest |
| `HTTP_BASIC_PASSWORD` | HTTP/HTTPS | `http://` `https://` | Password for HTTP Basic Auth remote ingest |
| `AWS_ACCESS_KEY_ID` | S3 | `s3://` | AWS access key for S3 remote ingest |
| `AWS_SECRET_ACCESS_KEY` | S3 | `s3://` | AWS secret key for S3 remote ingest |
| `AWS_ENDPOINT_URL` | S3 | `s3://` | S3-compatible endpoint URL (MinIO, Cloudflare R2, etc.) |

**Security:** All API keys and credential environment variables are stripped from
the environment of every child process started by `!` or `/run`. They are never
visible to commands Harvey executes on your behalf.

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
> /route add pi2 ollama://192.0.2.12:11434 llama3.1:8b
harvey
> /route on

# 3. Configure RAG (optional)
harvey
> /rag new golang
harvey
> /rag use golang
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
- Run `/rag use NAME` to activate it
- Verify the database file exists at the specified path

### Verification Commands

> **Reminder:** Add `agents/` to `.gitignore` before committing. These files
> contain endpoint URLs, model names, and database state that should stay local.

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
# ── Paths ────────────────────────────────────────────────────────────────────
sessions_dir: agents/sessions          # default
agents_dir: agents                     # default
model_cache_db: agents/model_cache.db  # default

# ── Behaviour ────────────────────────────────────────────────────────────────
auto_record: true          # false to disable session auto-recording
syntax_highlight: true     # false to disable ANSI colour in code blocks
auto_format: true          # false to disable auto-formatting after write_file

# ── Llamafile backend ─────────────────────────────────────────────────────────
llamafile:
  models_dir: ~/Models            # discovery directory; $HOME/Models is the default
  active: qwen-coding             # name of the active registered model
  url: http://localhost:8080      # API base URL; this is the default
  gpu_layers: 99                  # -ngl value; 99 = maximise GPU, -1 = CPU only
  startup_timeout: 120s           # time to wait for the server to become ready
  models:
    - name: qwen-coding
      path: ~/Models/Qwen2.5-Coder-7B-Q5_K_S.llamafile
      context_length: 32768       # optional; probed from server when omitted

# ── Model aliases ─────────────────────────────────────────────────────────────
model_aliases:
  coder: qwen2.5-coder:7b         # short name → full Ollama model ID or llamafile name
  phi: phi-mini

# ── Security ──────────────────────────────────────────────────────────────────
safe_mode: true
allowed_commands:
  - ls
  - cat
  - grep
  - git
run_timeout: "5m"
ollama_timeout: ""                # empty = no timeout (recommended for local hardware)
permissions:
  ".":      [read, write, exec, delete]
  "docs/":  [read]

# ── Tool calling ──────────────────────────────────────────────────────────────
tools:
  enabled: true
  max_tool_calls_per_turn: 10     # hard limit per user turn
  max_output_bytes: 65536         # cap on tool output injected into context (64 KiB)
  tool_result_compaction: true    # compact prior tool-call rounds before new calls

# ── Memory system ─────────────────────────────────────────────────────────────
memory:
  enabled: true
  top_k: 5                        # experience memories retrieved at session start
  inject_on_start: true
  budget_pct: 0.25                # fraction of context window reserved for memory
  rolling_summary:
    enabled: true
    warn_at_pct: 0.80             # compression triggers at 80% context usage
    keep_turns: 6                 # recent turns kept verbatim after compression
  knowledge_base:
    path: agents/knowledge.db
  rag:
    enabled: true
    active: golang
    stores:
      - name: golang
        db_path: agents/rag/golang.db
        embedding_model: nomic-embed-text
        model_map:
          qwen2.5-coder:7b: nomic-embed-text
        embedder_kind: ollama     # or "encoderfile" for a local encoderfile binary
        embedder_url: ""          # base URL when embedder_kind = "encoderfile"
```

*For more details on specific subsystems, see:*
- [ROUTING.md](ROUTING.md) — Remote endpoint routing
- [KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md) — Knowledge base schema
- [RAG_Support_Design.md](RAG_Support_Design.md) — RAG implementation details
- [SESSIONS.md](SESSIONS.md) — Session recording format
