# Harvey Assay — Llamafile Support & Output Location — Implementation Plan

See [assay-llamafile-design.md](assay-llamafile-design.md) for the full
design rationale and decisions.

The two changes — Llamafile backend and workspace-level output — are
independent. Phase A (output location) can ship before Phase B (Llamafile
support). Both compile and pass tests independently.

---

## Phase A — Workspace-Level Output Directory

**Goal:** Change the default assay output directory from `testout/` to
`$WORKSPACE/assay-results/assay-<timestamp>/`.

### Files to modify

| File | Change |
|------|--------|
| `cmd/assay/main.go` | Add `findWorkspaceRoot`, `defaultOutputDir`; update flag defaults and `--help` text |

### `findWorkspaceRoot` function

```go
// findWorkspaceRoot walks up from start looking for the directory containing
// agents/harvey.yaml. Returns "" if not found.
func findWorkspaceRoot(start string) string {
    dir := start
    for {
        if _, err := os.Stat(filepath.Join(dir, "agents", "harvey.yaml")); err == nil {
            return dir
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            return "" // reached filesystem root
        }
        dir = parent
    }
}
```

### `defaultOutputDir` function

```go
func defaultOutputDir() string {
    cwd, _ := os.Getwd()
    ts := time.Now().Format("20060102-150405")
    if root := findWorkspaceRoot(cwd); root != "" {
        return filepath.Join(root, "assay-results", "assay-"+ts)
    }
    return filepath.Join("assay-results", "assay-"+ts)
}
```

### Flag initialization change

Replace the current hardcoded default for `--output`:

```go
// Before
outputDir := flag.String("output", "testout/assay-"+timestamp, "...")

// After
outputDir := flag.String("output", defaultOutputDir(), "...")
```

Since `defaultOutputDir` is called at flag parse time, the timestamp is
captured correctly.

### Updated `--help` text for `--output`

```
--output PATH   write report and results to PATH
                default: $WORKSPACE/assay-results/assay-TIMESTAMP/
                         (assay-results/assay-TIMESTAMP/ if not in a workspace)
```

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `bin/assay` run from a Harvey workspace writes to
  `$WORKSPACE/assay-results/assay-<timestamp>/`.
- `bin/assay` run from outside a workspace writes to
  `assay-results/assay-<timestamp>/` relative to cwd.
- `bin/assay --output custom/path` writes to `custom/path` regardless of
  workspace discovery.
- `testout/` is not created by a default assay run.

---

## Phase B — Llamafile Backend Support

**Goal:** Add `--llamafile PATH` flag to `bin/assay` that starts, uses,
and cleanly stops a llamafile process during evaluation.

### Files to modify

| File | Change |
|------|--------|
| `cmd/assay/main.go` | Add `--llamafile` flag parsing; lifecycle management (`startLlamafile`, `findFreePort`, deferred stop); update report header; update `--help` |

### Note on package imports

`cmd/assay/main.go` already imports the root `harvey` package for `RagStore`
and `OllamaEmbedder`. The functions `startLlamafile` and `findFreePort` are
in `llamafile_service.go` (package `harvey`) and are accessible via the same
import. No new files or imports are needed.

### Flag addition

```go
llamafilePath := flag.String("llamafile", "", "path to a llamafile binary to evaluate")
```

### Lifecycle management

After flag parsing and before corpus loading:

```go
var llamafileURL string
if *llamafilePath != "" {
    port, err := harvey.FindFreePort()
    if err != nil {
        log.Fatalf("llamafile: cannot find free port: %v", err)
    }
    proc, err := harvey.StartLlamafile(*llamafilePath, port)
    if err != nil {
        log.Fatalf("llamafile: failed to start: %v", err)
    }
    defer proc.Stop()
    llamafileURL = fmt.Sprintf("http://localhost:%d", port)

    // Wait up to 30 s for the server to be ready.
    if err := harvey.WaitLlamafileReady(llamafileURL, 30*time.Second); err != nil {
        proc.Stop()
        log.Fatalf("llamafile: did not become ready: %v", err)
    }

    fmt.Printf("  Llamafile ready at %s\n", llamafileURL)
}
```

When `llamafileURL` is set, pass it to the LLM client in place of the Ollama
URL. The rest of the evaluation loop is unchanged.

### RAG + Llamafile guard

When both `--llamafile` and `--rag-db` are provided:

```go
if *llamafilePath != "" && *ragDBPath != "" {
    // Embeddings go to Ollama; warn if Ollama is not reachable.
    if err := harvey.ProbeOllama(*ollamaURL); err != nil {
        log.Fatalf("RAG evaluation with --llamafile requires Ollama for embeddings.\n"+
            "Start Ollama or use --ollama-url to specify a running instance.\n"+
            "Error: %v", err)
    }
}
```

### Report header extension

Add "Backend" and "Binary" rows to the report header table:

```markdown
| Backend | Llamafile |
| Binary  | /home/user/Models/Llama-3.2-1B.llamafile |
```

For Ollama runs, "Backend" shows "Ollama" and "Binary" is omitted.

### Updated `--help` text

```
--llamafile PATH    evaluate a llamafile model; starts and stops the process
                    automatically (mutually exclusive with --ollama-url)
```

### Export requirements

The following functions must be exported from `llamafile_service.go` (or
already be exported — verify before implementing):

| Function | Currently exported? | Action if not |
|---|---|---|
| `startLlamafile` | Check | Export as `StartLlamafile` |
| `findFreePort` | Check | Export as `FindFreePort` |
| `WaitLlamafileReady` (or equivalent) | Check | Export or add |

If the health-check loop is currently inline in `terminal.go`, extract it
into a named function in `llamafile_service.go` so assay can reuse it.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `bin/assay --llamafile /path/to/model.llamafile` starts the process,
  runs the full corpus, writes the report, and terminates the process.
- The report header shows "Llamafile" as backend and includes the binary path.
- `bin/assay --llamafile PATH --rag-db PATH` fails with a clear error if
  Ollama is not reachable for embeddings.
- Ctrl+C during evaluation terminates the llamafile process cleanly.

---

## Dependency Graph

```
Phase A (workspace output)    — independent
Phase B (llamafile backend)   — independent of Phase A
```

Both phases can be developed and committed in either order.

---

## Open Questions

- Verify export status of `startLlamafile`, `findFreePort`, and any
  health-check function in `llamafile_service.go` before implementing
  Phase B.
- Verify the `LlamafileProcess` type's stop method signature (likely
  `proc.Stop()` or `proc.Kill()`).
