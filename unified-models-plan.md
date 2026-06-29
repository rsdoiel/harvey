# Harvey Unified Model Support — Implementation Plan

See [unified-models-design.md](unified-models-design.md) for UX design rationale
and [unified-model-backend-design.md](unified-model-backend-design.md) for the
`ManagedBackend` architecture and all resolved design decisions.

Work items are labeled B0–B4 (backend abstraction) and U1–U5 (UX layer).
B0–B4 must precede U1. U2 must precede U3. U5 is deferred.

---

## B0 — `ManagedBackend` interface

**Goal:** Define the shared interface and supporting types in `backend.go`.
This is reviewed before any implementation is written.

**Effort:** ~2h.

### Files to create

| File | Change |
|------|--------|
| `backend.go` (new) | `ManagedBackend` interface, `ModelSummary`, `BackendStatus`, `BackendPID` types, PID file helpers |

### Types

```go
// ManagedBackend represents a local inference backend Harvey can start,
// stop, and query. Implemented by OllamaBackend, LlamafileBackend,
// and LlamaCppBackend.
type ManagedBackend interface {
    Name()            string       // "ollama", "llamafile", "llamacpp"
    BaseURL()         string       // e.g. "http://127.0.0.1:11434"
    ActiveModel()     string       // currently loaded model name, or ""
    IsRunning()       bool         // true when server is reachable
    Detect()          bool         // probe server; return IsRunning()
    Start(ctx context.Context, model string, out io.Writer) error
    Stop() error
    ListModels()      ([]ModelSummary, error)
    NewClient()       (LLMClient, error)
    StartedByHarvey() bool
}

// ModelSummary is a backend-neutral model description.
type ModelSummary struct {
    Name      string    // display / alias name
    Path      string    // filesystem path; empty for Ollama
    Engine    string    // "ollama", "llamafile", "llamacpp"
    SizeBytes int64     // 0 if unknown
    Modified  time.Time // zero if unknown
}

// BackendPID is the JSON payload written to agents/.harvey-backend.pid.
type BackendPID struct {
    Backend string `json:"backend"`
    PID     int    `json:"pid"`
    Model   string `json:"model"`
    URL     string `json:"url"`
}
```

### PID file helpers (also in `backend.go`)

```go
func writePIDFile(dir string, p BackendPID) error
func readPIDFile(dir string) (BackendPID, error)
func deletePIDFile(dir string) error
// probeOwnedProcess returns true if the PID in the file is still alive.
func probeOwnedProcess(p BackendPID) bool
```

### Acceptance criteria

- `go test ./...` passes (interface only, no implementations yet).
- `BackendPID` round-trips through JSON cleanly.

---

## B1 — `OllamaBackend`

**Goal:** Wrap existing Ollama connectivity in `ManagedBackend`. Absorbs
the current scattered `OllamaStartedByHarvey bool` logic.

**Effort:** ~3h. Depends on B0.

### Files to create / modify

| File | Change |
|------|--------|
| `backend_ollama.go` (new) | `OllamaBackend` struct implementing `ManagedBackend`. `Detect` calls `/api/tags`. `Start` runs `ollama serve` and writes PID file. `Stop` kills owned process via PID file. `ListModels` queries `/api/tags`. `NewClient` returns the existing Ollama-wired `LLMClient`. |
| `backend_ollama_test.go` (new) | Tests for `Detect` (mock HTTP), `ListModels`, config round-trip. |
| `harvey.go` | Replace `OllamaStartedByHarvey bool` with `Backend ManagedBackend`. |
| `terminal.go` | Replace direct ollama probe + startup with `OllamaBackend.Detect()` / `Start()`. |

### Acceptance criteria

- `go test ./...` passes.
- `OllamaBackend.Detect()` returns true when Ollama is running.
- `OllamaBackend.ListModels()` returns the live model list from `/api/tags`.
- `OllamaBackend.StartedByHarvey()` reads from the PID file, not a
  session-local boolean.

---

## B2 — `LlamafileBackend`

**Goal:** Wrap existing llamafile process management in `ManagedBackend`.
Absorbs `llamafileProc *os.Process` and the startup/stop logic scattered
across `llamafile.go` and `terminal.go`.

**Effort:** ~3h. Depends on B0.

### Files to create / modify

| File | Change |
|------|--------|
| `backend_llamafile.go` (new) | `LlamafileBackend` struct implementing `ManagedBackend`. `Start` launches the `.llamafile` binary, writes PID file. `Stop` kills owned process, deletes PID file. `Detect` calls `GET /v1/models`. `ListModels` scans `~/Models` for `*.llamafile`. `adoptExternal` migrates logic from current `adoptExternalServer`. |
| `backend_llamafile_test.go` (new) | Tests for `ListModels` (scan), `Detect` (mock HTTP). |
| `llamafile.go` | Retain backend-specific helpers (path resolution, port management). Remove lifecycle code; delegate to `backend_llamafile.go`. |
| `harvey.go` | Replace `llamafileProc *os.Process` (already removed with B1 change to `Backend ManagedBackend`). |
| `terminal.go` | Replace `stopLlamafileProc` call with `a.Backend.Stop()` on clean exit. |

### Acceptance criteria

- `go test ./...` passes.
- `LlamafileBackend.ListModels()` finds `*.llamafile` files in `~/Models`.
- `LlamafileBackend.StartedByHarvey()` reads from the PID file.
- Clean session exit stops the server if Harvey started it.
- Next startup reads PID file and adopts or cleans up.

---

## B3 — `LlamaCppBackend`

**Goal:** Add llama-server (llama.cpp) as a third `ManagedBackend`
implementation. Replaces the original U4 design; the backend abstraction
is built first rather than adding another ad-hoc implementation.

**Effort:** ~4h. Depends on B0. Independent of B1 and B2 once B0 is stable.

### New config type

```go
// LlamaCppConfig holds workspace-level llama.cpp settings.
type LlamaCppConfig struct {
    ServerBin  string // path to llama-server binary; default "llama-server"
    ModelsDir  string // default ~/Models
    URL        string // default http://127.0.0.1:8081
    CtxSize    int    // --ctx-size; 0 = server default
    Threads    int    // --threads; 0 = server default
    GPULayers  int    // --n-gpu-layers; 0 = CPU-only
}
```

`CtxSize`, `Threads`, and `GPULayers` are required fields for
Raspberry Pi deployments; they default to zero (server chooses) but
must be configurable. Cold startup timeout defaults to 120s
(configurable via `LlamaCppConfig.StartTimeout`).

### Files to create / modify

| File | Change |
|------|--------|
| `backend_llamacpp.go` (new) | `LlamaCppBackend` implementing `ManagedBackend`. `Start` runs `llama-server --model PATH --port PORT [--ctx-size N] [--threads N]`. `ListModels` scans `~/Models` for `*.gguf`. |
| `backend_llamacpp_test.go` (new) | Tests for `ListModels` (scan), config round-trip. |
| `config.go` | Add `LlamaCppConfig` struct; add `LlamaCpp LlamaCppConfig` field to `Config`. |
| `config_yaml.go` | Add `llamacppYAML` stanza under `harveyYAML`. |
| `terminal.go` | Add `LlamaCppBackend` to backend selection in startup sequence. |

### Acceptance criteria

- `go test ./...` passes.
- `LlamaCppBackend.ListModels()` finds `*.gguf` files in `~/Models`.
- `LlamaCppBackend.Start()` launches `llama-server --model PATH`.
- Chat turns route to the llama.cpp backend when it is active.
- `/llamacpp status` reports running/stopped and active model name.
- `/llamacpp drop NAME` unregisters a model (does not delete the file).

---

## B4 — PID file persistence across sessions

**Goal:** All three backends write a PID file on start and read it on
startup to re-adopt or clean up a server from a prior session.

**Effort:** ~2h. Depends on B1 and B2 (B3 inherits the same pattern).

### PID file location and format

```
agents/.harvey-backend.pid
```

```json
{"backend":"llamafile","pid":12345,"model":"phi4-Q4_K_M","url":"http://127.0.0.1:8080"}
```

### Startup sequence addition (`terminal.go`)

```
1. Read PID file if present.
2. probeOwnedProcess(pid) — is the process still alive?
   Yes → Detect() the URL → if reachable, adopt (set a.Backend, skip start).
         if not reachable, log "prior server (PID N) died; cleaning up" and
         delete PID file.
   No  → delete stale PID file; proceed with normal backend selection.
```

### Clean-exit addition (`terminal.go`)

```
On clean exit: if a.Backend.StartedByHarvey() → a.Backend.Stop() →
               deletePIDFile(workspaceDir).
```

### Acceptance criteria

- `go test ./...` passes.
- Starting a llamafile writes the PID file.
- Clean exit deletes the PID file and stops the server.
- Restarting Harvey with a live PID file re-adopts the server without
  starting a new process.
- Restarting Harvey with a stale PID file (process dead) cleans up
  and proceeds normally.

---

## U1 — `/model use` unified picker with lazy registration

**Goal:** Replace `/llamafile add`, `/llamacpp add`, and the post-`/ollama use`
alias prompt with a single gesture: `/model use` with no argument shows a
combined picker of all locally available models, prompts for an alias and
optional tags on first use, then starts the server and wires `a.Client`.

**Effort:** ~4h. Depends on B1 and B2 (needs `ListModels()` on both backends).

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | Rewrite `cmdModel` `case "use":` to call `pickAndUseModel` when no name supplied. Add `pickAndUseModel`, `aggregateModels`, `promptLazyRegister`. |

### Key functions

```go
// aggregateModels returns all locally available models across all sources.
// Ollama is queried only if reachable; absent silently.
func aggregateModels(a *Agent) ([]ModelSummary, error)

// promptLazyRegister checks whether item already has an alias; if not,
// prompts for a short name and optional comma-separated tags, then saves
// to a.Config.ModelAliases.
func promptLazyRegister(a *Agent, item ModelSummary, out io.Writer) (alias string, err error)

// pickAndUseModel presents the combined picker, handles lazy registration,
// starts the backend, and wires a.Client.
func pickAndUseModel(a *Agent, out io.Writer) error
```

### Picker display format

```
  Select model:
    1.  phi4-Q4_K_M          ~/Models/phi4-Q4_K_M.llamafile      [llamafile]
    2.  qwen2.5-7b            ~/Models/qwen2.5-7b-Q4_K_M.gguf    [llama-server]
    3.  granite3.3:8b         (ollama)                            [ollama]
    4.  qwen3:8b              (ollama)                            [ollama]
```

Items already aliased show their alias name. Unaliased items show the
stem of the filename or Ollama model name as the display label.

### Warn-and-switch behavior

When a model is selected and a different backend is currently active,
Harvey prints:

```
  Switching from ollama (granite3.3:8b) → llamafile (phi4-Q4_K_M)
```

If Harvey owns the outgoing server (per PID file), it is stopped first.
Servers Harvey did not start are left running.

### Acceptance criteria

- `go test ./...` passes.
- `/model use` with no args shows picker combining `~/Models` files and
  Ollama models (if reachable).
- Ollama section is absent and no error is shown when Ollama is not running.
- Picking an unaliased model prompts for name + tags; alias is saved.
- Picking an already-aliased model skips the registration prompt.
- Active backend switches with warn message when engines differ.
- `/model use granite` (known alias) switches without showing picker.

---

## U2 — Purpose tags and `/model alias`

**Goal:** Extend `ModelAliases` from `map[string]string` to
`map[string]ModelAlias` so each alias carries a `Tags []string`.
Move alias management from `/ollama alias` to `/model alias`.

**Effort:** ~3h. Depends on B0 (interface stable); independent of B1–B4.

### New type

```go
// ModelAlias maps a short alias name to a model and optional purpose tags.
type ModelAlias struct {
    Model string   // full model name / path passed to the backend
    Tags  []string // e.g. ["code", "instruct"]
}
```

### Files to modify

| File | Change |
|------|--------|
| `config.go` | Replace `ModelAliases map[string]string` with `map[string]ModelAlias`. Update `ResolveAlias` to read `.Model`. Add `AliasesByTag(tag string) []string`. |
| `config_yaml.go` | Add `modelAliasYAML` union type (accepts string or struct). Update `harveyYAML.ModelAliases`. |
| `config.go` `LoadHarveyYAML` | Decode `modelAliasYAML` → `ModelAlias`; string form sets `Tags: nil`. |
| `config.go` `SaveModelAliases` | Emit string form when `Tags` is empty; struct form otherwise. |
| `commands.go` `cmdModel` | Add `alias` subcommand dispatching `cmdModelAlias`. |
| `commands.go` `cmdModelAlias` (new) | `set NAME ID [--tags T,T]`, `tags NAME TAG [TAG...]`, `list`, `remove NAME`. Replaces `cmdOllamaAlias`. |
| All callers of `cfg.ModelAliases[k]` | Update to `cfg.ModelAliases[k].Model`. |

### Backward-compatible YAML

```yaml
# Old form — still accepted:
model_aliases:
  granite: granite3.3:8b

# New form:
model_aliases:
  granite:
    model: granite3.3:8b
    tags: [code, instruct]
```

### Acceptance criteria

- `go test ./...` passes; existing YAML with string aliases loads without error.
- `/model alias set granite granite3.3:8b --tags code,instruct` saves tags.
- `/model alias tags granite reasoning` appends a tag.
- `/model alias list` shows engine + tags columns.
- `/model alias remove granite` removes the alias.
- `Config.AliasesByTag("code")` returns `["granite"]`.
- `/ollama alias` remains as a deprecated alias routing to `cmdModelAlias`
  (removed in a later cleanup pass).

---

## U3 — `@mention` routing by purpose tag

**Goal:** `@code` resolves to the best-scored alias tagged `code` when
no alias named `code` exists.

**Effort:** ~2h. Depends on U2.

### Files to modify

| File | Change |
|------|--------|
| `routing.go` | Extend `resolveAtMention`: after exact alias lookup fails, call `cfg.AliasesByTag(mention)`. One result → use it. Multiple → pick highest `CapabilityStatus.Score` from `model_cache.db`; tie-break by first listed. |

### Routing resolution order

1. Exact alias name match → `ModelAlias.Model`.
2. Backend-native name (Ollama model list, registered llamafile/GGUF name).
3. Tag match via `AliasesByTag` → highest-scored alias.
4. No match → existing error / fall-through behaviour.

### Acceptance criteria

- `go test ./...` passes.
- With `granite` tagged `[code, instruct]` and `qwen` tagged `[reasoning]`,
  `@code` resolves to `granite3.3:8b`.
- `@code` when no alias carries the `code` tag → existing no-match behaviour.
- `@granite` still resolves to `granite3.3:8b` (exact alias wins).

---

## U5 — Cross-workspace model registry (deferred)

**Goal:** Single global alias + tag registry inherited by all workspaces,
with per-workspace overrides.

**Deferred until:** U1–U3 are stable and the per-workspace alias UX is
validated in practice.

**Likely approach:** `$HOME/.config/harvey/harvey.yaml` as a global layer
loaded before the workspace `agents/harvey.yaml`. Workspace values override
global values. The config loader merges the two alias maps.

---

## Dependency graph

```
B0 (ManagedBackend interface)
  ├── B1 (OllamaBackend)   ─┐
  ├── B2 (LlamafileBackend) ├── B4 (PID file persistence)
  └── B3 (LlamaCppBackend) ─┘
        │
        └── U1 (unified picker + lazy registration)  ← needs B1 + B2

B0 ──────────────────────────────── U2 (purpose tags + /model alias)
                                          │
                                          └── U3 (@mention tag routing)

U2 ─── R7-B (Config field grouping — unblocked once B0 interface is stable)

U5 (cross-workspace registry) — depends on U2; deferred
```

Recommended order: B0 → B1 + B2 (parallel) → B3 + B4 (parallel) →
U1 → U2 → U3 → R7-B → U5 (when warranted).
