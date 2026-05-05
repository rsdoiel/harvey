
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
harvey.go                   Core types: Agent, LLMClient, Message, ChatStats,
                              ExpandDynamicSections
config.go                   Config struct, DefaultConfig(), LoadHarveyMD(),
                              permissions, timeout, safe-mode configuration
terminal.go                 REPL loop (Run), startup sequence, backend selection
commands.go                 All slash-command handlers and dispatch
workspace.go                Sandboxed file I/O (Workspace)
knowledge.go                SQLite knowledge base (KnowledgeBase)
audit.go                    In-memory ring-buffer audit log (AuditBuffer)
permissions.go              Permission check helpers (CheckReadPermission, etc.)
ollama.go                   OllamaClient helpers and probing utilities
anyllm_client.go            AnyLLMClient — wraps mozilla-ai/any-llm-go
routing.go                  RouteRegistry, named remote endpoints, @mention dispatch
spinner.go                  Animated spinner with Edward Lear messages + timer
recorder.go                 Markdown session recorder
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
| `Recorder` | `*Recorder` | Optional Markdown session log |
| `In` | `io.Reader` | Interactive prompt source (default `os.Stdin`; injectable for tests) |
| `PinnedContext` | `string` | Text that survives `/clear` and is re-injected after the system prompt |
| `commands` | `map[string]*Command` | Registered slash commands |
| `statHistory` | `[]ChatStats` | Rolling window of up to 5 past turns for duration estimation |

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

`Chat` streams tokens to `out` as they arrive and returns `ChatStats` containing
`PromptTokens`, `ReplyTokens`, `Elapsed`, and `TokensPerSec` once the response
is complete. Backends that do not report token counts (currently PublicAI)
return zeros for those fields; only `Elapsed` is always populated.

### ChatStats & duration estimation

After each successful turn the REPL records stats via `agent.recordStats()`,
which maintains a rolling window of the last 5 turns. Before the next turn,
`agent.estimateDuration()` averages `ReplyTokens / TokensPerSec` across all
window entries that have token data, returning a `time.Duration` estimate that
the spinner displays as `[Xs / ~Ys]`.

### Message

```go
type Message struct {
    Role    string `json:"role"`    // "system", "user", or "assistant"
    Content string `json:"content"`
}
```

The full `History` slice is sent on every call to `Chat`; there is no partial
or sliding-window trimming beyond the explicit `/summarize` command.

## Workspace sandboxing (`workspace.go`)

`Workspace` resolves the root to an absolute, symlink-free path at construction
time. Every subsequent operation calls `AbsPath(rel)`, which joins `rel` under
`Root` and then checks that the result still has `Root` as a prefix — rejecting
`../` escapes and absolute paths outside the root. The `agents/` subdirectory
is created automatically and holds `knowledge.db`.

All file I/O in command handlers goes through `ws.ReadFile`, `ws.WriteFile`,
`ws.ListDir`, or `ws.MkdirAll`; direct `os.*` calls are not used for
workspace paths.

## Conversation model

### History lifecycle

```
startup
  └─ system message  ← HARVEY.md (after ExpandDynamicSections)
  └─ user message    ← [pinned context] if PinnedContext != ""

each turn
  user types input
  └─ AddMessage("user", input)
  └─ Client.Chat(History, &buf)  ← full History sent every time
  └─ AddMessage("assistant", buf.String())

/clear
  └─ History reset to [system message, pinned context message]

/summarize
  └─ History + summarizePrompt → LLM
  └─ ClearHistory()             ← re-injects system + pinned
  └─ AddMessage("user", "[Conversation summary]\n\n"+summary)
```

### Pinned context (`PinnedContext`)

`PinnedContext` is a free-form string managed by `/context add|show|clear`.
`add` appends (newline-separated); there is always at most one
`[pinned context]` message in `History` — subsequent `add` calls update it
in-place. `ClearHistory` re-injects it automatically, so it also survives
`/summarize`.

### Dynamic system prompt (`ExpandDynamicSections`)

`LoadHarveyMD` reads `HARVEY.md` from the process working directory.
`ExpandDynamicSections` is then called before the system message is injected,
replacing these markers with live data:

| Marker | Replaced with |
|---|---|
| `<!-- @date -->` | Current date (`YYYY-MM-DD`) |
| `<!-- @files -->` | Workspace file tree (hidden dirs excluded) |
| `<!-- @git-status -->` | `git status --short`, or `(not a git repository)` |

Example `HARVEY.md` header:

```markdown
You are a coding assistant for this Go project.
Date: <!-- @date -->
Workspace:
<!-- @files -->
Git:
<!-- @git-status -->
```

## Slash-command system (`commands.go`)

Commands are registered in `registerCommands()` as `map[string]*Command`.
Each `Command` has a `Handler func(a *Agent, args []string, out io.Writer) error`.
The REPL calls `dispatch(input, out)` for any line beginning with `/`.

### Foundation commands

| Command | Purpose |
|---|---|
| `/help` | List all commands |
| `/status` | Show backend, history length, workspace, KB, recording state |
| `/clear` | Reset history (re-injects system prompt + pinned context) |
| `/exit` `/quit` `/bye` | End the session |

### Backend commands

| Command | Purpose |
|---|---|
| `/ollama start\|stop\|status\|list\|use MODEL` | Control the local Ollama service |
| `/route add\|remove\|list\|enable\|disable` | Manage named remote endpoints |
| `/model [NAME]` | Switch Ollama model; list available models |

### Knowledge-base commands

| Command | Purpose |
|---|---|
| `/kb status` | Print project/concept/observation summary |
| `/kb project list\|add NAME [DESC]\|use ID` | Manage projects |
| `/kb observe [KIND] TEXT` | Record a note, finding, decision, question, or hypothesis |
| `/kb concept list\|add NAME [DESC]` | Manage shared concepts |

### Session recording

| Command | Purpose |
|---|---|
| `/record start [FILE]` | Begin writing turns to a Markdown file |
| `/record stop` | Close the recording |
| `/record status` | Show recording path or "not recording" |

### Tier 1 — file operations

| Command | Purpose |
|---|---|
| `/read FILE [FILE...]` | Read workspace files into conversation context |
| `/write PATH` | Write last assistant reply (or its first code block) to a file |
| `/run COMMAND [ARGS...]` | Run a shell command; inject stdout+stderr into context (8 KB cap) |

Context messages injected by these commands use a `[context: /cmd args]` label
so the model and user can distinguish injected context from typed conversation.

### Tier 2 — code assistance

| Command | Purpose |
|---|---|
| `/search PATTERN [PATH]` | Regexp search across workspace files (100-match cap, binary-safe) |
| `/git status\|diff\|log\|show\|blame [ARGS...]` | Read-only git commands; inject output into context |
| `/apply` | Detect fenced blocks tagged with filenames and write them to the workspace |

`/apply` uses `findTaggedBlocks` which recognises fence lines like
`` ```go harvey/spinner.go `` (language + path) or `` ```README.md `` (path only).
It prompts for a single Y/n confirmation before writing any files, reading from
`a.In` so the prompt is testable.

### Tier 3 — session quality

| Command | Purpose |
|---|---|
| `/summarize` | Condense history into a summary via the LLM; replaces full history |
| `/context add TEXT...` | Append text to pinned context (survives `/clear`) |
| `/context show` | Print current pinned context |
| `/context clear` | Remove pinned context from agent and history |

### File utility

| Command | Purpose |
|---|---|
| `/files [PATH]` | List workspace directory entries |

### Security commands

| Command | Purpose |
|---|---|
| `/safemode on\|off\|status\|allow CMD\|deny CMD\|reset` | Command allowlist; persisted to harvey.yaml |
| `/permissions list\|set PATH PERMS\|reset` | Path-prefix read/write/exec/delete control |
| `/audit show [N]\|clear\|status` | In-memory ring-buffer audit log (1000 events) |
| `/security` | Unified view of safe mode, permissions, and audit state |


## Security system (`audit.go`, `permissions.go`, `config.go`)

Harvey's security layer has four interlocking components.

**Command allowlist (safe mode).** When `Config.SafeMode` is true, every `!`
and `/run` invocation checks the command name against `Config.AllowedCommands`
before execution. Denied commands are audit-logged with `StatusDenied` and
the user sees which commands are permitted. Settings are persisted to
`agents/harvey.yaml` via `SaveRAGConfig` so they survive restart.

**Path permissions.** `Config.Permissions` is a `map[string][]string` keyed on
path prefixes. Before any `ws.ReadFile`, `ws.WriteFile`, or block-write in
`/apply`, the command handler calls `a.CheckReadPermission` /
`a.CheckWritePermission`. The most specific matching prefix wins; unmatched
paths are denied. Persisted under the `permissions:` key in harvey.yaml.

**Audit log.** `AuditBuffer` (`audit.go`) is a thread-safe ring buffer
(capacity 1000) using `sync.RWMutex`. It records action type, details, and
outcome (`allowed`, `denied`, `error`, `success`). The global instance is held
via `sync/atomic.Pointer[AuditBuffer]` so it can be written from multiple
goroutines without a lock on the pointer itself.

**Environment filtering.** Every child process launched via `!` or `/run`
receives a filtered environment from `filterCommandEnvironment` (`commands.go`).
Only variables in a safe-prefix allowlist (`PATH`, `HOME`, `USER`, `SHELL`,
`TERM`, `LANG`, `LC_*`, `PWD`, `OLLAMA_*`, `HARVEY_*`) are forwarded; all
cloud provider API key variables are unconditionally stripped.

**Timeout configuration.** `Config.RunTimeout` (default 5 minutes) bounds
every `!` and `/run` subprocess via `context.WithTimeout`. `Config.OllamaTimeout`
controls the HTTP client timeout for local LLM providers; it defaults to 0
(no timeout) because inference on slow hardware (Raspberry Pi) can take
several minutes. Both are configurable in harvey.yaml as duration strings
(`"5m"`, `"300s"`, `"1m30s"`) or plain integer seconds.


## Knowledge base (`knowledge.go`)

The SQLite database lives at `<workspace>/agents/knowledge.db` and is opened
with `MaxOpenConns(1)` and WAL mode. The schema has five tables:

| Table | Purpose |
|---|---|
| `projects` | One row per named project with status (`active`, `paused`, `concluded`) |
| `observations` | Timestamped notes attached to a project; kinds: `note`, `finding`, `decision`, `question`, `hypothesis` |
| `concepts` | Named terms that can span projects |
| `project_concepts` | M:N link between projects and concepts |
| `observation_concepts` | M:N link between observations and concepts |

A `project_summary` view joins projects with their concept names for display.

**Important:** `Summary()` drains all project rows into a slice and closes the
cursor before calling `recentObservations` for each project, avoiding a
deadlock that would occur if the outer `rows` cursor were held open while a
second query tried to acquire the single allowed connection.

## Session recording (`recorder.go`)

`Recorder` writes a Markdown file with a header (started timestamp, model,
workspace) followed by numbered `### Turn N` sections. It is started with
`/record start [FILE]` and closed on `/record stop` or clean session exit.
`DefaultSessionPath` generates a timestamped filename:
`<workspace>/harvey-session-YYYYMMDD-HHMMSS.md`.

## Spinner (`spinner.go`)

The `Spinner` runs a background goroutine that:

1. Rotates braille animation frames every 100 ms.
2. Cycles through 50 Edward Lear–themed waiting messages every 4 s.
3. Displays a live timer via `timerLabel(elapsed)`:
   - No estimate available: `[8s]`
   - Estimate available, still under: `[8s / ~12s]`
   - Elapsed exceeds estimate: `[13s]` (estimate dropped to avoid showing overrun)

`newSpinner(out, estimate)` accepts a `time.Duration` estimate (0 = none)
computed by `agent.estimateDuration()` from the rolling stat history.

## Backends

### OllamaClient (`ollama.go`)

Posts to `/api/chat` with `stream: true`. The final chunk with `done: true`
carries `eval_count`, `eval_duration`, `prompt_eval_count`, and
`prompt_eval_duration` (all in nanoseconds). These are parsed into `ChatStats`;
`TokensPerSec` is computed as `eval_count / (eval_duration / 1e9)`.

### AnyLLMClient (`anyllm_client.go`)

Wraps [mozilla-ai/any-llm-go](https://github.com/mozilla-ai/any-llm-go) and
implements `LLMClient`. Local providers (Ollama, Llamafile, llama.cpp) are
constructed with `anyllm.WithHTTPClient(&http.Client{})` when
`Config.OllamaTimeout` is zero, removing the library's default 120 s timeout
which was too short for inference on slow hardware. Cloud providers (Anthropic,
DeepSeek, Gemini, Mistral, OpenAI) use the library's default timeout.

Named remote endpoints are managed by `RouteRegistry` (`routing.go`). The
`@name` prefix in a prompt dispatches that turn to the named endpoint instead
of the primary backend.

### Backend selection at startup (`terminal.go`)

1. Probe Ollama at the configured URL.
2. If reachable: list models; auto-select if only one, otherwise present a menu.
3. If unreachable: offer to start `ollama serve`, then retry.
4. If nothing selected: Harvey starts without a backend; commands that need one
   (`/summarize`, chat turns) print a prompt to connect first.

## RAG — Retrieval-Augmented Generation (`rag_support.go`)

RAG augments each user prompt with relevant chunks retrieved from a local
SQLite store before the prompt is sent to the generation model. The key
types are:

| Type | Purpose |
|---|---|
| `Embedder` | Interface: `Embed(text) ([]float64, error)` + `Name() string` |
| `RagStore` | SQLite-backed chunk store; enforces single-embedder consistency |
| `Chunk` | Retrieved result: ID, Content, Score, Source |
| `RagStoreEntry` | Registry entry: Name, DBPath, EmbeddingModel, ModelMap |

**Embedding model binding.** Each `RagStore` is created with an explicit
embedding model name. `Ingest` and `Query` both reject an `Embedder` whose
`Name()` differs from the stored model — mixing vector spaces from different
models would produce meaningless similarity scores.

**Named store registry.** `Config` holds a `[]RagStoreEntry` registry and a
`RagActive` string naming the currently selected store. Only the active store
is opened at runtime; all others remain dormant on disk. Helpers:

- `Config.ActiveRagStore()` — returns the active entry or nil.
- `Config.RagStoreByName(name)` — lookup by name.
- `Config.AddOrUpdateRagStore(e)` — upsert into the registry.
- `Config.RemoveRagStore(name)` — remove from the registry.

**YAML persistence.** `SaveRAGConfig` writes new multi-store format:

```yaml
rag:
  enabled: true
  active: golang
  stores:
    - name: golang
      db_path: agents/rag/golang.db
      embedding_model: nomic-embed-text
```

Old single-store format (`db_path` / `embedding_model` at the top of `rag:`)
is read and automatically migrated to a store named `"default"` on load.

**Retrieval flow.** `Agent.ragAugment(prompt)` is called before every `Chat`
invocation when `RagOn` is true. It looks up the active store's embedding
model (resolving via `ModelMap` when the current generation model has a
specific override), queries the top 5 chunks, discards any below the
`ragMinScore` threshold (0.3 cosine similarity), and prepends a
`### Context (from knowledge base)` block to the prompt. The original prompt
is returned unchanged if RAG is off, the store is nil, or no chunks exceed
the threshold.

**Database layout.** One table per store:

```sql
CREATE TABLE chunks (
    id        INTEGER PRIMARY KEY,
    content   TEXT    NOT NULL,
    embedding BLOB    NOT NULL,
    source    TEXT    NOT NULL DEFAULT ''
);
```

Embeddings are stored as `[int32 length][float64...]` in little-endian byte
order. Cosine similarity is computed in Go at query time (no vector extension
required in SQLite).

## Test coverage

Tests live alongside source in six files:

| File | Covers |
|---|---|
| `workspace_test.go` | Path sandboxing, escape detection, file I/O |
| `harvey_test.go` | `ChatStats.Format`, rolling stats, duration estimation, `AddMessage`, `ClearHistory` |
| `spinner_test.go` | `timerLabel` boundary conditions |
| `recorder_test.go` | Session file write round-trip, path format |
| `knowledge_test.go` | Full KB CRUD, duplicate handling, links, `Summary` |
| `commands_test.go` | `extractCodeBlock`, `/read`, `/write`, `/run` |
| `tier2_test.go` | Helpers (`isBinary`, `looksLikePath`, `findTaggedBlocks`), `/search`, `/git`, `/apply` |
| `tier3_test.go` | Mock client, `/summarize`, `/context`, `ExpandDynamicSections` |
| `rag_support_test.go` | `RagStore` ingest, query, source round-trip, semantic ranking, mismatch rejection |

The `mockLLMClient` in `tier3_test.go` satisfies the `LLMClient` interface with
a configurable reply string and optional error, making it available for any
future test that needs an LLM without a live server.

## Roadmap

The commands implemented so far map to three tiers of increasing capability.
The following outlines the thinking behind each tier and where the natural next
steps lie.

### Tier 1 — file operations (implemented)

The minimum for a coding assistant: Harvey must be able to *see* code and
*produce* files.

- `/read` brings code into context.
- `/run` closes the feedback loop (build errors, test output).
- `/write` turns suggestions into actual files without copy-pasting.

### Tier 2 — code navigation (implemented)

Makes the workspace explorable without leaving Harvey.

- `/search` finds symbols and patterns across the whole codebase.
- `/git` gives Harvey awareness of what has changed.
- `/apply` auto-dispatches multi-file code suggestions, making Harvey feel
  agentic rather than advisory.

### Tier 3 — session quality (implemented)

Keeps long sessions useful and the system prompt rich.

- `/summarize` prevents context-window exhaustion on long refactoring sessions.
- `/context` pins invariants (target platform, coding conventions, open
  constraints) that should frame every response.
- Dynamic HARVEY.md sections give the model a live snapshot of the project at
  startup with no manual maintenance.

### Potential future directions

**Diff/patch workflow** — After `/apply`, run a `git diff` and offer a one-step
undo (`/unapply`). The current write is destructive; a patch-based apply would
stage changes and let the user review them with `git diff --staged` before
committing.

**Multi-file context loading** — `/read` currently requires explicit file names.
A `/context load-dir PATH [GLOB]` variant that recursively adds all matching
files (e.g. `*.go`) would reduce friction when reviewing a package or subsystem.

**Prompt templates** — A `/template use NAME` command backed by an
`agents/templates/` directory would let teams share structured prompts for
common tasks (code review, test generation, commit message drafting) without
repeating them each session.

**Knowledge-base auto-recording** — After each turn, Harvey could optionally
extract decisions or findings from the assistant reply and offer to record them
as observations in the KB (`/kb observe` today requires manual invocation).

**Token budget awareness** — The Ollama done-packet already returns
`prompt_eval_count`. Harvey could warn when the accumulated history is
approaching the model's context window and suggest `/summarize` before the
model starts silently truncating earlier context.

**Model benchmarking** — Since Harvey tracks `TokensPerSec` per turn, a
`/bench` command could run a fixed prompt against each installed Ollama model
and display a comparison table, helping users pick the right model for
latency-vs-quality tradeoffs on their hardware.

**Headless / pipe mode** — A non-interactive `harvey --run SCRIPT` mode that
reads slash commands from a file would enable Harvey to be driven from shell
scripts or CI workflows, applying the same workspace-sandboxed agent loop
without a human at the terminal.
