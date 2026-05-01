
# Starting Harvey

## What is Harvey?

harvey is a terminal agent for local large language models.

See [man page](harvey.1.md)

How it starts:

harvey looks for HARVEY.md in the current directory and uses it as a
system prompt. It then connects to a local Ollama server or publicai.co
and starts an interactive chat session.

All file I/O is constrained to the workspace directory (--workdir or ".").
A knowledge base is stored at `<workdir>/harvey/knowledge.db` and is created
automatically on first run. Session recordings are stored in
`<workdir>/harvey/sessions/`. Both paths can be overridden in `harvey/harvey.yaml`.

> Type /help inside the session for available slash commands.

## Typical invocations

```sh
# Start in the current directory, auto-select Ollama model
harvey

# Start with a specific model
harvey -m codellama:13b

# Point at a workspace that is not the current directory
harvey -w ~/projects/myapp

# Use a non-default Ollama endpoint
harvey --ollama http://192.168.1.10:11434

# Use publicai.co (requires PUBLICAI_API_KEY to be set)
export PUBLICAI_API_KEY=sk-...
harvey
```

## Startup sequence

When Harvey starts it:

1. Prints the banner and resolves the workspace root.
2. Opens (or creates) `harvey/knowledge.db` in the workspace.
3. Loads `harvey/harvey.yaml` if present (overrides paths for KB, sessions, agents).
4. Scans `harvey/sessions/` for prior `.spmd` / `.fountain` session files and
   offers to resume one (default: No). If a session is chosen, the model it
   used is pre-selected in the next step.
5. Reads `HARVEY.md` from the current directory, expands any
   [dynamic markers](#dynamic-markers), and injects it as the system prompt.
6. Probes Ollama; if reachable, selects the model from the resumed session or
   lets you choose from the installed list.
7. If Ollama is unreachable, offers to start `ollama serve` then retries.
8. If Ollama is still unavailable, offers to connect to publicai.co instead.
9. Begins recording the session to a new `.spmd` file in `harvey/sessions/`.
10. Drops you into the REPL prompt: `harvey > `

---

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

---

## Session walkthrough

~~~markdown
$ harvey -m llama3:latest

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Harvey  0.0.0
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Workspace: /home/user/myproject
✓ Knowledge base: harvey/knowledge.db
✓ Loaded HARVEY.md as system prompt

  Checking Ollama at http://localhost:11434...
  ✓ Ollama is running
  Using model: llama3:latest
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Connected: Ollama (llama3:latest)
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

harvey > /apply
  Found 1 tagged block(s):
    internal/parser/parser.go (1847 bytes)
  Apply all? [Y/n] y
  ✓ internal/parser/parser.go

harvey > /run go test ./internal/parser/...
  $ go test ./internal/parser/...
  312 bytes of output added to context.

harvey > All tests pass now. /bye
Goodbye.
~~~

After each assistant response Harvey prints a stats line showing prompt tokens,
reply tokens, elapsed time, and generation speed. While the model is thinking,
an animated spinner with an estimated completion time keeps the terminal alive.

---

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

---

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

**Ollama**

| Command | Description |
|---|---|
| `/ollama start` | Launch `ollama serve` in the background |
| `/ollama stop` | Print a reminder to use your system's service manager |
| `/ollama status` | Check whether Ollama is reachable |
| `/ollama list` | List installed models; the current model is marked with `*` |
| `/ollama ps` | Show which models are currently loaded in memory |
| `/ollama run MODEL [PROMPT]` | Start an interactive Ollama session (passes through the terminal) |
| `/ollama pull MODEL` | Download a model from the Ollama registry |
| `/ollama push MODEL` | Upload a model to the Ollama registry |
| `/ollama show MODEL` | Display a model's Modelfile and parameters |
| `/ollama create NAME [-f MODELFILE]` | Create a new model from a Modelfile |
| `/ollama cp SOURCE DEST` | Copy an installed model to a new name |
| `/ollama rm MODEL [MODEL...]` | Remove one or more installed models |
| `/ollama use MODEL` | Switch to a different installed model mid-session |
| `/ollama logs` | Tail the Ollama service log |
| `/ollama env` | Show Ollama environment variables as seen by Harvey |

**publicai.co**

| Command | Description |
|---|---|
| `/publicai connect` | Connect using `PUBLICAI_API_KEY` |
| `/publicai disconnect` | Drop the publicai.co connection |
| `/publicai status` | Show whether publicai.co is connected |

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

## Code assistance

### `/search PATTERN [PATH]`

Searches workspace files for a regular expression. Results are capped at 100
matching lines. Binary files and hidden directories (`.git`, `.harvey`, etc.)
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

### `/apply`

Scans the last assistant reply for fenced code blocks whose opening fence
line includes a file path (e.g. `` ```go harvey/spinner.go ``), lists the
files found, asks for a single Y/n confirmation, then writes all of them.

```
harvey > /apply
  Found 2 tagged block(s):
    harvey/spinner.go (4312 bytes)
    harvey/terminal.go (7891 bytes)
  Apply all? [Y/n] y
  ✓ harvey/spinner.go
  ✓ harvey/terminal.go
```

For Harvey to auto-detect a file, tag the fence line with the target path:

~~~markdown
```go harvey/spinner.go
func (s *Spinner) run() {
    ...
}
```
~~~

## Session quality (Tier 3)

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

Harvey maintains a SQLite knowledge base at `harvey/knowledge.db` in the
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
Recording started: /home/user/myproject/harvey-session-20260415-142300.fountain

harvey > /record stop
Recording stopped. Session saved to harvey-session-20260415-142300.fountain
```

You can also start recording automatically at launch:

```bash
harvey --record                          # auto-named timestamped file
harvey --record-file mysession.fountain  # explicit path
```

### Resuming or replaying a Fountain file

There are two ways to use a recorded `.fountain` file in a later session:

**Continue** — loads the Fountain file's conversation history into context and
drops you into the interactive REPL, so you can pick up exactly where you left
off:

```bash
harvey --continue mysession.fountain
```

Or from inside a running Harvey session:

```
harvey > /session continue mysession.fountain
```

**Replay** — re-sends every user turn from the Fountain file to the currently
connected model and records the fresh responses to a new file. Useful for
re-running a session against a different model:

```bash
harvey --replay mysession.fountain
harvey --replay mysession.fountain --replay-output newresponses.fountain
```

Or from inside a running Harvey session:

```
harvey > /session replay mysession.fountain
harvey > /session replay mysession.fountain newresponses.fountain
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

---

## Typical workflows

### Review and patch a failing test

```
harvey > /run go test ./...
harvey > /read internal/parser/parser_test.go internal/parser/parser.go
harvey > The TestParseExpr test is failing on empty input. Please fix parser.go.
harvey > /apply
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

