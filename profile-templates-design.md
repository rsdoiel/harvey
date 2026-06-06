# Harvey Profile Templates — Design

**Status (2026-06-05):** Design settled. See
[profile-templates-plan.md](profile-templates-plan.md) for the phased
implementation plan.

---

## Motivation

Harvey's current onboarding presents a blank-slate prompt:

```
What should I call you?
What is your role?
Primary language(s) or tools?
Anything else I should know?
```

This is adequate for a user who already knows what they want to tell
a language model. For someone new to LLM tooling, these questions are
too open. They do not know what kind of information changes Harvey's
behaviour, what level of detail is useful, or what a good answer
looks like.

A second problem: new users trying to install Harvey must discover
on their own that Ollama and PDF tools are prerequisites. Harvey
cannot tell them how to fix a failed start if it has no built-in
guidance to offer.

The goal of this design is to:

1. Replace the blank onboarding with **template-driven profile
   selection** — show the user what a good profile looks like before
   they write a word.
2. Support a **progressive personalisation** path: start from a
   template, edit it, refine it over time.
3. Add a **`/profile use`** workflow for switching context mid-session
   without losing conversation continuity.
4. Embed **help guides** for Ollama and PDF tools directly in the
   binary so users get actionable help at the moment they need it.

---

## Principles

**Single binary.** Harvey installs by copying one executable to
`$HOME/bin`. No separate asset directory. Templates and help guides
are compiled into the binary at build time via Go's `//go:embed`
directive.

**Shallow-to-deep curve.** A template gives the user an immediately
useful starting point. Every field in the template is editable, and
`/memory profile update` lets them refine it at any time. The
onboarding flow never blocks progress.

**Workspace-centric.** All runtime state lives under `agents/`.
The embedded templates are read-only defaults. Workspace-local
templates in `agents/templates/` override or extend the built-in set,
allowing organisations to ship their own starting points without
modifying Harvey.

**Same Fountain format.** Profile templates are `.fountain` memory
files — the same format as all other memory documents. No new parser,
no new storage mechanism.

---

## Template Storage

### Embedded templates (built into the binary)

```
harvey/
  templates/
    profiles/
      backend-developer.fountain
      frontend-developer.fountain
      dataset-developer.fountain
      data-scientist.fountain
      technical-writer.fountain
      blank.fountain
    help/
      ollama.md
      pdf-tools.md
      getting-started.md
```

Compiled in via a single directive in a new `templates.go` source
file:

```go
//go:embed templates
var EmbeddedTemplates embed.FS
```

### Workspace-local templates

Any `.fountain` files placed in `agents/templates/profiles/` by the
user or their organisation appear alongside the built-in list.
Workspace-local templates take precedence when names collide.
This is how a team adds a shared "Library Systems Developer" template
without patching Harvey.

---

## Template Format

A profile template is a standard `.fountain` memory document with one
addition: an optional `NOTE:` field that Harvey surfaces at selection
time as a non-enforced recommendation.

```fountain
INT. WORKSPACE PROFILE - TEMPLATE

TITLE: Back End Developer

NOTE: Recommended model: qwen2.5-coder:7b or granite3.3:2b
      RAG: ingest project source and dependency docs

ROLE:
  Back end developer. Primary languages: Go, Python,
  TypeScript (Deno runtime). Uses SQL for application
  data access (Postgres and SQLite3).

PREFERENCES:
  Concise code with no unnecessary comments.
  Prefer stdlib over third-party where reasonable.
  Tests are written alongside implementation.

CONTEXT:
  Edit this section to describe your current project
  and any context Harvey should keep in mind.
```

The `NOTE:` content is printed during template selection and not
injected into the model context. All other fields become the
`workspace_profile` memory document.

---

## Initial Template List (v1)

Five templates ship with Harvey for the first release. Library
roles are deferred pending input from library staff and UX review.

| File | Role |
|------|------|
| `backend-developer.fountain` | Go, Python, TypeScript+Deno, SQL for application work |
| `frontend-developer.fountain` | HTML, CSS, TypeScript/JavaScript, Deno for bundling and transpilation |
| `dataset-developer.fountain` | Front end skills plus SQL, dataset CLI and datasetd web service configuration |
| `data-scientist.fountain` | Data analysis, SQL for exploration, Python data tooling |
| `technical-writer.fountain` | Documentation, man pages, tutorials, Markdown and Fountain formats |
| `blank.fountain` | No pre-filled content; equivalent to current onboarding |

**Library templates (deferred):** A placeholder set of four broad
categories will be defined after consultation with library staff and
UX review. Broad placeholders under consideration:

- `librarian-subject-specialist.fountain`
- `librarian-systems-digital.fountain`
- `librarian-instruction-data-literacy.fountain`
- `library-support-staff.fountain`

---

## Onboarding Flow (revised)

When Harvey detects no `workspace_profile` documents in
`agents/memories/workspace_profile/`, it runs onboarding before
injecting memory context.

**Current flow (replaced):**
```
Harvey: What should I call you? _
```

**New flow:**
```
Harvey: I don't have a workspace profile yet.
        Choose a starting point:

  [1] Back End Developer
  [2] Front End Developer
  [3] Dataset Developer
  [4] Data Scientist
  [5] Technical Writer
  [6] Blank (start from scratch)

  Also available in agents/templates/profiles/ (if any)

> _
```

After selection, Harvey shows the `NOTE:` recommendation (if present)
and opens the template in `$EDITOR`. The user edits and saves; Harvey
writes the result as a `workspace_profile` memory document and
proceeds.

If the user closes the editor without changes, the template is saved
as-is — a reasonable default is always better than an empty profile.

`extractProjectFact` runs immediately after, auto-populating a
`project_fact` document from `codemeta.json`, `go.mod`, or
`package.json` as before.

---

## Profile Switching

### The problem

A user may legitimately want to work in different roles within one
workspace. A data scientist might shift mid-week to writing
documentation, or a developer might switch from back end to dataset
work. The current model has no mechanism for this: `/memory profile
update` edits the existing profile but does not preserve what came
before.

### Solution: `/profile use`

```
/profile use <template-or-profile-name>
```

Steps executed in order:

1. **Write handoff document.** Harvey summarises the current session
   and writes a `.fountain` file to `agents/hand-off/<timestamp>.spmd`.
   The handoff is auto-mined on the next memory mining run, so context
   from the previous role is not lost — it migrates into experience
   memories.

2. **Select new profile.** If a template name is given, open that
   template in `$EDITOR`. If no name is given, show the template
   picker. The resulting document is saved as a new `workspace_profile`
   memory, archiving the old one (not deleting it).

3. **Reset session.** Call `ClearHistory()` which sets
   `memoryContextPending = true`. On the next user prompt, the new
   profile is injected automatically.

4. **Confirm.** Harvey prints:
   ```
   ✓ Switched to [Back End Developer]. Handoff saved to agents/hand-off/.
     Type your first message to continue with the new profile.
   ```

### Naming

`use` is the established verb in Harvey: `/ollama use`, `/rag use`,
`/kb use` all select the active item from a list. `/profile use`
follows the same convention.

---

## `/profile` Alias

`/profile` is registered as a top-level alias that delegates entirely
to `/memory profile`:

```
/profile use <name>     →  /memory profile use <name>
/profile show           →  /memory profile show
/profile update         →  /memory profile update
```

This matches the `/recall` alias pattern (a one-line handler in the
command table). Users who discover memory via `/memory` and users who
reach for `/profile` both arrive at the same functionality.

---

## Help Guides

Two embedded Markdown files, surfaced via `/help`:

### `/help ollama`

- One sentence: what Ollama is.
- Download link (ollama.com).
- The two commands to run: install + `ollama pull <model>`.
- One troubleshooting line: "if Harvey can't connect, make sure
  Ollama is running."

### `/help pdf-tools`

- One sentence: what `pdftotext` is and why Harvey needs it.
- Install commands for all three platforms:
  - macOS: `brew install poppler`
  - Linux: `apt install poppler-utils`
  - Windows: link to poppler Windows build.
- One note: "Harvey works without PDF tools but cannot read PDF
  files."

Both guides are also printed proactively: if Ollama is unreachable at
startup, Harvey prints a short pointer to `/help ollama`. If a PDF
read fails, Harvey prints a pointer to `/help pdf-tools`.

---

## Relationship to Existing Memory Architecture

Profile templates feed directly into the existing three-silo memory
architecture. Nothing in the retrieval or injection path changes.

| Template field | Becomes | Injected as |
|----------------|---------|-------------|
| `ROLE:`, `PREFERENCES:`, `CONTEXT:` | `workspace_profile` memory doc | Always, score 1.0, first |
| Auto-extracted workspace metadata | `project_fact` memory doc | Always, score 1.0, second |
| Handoff document from `/profile use` | `.fountain` in `agents/hand-off/` | After mining: experience memory |

---

## Out of Scope

- **Bundled RAG stores per template.** Templates recommend a RAG
  strategy via the `NOTE:` field but do not ship pre-built indexes.
  Maintaining indexes for six roles across three OS / two CPU
  architectures is not sustainable.
- **Global profile across workspaces.** Harvey is workspace-centric.
  A profile in workspace A has no effect on workspace B.
- **Profile versioning / diff.** The archive mechanism (old profile
  is archived, not deleted) provides a basic history. Full versioning
  is deferred.
- **Library role templates (v1).** Defined after UX review with
  library staff.
