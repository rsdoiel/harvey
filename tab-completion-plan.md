# Harvey Tab Completion — Implementation Plan

See [tab-completion-design.md](tab-completion-design.md) for the full
design rationale and decisions.

Phases are ordered so each one compiles, passes tests, and provides
standalone value before the next begins.

---

## Phase A — `ui.go`: shared select list helper

**Goal:** Create the `SelectFrom` / `SelectFromStrings` / `SelectItem`
API that all later phases depend on. No existing behaviour changes.

### Files to create

| File | Purpose |
|------|---------|
| `ui.go` | `SelectItem`, `SelectFrom`, `SelectFromStrings`, unexported helpers |
| `ui_test.go` | Unit tests for all exported symbols |

### `ui.go` implementation notes

```go
package harvey

import (
    "bufio"
    "fmt"
    "io"
    "strings"
)

type SelectItem struct {
    Value  string
    Label  string
    Active bool
}

func SelectFrom(items []SelectItem, prompt string, in io.Reader, out io.Writer) (string, error) {
    if len(items) == 0 {
        return "", nil
    }
    if len(items) == 1 {
        return items[0].Value, nil
    }
    renderSelectList(items, out)
    fmt.Fprint(out, "  "+prompt)
    reader := bufio.NewReader(in)
    line, _ := reader.ReadString('\n')
    line = strings.TrimSpace(line)
    idx, raw := parseSelectInput(line, len(items))
    if idx >= 1 && idx <= len(items) {
        return items[idx-1].Value, nil
    }
    if raw != "" {
        return raw, nil  // caller validates (e.g. file path)
    }
    return "", nil  // cancelled
}

func SelectFromStrings(items []string, prompt string, in io.Reader, out io.Writer) (string, error) {
    si := make([]SelectItem, len(items))
    for i, s := range items {
        si[i] = SelectItem{Value: s, Label: s}
    }
    return SelectFrom(si, prompt, in, out)
}
```

`renderSelectList` prints:

```
  →  [1] active-store
     [2] other-store
     [3] third-store
```

`parseSelectInput` returns `(idx, "")` for numeric input, `(0, raw)`
for non-numeric. Input `""`, `"q"`, or `"0"` all return `(0, "")`.

### `ui_test.go` tests

| Test | Checks |
|------|--------|
| `TestSelectFrom_singleItem` | Returns item without prompting |
| `TestSelectFrom_validIndex` | Picks item by number |
| `TestSelectFrom_outOfRange` | Returns `""` for index > len |
| `TestSelectFrom_empty` | Returns `""` for empty input |
| `TestSelectFrom_rawInput` | Returns raw string for non-numeric |
| `TestSelectFrom_activeMarker` | Active item rendered with `→` |
| `TestSelectFromStrings_basic` | Value == Label, works correctly |
| `TestSelectFrom_cancel_q` | `"q"` returns `""` |

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `SelectFrom` returns the `Value` (not `Label`) of the chosen item.
- `SelectFrom` with 1 item returns it without printing the list.
- Active item is rendered with `→`; inactive with equivalent indent.

---

## Phase B — `Command` struct + layer 1 subcommand completion

**Goal:** Add `Subcommands []string` and `ArgCompletion` to `Command`.
Populate them for every multi-subcommand command. Extend
`buildCompleter()` to use them for second-token completion.

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | Add `Subcommands []string` and `ArgCompletion map[string]func(*Agent) []string` to `Command` struct and its doc comment. Populate both fields for: `memory`, `rag`, `ollama`, `llamafile`, `kb`, `skill`, `skill-set`, `route`, `plan`, `session`. |
| `terminal.go` | In `buildCompleter()`: insert layer 1 block between the `@mention` block and the Ollama-model block. |
| `commands_test.go` | Add `TestSubcommandCompletion`: create agent, register commands, call `buildCompleter()(line)` for a handful of cases, assert expected completions. |

### Layer 1 block in `buildCompleter()`

```go
// Layer 1: subcommand completion (second token position).
if len(tokens) == 2 || (len(tokens) == 1 && strings.HasSuffix(line, " ")) {
    cmdName := ""
    if len(tokens) >= 1 {
        cmdName = strings.ToLower(strings.TrimPrefix(tokens[0], "/"))
    }
    if cmd, ok := a.commands[cmdName]; ok && len(cmd.Subcommands) > 0 {
        prefix := strings.ToLower(word)
        var matches []string
        for _, sub := range cmd.Subcommands {
            if strings.HasPrefix(sub, prefix) {
                matches = append(matches, sub)
            }
        }
        sortStrings(matches)
        return matches
    }
}
```

### Subcommand lists to register

| Command | Subcommands |
|---------|-------------|
| `memory` | `mine list show flag forget status recall profile` |
| `rag` | `list new use drop ingest status query on off` |
| `ollama` | `start stop status list ps run pull push show create cp rm logs use env alias` |
| `llamafile` | `add use list start status drop` |
| `kb` | `status search inject project observe concept` |
| `skill` | `list load info status new run` |
| `skill-set` | `list load info create status unload` |
| `route` | `add rm models probe set list on off status` |
| `plan` | `next status show clear` |
| `session` | `continue replay` |

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `/memory <tab>` completes to subcommand names.
- `/memory m<tab>` completes to `mine`.
- Commands without `Subcommands` (e.g. `/read`, `/status`) are
  unaffected by the new block.

---

## Phase C — Layer 2 argument value completion

**Goal:** Extend `buildCompleter()` with third-token completion using
`ArgCompletion` functions. Add the candidate functions.

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | Add `ArgCompletion` population to each `Command` registration that needs it (see table below). Add candidate functions as package-level helpers. |
| `terminal.go` | In `buildCompleter()`: insert layer 2 block after layer 1 and before the Ollama-model block. |
| `commands_test.go` | Add `TestArgCompletion`: test a few cases (`/rag use <tab>` returns store names, `/memory list <tab>` returns type names). |

### Layer 2 block in `buildCompleter()`

```go
// Layer 2: argument value completion (third token position).
if len(tokens) >= 2 {
    cmdName := strings.ToLower(strings.TrimPrefix(tokens[0], "/"))
    sub := strings.ToLower(tokens[1])
    if cmd, ok := a.commands[cmdName]; ok && cmd.ArgCompletion != nil {
        if fn, ok := cmd.ArgCompletion[sub]; ok {
            candidates := fn(a)
            prefix := strings.ToLower(word)
            var matches []string
            for _, c := range candidates {
                if strings.HasPrefix(strings.ToLower(c), prefix) {
                    matches = append(matches, c)
                }
            }
            sortStrings(matches)
            return matches
        }
    }
}
```

This block runs before the Ollama-model block and before the
file-path switch. Commands that have `ArgCompletion` registered for a
subcommand short-circuit here. Others fall through to existing logic.

### Candidate functions to add

```go
// ragStoreNameCandidates returns the names of all registered RAG stores.
func ragStoreNameCandidates(a *Agent) []string

// memoryTypeCandidates returns all valid MemoryType constant strings.
func memoryTypeCandidates(a *Agent) []string

// memoryIDCandidates returns IDs of all active memories across all types.
// Opens and closes the MemoryStore; must not be called in a hot loop.
func memoryIDCandidates(a *Agent) []string

// llamafileNameCandidates returns names of all registered llamafile models.
func llamafileNameCandidates(a *Agent) []string

// routeNameCandidates returns the names of all registered routes.
func routeNameCandidates(a *Agent) []string

// skillNameCandidates returns the names of all loaded skills.
func skillNameCandidates(a *Agent) []string

// profileTemplateNameCandidates returns template names from ListTemplates.
func profileTemplateNameCandidates(a *Agent) []string
```

### ArgCompletion registrations

| Command | Subcommand | Candidate function |
|---------|------------|--------------------|
| `rag` | `use` | `ragStoreNameCandidates` |
| `rag` | `drop` | `ragStoreNameCandidates` |
| `memory` | `list` | `memoryTypeCandidates` |
| `memory` | `show` | `memoryIDCandidates` |
| `memory` | `forget` | `memoryIDCandidates` |
| `memory` | `flag` | `memoryIDCandidates` |
| `llamafile` | `use` | `llamafileNameCandidates` |
| `llamafile` | `drop` | `llamafileNameCandidates` |
| `route` | `rm` | `routeNameCandidates` |
| `route` | `probe` | `routeNameCandidates` |
| `skill` | `load` | `skillNameCandidates` |
| `skill` | `run` | `skillNameCandidates` |
| `profile` | `use` | `profileTemplateNameCandidates` |

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `/rag use <tab>` returns registered store names.
- `/memory list <tab>` returns memory type names.
- `/memory show tool<tab>` returns IDs prefixed with `tool`.
- File-path completion for `/rag ingest <tab>` is unaffected.

---

## Phase D — Picker fallback in command handlers

**Goal:** Commands that take a NAME argument but receive none show the
`SelectFrom` picker. Uses `SelectFrom` from Phase A and the candidate
functions from Phase C.

### Files to modify

| File | Commands updated |
|------|-----------------|
| `commands.go` | `/rag use`, `/rag drop`, `/memory show`, `/memory forget`, `/memory flag`, `/memory list`, `/route rm`, `/skill load`, `/skill run` |
| `llamafile.go` | `/llamafile use`, `/llamafile drop` |

### Pattern for each command

```go
// Example: /rag use
if len(args) == 0 || args[0] == "" {
    items := ragStoreSelectItems(a)  // []SelectItem with Active flag
    chosen, err := SelectFrom(items, fmt.Sprintf("Select store [1-%d] or Enter to cancel: ", len(items)), a.In, out)
    if err != nil || chosen == "" {
        return err
    }
    args = []string{chosen}
}
// ... existing name-based logic
```

### Helper: `ragStoreSelectItems`

```go
func ragStoreSelectItems(a *Agent) []SelectItem {
    active := a.Config.Memory.ActiveRagStore()
    items := make([]SelectItem, len(a.Config.Memory.RagStores))
    for i, s := range a.Config.Memory.RagStores {
        items[i] = SelectItem{
            Value:  s.Name,
            Label:  s.Name,
            Active: active != nil && active.Name == s.Name,
        }
    }
    return items
}
```

Similar helpers for memory IDs (building `Label = "ID — description"`).

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `/rag use` (no arg) → numbered list of stores → selection works.
- `/rag use existing-name` (with arg) → proceeds directly, no picker.
- `/memory show` (no arg) → picker of memories; selection loads that ID.
- Commands in test mode (non-interactive `a.In`) return `""` without
  blocking (test passes `strings.NewReader("")`).

---

## Phase E — Refactor existing pickers to use `SelectFrom`

**Goal:** Replace the hand-rolled picker loops in `llamafile.go` and
`commands.go` with calls to `SelectFrom`. Behaviour is identical;
code is shorter and consistent.

### Files to modify

| File | Function to refactor |
|------|---------------------|
| `llamafile.go` | `llamafilePickFromRegistered` |
| `llamafile.go` | `llamafilePickFromDir` (retains raw-path passthrough via `SelectFrom`'s raw-string return) |
| `commands.go` | profile template picker inside `cmdMemoryProfileUse` |

### Notes

- `llamafilePickFromDir` currently accepts a typed path when input is
  non-numeric. `SelectFrom` already returns the raw string for
  non-numeric input. The caller checks whether the return value is a
  registered name or a file path.
- The profile picker currently calls `maybeEditTemplate` on the
  chosen template; that call stays in `cmdMemoryProfileUse` after
  `SelectFrom` returns the template name.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- All existing picker tests (in `profile_use_test.go`,
  `llamafile_test.go`) continue to pass without modification.
- `/llamafile use` behaviour is visually identical to before.

---

## Dependency graph

```
Phase A (ui.go — SelectFrom)
    └─► Phase B (Command.Subcommands + layer 1 completion)
            └─► Phase C (ArgCompletion + layer 2 completion)
                    ├─► Phase D (picker fallback in handlers)
                    └─► Phase E (refactor existing pickers)
```

Phases D and E are independent of each other after Phase C completes.

---

## New module dependencies

None. All implementation uses packages already imported: `bufio`,
`fmt`, `io`, `strings`, and the Go standard library.

---

## Open questions

None. Design decisions settled 2026-06-19. Record reversals here as
implementation proceeds.
