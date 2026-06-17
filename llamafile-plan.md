# Harvey Llamafile Integration — Implementation Plan

> **Revision history**
> - v1: single `LlamafilePath`; `--llamafile` CLI flag.
> - v2: named model registry; `/llamafile` command family; process tracking.
> - v3: `LlamafileModelsDir` with `$HOME/Models` default; env var and CLI
>   override; interactive picker in `/llamafile add`.

See [llamafile-design.md](llamafile-design.md) for the full design rationale.

## New Module Dependencies

None. All new code uses the standard library and existing Harvey packages.

## Files to Create

| File | Purpose |
|------|---------|
| `harvey/llamafile_service.go` | `ProbeLlamafile`, `StartLlamafileService` (exists; updated in Phase 2) |
| `harvey/llamafile_service_test.go` | Unit tests (exists; extended in Phase 7) |
| `harvey/llamafile.go` | `/llamafile` command handler, subcommands, picker, scanner |
| `harvey/llamafile_test.go` | Unit tests for command parsing, registry, and picker |

## Files to Modify

| File | Change |
|------|--------|
| `harvey/config.go` | Replace `LlamafilePath` with `LlamafileEntry`, `LlamafileModels`, `LlamafileActive`, `LlamafileModelsDir`; update YAML types, `DefaultConfig`, `LoadHarveyYAML`; add `SaveLlamafileConfig` and registry helpers |
| `harvey/harvey.go` | Add `llamafileProc *os.Process` to `Agent`; add `stopLlamafileProc()` |
| `harvey/llamafile_service.go` | `StartLlamafileService` returns `(*os.Process, error)` |
| `harvey/terminal.go` | Update `selectBackend`, `useLlamafileEntry`; update "no backend" hint; add `resolveLlamafilePath` |
| `harvey/commands.go` | Register `/llamafile` |
| `harvey/helptext.go` | Add `LlamafileHelpText` |
| `harvey/cmd/harvey/main.go` | Add `--llamafile`, `--llamafile-url`, `--llamafile-dir` flags; check `HARVEY_LLAMAFILE_DIR` env var |
| `harvey/Llamafile_notes.md` | Update to reflect revised design |

## Implementation Phases

### Phase 1 — Config (`config.go`)

**Replace** `LlamafilePath string` in `Config` with:

```go
LlamafileModels    []LlamafileEntry // registered models
LlamafileActive    string           // name of the active model; "" = none
LlamafileURL       string           // API base URL; default "http://localhost:8080"
LlamafileModelsDir string           // discovery path; default "$HOME/Models"
```

**Add** type alongside `RagStoreEntry`:

```go
/** LlamafileEntry describes one registered llamafile model.
 *
 * Fields:
 *   Name (string) — short identifier used with /llamafile use.
 *   Path (string) — path to binary; absolute or workspace-relative.
 */
type LlamafileEntry struct {
    Name string
    Path string
}
```

**Update** YAML mirror types. Replace `llamafileYAML`:

```go
type llamafileEntryYAML struct {
    Name string `yaml:"name"`
    Path string `yaml:"path"`
}

type llamafileYAML struct {
    ModelsDir string               `yaml:"models_dir,omitempty"`
    Active    string               `yaml:"active,omitempty"`
    URL       string               `yaml:"url,omitempty"`
    Models    []llamafileEntryYAML `yaml:"models,omitempty"`
}
```

**Update** `DefaultConfig`:

```go
LlamafileURL:       "http://localhost:8080",
LlamafileModelsDir: llamafileDefaultModelsDir(), // returns filepath.Join(os.UserHomeDir(), "Models")
```

`llamafileDefaultModelsDir` is a package-level helper that calls
`os.UserHomeDir()`, returns `"."` on error.

**Update** `LoadHarveyYAML` (replace the two-field llamafile block):

```go
if y.Llamafile.ModelsDir != "" {
    cfg.LlamafileModelsDir = expandTilde(y.Llamafile.ModelsDir)
}
if y.Llamafile.Active != "" {
    cfg.LlamafileActive = y.Llamafile.Active
}
if y.Llamafile.URL != "" {
    cfg.LlamafileURL = y.Llamafile.URL
}
for _, m := range y.Llamafile.Models {
    cfg.LlamafileModels = append(cfg.LlamafileModels, LlamafileEntry{
        Name: m.Name, Path: m.Path,
    })
}
```

`expandTilde(s string) string` replaces a leading `~/` or `~` with
`os.UserHomeDir()`. Used only for `models_dir`.

**Add** config helpers (follow `RagStoreByName` / `AddOrUpdateRagStore` conventions):

```go
func (c *Config) ActiveLlamafileEntry() *LlamafileEntry
func (c *Config) LlamafileEntryByName(name string) *LlamafileEntry
func (c *Config) AddOrUpdateLlamafileEntry(e LlamafileEntry)
```

**Add** `SaveLlamafileConfig(ws *Workspace, cfg *Config) error` — mirrors
`SaveMemoryConfig`: read existing YAML, overwrite only the `llamafile:`
section, write back. Serialises `LlamafileModelsDir` (unexpanded, preserving
`~/` notation), `LlamafileActive`, `LlamafileURL`, and `LlamafileModels`.

### Phase 2 — Update `StartLlamafileService` (`llamafile_service.go`)

Change signature:

```go
func StartLlamafileService(path, baseURL, logPath string) (*os.Process, error)
```

Return `(proc, nil)` on success. Return `(nil, err)` on exec failure or probe
timeout. The probe loop and invocation flags are unchanged.

### Phase 3 — Process Tracking (`harvey.go`)

Add to `Agent`:

```go
llamafileProc *os.Process
```

Add helper:

```go
func (a *Agent) stopLlamafileProc() {
    if a.llamafileProc != nil {
        _ = a.llamafileProc.Signal(os.Interrupt)
        a.llamafileProc = nil
    }
}
```

Call `a.stopLlamafileProc()` in the clean-exit handler in `terminal.go`
(wherever `/exit`, `/quit`, `/bye` are dispatched).

### Phase 4 — Startup Sequence (`terminal.go`)

**Add** `resolveLlamafilePath(path, root string) string` — returns `path` if
absolute, otherwise `filepath.Join(root, path)`.

**Update** `selectBackend` opening block to use the registry:

```go
if entry := a.Config.ActiveLlamafileEntry(); entry != nil {
    absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
    fmt.Fprintf(out, "\n  Checking llamafile (%s) at %s...\n",
        entry.Name, a.Config.LlamafileURL)
    if ProbeLlamafile(a.Config.LlamafileURL) {
        fmt.Fprintln(out, green("  ✓")+" Llamafile is running")
        return a.useLlamafileEntry(entry.Name, out)
    }
    fmt.Fprintf(out, yellow("  ✗")+" Llamafile (%s) is not running\n", entry.Name)
    if askYesNo(reader, out,
        fmt.Sprintf("    Start %s now? [Y/n] ", entry.Name), true) {
        fmt.Fprintln(out, "  Starting llamafile...")
        proc, err := StartLlamafileService(absPath, a.Config.LlamafileURL, "")
        if err != nil {
            fmt.Fprintf(out, red("  Failed: ")+"%v\n", err)
        } else {
            a.llamafileProc = proc
            fmt.Fprintln(out, green("  ✓")+" Llamafile started")
            return a.useLlamafileEntry(entry.Name, out)
        }
    }
    fmt.Fprintln(out)
    fmt.Fprintln(out, dim("  No backend selected."))
    fmt.Fprintln(out, dim("  → If Ollama is installed, use /ollama start once inside."))
    return nil
}
```

**Update** Ollama "no backend" hint:

```go
fmt.Fprintln(out, dim("  No backend selected."))
fmt.Fprintln(out, dim("  → If Ollama is installed, use /ollama start once inside."))
fmt.Fprintln(out, dim("  → Or pick a llamafile from:"))
fmt.Fprintln(out, dim("      https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles"))
fmt.Fprintln(out, dim("    Download it to ~/Models, then run /llamafile add to connect."))
```

**Rename** `useLlamafile` → `useLlamafileEntry(name string, out io.Writer) error`:

```go
func (a *Agent) useLlamafileEntry(name string, out io.Writer) error {
    a.Client = newLlamafileLLMClient(
        a.Config.LlamafileURL+"/v1", name, a.Config.OllamaTimeout)
    fmt.Fprintf(out, "  Using model: %s\n", cyan(name))
    return nil
}
```

Move `llamafileModelName` from `terminal.go` to `llamafile.go` — it is no
longer needed at startup (name comes from the registry), but is used by
`/llamafile add` to derive a default name from a path.

### Phase 5 — `/llamafile` Command (`llamafile.go`)

New file in the `harvey` package.

**Scanner helper**:

```go
// scanLlamafileModels returns the paths of all *.llamafile files in dir.
// Returns nil when dir does not exist or is unreadable.
func scanLlamafileModels(dir string) []string
```

Uses `os.ReadDir`; filters for files whose names end in `.llamafile`.

**Subcommand dispatch** (`cmdLlamafile`):

```go
func cmdLlamafile(a *Agent, args []string, out io.Writer) error {
    sub := ""
    if len(args) > 0 { sub = args[0] }
    switch sub {
    case "add":    return cmdLlamafileAdd(a, args[1:], out)
    case "use":    return cmdLlamafileUse(a, args[1:], out)
    case "list":   return cmdLlamafileList(a, out)
    case "start":  return cmdLlamafileStart(a, args[1:], out)
    case "status": return cmdLlamafileStatus(a, out)
    default:
        fmt.Fprint(out, LlamafileHelpText)
        return nil
    }
}
```

**`cmdLlamafileAdd`**:

1. If `args` is non-empty: `path = args[0]`; `name = args[1]` (or derived).
   Skip to step 5.
2. Scan `a.Config.LlamafileModelsDir` with `scanLlamafileModels`.
3. If empty: print "No llamafiles found in `<dir>`." then prompt for a path.
   If still empty after prompt: print usage and return.
4. Print numbered picker. Mark paths already in registry as `(registered as
   NAME)`. Read selection; if user types a path directly, use that.
   Prompt for a name (default = `llamafileModelName(selectedPath)`).
5. Resolve path (absolute or workspace-relative).
6. `os.Stat` the resolved path; error if missing.
7. Probe `a.Config.LlamafileURL`; if not reachable, `StartLlamafileService`;
   store `*os.Process` on agent.
8. `a.useLlamafileEntry(name, out)`.
9. `a.Config.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: name, Path: path})`;
   `a.Config.LlamafileActive = name`.
10. `SaveLlamafileConfig(a.Workspace, a.Config)`.
11. Print: `Saved to agents/harvey.yaml — Harvey will connect automatically on next start.`

**`cmdLlamafileUse`**:

1. `args[0]` = name (required; error if missing or not in registry).
2. `a.stopLlamafileProc()`.
3. Resolve path; `StartLlamafileService`; store `*os.Process`.
4. `a.useLlamafileEntry(name, out)`.
5. `a.Config.LlamafileActive = name`; `SaveLlamafileConfig`.

**`cmdLlamafileList`**:

Print header + one row per registered entry: arrow for active, name, path,
file size from `os.Stat` (show `(unknown size)` on error). Print models_dir
at the bottom:
```
  Discovery directory: ~/Models
```

**`cmdLlamafileStart`**:

1. Determine target: `args[0]` if given, else `a.Config.LlamafileActive`;
   error if neither resolves to a registered entry.
2. Probe; if already reachable, print status and return.
3. `StartLlamafileService`; store `*os.Process`; print confirmation.

**`cmdLlamafileStatus`**:

Print: active model (or "none"), API URL, reachable (probe result), models_dir,
count of registered models.

### Phase 6 — CLI Flags (`cmd/harvey/main.go`)

```go
case "--llamafile":
    cfg.LlamafilePath = next()  // session-only: set active without persisting
case "--llamafile-url":
    cfg.LlamafileURL = next()
case "--llamafile-dir":
    cfg.LlamafileModelsDir = next()
```

After flag parsing, apply the environment variable override (before
`LoadHarveyYAML` runs, so YAML can still override the env var only if we
want; but per the design the env var outranks YAML):

```go
if v := os.Getenv("HARVEY_LLAMAFILE_DIR"); v != "" {
    cfg.LlamafileModelsDir = v
}
```

Note: `--llamafile PATH` sets a session-only active model. It creates a
synthetic `LlamafileEntry{Name: llamafileModelName(PATH), Path: PATH}` and
sets `LlamafileActive` without calling `SaveLlamafileConfig`.

### Phase 7 — Help Text & Registration

Add `LlamafileHelpText` to `helptext.go`. Content: synopsis of each
subcommand, the pre-built models URL, the `harvey.yaml` config example, and
a note about `HARVEY_LLAMAFILE_DIR`.

Register in `commands.go`:

```go
"llamafile": {
    Usage:       "/llamafile <add|use|list|start|status>",
    Description: "Manage llamafile model backends",
    Handler:     cmdLlamafile,
},
```

Add `"llamafile"` to `cmdHelp`'s topic switch, both topic-list strings in
`commands.go`, and the `--help` topic list in `cmd/harvey/main.go`.

### Phase 8 — Tests

**`llamafile_service_test.go`** (extend existing):

| Test | Covers |
|------|--------|
| `TestProbeLlamafile_unreachable` | Existing — unchanged |
| `TestProbeLlamafile_reachable` | Existing — unchanged |
| `TestStartLlamafileService_badPath` | Existing — unchanged |
| `TestStartLlamafileService_returnsNilProcOnTimeout` | Mocked binary that starts but never serves; expect `(nil, err)` |

**`llamafile_test.go`** (new):

| Test | Covers |
|------|--------|
| `TestLlamafileModelName` | Move from service test; strip suffix, handle dirs |
| `TestScanLlamafileModels_empty` | Returns nil for missing or empty dir |
| `TestScanLlamafileModels_findsFiles` | Returns only `.llamafile` entries, ignores others |
| `TestExpandTilde` | `~/foo` → `/home/user/foo`; absolute paths unchanged |
| `TestConfigActiveEntry_none` | Returns nil when `LlamafileActive` is "" |
| `TestConfigActiveEntry_found` | Returns correct entry by name |
| `TestConfigAddOrUpdateEntry_insert` | Appends new entry |
| `TestConfigAddOrUpdateEntry_update` | Replaces entry with same name |
| `TestCmdLlamafileAdd_missingPath` | No models_dir and no arg → prompts then errors |
| `TestCmdLlamafileAdd_pathNotFound` | `os.Stat` fails → clear error |
| `TestCmdLlamafileUse_notRegistered` | Error when name absent from registry |
| `TestCmdLlamafileList_empty` | Prints "no models registered" and models_dir |
| `TestCmdLlamafileList_withEntries` | Correct table; active entry marked with arrow |

## Acceptance Criteria

- [ ] `/llamafile add` with no argument scans `LlamafileModelsDir`, shows a
      numbered picker, and registers the selected model
- [ ] `/llamafile add /path/to/model.llamafile` registers and starts without
      prompting for a path
- [ ] `/llamafile add /path/to/model.llamafile my-name` uses `my-name` as
      the registry key
- [ ] Already-registered models appear with `(registered as NAME)` in the
      picker
- [ ] If `models_dir` is empty or missing, `/llamafile add` prompts for a
      path instead of erroring
- [ ] `/llamafile list` shows all registered models; active entry is marked
      with an arrow; models_dir is shown at the bottom
- [ ] `/llamafile use NAME` stops the current server, starts the named model,
      and updates `agents/harvey.yaml`
- [ ] `/llamafile status` shows active model, URL, reachability, models_dir
- [ ] `$HOME/Models` is the default `models_dir`; `HARVEY_LLAMAFILE_DIR` env
      var overrides it; `--llamafile-dir` flag overrides both
- [ ] `llamafile.models_dir: ~/Models` in harvey.yaml expands `~` correctly
- [ ] Startup "no backend" hint includes the pre-built URL and
      `/llamafile add` command
- [ ] When `LlamafileActive` is empty (default), startup sequence is
      unchanged — Ollama is probed as today
- [ ] `go test ./...` and `go test -race` pass
- [ ] `/help llamafile` prints `LlamafileHelpText`
