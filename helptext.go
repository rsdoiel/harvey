package harvey

var (
	// SkillsHelpText explains the Agent Skills feature and is displayed by
	// /help skills (REPL) or harvey --help skills (CLI).
	SkillsHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SKILLS

# SYNOPSIS

Skills allow agents to cary out uniform structured tasks. The SKILL.md file
is a standard proposed by Anthoropic.

# DESCRIPTION

Skills are Markdown files that inject specialised instructions into Harvey's
context on demand. Harvey scans for skills at startup but only loads a
skill's full instructions when you explicitly ask for it. SKILL.md
is document at <https://agentskills.io/home>.


# HOW SKILLS WORK

  1. Discovery — Harvey scans the standard paths below and builds a catalog
     of (name, description) pairs. The catalog is added to the system prompt
     so the model knows what skills are available.

  2. Activation — type /skill load <name> to inject the full skill body into
     the conversation. The model then follows the skill's instructions for
     the current task.

  3. Resources — a skill directory may also contain scripts/, references/,
     and assets/ subdirectories. Use /read to bring any of those files into
     context when the skill's instructions call for them.


# SKILL DIRECTORY STRUCTURE

~~~
  my-skill/
  ├── SKILL.md          required: metadata + instructions
  ├── scripts/          optional: runnable code
  ├── references/       optional: extra documentation
  └── assets/           optional: templates, data files
~~~


# SKILL.md FORMAT

~~~markdown
  ---
  name: my-skill
  description: One or two sentences on what this skill does and when to use it.
  license: Apache-2.0          (optional)
  compatibility: Requires git  (optional)
  trigger: pdf extract         (optional: keyword list or /regexp/)
  metadata:                    (optional)
    author: you
    version: "1.0"
  ---

  # My Skill

  Instructions in plain Markdown. The model reads this entire block
  when the skill is activated.

~~~

Required frontmatter fields: name, description.
The name must be lowercase letters, numbers, and hyphens only, and must
match the parent directory name.


# EXAMPLE — the bundled go-review skill

  Location: .harvey/skills/go-review/SKILL.md

~~~markdown
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
~~~

# COMPILED SKILLS

Harvey can ask the LLM to "compile" a SKILL.md into executable scripts
(compiled.bash for Linux/macOS/BSD, compiled.ps1 for Windows) stored in the
skill's scripts/ directory. When a compiled skill is triggered, Harvey runs the
script directly — no LLM round-trip needed — and injects the output into context.

Compiled skill directory layout:

~~~
  my-skill/
  ├── SKILL.md
  └── scripts/
      ├── compiled.bash
      └── compiled.ps1
~~~

HARVEY_* environment variables set before each script run:

  HARVEY_PROMPT      the user's exact prompt text
  HARVEY_WORKDIR     absolute path to the workspace root
  HARVEY_MODEL       the name of the currently active LLM model
  HARVEY_SESSION_ID  the current session ID as a string

Staleness: if SKILL.md is modified after compiling, Harvey detects that the
scripts are older and prompts to recompile before running.

TRIGGER field: add an optional trigger field to SKILL.md frontmatter to enable
automatic skill dispatch when user input matches:

  trigger: pdf extract document   (keyword mode — any word triggers)
  trigger: /\bpdf\b/              (regexp mode — wrap pattern in slashes)

When Harvey receives a user prompt matching a trigger, it invokes the compiled
skill directly instead of sending the prompt to the LLM. First alphabetically
matching trigger wins.


## DISCOVERY PATHS  (project overrides user on name collision)

~~~
  User scope
    ~/.harvey/skills/          Harvey-native
    ~/.agents/skills/          cross-client (shared with Claude Code, etc.)

  Project scope  (relative to --workdir, default ".")
    .harvey/skills/            Harvey-native
    .agents/skills/            cross-client
~~~

Skills placed in .agents/skills/ are visible to any agent that follows
the Agent Skills specification (https://agentskills.dev).


# SLASH COMMANDS

~~~
  /skill                   list all discovered skills
  /skill list              same as above
  /skill load NAME         inject the full skill body into context
  /skill info NAME         show path, compatibility, and license
  /skill status            count skills by scope
  /skill new               interactive wizard to create a new skill
  /skill compile NAME      send SKILL.md to the LLM to generate compiled scripts
  /skill run NAME          run a skill (dispatches compiled scripts if available)
~~~

`

	RoutingHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

ROUTING

# SYNOPSIS

/route FAST_MODEL FULL_MODEL

# DESCRIPTION

Multi-model routing lets Harvey automatically choose between two Ollama models
on every turn: a small, fast model for simple requests and a larger, more
capable model for complex ones. When routing is active, each prompt is first
classified by the fast model. If the fast model decides the task exceeds its
capability it replies with "ROUTE:full" and Harvey re-sends the full
conversation to the full model. Otherwise the fast model's answer is used
directly, saving the time and resources of a larger model call.

# HOW IT WORKS

For each user prompt Harvey:

  1. Sends a trimmed slice of conversation history to the fast model together
     with a routing system prompt that instructs it to either answer directly
     or reply with exactly "ROUTE:full".

  2. If the fast model replies with "ROUTE:full", Harvey switches its active
     client to the full model and sends the complete history there instead.

  3. If the fast model answers directly, that answer is used as-is — the full
     model is never called.

After the response a status line is printed showing which model(s) handled
the turn, token counts, elapsed time, and throughput:

~~~
  llama3.2:1b → Ollama (llama3.1:8b) · 312 reply + 1840 ctx · 18.4s · 16.9 tok/s
  llama3.2:1b · 28 reply + 94 ctx · 1.2s · 23.1 tok/s
~~~

The first line is an escalated turn (fast routed to full). The second is a
direct answer from the fast model. When routing is disabled there is only one
model name in the line.

# CONTEXT BUDGET

The fast model receives at most 25% of its context window so there is room for
its reply without overwhelming a small model. Recent non-system turns are
included newest-first until the budget is consumed. Older turns and the
Harvey system prompt are excluded from the routing call.

# WHEN TO USE ROUTING

Routing is most useful when the fast model is small enough to run at many
tokens-per-second but too limited for complex tasks like code generation or
architecture decisions. A good pairing on a Raspberry Pi 5 is:

  fast model  llama3.2:1b  — quick classification, conversational answers
  full model  llama3.1:8b  — code generation, debugging, longer analysis

# SLASH COMMANDS

~~~
  /route FAST FULL   enable routing between FAST and FULL Ollama models
                     example: /route llama3.2:1b llama3.1:8b
  /route off         disable routing and return to the current single model
  /route status      show the current routing configuration
~~~

`

	OllamaHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

OLLAMA COMMANDS

# SYNOPSIS

/ollama SUBCOMMAND [ARGS...]

# DESCRIPTION

The /ollama command controls the local Ollama service and manages models
from inside the Harvey REPL. Every subcommand maps directly to an ollama
CLI operation.

# SUBCOMMANDS

Service control:

  /ollama start
    Launch ollama serve in the background.

  /ollama stop
    Print a reminder to stop Ollama via your system's service manager
    (e.g. systemctl stop ollama). Harvey does not stop the daemon itself.

  /ollama status
    Check whether the Ollama daemon is reachable at the configured URL.

  /ollama logs
    Tail the Ollama service log. Tries ollama logs first, falls back
    to journalctl -u ollama.

  /ollama env
    Show the Ollama environment variables (OLLAMA_HOST, etc.) as seen
    by the Harvey process.

Model management:

  /ollama list
    List all installed models. The model currently in use is marked with *.

  /ollama ps
    Show which models are loaded in memory (delegates to ollama ps).

  /ollama pull MODEL
    Download a model from the Ollama registry (e.g. /ollama pull mistral).

  /ollama push MODEL
    Upload a model to the Ollama registry.

  /ollama show MODEL
    Display a model's Modelfile, parameters, and template.

  /ollama create NAME [-f MODELFILE]
    Create a new model from a Modelfile. Passes all arguments directly
    to ollama create.

  /ollama cp SOURCE DEST
    Copy an installed model to a new name.

  /ollama rm MODEL [MODEL...]
    Remove one or more installed models.

  /ollama use MODEL
    Switch the active model to MODEL for the current session without
    restarting Harvey.

  /ollama run MODEL [PROMPT]
    Launch an interactive ollama run session inside the terminal.
    stdin, stdout, and stderr are passed through directly.

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

--continue FILE
: load conversation history from a Fountain recording and open the REPL

--replay FILE
: re-send every user turn from FILE to the current model and record fresh responses

--replay-output FILE
: write replay responses to FILE (default: auto-named timestamped file; implies --replay)

-s, --session ID
: resume a specific session by ID on startup; omit to be prompted

# ENVIRONMENT

PUBLICAI_API_KEY    API key for publicai.co

`
)
