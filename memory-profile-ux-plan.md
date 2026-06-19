# Harvey Memory Profile UX — Implementation Plan

See [memory-profile-ux-design.md](memory-profile-ux-design.md) for the full
design rationale and decisions.

Phases are ordered so each compiles, passes tests, and delivers standalone
value. Phases A and B are independent; Phase C depends on both.

---

## Phase A — Command Set Expansion (`list`, `show`, `edit`, `rename`)

**Goal:** Add the missing subcommands and standardize naming. All changes are
in `cmdMemoryProfile` and its helpers.

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | Update `cmdMemoryProfile` dispatch; add `cmdMemoryProfileList`, `cmdMemoryProfileShowContent`, `cmdMemoryProfileRename`; keep `update` as deprecated alias for `edit` |
| `helptext.go` | Update `/memory` and `/profile` help text to list all five subcommands |
| `harvey-memory.7.md` | Update man page SUBCOMMANDS section |
| `commands_test.go` | Add tests for `list`, `show` (content), `edit` dispatch, `rename` |

### `cmdMemoryProfile` dispatch

```go
func cmdMemoryProfile(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
    if len(args) == 0 {
        fmt.Fprintln(out, "Usage: /memory profile <list|show|edit|use|rename> [args...]")
        return nil
    }
    switch args[0] {
    case "list":
        return cmdMemoryProfileList(a, args[1:], out, store)
    case "show":
        return cmdMemoryProfileShowContent(a, out, store)
    case "edit":
        return cmdMemoryProfileUpdate(a, out, store) // renamed internally
    case "update": // deprecated alias
        fmt.Fprintln(out, dim("  ⚠  /memory profile update is deprecated; use /memory profile edit"))
        return cmdMemoryProfileUpdate(a, out, store)
    case "use":
        return cmdMemoryProfileUse(a, args[1:], out, store)
    case "rename":
        return cmdMemoryProfileRename(a, args[1:], out, store)
    default:
        fmt.Fprintf(out, "Unknown profile subcommand %q\n", args[0])
        fmt.Fprintln(out, "Usage: /memory profile <list|show|edit|use|rename> [args...]")
        return nil
    }
}
```

### `cmdMemoryProfileList` (replaces old `show` behavior)

Move the existing `show` implementation into a function named `cmdMemoryProfileList`.
Output format unchanged — ID, description, status.

### `cmdMemoryProfileShowContent`

```go
func cmdMemoryProfileShowContent(a *Agent, out io.Writer, store *MemoryStore) error {
    profiles, err := store.List(string(MemoryTypeWorkspaceProfile))
    if err != nil {
        return err
    }
    if len(profiles) == 0 {
        fmt.Fprintln(out, "  No workspace profiles found. Run /profile use to set one.")
        return nil
    }
    active := profiles[0] // List returns active first
    doc, err := store.Load(active.ID)
    if err != nil {
        return fmt.Errorf("profile show: %w", err)
    }
    fmt.Fprintf(out, "\nActive workspace profile: %s (%s)\n\n", active.Description, active.ID)
    fmt.Fprintln(out, strings.Repeat("─", 60))
    fmt.Fprintln(out, string(doc))
    fmt.Fprintln(out, strings.Repeat("─", 60))
    return nil
}
```

If an active RAG store with chunks exists, append a RAG summary line:
```
RAG context: go-source (1,204 chunks, on)
```
This is read from `a.Config` and `a.RagStore`.

### `cmdMemoryProfileRename`

```go
func cmdMemoryProfileRename(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
    if len(args) == 0 {
        fmt.Fprintln(out, "Usage: /memory profile rename NAME")
        return nil
    }
    newName := strings.Join(args, " ")
    profiles, err := store.List(string(MemoryTypeWorkspaceProfile))
    if err != nil || len(profiles) == 0 {
        fmt.Fprintln(out, "  No active workspace profile to rename.")
        return nil
    }
    active := profiles[0]
    doc, err := store.Load(active.ID)
    if err != nil {
        return fmt.Errorf("profile rename: %w", err)
    }
    // Replace the INT. scene heading or TITLE: field.
    updated := rewriteProfileTitle(doc, newName)
    if err := store.SaveWithID(active.ID, updated, MemoryTypeWorkspaceProfile, newName); err != nil {
        return fmt.Errorf("profile rename: %w", err)
    }
    fmt.Fprintf(out, green("✓")+" Workspace renamed to %q\n", newName)
    return nil
}
```

`rewriteProfileTitle(doc []byte, newName string) []byte` — a small helper
that rewrites the `TITLE:` line or the `INT. WORKSPACE PROFILE - ...` line.
Strategy: scan line-by-line; replace the first line matching either pattern;
return the modified content.

`store.SaveWithID` is a new method on `MemoryStore` that overwrites an
existing document by ID rather than creating a new one. This may already
exist as an unexported method; if not, it is a small addition.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `/memory profile list` shows IDs and descriptions.
- `/memory profile show` prints full content of the active profile.
- `/memory profile edit` opens `$EDITOR` (existing behavior, renamed entry point).
- `/memory profile update` prints deprecation warning then behaves like `edit`.
- `/memory profile rename "New Name"` updates the profile's title and the
  description shown by `list` and `/status`.
- All subcommands also work via `/profile <sub>`.

---

## Phase B — Help Text Updates

**Goal:** Update all help text and the man page to reflect the new subcommand
set.

### Files to modify

| File | Change |
|------|--------|
| `helptext.go` | Update `MemoryHelpText` to list all five subcommands with one-line descriptions |
| `harvey-memory.7.md` | Rewrite SUBCOMMANDS section for `/memory profile` |

### Memory help text excerpt (updated)

```
/memory profile list           — list active and archived profiles
/memory profile show           — print the content of the active profile
/memory profile edit           — open the active profile in $EDITOR
/memory profile use [NAME]     — switch to a template or profile
/memory profile rename NAME    — rename the workspace in the active profile

  /profile <sub>               — alias for /memory profile <sub>
```

### Acceptance criteria

- `/help memory` shows the updated subcommand list.
- `harvey help memory` (CLI form) shows the same.
- Man page renders correctly with `pandoc`.

---

## Phase C — RAG Summary in `show` Output

**Goal:** When `/memory profile show` is called and an active RAG store
with chunks exists, append a one-line RAG context summary.

This phase depends on Phase A (the `cmdMemoryProfileShowContent` function).

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | In `cmdMemoryProfileShowContent`, add RAG summary after the profile content block |

### RAG summary logic

```go
// After printing the profile content:
if a.RagStore != nil && a.RagOn {
    n, _ := a.RagStore.ChunkCount()
    if n > 0 {
        storeName := a.Config.ActiveRagStore
        fmt.Fprintf(out, "\nRAG context: %s (%d chunk(s), on)\n", storeName, n)
    }
} else if a.RagStore != nil {
    n, _ := a.RagStore.ChunkCount()
    if n > 0 {
        storeName := a.Config.ActiveRagStore
        fmt.Fprintf(out, "\nRAG context: %s (%d chunk(s), off — /rag on to enable)\n", storeName, n)
    }
}
```

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `/memory profile show` with an active RAG store appends the summary line.
- `/memory profile show` with no active store shows no RAG line.

---

## Dependency Graph

```
Phase A (command set expansion)
    └─► Phase C (RAG summary in show)

Phase B (help text)
    └─► independent; can land before or after A
```

---

## Open Questions

- `store.SaveWithID` — verify whether `MemoryStore` already has a method
  to overwrite a document by ID, or whether it needs to be added. Check
  `memory_store.go` before implementing Phase A.
