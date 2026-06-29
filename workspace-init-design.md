# Workspace Init and Profile Separation — Design

## Context

This document covers two related design decisions made after completing the U1–U3
unified model backend work:

1. **U5-revised** — alias seeding between workspaces via `harvey init <path>`
2. **P1** — workspace profile injection off by default

---

## U5-revised — Alias seeding (`harvey init` / `/workspace init`)

### Problem

The original U5 design proposed a global `~/.config/harvey/harvey.yaml` loaded at
runtime as a base layer, with per-workspace YAML overlaying it. This introduces:

- **Spooky action at a distance**: a globally-defined alias silently affects
  workspace `@mention` routing with no trace in the workspace's own files.
- **Machine-local coupling**: the global file travels with the machine, not the
  work. Workspaces handed off or shared become unpredictable.
- **No audit trail**: nothing in `agents/harvey.yaml` tells you where an alias
  came from.

### Decision

Strict workspace separation. No global config is loaded at runtime. Aliases are
seeded explicitly and become owned by the destination workspace.

The mechanism is a one-time import operation with two entry points:

**CLI (before REPL starts):**
```
harvey init <source>
```
`<source>` is either a workspace directory (reads `agents/harvey.yaml`) or a
standalone `.yaml` / `.yml` file with a `model_aliases:` map. After the import,
Harvey exits — it does not start a session.

**REPL (mid-session):**
```
/workspace init [<source>]
```
With no source, prints workspace info and a tip. With a source path, imports
aliases and saves immediately.

### Source resolution

| Source type | What is read |
|---|---|
| Directory | `<dir>/agents/harvey.yaml` |
| `.yaml`/`.yml` file | The file itself, `model_aliases:` key only |

### Merge strategy

- Source aliases fill **gaps only** — existing aliases are never overwritten.
- After merge, `SaveModelAliases` is called to persist.
- Output: `Imported N aliases (M skipped — already defined)`.

### What is NOT imported

Only `model_aliases` is imported. Purpose tags, LlamafileModels registry,
llamacpp config, security config, session config — none of these cross workspace
boundaries. Aliases were chosen because they are lightweight name→model mappings
with purpose tags, and they are the primary cross-workspace concern.

### CLI: `harvey init` vs `harvey --init-from`

`harvey init <path>` is the preferred form (matches `git init` convention).
The `--init-from` flag approach couples alias seeding to a normal session startup,
which is surprising. The `init` subcommand exits after import with no REPL.

### Implementation

`case "init":` branch in the `os.Args` switch in `cmd/harvey/main.go`. Called
after `NewWorkspace` + `LoadHarveyYAML` so existing aliases are loaded before the
merge, then `ImportAliasesFrom` is called, then `os.Exit(0)`.

---

## P1 — Workspace profile injection off by default

### Problem

`Memory.InjectOnStart` currently defaults to `true`, meaning the workspace profile
(a block of text describing the workspace) is automatically injected into every
session's system context. For small models (≤ 8B parameters), this consumes
precious context tokens that are better used for actual work.

### Decision

Change the default to `false`. The workspace profile is still available:
- `/memory profile show` displays it.
- `memory.inject_on_start: true` in `harvey.yaml` re-enables injection for
  workspaces where the context cost is acceptable.
- `/workspace status` shows whether a profile exists and whether injection is on,
  giving the user visibility without injecting anything.

### `/workspace` command scope

`/workspace` is about workspace identity and initialization — not about filling the
system prompt. The commands are:

| Command | Action |
|---|---|
| `/workspace status` | Root, alias count, profile name (if any), injection status |
| `/workspace init [<path>]` | Seed aliases from source; print tip when no path given |

`/memory profile` remains the management surface for creating, editing, and using
profiles. The two commands do not overlap.

### `/workspace status` output format

```
  Root:      /Users/rsdoiel/myproject
  Aliases:   5 defined
  Profile:   "myproject" (injection off — enable with: memory.inject_on_start: true)
```

When no profile exists:
```
  Root:      /Users/rsdoiel/myproject
  Aliases:   0 defined
  Profile:   none  (create one with /memory profile new)
```

---

## Files to change

| File | Change |
|---|---|
| `config.go` | `InjectOnStart: false` in `DefaultConfig()` |
| `cmd/harvey/main.go` | Replace `--init-from` flag with `case "init":` subcommand |
| `workspace_init.go` | `ImportAliasesFrom` (already drafted) |
| `commands.go` | `/workspace` command: `init` + `status` subcommands; status shows profile info |
| `commands_test.go` or `workspace_init_test.go` | Tests for alias import (no-op when empty, gap-fill, skip existing) |

---

## Acceptance criteria

- `harvey init /other/workspace` imports aliases and exits without starting REPL.
- `harvey init ~/aliases.yaml` imports from a standalone file and exits.
- `harvey init /nonexistent` prints an error and exits non-zero.
- `/workspace init /other/workspace` imports aliases mid-session and saves.
- `/workspace init` (no path) prints info and a tip.
- `/workspace status` shows root, alias count, profile name and injection status.
- `go test ./...` passes.
- `DefaultConfig().Memory.InjectOnStart == false`.
