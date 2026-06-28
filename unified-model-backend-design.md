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

Three options for the user-facing command structure:

### Option A — Unified `/model backend` namespace

```
/model backend list              — show all backends and their status
/model backend use ollama        — switch active backend (must already be running)
/model backend start ollama MODEL
/model backend start llamafile MODEL
/model backend start llamacpp MODEL
/model backend stop
/model backend status
```

Pros: single entry point, consistent vocabulary.  
Cons: breaks existing `/ollama` and `/llamafile` muscle memory.

### Option B — Per-backend commands sharing one implementation

Keep `/ollama`, `/llamafile`, and `/llamacpp` as separate command groups, but
implement each as a thin wrapper over the shared `ManagedBackend` dispatch:

```go
func cmdBackend(backend ManagedBackend, sub string, args []string, out io.Writer) error
```

`cmdOllama`, `cmdLlamafile`, and `cmdLlamaCpp` each construct the appropriate
`ManagedBackend` and delegate to `cmdBackend`. Backend-specific subcommands
(e.g., `/ollama pull`, `/llamafile add`) remain independent.

Pros: backward compatible; users keep familiar commands.  
Cons: three command trees to document; divergence remains visible.

### Option C — Alias both

Implement Option B, then add `/model backend` as an alias that auto-detects
the active backend:

```
/model backend start MODEL   — starts whatever backend is configured as default
/model backend status        — shows status of all detected backends
```

**Recommended: Option B for the initial release, with Option C's `/model backend status` added immediately because cross-backend visibility is the most acute current pain.**

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

## Open Questions

1. **Option A vs. B vs. C for commands?** Recommend Option B + status alias
   from Option C, but needs confirmation before implementation.

2. **When should Harvey auto-stop a backend it started?** On clean session
   exit? Only with a config flag? Only if the user asked for it? The
   `llamafile.go` current behavior (`stopLlamafileProc` in `terminal.go`)
   auto-stops on exit. This is likely correct but should be consistent across
   all three backends.

3. **What happens when the user runs `/llamafile start X` while Ollama is
   active?** Should Harvey stop Ollama first, or run both? Current behavior
   (keep Ollama running, start llamafile, switch `a.Client`) should be
   preserved, but made explicit in the `ManagedBackend` contract.

4. **Can the unified command path expose backend-specific subcommands cleanly?**
   `/ollama probe` and `/ollama alias` have no llamafile or llama.cpp
   equivalents. The shared `cmdBackend` dispatcher must have an escape hatch
   for backend-specific operations.

---

## Next Steps

1. Confirm command surface option (B recommended).
2. Draft `backend.go` interface with stakeholder review before writing
   implementations.
3. Implement `OllamaBackend` and `LlamafileBackend` first (existing backends,
   lower risk of regression) and run full test suite.
4. Add `LlamaCppBackend` as the third implementation once the interface is stable.
5. Update `harvey.go` and `config.go` last, after all three backends pass tests.
