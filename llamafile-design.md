# Harvey Llamafile Integration — Design

> **Revision history**
> - v1: single `LlamafilePath`; `--llamafile` CLI flag as primary interface.
> - v2: named model registry; `/llamafile` command family replaces CLI flag as
>   primary interface; binaries stored anywhere on the filesystem.
> - v3: `models_dir` discovery path added; `/llamafile add` gains an
>   interactive picker when invoked with no path argument.

## Overview

Harvey currently requires a running Ollama server to function. From the
user's perspective this means two separate programs must be installed and
running before a single session can start. This design adds native support
for [Llamafile](https://github.com/Mozilla-Ocho/llamafile) as an alternative
backend — a self-contained executable that bundles a GGUF model and an HTTP
inference server into a single file.

The user story:

1. Install Harvey.
2. Pick a llamafile from the pre-built list at
   `https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles`.
3. Download it to `~/Models` (or anywhere).
4. Start Harvey — on the first run with no backend, Harvey hints at the above
   URL and the `/llamafile add` command.
5. Type `/llamafile add` — Harvey scans `~/Models`, shows a numbered picker,
   starts the selected server, and saves the choice.
6. On every subsequent start, Harvey connects automatically.

## Background: Llamafile's API Surface

Earlier investigation concluded that llamafile could not be integrated with
Harvey because it exposed only an interactive chat interface. That conclusion
is now outdated. Llamafile v0.10.x exposes a full OpenAI-compatible HTTP API
at `http://localhost:8080/v1`, including:

| Capability | Status |
|---|---|
| `/v1/chat/completions` (streaming) | Yes |
| `/v1/embeddings` | Yes |
| Tool / function calling | Yes |
| `/v1/models` | Yes |
| Multimodal (image input) | Yes, model-dependent |

Harvey already depends on `github.com/mozilla-ai/any-llm-go`, which ships a
`providers/llamafile` package wrapping this API. The client-side wiring
(`newLlamafileLLMClient` in `anyllm_client.go`) is already present.

## Key Design Decisions

### Binary storage: anywhere on the filesystem

Llamafile binaries are system resources, not workspace resources. A user
will likely maintain one set of llamafile binaries and use them across
multiple Harvey workspaces. Paths registered with `/llamafile add` may be
absolute paths anywhere on the filesystem. Workspace-relative paths are also
accepted and resolved against `a.Workspace.Root`.

The registry itself (in `agents/harvey.yaml`) is per-workspace, allowing
different workspaces to use different active models.

### `models_dir`: a discovery path, not a requirement

A `models_dir` setting tells Harvey where to look for `.llamafile` binaries.
It is used only for the interactive picker in `/llamafile add` — Harvey never
auto-registers models from this directory without user action.

**Default**: `~/Models` (`$HOME/Models`). If the directory does not exist,
the picker is simply unavailable; Harvey does not error. Users who download
llamafiles to the default location get the picker immediately; users who keep
files elsewhere can still use `/llamafile add /full/path/to/model.llamafile`.

**Override chain** (highest to lowest priority):
1. `--llamafile-dir PATH` CLI flag
2. `HARVEY_LLAMAFILE_DIR` environment variable
3. `llamafile.models_dir` in `agents/harvey.yaml`
4. Default: `$HOME/Models`

### Named model registry

Users may have multiple llamafile models for different tasks (e.g. a small
fast model for coding, a larger model for analysis). The config holds a
named registry rather than a single path.

### Single port constraint

Llamafile is single-process per port. Switching models means stopping the
current server and starting the new one. Harvey manages this transition via
a stored `*os.Process`.

### `/llamafile` command family as the primary interface

CLI flags (`--llamafile`, `--llamafile-url`, `--llamafile-dir`) are retained
for scripted use but are not the recommended first-run path. A `/llamafile`
REPL command is more discoverable for new users.

## What Changes

### 1. Configuration

**Removed** from `Config`:
```go
LlamafilePath string
```

**Added** to `Config`:
```go
LlamafileModels   []LlamafileEntry // registered models
LlamafileActive   string           // name of the active model; "" = none
LlamafileURL      string           // API base URL; default "http://localhost:8080"
LlamafileModelsDir string          // discovery path; default "$HOME/Models"
```

New type:
```go
type LlamafileEntry struct {
    Name string // short identifier, e.g. "qwen-coding"
    Path string // path to binary; absolute or workspace-relative
}
```

`harvey.yaml` representation:
```yaml
llamafile:
  models_dir: ~/Models          # optional; $HOME/Models is the default
  active: qwen-coding
  url: http://localhost:8080    # optional; this is the default
  models:
    - name: qwen-coding
      path: /home/user/Models/Qwen3.5-4B-Q5_K_S.llamafile
    - name: apertus
      path: /home/user/Models/Apertus-8B-Instruct-2509.llamafile
```

`~` in `models_dir` is expanded to `os.UserHomeDir()` at load time.

**Override chain** for `LlamafileModelsDir` is applied in `LoadHarveyYAML`
and at CLI flag parsing time, with the environment variable checked between
them. The default is applied in `DefaultConfig`.

### 2. The `/llamafile` command family

```
/llamafile list              — list registered models; mark active
/llamafile add [PATH] [NAME] — register a model; picker shown when PATH omitted
/llamafile use NAME          — switch to a named model
/llamafile start [NAME]      — start the active (or named) model's server
/llamafile status            — show current connection info
```

**`/llamafile add [PATH] [NAME]`**

*With PATH*: register the model at that path. Name is derived from filename
if omitted. Starts the server, connects, saves to `harvey.yaml`.

*Without PATH*: scan `LlamafileModelsDir` for `*.llamafile` files, show a
numbered picker, then proceed as if the user had supplied the selected path.
Already-registered models are shown with a `(registered)` marker so the user
can re-register or skip them. If `models_dir` does not exist or contains no
`.llamafile` files, print a helpful message and prompt for a path instead:

```
  No llamafiles found in ~/Models.
  Enter a path to a llamafile: _
```

**`/llamafile use NAME`**

Look up NAME in the registry. Stop the current server (if Harvey started
it), start the new one, connect `a.Client`, update `LlamafileActive`, save
to `harvey.yaml`.

**`/llamafile list`**

Print a table with name, path, file size, and an arrow marking the active
model:
```
  Registered llamafile models:
  → qwen-coding   ~/Models/Qwen3.5-4B-Q5_K_S.llamafile       (4.1 GB)
    apertus       ~/Models/Apertus-8B-Instruct-2509.llamafile  (5.9 GB)
```

**`/llamafile start [NAME]`**

Start the active (or named) model's server without changing the active
setting. Useful after Harvey restarts when the server is no longer running.

**`/llamafile status`**

Show: active model name (or "none"), API URL, reachable yes/no, models_dir,
number of registered models.

### 3. Startup sequence (`terminal.go`)

When `LlamafileActive` is set and matches a registry entry, Harvey probes and
auto-starts as before. The "no backend" message in the Ollama-failure path
gains the llamafile hint:

```
  No backend selected.
  → If Ollama is installed, use /ollama start once inside.
  → Or pick a llamafile from:
      https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles
    Download it to ~/Models, then run /llamafile add to connect.
```

When no active model is configured, Harvey falls through to Ollama exactly
as today.

### 4. Process tracking

`Agent` gains a `llamafileProc *os.Process` field. `StartLlamafileService`
returns `(*os.Process, error)`. On clean exit, Harvey signals the process.

### 5. CLI flags

| Flag | Env var | Config field | Behaviour |
|---|---|---|---|
| `--llamafile PATH` | — | session-only | Connect to PATH for this session only; does not persist |
| `--llamafile-url URL` | — | `LlamafileURL` | Override API base URL |
| `--llamafile-dir PATH` | `HARVEY_LLAMAFILE_DIR` | `LlamafileModelsDir` | Override discovery directory |

`--llamafile PATH` is a session-only convenience; it does not write to
`harvey.yaml`. To persist a model, use `/llamafile add` inside the REPL.

## User Experience

**No backend, first run**
```
  ✗ Ollama is not running
    Start Ollama now? [Y/n] n

  No backend selected.
  → If Ollama is installed, use /ollama start once inside.
  → Or pick a llamafile from:
      https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles
    Download it to ~/Models, then run /llamafile add to connect.
```

**After downloading a model to ~/Models**
```
harvey> /llamafile add
  Llamafiles found in ~/Models:
  [1] Qwen3.5-4B-Q5_K_S.llamafile       (4.1 GB)
  [2] Apertus-8B-Instruct-2509.llamafile  (5.9 GB)
  Select [1-2] or enter a path: 1
  Starting llamafile...
  ✓ Using model: Qwen3.5-4B-Q5_K_S
  Saved to agents/harvey.yaml — Harvey will connect automatically on next start.
```

**Registering a second model**
```
harvey> /llamafile add
  Llamafiles found in ~/Models:
  [1] Qwen3.5-4B-Q5_K_S.llamafile       (4.1 GB)  (registered as qwen-coding)
  [2] Apertus-8B-Instruct-2509.llamafile  (5.9 GB)
  Select [1-2] or enter a path: 2
  Name [Apertus-8B-Instruct-2509]: apertus
  Registered apertus — use /llamafile use apertus to switch.
```

**Switching models mid-session**
```
harvey> /llamafile use apertus
  Stopping Qwen3.5-4B-Q5_K_S...
  Starting apertus...
  ✓ Using model: apertus
```

**Subsequent start (active model saved)**
```
  Checking llamafile (qwen-coding) at http://localhost:8080...
  ✗ Llamafile is not running
    Start qwen-coding now? [Y/n] y
  Starting llamafile...
  ✓ Using model: qwen-coding
```

## Rejected Alternatives

**Auto-register all `.llamafile` files in `models_dir` on startup**: too
implicit. Harvey would add models to the registry without user action, which
is surprising and makes it hard to reason about what is configured. The
picker preserves user intent.

**Tab-completion for file paths**: requires REPL changes to the terminal
library. The numbered picker gives the same benefit (no need to remember
filenames) with no library changes and is friendlier for new users.

**Single default `$HOME/Models` with no override**: advanced users need to
keep binaries on a separate drive or in a shared network location. The
three-level override chain (CLI flag → env var → YAML → default) follows
Harvey's existing pattern for other configurable paths.

**Storing models inside the workspace (`agents/models/`)**: rejected because
users run Harvey across multiple workspaces and should not need to duplicate
or symlink large binary files.

## Out of Scope

- `/llamafile remove NAME` — delete a registry entry (edit `harvey.yaml` directly).
- `/llamafile rename OLD NEW` — rename a registry entry.
- Multiple simultaneous llamafile servers on different ports.
- Automatic download of a recommended llamafile binary.
- Llamafile embedding support for RAG.
- Recursive scan of `models_dir` subdirectories.
