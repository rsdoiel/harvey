# Harvey Skills System

*Version 1.0 — Complete guide to Agent Skills in Harvey*

## Overview

Harvey implements the **[Agent Skills](https://agentskills.io/home)** specification,
which provides a standardized way to define, discover, and execute structured
tasks across different AI agents. Skills allow you to:

- **Extend Harvey's capabilities** without modifying code
- **Share tasks** between Harvey, Claude Code, and other compatible agents
- **Create reusable workflows** for common operations
- **Document complex procedures** in a machine-executable format

### What is a Skill?

A **Skill** is a **SKILL.md** file that contains:
1. **Metadata** (YAML frontmatter) — name, description, variables
2. **Instructions** (Markdown) — step-by-step guidance for the LLM
3. **Optional resources** — `scripts/`, `references/` and `assets/` files the instructions point at

Skills are discovered automatically by Harvey at startup and can be invoked
via the `/skill` command, as `/NAME`, or automatically via triggers. A skill is used
directly: its instructions are loaded into the conversation and the model follows them.
Harvey does not compile skills to Bash or PowerShell (workspace DR-0008).

## Quick Start

### List Available Skills

```
harvey> /skill list

  Current model: llama3:latest

  fountain-analysis       [project] - Read and actively monitor a Harvey Fountain screenplay file...
  fountain-session        [project] - Write a well-formed Harvey session handoff summary as a...
  harvey-memory           [project] - Load Harvey's accumulated project memories before working...
  multi-file              [project] - Generates a bounded-context execution plan when the user asks...
  review-knowledge-base   [project] - Queries knowledge_base.db and delivers a structured report...
  setup-knowledge-base    [project] - Creates knowledge_base.db in the workspace...
  update-knowledge-base   [project] - Adds or updates records in knowledge_base.db...
```

### Load a Skill

```
harvey> /skill load fountain-analysis

  ✓ Skill "fountain-analysis" loaded into context (1234 chars).
```

The skill's full instructions are injected into the conversation, and the LLM
will follow them to complete the task.

### Get Skill Info

```
harvey> /skill info fountain-analysis

  Name:          fountain-analysis
  Description:   Read and actively monitor a Harvey Fountain screenplay file...
  Source:        project
  Path:          /home/user/Laboratory/agents/skills/fountain-analysis/SKILL.md
  License:       AGPL-3.0
  Compatibility: claude-code, harvey
```

### Use a Skill Directly

```
harvey> /fountain-analysis summarize session.spmd

  ✓ Skill "fountain-analysis" loaded into context (1234 chars).
  ... (the model answers "summarize session.spmd" with the skill in context)
```

`/NAME` loads the skill like `/skill load NAME`; any text after the name is sent to the
model as your next message.

## Skill Discovery Paths

Harvey scans for skills in the workspace's `agents/skills/` directory.

### Project Scope

| Path | Scope | Notes |
|------|-------|-------|
| `<workdir>/agents/skills/` | Harvey-native and Cross-client | Shared with other agents |

**Example:** A skill named `review` in `<workdir>/agents/skills/`.

## SKILL.md Format

### Frontmatter (YAML)

Every SKILL.md file **must** begin with a YAML frontmatter block containing
at minimum the `name` and `description` fields.

**Required fields:**
- `name` — Unique identifier (lowercase, hyphens only)
- `description` — What the skill does and when to use it

**Optional fields:**
- `license` — License identifier (e.g., `AGPL-3.0`)
- `compatibility` — Compatible agents (e.g., `claude-code, harvey`)
- `version` — Skill version (string)
- `trigger` — Auto-dispatch pattern (keyword or regexp)
- `allowed-tools` — Space-separated tool allowlist
- `variables` — Input variables block
- `metadata` — Arbitrary key-value pairs

**Example Frontmatter:**
```yaml
---
name: review-code
description: |
  Reviews Go source code for correctness, style, and idiomatic patterns.
  Use when the user asks to review, audit, or critique Go code.
license: AGPL-3.0
compatibility: claude-code, harvey
metadata:
  author: R. S. Doiel
  version: "1.0"
variables:
  DIRECTORY:
    type: string
    description: Path to directory containing code to review
    example: "internal/parser"
---
```

### Content Structure

After the frontmatter, the skill content is structured Markdown with these
recommended sections:

```markdown
# Skill Title

Brief introduction of what the skill does.

## When to use this skill

Use when the user asks you to:
- Action 1
- Action 2
- Action 3

## Variables

| Variable | Type | Description | Default | Prompted? |
|----------|------|-------------|---------|-----------|
| DIRECTORY | string | Path to code | . | Yes |

## Step 1: Setup

Instructions for the LLM to follow.

## Step 2: Execution

More instructions.

## Error handling

| Condition | Action |
|-----------|--------|
| No files found | Report error |

```

### Variables Block

The `variables:` block in frontmatter defines input parameters that can be
passed to the skill:

```yaml
variables:
  NAME:
    type: string
    description: The name of something
    example: "myproject"
  COUNT:
    type: integer
    description: Number of items
    example: "5"
  ITEMS:
    type: array
    description: List of items
```

**Variable types:**
- `string` — Text input
- `integer` — Numeric input
- `array` — List of values

Harvey does not fill variables itself. Text given after `/NAME`, or typed after
`/skill load NAME`, reaches the model as ordinary text, and the skill's instructions
say which variables to take from it and what to ask for when one is missing.

## Skill Types

### Type 1: LLM-Executed Skills

The LLM reads the skill instructions and executes the steps by generating
appropriate responses. This is the most common type.

**Example:** `fountain-analysis` — The LLM reads the Fountain file and generates
an analysis report.

**Workflow:**
1. User invokes: `/skill load fountain-analysis`
2. Skill content injected into context
3. LLM reads instructions and follows them
4. LLM generates response based on skill guidance

### Type 2: Triggered Skills

Skills with a `trigger:` field can be **loaded automatically** when user input
matches the pattern.

**Trigger types:**
- **Keyword mode:** `trigger: pdf extract document` — Any word matches
- **Regexp mode:** `trigger: /\bpdf\b/` — Pattern must match

**Example:**
```yaml
trigger: /\b(run|execute) tests?\b/
```

When the user types "run tests" or "execute test", Harvey loads the skill into
context and then sends the message to the model.

> **Prompt injection risk:** If a trigger pattern uses broad keywords that naturally
> appear in code, documentation, or RAG-retrieved content, a skill may be activated by
> injected text rather than by the user's explicit intent. Keep trigger patterns
> specific to avoid unintentional loading.

## Commands Reference

### `/skill list`

List all discovered skills with their name, source, and first line of
description.

**Output:**
```
  Current model: llama3:latest

  fountain-analysis       [project] Read and actively monitor...
  review-knowledge-base   [project] Queries knowledge_base.db...
  ...
```

### `/skill load NAME`

Inject the full skill body into the conversation context. The LLM will then
follow the skill's instructions.

**Example:**
```
harvey> /skill load fountain-analysis
  ✓ Skill "fountain-analysis" loaded into context (4521 chars).
```

### `/skill info NAME`

Show detailed metadata about a skill.

**Output:**
```
  Name:          fountain-analysis
  Description:   Read and actively monitor a Harvey Fountain...
  Source:        project
  Path:          <workspace>/agents/skills/fountain-analysis/SKILL.md
  License:       AGPL-3.0
  Compatibility: claude-code, harvey
  Variables:
    FOUNTAIN_FILE (string, prompted) - Path to file
    INTERVAL (integer, default 30) - Poll interval
  Metadata:
    author: R. S. Doiel
    version: "2.0"
```

### `/skill status`

Show statistics about discovered skills.

**Output:**
```
  Total: 8 skill(s)
    Project scope: 8
```

### `/skill new`

Run the interactive **skill wizard** to create a new skill.

**Workflow:**
1. Prompts for skill name (must be valid identifier)
2. Prompts for description
3. Prompts for other metadata
4. Creates the skill directory with SKILL.md template
5. Opens SKILL.md in $EDITOR for editing

**Example:**
```
harvey> /skill new
  Skill name: review-code
  Description: Reviews Go source code
  License [AGPL-3.0]: 
  Compatibility [claude-code, harvey]: 
  Created: agents/skills/review-code/SKILL.md
  Open in editor? [Y/n] y
```

### `/NAME [REQUEST...]`

Every discovered skill is also a slash command. It loads the skill exactly as
`/skill load NAME` does; if REQUEST is given it is sent to the model as your next
message. A skill never replaces a built-in command of the same name.

**Example:**
```
harvey> /fountain-analysis session.spmd

  ✓ Skill "fountain-analysis" loaded into context (1234 chars).
```

## Skill Discovery & Loading

### Discovery Process

At startup, Harvey:

1. Builds list of search directories based on config
2. Scans each directory for subdirectories containing `SKILL.md`
3. Parses each SKILL.md file
4. Validates required fields (name, description)
5. Builds in-memory catalog indexed by name
6. Merges catalog into system prompt (for LLM awareness)

**Timing:** Discovery happens during agent initialization, before the
first prompt.

### Loading Process

When you invoke `/skill load NAME`:

1. Harvey looks up the skill in the catalog
2. Reads the full SKILL.md file
3. Extracts the body (content after frontmatter)
4. Injects into conversation as user message: `[skill: NAME]\n\n<body>`
5. Sets `ActiveSkill` to NAME for context
6. Records in session if recording is active

**Note:** Skills are **not** loaded automatically. You must explicitly
invoke `/skill load` or have a trigger match.

## Creating a New Skill

### Method 1: Interactive Wizard (`/skill new`)

```
harvey> /skill new
```

Follow the prompts to create a new skill with the wizard.

### Method 2: Manual Creation

1. Create the skill directory:
   ```bash
   mkdir -p agents/skills/my-skill
   ```

2. Create SKILL.md with proper frontmatter:
   ```markdown
   ---
   name: my-skill
   description: Does something useful
   license: AGPL-3.0
   compatibility: claude-code, harvey
   metadata:
     author: Your Name
     version: "1.0"
   ---
   
   # My Skill
   
   Instructions for the LLM...
   ```

## Best Practices

### Skill Design

**Do:**
- Make skills **single-purpose** and focused
- Include **clear examples** in the description
- Define **variables** for parameterization
- Add **error handling** guidance
- Document **preconditions** and **postconditions**
- Use **tables** for structured data

**Don't:**
- Make skills that modify Harvey's internals
- Require specific workspace structures (be flexible)
- Include hardcoded paths (use variables)
- Assume specific tools are available (check first)

### Naming Skills

| Good | Bad | Reason |
|------|-----|--------|
| `review-code` | `code-review` | Verb-first is more natural |
| `setup-experiment` | `experiment-setup` | Verb-first |
| `fetch-pg-corpus` | `getPGData` | Hyphens, lowercase |
| `wrap-up-session` | `WrapUpSession` | Lowercase only |

**Rules:**
- Lowercase letters only
- Hyphens to separate words
- No spaces or special characters
- Must match the directory name

### Variable Design

**Good variables:**
- Specific and focused
- Clear type (string, integer, array)
- Descriptive name
- Useful example value

**Bad variables:**
- Vague names (`input`, `data`)
- Too many variables (> 5)
- Complex nested structures
- No default values

### Testing Skills

Test your skill by:

1. Loading it: `/skill load my-skill`
2. Following the instructions manually
3. Verifying the LLM produces correct output
4. Try the same request with `/my-skill arg1 arg2`

## Troubleshooting

### "Skill not found"

**Causes:**
- Skill directory doesn't exist
- SKILL.md is missing or malformed
- Skill name doesn't match directory name
- Discovery path not scanned

**Fix:**
```
# Check discovery paths
harvey> /skill status

# Verify skill exists
ls agents/skills/my-skill/SKILL.md

# Check for YAML errors
# (malformed frontmatter causes parse failure)
```

### "Description is required"

**Cause:** SKILL.md frontmatter is missing the `description` field.

**Fix:** Add a description to the frontmatter:
```yaml
---
name: my-skill
description: Does something useful
...
---
```

### "Invalid skill name"

**Cause:** Name contains uppercase letters, spaces, or special characters.

**Fix:** Use only lowercase letters, numbers, and hyphens:
```yaml
name: my-skill  # good
name: My Skill  # bad (spaces, uppercase)
name: my_skill  # bad (underscore)
```

## Skill Directory Structure

A complete skill directory:

```
my-skill/
├── SKILL.md                    # Required: Skill definition
├── LICENSE                     # Optional: License file
├── README.md                   # Optional: Human-readable docs
├── references/                 # Optional: Reference material
│   └── guide.md
├── assets/                     # Optional: Templates, data files
│   └── template.txt
└── scripts/                    # Optional: helper scripts the instructions refer to
```

## Cross-Agent Compatibility

Harvey skills are compatible with **Claude Code** and any other agent that
implements the [Agent Skills specification](https://agentskills.io/home).

### Shared Skills Directory

Place skills in `<workspace>/agents/skills/` to make
them available to all compatible agents:

```
agents/
└── skills/
    ├── my-skill/          # Available to Harvey and Claude Code
    │   └── SKILL.md
    └── shared-skill/
        └── SKILL.md
```

### Compatibility Field

Specify which agents can use the skill:

```yaml
compatibility: claude-code, harvey
```

Or use a wildcard:
```yaml
compatibility: "*"
```

## Advanced: System Prompt Integration

Skills can be injected into Harvey's system prompt so the LLM knows what
skills are available without loading them all. The catalog is formatted as:

```xml
<available_skills>
  <skill>
    <name>fountain-analysis</name>
    <description>Read and actively monitor a Harvey Fountain...</description>
    <location><workspace>/agents/skills/fountain-analysis/SKILL.md</location>
  </skill>
  <skill>
    <name>review-knowledge-base</name>
    <description>Queries knowledge_base.db and delivers...</description>
    <location><workspace>/agents/skills/review-knowledge-base/SKILL.md</location>
  </skill>
</available_skills>

When a task matches a skill's description, the user can type:
  /skill load <name>
to activate it.
```

This helps the LLM suggest appropriate skills to the user.

## Reference: Built-in Skills

Harvey includes these skills out of the box (in `agents/skills/`):

| Skill | Purpose |
|-------|---------|
| **fountain-analysis** | Reads and monitors Harvey Fountain session files (`.spmd`) |
| **fountain-session** | Writes a well-formed session handoff summary in Fountain format |
| **harvey-memory** | Loads Harvey's accumulated workspace memories before a session |
| **multi-file** | Generates a bounded-context plan for tasks that create multiple files |
| **review-knowledge-base** | Queries and reports on `agents/knowledge.db` |
| **setup-knowledge-base** | Creates and initializes `agents/knowledge.db` with the standard schema |
| **update-knowledge-base** | Adds experiments, observations, and concepts to `agents/knowledge.db` |

## See Also

- [Agent Skills Specification](https://agentskills.io/home) — Official spec
- [SKILL.md files in agents/skills/](agents/skills/) — Built-in skills
- [fountain-analysis SKILL.md](agents/skills/fountain-analysis/SKILL.md) — Example of comprehensive skill
- [ARCHITECTURE.md](ARCHITECTURE.md) — Technical details on skill implementation
