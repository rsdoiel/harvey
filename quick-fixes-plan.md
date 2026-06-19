# Harvey Quick Fixes — Implementation Plan

See [quick-fixes-design.md](quick-fixes-design.md) for the full design
rationale and decisions.

Each fix is independent and can be shipped in any order. All four are
included in a single plan document because they are small and likely to
be committed together.

---

## Fix A — PDF Capability in HARVEY.md

**Goal:** Add a "File reading capabilities" section to `HARVEY.md` so the
model knows about automatic PDF extraction regardless of whether tools are
enabled.

### Files to modify

| File | Change |
|------|--------|
| `HARVEY.md` | Add "File reading capabilities" section after "Agent execution model" |

### Content to add

Insert after the `## Documentation conventions` heading (which follows the
`## Agent execution model` section):

```markdown
## File reading capabilities

When asked to read a file, Harvey handles these formats automatically:

- **Plain text, Markdown, source code** — returned as-is.
- **PDF (.pdf)** — text is extracted using the poppler utilities
  (pdfinfo, pdftotext). No user conversion is needed. Use the optional
  `pages` parameter to read a subset (e.g. `"1-10"` or `"5"`).
- **Images (.png, .jpg, .jpeg, .gif, .webp)** — injected directly when
  the active model supports vision input.

Never ask the user to convert a PDF to text before reading it. Call
`read_file` with the `.pdf` path directly.
```

### Acceptance criteria

- `HARVEY.md` contains the new section.
- In a session with tools disabled, asking Harvey to summarize a PDF no
  longer produces "please convert this to text first" responses.
- `go build ./...` and `go test ./...` pass (no code changes).

---

## Fix B — Llamafile Windows .exe Discovery

**Goal:** Make `scanLlamafileModels` and `llamafileModelName` handle
`.llamafile.exe` and `.exe` (Windows) extensions correctly.

### Files to modify

| File | Change |
|------|--------|
| `llamafile.go` | Update `scanLlamafileModels` to match `.llamafile.exe` and `.exe` (Windows guard). Update `llamafileModelName` to strip suffixes in longest-first order. |
| `llamafile_test.go` | Add test cases for `.llamafile.exe` and `.exe` naming. |

### `scanLlamafileModels` implementation

```go
func scanLlamafileModels(dir string) []string {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return nil
    }
    var paths []string
    for _, e := range entries {
        if e.IsDir() {
            continue
        }
        name := e.Name()
        if strings.HasSuffix(name, ".llamafile") ||
            strings.HasSuffix(name, ".llamafile.exe") ||
            (runtime.GOOS == "windows" && strings.HasSuffix(name, ".exe")) {
            paths = append(paths, filepath.Join(dir, name))
        }
    }
    return paths
}
```

### `llamafileModelName` implementation

```go
func llamafileModelName(path string) string {
    name := filepath.Base(path)
    name = strings.TrimSuffix(name, ".llamafile.exe")
    name = strings.TrimSuffix(name, ".exe")
    name = strings.TrimSuffix(name, ".llamafile")
    return name
}
```

The three `TrimSuffix` calls are safe: each is a no-op if the suffix is not
present. Longest suffix is stripped first so `Foo.llamafile.exe` becomes
`Foo` (not `Foo.llamafile`).

### Test cases to add in `llamafile_test.go`

| Input | Expected output |
|---|---|
| `Llama-3.2-1B.llamafile` | `Llama-3.2-1B` |
| `Llama-3.2-1B.llamafile.exe` | `Llama-3.2-1B` |
| `Llama-3.2-1B.exe` | `Llama-3.2-1B` |
| `/path/to/Qwen3.llamafile` | `Qwen3` |

Also add a test for `scanLlamafileModels` using a temp directory with one
file of each extension type.

### Import needed

Add `"runtime"` to `llamafile.go` imports (if not already present).

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- All four `llamafileModelName` cases above return the expected string.
- `scanLlamafileModels` returns `.llamafile.exe` files on all platforms
  and `.exe` files only on Windows.

---

## Fix C — `--resume` Flag

**Goal:** Add a `--resume` flag to `cmd/harvey/main.go` that auto-selects
the most recent session without prompting.

### Files to modify

| File | Change |
|------|--------|
| `sessions_files.go` | Add `mostRecentSession(sessDir string) string` helper |
| `cmd/harvey/main.go` | Add `--resume` flag case |
| `helptext.go` | Add `--resume` to the flags section of `HelpText` |

### `mostRecentSession` implementation

```go
/** mostRecentSession returns the path of the most recently modified .spmd
 * file in sessDir, or "" if the directory is empty or does not exist.
 *
 * Parameters:
 *   sessDir (string) — absolute path to the sessions directory.
 *
 * Returns:
 *   string — absolute path to the most recent .spmd file, or "".
 *
 * Example:
 *   path := mostRecentSession("/workspace/agents/sessions")
 */
func mostRecentSession(sessDir string) string {
    entries, err := os.ReadDir(sessDir)
    if err != nil {
        return ""
    }
    var newest string
    var newestTime time.Time
    for _, e := range entries {
        if e.IsDir() || !strings.HasSuffix(e.Name(), ".spmd") {
            continue
        }
        info, err := e.Info()
        if err != nil {
            continue
        }
        if info.ModTime().After(newestTime) {
            newestTime = info.ModTime()
            newest = filepath.Join(sessDir, e.Name())
        }
    }
    return newest
}
```

### `cmd/harvey/main.go` change

Add to the flag switch before `cfg.SystemPrompt = harvey.LoadHarveyMD()`:

```go
case "--resume":
    // Resolved after workspace is known (sessDir depends on cfg.WorkDir).
    cfg.ResumeLatest = true
```

And after `ws, err := harvey.NewWorkspace(cfg.WorkDir)`:

```go
if cfg.ResumeLatest && cfg.ContinuePath == "" {
    sessDir := filepath.Join(ws.Root, "agents", "sessions")
    if p := harvey.MostRecentSession(sessDir); p != "" {
        cfg.ContinuePath = p
    } else {
        fmt.Fprintln(os.Stderr, "  No sessions found in agents/sessions/ — starting fresh.")
    }
}
```

### `Config` change

Add `ResumeLatest bool` to `Config` in `config.go` alongside `ContinuePath`.

### Help text

Add to the flags section in `helptext.go`:

```
--resume                resume the most recent session (no argument needed)
--continue PATH         resume from a specific session file
```

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `harvey --resume` in a workspace with sessions loads the most recent
  `.spmd` and prints the "Resumed N turns" line.
- `harvey --resume` in a workspace with no sessions starts fresh with
  a one-line notice.
- `harvey --resume --continue PATH` ignores `--resume` (ContinuePath is set;
  ResumeLatest is skipped because `cfg.ContinuePath != ""`).
- Existing `--continue` behavior is unchanged.

---

## Fix D — Assay Output at Workspace Level

**Goal:** Change `bin/assay` default output directory from `testout/` to
`$WORKSPACE/assay-results/assay-<timestamp>/`.

### Files to modify

| File | Change |
|------|--------|
| `cmd/assay/main.go` | Add `findWorkspaceRoot` helper; change default output path logic |

### `findWorkspaceRoot` implementation

```go
// findWorkspaceRoot walks up from start looking for a directory containing
// agents/harvey.yaml. Returns "" if not found.
func findWorkspaceRoot(start string) string {
    dir := start
    for {
        if _, err := os.Stat(filepath.Join(dir, "agents", "harvey.yaml")); err == nil {
            return dir
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            return ""
        }
        dir = parent
    }
}
```

### Default output path logic

Replace the current `defaultOutputDir()` (or inline default):

```go
func defaultOutputDir() string {
    cwd, _ := os.Getwd()
    root := findWorkspaceRoot(cwd)
    ts := time.Now().Format("20060102-150405")
    if root != "" {
        return filepath.Join(root, "assay-results", "assay-"+ts)
    }
    return filepath.Join("assay-results", "assay-"+ts)
}
```

### Help text update in `cmd/assay/main.go`

Update the `--output` flag description:

```
--output PATH   write report and results to PATH
                (default: $WORKSPACE/assay-results/assay-TIMESTAMP/
                 or assay-results/assay-TIMESTAMP/ if not in a workspace)
```

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- Running `bin/assay` from inside a Harvey workspace creates output in
  `$WORKSPACE/assay-results/assay-<timestamp>/`.
- Running `bin/assay` from outside a workspace creates output in
  `assay-results/assay-<timestamp>/` relative to cwd.
- `bin/assay --output testout/myrun` still writes to `testout/myrun`.
- `testout/` inside the harvey repo is not populated by a default assay run.

---

## Dependency Graph

All four fixes are independent. Suggested commit order:

```
Fix A  (HARVEY.md)            — no tests needed; low risk
Fix B  (llamafile .exe)       — add tests before shipping
Fix C  (--resume)             — new flag + helper; add unit test
Fix D  (assay output)         — new helper + default change
```

Each fix can be committed separately on `main`.

---

## Open Questions

None as of 2026-06-18.
