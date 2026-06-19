
# Harvey Architecture

Harvey is a terminal-based coding agent written in Go. It connects to a local
[Ollama](https://ollama.com) server (and optionally to cloud providers via named
routes) and provides an interactive REPL for code-focused conversations. The
design philosophy is: keep the LLM backend swappable, constrain all file I/O
to a sandboxed workspace, and accumulate context through explicit slash commands
rather than implicit background crawling.

## Component map

```
cmd/harvey/main.go          CLI entry point; parses flags; calls agent.Run()
cmd/assay/main.go           Evaluation harness binary (LLM prompt corpus runner)
harvey.go                   Core types: Agent, LLMClient, Message, ChatStats,
                              ExpandDynamicSections
config.go                   Config struct, DefaultConfig(), LoadHarveyMD(),
                              permissions, timeout, safe-mode, model aliases
terminal.go                 REPL loop (Run), startup sequence, backend selection,
                              prose tool-call dispatch, auto-execute reply
ui.go                       Terminal line editor, tab completion
completion_candidates.go    Completion candidate generation (paths, commands, topics)
commands.go                 All slash-command handlers and dispatch
workspace.go                Sandboxed file I/O (Workspace)
knowledge.go                SQLite knowledge base (KnowledgeBase)
audit.go                    In-memory ring-buffer audit log (AuditBuffer)
permissions.go              Permission check helpers (CheckReadPermission, etc.)
debuglog.go                 JSONL diagnostic event log (DebugLog); written when --debug
lear_messages.go            Edward Lear-themed spinner waiting messages

--- Backends ---
ollama.go                   OllamaClient helpers and probing utilities
anyllm_client.go            AnyLLMClient — wraps mozilla-ai/any-llm-go for cloud providers
llamafile.go                /llamafile command family; model registration and discovery
llamafile_service.go        Llamafile server lifecycle (start, probe, port selection)
model_cache.go              ModelCache — capability cache for installed Ollama models
routing.go                  RouteRegistry, named remote endpoints, @mention dispatch
route_persist.go            Persistence helpers for routes.json

--- Tool system ---
tool_registry.go            ToolRegistry — named tools with JSON schemas; Dispatch()
tool_executor.go            ToolExecutor — RunToolLoop (structured multi-turn tool calls)
tools.go                    Tool schema definitions and LLMClient tool extensions
builtin_tools.go            Built-in tools: read_file, write_file, list_files,
                              run_command, git, create_dir, get_user_info

--- Memory (three-silo) ---
memory_store.go             MemoryStore — typed experience records in agents/memories/
memory_miner.go             Miner — LLM-based extraction from Fountain session files
memory_manifest.go          Manifest — tracks which sessions have been mined
memory_unified.go           UnifiedMemory — unified Recall() across all three silos
memory_rolling.go           Rolling summary — compresses history when budget exceeded
memory_scrub.go             Credential scrubbing before memory review cards
memory_onboarding.go        Workspace-profile onboarding on first run
memory_enrichment_*.go      Design/plan documents (not compiled)

--- Skills ---
skills.go                   SkillCatalog, skill discovery and metadata
skill_dispatch.go           Skill dispatch (compiled script path vs. LLM fallback)
skill_compile.go            Compiled skill script generation via LLM
skill_wizard.go             Interactive skill creation wizard (/skill new)
skill_set.go                SkillSet — YAML bundles of multiple skills

--- Pipelines and automation ---
pipeline.go                 /pipeline — multi-step confidence-gated prompt chains
plan.go                     Plan, PlanStep — GFM checklist plan data structures
plan_cmd.go                 /plan command handler; step execution and progress tracking
loop.go                     /loop — repeating prompt/command runner with interval

--- Session recording ---
recorder.go                 Fountain .spmd session recorder
sessions_files.go           Session file discovery, listing, MostRecentSession()
replay.go                   Session replay (re-run user turns against new model)

--- RAG ---
rag_support.go              RagStore, Embedder, chunk ingest/query, FTS5+cosine hybrid

--- Scholarly support ---
scholarly_identifiers.go    14-type identifier extraction (DOI, ORCID, ROR, ArXiv, etc.)
scholarly_pdf.go            Section-aware PDF chunking; identifier tagging for RAG

--- Language support ---
language_detector.go        Language detection from filename and content
language_registry.go        Language registry (extension → language mapping)
code_chunkers.go            Symbol-aware chunking for 13+ languages
code_formatters.go          Auto-formatters (gofmt, clang-format, prettier, black, etc.)
doc_extractors.go           Comment/docstring extraction and symbol association
syntax_highlighters.go      ANSI syntax highlighting for code blocks in LLM replies

--- Remote I/O ---
remote.go                   RemoteReader interface, URI-scheme factory
remote_http.go              HTTP/HTTPS backend with body limit and timeout
remote_s3.go                S3-compatible backend (AWS S3, MinIO, Cloudflare R2)
remote_sftp.go              SFTP/SCP backend with strict host-key verification

--- Utilities ---
pdf_extract.go              PDF text extraction via poppler (pdfinfo, pdftotext)
codeblock.go                Fenced code block parser
templates.go                Workspace profile templates
spinner.go                  Animated spinner: braille frames, Lear messages, live timer,
                              StatusCh for tool-call status updates
```

## Core types

### Agent (`harvey.go`)

`Agent` is the central struct that wires everything together for one session:

| Field | Type | Purpose |
|---|---|---|
| `Client` | `LLMClient` | Current backend (Ollama, Llamafile, cloud provider) |
| `Config` | `*Config` | Runtime configuration |
| `History` | `[]Message` | Full conversation sent to the LLM on every turn |
| `Workspace` | `*Workspace` | Sandboxed root for all file operations |
| `KB` | `*KnowledgeBase` | SQLite knowledge base (`agents/knowledge.db`) |
| `ModelCache` | `*ModelCache` | Capability cache for installed Ollama models |
| `Rag` | `*RagStore` | Active RAG chunk store; nil when not configured |
| `RagOn` | `bool` | When true, top-K chunks are prepended before each Chat call |
| `SessionsDir` | `string` | Absolute path to the sessions directory |
| `Skills` | `SkillCatalog` | Skills discovered at startup |
| `Recorder` | `*Recorder` | Optional Fountain .spmd session log |
| `In` | `io.Reader` | Interactive prompt source (default `os.Stdin`; injectable for tests) |
| `PinnedContext` | `string` | Text that survives `/clear`; re-injected after system prompt |
| `Routes` | `*RouteRegistry` | Registered remote endpoints; nil when routing not configured |
| `ActiveSkill` | `string` | Most recently loaded skill name; `""` when none |
| `ActiveSkillSet` | `string` | Currently loaded skill-set bundle name; `""` when none |
| `Tools` | `*ToolRegistry` | Schema-based tool registry; nil when tools disabled |
| `AuditBuffer` | `*AuditBuffer` | In-memory audit log ring buffer |
| `DebugLog` | `*DebugLog` | JSONL diagnostic log; nil when `--debug` not set |
| `OllamaStartedByHarvey` | `bool` | True when Harvey launched the Ollama subprocess |
| `llamafileProc` | `*os.Process` | Non-nil when Harvey started the current llamafile server |
| `sessionTurns` | `int` | Total completed user turns (used to trigger auto-mine) |
| `sessionInjectedTokens` | `int` | Tokens injected via UnifiedMemory this session |
| `sessionCompressed` | `bool` | True if rolling summary fired at least once this session |

### LLMClient interface (`harvey.go`)

Every backend implements this four-method interface:

```go
type LLMClient interface {
    Name()   string
    Chat(ctx context.Context, messages []Message, out io.Writer) (ChatStats, error)
    Models(ctx context.Context) ([]string, error)
    Close() error
}
```

`Chat` streams tokens to `out` as they arrive and returns `ChatStats` (containing
`PromptTokens`, `ReplyTokens`, `Elapsed`, `TokensPerSec`) once complete. Backends
that do not report token counts return zeros for those fields; `Elapsed` is always
populated.

### ChatStats & duration estimation

After each turn the REPL records stats via `agent.recordStats()`, which maintains
a rolling window of the last 5 turns. Before the next turn, `agent.estimateDuration()`
averages `ReplyTokens / TokensPerSec` across window entries that have token data,
returning a `time.Duration` estimate displayed by the spinner as `[Xs / ~Ys]`.

### Message

```go
type Message struct {
    Role    string `json:"role"`    // "system", "user", or "assistant"
    Content string `json:"content"`
}
```

The full `History` slice is sent on every call to `Chat`. Rolling summary
(`memory_rolling.go`) compresses older turns when the budget threshold is exceeded,
keeping the last N turns verbatim.

## Workspace sandboxing (`workspace.go`)

`Workspace` resolves the root to an absolute, symlink-free path at construction
time. Every subsequent operation calls `AbsPath(rel)`, which joins `rel` under
`Root` and then checks that the result still has `Root` as a prefix — rejecting
`../` escapes and absolute paths outside the root. The `agents/` subdirectory
is created automatically and holds `knowledge.db`, `memories/`, `sessions/`, and
`rag/*.db`.

All file I/O in command handlers goes through `ws.ReadFile`, `ws.WriteFile`,
`ws.ListDir`, or `ws.MkdirAll`; direct `os.*` calls are not used for workspace
paths.

## Conversation model

### History lifecycle

```
startup
  └─ system message  ← HARVEY.md (after ExpandDynamicSections)
  └─ user message    ← [pinned context] if PinnedContext != ""
  └─ user message    ← [memory context] from UnifiedMemory.Recall()

each turn
  user types input
  └─ AddMessage("user", input)
  └─ ragAugment(input) prepends context chunks when RagOn == true
  └─ Client.Chat(History, &buf)  ← full History sent every time
     └─ RunToolLoop if model uses structured tool_calls
     └─ tryExecuteProseToolCalls if model uses JSON fenced blocks
  └─ AddMessage("assistant", buf.String())

/clear
  └─ History reset to [system message, pinned context message]
  └─ memoryContextPending = true  → re-injected on next user turn

/summarize
  └─ History + summarizePrompt → LLM
  └─ ClearHistory()             ← re-injects system + pinned
  └─ AddMessage("user", "[Conversation summary]\n\n"+summary)
```

### Pinned context (`PinnedContext`)

`PinnedContext` is a free-form string managed by `/context add|show|clear`.
`add` appends (newline-separated); there is always at most one `[pinned context]`
message in `History` — subsequent `add` calls update it in-place. `ClearHistory`
re-injects it automatically, so it survives `/summarize`.

### Dynamic system prompt (`ExpandDynamicSections`)

`LoadHarveyMD` reads `HARVEY.md` from the process working directory.
`ExpandDynamicSections` is called before the system message is injected,
replacing these markers with live data:

| Marker | Replaced with |
|---|---|
| `<!-- @date -->` | Current date (`YYYY-MM-DD`) |
| `<!-- @files -->` | Workspace file tree (hidden dirs excluded) |
| `<!-- @git-status -->` | `git status --short`, or `(not a git repository)` |

## Slash-command system (`commands.go`)

Commands are registered in `registerCommands()` as `map[string]*Command`.
Each `Command` has a `Handler func(a *Agent, args []string, out io.Writer) error`.
The REPL calls `dispatch(input, out)` for any line beginning with `/`.

### Workspace commands

| Command | Purpose |
|---|---|
| `/files [PATH]` | List workspace directory contents |
| `/read FILE [FILE...]` | Inject file contents into context |
| `/attach FILE` | Attach image, PDF, or text; selects best representation for route |
| `/read-pdf FILE [PAGES]` | Extract PDF text via poppler; inject into context |
| `/write PATH` | Save last assistant reply (or first code block) to a file |
| `/read-dir [PATH] [--depth N]` | Read all eligible files in a directory tree into context |
| `/file-tree [PATH]` | Display recursive directory tree |
| `/search PATTERN [PATH]` | Regex search across workspace files |
| `/run COMMAND [ARGS...]` | Run shell command; subject to safe mode and timeout |
| `/git status\|diff\|log\|show\|blame [ARGS...]` | Read-only git commands |
| `/format FILE [FILE...]` | Detect and apply language-appropriate formatters |

### Model and backend commands

| Command | Purpose |
|---|---|
| `/ollama start\|stop\|status\|list\|ps\|pull\|push\|show\|create\|cp\|rm\|probe\|logs\|use\|env\|alias` | Manage local Ollama server |
| `/inspect [MODEL]` | Show detailed model capability information |
| `/route add\|rm\|models\|probe\|set\|list\|on\|off\|status` | Manage named remote endpoints |
| `/llamafile add\|use\|list\|start\|status\|drop` | Manage llamafile model backends |

### Context and history commands

| Command | Purpose |
|---|---|
| `/context add TEXT...\|show\|clear` | Manage pinned context that survives `/clear` |
| `/clear` | Reset conversation history (system prompt + pinned context survive) |
| `/summarize` | Condense history to a summary; `/compact` is an alias |
| `/status` | Show backend, token usage, routing, recording, memory, debug state |
| `/hint` | Actionable suggestions (unmined sessions, empty RAG, empty KB) |

### Session commands

| Command | Purpose |
|---|---|
| `/record start [FILE]\|stop\|status` | Start or stop Fountain session recording |
| `/rename NAME` | Rename the active session file without interrupting recording |
| `/session continue FILE\|replay FILE [OUTPUT]` | Load or replay a prior session |

### Knowledge-base commands

| Command | Purpose |
|---|---|
| `/kb status\|search\|inject\|project\|observe\|concept` | Query and update SQLite knowledge base |
| `/rag list\|new\|use\|drop\|ingest\|status\|query\|on\|off` | Manage RAG stores |
| `/memory mine\|list\|show\|flag\|forget\|status\|recall\|profile` | Manage session-experience memory store |
| `/recall QUERY` | Search all knowledge silos (alias for `/memory recall`) |

### Skills commands

| Command | Purpose |
|---|---|
| `/skill list\|load NAME\|info NAME\|status\|new\|run NAME` | Discover, load, and run skills |
| `/skill-set list\|load NAME\|info NAME\|create NAME\|status\|unload` | Manage skill-set bundles |

### Pipelines and automation commands

| Command | Purpose |
|---|---|
| `/pipeline CONFIDENCE% FILE [FILE...]` | Chain Markdown prompt files as confidence-gated steps |
| `/plan TASK\|next\|status\|show\|clear` | Generate a GFM checklist plan; execute each step |
| `/loop INTERVAL [--count N] PROMPT\|/COMMAND` | Run a prompt or command repeatedly on an interval |

### Security commands

| Command | Purpose |
|---|---|
| `/safemode on\|off\|status\|allow CMD\|deny CMD\|reset` | Command allowlist; persisted to harvey.yaml |
| `/safe` | Alias for `/safemode` |
| `/permissions list\|set PATH PERMS\|reset` | Path-prefix read/write/exec/delete control |
| `/audit show [N]\|clear\|status` | In-memory ring-buffer audit log (1000 events) |
| `/security status` | Unified view of safe mode, permissions, and audit state |

## Security system (`audit.go`, `permissions.go`, `config.go`)

Harvey's security layer has four interlocking components.

**Command allowlist (safe mode).** When `Config.SafeMode` is true, every `!`
and `/run` invocation checks the command name against `Config.AllowedCommands`
before execution. Denied commands are audit-logged with `StatusDenied`. Settings
are persisted to `agents/harvey.yaml` and survive restart.

**Path permissions.** `Config.Permissions` is a `map[string][]string` keyed on
path prefixes. Before any file read, write, or block-write, the command handler
calls `a.CheckReadPermission` / `a.CheckWritePermission`. The most specific
matching prefix wins; unmatched paths are denied. Persisted under `permissions:`
in harvey.yaml.

**Audit log.** `AuditBuffer` (`audit.go`) is a thread-safe ring buffer
(capacity 1000) using `sync.RWMutex`. It records action type, details, and
outcome (`allowed`, `denied`, `error`, `success`). The global instance is held
via `sync/atomic.Pointer[AuditBuffer]` so it can be written from multiple
goroutines.

**Environment filtering.** Every child process launched via `!` or `/run`
receives a filtered environment from `filterCommandEnvironment` (`commands.go`).
Only variables matching a safe-prefix allowlist (`PATH`, `HOME`, `USER`,
`USERNAME`, `SHELL`, `TERM`, `LANG`, `LC_*`, `PWD`, `OLLAMA_*`, `HARVEY_*`)
are forwarded. The following variables are unconditionally stripped:

- LLM provider API keys: `ANTHROPIC_API_KEY`, `COHERE_API_KEY`, `DEEPSEEK_API_KEY`,
  `GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY`,
  `OPENAI_API_KEY`, `PERPLEXITY_API_KEY`
- S3-compatible credentials: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
  `AWS_SESSION_TOKEN`, `AWS_SECURITY_TOKEN`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`
- SFTP/SCP credentials: `SFTP_PASSWORD`, `SFTP_KEY_PATH`
- HTTP authentication: `HTTP_BEARER_TOKEN`, `HTTP_BASIC_PASSWORD`

Note: all variables matching the `HARVEY_*` prefix are forwarded to child
processes (including compiled skills). Avoid storing secrets in `HARVEY_`-prefixed
variables.

**Timeout configuration.** `Config.RunTimeout` (default 5 minutes) bounds every
`!` and `/run` subprocess via `context.WithTimeout`. `Config.OllamaTimeout`
controls the HTTP client timeout for local LLM providers; it defaults to 0 (no
timeout) because inference on slow hardware can take several minutes.

## Knowledge base (`knowledge.go`)

The SQLite database lives at `<workspace>/agents/knowledge.db` and is opened
with `MaxOpenConns(1)` and WAL mode. The schema has five tables, extended in
v0.0.11 for scholarly identifiers:

| Table | Purpose |
|---|---|
| `projects` | One row per named project with status (`active`, `paused`, `concluded`) |
| `observations` | Timestamped notes per project; kind: `note`/`finding`/`decision`/`question`/`hypothesis`; `source_doi` column for scholarly citations |
| `concepts` | Named terms spanning projects; `identifier_type` and `identifier_value` columns for scholarly entities (people, papers, institutions, funders) |
| `project_concepts` | M:N link between projects and concepts |
| `observation_concepts` | M:N link between observations and concepts |

A `project_summary` view joins projects with their concept names for display.

## Three-silo memory architecture

Harvey has three independent knowledge stores that are unified at retrieval time
by `UnifiedMemory.Recall()` (`memory_unified.go`):

| Silo | Files | Content | Injection point |
|---|---|---|---|
| **RAG store** | `rag_support.go`, `agents/rag/*.db` | Vector-embedded document chunks | Per-prompt via `ragAugment()` when RAG is on |
| **Memory store** | `memory_store.go`, `memory_miner.go`, `memory_manifest.go`, `agents/memories/` | Typed experience records extracted from sessions | Session start via `UnifiedMemory.Recall()` |
| **Knowledge base** | `knowledge.go`, `agents/knowledge.db` | Hand-authored experiments/observations/concepts (relational) | Optional; also via `UnifiedMemory` |

### Memory store

Experience records are typed as `tool_use`, `workflow`, `user_preference`,
`workspace_profile`, or `project_fact`. Each record carries three enrichment fields:

- `kind` — why this knowledge matters: `pitfall`, `workaround`, `recommendation`, or `pattern`
- `action` — an imperative step a future agent should take; included in embedding text
- `confidence` — float 0.0–1.0 (default 0.5); final retrieval score = `cosine × confidence`

`/memory flag ID` reduces confidence by 0.1 per call; memories at or below 0.2
are auto-archived. `WriteDigest()` writes `agents/memories/DIGEST.md` on every
save, archive, and auto-mine — a plain Markdown index readable by any LLM.

### Memory mining flow

```
Manifest.UnminedSessions()   → find .spmd files not yet in the manifest
Miner.Mine(session)          → send session text to LLM for JSON extraction
interactive review           → user accepts / edits / skips each proposed memory
MemoryStore.Save()           → write typed Fountain files to agents/memories/
```

Auto-mine triggers on session exit when `sessionTurns >= 10`.

### UnifiedMemory.Recall() priority order

1. `workspace_profile` + `project_fact` memories — always injected first (score 1.0)
2. Experiential memories (FTS5 full-text + cosine × confidence) — ranked by final score
3. RAG chunks — relevance-ranked, discarded below `ragMinScore` (0.3)
4. KB observations — optional

Token budget is enforced via `memory.budget_pct` in harvey.yaml (default 0.25 of
the model context window). Setting `memory.inject_on_start: false` disables injection.

### Rolling summary (`memory_rolling.go`)

When `len(History)` token count reaches `memory.rolling_summary.warn_at_pct`
(default 80 %) of the context window, Harvey compresses all but the last
`rolling_summary.keep_turns` turns (default 6) into a ~150-token summary.
The session recording on disk retains the full pre-compression history.

## Tool registry and executor

### ToolRegistry (`tool_registry.go`)

`ToolRegistry` holds named tool definitions with JSON schemas. `Dispatch(name, argsJSON, maxBytes)`
looks up the tool by name and calls its handler, returning a string result or an
error. `ToolRegistry.Dispatch` returns `fmt.Errorf("unknown tool %q", name)` for
unregistered names — `tryExecuteProseToolCalls` detects this via
`strings.Contains(r.Content, "unknown tool")`.

### Two execution paths (`tool_executor.go`, `terminal.go`)

Harvey uses two separate paths depending on model capability:

**Structured tools (`RunToolLoop` in `tool_executor.go`)** — used when the model
returns proper `tool_calls` in the API response. Multi-turn: LLM → tools →
results appended to history → LLM again. `ToolExecutor.Status` accepts a
`StatusReporter` (satisfied by `*Spinner`) to display transient "Calling tool…"
status during execution. Prior tool-call rounds are compacted before each new LLM
turn to keep context bounded.

**Prose tool calls (`tryExecuteProseToolCalls` in `terminal.go`)** — used when
small models emit JSON in fenced blocks instead of structured calls. Single-turn
only. Returns `(dispatched bool, unknownNames []string)`. When `dispatched` is
false, Harvey prints a "try /tools off or a larger model" warning. When
`unknownNames` is non-empty, available tool names are printed and a correction
message is injected into history.

The `noToolCalls` flag is captured **before** `AddMessage` — after that call the
lengths are never equal, so it must be read first.

### Built-in tools (`builtin_tools.go`)

| Tool | Purpose |
|---|---|
| `read_file` | Read a workspace file; supports PDF (via poppler) and images (vision routes) |
| `write_file` | Write to a workspace file; applies auto-formatters when `auto_format` is on |
| `list_files` | List workspace directory contents |
| `run_command` | Run a shell command (subject to safe mode) |
| `git` | Read-only git operations |
| `create_dir` | Create a directory inside the workspace |
| `get_user_info` | Return OS username, git user name/email, hostname |

## Skills system

Skills are `SKILL.md` files discovered at startup in:
- `<workspace>/agents/skills/` — workspace-local skills
- `~/.harvey/skills/` — user-global skills

`SkillCatalog` (`skills.go`) maps skill names to `SkillMeta` (path, description,
trigger, compatibility, license). The catalog summary is added to the system
prompt so the model knows what skills are available.

### Skill dispatch (`skill_dispatch.go`)

When `/skill run NAME` is called:

1. **Compiled path** — if `scripts/compiled.bash` (Linux/macOS) or
   `scripts/compiled.ps1` (Windows) exists, the script is executed directly.
   Environment includes `HARVEY_PROMPT`, `HARVEY_WORKDIR`, `HARVEY_MODEL`,
   `HARVEY_SESSION_ID`, and `HARVEY_API_BASE` (for compiled skills that call
   the LLM API directly).

2. **LLM-fallback path** — if no compiled script, the full SKILL.md body is
   injected into context and an LLM response turn is triggered.

Compilation failure falls back to the LLM-fallback path rather than erroring.
Skill trigger regexes use `/pattern/flags` format; the trailing flag suffix is
stripped before compilation.

### Skill sets (`skill_set.go`)

`SkillSetMeta` defines a YAML bundle (`agents/skill-sets/*.yaml`) that groups
multiple skills by name. `/skill-set load NAME` injects every skill in the bundle.

### Skill wizard (`skill_wizard.go`)

`/skill new` launches an interactive wizard that prompts for name, description,
trigger, and compatibility, then scaffolds a new `SKILL.md` in `agents/skills/`.

## Pipelines and automation

### Pipeline (`pipeline.go`)

`/pipeline CONFIDENCE% FILE [FILE...]` chains Markdown prompt files as discrete
steps. After each step Harvey attempts to extract a confidence score using three
strategies (explicit "Confidence: N%", score patterns, and sentiment heuristics).
If the score falls below the threshold, the pipeline stops with a report.

### Plan (`plan.go`, `plan_cmd.go`)

`Plan` holds a slice of `PlanStep` (GFM checkbox items). `/plan TASK` asks the
LLM to generate a checklist, saves it to `agents/plan.md`, then executes each
step with fresh bounded context. Progress is tracked in the plan file:
- `[ ]` — pending
- `[x]` — completed
- `[!]` — failed or blocked

Steps with blocked or failed tool calls are not auto-marked complete.

### Loop (`loop.go`)

`/loop INTERVAL [--count N] PROMPT|/COMMAND` runs a prompt or slash command
repeatedly on a fixed interval. Useful for polling, watch loops, and
repeated evaluation.

## Session recording (`recorder.go`, `sessions_files.go`, `replay.go`)

Sessions are recorded as Fountain screenplay dialect files (`.spmd`) in
`agents/sessions/`, not plain Markdown. Key recorder calls:

- `RecordTurnWithStats` — normal chat turn
- `RecordAgentAction` — file write confirmation
- `RecordShellCommand` — `!` command execution

`ListSessionFiles` returns `.spmd`/`.fountain` files sorted newest-first.
`MostRecentSession(dir)` delegates to `ListSessionFiles` and returns the path
of the most recent file — used by `--resume` to automatically load the last
session at startup.

`replay.go` re-sends every user turn from a `.spmd` file to the current model
and records fresh responses, enabling cross-model comparison.

## Backends

### OllamaClient (`ollama.go`)

Posts to `/api/chat` with `stream: true`. The final chunk with `done: true`
carries eval counts and durations (nanoseconds). `TokensPerSec` is computed as
`eval_count / (eval_duration / 1e9)`.

### AnyLLMClient (`anyllm_client.go`)

Wraps [mozilla-ai/any-llm-go](https://github.com/mozilla-ai/any-llm-go) for
cloud providers (Anthropic, DeepSeek, Gemini, Mistral, OpenAI). Local providers
with `Config.OllamaTimeout == 0` use `anyllm.WithHTTPClient(&http.Client{})`
to remove the library's default 120 s timeout.

Named remote endpoints are managed by `RouteRegistry` (`routing.go`). The
`@name` prefix in a prompt dispatches that turn to the named endpoint.

### Llamafile backend (`llamafile.go`, `llamafile_service.go`)

Llamafile binaries are registered via `/llamafile add` and stored in
`Config.LlamafileModels`. `llamafile_service.go` handles the server lifecycle:
`FindFreePort` selects an available TCP port, `StartLlamafileServer` launches the
binary via `/bin/sh` (required on macOS for APE format binaries), and probes the
health endpoint before returning. Key config fields:

| Field | Default | Purpose |
|---|---|---|
| `startup_timeout` | 120s | Fast-fail if server does not become ready |
| `gpu_layers` | 99 | `-ngl` flag for Metal/CUDA layer offload |

### Backend selection at startup (`terminal.go`)

1. Probe Ollama at the configured URL.
2. If reachable: list models; auto-select if only one, otherwise present a menu.
3. If unreachable: offer to start `ollama serve`, then retry.
4. If nothing selected: Harvey starts without a backend; commands that need one
   print a prompt to connect first.

## RAG — Retrieval-Augmented Generation (`rag_support.go`)

| Type | Purpose |
|---|---|
| `Embedder` | Interface: `Embed(text) ([]float64, error)` + `Name() string` |
| `RagStore` | SQLite-backed chunk store; enforces single-embedder consistency |
| `Chunk` | Retrieved result: ID, Content, Score, Source |
| `RagStoreEntry` | Registry entry: Name, DBPath, EmbeddingModel, ModelMap |

**Embedding model binding.** Each `RagStore` is bound to one embedding model at
creation. `Ingest` and `Query` both reject an `Embedder` whose `Name()` differs
from the stored model.

**Hybrid retrieval.** `RagStore.Query` uses FTS5 full-text search as a fast
lexical pass, then re-ranks results using cosine similarity. Chunks below
`ragMinScore` (0.3) are discarded. The same threshold is used in `terminal.go`
and `cmd/assay/main.go` — keep them in sync if changed.

**Retrieval flow.** `Agent.ragAugment(prompt)` is called before every `Chat`
invocation when `RagOn` is true. It embeds the prompt, queries the active store
for the top 5 chunks, discards any below the threshold, and prepends a
`### Context (from knowledge base)` block. Silent no-op if the store is empty or
nothing exceeds threshold.

**Database layout.** One table per store:

```sql
CREATE TABLE chunks (
    id        INTEGER PRIMARY KEY,
    content   TEXT    NOT NULL,
    embedding BLOB    NOT NULL,
    source    TEXT    NOT NULL DEFAULT ''
);
```

Embeddings are stored as `[int32 length][float64...]` in little-endian byte order.
Cosine similarity is computed in Go at query time.

## Spinner (`spinner.go`)

The `Spinner` runs a background goroutine that:

1. Rotates braille animation frames every 100 ms.
2. Cycles through Edward Lear–themed waiting messages every 4 s.
3. Displays a live timer: `[8s]` / `[8s / ~12s]` / drops estimate when overrun.
4. Displays a transient tool-call status line via `StatusCh chan string`. The
   `ToolExecutor` calls `spin.UpdateStatus("Calling read_file…")` before each
   tool dispatch; the spinner renders it on a second line below the animation.

`Spinner` satisfies the `StatusReporter` interface (`UpdateStatus(msg string)`),
making it directly assignable to `ToolExecutor.Status`.

## Test coverage

Tests live alongside source files. Current test files:

| File | Covers |
|---|---|
| `workspace_test.go` | Path sandboxing, escape detection, file I/O |
| `harvey_test.go` | `ChatStats.Format`, rolling stats, duration estimation, `AddMessage`, `ClearHistory` |
| `spinner_test.go` | `timerLabel` boundary conditions |
| `recorder_test.go` | Session recorder round-trip |
| `sessions_files_test.go` | Session file listing, `MostRecentSession` |
| `knowledge_test.go` | Full KB CRUD, duplicate handling, links, `Summary`, scholarly fields |
| `commands_test.go` | Core slash-command handlers |
| `tier2_test.go` | Helpers (`isBinary`, `looksLikePath`), `/search`, `/git` |
| `tier3_test.go` | Mock client, `/summarize`, `/context`, `ExpandDynamicSections` |
| `rag_support_test.go` | `RagStore` ingest, query, source round-trip, semantic ranking |
| `anyllm_client_test.go` | AnyLLMClient construction and routing |
| `codeblock_test.go` | Fenced code block parser |
| `code_chunkers_test.go` | Symbol-aware chunking for multiple languages |
| `code_formatters_test.go` | Auto-formatter detection and dispatch |
| `doc_extractors_test.go` | Comment/docstring extraction |
| `language_detector_test.go` | Language detection from filename and content |
| `language_registry_test.go` | Extension → language mapping |
| `syntax_highlighters_test.go` | ANSI highlighting output |
| `memory_store_test.go` | MemoryStore CRUD, enrichment fields, archiving |
| `memory_miner_test.go` | Miner extraction and scrub |
| `memory_rolling_test.go` | Rolling summary trigger and compression |
| `memory_unified_test.go` | UnifiedMemory.Recall() priority ordering |
| `memory_onboarding_test.go` | Workspace-profile onboarding flow |
| `memory_test.go` | Integration across memory subsystems |
| `llamafile_test.go` | Llamafile command parsing and model name derivation |
| `llamafile_service_test.go` | Port selection, server start/stop lifecycle |
| `tools_test.go` | Tool schema definitions and registry construction |
| `tool_executor_test.go` | RunToolLoop multi-turn execution, compaction |
| `skill_dispatch_test.go` | Compiled vs. LLM-fallback dispatch paths |
| `skill_compile_test.go` | Skill compilation prompt and output |
| `skill_set_test.go` | SkillSet YAML loading and validation |
| `skills_test.go` | Skill discovery and catalog |
| `skill_wizard_test.go` | Wizard scaffolding output |
| `pipeline_test.go` | Confidence extraction and pipeline step gating |
| `plan_test.go` | Plan parsing, step state transitions |
| `loop_test.go` | Loop interval parsing and execution |
| `routing_test.go` | RouteRegistry CRUD and @mention dispatch |
| `routing_phase4_test.go` | Routing phase 4 integration scenarios |
| `config_test.go` | Config load, YAML migration, model alias resolution |
| `remote_test.go` | RemoteReader scheme selection |
| `remote_sftp_test.go` | SFTP packet size limits and string length caps |
| `scholarly_identifiers_test.go` | 14-type identifier extraction and normalization |
| `scholarly_pdf_test.go` | Section-aware PDF chunking |
| `pdf_extract_test.go` | Poppler-based text extraction |
| `ollama_probe_test.go` | Ollama server health probing |
| `agent_integration_test.go` | Full agent lifecycle integration tests |
| `profile_use_test.go` | Profile template loading and `/profile use` flow |
| `phase_d_test.go` | Phase D feature integration tests |
| `phase_e_test.go` | Phase E feature integration tests |
| `ui_test.go` | Terminal line editor and completion |
| `terminal_test.go` | REPL startup sequence and backend selection |
| `model_cache_test.go` | ModelCache capability lookup and expiry |
| `encoderfile_embedder_test.go` | Embedder interface and embedding consistency |
| `templates_test.go` | Workspace profile template rendering |

The `mockLLMClient` in `tier3_test.go` is the canonical test double for
`LLMClient` — reuse it rather than creating new ones.

## Roadmap

Harvey is in active development. All major architectural components described
in this document are fully implemented as of v0.0.13:

- ✓ Three-silo memory (RAG, experience store, knowledge base)
- ✓ Tool registry and dual execution paths (structured + prose)
- ✓ Multi-backend routing (Ollama, Llamafile, cloud providers)
- ✓ Skills system (compiled and LLM-fallback)
- ✓ Pipelines, plan execution, and loop automation
- ✓ Scholarly identifier support (14 types) and scholarly PDF chunking
- ✓ Remote I/O (S3, HTTP/S, SFTP/SCP)
- ✓ Security system (safe mode, permissions, audit log, env filtering)

See `TODO.md` for current open items and `CHANGES.md` for the release history.
