%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

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

  /ollama start [debug]
    Launch ollama serve in the background. If Ollama is already running,
    prints a warning and exits. Pass debug to also set OLLAMA_DEBUG=1
    in the Ollama process; output is captured to agents/logs/ollama-TIMESTAMP.log.
    Note: OLLAMA_DEBUG is inherited from Harvey's process — start Harvey with
    --debug for full diagnostic coverage.

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

Capability probing:

  /ollama probe [MODEL]
    Run a thorough probe on MODEL (or on all not-yet-probed models when
    MODEL is omitted). Detects tool-calling support, embedding capability,
    and whether the model reliably emits path-tagged code blocks (the
    format Harvey's auto-execute relies on). Results are cached in
    harvey/model_cache.db so /ollama list can display them immediately.

  /ollama probe-all
    Re-probe every model currently installed on the local Ollama server,
    refreshing cached capability data. Useful after pulling several new
    models or when moving between machines with different model sets.
    Equivalent to /ollama probe --all.

Model aliases:

  /ollama alias NAME FULLNAME
    Create a short alias for a long model name. Equivalent to
    /model alias set NAME FULLNAME.

  /ollama alias list
    List all defined model aliases.

  /ollama alias remove NAME
    Remove an alias. Equivalent to /model alias remove NAME.

  See also: /help model-alias

