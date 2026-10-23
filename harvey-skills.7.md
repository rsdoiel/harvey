%harvey(7) user manual | version 0.0.20 cd480d8
% R. S. Doiel
% 2026-10-20

# NAME

SKILLS

# SYNOPSIS

Skills allow agents to carry out uniform structured tasks. The SKILL.md file
is a standard proposed by Anthropic.

# DESCRIPTION

Skills are Markdown files that inject specialised instructions into Harvey's
context on demand. Harvey scans for skills at startup but only loads a
skill's full instructions when you explicitly ask for it. SKILL.md
is documented at <https://agentskills.io/home>.


# HOW SKILLS WORK

  1. Discovery — Harvey scans the standard paths below and builds a catalog
     of (name, description) pairs. The catalog is kept out of the system prompt
     by default, since the model cannot load a skill itself and the catalog
     costs tokens on every turn. To put it in the prompt, set
     skills: catalog: full in agents/harvey.yaml.

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

  Location: agents/skills/go-review/SKILL.md

~~~markdown
  ---
  name: go-review
  description: Review Go source code for correctness, style, and idiomatic
    patterns. Use when the user asks to review, audit, or critique Go code,
    or when checking a Go file before committing.
  license: AGPL-3.0
  compatibility: Designed for Harvey (or any agent working in a Go codebase)
  metadata:
    author: you
    version: "1.0"
  ---

  Activate it with:   /skill load go-review
  Then ask Harvey:    Please review cmd/harvey/main.go
~~~

# USING SKILLS

A skill is used directly. Activating one puts its SKILL.md body into the
conversation and the model answers with that guidance in context. There are
three ways to activate a skill:

  /skill load NAME     load the skill, then type your request
  /NAME [REQUEST]      load the skill; REQUEST, if given, is sent to the model
                       as your next message
  trigger field        load the skill automatically when your message matches

Skills are not compiled to Bash or PowerShell, and a scripts/compiled.bash or
scripts/compiled.ps1 left in a skill directory from an earlier release is
ignored: it is never run. A skill that needs a script should say so in its body
and the model runs it with the usual tools.

TRIGGER field: add an optional trigger field to SKILL.md frontmatter to load
the skill automatically when user input matches:

  trigger: pdf extract document   (keyword mode — any word triggers)
  trigger: /\bpdf\b/              (regexp mode — wrap pattern in slashes)

When Harvey receives a user prompt matching a trigger, it loads the skill and
then sends the prompt to the model. First alphabetically matching trigger wins.


## DISCOVERY PATHS  (project overrides user on name collision)

~~~
  Project scope
    <workspace>/agents/skills/           Harvey-native (and shared clients)
~~~

Skills placed in agents/skills/ are visible to any agent
that follows the Agent Skills specification (https://agentskills.io/home).


# SLASH COMMANDS

~~~
  /skill                        list all discovered skills
  /skill list                   same as above
  /skill load NAME              inject the full skill body into context
  /skill show NAME              show path, compatibility, and license (alias: info)
  /skill info NAME              alias for show
  /skill status                 count skills by scope
  /skill new                    interactive wizard to create a new skill
  /NAME [REQUEST]               load the skill NAME; REQUEST is sent to the model
  /skill suggest [SESSION]      mine a session recording for reusable workflow
                                candidates; for each accepted candidate, writes a
                                SKILL.md stub to agents/skills/<name>/. SESSION is
                                a path to a .spmd session file; omit to use the
                                most recent session in agents/sessions/.
~~~

# CREATING SKILLS FROM SESSIONS

/skill suggest analyses a session transcript with the active LLM and proposes
multi-step workflows that appear in the session as skill candidates. Only
workflows with at least three distinct repeatable steps are proposed.

For each proposed skill you can:
  y — accept and write agents/skills/<name>/SKILL.md immediately
  n — skip this candidate
  q — stop the review loop

Accepted skills are written to the live agents/skills/ directory and are
immediately available via /skill load. Review the generated SKILL.md and
refine the instructions before committing it.

# SEE ALSO

  /skill-set load NAME   — load a named bundle of skills
  /help skill-set        — skill bundle management

