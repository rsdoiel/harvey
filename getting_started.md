
# Starting Harvey

## What is Harvey?

harvey is a terminal agent for local language models.

See [man page](harvey.1.md)

How it starts:

Harvey looks for HARVEY.md in the workspace root and uses it as a system
prompt. It then connects to a local model — either a llamafile executable
or an Ollama server — and starts an interactive chat session.

> **Tip:** The easiest way to get started is to download a llamafile from the
> [Mozilla AI pre-built llamafiles page](https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles),
> place it in `~/Models/`, and run `harvey`. Harvey will find and connect to
> it automatically. See [Llamafile Commands](harvey-llamafile.7.md) for details.
> Ollama is also supported as an alternative backend.

All file I/O is constrained to the workspace.
A knowledge base is stored at `<workspace>/agents/knowledge.db` and is
created automatically on first run. Session recordings are stored in
`<workspace>/agents/sessions/`. Both paths can be overridden in
`<workspace>/agents/harvey.yaml`.

> Type /help inside the session for available slash commands.

## Typical invocations

1. Change to your project directory (example: "$HOME/myproject")
2. Launch Harvey

```sh
# Change to project directory
cd $HOME/myproject

# Start Harvey — auto-detects llamafile or Ollama
harvey

# Use a specific llamafile for this session (not persisted)
harvey --llamafile ~/Models/Qwen2.5-Coder-7B-Q5_K_S.llamafile

# Use a specific Ollama model
harvey --model qwen2.5-coder:7b
```

## Startup sequence

When Harvey starts it:

1. Prints the banner and resolves the workspace root.
2. Opens (or creates) `agents/knowledge.db` in the workspace.
3. Loads `agents/harvey.yaml` if present (overrides paths for KB, sessions, agents).
4. Scans `agents/sessions/` for prior `.spmd` session files and
   offers to resume one (default: No). If a session is chosen, the model it
   used is pre-selected in the next step.
5. Reads `HARVEY.md` from the workspace root, expands any
   [dynamic markers](#dynamic-markers), and injects it as the system prompt.
6. Detects available model backends: scans `~/Models/` (or the configured
   `llamafile.models_dir`) for registered llamafile executables and probes
   the Ollama server. Llamafile models are offered first; Ollama models follow.
7. Selects the model — from the resumed session, from `--llamafile` or
   `--model` flags, or via an interactive picker.
8. If no model is reachable, prints the first-run guide with download
   instructions and exits.
9. Begins recording the session to a new `.spmd` file in `agents/sessions/`.
10. Drops you into the REPL prompt: `harvey > `

## The HARVEY.md system prompt

Harvey looks for `HARVEY.md` in the directory where it is launched and uses
its contents as the LLM system prompt for the whole session. This is the
primary way to give the model persistent context about your project.

A minimal `HARVEY.md`:

```markdown
You are a coding assistant for a Go project.
The codebase uses Go 1.26, targets macOS and Linux.
Prefer table-driven tests and avoid third-party testing libraries.
```

## Dynamic markers

Harvey expands the following HTML comment markers before injecting the system
prompt, inserting live workspace data so the model always has an up-to-date
picture of the project:

| Marker | Replaced with |
|---|---|
| `<!-- @date -->` | Today's date (`YYYY-MM-DD`) |
| `<!-- @files -->` | Workspace file tree (hidden directories excluded) |
| `<!-- @git-status -->` | Output of `git status --short`, or `(not a git repository)` |

Example `HARVEY.md` with all three markers:

~~~markdown

You are a coding assistant for this Go project.
Today: <!-- @date -->

## Workspace files

<!-- @files -->

## Current git status

<!-- @git-status -->

## Conventions

- All exported symbols need /** ... */ doc comments.
- Use `t.TempDir()` in tests; no global state.

~~~

## Session walkthrough

~~~markdown
$ harvey

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Harvey  0.0.14
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Workspace: /home/user/myproject
✓ Knowledge base: agents/knowledge.db
✓ Loaded HARVEY.md as system prompt

  Starting qwen-coding (llamafile)...
  ✓ Ready at http://localhost:8080
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Connected: llamafile (qwen-coding)
  /help for commands · /exit to quit
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

harvey > /read internal/parser/parser.go
  ✓ internal/parser/parser.go (3241 bytes)
  1 file(s) added to context.

harvey > The ParseExpr function panics on empty input. Can you fix it?
⠹ The Jumblies have gone to sea in a sieve... [4s / ~11s]

Here is the corrected function:

```go internal/parser/parser.go
func ParseExpr(src string) (Expr, error) {
    if src == "" {
        return nil, fmt.Errorf("parseExpr: empty input")
    }
    // ... rest of function
}
```

  26 prompt + 58 reply tokens · 11.2s · 5.2 tok/s

  ┌─ Write: internal/parser/parser.go ─────────────────────────┐
  │  func ParseExpr(src string) (Expr, error) {
  │      if src == "" {
  │          return nil, fmt.Errorf("parseExpr: empty input")
  │      }
  │      // ... rest of function
  │  }
  └──────────────────────────────────────────────────────────────┘
  [y]es  [n]o  [A]ll  [q]uit > y
  ✓ wrote internal/parser/parser.go (847 bytes)

harvey > /run go test ./internal/parser/...
  $ go test ./internal/parser/...
  312 bytes of output added to context.

harvey > All tests pass now. /bye
Goodbye.
~~~

After each assistant response Harvey prints a stats line showing prompt tokens,
reply tokens, elapsed time, and generation speed. While the model is thinking,
an animated spinner with an estimated completion time keeps the terminal alive.

## Keyboard shortcuts

Harvey's prompt supports readline-style line editing.

### Navigation

| Key | Action |
|---|---|
| `←` / `→` | Move cursor one character |
| `Home` / `Ctrl+A` | Jump to beginning of line |
| `End` / `Ctrl+E` | Jump to end of line |
| `↑` / `↓` | Cycle through command history |

### Editing

| Key | Action |
|---|---|
| `Backspace` | Delete character before cursor |
| `Ctrl+D` | Delete character under cursor (or EOF on an empty line) |
| `Ctrl+K` | Delete from cursor to end of line |

### Actions

| Key | Action |
|---|---|
| `Ctrl+C` | Cancel current input and return to prompt |
| `Ctrl+X` then `Ctrl+E` | Open `$EDITOR` (falling back to `$VISUAL`, then `vi`) to compose a multi-line prompt; the file's content is submitted when the editor exits |

The `Ctrl+X Ctrl+E` shortcut is especially useful for longer prompts — you can
write, edit, and review the full prompt in your preferred editor before sending
it. The current line content is pre-loaded into the editor so you can also edit
a prompt you have already started typing.

## Command vocabulary

Harvey's resource management commands share a consistent set of verbs. Learn
them once and you can predict subcommands for any command family.

| Verb | Meaning | When to use |
|---|---|---|
| `list` | Show all registered items | Always available |
| `add` | Register an **existing** external resource | Registering a file path, URL, or endpoint |
| `new` | Create a **fresh** internal item | Creating a database, skill, or plan |
| `use [NAME]` | Activate an item; shows a picker when NAME is omitted | Switching the active model, store, or route |
| `show [NAME]` | Display item content or details | Inspecting what something contains |
| `edit [NAME]` | Open an item in `$EDITOR` | Modifying a profile or skill |
| `remove [NAME]` | Delete or unregister an item; picker when NAME is omitted | Cleaning up |
| `rename OLD NEW` | Rename an item | Renaming a workspace or session |
| `status` | Health/connection state of a *service* | Backend and store health checks |

The key distinction: **`add`** registers something you already have (a llamafile
binary, a route URL); **`new`** creates something Harvey manages from scratch
(a RAG database, a skill bundle, a plan). Both verbs are distinct from `use`,
which activates something already registered.

For backend services (`/llamafile`, `/ollama`), `status` checks whether the
server is reachable — it is not the same as `show`, which displays item
content.

**Examples across command families:**

```
/llamafile add ~/Models/Qwen.llamafile   — register an existing file
/llamafile use qwen-coding               — activate a registered model
/llamafile show qwen-coding              — show model details
/llamafile remove qwen-coding            — unregister

/rag new my-docs                         — create a new RAG database
/rag use my-docs                         — activate it
/rag remove my-docs                      — delete it

/session list                            — show all recorded sessions
/session show session.spmd               — show metadata for one session
/session use session.spmd                — load it into context

/route add pi2 ollama://192.0.2.12       — register a remote endpoint
/route use pi2                           — set as sticky default
/route remove pi2                        — unregister
```

## Slash commands

Type `/help` at any prompt for a live command list. All commands begin with `/`.

## Session commands

| Command | Description |
|---|---|
| `/help` | List all available slash commands |
| `/status` | Show backend, history length, workspace, KB state, and recording status |
| `/clear` | Reset conversation history (system prompt and pinned context are kept) |
| `/exit` `/quit` `/bye` | End the session |

## Backend commands

**Llamafile (primary)**

| Command | Description |
|---|---|
| `/llamafile add [PATH] [NAME]` | Register a llamafile and connect to it; picker shown when PATH is omitted |
| `/llamafile use [NAME]` | Switch to a registered llamafile; picker shown when NAME is omitted |
| `/llamafile show [NAME]` | Show path, size, and context length for a model |
| `/llamafile list` | List all registered models; active model marked with `→` |
| `/llamafile start [NAME]` | Start the active (or named) model's server |
| `/llamafile status` | Show active model, API URL, and reachability |
| `/llamafile remove NAME` | Unregister a model (binary not deleted) |
| `/llamafile download` | Print a table of recommended models with download URLs |

See [Llamafile Commands](harvey-llamafile.7.md) for full reference.

**Unified model management**

| Command | Description |
|---|---|
| `/model list` | List all models across llamafile and Ollama |
| `/model use NAME` | Switch to a named model regardless of backend |
| `/model show [NAME]` | Show the active (or named) model details |
| `/model status` | Check whether the active backend is reachable |
| `/model alias add ALIAS FULLNAME` | Define a short alias for a long model name |
| `/model alias list` | List all defined aliases |

**Ollama (alternative)**

| Command | Description |
|---|---|
| `/ollama start [debug]` | Launch `ollama serve` in the background |
| `/ollama stop` | Print a reminder to use your system's service manager |
| `/ollama status` | Check whether Ollama is reachable |
| `/ollama list` | List installed models; the current model is marked with `*` |
| `/ollama pull MODEL` | Download a model from the Ollama registry |
| `/ollama use MODEL` | Switch to a different installed model mid-session |
| `/ollama probe [MODEL]` | Test and cache capability flags for a model |
| `/ollama logs` | Tail the Ollama service log |
| `/ollama env` | Show Ollama environment variables as seen by Harvey |

See [Ollama Commands](harvey-ollama.7.md) for full reference.

## File operations

These commands move information between the workspace and the conversation.

### `/read FILE [FILE...]`

Reads one or more workspace files and injects their contents as a labelled
user context message. Supports multiple files in a single call.

```
harvey > /read cmd/harvey/main.go harvey/commands.go
  ✓ cmd/harvey/main.go (2104 bytes)
  ✓ harvey/commands.go (28341 bytes)
  2 file(s) added to context.
```

### `/write PATH`

Writes the last assistant reply to a workspace file. If the reply contains
a fenced code block, the first such block is extracted and written; otherwise
the full reply text is written.

```
harvey > /write harvey/spinner.go
  ✓ Wrote first code block to harvey/spinner.go (4201 bytes)
```

### `/run COMMAND [ARGS...]`

Runs a shell command in the workspace root, captures combined stdout and
stderr (up to 8 000 bytes), and injects the output as context. Exit codes
are noted in the context message.

```
harvey > /run go test ./...
  $ go test ./...
  847 bytes of output added to context.

harvey > /run make build
  $ make build
  1203 bytes of output added to context (exit 1).
```

> **Prompt injection note:** `/run` injects command output directly into the
> model's context. If that output comes from reading untrusted files, fetching
> URLs, or processing external content, it may contain adversarial text designed
> to redirect the model's behavior. Review output before accepting the model's
> follow-up suggestions.

## Security

Harvey includes a layered security system for controlling what it can do on
your system. All settings persist across sessions in `agents/harvey.yaml`.

### Safe mode

Restricts which programs may be run via `!` or `/run` to an explicit allowlist.

```
harvey > /safemode on
  Safe mode enabled. Only allowed commands can be executed.
  Allowed: ls, cat, grep, head, tail, wc, find, stat, jq, htmlq, bat, batcat

harvey > /safemode allow git
  Added "git" to allowlist.

harvey > /safemode status
  Safe mode: on
  Allowed commands (13): ls, cat, grep, head, tail, wc, find, stat, jq, ...

harvey > /safemode off
  Safe mode disabled. All commands are allowed.
```

> **When to keep safe mode on:** The default allowlist is a reasonable starting
> point for read-only workflows (browsing, reviewing, querying). Only run
> `/safemode off` when you understand what commands the model may attempt.
> The current safe mode state is always visible in the REPL prompt
> (`harvey >` = safe, `harvey [unsafe] >` in red = off).

Subcommands: `on`, `off`, `status`, `allow CMD`, `deny CMD`, `reset`.

### Workspace permissions

Fine-grained read/write/exec/delete control per path prefix, checked before
every `/read`, `/write`, and `/apply` operation.

```
harvey > /permissions set docs/ read
  Set permissions for "docs/": read

harvey > /permissions list
  Configured permissions:
    .:     read, write, exec, delete
    docs/: read
```

Subcommands: `list [PATH]`, `set PATH PERMS`, `reset`. Valid permission
tokens: `read`, `write`, `exec`, `delete`.

### Audit log

Every command execution, file read, file write, and skill invocation is
recorded to an in-memory ring buffer (last 1000 events).

```
harvey > /audit show 5
  Last 5 audit events:
  [14:02:11.432] command: go test ./... (allowed)
  [14:02:09.801] file_read: README.md (success)
  [14:02:05.120] command: rm -rf / (denied)

harvey > /audit status
  Audit buffer: 3/1000 events
```

Subcommands: `show [N]`, `clear`, `status`.

### Security overview

```
harvey > /security
```

Shows safe mode state, all configured path permissions, and audit buffer
capacity at a glance.

### API key filtering

Cloud provider API keys (`ANTHROPIC_API_KEY`, `COHERE_API_KEY`,
`DEEPSEEK_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GROQ_API_KEY`,
`MISTRAL_API_KEY`, `OPENAI_API_KEY`, `PERPLEXITY_API_KEY`) are stripped from
the environment of every child process started by `!` or `/run`. They are
never visible to commands Harvey runs on your behalf.

### Configurable timeouts

Shell commands have a configurable timeout (default 5 minutes). Model query
timeouts default to no limit, which is appropriate for slow hardware. Settings
live in `agents/harvey.yaml`:

```yaml
run_timeout: "5m"             # timeout for ! and /run commands
ollama_timeout: ""            # empty = no timeout (recommended on a Pi)

llamafile:
  startup_timeout: 120s       # how long to wait for llamafile server ready
```

## Code assistance

### `/search PATTERN [PATH]`

Searches workspace files for a regular expression. Results are capped at 100
matching lines. Binary files and hidden directories (`.git`, `.gitignore`, etc.)
are skipped automatically. An optional second argument scopes the search to
a subdirectory.

```
harvey > /search "func.*Handler"
  12 match(es) for "func.*Handler" added to context.

harvey > /search TODO internal/
  3 match(es) for "TODO" added to context.
```

### `/git SUBCOMMAND [ARGS...]`

Runs a read-only git command (`status`, `diff`, `log`, `show`, `blame`) in
the workspace root and injects the output into context. Additional arguments
are passed through.

```
harvey > /git status
harvey > /git diff HEAD~1
harvey > /git log --oneline -10
harvey > /git blame internal/parser/parser.go
```

### Auto-apply tagged code blocks

When the model produces a fenced code block tagged with a file path, Harvey
automatically previews the content and prompts you to write it — no explicit
command needed. Tag the fence line with the target path:

~~~markdown
```go harvey/spinner.go
func (s *Spinner) run() {
    ...
}
```
~~~

Harvey will show a box preview and ask for confirmation:

```
  ┌─ Write: harvey/spinner.go ──────────────────────────────────┐
  │  func (s *Spinner) run() {
  │      ...
  │  }
  └──────────────────────────────────────────────────────────────┘
  [y]es  [n]o  [A]ll  [q]uit > y
  ✓ wrote harvey/spinner.go (4312 bytes)
```

Press `Enter` or `y` to write the file, `n` to skip, `A` to write all
remaining files without further prompts, or `q` to cancel.

## Session quality

### `/summarize`

Asks the connected LLM to condense the current conversation into a single
paragraph, then replaces the full history with that summary. Use this when
a long session is approaching the model's context window.

```
harvey > /summarize
  History condensed to 312 chars.
```

The summary is stored as a `[Conversation summary]` user message. The system
prompt and any pinned context are preserved.

### `/context add|show|clear`

Manages *pinned context* — text that is automatically re-injected into the
conversation history after every `/clear` or `/summarize`, keeping persistent
facts in front of the model throughout the session.

```
harvey > /context add Target platform: macOS arm64. Prefer stdlib over third-party.
  Pinned context updated (57 chars).

harvey > /context add The project uses semantic versioning via codemeta.json.
  Pinned context updated (106 chars).

harvey > /context show
  Pinned context (106 chars):

  Target platform: macOS arm64. Prefer stdlib over third-party.
  The project uses semantic versioning via codemeta.json.

harvey > /context clear
  Pinned context cleared.
```

Multiple `/context add` calls append to the existing pinned context, separated
by newlines. To replace it entirely, `/context clear` then `/context add`.

## Knowledge base commands

Harvey maintains a SQLite knowledge base at `agents/knowledge.db` in the
workspace. It is independent of conversation history and persists across
sessions.

| Command | Description |
|---|---|
| `/kb status` | Show all projects with recent observations |
| `/kb project list` | List projects with ID and status |
| `/kb project add NAME [DESC]` | Create a project and set it as current |
| `/kb project use ID` | Set the active project by ID |
| `/kb observe [KIND] TEXT` | Record an observation against the active project |
| `/kb concept list` | List all concepts |
| `/kb concept add NAME [DESC]` | Add a named concept |

Observation kinds: `note`, `finding`, `decision`, `question`, `hypothesis`.

```
harvey > /kb project add harvey "Terminal coding agent"
Project "harvey" added (id=1) and set as current.

harvey > /kb observe finding WAL mode doubled write throughput in the knowledge base
Observation recorded (id=1, kind=finding).

harvey > /kb observe decision Use bufio.Scanner for all interactive prompt reading
Observation recorded (id=2, kind=decision).

harvey > /kb status
  [1] harvey  (active)
      Terminal coding agent
      [finding] WAL mode doubled write throughput in the knowledge base
      [decision] Use bufio.Scanner for all interactive prompt reading
```

## Recording commands

Harvey can record sessions to [Fountain](https://fountain.io) screenplay files.
Fountain recordings capture every exchange in a structured, human-readable
format that Harvey can replay or resume later.

### Starting a recording

| Command | Description |
|---|---|
| `/record start [FILE]` | Begin recording; FILE defaults to a timestamped path in the workspace |
| `/record stop` | Close the recording file |
| `/record status` | Show the current recording path or "not recording" |

```
harvey > /record start
Recording started: /home/user/myproject/agents/harvey-session-20260415-142300.spmd

harvey > /record stop
Recording stopped. Session saved to harvey-session-20260415-142300.spmd
```

You can also start recording automatically at launch:

```bash
harvey --record                          # auto-named timestamped file
harvey --record-file mysession.spmd  # explicit path
```

### Resuming or replaying a Fountain file

There are two ways to use a recorded `.spmd` file in a later session:

**Continue** — loads the Fountain file's conversation history into context and
drops you into the interactive REPL, so you can pick up exactly where you left
off:

```bash
harvey --continue mysession.spmd
```

Or from inside a running Harvey session:

```
harvey > /session continue mysession.spmd
```

**Replay** — re-sends every user turn from the Fountain file to the currently
connected model and records the fresh responses to a new file. Useful for
re-running a session against a different model:

```bash
harvey --replay mysession.spmd
harvey --replay mysession.spmd --replay-output newresponses.spmd
```

Or from inside a running Harvey session:

```
harvey > /session replay mysession.spmd
harvey > /session replay mysession.spmd newresponses.spmd
```

| Startup flag | Description |
|---|---|
| `--continue FILE` | Load history from FILE and open the REPL |
| `--replay FILE` | Re-run all turns from FILE against the current model |
| `--replay-output FILE` | Write replay responses to FILE (default: auto-named) |

## File browser

```
harvey > /files
harvey > /files internal/parser
```

Lists files in the workspace root or a subdirectory. Useful for discovering
paths before using `/read` or `/write`.

## Typical workflows

### Review and patch a failing test

```
harvey > /run go test ./...
harvey > /read internal/parser/parser_test.go internal/parser/parser.go
harvey > The TestParseExpr test is failing on empty input. Please fix parser.go.
  (Harvey shows the fix; prompts to write the tagged code block — press Enter to accept)
harvey > /run go test ./internal/parser/...
```

### Explore an unfamiliar codebase

```
harvey > /files
harvey > /search "type.*interface"
harvey > /read harvey/harvey.go
harvey > Can you summarize the LLMClient interface and explain how the two backends differ?
```

### Draft a commit message

```
harvey > /git diff HEAD
harvey > /git status
harvey > Please write a conventional commit message for these changes.
harvey > /write .git/COMMIT_EDITMSG
```

### Keep context focused on a long refactor

```
harvey > /context add We are migrating from the old auth middleware to the new JWT-based one.
harvey > /context add All session tokens must be stored as httpOnly cookies, not localStorage.
harvey > /read internal/auth/middleware.go
harvey > ...several turns of back and forth...
harvey > /summarize
harvey > /read internal/auth/jwt.go
harvey > Continue the migration — here is the JWT helper...
```

### Record architectural decisions

```
harvey > /kb project add myapp "Main web application"
harvey > ...discussion about database choice...
harvey > /kb observe decision Use SQLite with WAL mode; Postgres is overkill for this deployment
harvey > /kb observe question How do we handle schema migrations across versions?
harvey > /kb status
```
