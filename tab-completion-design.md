# Harvey Tab Completion — Design

**Status (2026-06-19):** Design draft.
See [tab-completion-plan.md](tab-completion-plan.md) for the phased
implementation plan.

---

## Motivation

Harvey already has a `buildCompleter()` function (`terminal.go:1511`)
wired to the `LineEditor`. It handles four cases today:

| Case | What triggers it |
|------|-----------------|
| Top-level commands | First token starts with `/` |
| Route `@names` | First token starts with `@` |
| Ollama model names | `/ollama use` or `/ollama probe` |
| Workspace file paths | `/read`, `/write`, `/attach`, `/rag ingest`, etc. |

What is missing:

1. **Layer 1 — subcommand names.** Typing `/memory <tab>` produces
   nothing. The user must remember all subcommands by heart.

2. **Layer 2 — argument values.** Typing `/rag use <tab>` produces
   nothing. The user must know the exact store name or look it up with
   `/rag list` first.

3. **Inconsistent picker UX.** Several commands already show a
   numbered picker when no name is given (`/llamafile use`, `/profile
   use`, `/llamafile add`). Each reimplements the pattern differently.
   Other commands that take a name from a finite list (`/rag use NAME`,
   `/memory show ID`) fail silently or print an error instead.

The goal of this design is to fix all three consistently.

---

## Guiding Principles

**Finite, dynamic lists use the picker.** Any subcommand argument that
comes from a list Harvey can enumerate (store names, model names,
memory IDs) gets a numbered select list when the argument is omitted.
Free-text arguments (`/rag query TEXT`, `/plan TASK`) never get a
picker.

**Tab completion is the fast path; the picker is the fallback.** A
user who knows the name types it directly (or tab-completes it). A
user who doesn't know the name presses Enter without an argument and
gets the picker.

**One implementation, used everywhere.** A shared `SelectFrom` helper
in `ui.go` replaces all duplicated picker code. Existing pickers
(`llamafilePickFromRegistered`, the profile picker) are refactored to
call it.

**No termlib changes yet.** `ui.go` lives in the `harvey` package. If
a clean generalisation emerges after a few uses, it can be promoted to
`termlib` via the standard design → plan → decision process.

---

## Architecture

### 1. `SelectItem` and `SelectFrom` (`ui.go`)

```go
// SelectItem is one choosable option in a select list.
type SelectItem struct {
    Value  string // returned on selection; used for tab completion
    Label  string // display text; may be longer than Value
    Active bool   // when true, rendered with a "→" marker
}

// SelectFrom presents a numbered list and returns the chosen Value.
// If items contains exactly one entry it is returned without prompting.
// Empty input or out-of-range input returns ("", nil) — treat as cancel.
func SelectFrom(items []SelectItem, prompt string, in io.Reader, out io.Writer) (string, error)

// SelectFromStrings is a convenience wrapper where Label == Value.
func SelectFromStrings(items []string, prompt string, in io.Reader, out io.Writer) (string, error)
```

**Display format (produced by `SelectFrom`):**

```
  →  [1] harvey       ← active item
     [2] project-docs
     [3] meeting-notes

Select [1-3] or press Enter to cancel:
```

The `→` marker is rendered only when `Active == true`. Items without
the marker are indented by the same width (four spaces) so columns
align.

**Cancellation:** empty input, `q`, or `0` all return `("", nil)`.
The caller decides whether cancellation is an error or a no-op.

---

### 2. `Command` struct extension (`commands.go`)

Two new fields added to `Command`:

```go
type Command struct {
    Usage       string
    Description string
    UserDefined bool
    Handler     func(a *Agent, args []string, out io.Writer) error

    // Subcommands lists the valid subcommand tokens for tab completion.
    // Empty for commands that take no subcommand (e.g. /read, /status).
    Subcommands []string

    // ArgCompletion maps each subcommand name to a function that returns
    // candidate values for its first positional argument.
    // Called by buildCompleter at tab time; must be fast (no LLM calls).
    ArgCompletion map[string]func(*Agent) []string
}
```

`Subcommands` and `ArgCompletion` are populated in `registerCommands()`
alongside the existing `Usage`, `Description`, and `Handler` fields.

---

### 3. `buildCompleter()` changes (`terminal.go`)

The completer is extended with two new cases inserted **before** the
existing file-path switch:

**Layer 1 — subcommand names (second token):**

```
/memory <tab>   →  mine list show flag forget status recall profile
/rag <tab>      →  list new use drop ingest status query on off
/ollama <tab>   →  start stop status list ps run pull push show create cp rm logs use env
/llamafile <tab>→  add use list start status drop
/kb <tab>       →  status search inject project observe concept
/skill <tab>    →  list load info status new run
/route <tab>    →  add rm models probe set list on off status
/plan <tab>     →  next status show clear  (and free text for task name)
/session <tab>  →  continue replay
```

**Layer 2 — argument values (third token):**

Completion candidates are returned by `ArgCompletion[subcommand](a)`.
The return value is a flat `[]string`; `buildCompleter` prefix-filters
it against the word being typed, exactly as it does for model names
today.

Layer 2 fires only when `len(tokens) == 3` (or `len(tokens) == 2`
with a trailing space). It short-circuits before the existing
file-path switch so that commands with registered `ArgCompletion` get
their structured list first; commands without `ArgCompletion` fall
through to path completion as today.

---

### 4. Candidate functions

These are small package-level functions (not methods) so they can be
stored in `ArgCompletion` without a method expression.

| Function | Returns |
|----------|---------|
| `ragStoreNameCandidates(a)` | `cfg.Memory.RagStores[*].Name` |
| `memoryTypeCandidates(a)` | all `ValidMemoryTypes` constants |
| `memoryIDCandidates(a)` | IDs of active memories (opens store, lists, closes) |
| `llamafileNameCandidates(a)` | `cfg.LlamafileModels[*].Name` |
| `ollamaModelCandidates(a)` | reuses existing `modelAndAliasCandidates` |
| `routeNameCandidates(a)` | `a.Routes.Endpoints` keys |
| `skillNameCandidates(a)` | compiled skill names from `a.SkillSet` |
| `profileTemplateNameCandidates(a)` | template names from `ListTemplates` |

`memoryIDCandidates` opens and closes the store; all others are
in-memory reads. None makes an Ollama or network call.

---

### 5. Picker fallback in command handlers

Commands where the first positional argument is a NAME from a finite
list adopt this pattern:

```go
func cmdRagUse(a *Agent, args []string, out io.Writer, cfg *Config) error {
    name := ""
    if len(args) > 0 {
        name = args[0]
    }
    if name == "" {
        items := ragStoreSelectItems(a)  // []SelectItem with Active flag set
        var err error
        name, err = ui.SelectFrom(items, "Select store: ", a.In, out)
        if err != nil || name == "" {
            return err
        }
    }
    // ... proceed with name
}
```

The `ragStoreSelectItems` helper builds `[]SelectItem` with `Active`
set on the currently-active store. The same helper is called by both
the picker fallback and any future display commands.

#### Commands that gain a picker fallback

| Command + subcommand | Picker shows | Candidate function |
|----------------------|--------------|--------------------|
| `/rag use` | registered store names | `ragStoreNameCandidates` |
| `/rag drop` | registered store names | `ragStoreNameCandidates` |
| `/llamafile use` | registered model names | `llamafileNameCandidates` |
| `/llamafile drop` | registered model names | `llamafileNameCandidates` |
| `/memory list` | memory type names | `memoryTypeCandidates` |
| `/memory show` | memory ID + description | `memoryIDCandidates` |
| `/memory forget` | memory ID + description | `memoryIDCandidates` |
| `/memory flag` | memory ID + description | `memoryIDCandidates` |
| `/profile use` | template names | `profileTemplateNameCandidates` |
| `/route rm` | registered route names | `routeNameCandidates` |
| `/skill load` | skill names from set | `skillNameCandidates` |
| `/skill run` | skill names from set | `skillNameCandidates` |

#### Commands that do NOT get a picker fallback (free text or paths)

| Command | Reason |
|---------|--------|
| `/rag ingest PATH` | file path — uses existing path completer |
| `/rag query TEXT` | free text |
| `/rag new NAME` | user-chosen name, no existing list |
| `/ollama use MODEL` | already handled by `modelAndAliasCandidates` |
| `/ollama pull MODEL` | free text (model registry lookup would need Ollama) |
| `/plan TASK` | free text task description |
| `/search PATTERN DIR` | free text + path |

---

### 6. Refactoring existing pickers

`llamafilePickFromRegistered`, `llamafilePickFromDir`, and the profile
template picker in `cmdMemoryProfileUse` each implement the numbered
list loop manually. Phase E refactors all three to call `SelectFrom`,
keeping their behaviour identical but removing the duplicated code.

`llamafilePickFromDir` is a special case: it also accepts a typed
file path if the user enters something that is not a number. That
behaviour is preserved by `SelectFrom`'s design — if the user input is
non-numeric and non-empty, `SelectFrom` returns the raw input string
unchanged. The caller validates it as a path.

---

## `ui.go` full API (exported symbols)

```go
// SelectItem is one option in a numbered select list.
type SelectItem struct {
    Value  string
    Label  string
    Active bool
}

// SelectFrom presents a numbered list and returns the chosen Value.
func SelectFrom(items []SelectItem, prompt string, in io.Reader, out io.Writer) (string, error)

// SelectFromStrings is a convenience wrapper where Label == Value.
func SelectFromStrings(items []string, prompt string, in io.Reader, out io.Writer) (string, error)
```

Internal helpers (unexported):
- `renderSelectList(items []SelectItem, out io.Writer)` — prints the numbered list
- `parseSelectInput(line string, n int) (idx int, raw string)` — parses user input; idx is 1-based, raw is the untouched string when input is non-numeric

---

## What stays unchanged

- The `LineEditor` and `termlib` API are not touched.
- Existing tab completion for file paths, `@routes`, and Ollama model
  names is unchanged. The new layers are inserted before the path
  switch and fall through naturally.
- Command handler signatures (`func(*Agent, []string, io.Writer) error`)
  are unchanged. The picker fallback is internal to each handler.
- `harvey.yaml` is not changed; no new config fields are needed.

---

## Out of scope

- **Multi-match cycling.** When Tab produces more than one match,
  `termlib/lineeditor.go` already renders a completion menu inline
  (or inserts the common prefix). That behaviour is not changed here.
- **Context-aware deep completion.** Completing `/memory show`
  argument values requires opening the memory store, which is fast
  enough. Completing model names that require an Ollama round-trip is
  out of scope; those commands get tab-only candidates from the local
  cache.
- **Promotion to termlib.** If `SelectFrom` turns out to be useful in
  other tools, move it to `termlib` via the standard design → plan →
  decision process.
- **Fuzzy matching.** All completion is prefix-only for now.
  Fuzzy/substring matching can be added later without changing the API.
