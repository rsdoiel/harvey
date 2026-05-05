# Harvey Skills System

*Version 1.0 — Complete guide to Agent Skills in Harvey*

## Overview

Harvey implements the **[Agent Skills](https://agentskills.dev)** specification,
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
3. **Optional scripts** — compiled executables for direct execution

Skills are discovered automatically by Harvey at startup and can be invoked
via the `/skill` command or automatically via triggers.

## Quick Start

### List Available Skills

```
harvey> /skill list

  Current model: llama3:latest

  compile-skill           [project] - Compiles a target SKILL.md into correct compiled.bash and...
  fountain-analysis       [project] - Read and actively monitor a Harvey Fountain screenplay file...
  review-knowledge-base   [project] - Queries knowledge_base.db and delivers a structured report...
  setup-codemeta...       [project] - Generates or updates a codemeta.json file for an experiment...
  setup-experiment        [project] - Creates a new experiment directory in the Laboratory...
  setup-knowledge-base    [project] - Creates knowledge_base.db in the workspace...
  update-knowledge-base   [project] - Adds or updates records in knowledge_base.db...
  fetch-pg-corpus         [project] - Downloads the Project Gutenberg catalog and full text...
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

### Run a Compiled Skill

```
harvey> /skill run fountain-analysis

  Running skill: fountain-analysis
  FOUNTAIN_FILE? session.spmd
  ... (skill executes directly)
```

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

## Compiled Script Guidance

> This section is for the LLM when generating compiled.bash/compiled.ps1.
> Read it carefully before writing any code.

**Script structure:**
- Use `#!/usr/bin/env bash` and `set -euo pipefail`
- ...
```
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

When a skill is invoked, variables can be passed:
- Via HARVEY_PROMPT environment variable (for compiled skills)
- Via command arguments (for `/skill run`)
- Via interactive prompting (if not provided)

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

### Type 2: Compiled Skills

Skills with `scripts/compiled.bash` and/or `scripts/compiled.ps1` can be
executed **directly** without LLM intervention. This is faster and more
deterministic.

**Structure:**
```
skill-name/
├── SKILL.md              # Skill definition
└── scripts/
    ├── compiled.bash     # Linux/macOS script
    └── compiled.ps1      # Windows PowerShell script
```

**When to compile:**
- Tasks that are purely procedural (no creativity needed)
- Tasks that benefit from direct shell execution
- Tasks that need to run tools not available to the LLM

**Compilation process:**
1. Create the skill with clear instructions
2. Use `/skill load compile-skill` to generate scripts
3. Review the generated scripts
4. Commit them alongside SKILL.md

### Type 3: Triggered Skills

Skills with a `trigger:` field can be **auto-dispatched** when user input
matches the pattern.

**Trigger types:**
- **Keyword mode:** `trigger: pdf extract document` — Any word matches
- **Regexp mode:** `trigger: /\bpdf\b/` — Pattern must match

**Example:**
```yaml
trigger: /\b(run|execute) tests?\b/
```

When the user types "run tests" or "execute test", Harvey automatically invokes
the skill (if it's compiled) or loads it into context.

## Commands Reference

### `/skill list`

List all discovered skills with their name, source, and first line of
description.

**Output:**
```
  Current model: llama3:latest

  compile-skill           [project] Compiles a target SKILL.md...
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
  Path:          /home/user/Laboratory/agents/skills/fountain-analysis/SKILL.md
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

### `/skill run NAME [ARGS...]`

Run a **compiled skill** directly. The skill's compiled.bash or compiled.ps1
script is executed with HARVEY_PROMPT set to the arguments.

**Example:**
```
harvey> /skill run fountain-analysis session.spmd

  Running skill: fountain-analysis
  [Output from compiled.bash]
```

**Note:** This only works for skills with compiled scripts. For LLM-executed
skills, use `/skill load` instead.

## Compiled Scripts

### Environment Variables

When a compiled skill script runs, Harvey sets these environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `HARVEY_PROMPT` | The user's prompt/arguments | `session.spmd` |
| `HARVEY_WORKDIR` | Absolute workspace root | `/home/user/project` |
| `HARVEY_MODEL` | Current model name | `llama3:latest` |
| `HARVEY_SESSION_ID` | Current session ID | `20260504-143000` |

**Example compiled.bash:**
```bash
#!/usr/bin/env bash
set -euo pipefail

# Parse arguments from HARVEY_PROMPT
read -ra _args <<< "${HARVEY_PROMPT:-}"
FOUNTAIN_FILE="${_args[0]:-}"

# Validate
if [ -z "$FOUNTAIN_FILE" ]; then
    read -rp "Fountain file path: " FOUNTAIN_FILE
fi

# Execute
if [ ! -f "$FOUNTAIN_FILE" ]; then
    echo "Error: File not found: $FOUNTAIN_FILE" >&2
    exit 1
fi

echo "Analyzing: $FOUNTAIN_FILE"
# ... rest of script
```

### Script Generation Rules

When generating compiled scripts (via compile-skill), follow these rules:

**For compiled.bash:**
1. Shebang: `#!/usr/bin/env bash`
2. Safety: `set -euo pipefail`
3. Variable parsing: Use `read -ra _args <<< "${HARVEY_PROMPT:-}"`
4. Safe defaults: Always use `${VAR:-}` or `${VAR:-default}`
5. No functions, no subshells
6. SQLite3: Call as command, never redirect into db file
7. Heredocs: Use single-quoted delimiter (`<<'EOF'`) for literal content
8. Tool checks: `command -v tool >/dev/null 2>&1 || { echo "error"; exit 1; }`

**For compiled.ps1:**
1. Error handling: `$ErrorActionPreference = 'Stop'`
2. Variable parsing: `$promptArgs = ($env:HARVEY_PROMPT ?? '').Trim() -split '\s+', N`
3. PowerShell-only syntax (no bash-isms)
4. Tool checks: `Get-Command tool -ErrorAction SilentlyContinue`

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

### Staleness Checking

For compiled skills, Harvey checks if SKILL.md is newer than the compiled
scripts. If so, it warns:

```
  Warning: Skill "name" has been modified since compilation.
  Recompile with: /skill compile name
```

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

3. (Optional) Create compiled scripts:
   ```bash
   mkdir -p agents/skills/my-skill/scripts
   touch agents/skills/my-skill/scripts/compiled.bash
   touch agents/skills/my-skill/scripts/compiled.ps1
   ```

4. (Optional) Compile the skill:
   ```
harvey> /skill compile my-skill
   ```

## Compiling a Skill

Compile a SKILL.md into executable scripts using the LLM:

```
harvey> /skill compile my-skill
  Compiling skill "my-skill"...
  ✓ Compiled: scripts/compiled.bash and scripts/compiled.ps1
  Tip: you can now run it as /skill run my-skill
```

**What happens:**
1. Harvey uses the current LLM to generate scripts
2. Reads the SKILL.md for variable definitions and instructions
3. Generates `scripts/compiled.bash` for Linux/macOS/BSD
4. Generates `scripts/compiled.ps1` for Windows
5. Makes both scripts executable (where applicable)

**Note:** Compilation requires a capable model. Small local models may not
generate good scripts. Use Claude Code or a large cloud model for compilation.

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
| `compile-skill` | `CompileSkill` | Lowercase only |

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
4. For compiled skills: `/skill run my-skill arg1 arg2`

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

### Compiled skill not executing

**Causes:**
- Scripts not compiled yet
- Scripts not executable
- Missing shebang line
- Syntax errors in script

**Fix:**
```
# Compile the skill
harvey> /skill compile my-skill

# Check file permissions
ls -la agents/skills/my-skill/scripts/

# Make executable (if needed)
chmod +x agents/skills/my-skill/scripts/compiled.bash
```

### Skill stale (warning)

**Cause:** SKILL.md was modified after compilation.

**Fix:**
```
# Recompile
harvey> /skill compile my-skill
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
└── scripts/                    # Optional: Compiled scripts
    ├── compiled.bash          # Linux/macOS/BSD script
    └── compiled.ps1           # Windows PowerShell script
```

## Cross-Agent Compatibility

Harvey skills are compatible with **Claude Code** and any other agent that
implements the [Agent Skills specification](https://agentskills.dev).

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
    <location>/home/user/Laboratory/agents/skills/fountain-analysis/SKILL.md</location>
  </skill>
  <skill>
    <name>review-knowledge-base</name>
    <description>Queries knowledge_base.db and delivers...</description>
    <location>/home/user/Laboratory/agents/skills/review-knowledge-base/SKILL.md</location>
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
| **compile-skill** | Compiles SKILL.md into executable scripts |
| **fountain-analysis** | Reads and analyzes Fountain session files |
| **review-knowledge-base** | Queries and reports on knowledge_base.db |
| **setup-codemeta...** | Generates codemeta.json for experiments |
| **setup-experiment** | Creates new experiment directory + git init |
| **setup-knowledge-base** | Initializes knowledge_base.db with schema |
| **update-knowledge-base** | Adds records to knowledge_base.db |
| **fetch-pg-corpus** | Downloads Project Gutenberg corpus |

## See Also

- [Agent Skills Specification](https://agentskills.dev) — Official spec
- [SKILL.md files in agents/skills/](agents/skills/) — Built-in skills
- [fountain-analysis SKILL.md](agents/skills/fountain-analysis/SKILL.md) — Example of comprehensive skill
- [ARCHITECTURE.md](ARCHITECTURE.md) — Technical details on skill implementation
- [compile-skill SKILL.md](agents/skills/compile-skill/SKILL.md) — Skill compilation guide
