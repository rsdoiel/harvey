%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

INSPECT — show detailed Ollama model information

# SYNOPSIS

/inspect
/inspect MODEL

# DESCRIPTION

/inspect queries the local Ollama server for detailed information about
installed models. Requires an Ollama backend; use /ollama start first if
Ollama is not running.

Without a MODEL argument, /inspect shows a summary table of all installed
models: name, disk size, family, context length, and capability flags
(tools, embed, tagged-blocks). This is identical to /ollama list.

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

  /ollama list        — model table (same as /inspect with no args)
  /ollama probe       — test and cache capability flags
  /ollama show MODEL  — raw Modelfile via the ollama CLI
  /help ollama

