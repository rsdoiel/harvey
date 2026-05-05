# Harvey Sessions: Recording and Fountain Format

*Version 1.0 — Complete guide to session recording in Harvey*

## Overview

Harvey **records every conversation** to a **Fountain screenplay file** (`.spmd` extension), creating a persistent, human-readable, and machine-parseable record of all interactions. This enables:

- **Session resumption** — Continue a previous conversation with full context
- **Session replay** — Re-send prompts to a different model and capture new responses
- **Audit trail** — Complete history of all prompts, responses, and file operations
- **Cross-agent compatibility** — Fountain format is shared with other LLM systems
- **Multi-model tracking** — Explicit character attribution for HARVEY, USER, and MODEL

### File Extensions

| Extension | Description | Usage |
|-----------|-------------|-------|
| `.spmd` | **Screenplay Markdown** — Harvey's primary session format | Written by Harvey, read by all |
| `.fountain` | Standard Fountain format | Read by Harvey (for compatibility) |

**Note:** Harvey writes `.spmd` files but can read both `.spmd` and `.fountain` files.

## Quick Start

### Start a Recorded Session

```bash
# Automatic recording to default location
harvey
# Creates: harvey/sessions/harvey-session-YYYYMMDD-HHMMSS.spmd

# Explicit recording path
harvey --record-file mysession.spmd

# In REPL, check recording status
harvey> /record
  Recording: ON
  File: /home/user/project/harvey/sessions/harvey-session-20260504-142300.spmd
```

### Continue a Previous Session

```bash
# From command line
harvey --continue harvey/sessions/harvey-session-20260504.spmd

# From REPL
harvey> /session continue harvey/sessions/harvey-session-20260504.spmd
  Loaded 15 turns from harvey-session-20260504.spmd
  Model: llama3.1:8b (from session recording)
```

### Replay a Session with a Different Model

```bash
# From command line (replay to same file)
harvey --replay oldsession.spmd

# From command line (replay to new file)
harvey --replay oldsession.spmd --replay-output newresponses.spmd

# From REPL
harvey> /session replay oldsession.spmd newresponses.spmd
```

### List Available Sessions

```bash
# Sessions are stored in harvey/sessions/ by default
harvey> /sessions
  Available sessions (newest first):
    [0]  harvey-session-20260504-142300.spmd  (2026-05-04 14:23:00)
    [1]  harvey-session-20260503-101500.spmd  (2026-05-03 10:15:00)
    [2]  debugging-issue-42.spmd               (2026-05-02 09:30:00)
```

## Session File Structure

### Fountain Screenplay Format

Harvey sessions follow the **[Fountain screenplay format](https://fountain.io)**, a plain-text markup for screenplays that is both human-readable and machine-parseable.

### File Layout

```
Title: Harvey Session
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 14:23:00
Draft date: 2026-05-04

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-05-04 14:23:05

Harvey and RSDOIEL are in chat mode. Model: LLAMA3.1. Workspace: /home/user/project.

RSDOIEL
What is the capital of France?

HARVEY
Forwarding to LLAMA3.1.

LLAMA3.1
The capital of France is Paris.

[[stats: LLAMA3.1 · 10 tokens · 0.5s · 20.0 tok/s]]

INT. AGENT MODE 2026-05-04 14:25:00

HARVEY
Harvey proposes to write 1 file(s) to the workspace.

HARVEY
Write notes.md?

RSDOIEL
yes

[[write: notes.md — ok]]

INT. SHELL 2026-05-04 14:30:00

RSDOIEL
! ls -la

SHELL
notes.md
todo.txt

[[shell: ls -la — exit 0]]

THE END.
```

### Character Model

Harvey uses a **multi-character model** in Fountain files to distinguish between different participants:

| Character | Representation | Description |
|-----------|----------------|-------------|
| `USER` | All caps (e.g., `RSDOIEL`) | The human operator; name from `$USER` environment variable |
| `HARVEY` | Always `HARVEY` | The Harvey agent program |
| `MODEL` | All caps (e.g., `LLAMA3.1`, `GEMMA4`) | The LLM backend model |
| `SHELL` | Always `SHELL` | Shell command execution output |
| `SKILL-NAME` | Uppercase skill name (e.g., `FOUNTAIN-ANALYSIS`) | Skill execution dialogue |
| `ROUTE_NAME` | Uppercase route name (e.g., `PI2`, `CLAUDE`) | Routed endpoint responses |

**Character Attribution Rules:**
- **HARVEY** = local Ollama/Llamafile interactions
- **ROUTE_NAME** = responses from routed endpoints (when `@mention` routing is used)
- **MODEL_NAME** = direct model responses (cloud/direct, or when `@modelname` is used)
- **INT.** scenes = Harvey is involved as an intermediary
- **EXT.** scenes = direct model-human interaction (not used by Harvey currently)

## Scene Types

Harvey sessions contain different **scene types** that represent distinct interaction modes:

### 1. Chat Scenes (`INT. HARVEY AND {USER} TALKING`)

Standard question-and-answer exchanges between user and model.

**Structure:**
```
INT. HARVEY AND {USER} TALKING {TIMESTAMP}

Harvey and {USER} are in chat mode. Model: {MODEL}. Workspace: {PATH}.

{USER}
{user input text}

HARVEY
Forwarding to {MODEL}.

{MODEL}
{LLM response text}

{ROUTE_NAME}
{routed response text}

[[stats: ...]]
[[route: ...]]
```

**Example:**
```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 14:23:05

Harvey and RSDOIEL are in chat mode. Model: LLAMA3.1. Workspace: /home/user/project.

RSDOIEL
@claude refactor this function

HARVEY
Forwarding to CLAUDE.

CLAUDE
Here is the refactored version of your function...

[[stats: CLAUDE · 250 tokens · 2.5s · 100.0 tok/s]]
```

### 2. Agent Mode Scenes (`INT. AGENT MODE`)

File operations and tool usage (write, run, edit, etc.).

**Structure:**
```
INT. AGENT MODE {TIMESTAMP}

HARVEY
Harvey proposes to write {N} file(s) to the workspace.

HARVEY
Write {filename}?

{USER}
{yes/no/all/quit}

[[write: {filename} — {status}]]
```

**Example:**
```
INT. AGENT MODE 2026-05-04 14:25:00

HARVEY
Harvey proposes to write 2 file(s) to the workspace.

HARVEY
Write src/main.go?

RSDOIEL
yes

[[write: src/main.go — ok]]

HARVEY
Write src/utils.go?

RSDOIEL
all

[[write: src/utils.go — ok]]
```

### 3. Shell Scenes (`INT. SHELL`)

Shell command execution.

**Structure:**
```
INT. SHELL {TIMESTAMP}

{USER}
! {command}

SHELL
{output}

[[shell: {command} — exit {code}]]
```

**Example:**
```
INT. SHELL 2026-05-04 14:30:00

RSDOIEL
! git status

SHELL
On branch main
Your branch is up to date with 'origin/main'.

[[shell: git status — exit 0]]
```

### 4. Skill Scenes (`INT. SKILL {NAME}`)

Skill execution and output.

**Structure:**
```
INT. SKILL {SKILL-NAME} {TIMESTAMP}

Harvey executes the {skill-name} skill.

{description}

{SKILL-NAME}
{skill body/instructions}
```

**Example:**
```
INT. SKILL FOUNTAIN-ANALYSIS 2026-05-04 14:35:00

Harvey executes the fountain-analysis skill.

Read and actively monitor a Harvey Fountain screenplay file, parsing its structure...

FOUNTAIN-ANALYSIS
## Analysis Steps
1. Parse the Fountain file...
2. Extract scene metadata...
```

## @mention Routing in Scenes

When **@mention routing** is used, Harvey records the routing decision and attributes responses to the routed model:

### Routing Flow in Fountain

```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 14:40:00

Harvey and RSDOIEL are in chat mode. Model: LLAMA3.1. Workspace: /home/user/project.

RSDOIEL
@claude analyze this code

HARVEY
Forwarding to CLAUDE.

CLAUDE
The code has the following structure...

[[stats: CLAUDE · 450 tokens · 3.2s · 140.6 tok/s]]
```

### Multi-Model Context Tracking

When a user mentions multiple models or routing is involved:

```
RSDOIEL
@mistral @claude compare your approaches

HARVEY
Forwarding to MISTRAL.

MISTRAL
My approach uses...

HARVEY
Forwarding to CLAUDE.

CLAUDE
My approach differs by...
```

**Context Window:** The last 10 non-system messages are sent to the remote model for context.

## Fountain Syntax Reference

### Element Types Used by Harvey

| Fountain Element | Syntax | Harvey Usage |
|-----------------|--------|--------------|
| Title Page | `Title: value` | Session metadata (Title, Credit, Author, Date) |
| Transition | `FADE IN:`, `THE END.` | Document start/end markers |
| Scene Heading | `INT. ...` or `EXT. ...` | Scene type and timestamp |
| Action | Plain text paragraphs | State description, routing info, stats |
| Character | `CHARACTER` (all caps) | Speaker identification (USER, HARVEY, MODEL) |
| Parenthetical | `(parenthetical)` | Not currently used by Harvey |
| Dialogue | Text under character | Spoken content (prompts, responses) |
| Note | `[[note content]]` | Metadata: stats, file operations, shell results |
| Centered | `> centered text <` | Not currently used by Harvey |

### Harvey-Specific Note Formats

Harvey uses Fountain **notes** (`[[...]]`) for machine-readable metadata:

| Note Type | Format | Example |
|-----------|--------|---------|
| Stats | `[[stats: {model} · {tokens} tokens · {time}s · {tok/s} tok/s]]` | `[[stats: LLAMA3.1 · 50 tokens · 0.8s · 62.5 tok/s]]` |
| Write | `[[write: {path} — {status}]]` | `[[write: src/main.go — ok]]` |
| Shell | `[[shell: {command} — exit {code}]]` | `[[shell: ls -la — exit 0]]` |
| Read | `[[read: {path} — {status}]]` | `[[read: data.json — ok]]` |
| Edit | `[[edit: {path} — {status}]]` | `[[edit: README.md — ok]]` |
| Run | `[[run: {command} — {status}]]` | `[[run: go test — ok]]` |

## Session File Location

### Default Location

Sessions are stored in:

```
<workspace>/harvey/sessions/
```

### Custom Location

Override in `harvey.yaml`:

```yaml
sessions_dir: custom/sessions/path
```

Or via command line:

```bash
harvey --sessions-dir custom/sessions
```

### Global Sessions

Global sessions (not workspace-specific) are stored in:

```
~/harvey/sessions/
```

## Session Commands

### `/record` — Manage Recording

| Command | Description |
|---------|-------------|
| `/record` | Show current recording status |
| `/record on` | Start recording (default at startup) |
| `/record off` | Stop recording current session |
| `/record file <path>` | Change recording file path |

**Examples:**
```
harvey> /record
  Recording: ON
  File: /home/user/project/harvey/sessions/harvey-session-20260504-142300.spmd

harvey> /record off
  Recording stopped.

harvey> /record on
  Recording resumed to /home/user/project/harvey/sessions/harvey-session-20260504-142300.spmd
```

### `/session` — Session Operations

| Command | Description |
|---------|-------------|
| `/session continue <file>` | Load chat history from file and continue |
| `/session replay <file> [output]` | Replay session to current model, optionally saving to new file |
| `/sessions` | List available session files |

**Examples:**
```
# Continue a previous session
harvey> /session continue harvey/sessions/old.spmd
  Loaded 23 turns from old.spmd
  Resuming conversation...

# Replay to a different model
harvey> /session replay old.spmd new-responses.spmd
  Replaying 23 turns from old.spmd
  Recording to new-responses.spmd
  [1/23] What is the capital of France?
  ...

# List sessions
harvey> /sessions
  [0]  harvey-session-20260504-142300.spmd
  [1]  harvey-session-20260503-101500.spmd
```

### Command-Line Session Options

| Flag | Description |
|------|-------------|
| `--record` | Enable session recording (default: on) |
| `--record-file <path>` | Record to specific file |
| `--no-record` | Disable session recording |
| `--continue <file>` | Continue from a session file |
| `--replay <file>` | Replay a session file |
| `--replay-output <file>` | Output file for replay (use with `--replay`) |

**Examples:**
```bash
# Start with recording disabled
harvey --no-record

# Record to specific file
harvey --record-file my-session.spmd

# Continue from previous session
harvey --continue ~/harvey/sessions/previous.spmd

# Replay a session with new model
harvey --model claude-3-haiku --replay old-session.spmd --replay-output new-session.spmd
```

## Session File Discovery

Harvey automatically scans for session files on startup:

1. **Workspace sessions:** `harvey/sessions/` (newest first)
2. **Global sessions:** `~/harvey/sessions/` (if workspace has no sessions)

### `ListSessionFiles()` Function

```go
files, err := ListSessionFiles("harvey/sessions")
for _, f := range files {
    fmt.Printf("%s  %s\n", f.ModTime.Format("2006-01-02 15:04"), f.Name)
}
```

Returns session files sorted by modification time (newest first).

## Parsing Session Files

Harvey provides functions to parse and extract data from session files:

### `parseFountainSession()`

Extracts chat turns from a Harvey Fountain file:

```go
userName, modelName, turns, err := parseFountainSession("session.spmd")
for _, turn := range turns {
    fmt.Printf("USER: %s\n", turn.UserInput)
    fmt.Printf("MODEL: %s\n", turn.ModelReply)
}
```

**Returns:**
- `userName` — ALL-CAPS user name from scene headings
- `modelName` — ALL-CAPS model name from HARVEY's "Forwarding to" dialogue
- `turns` — Slice of `PlaybackTurn` structs with `UserInput` and `ModelReply`

### `ExtractModelFromSession()`

Extracts just the model name from a session file:

```go
model, err := ExtractModelFromSession("session.spmd")
// model == "LLAMA3.1"
```

## Recording Implementation

### `Recorder` Type

The `Recorder` struct handles writing session events to Fountain files:

```go
type Recorder struct {
    f         *os.File      // File handle
    path      string        // File path
    userName  string        // ALL-CAPS user name
    modelName string        // ALL-CAPS model name
    workspace string        // Workspace path
}
```

### Creating a Recorder

```go
r, err := NewRecorder("session.spmd", "Ollama (llama3.1:8b)", "/home/user/project")
defer r.Close()
```

This creates the file, writes the title page, and adds `FADE IN:`.

### Recording Chat Turns

```go
// Simple turn (no stats)
err := r.RecordTurn("Hello", "Hi there!")

// Full turn with stats and routing
stats := ChatStats{
    Model:    "llama3.1:8b",
    Tokens:   50,
    Time:     0.8,
    TokensPerSec: 62.5,
}
err := r.RecordTurnWithStats(
    "Hello",
    "Hi there!",
    stats,
    []string{"Ollama (llama3.1:8b)"},
    "",
)
```

### Recording Agent Actions

```go
// Start agent mode scene
err := r.StartAgentScene("Harvey proposes to write 1 file.")

// Record write action
err := r.RecordAgentAction("write", "hello.txt", "yes", "ok")
```

### Recording Shell Commands

```go
err := r.RecordShellCommand("ls -la", "file1.txt\nfile2.txt\n", 0)
```

### Recording Skill Execution

```go
err := r.RecordSkillLoad(
    "fountain-analysis",
    "Read and analyze a Fountain session file",
    skillBody,
)
```

### Closing the Recorder

```go
err := r.Close() // Writes "THE END."
```

## Session Replay

### How Replay Works

1. Parse the source session file to extract chat turns
2. Create a new Recorder for the output file
3. For each turn:
   - Send the user input to the current LLM backend
   - Record the new response to the output file
   - Apply any tagged code blocks (with backup protection)
4. Close the output file with "THE END."

### Replay with Backup Protection

When replaying sessions that contain file writes:

1. If the target file exists, it's renamed to `{file}.bak.{TIMESTAMP}`
2. The new content is written
3. If backup or write fails, the action is recorded as "skipped"

**Example:**
```
[[write: src/main.go — ok]]
# If src/main.go existed, it becomes src/main.go.bak.20260504-142300

[[write: src/main.go — skipped: cannot backup]]
# If backup failed, write is skipped
```

## Session Metadata

### Title Page Fields

Every Harvey session file begins with a title page containing metadata:

```
Title: Harvey Session
Credit: Recorded by Harvey
Author: {USER}
Date: {YYYY-MM-DD HH:MM:SS}
Draft date: {YYYY-MM-DD}
```

| Field | Source | Format |
|-------|--------|--------|
| `Title` | Fixed | `"Harvey Session"` |
| `Credit` | Fixed | `"Recorded by Harvey"` |
| `Author` | Environment | `$USER` (uppercase) or `"OPERATOR"` |
| `Date` | Timestamp | `2006-01-02 15:04:05` |
| `Draft date` | Timestamp | `2006-01-02` |

### Characters Header (Optional)

For multi-model sessions, a `Characters:` line can be added to the title page:

```
Title: Harvey Session
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 14:23:00
Draft date: 2026-05-04
Characters: RSDOIEL, HARVEY, LLAMA3.1, CLAUDE
```

## Fountain Format Compatibility

### Fountain Specification

Harvey uses a subset of the **[Fountain screenplay format](https://fountain.io/syntax)**:

- **Title Page:** Key-value pairs for metadata
- **Scene Headings:** `INT.` or `EXT.` followed by description and timestamp
- **Action:** Plain text paragraphs for stage directions
- **Character:** ALL-CAPS names for speakers
- **Dialogue:** Text following a character line
- **Notes:** `[[content]]` for production notes
- **Transitions:** `FADE IN:`, `THE END.` for scene markers

### Compatibility with Other Tools

Harvey session files (`.spmd`) are valid Fountain files and can be:

- **Read by other Fountain-compatible tools** — Any Fountain parser can read them
- **Imported into screenplay software** — Final Draft, Highland, etc.
- **Processed by Harvey's skills** — The `fountain-analysis` skill can analyze any Fountain file

### Differences from Standard Fountain

Harvey makes these Fountain-specific adaptations:

1. **Character names:** Always ALL-CAPS (standard Fountain allows mixed case)
2. **Timestamp in scene headings:** Harvey includes timestamps in all scene headings
3. **Machine-readable notes:** Uses `[[key: value]]` format for structured metadata
4. **State in action blocks:** Includes `Model:`, `Workspace:` in action descriptions

## Best Practices

### Naming Sessions

1. **Use descriptive names:** `debugging-issue-42.spmd` instead of `session-1.spmd`
2. **Include dates:** `20260504-research-llm-security.spmd`
3. **Use hyphens:** `my-project-analysis.spmd` (avoid spaces)
4. **Keep it short:** Under 80 characters for readability

### Organizing Sessions

1. **Use subdirectories:** `harvey/sessions/project-x/`, `harvey/sessions/experiments/`
2. **Archive old sessions:** Move completed sessions to `harvey/sessions/archive/`
3. **Tag in filename:** `20260504-llama3.1-research.spmd` to track model used
4. **Group related sessions:** `20260504-part1.spmd`, `20260504-part2.spmd`

### Session Management

1. **Resume instead of replay:** Use `continue` to pick up where you left off
2. **Replay for model comparison:** Use `replay` to compare responses from different models
3. **Delete with care:** Session files are the only record of your conversations
4. **Backup important sessions:** Copy `.spmd` files to a safe location

### Multi-Model Workflows

1. **Use @mention explicitly:** `@claude`, `@mistral`, etc. for clear attribution
2. **Check routing config:** Ensure endpoints are registered in `<workspace>/agents/routes.json`
3. **Verify context window:** Remember only last 10 messages are sent to remote models
4. **Review session files:** Confirm correct model attribution in the Fountain file

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| Session not recording | `--no-record` flag or config | Check `/record` status, enable with `/record on` |
| Cannot find session file | Wrong path or directory | Use `/sessions` to list available files, check path |
| Replay produces errors | Model unavailable or rate limited | Check model is running/available, try different model |
| File writes skipped on replay | Backup failed or permission denied | Check file permissions, free disk space |
| Characters not recognized | Non-standard character names | Use ALL-CAPS character names consistently |

### Session File Corruption

If a session file becomes corrupted:

1. **Try to continue anyway:** Harvey may still extract valid turns
2. **Check for backup:** Harvey doesn't auto-backup, but you may have manual backups
3. **Reconstruct manually:** Create a new session file with the important exchanges
4. **Validate with Fountain parser:**
   ```bash
   # If fountain CLI is available
   fountain validate session.spmd
   ```

### Missing Title Page

If a session file is missing the title page:
- Harvey will still parse it but may have incomplete metadata
- Author will default to `OPERATOR`
- Date will be missing from the file

### Model Name Extraction Failure

If the model name cannot be extracted:
- Defaults to `MODEL`
- Check that HARVEY has a "Forwarding to {MODEL}." dialogue line
- Ensure the session was recorded with Harvey (not manually created)

## Advanced Usage

### Programmatic Session Access

```go
// Open a session file
user, model, turns, err := parseFountainSession("session.spmd")

// Process turns
for i, turn := range turns {
    fmt.Printf("Turn %d:\n", i+1)
    fmt.Printf("  User: %s\n", turn.UserInput)
    fmt.Printf("  Model: %s\n", turn.ModelReply)
}

// Create a custom session file
r, _ := NewRecorder("custom.spmd", "Custom Model", "/path/to/workspace")
r.StartAgentScene("Custom scene")
r.RecordAgentAction("custom", "target", "yes", "ok")
r.Close()
```

### Custom Session Parsing

For advanced analysis, use the Fountain library directly:

```go
import "github.com/rsdoiel/fountain"

doc, err := fountain.ParseFile("session.spmd")
for _, elem := range doc.Elements {
    switch elem.Type {
    case fountain.SceneHeadingType:
        fmt.Printf("Scene: %s\n", elem.Content)
    case fountain.CharacterType:
        fmt.Printf("Character: %s\n", fountain.CharacterName(elem))
    case fountain.DialogueType:
        fmt.Printf("Dialogue: %s\n", elem.Content)
    }
}
```

### Session File Transformation

Convert sessions to other formats:

```go
// Convert to plain text
doc, _ := fountain.ParseFile("session.spmd")
for _, elem := range doc.Elements {
    fmt.Println(fountainSrc(&elem))
}

// Convert to JSON
// (Would need custom marshaling of fountain.Element)
```

## Fountain Format Specification

For complete Fountain syntax, see:
- **[Fountain.io](https://fountain.io)** — Official specification
- **[Fountain GitHub](https://github.com/mattgemmell/Fountain)** — Reference implementation

### Supported Fountain Features

| Feature | Supported | Notes |
|---------|-----------|-------|
| Title Page | ✓ | With Harvey-specific fields |
| Scene Headings | ✓ | INT./EXT. with timestamps |
| Action | ✓ | State descriptions, routing info |
| Character | ✓ | ALL-CAPS only |
| Dialogue | ✓ | Full support |
| Parenthetical | ✓ | Not currently used |
| Notes | ✓ | Structured metadata format |
| Transitions | ✓ | FADE IN:, THE END. |
| Centered | ✗ | Not used |
| Lyrics | ✗ | Not used |
| Page Breaks | ✗ | Not used |
| Section Headings | ✗ | Not used |

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) — Configuration file reference
- [ROUTING.md](ROUTING.md) — Remote endpoint routing guide
- [KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md) — Knowledge base documentation
- [SKILLS.md](SKILLS.md) — Agent Skills system
- [agents/skills/fountain-analysis/SKILL.md](agents/skills/fountain-analysis/SKILL.md) — Fountain file analysis skill

*Documentation generated from recorder.go, replay.go, and sessions_files.go source code. Version 1.0.*
