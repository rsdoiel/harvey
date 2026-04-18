package harvey

var (
	// SkillsHelpText explains the Agent Skills feature and is displayed by
	// /help skills (REPL) or harvey --help skills (CLI).
	SkillsHelpText = `
SKILLS

Skills are Markdown files that inject specialised instructions into Harvey's
context on demand. Harvey scans for skills at startup but only loads a skill's
full instructions when you explicitly ask for it.


HOW SKILLS WORK

  1. Discovery — Harvey scans the standard paths below and builds a catalog
     of (name, description) pairs. The catalog is added to the system prompt
     so the model knows what skills are available.

  2. Activation — type /skill load <name> to inject the full skill body into
     the conversation. The model then follows the skill's instructions for
     the current task.

  3. Resources — a skill directory may also contain scripts/, references/,
     and assets/ subdirectories. Use /read to bring any of those files into
     context when the skill's instructions call for them.


SKILL DIRECTORY STRUCTURE

  my-skill/
  ├── SKILL.md          required: metadata + instructions
  ├── scripts/          optional: runnable code
  ├── references/       optional: extra documentation
  └── assets/           optional: templates, data files


SKILL.md FORMAT

  ---
  name: my-skill
  description: One or two sentences on what this skill does and when to use it.
  license: Apache-2.0          (optional)
  compatibility: Requires git  (optional)
  metadata:                    (optional)
    author: you
    version: "1.0"
  ---

  # My Skill

  Instructions in plain Markdown. The model reads this entire block
  when the skill is activated.

Required frontmatter fields: name, description.
The name must be lowercase letters, numbers, and hyphens only, and must
match the parent directory name.


EXAMPLE — the bundled go-review skill

  Location: .harvey/skills/go-review/SKILL.md

  ---
  name: go-review
  description: Review Go source code for correctness, style, and idiomatic
    patterns. Use when the user asks to review, audit, or critique Go code,
    or when checking a Go file before committing.
  license: AGPL-3.0
  compatibility: Designed for Harvey (or any agent working in a Go codebase)
  metadata:
    author: rsdoiel
    version: "1.0"
  ---

  Activate it with:   /skill load go-review
  Then ask Harvey:    Please review cmd/harvey/main.go


DISCOVERY PATHS  (project overrides user on name collision)

  User scope
    ~/.harvey/skills/          Harvey-native
    ~/.agents/skills/          cross-client (shared with Claude Code, etc.)

  Project scope  (relative to --workdir, default ".")
    .harvey/skills/            Harvey-native
    .agents/skills/            cross-client

Skills placed in .agents/skills/ are visible to any agent that follows
the Agent Skills specification (https://agentskills.dev).


SLASH COMMANDS

  /skill                   list all discovered skills
  /skill list              same as above
  /skill load NAME         inject the full skill body into context
  /skill info NAME         show path, compatibility, and license
  /skill status            count skills by scope

`

	HelpText = `%{app_name}(1) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

{app_name}

# SYNOPSIS

{app_name} [OPTIONS] 

# DESCRIPTION

{app_name} is a terminal agent for local large language models and optionally
for publicai.co. It was inspired by Claude Code but focused on working with
large language models in small computer environments like a Raspberry Pi
computer running Raspberry Pi OS. While the inspiration was to run an
agent locally with Ollama it can also be run on larger computers like
Linux, macOS and Windows systems you find on desktop and laptop computers.
It should compile it for most systems where Ollama is avialable and Go 
is supported (exmample *BSD).

{app_name} looks for HARVEY.md in the current directory and uses it as a
system prompt. It then connects to a local Ollama server or publicai.co
and starts an interactive chat session.

All file I/O is constrained to the workspace directory (--workdir or ".").
A knowledge base is stored at <workdir>/.harvey/knowledge.db and is created
automatically on first run.

Type /help inside the session for available slash commands.

# OPTIONS

-h, --help
: display this help message

-v, --version
: display version information

-l, --license
: display license information

-m, --model
: MODEL   Ollama model to use on startup

--ollama URL
: Ollama base URL (default: http://localhost:11434)
-w, --workdir DIR
: workspace directory (default: current directory)

-r, --record
: start a Fountain recording automatically at startup

--record-file FILE
: path for the auto-recording file (implies --record)

-s, --session ID
: resume a specific session by ID on startup; omit to be prompted

# ENVIRONMENT

PUBLICAI_API_KEY    API key for publicai.co

`
)
