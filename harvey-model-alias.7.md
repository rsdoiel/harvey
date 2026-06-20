%harvey(7) user manual | version 0.0.14
% R. S. Doiel
% 2026-06-20

# NAME

MODEL ALIAS — inline model switching and short-name aliases

# SYNOPSIS

@NAME [prompt...]

/model alias [list|add ALIAS FULLNAME|set ALIAS FULLNAME|remove ALIAS]

# DESCRIPTION

Harvey supports two ways to work with multiple models in a session:

  @NAME syntax — switch the active model inline, as part of a prompt.
  /model alias — define short names for long model identifiers.

Both are preserved in the session recording as Fountain notes so the
memory miner can attribute turns to the correct model.

# AT-MENTION SWITCHING

Prefix any prompt with @NAME to switch to that model for this turn and
all subsequent turns:

  @phi-mini summarise this function in one sentence

  @qwen-coding rewrite the loop to avoid the allocation

If NAME matches a registered llamafile model, Harvey stops the current
server and starts the new one. If NAME matches an Ollama model, Harvey
switches the Ollama client. If NAME is not recognised, the @ prefix is
treated as part of the normal prompt — no error, no switch.

Conversation history is preserved unchanged across a switch. The model
switch is recorded in the session file as:

  [[model switch: NAME (BACKEND) at TIMESTAMP]]

The session title page also records the starting backend:

  Backend: llamafile

Use @NAME with no trailing text to switch model without sending a prompt:

  @phi-mini

# MODEL ALIASES

Aliases let you use short, memorable names for long model identifiers.
They are stored in agents/harvey.yaml under model_aliases: and persist
across sessions. Aliases resolve at the @NAME lookup step, so:

  /model alias add coder qwen2.5-coder:7b

  @coder tell me about this function

is equivalent to switching to qwen2.5-coder:7b.

# SUBCOMMANDS

  /model alias list
    List all defined aliases and their full model names.

  /model alias add ALIAS FULLNAME
    Define a new alias. FULLNAME is an Ollama model identifier or a
    registered llamafile name. `add` is the preferred form; `set` is
    also accepted for compatibility.

  /model alias set ALIAS FULLNAME
    Alias for `add`.

  /model alias remove ALIAS
    Remove an alias. Also accepted: rm, delete.

# SEE ALSO

  harvey-llamafile(7), harvey-ollama(7), harvey-routing(7),
  harvey-getting-started(7)
