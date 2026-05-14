%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

SKILL-SET — load and manage named bundles of Harvey skills

# SYNOPSIS

/skill-set <list|load NAME|info NAME|create NAME|status|unload>

# DESCRIPTION

/skill-set groups multiple skills into a named YAML bundle stored in
agents/skill-sets/. Loading a bundle injects every skill in the bundle
into the current conversation context in one step.

Skill-set YAML files live in agents/skill-sets/ and reference skills by
the name field in their SKILL.md frontmatter (e.g. "fountain-analysis").

# SUBCOMMANDS

list
  List all YAML files found in agents/skill-sets/.

load NAME
  Parse NAME.yaml, validate every skill exists in agents/skills/, count
  tokens for the combined bodies, and load each skill into context. Warns
  when combined tokens exceed 50 % of the active context window; errors
  when they exceed 100 %.

info NAME
  Show the skill-set description and the skills it contains without loading.

create NAME
  Scaffold a new NAME.yaml in agents/skill-sets/ with placeholder content.

status
  Show the currently loaded skill-set (if any).

unload
  Clear the active skill-set indicator. The injected context remains in
  history; use /clear if you need a clean slate.

# YAML FORMAT

  name: go-dev
  description: |
    Skills for Go development sessions.
  skills:
    - fountain-analysis
    - review-knowledge-base
  metadata:
    version: "1.0"
    author: "R. S. Doiel"

# EXAMPLES

List available bundles:

  /skill-set list

Load the fountain bundle:

  /skill-set load fountain

Show bundle contents without loading:

  /skill-set info fountain

Check what is active:

  /skill-set status

Create a new bundle:

  /skill-set create my-bundle

# SEE ALSO

  /skill load NAME — load a single skill
  /skill list      — list individual skills
  /help skills     — skills system overview

