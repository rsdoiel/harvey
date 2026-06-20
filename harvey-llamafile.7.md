%harvey(7) user manual | version 0.0.14
% R. S. Doiel
% 2026-06-20

# NAME

LLAMAFILE COMMANDS

# SYNOPSIS

/llamafile SUBCOMMAND [ARGS...]

# DESCRIPTION

The /llamafile command manages llamafile model backends. A llamafile is a
self-contained executable that bundles a GGUF model and an HTTP inference
server into a single file — no separate server installation required.

Llamafile is a project by Mozilla. Learn more at:
  <https://github.com/mozilla-ai/llamafile>

Harvey scans $HOME/Models for .llamafile executables at startup and connects
automatically when an active model is registered. Use /llamafile add to
register a model for the first time.

# SUBCOMMANDS

  /llamafile add [PATH] [NAME]
    Register a model and connect to it immediately. If PATH is omitted,
    Harvey scans the discovery directory ($HOME/Models by default) and
    shows a numbered picker. NAME is derived from the filename if not
    given. When a llamafile server is already running, Harvey offers to
    adopt it rather than failing.
    The choice is saved to agents/harvey.yaml so Harvey connects
    automatically on next start.

  /llamafile use [NAME]
    Switch to a named registered model. If NAME is omitted, Harvey shows
    a numbered picker of registered models. The current server is stopped
    (if Harvey started it) and the new one is launched.

  /llamafile list
    List all registered models. The active model is marked with an arrow.
    The discovery directory is shown at the bottom.

  /llamafile start [NAME]
    Start the active (or named) model's server without changing the
    active setting. Useful after Harvey restarts.

  /llamafile status
    Show the active model, API URL, reachability, process ownership,
    discovery directory, and number of registered models.

  /llamafile remove NAME
  /llamafile drop NAME
    Remove a named model from the registry. `remove` and `drop` are
    equivalent; `remove` is the preferred form.

  /llamafile download
    Print a curated table of recommended models with file sizes and
    download guidance. No network access is performed — use the printed
    URL to download the file, then run /llamafile add.

# CONFIGURATION

In agents/harvey.yaml:

  llamafile:
    models_dir: ~/Models           # optional; $HOME/Models is the default
    active: qwen-coding
    url: http://localhost:8080     # optional; this is the default
    gpu_layers: 99                 # optional; layers to offload to GPU via -ngl
                                   # default 99 maximises Metal/CUDA offload
                                   # set to -1 to force CPU-only inference
    startup_timeout: 120s          # optional; time to wait for server ready
                                   # default is 120 seconds
    models:
      - name: qwen-coding
        path: /home/user/Models/Qwen3.5-4B-Q5_K_S.llamafile
        context_length: 16384      # optional; probed automatically at startup
      - name: phi-mini
        path: /home/user/Models/Phi-3.5-mini-instruct-Q4_K_M.llamafile

The context_length field sets the model's context window in tokens. When
omitted, Harvey probes the server after startup and stores the value in
memory. Set it explicitly to override or to make the [ctx: N%] indicator
accurate before the first probe.

# ENVIRONMENT

  HARVEY_LLAMAFILE_DIR
    Override the discovery directory. Takes precedence over the YAML
    value but is itself overridden by the --llamafile-dir flag.

# COMMAND LINE FLAGS

  --llamafile PATH        Connect to PATH for this session (not persisted).
  --llamafile-url URL     Override the API base URL (default: http://localhost:8080).
  --llamafile-dir PATH    Override the discovery directory.

# NOTES

macOS: llamafile binaries use the APE (Actually Portable Executable) format,
which macOS cannot exec directly via execve. Harvey launches them via
/bin/sh, which mirrors what the terminal does when you double-click or
run the file directly. No extra setup is required.

GPU offload: on macOS (Apple Silicon) llamafile uses Metal; on Linux it
uses CUDA or ROCm when available. The gpu_layers config option controls
how many transformer layers are offloaded to the GPU. The default of 99
offloads everything that fits; lower it if you run out of VRAM, or set
it to -1 for CPU-only inference.

Startup: Harvey waits up to startup_timeout for the llamafile HTTP server
to become ready. If the process exits before the server responds, Harvey
reports the error and prints any stderr output to help diagnose the
failure.

Auto-reconnect: if the llamafile server stops unexpectedly during a session,
Harvey detects the connection error on the next prompt and offers to
restart the server automatically before retrying the turn.

External servers: if a llamafile server is already running when Harvey
starts (or when you run /llamafile add), Harvey probes /v1/models to
identify the running model and offers to adopt it as the active model
without stopping and restarting the server.

# SEE ALSO

  harvey-ollama(7)       — Ollama model backend management
  harvey-model-alias(7)  — @mention switching and model aliases
  harvey-routing(7)      — add a llamafile server as a named route
  harvey-getting-started(7) — getting started with Harvey
