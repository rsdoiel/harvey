# Harvey Quick Fixes — Design

**Status (2026-06-18):** Design settled. See [quick-fixes-plan.md](quick-fixes-plan.md) for the
phased implementation plan.

This document covers four related fixes: two bugs that have been reported, one
small ergonomics feature, and one housekeeping improvement. They are grouped
into a single document because each is small, self-contained, and can be
shipped independently.

---

## 1. PDF Capability Disclosure in HARVEY.md

### Problem

Harvey's `read_file` built-in tool description explicitly states:

> PDF files (.pdf) are automatically extracted to plain text using poppler
> utilities — no manual conversion is needed.

This disclosure only reaches the model when tools are enabled *and* the model
reads tool descriptions before deciding how to respond. Small models using
prose tool calls, and any session with `--tools off`, have no knowledge of
Harvey's PDF support. The result: the model asks the user to convert a PDF
to text before it can help, even though Harvey would handle this automatically
if asked via `read_file` or `/attach`.

### Root cause

`HARVEY.md` is always injected as the system prompt. It documents Harvey's
automatic write-proposal behavior (tagged code blocks) and safe-mode rules,
but says nothing about Harvey's file reading capabilities. There is no single
source of truth for "what can Harvey read without user intervention?"

### Solution

Add a **File reading capabilities** section to `HARVEY.md`. It lists all
formats Harvey handles transparently, with a single line per format. The
section follows the existing "Agent execution model" section, which already
documents automatic behaviors.

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

The last sentence is a model instruction, not a description — it closes the
loop by telling the model what to do differently.

### Scope

- `HARVEY.md` only. No code changes.
- Does not affect `builtin_tools.go` (the tool description already correct).
- Does not affect `pdf_extract.go`, `helptext.go`, or any test file.

---

## 2. Llamafile Windows .exe Discovery

### Problem

`scanLlamafileModels()` in `llamafile.go` identifies binaries by suffix:

```go
if !e.IsDir() && strings.HasSuffix(e.Name(), ".llamafile") {
```

The llamafile project ships Windows binaries with two possible names:
- `Llama-3.2-1B.llamafile.exe` — the "universal" format that works on all
  platforms after renaming `.exe` to `.llamafile` on non-Windows.
- `Llama-3.2-1B.exe` — plain Windows executable distributed separately.

Neither matches `.llamafile`. Users on Windows see an empty model picker
even with valid models in `~/Models`.

A second bug: `llamafileModelName` strips only `.llamafile`, leaving `.exe`
in the display name:

```go
return strings.TrimSuffix(name, ".llamafile")
// "Llama-3.2-1B.llamafile.exe" → "Llama-3.2-1B.llamafile.exe" (unchanged)
```

### Solution

**`scanLlamafileModels` changes:**

Match three patterns, in priority order:
1. `.llamafile` — Linux/macOS standard form (unchanged).
2. `.llamafile.exe` — Windows universal form.
3. `.exe` — Windows-only plain executable. This pattern is only activated
   when the OS is Windows (`runtime.GOOS == "windows"`), to avoid picking
   up unrelated executables on other platforms.

**`llamafileModelName` changes:**

Strip suffixes in longest-first order so the double extension is handled
correctly before the single extension:
1. Strip `.llamafile.exe` (if present).
2. Strip `.exe` (if still present).
3. Strip `.llamafile` (if still present).

The helper `strings.TrimSuffix` already handles the "no-op if suffix not
present" case, so the chain is safe.

### Platform guard

The `.exe` fallback scan is guarded by `runtime.GOOS == "windows"`. On
Linux and macOS, only `.llamafile` and `.llamafile.exe` are matched. This
prevents Harvey from accidentally treating non-llamafile binaries as models
on developer machines with mixed executable types in `~/Models`.

---

## 3. `--resume` Flag

### Problem

The most common workflow after reopening a terminal is to resume the last
Harvey session. The current options are:

- `--continue PATH` — requires finding and typing the session file path.
- Interactive picker at startup — offers all sessions and requires a
  keypress to select the most recent one.

Both are unnecessary friction when the user simply wants to pick up exactly
where they left off.

### Solution

Add a `--resume` flag (no argument) to `cmd/harvey/main.go`. When present,
it resolves the most recently modified `.spmd` file in `agents/sessions/`
and assigns it to `cfg.ContinuePath`. Harvey then enters the existing
`ContinueFromFountain` path without any interactive prompt.

```
harvey --resume
```

If no `.spmd` files exist in `agents/sessions/`, Harvey prints:
```
  No sessions found in agents/sessions/ — starting fresh.
```
and continues to a clean session.

**Implementation detail:** `--resume` sets `cfg.ContinuePath` before `Run`
is called. Inside `Run`, the `pickSession` call is skipped when
`cfg.ContinuePath` is already set (this logic already exists for
`--continue`). No changes to `terminal.go` are needed.

A new helper `mostRecentSession(sessDir string) string` returns the path of
the `.spmd` file with the most recent `ModTime` in `sessDir`, or `""` if
none exist. It lives in `sessions_files.go` alongside the other session
file utilities.

### Interaction with `--record`

The existing guard in `terminal.go:333-338` prevents re-recording a session
that is being resumed. `--resume --record` is safe: recording starts a new
file distinct from the resumed session.

---

## 4. Assay Output at Workspace Level

### Problem

`bin/assay` writes evaluation output to `testout/` inside the `harvey/`
source repository:

```
harvey/testout/assay-20260618-143022/
  report.md
  results.json
```

`testout/` is gitignored, but the artifacts are still visible to file-tree
tools (`/file-tree`, `read_dir`). Language models reading the harvey source
tree interpret these as current test results, generating false alarms about
test failures and confusing the agent's understanding of the project state.

### Solution

Change the default output directory to a workspace-level path:

```
$WORKSPACE/assay-results/assay-<timestamp>/
  report.md
  results.json
```

where `$WORKSPACE` is resolved using the same heuristic Harvey uses:
walk up from the current working directory to the nearest directory
containing `agents/harvey.yaml`.

If no workspace is found (e.g., assay is run outside a Harvey workspace),
fall back to `assay-results/` in the current working directory — same
behaviour as today's default, but with a clearer directory name.

The `--output PATH` flag continues to override the default entirely.

### Implementation

A `findWorkspaceRoot(start string) string` helper in `cmd/assay/main.go`
walks up the directory tree looking for `agents/harvey.yaml`. This is a
small standalone function (~15 lines); it does not import the Harvey
package's `NewWorkspace` to avoid increasing the assay binary's dependency
surface unnecessarily.

The `defaultOutputDir()` function uses `findWorkspaceRoot` and returns the
fully qualified output path with a timestamp suffix.

### Migration note

Users with scripts referencing the old `testout/assay-*` path need to
update to the new location or pass `--output testout/` explicitly. The
`--help` output documents the default location so users can find results.

---

## Out of Scope

- **Command tab completion** — mentioned in TODO but requires significant
  `termlib` changes. Tracked separately.
- **Min.io replacement** — tracked in `s3-replacement-design.md`.
- **Assay Llamafile evaluation** — tracked in `assay-llamafile-design.md`.
