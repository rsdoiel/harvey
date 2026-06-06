# Harvey Profile Templates — Implementation Plan

See [profile-templates-design.md](profile-templates-design.md) for
the full design rationale and decisions.

Phases are ordered so each one compiles, passes tests, and delivers
standalone value. No phase depends on a later phase being started.

---

## Phase A — Embedded Template Infrastructure

**Goal:** Compile templates and help guides into the binary via
`//go:embed`. Provide functions to list and load them. No UX change
yet — this is the foundation the later phases build on.

### Files to create

| File | Purpose |
|------|---------|
| `templates.go` | `//go:embed templates` declaration, `EmbeddedTemplates embed.FS` var, `ListTemplates() []TemplateEntry`, `LoadTemplate(name string) ([]byte, error)`, `LoadHelpGuide(name string) ([]byte, error)` |
| `templates/profiles/backend-developer.fountain` | Back end developer template |
| `templates/profiles/frontend-developer.fountain` | Front end developer template |
| `templates/profiles/dataset-developer.fountain` | Dataset/datasetd developer template |
| `templates/profiles/data-scientist.fountain` | Data scientist template |
| `templates/profiles/technical-writer.fountain` | Technical writer template |
| `templates/profiles/blank.fountain` | Minimal template (equivalent to current onboarding) |
| `templates/help/ollama.md` | Ollama install guide |
| `templates/help/pdf-tools.md` | PDF tools install guide |
| `templates/help/getting-started.md` | First-run orientation |

### Files to modify

| File | Change |
|------|--------|
| `helptext.go` | Add `OllamaHelpText`, `PDFToolsHelpText` constants (loaded from embedded guides at init or on demand). Add `"ollama"` and `"pdf-tools"` to the `/help` topic list. |
| `commands.go` | In `cmdHelp`, dispatch `"ollama"` and `"pdf-tools"` to the embedded guide text. |

### `TemplateEntry` type

```go
type TemplateEntry struct {
    Name        string  // display name, e.g. "Back End Developer"
    File        string  // filename without path, e.g. "backend-developer.fountain"
    Source      string  // "builtin" or "workspace"
    Recommended string  // content of NOTE: field, or ""
}
```

`ListTemplates` merges built-in templates with any `.fountain` files
found in `agents/templates/profiles/` in the current workspace.
Workspace-local entries appear after built-ins; same-name workspace
entry shadows the built-in.

### `TemplateNoteField` helper

Extract the `NOTE:` block from a `.fountain` file for display during
selection without parsing the full document:

```go
func TemplateNoteField(content []byte) string
```

Reads lines between `NOTE:` and the next all-caps scene heading or
end of file. Returns empty string if no `NOTE:` field present.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `ListTemplates()` returns all six built-in templates.
- `LoadTemplate("backend-developer")` returns the file contents.
- `LoadHelpGuide("ollama")` returns the Ollama guide text.
- `/help ollama` and `/help pdf-tools` print the embedded guides.
- Adding a `.fountain` file to `agents/templates/profiles/` causes
  `ListTemplates()` to include it (integration test with temp dir).

---

## Phase B — Onboarding Upgrade

**Goal:** Replace the blank-question onboarding with a template
picker. The user selects a template (or blank), optionally edits it,
and Harvey saves it as the `workspace_profile` memory. All subsequent
logic (project_fact extraction, memory injection) is unchanged.

### Files to modify

| File | Change |
|------|--------|
| `memory_onboarding.go` | Replace `RunOnboarding` implementation. New flow: call `ListTemplates()`, display numbered list with `NOTE:` recommendations, read selection, open chosen template in `$EDITOR`, save result via `store.Save()`. Keep `NeedsOnboarding` and `extractProjectFact` unchanged. |
| `memory_onboarding_test.go` | Update `RunOnboarding` tests. Add test: selecting template 1 in a mock terminal produces a `workspace_profile` document whose content matches the template. Add test: selecting blank produces a minimal document. |
| `helptext.go` | Update onboarding-related help text to mention templates. |

### Template selection display

```
Harvey: I don't have a workspace profile yet. Choose a starting point:

  [1] Back End Developer
      Recommended: qwen2.5-coder:7b · ingest project source and deps
  [2] Front End Developer
  [3] Dataset Developer
  [4] Data Scientist
  [5] Technical Writer
  [6] Blank (no pre-filled content)

  (workspace templates from agents/templates/profiles/ listed after
   built-ins if present)

Select [1-N] or press Enter for Blank: _
```

If the terminal is non-interactive (replay or pipe), skip the picker
and use `blank.fountain` silently — same behaviour as today.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- First run in a workspace with no `workspace_profile` memories shows
  the picker.
- Selecting a numbered template opens it in `$EDITOR`.
- Saving the editor result writes a `workspace_profile` memory and
  skips onboarding on all subsequent starts.
- Non-interactive mode skips the picker without error.

---

## Phase C — `/profile use` Command and `/profile` Alias

**Goal:** Add profile switching with handoff preservation and the
top-level `/profile` alias.

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | Add `"use"` case to `cmdMemoryProfile` dispatch. Implement `cmdMemoryProfileUse(a, args, out, store)`: write handoff doc, run template picker or load named template, archive old profile, save new profile, call `a.ClearHistory()`. Register `"profile"` in the top-level command table as an alias delegating to `cmdMemory(a, append([]string{"profile"}, args...), out)`. |
| `harvey.go` | Add `writeHandoff(a *Agent, store *MemoryStore) (string, error)` — writes a brief `.fountain` summary of the current session to `agents/hand-off/<timestamp>.spmd`. The summary is Harvey's last N assistant messages collapsed to bullet points (no LLM call required; purely structural). |
| `sessions_files.go` | Ensure `agents/hand-off/` is created alongside `agents/sessions/` at workspace init if it does not exist. |
| `helptext.go` | Add `profile use` to `/memory` help text. Add `/profile` to the top-level command list and to `/help` dispatch. Document the handoff mechanism and the archive behaviour. |

### Handoff document format

```fountain
INT. HAND-OFF - 2026-06-05T14:32:00Z

HARVEY
  Profile switched from [previous profile name] to [new profile name].

NOTE:
  Last topics: <bullet list from assistant messages>
  Files touched: <paths mentioned in last 10 turns>
  Open questions: <user messages ending in ?>
```

This is intentionally lightweight — no LLM call, no blocking. The
memory miner can extract richer facts from it in a later session.

### Archive behaviour

When `/profile use` saves a new `workspace_profile`, the previous
profile document is updated in the store with `status: archived`
(same mechanism as `/memory forget`). It remains visible in
`/memory list workspace_profile --all` but is excluded from active
injection. This preserves history without cluttering the active
memory set.

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `/profile use backend-developer` writes a handoff file to
  `agents/hand-off/`, archives the old `workspace_profile`, saves the
  new one, and resets history.
- `/profile use` with no argument shows the template picker.
- `/profile show` lists active `workspace_profile` documents.
- `/profile update` opens the most recent profile in `$EDITOR`.
- All three subcommands work equally via `/memory profile <sub>`.
- `agents/hand-off/` directory is created at workspace init.

---

## Phase D — Proactive Help Triggers

**Goal:** Print a short pointer to the relevant help guide when a
prerequisite failure is detected at startup or during a command.

### Files to modify

| File | Change |
|------|--------|
| `terminal.go` | At startup, if the Ollama health check fails, print: `  Run /help ollama for installation instructions.` |
| `pdf_extract.go` | If `pdftotext` is not found or returns an error, include a pointer: `  Run /help pdf-tools for installation instructions.` |
| `helptext.go` | Ensure `/help getting-started` exists and covers both prerequisites. |

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- Starting Harvey when Ollama is not running prints the Ollama help
  pointer alongside the existing connection error.
- Attempting to read a PDF when `pdftotext` is absent prints the
  PDF tools help pointer.
- `/help ollama`, `/help pdf-tools`, and `/help getting-started` all
  print their guides without error.

---

## Phase E — Status Shows Active Profile

**Goal:** `/status` includes the name of the active workspace profile
so the user knows which context Harvey is working from.

### Files to modify

| File | Change |
|------|--------|
| `commands.go` | In `cmdStatus`, query the memory store for the most recent active `workspace_profile` document and print its title in the Memory section: `Profile: Back End Developer (workspace_profile_a1b2c3)`. Print `Profile: (none — run /profile use to set one)` when absent. |

### Acceptance criteria

- `go build ./...` and `go test ./...` pass.
- `/status` shows the active profile name when one exists.
- `/status` shows a helpful nudge when none exists.

---

## Phase F — Library Role Templates (deferred)

**Goal:** Add library-oriented profile templates after UX review with
library staff.

**Blocked on:** Consultation with library staff and UX colleague to
define the right role categories and content.

**Placeholder categories under consideration:**

| File | Role |
|------|------|
| `librarian-subject-specialist.fountain` | Subject librarian (general) |
| `librarian-systems-digital.fountain` | Library systems: FOLIO, ArchiveSpace, InvenioRDM, EPrints |
| `librarian-instruction-data-literacy.fountain` | Teaching, Data Carpentry, information literacy |
| `library-support-staff.fountain` | Circulation, patron services, general support |

No implementation work begins until the role definitions are agreed.

---

## Dependency Graph

```
Phase A (embedded template infrastructure)
    └─► Phase B (onboarding upgrade)
    └─► Phase C (/profile use + alias)
    └─► Phase D (proactive help triggers)
    └─► Phase E (status shows profile)

Phase F (library templates)
    └─► blocked on UX review; can follow any phase after A
```

Phases B, C, D, and E each depend only on Phase A and are independent
of each other. They can be developed in any order after Phase A is
complete.

---

## New Module Dependencies

None. `embed` is in the Go standard library (Go 1.16+). All other
implementation uses packages already imported by Harvey.

---

## Open Questions

None outstanding as of 2026-06-05. Record any new decisions or
reversals here as implementation proceeds.
