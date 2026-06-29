# Harvey Unified Model Support — Implementation Plan

See [unified-models-design.md](unified-models-design.md) for the full
design rationale and alternatives considered.

Work items are labeled U1 → U5. U1 is independent. U2 must precede U3.
U4 is independent of U1–U3. U5 is deferred.

---

## U1 — `/ollama use` alias prompt

**Goal:** After switching to a model via `/ollama use`, if no alias
exists for that model, prompt the user to set one up. Brings `/ollama use`
into alignment with the `/llamafile add` registration UX.

**Effort:** ~1h.

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | In the `case "use":` block, after `modelSwitch` succeeds, call `promptOllamaAlias(a, modelName, out)`. |

### `promptOllamaAlias` logic

```go
func promptOllamaAlias(a *Agent, modelName string, out io.Writer) {
    // Check whether any alias already points to this model.
    for _, full := range a.Config.ModelAliases {
        if strings.EqualFold(full, modelName) {
            return // already aliased
        }
    }
    defaultAlias := ollamaModelStem(modelName) // e.g. "granite3.3:8b" → "granite"
    fmt.Fprintf(out, "  Short alias for %q [%s] (Enter to skip): ", modelName, defaultAlias)
    line, _ := bufio.NewReader(a.In).ReadString('\n')
    alias := strings.TrimSpace(line)
    if alias == "" {
        alias = defaultAlias
    }
    if alias == "" {
        return
    }
    _ = cmdModelAlias(a, []string{"set", alias, modelName}, out)
}
```

`ollamaModelStem` strips the tag suffix and any namespace prefix from
an Ollama model name: `"library/granite3.3:8b"` → `"granite3"`,
`"granite3.3:8b"` → `"granite3"`. Keep it simple: split on `:`,
take the left half, split on `/`, take the last segment, strip any
trailing digit-dot sequences beyond the first word.

### Acceptance criteria

- `go test ./...` passes.
- `/ollama use granite3.3:8b` (no existing alias) prompts for alias.
- Entering `granite` → alias saved; `/ollama alias list` shows it.
- Pressing Enter alone → skips with no alias saved.
- `/ollama use granite3.3:8b` when `granite` already maps to that model
  → no prompt shown.

---

## U2 — Purpose tags in model aliases

**Goal:** Extend `ModelAliases` from `map[string]string` to
`map[string]ModelAlias` so each alias can carry a `Tags []string` list.
Preserve backward compatibility: plain string values in YAML continue
to work.

**Effort:** ~3h (schema change touches config, YAML adapter, and all
callers of `ModelAliases`).

### New types

```go
// ModelAlias maps a short alias name to a backend model and optional
// purpose tags. The zero value (empty Model and nil Tags) is valid.
type ModelAlias struct {
    Model string   // full model name passed to the backend
    Tags  []string // e.g. ["code", "instruct"]
}
```

### Files to modify

| File | Change |
|------|--------|
| `config.go` | Replace `ModelAliases map[string]string` with `map[string]ModelAlias`. Update `ResolveAlias` to read `.Model`. Add `AliasesByTag(tag string) []string` helper. |
| `config_yaml.go` | Add `modelAliasYAML` union type that accepts both string and struct forms. Update `harveyYAML.ModelAliases` to `map[string]modelAliasYAML`. |
| `config.go` `LoadHarveyYAML` | Decode `modelAliasYAML` → `ModelAlias` with backward-compat string path. |
| `config.go` `SaveModelAliases` | Write `ModelAlias` structs; emit plain string when Tags is empty to keep YAML compact. |
| `commands.go` `cmdModelAlias` | Extend `set` subcommand with `--tags TAG,TAG,...` flag. Add `tags` subcommand: `/ollama alias tags ALIAS TAG [TAG...]`. Update `list` output to show tags column. |
| All callers of `cfg.ModelAliases[k]` | Update to `cfg.ModelAliases[k].Model`. |

### Backward-compatible YAML form

```yaml
# Old form (string) — still accepted, Tags defaults to []:
model_aliases:
  granite: granite3.3:8b

# New form (struct):
model_aliases:
  granite:
    model: granite3.3:8b
    tags: [code, instruct]
```

The YAML loader accepts both. The writer emits the compact string form
when `Tags` is empty and the struct form when tags are present.

### Acceptance criteria

- `go test ./...` passes; existing YAML files with string aliases load
  without error.
- `/ollama alias set granite granite3.3:8b --tags code,instruct` saves
  tags.
- `/ollama alias tags granite reasoning` appends a tag.
- `/ollama alias list` shows a tags column.
- `Config.AliasesByTag("code")` returns `["granite"]`.

---

## U3 — `@mention` routing by purpose tag

**Goal:** `@code` resolves to the best-scored alias tagged `code` when
no alias named `code` exists.

**Effort:** ~2h. Depends on U2.

### Files to modify

| File | Change |
|------|--------|
| `routing.go` | Extend `resolveAtMention` (or equivalent): after exact alias lookup fails, call `cfg.AliasesByTag(mention)`. If one result, use it. If multiple, pick highest `CapabilityStatus.Score` from `model_cache.db`; tie-break by first listed. |

### Routing resolution order

1. Exact alias name match → `ModelAlias.Model`.
2. Backend-native name (Ollama model list, registered llamafile name).
3. Tag match via `AliasesByTag` → highest-scored alias.
4. No match → existing error / fall-through behaviour.

### Acceptance criteria

- `go test ./...` passes.
- With `granite` tagged `[code, instruct]` and `qwen` tagged
  `[reasoning]`, `@code` resolves to `granite3.3:8b`.
- `@code` when no alias carries the `code` tag → existing no-match
  behaviour (no silent failure).
- `@granite` still resolves to `granite3.3:8b` (exact alias wins).

---

## U4 — llama.cpp backend

**Goal:** Add `/llamacpp add|use|list|drop|status` mirroring the
`/llamafile` command family. Scans `~/Models` for `.gguf` files. Starts
`llama-server` with `--model PATH`. Reuses the OpenAI-compatible HTTP
client already used for llamafile.

**Effort:** ~4h. Independent of U1–U3.

### New config type

```go
type LlamacppEntry struct {
    Name          string // short alias
    Path          string // path to .gguf file
    ContextLength int    // 0 = use server default
    ServerBin     string // path to llama-server binary; default "llama-server"
}
```

Add `LlamacppModels []LlamacppEntry`, `LlamacppActive string`,
`LlamacppURL string` (default `http://localhost:8081` — distinct from
llamafile's 8080) to `Config`.

### Files to create / modify

| File | Change |
|------|--------|
| `llamacpp.go` (new) | `cmdLlamacpp`, `cmdLlamacppAdd`, `cmdLlamacppUse`, `cmdLlamacppList`, `cmdLlamacppDrop`, `cmdLlamacppStatus`. Mirror `llamafile.go` structure. `scanLlamacppModels` finds `*.gguf` in `LlamacppModelsDir` (default `~/Models`). |
| `llamacpp_test.go` (new) | Tests for `scanLlamacppModels`, config round-trip. |
| `config.go` | Add `LlamacppEntry`, `LlamacppModels`, `LlamacppActive`, `LlamacppURL`, `LlamacppModelsDir`. |
| `config_yaml.go` | Add `llamacppYAML` stanza under `harveyYAML`. |
| `terminal.go` | Add `"llamacpp"` to backend selection in the startup sequence; add `cmdLlamacpp` to the command dispatcher. |
| `helptext.go` | Add `LlamacppHelpText`. |

### Start / stop lifecycle

```
/llamacpp add         → scan ~/Models for *.gguf, picker, name prompt,
                        start llama-server --model PATH --port PORT, save
/llamacpp use NAME    → stop current if Harvey started it, start new
/llamacpp drop NAME   → remove registration (stop if active)
/llamacpp status      → probe URL, show active model
```

`llama-server` is started with `--port`, `--ctx-size` (from
`ContextLength`), and `--model PATH`. All other flags use llama-server
defaults. Harvey does not pass `--n-gpu-layers`; users configure that
directly in their llama-server startup if needed (out of scope).

### Acceptance criteria

- `go test ./...` passes.
- `/llamacpp add` with `.gguf` files in `~/Models` shows a picker.
- Selected model starts `llama-server`, registers in `harvey.yaml`.
- `/llamacpp use NAME` switches between registered models.
- Chat turns route to the llamacpp backend when it is active.
- `/llamacpp status` reports running/stopped and active model name.

---

## U5 — Cross-workspace model registry (deferred)

**Goal:** Single global alias + tag registry inherited by all workspaces,
with per-workspace overrides.

**Deferred until:** U1–U3 are stable and the per-workspace alias UX is
validated in practice.

**Likely approach:** `$HOME/.config/harvey/harvey.yaml` (or
`$HOME/.harvey.yaml`) as a global layer loaded before the workspace
`agents/harvey.yaml`. Workspace values override global values. The
config loader merges the two maps.

---

## Dependency graph

```
U1 (ollama use alias prompt)    — independent; start here
U2 (purpose tags)               — independent of U1; start after U1
U3 (@mention tag routing)       — depends on U2
U4 (llama.cpp backend)          — independent of U1–U3
U5 (cross-workspace registry)   — depends on U2; deferred
```

Recommended order: U1 → U4 → U2 → U3 → U5 (when warranted).
