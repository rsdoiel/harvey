# Harvey Memory Profile UX — Design

**Status (2026-06-18):** Design settled. See
[memory-profile-ux-plan.md](memory-profile-ux-plan.md) for the phased
implementation plan.

---

## Motivation

The `/memory profile` command set has grown organically and now has three
problems:

1. **Naming inconsistency.** `show` lists profile IDs (like `list` does
   everywhere else in Harvey). Users who type `/memory profile show`
   expecting to see the *content* of their current profile instead see a
   terse ID list. This contradicts Harvey's established vocabulary where
   `show <id>` displays a document's full content.

2. **Missing `show` (content display).** There is no command to print
   the full text of the active workspace profile. The user must run
   `/memory show <id>` after first running `/memory profile show` to
   find the ID — a two-step process for a common action.

3. **Missing `edit` and `rename`.** The only way to modify the current
   profile is `/memory profile update`, which opens `$EDITOR`. A user
   who wants to rename the workspace (the description field in the
   profile document) has no direct command.

---

## Revised Command Set

### Summary table

| Subcommand | Semantics | Was |
|---|---|---|
| `list` | List all profiles (active + archived) with IDs and descriptions | `show` (old) |
| `show` | Print the full content of the *current active* profile document | (missing) |
| `edit` | Open the active profile in `$EDITOR` and re-save on close | `update` |
| `use [NAME]` | Switch to a named template or show picker | unchanged |
| `rename NAME` | Update the description field of the active profile | (missing) |

`update` is kept as a deprecated alias for `edit`. When called, it prints:
```
  ⚠  /memory profile update is deprecated; use /memory profile edit
```
then proceeds identically to `edit`.

The `/profile` top-level alias continues to work for all subcommands:
```
/profile list
/profile show
/profile edit
/profile use [name]
/profile rename NAME
```

---

## Subcommand Specifications

### `list`

Replaces the current `show` behavior.

```
/memory profile list
```

Output format (same as current `show`):
```
Active workspace profiles:

  workspace_profile_a1b2c3  Back End Developer      (active)
  workspace_profile_d4e5f6  Technical Writer        (archived 2026-06-10)
```

If `--all` is passed, archived profiles are included (same as today).
If no profiles exist, print:
```
  No workspace profiles found. Run /profile use to set one.
```

### `show`

Displays the full content of the most recent active `workspace_profile`
document — the text that is injected into the LLM context at session start.

```
/memory profile show
```

Output:
```
Active workspace profile: Back End Developer (workspace_profile_a1b2c3)

─────────────────────────────────────────────
INT. WORKSPACE PROFILE - BACK END DEVELOPER

ROLE:
  Back end developer. Primary languages: Go, Python,
  TypeScript (Deno runtime). Uses SQL for application
  data access (Postgres and SQLite3).

PREFERENCES:
  Concise code with no unnecessary comments.
  …
─────────────────────────────────────────────
```

If no active profile exists, print the same nudge as `list`.

### `edit`

Opens the active profile in `$EDITOR` (same as current `update`).

```
/memory profile edit
```

After the user saves and closes the editor, the updated content replaces the
existing profile document in the store. If the user closes the editor without
changes, a message confirms no change was made.

### `use [NAME]`

Unchanged from the current implementation. Switches to a named template or
shows the interactive picker. Writes a handoff document before switching.

### `rename NAME`

Updates the description line in the active profile document. The description
appears in `list` output and in `/status` (the "Profile:" field).

```
/memory profile rename "Harvey Web Developer"
```

The rename is a targeted string replacement in the stored Fountain document:
it finds the `TITLE:` line (or the `INT.` scene heading) and updates it. The
profile's ID and content are otherwise unchanged. No new document is
created — this is an in-place edit.

If no active profile exists, print an error.

---

## Memory/RAG Unified View

The TODO notes that users think of memory and RAG settings as going together.
This design does not merge the two — they remain separate silos with separate
commands — but acknowledges the confusion with two UX touches:

1. `/memory profile show` will include a brief RAG summary at the bottom if
   a RAG store is active:

   ```
   RAG context: go-source (1,204 chunks, on)
   ```

   This surfaces the most relevant RAG state alongside the profile without
   mixing the storage models.

2. `/hint` (existing command) already aggregates memory and RAG state. No
   change needed there.

A deeper unification — e.g., a `/workspace` command that shows profile,
active RAG stores, active memory count, and KB status together — is left as
a future design exercise once user feedback clarifies what information is
most commonly needed at a glance.

---

## Workspace Rename

`/memory profile rename NAME` targets the description embedded in the profile
Fountain document. Specifically, it rewrites the `TITLE:` or `INT.` line in
the stored `.spmd` file.

The current format of a profile document's opening lines is:
```
INT. WORKSPACE PROFILE - BACK END DEVELOPER
```

After `rename "Web Developer"`:
```
INT. WORKSPACE PROFILE - WEB DEVELOPER
```

The memory store's `Description` field for the record is derived from this
heading at load time, so `list` and `/status` pick up the new name
automatically without requiring a store migration.

---

## Help Text Updates

The `/memory` help text is updated to list all five subcommands:

```
/memory profile list           — list active and archived profiles
/memory profile show           — print the content of the active profile
/memory profile edit           — open the active profile in $EDITOR
/memory profile use [NAME]     — switch to a template or profile
/memory profile rename NAME    — rename the workspace in the active profile
```

The man page `harvey-memory.7.md` is updated to match.

---

## Out of Scope

- **Profile versioning / diff** — deferred. The archive mechanism preserves
  history without a full diff UI.
- **Global `/workspace` command** — deferred pending user feedback on what
  information is most useful at a glance.
- **Merging RAG and memory toggles** — the silos serve different retrieval
  strategies; merging them into a single toggle would lose retrieval precision
  for small models.
