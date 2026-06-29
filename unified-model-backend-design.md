# Harvey Unified Model Backend Design

**Date**: 2026-06-28  
**Status**: Design exploration — not yet approved  
**Related**: DECISIONS.md (2026-06-28 — Local model backend design deferred)  
**Supersedes**: [llamacpp-support.design.md](llamacpp-support.design.md) (as a direction, not a spec)

---

## Problem

Harvey currently supports three local inference backends. Each is implemented
independently, with different data structures, different command vocabularies,
and different process-tracking approaches:

| Backend | Process tracking | Config field | Command |
|---------|-----------------|--------------|---------|
| Ollama | `OllamaStartedByHarvey bool` | `OllamaURL` | `/ollama` |
| Llamafile | `llamafileProc *os.Process` | `LlamafileActive`, `LlamafileEntries` | `/llamafile` |
| llama.cpp | (not implemented) | (none) | (proposed `/llamacpp`) |

User-reported symptoms of the inconsistency:
- Ollama model list in `harvey.yaml` grows stale as models are added and removed
  outside Harvey.
- Llamafile lifecycle ("feels broken at times") — adoption of external servers,
  start/stop sequencing, and client wiring after switching models all have
  independent code paths that share no logic.
- Adding llama.cpp as a third independent backend would embed the same inconsistency
  a third time.

The goal is a unified `ManagedBackend` abstraction that all three backends
implement, so lifecycle management, client wiring, and command dispatch share
one path regardless of which backend is active.

---

## What the Backends Share

All three backends are HTTP servers that accept chat requests. The differences
are in how Harvey finds, starts, and manages them.

### Common lifecycle operations

| Operation | Ollama | Llamafile | llama.cpp |
|-----------|--------|-----------|-----------|
| Detect if running | `GET /api/tags` | `GET /v1/models` | `GET /v1/models` |
| Start | `ollama serve` subprocess | `.llamafile` executable subprocess | `llama-server --model` subprocess |
| Stop | kill subprocess (if Harvey started it) | `SIGINT` to `llamafileProc` | `SIGTERM` to subprocess |
| List models | `GET /api/tags` | scan `~/Models/*.llamafile` | scan `~/Models/*.gguf` |
| Switch model | `POST /api/generate` with new model name | stop + start new llamafile | stop + start with new `--model` |
| Adopt external | not implemented | `adoptExternalServer` | not implemented |
| Pull/download | `ollama pull MODEL` | external download (no Harvey support) | `huggingface-cli` or `wget` |

### Common data a backend must expose

```
BaseURL()      string        — e.g., "http://127.0.0.1:11434"
ActiveModel()  string        — currently loaded model name
IsRunning()    bool          — true when server is reachable
Name()         string        — "ollama", "llamafile", "llamacpp"
```

### Common operations a backend must support

```
Detect()                    — probe the server URL; return IsRunning()
Start(model string) error   — start the server subprocess with the given model
Stop() error                — stop the subprocess Harvey started
ListModels() []ModelSummary — enumerate available models
NewClient() LLMClient       — return a wired LLMClient for the active model
```

---

## Proposed Interface

```go
// ManagedBackend represents a local inference backend that Harvey can
// start, stop, and query. It is implemented by OllamaBackend,
// LlamafileBackend, and LlamaCppBackend.
type ManagedBackend interface {
    // Name returns the backend identifier: "ollama", "llamafile", or "llamacpp".
    Name() string

    // BaseURL returns the HTTP base URL of the server.
    BaseURL() string

    // ActiveModel returns the name of the currently loaded model, or "".
    ActiveModel() string

    // IsRunning reports whether the server is reachable.
    IsRunning() bool

    // Detect probes the server and returns true if reachable.
    Detect() bool

    // Start launches the server with the given model.
    // If the server is already running with a different model, it must be
    // stopped first. Calling Start when already running with the same model
    // is a no-op.
    Start(ctx context.Context, model string, out io.Writer) error

    // Stop stops the server process Harvey started.
    // Safe to call when not running or when Harvey did not start the server.
    Stop() error

    // ListModels returns the set of locally available models.
    ListModels() ([]ModelSummary, error)

    // NewClient returns a wired LLMClient pointing at the running server.
    // Returns an error if the server is not running.
    NewClient() (LLMClient, error)

    // StartedByHarvey reports whether this session's Harvey process started
    // the currently running server (as opposed to adopting an external one).
    StartedByHarvey() bool
}

// ModelSummary is a backend-neutral description of an available model.
type ModelSummary struct {
    Name     string    // display name
    Path     string    // filesystem path (empty for Ollama)
    SizeBytes int64    // model weight size; 0 if unknown
    Modified time.Time // last modified; zero if unknown
}
```

---

## Agent Integration

Replace the current ad-hoc fields with a single `ManagedBackend`:

```go
// Current (scattered):
OllamaStartedByHarvey bool
llamafileProc         *os.Process

// Proposed:
Backend ManagedBackend // nil when no backend is active
```

When `Backend` is set, `a.Client` is always obtained via `Backend.NewClient()`.
When the backend is stopped or switched, `a.Client` is replaced atomically.
This eliminates the current inconsistency where `a.Client` can diverge from
the backend state (e.g., after `/llamafile use` if the wiring code has a bug).

---

## Command Surface

**Decision (2026-06-29):** Option D — extend `/model` directly; no "backend"
namespace; engine-specific operations stay in their own namespaces.

`/model` is already the cross-backend entry point (`list`, `use`, `status`,
`mode` all exist). The unified design extends it with `stop` and promotes
`alias` from `/ollama alias` to `/model alias`. The word "backend" never
appears in the user-facing command surface.

### `/model` — unified command surface

```
/model                              — show active model and engine (existing)
/model use [NAME|ALIAS]             — primary command: see picker design below
/model stop                         — stop server Harvey started; delete PID file
/model status                       — probe all three engines; show active + status
/model list                         — list all models across all sources with engine
/model alias set NAME ID [--tags T] — register alias with optional purpose tags
/model alias tags NAME TAG [TAG...] — add tags to existing alias
/model alias list                   — list all aliases with engine + tags columns
/model alias remove NAME            — unregister alias
/model mode [MODEL] [MODE]          — set tool mode (existing, unchanged)
```

`use` and `start` are combined into `/model use`. Starting the server is a
side effect of using a file-based model, not a separate gesture.

### Unified picker — `/model use` with no argument

When called without a name, `/model use` builds a combined list from all
available sources and presents a single picker:

1. Scan `~/Models` for `.llamafile` files → engine: llamafile
2. Scan `~/Models` for `.gguf` files → engine: llama-server
3. Query Ollama `/api/tags` if Ollama is reachable → engine: ollama
   (silently omitted if Ollama is not installed or not running)
4. Present combined picker with a source/engine column
5. If the selection has no alias → prompt for short name + optional tags
   → save to `harvey.yaml` (lazy registration)
6. Start server if needed (file-based models); for Ollama, ensure the
   daemon is reachable
7. Wire `a.Client`; print confirmation

Lazy registration applies to all sources — a raw `.gguf` file, a
`.llamafile` binary, and an Ollama model all go through the same
name + tags prompt if they have no alias yet. This replaces the separate
`/llamafile add` and `/llamacpp add` commands; registration happens
at first use.

### Engine-specific commands

Operations with no cross-engine equivalent remain in their own namespaces:

```
/ollama pull MODEL      — pull a new model from the Ollama registry
/ollama rm MODEL        — remove an Ollama model from the daemon
/ollama probe           — health-check the Ollama server
/ollama clean           — prune stale aliases/config references

/llamafile drop NAME    — unregister a llamafile (does not delete the file)
/llamafile status       — engine-specific diagnostics

/llamacpp drop NAME     — unregister a GGUF model
/llamacpp status        — engine-specific diagnostics
```

`/llamafile add` and `/llamacpp add` are removed as primary commands;
the unified picker in `/model use` replaces them. `/ollama alias` is
removed; `/model alias` is the canonical alias management command.

---

## Process Persistence Across Sessions

`llamafileProc *os.Process` is nil when Harvey restarts, even if the server
Harvey started in the prior session is still running. `OllamaStartedByHarvey`
is always false on restart.

Neither backend has a reliable mechanism for Harvey to know on startup "I
started this server, so I should stop it on exit."

Proposed: write a small PID file to the workspace when Harvey starts a
backend process. On startup, read the PID file if present and probe the
PID (via `/proc/<PID>/cmdline` on Linux or `os.FindProcess`) to determine
if the process is still Harvey's server.

```
agents/.harvey-backend.pid      — JSON: {"backend":"llamafile","pid":12345,"model":"Qwen3.5-4B","url":"http://127.0.0.1:8080"}
```

On clean exit: delete the PID file.  
On next startup: read PID file, probe the process, offer to adopt if alive
or clean up if dead.

This replaces `OllamaStartedByHarvey bool` and `llamafileProc *os.Process`
with a persistent, session-crossing record.

---

## Known Backend-Specific Issues to Resolve

### Ollama

- Model list in `harvey.yaml` becomes stale as the user adds/removes models
  via `ollama pull`/`ollama rm` outside Harvey. Fix: `/ollama list` should
  always query the live Ollama API (`/api/tags`), not a cached YAML list.
  The YAML stanza for Ollama should contain only configuration (URL, timeout),
  not a model inventory.
- `OllamaStartedByHarvey` is a boolean; there is no `*os.Process` to
  stop on exit. If Harvey called `ollama serve`, it cannot kill that process
  reliably. Use the PID file approach above.

### Llamafile

- `adoptExternalServer` is the only backend with this logic. All three backends
  should adopt an already-running server that Harvey did not start.
- `/llamafile download` points to `docs.mozilla.ai` but that URL and the
  available models have changed since the feature was written (see `henry`
  project notes). The download helper needs a refresh or removal.
- Llamafile binaries embed both model weights and server code. There is no
  separate "start server" and "load model" step — the executable IS the server
  for one model. The `ManagedBackend` abstraction must account for this: for
  Llamafile, `Start(model)` means "launch `<model>.llamafile`" rather than
  "launch a generic server and tell it to load model."

### llama.cpp

- `llama-server` binary name varies by distro. `LlamaCppConfig.ServerPath`
  must be configurable (default: `llama-server` on PATH).
- Cold startup on Pi can exceed 60 seconds for 7B+ models. Default startup
  probe timeout should be 120 seconds, configurable.
- Critical performance flags (`--ctx-size`, `--threads`, `--n-gpu-layers`)
  are absent from the current design. These are required fields for Pi,
  not optional.
- `/llamacpp pull` from HuggingFace requires auth (`HF_TOKEN`) for many
  models and must support range-request resumption for large files on
  unreliable networks. This is a non-trivial implementation. Defer to Phase 2.

---

## Proposed File Layout

| File | Change |
|------|--------|
| `backend.go` | `ManagedBackend` interface, `ModelSummary`, `BackendStatus` types |
| `backend_ollama.go` | `OllamaBackend` implementing `ManagedBackend` |
| `backend_llamafile.go` | `LlamafileBackend` implementing `ManagedBackend`; absorbs core of `llamafile.go` |
| `backend_llamacpp.go` | `LlamaCppBackend` implementing `ManagedBackend` |
| `llamafile.go` | Retain backend-specific helpers; delegate lifecycle to `backend_llamafile.go` |
| `harvey.go` | Replace `OllamaStartedByHarvey bool` + `llamafileProc *os.Process` with `Backend ManagedBackend` |
| `config.go` | Add `LlamaCppConfig`; clean Ollama stanza to config-only (no model inventory) |

---

## Decisions (2026-06-29)

All open questions from the original draft were resolved in a design session.

| # | Question | Decision |
|---|---|---|
| 1 | Command surface | Option D: extend `/model` directly; no "backend" namespace; `use` and `start` combined into `/model use`; engine-specific ops stay in `/ollama`, `/llamafile`, `/llamacpp` |
| 2 | Auto-stop on exit | Stop on clean exit for all three backends (consistent with current llamafile behavior). PID file written when Harvey starts a server; re-adoption offered on next startup if process still alive |
| 3 | Concurrent backends | Warn and auto-switch: Harvey prints what it is leaving and what it is switching to, then switches `a.Client` immediately. Servers Harvey owns (per PID file) are stopped on switch; servers Harvey did not start are left running |
| 4 | Backend-specific subcommands | Structural separation — per-engine namespaces (`/ollama`, `/llamafile`, `/llamacpp`) are the escape hatch; no dispatcher escape hatch needed in `ManagedBackend` |
| 5 | `/llamafile add` / `/llamacpp add` | Removed as primary commands; replaced by the unified picker in `/model use` with lazy registration |
| 6 | `/ollama alias` | Moved to `/model alias`; aliases are engine-agnostic and belong at the `/model` level |
| 7 | Lazy registration scope | Applies to all sources — `.llamafile` files, `.gguf` files, and Ollama models all receive the same name + tags prompt on first use |

---

## Next Steps

1. Draft `backend.go`: `ManagedBackend` interface, `ModelSummary`,
   `BackendStatus` types. Review before writing implementations.
2. Implement `OllamaBackend` and `LlamafileBackend` (existing backends,
   lower regression risk). Run full test suite.
3. Add `LlamaCppBackend` once the interface is stable.
4. Update `harvey.go` and `config.go` last: replace `OllamaStartedByHarvey`
   and `llamafileProc` with `Backend ManagedBackend`; move `ModelAliases`
   to `map[string]ModelAlias` (U2); add PID file read/write.
5. Implement `/model use` unified picker with lazy registration.
6. Implement `/model alias` (absorbing `/ollama alias`).
7. Extend `/model status` and `/model list` to cover all three engines.
