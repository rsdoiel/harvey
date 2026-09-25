%harvey(7) user manual | version 0.0.13 7ec384f
% R. S. Doiel
% 2026-06-19

# NAME

INSPECT — show detailed Ollama model information

# SYNOPSIS

/inspect
/inspect MODEL

# DESCRIPTION

/inspect queries the local Ollama server for detailed information about
installed models. Requires an Ollama backend; use /model use to pick an
Ollama model, and run "ollama serve" in a shell if Ollama is not running.

Without a MODEL argument, /inspect shows a summary table of all installed
models: name, disk size, family, context length, and capability flags
(tools, embed, tagged-blocks).

With a MODEL argument, /inspect shows the full detail view for that model:
family, parameter count, quantization level, disk size, context length,
and all Modelfile parameter lines (e.g. temperature, system prompt).

# EXAMPLES

Summary of all installed models:

~~~
  harvey > /inspect
~~~

Detail view for a specific model:

~~~
  harvey > /inspect gemma4:e2b
  Model:        gemma4:e2b [loaded]
  Family:       gemma4
  Parameters:   2.5B
  Quantization: Q4_K_M
  Context:      131072 tokens
  Disk size:    1.7 GiB

  Modelfile parameters:
    stop "<end_of_turn>"
~~~

# SEE ALSO

  /model list         — models across llamafile, llama.cpp, and Ollama
  /model use NAME     — switch models (an Ollama model is probed on first alias)
  ollama show MODEL   — raw Modelfile, run in a shell
  /help model

