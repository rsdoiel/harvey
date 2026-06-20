%harvey(7) user manual | version 0.0.13 00feb2f
% R. S. Doiel
% 2026-06-19

# NAME

MODEL — backend-agnostic model management and inline switching

# SYNOPSIS

/model [list|use NAME|show [NAME]|status]

@NAME [prompt...]

# DESCRIPTION

The /model command manages models across all backends (llamafile and Ollama)
using a single consistent interface. Use it when you don't want to remember
which backend is active.

The @NAME prefix switches the active model inline, within the current prompt.
History is preserved — the switch is like a new character entering the scene.

# SUBCOMMANDS

  /model
  /model show [NAME]
    Print the currently active model and backend. If NAME is given, show
    details for that model.

  /model list
    List all registered models across llamafile and Ollama, marking the
    active entry with an arrow.

  /model use NAME
    Switch to the named model. Harvey checks llamafile models first, then
    Ollama models. Equivalent to /llamafile use or /ollama use depending on
    where NAME is registered.

  /model status
    Show whether the active backend is reachable.

# AT-MENTION SWITCHING

Prefix any prompt with @NAME to switch to that model for this turn and all
subsequent turns:

  @phi-mini summarise this in under 100 words

If NAME is not recognised, the @ prefix is treated as part of the prompt and
no switch occurs. History is never cleared on a switch — the new model sees
the full conversation so far.

Switch notes are written to the session file:
  [[model switch: phi-mini (llamafile) at 2026-06-20 14:32:11]]

# MODEL ALIASES

  /model alias list
    List all defined short-name aliases.

  /model alias add ALIAS FULL_NAME
    Define an alias (also accepts: set).

  /model alias remove ALIAS
    Remove an alias (also accepts: rm, delete).

Aliases are resolved in @NAME switching and /model use.

# SEE ALSO

  harvey-llamafile(7), harvey-ollama(7), harvey-routing(7)

