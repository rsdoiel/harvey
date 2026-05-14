%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

MODEL ALIAS — manage short names for Ollama model identifiers

# SYNOPSIS

/model alias [list]
/model alias set ALIAS FULL_MODEL_NAME
/model alias remove ALIAS

# DESCRIPTION

Model aliases let you use short, memorable names instead of full Ollama model
identifiers. Aliases are stored in agents/harvey.yaml under model_aliases: and
persist across sessions.

When you switch models with /model ALIAS, Harvey resolves the alias to the full
identifier before querying Ollama. The full name is always recorded in Fountain
session headers so a session can be resumed with the correct model.

Aliases are case-insensitive. The full model name is stored as-is.

# EXAMPLES

Define an alias:

  /model alias set qwen-coder qwen2.5-coder:7b
  /model alias set granite ibm/granite4.1:3b

Use the alias to switch models:

  /model qwen-coder

List all aliases:

  /model alias list

Remove an alias:

  /model alias remove qwen-coder

# SEE ALSO

  /model       — list models or switch to a named model
  /ollama list — show installed Ollama models with capabilities

