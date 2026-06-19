%harvey(7) user manual | version 0.0.13 aa87def
% R. S. Doiel
% 2026-06-19

# NAME

LLAMAFILE COMMANDS

# SYNOPSIS

/llamafile SUBCOMMAND [ARGS...]

# DESCRIPTION

The /llamafile command manages llamafile model backends. A llamafile is a
self-contained executable that bundles a GGUF model and an HTTP inference
server into a single file — no separate server installation required.

Llamafile is a project by Mozilla that makes it easy to distribute and run
local LLMs on any platform. Learn more at:
  <https://github.com/mozilla-ai/llamafile>

Harvey assumes models are stored in $HOME/Models by default. Place
.llamafile executables there and use /llamafile add to register and
connect to them.

Pre-built models are available at:
  <https://huggingface.co/Mozilla/llamafile-models>

# SUBCOMMANDS

  /llamafile add [PATH] [NAME]
    Register a model and connect to it immediately. If PATH is omitted,
    Harvey scans the discovery directory ($HOME/Models by default) and
    shows a numbered picker. NAME is derived from the filename if not
    given. The choice is saved to agents/harvey.yaml so Harvey connects
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
      - name: apertus
        path: /home/user/Models/Apertus-8B-Instruct-2509.llamafile

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

# SEE ALSO

  /ollama              — Ollama model backend management
  /help routing        — add a llamafile:// server as a named route
  /help getting-started — Getting started with Harvey

