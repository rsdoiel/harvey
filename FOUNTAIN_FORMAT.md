# Harvey Fountain Format Specification

*Version 1.0 — Multi-model character attribution for Harvey session recordings*

---

## Overview

Harvey uses the **[Fountain](https://fountain.io)** screenplay format for session
recordings, stored with `.spmd` (Harvey-native) or `.fountain` (compatible with
other tools) extensions. Fountain provides a plain-text, human-readable, and
machine-parseable format that serves as a **lingua franca** between Harvey,
Claude Code, and other LLM-based agents.

### Why Fountain?

| Requirement | Fountain Solution |
|-------------|-------------------|
| Human-readable | Plain text, familiar screenplay format |
| Machine-parseable | Strict structure, regex-friendly |
| Version-control friendly | Text files, good diff/merge behavior |
| Multi-participant | Natural character/dialogue model |
| Extensible | Scene descriptions, parenthetical notes |
| Cross-agent compatible | Open format, shared with Claude Code |

### File Extensions

| Extension | Created By | Notes |
|-----------|------------|-------|
| `.spmd` | Harvey | Primary format for new recordings |
| `.fountain` | Any Fountain-compatible tool | Accepted by Harvey for reading |


## Character Model

Harvey Fountain sessions treat each participant (human, Harvey agent, or LLM)
as a **distinct character** in a screenplay. Character names follow specific
conventions that reveal their identity and role.

### Character Types

| Type | Naming Convention | Example | Identity |
|------|-------------------|---------|----------|
| Human User | ALL-CAPS (matches `Author:`) | RSDOIEL | Human participant |
| Harvey Agent | **HARVEY** | HARVEY | Harvey using local Ollama/Llamafile |
| Routed Ollama | **ROUTE_NAME** (ALL-CAPS) | PI2, NODE1, JULIE | Remote Ollama via `/route add` |
| Cloud Model | **MODEL_NAME** (ALL-CAPS) | MISTRAL, CLAUDE, GEMMA4 | Cloud API or forwarded |

### Character Identity Rules

**HARVEY** represents Harvey when:
- Using its locally configured Ollama backend
- Using its locally configured Llamafile backend
- The active model is noted in the scene description: `Model: llama3:latest`

**ROUTE_NAME** represents a remote Ollama instance when:
- Defined via `/route add NAME URL` (e.g., `/route add pi2 ollama://192.168.1.2:11434`)
- Harvey forwards to it: `HARVEY\nForwarding to PI2.`
- The route appears as a character: `PI2\nResponse...`

**MODEL_NAME** represents a cloud/remote model when:
- User uses @mention: `RSDOIEL\n@mistral explain this`
- Harvey forwards: `HARVEY\nForwarding to MISTRAL.`
- Model responds: `MISTRAL\nExplanation...`
- OR in EXT. scenes (direct conversation without Harvey)

### Character Naming Conventions

| Pattern | Matches | Example |
|---------|---------|---------|
| `^[A-Z]{2,}$` | Cloud model names | CLAUDE, MISTRAL, LLAMA3 |
| `^[A-Z][a-zA-Z0-9_-]*$` | Mixed case (rare) | Julie (if configured as route) |
| `^HARVEY$` | Harvey agent | HARVEY |
| Author name (from title) | Human user | RSDOIEL |

## Scene Types

Harvey uses two scene type prefixes from Fountain: **INT.** (Interior) and
**EXT.** (Exterior). These distinguish whether Harvey is involved in the
conversation.

### INT. — Interior Scenes (Harvey Involved)

Harvey is acting as the **agent/orchestrator** in these scenes. The scene
heading lists Harvey and the human participant.

**Format:**
```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

RSDOIEL
User prompt here...

HARVEY
Harvey's response (local model) or forwarding...
```

**Use cases:**
- Harvey responding with its local Ollama/Llamafile model
- Harvey forwarding to routed Ollama endpoints
- Harvey forwarding to cloud models via @mention
- Agent actions (file writes)
- Skill activations

### EXT. — Exterior Scenes (Direct Model-Human)

Direct conversation between a model and human **without Harvey's
intermediation**. Harvey is not involved in these scenes.

**Format:**
```
EXT. MISTRAL AND RSDOIEL 2026-05-04 18:30:00

MISTRAL and RSDOIEL in direct conversation. Workspace: /home/user/project.

RSDOIEL
Direct prompt to Mistral...

MISTRAL
Mistral's direct response...
```

**Use cases:**
- Direct API conversations (bypassing Harvey)
- Hypothetical: Future direct model access

## Scene Structure

### Title Block (Before FADE IN:)

The title block contains metadata about the session. All fields are optional
except `Title:` and `Author:`.

**Fields:**

| Field | Required | Example | Description |
|-------|----------|---------|-------------|
| `Title:` | Yes | `Harvey Session` | Session title |
| `Credit:` | No | `Recorded by Harvey` | Recording application |
| `Author:` | Yes | `RSDOIEL` | Human participant (ALL-CAPS) |
| `Date:` | No | `2026-05-04 18:30:00` | Session start timestamp |
| `Draft date:` | No | `2026-05-04` | Date only |
| `Characters:` | No | `RSDOIEL, HARVEY, MISTRAL` | All characters (summary files only) |

**Example:**
```
Title: Harvey Session - Documentation Review
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 18:30:00
Draft date: 2026-05-04
Characters: RSDOIEL, HARVEY, MISTRAL

FADE IN:
```

**Note:** The `Characters:` field is typically omitted in **streaming sessions**
(since future characters are unknown) and added in **summary/analysis files**
(where all participants are known).

### Scene Heading

**Format:**
```
(INT|EXT)\. CHARACTER1 AND CHARACTER2 (TALKING|) TIMESTAMP
```

**Regex:**
```
(INT|EXT)\. ([A-Z0-9_-]+) AND ([A-Z0-9_-]+) (TALKING)? (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})
```

**Examples:**
```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00
INT. AGENT MODE 2026-05-04 18:35:00
INT. SKILL FOUNTAIN-ANALYSIS 2026-05-04 18:40:00
EXT. MISTRAL AND RSDOIEL 2026-05-04 18:45:00
```

### Scene Description

The scene description appears between the scene heading and the first
character/dialogue line. It provides context for the scene.

**Required fields:**
- `Model: <identifier>` — The model/route for this interaction
- `Workspace: <path>` — The workspace directory

**Optional fields:**
- Any additional context (e.g., "Connected: Ollama (llama3:latest)")
- Parenthetical notes about @mentions: `(@modelname mentioned but does not respond)`

**Examples:**
```
# Harvey-native (local Ollama)
Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

# Routed Ollama
Harvey and RSDOIEL are in chat mode. Model: ollama://192.168.1.2:11434. Workspace: /home/user/project.

# With @mention note
Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.
(@julie mentioned but does not respond)
```

### Dialogue

Dialogue consists of **character names** (ALL-CAPS) followed by their **lines**.
Character names must match the names in the scene heading or be introduced
via forwarding.

**Format:**
```
CHARACTER_NAME
Dialogue line 1
Dialogue line 2

ANOTHER_CHARACTER
Response...
```

**Rules:**
- Character names are always ALL-CAPS
- Blank line separates character from dialogue
- Blank line between dialogue blocks
- Dialogue wraps naturally (no special formatting needed)

### Special Syntax

#### Forwarding

When Harvey forwards a prompt to another model:
```
HARVEY
Forwarding to MODEL_NAME.

MODEL_NAME
Response...
```

#### @Mention

User can direct prompts to specific models:
```
RSDOIEL
@mistral explain this code

HARVEY
Forwarding to MISTRAL.

MISTRAL
Explanation...
```

#### File Operations (Agent Scenes)

```
HARVEY
Write path/to/file?

RSDOIEL
yes

[[write: path/to/file — ok]]

[[write: path/to/other — skipped]]

[[write: path/to/error — error: permission denied]]
```

Status values: `ok`, `skipped`, `error: <message>`

#### Session End

Every session file must end with:
```
THE END.
```

This marker is used by parsers to detect complete sessions.

## Model Representation Scenarios

### Scenario 1: Harvey Using Local Ollama

Harvey responds directly using its configured local model.

```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

RSDOIEL
What is the capital of France?

HARVEY
The capital of France is Paris.
```

**Character:** HARVEY (Harvey agent with local model)
**Model:** llama3:latest (from scene description)

### Scenario 2: Harvey Forwarding to Routed Ollama

Harvey forwards to a remote Ollama instance registered as a route.

```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00

Harvey and RSDOIEL are in chat mode. Model: ollama://192.168.1.2:11434. Workspace: /home/user/project.

RSDOIEL
@pi2 analyze this data

HARVEY
Forwarding to PI2.

PI2
Analysis: The data shows a normal distribution with mean 42.
```

**Route:** PI2 (registered via `/route add pi2 ollama://192.168.1.2:11434`)
**Character:** PI2 (the route name as character)

### Scenario 3: Harvey Forwarding to Cloud Model via @mention

User uses @mention to direct a prompt to a specific cloud model.

```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

RSDOIEL
@mistral review this code

HARVEY
Forwarding to MISTRAL.

MISTRAL
The code follows good practices. Consider adding error handling for the edge case at line 42.
```

**Character:** MISTRAL (cloud model)
**Trigger:** @mention in user dialogue

### Scenario 4: Direct External Conversation

Direct conversation with a model, bypassing Harvey.

```
EXT. CLAUDE AND RSDOIEL 2026-05-04 18:30:00

CLAUDE and RSDOIEL in direct conversation. Workspace: /home/user/project.

RSDOIEL
Explain quantum computing

CLAUDE
Quantum computing uses quantum bits (qubits) that can exist in superposition...
```

**Note:** No HARVEY character appears in EXT. scenes.

### Scenario 5: Unfulfilled @mention

User mentions a model that doesn't respond.

```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.
(@julie mentioned but does not respond)

RSDOIEL
@julie what do you think?

HARVEY
Julie does not respond.
```

**Note:** The unfulfilled @mention is noted in the scene description.

### Scenario 6: Multi-Model Within Scene

Multiple models introduced within a single scene via @mention.

```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

RSDOIEL
@mistral review this, then @claude give a second opinion

HARVEY
Forwarding to MISTRAL.

MISTRAL
First opinion: The code is well-structured.

HARVEY
Forwarding to CLAUDE.

CLAUDE
Second opinion: I agree, but consider adding tests.
```

**Note:** Multiple models can appear in a single INT. scene via sequential forwarding.

## Scene Types Reference

### Chat Scenes (`INT. … AND … TALKING` or `EXT. … AND …`)

Regular conversation between participants.

**INT. example:**
```
INT. HARVEY AND RSDOIEL TALKING 2026-05-04 18:30:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /path.

RSDOIEL
Prompt...

HARVEY
Response...
```

**EXT. example:**
```
EXT. MISTRAL AND RSDOIEL 2026-05-04 18:30:00

MISTRAL and RSDOIEL in direct conversation. Workspace: /path.

RSDOIEL
Prompt...

MISTRAL
Response...
```

### Agent Mode Scenes (`INT. AGENT MODE`)

File write operations and other agent actions.

```
INT. AGENT MODE 2026-05-04 18:35:00

HARVEY
Harvey proposes to write 1 file(s) to the workspace.

HARVEY
Write path/to/file?

RSDOIEL
yes

[[write: path/to/file — ok]]
```

**Fields:**
- Proposal: `Harvey proposes to write N file(s) to the workspace.`
- Prompt: `Write path?`
- Response: User's `yes`/`no`
- Outcome: `[[write: path — status]]`

### Skill Scenes (`INT. SKILL <NAME>`)

Skill activation and execution.

```
INT. SKILL FOUNTAIN-ANALYSIS 2026-05-04 18:40:00

Harvey executes the fountain-analysis skill.

FOUNTAIN-ANALYSIS
Reading agents/sessions/session.spmd for analysis.
```

**Fields:**
- Skill name (from heading)
- Description action line

## Parsing Rules for Tools

### File Structure

```
^Title: .*$
^Credit: .*$
^Author: ([A-Z]+)$
^Date: \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$
^Draft date: \d{4}-\d{2}-\d{2}$
^(Characters: .*)?$
^$ 
FADE IN:$ 
^$ 
```

### Scene Parsing

1. **Match scene heading:** `(INT|EXT)\. ([A-Z0-9_-]+) AND ([A-Z0-9_-]+) (TALKING)? (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`
2. **Extract scene description:** Lines between heading and first ALL-CAPS character
3. **Parse scene description:**
   - `Model: (.+)` → model identifier
   - `Workspace: (.+)` → workspace path
   - `\(@(\w+) mentioned but does not respond\)` → unfulfilled @mention
4. **Parse dialogue:**
   - Character: `^[A-Z0-9_-]+$`
   - Dialogue: Subsequent lines until next character or blank line
5. **Detect special syntax:**
   - Forwarding: `^Forwarding to ([A-Z0-9_-]+)\.$`
   - File write: `^\[\[write: (.+) — (ok|skipped|error:.+)\]\]`
   - @mention: `@([a-zA-Z][a-zA-Z0-9_-]*)`

### Character Type Detection

```python
# Pseudocode
def character_type(name, scene_description, known_routes, author):
    if name == "HARVEY":
        return "harvey-native"
    elif name in known_routes:
        return "routed-ollama"
    elif name == author:
        return "human"
    elif is_all_caps_alphanumeric(name):
        return "cloud-model"
    else:
        return "unknown"
```

## Complete Example Sessions

### Example 1: Basic Harvey Session (Local Model)

```
Title: Quick Question
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 10:00:00
Draft date: 2026-05-04

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-05-04 10:00:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

RSDOIEL
What is 2+2?

HARVEY
4

THE END.
```

### Example 2: Multi-Model with @mention

```
Title: Code Review
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 14:00:00
Draft date: 2026-05-04

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-05-04 14:00:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/code.

RSDOIEL
@mistral review this function

HARVEY
Forwarding to MISTRAL.

MISTRAL
The function has a potential off-by-one error at line 23.

RSDOIEL
@claude can you confirm?

HARVEY
Forwarding to CLAUDE.

CLAUDE
Confirmed. The loop should use `<=` not `<`.

THE END.
```

### Example 3: With File Operations

```
Title: Fix and Test
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 15:00:00
Draft date: 2026-05-04

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-05-04 15:00:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/code.

RSDOIEL
This function has a bug. Please fix it.

HARVEY
Here is the corrected version:

```go src/calc.go
func Add(a, b int) int {
    return a + b
}
```

RSDOIEL
/apply


INT. AGENT MODE 2026-05-04 15:02:00

HARVEY
Harvey proposes to write 1 file(s) to the workspace.

HARVEY
Write src/calc.go?

RSDOIEL
yes

[[write: src/calc.go — ok]]


INT. HARVEY AND RSDOIEL TALKING 2026-05-04 15:03:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/code.

RSDOIEL
/run go test ./...

HARVEY
All tests pass.

THE END.
```

### Example 4: Routed Ollama

```
Title: Cluster Query
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 16:00:00
Draft date: 2026-05-04

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-05-04 16:00:00

Harvey and RSDOIEL are in chat mode. Model: ollama://192.168.1.2:11434. Workspace: /home/user/data.

RSDOIEL
@pi2 process this dataset

HARVEY
Forwarding to PI2.

PI2
Processing complete. 1000 records analyzed.

THE END.
```

### Example 5: Unfulfilled @mention

```
Title: Offline Model
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 17:00:00
Draft date: 2026-05-04

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-05-04 17:00:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.
(@offline-model mentioned but does not respond)

RSDOIEL
@offline-model what is your status?

HARVEY
Offline-model does not respond.

THE END.
```

## Validation Rules

### Required Elements

| Element | Required | Validation |
|---------|----------|------------|
| `Title:` | Yes | Non-empty |
| `Author:` | Yes | ALL-CAPS, non-empty |
| `FADE IN:` | Yes | Exact match |
| `THE END.` | Yes | Exact match at end |
| Scene headings | Per scene | Valid INT./EXT. format |
| Scene descriptions | Per scene | Contains `Model:` and `Workspace:` |

### Character Validation

| Rule | Error |
|------|-------|
| Character not ALL-CAPS | Warn: "Character name not uppercase: {name}" |
| Character in INT. scene not in heading | Warn: "Character {name} not in scene heading" |
| HARVEY missing from INT. scene | Warn: "INT. scene without HARVEY" |
| HARVEY in EXT. scene | Warn: "EXT. scene contains HARVEY" |
| Unknown character type | Warn: "Unknown character type: {name}" |

### Model Validation

| Rule | Error |
|------|-------|
| Missing `Model:` in description | Warn: "Scene missing Model: declaration" |
| Model in description doesn't match character | Warn: "Model mismatch: description={x}, character={y}" |

## Best Practices

### For Session Recording (Harvey)

1. **Always include** `Model:` and `Workspace:` in scene descriptions
2. **Use INT.** for all Harvey-mediated conversations
3. **Use EXT.** only for direct model-human conversations (no Harvey)
4. **Attribute correctly**: HARVEY for local, ROUTE_NAME for routed, MODEL_NAME for cloud
5. **Track @mentions**: Note unfulfilled mentions in scene description
6. **Start new scenes** when model changes (for clarity in analysis)

### For Session Analysis

1. **Extract all characters** from dialogue (not just heading)
2. **Track model timeline** across scenes
3. **Distinguish** Harvey-native vs forwarded responses
4. **Report @mention status** (fulfilled/unfulfilled)
5. **Warn on inconsistencies** (model mismatches, missing declarations)

### For Human Readability

1. **Keep scene descriptions concise** but informative
2. **Use parenthetical notes** sparingly (for @mention tracking)
3. **Group related exchanges** in single scenes when possible
4. **Start new scenes** on major topic shifts or model changes

## Compatibility Notes

### Claude Code Compatibility

Harvey accepts `.fountain` files created by Claude Code. These files may use
slightly different formatting but follow the same fundamental structure:

- Scene headings with character names
- Dialogue blocks under characters
- Scene descriptions between headings and dialogue

**Differences to handle:**
- Claude Code may use different scene description formats
- Model names may appear differently
- File operations may use different syntax

Harvey's parser is designed to be **tolerant** of these variations while
maintaining strict output format for its own recordings.

### Future Compatibility

The Fountain format is designed to be **extensible**. Future versions may add:
- New scene types
- Additional metadata fields
- New special syntax elements

Older parsers should **gracefully ignore** unknown elements.

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2026-05-04 | Initial specification with multi-model character attribution |

*This document describes Harvey's use of the Fountain format. For the official
Fountain specification, see https://fountain.io.*
