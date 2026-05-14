%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

MODEL — list installed models or switch the active model

# SYNOPSIS

/model
/model NAME
/model ollama://NAME
/model alias [list|set ALIAS NAME|remove ALIAS]

# DESCRIPTION

Without arguments, /model lists all models available across every reachable
backend (Ollama, routes), marking the currently active model with *.

With a NAME argument, /model switches the active model for the current
session. Harvey does not restart; the switch takes effect on the next
user turn.

NAME can be:
  - A bare Ollama model identifier: llama3.1:8b
  - An ollama:// URL: ollama://llama3.1:8b
  - A model alias defined with /model alias set

/model alias is a shorthand for managing short memorable names for long
Ollama model identifiers. See /help model-alias for full details.

# SWITCHING MODELS

~~~
  harvey > /model
  * gemma4:e2b        ...
    llama3.1:8b       ...
    qwen2.5-coder:7b  ...

  harvey > /model llama3.1:8b
  harvey > /model qwen-coder        (if alias defined)
  harvey > /model ollama://mistral:latest
~~~

When switching, Harvey replaces the backend client immediately. The
conversation history is preserved so you can compare responses from
different models within the same session.

# CAPABILITY PROBING

After pulling a new model, run /ollama probe MODEL to detect its tool-
calling, embedding, and tagged-block capabilities. Results are cached in
agents/model_cache.db and shown in /model and /ollama list.

# SEE ALSO

  /ollama list       — list installed Ollama models with capabilities
  /ollama probe      — probe and cache model capabilities
  /model alias set   — define a short alias for a long model name
  /help model-alias
  /help ollama

