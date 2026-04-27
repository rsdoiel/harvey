---
name: fountain-session
description: Write a Harvey-compatible Fountain screenplay document summarizing a coding session for handoff or replay by Harvey.
license: AGPL-3.0
compatibility: claude-code, harvey
metadata:
  author: rsdoiel
  version: "1.0"
---

# Fountain Session

## When to use this skill

Use this skill when the user asks you to:
- Summarize a Claude Code session as a Fountain document for Harvey to continue
- Write a handoff document so Harvey can resume context with a local Ollama model
- Create a session file that Harvey can replay (`harvey --replay session.fountain`)
- Document a completed session as a human-readable screenplay record

## Harvey's Fountain format

Harvey reads and writes Fountain screenplay files using a strict scene structure.
Follow these conventions exactly so Harvey's parser can round-trip the document.

### Title page

Always begin with these key-value pairs (no blank lines between them), then one
blank line, then `FADE IN:`:

```
Title: Harvey Session
Credit: Recorded by Claude Code
Author: {USER_ALLCAPS}
Date: {YYYY-MM-DD HH:MM:SS}
Draft date: {YYYY-MM-DD}

FADE IN:
```

### Chat scene — one per meaningful exchange

```
INT. HARVEY AND {USER} TALKING {YYYY-MM-DD HH:MM:SS}

Harvey and {USER} are in chat mode. Model: {MODEL}. Workspace: {workspace_path}.

{USER}
The user's message, verbatim or faithfully summarised.

HARVEY
Forwarding to {MODEL}.

{MODEL}
The assistant's response, verbatim or faithfully summarised.
```

Rules:
- `{USER}` and `{MODEL}` are ALL CAPS identifiers — use the actual `$USER` env value
  uppercased (e.g., `RSDOIEL`) and a short model tag (e.g., `CLAUDE`, `LLAMA3`)
- The scene heading **must** contain ` HARVEY AND ` and ` TALKING ` for Harvey's
  parser to recognise it as a chat scene
- HARVEY's dialogue line **must** read `Forwarding to {MODEL}.` exactly — Harvey
  extracts the model name from this line
- One blank line between elements; two blank lines before each new scene heading
- **Never use blank lines inside a dialogue block** — the Fountain parser treats a
  blank line as the end of dialogue and starts a new element type

### Agent scene — when files were written

```
INT. AGENT MODE {YYYY-MM-DD HH:MM:SS}

HARVEY
Harvey proposes to write N file(s) to the workspace.

HARVEY
Write path/to/file.go?

{USER}
yes

[[write: path/to/file.go — ok]]
```

### Skill scene — when a skill was activated

```
INT. SKILL {SKILL-NAME} {YYYY-MM-DD HH:MM:SS}

Harvey executes the {skill-name} skill.

{One-line description of what the skill does.}

{SKILL-NAME}
The skill's instructions or output delivered to Harvey.
```

The skill character name is the skill identifier uppercased: `go-review` → `GO-REVIEW`.

### Closing

Always end with:

```
THE END.
```

## What to include

**Include verbatim:**
- User questions that represent the core intent of the session
- Code blocks, file paths, and exact decisions made
- Error messages that shaped the solution direction
- Final agreed-upon implementation steps

**Summarise concisely:**
- Exploratory back-and-forth before a direction was settled
- Long LLM responses — keep the key conclusion, trim the explanation

**Omit:**
- Purely administrative exchanges ("thanks", "looks good")
- Abandoned attempts that left no lasting effect on the codebase
- Duplicate questions that were rephrased and answered later

## Complete example

```
Title: Harvey Session
Credit: Recorded by Claude Code
Author: RSDOIEL
Date: 2026-04-17 14:23:00
Draft date: 2026-04-17

FADE IN:


INT. HARVEY AND RSDOIEL TALKING 2026-04-17 14:23:00

Harvey and RSDOIEL are in chat mode. Model: CLAUDE. Workspace: /home/rsdoiel/proj.

RSDOIEL
Add a /compact alias for /summarize in commands.go.

HARVEY
Forwarding to CLAUDE.

CLAUDE
I'll add the alias by registering "compact" in registerCommands pointing
to cmdSummarize. Let me make the edit.


INT. AGENT MODE 2026-04-17 14:24:10

HARVEY
Harvey proposes to write 1 file(s) to the workspace.

HARVEY
Write commands.go?

RSDOIEL
yes

[[write: commands.go — ok]]


INT. HARVEY AND RSDOIEL TALKING 2026-04-17 14:25:00

Harvey and RSDOIEL are in chat mode. Model: CLAUDE. Workspace: /home/rsdoiel/proj.

RSDOIEL
Run the tests to confirm.

HARVEY
Forwarding to CLAUDE.

CLAUDE
Running go test ./... — all tests pass. The /compact alias is live.

THE END.
```

## Handoff note

After writing the Fountain file, tell the user the path and suggest:

```
harvey --continue session.fountain    # resume with full context
harvey --replay   session.fountain    # re-run turns against local model
```

Or inside a running Harvey REPL:

```
/session continue session.fountain
/session replay  session.fountain new-session.fountain
```
