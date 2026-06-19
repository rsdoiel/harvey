# Harvey Web Developer Template — Design

**Status (2026-06-18):** Design settled. See
[web-developer-template-plan.md](web-developer-template-plan.md) for the
implementation plan.

---

## Motivation

The five profile templates shipped in v1 cover:

| Template | Stack focus |
|---|---|
| `backend-developer` | Go, Python, TypeScript+Deno, SQL |
| `frontend-developer` | HTML, CSS, TypeScript/JavaScript, Deno bundling |
| `dataset-developer` | Front end + SQL, dataset CLI and datasetd |
| `data-scientist` | Data analysis, SQL, Python |
| `technical-writer` | Documentation, Markdown, Fountain |

A developer building web applications in the Laboratory workspace uses *all*
of these stacks simultaneously:

- **Go** — HTTP servers, middleware, API handlers (`net/http`, `database/sql`)
- **uv + Python** — scripts, data processing, automation
- **SQL** — SQLite3 for application data, Postgres for shared/production data
- **Deno + TypeScript** — frontend logic, build scripts, server-side TypeScript
- **JavaScript** — browser-native ES modules, no framework default
- **CSS** — custom properties, no utility framework default
- **HTML5** — semantic markup

The closest match is `backend-developer`, but that template emphasizes server
architecture and does not mention the front-end layers. The `frontend-developer`
template covers HTML/CSS/JS but omits Go and SQL. Neither gives Harvey the
full picture needed to assist across a full-stack session.

---

## Template Design

### Name

`web-developer` — concise, describes the role without implying a specific
framework or language. The display name is "Web Developer".

### NOTE field

Shown during onboarding and template selection, not injected into context:

```
NOTE: Recommended model: qwen2.5-coder:7b or granite3.3:2b
      RAG: ingest Go source, deno.json, package.json, SQL schema files
      Style: prefer stdlib and web platform APIs; avoid heavy frameworks
```

### Full template content

```fountain
INT. WORKSPACE PROFILE - WEB DEVELOPER

TITLE: Web Developer

NOTE: Recommended model: qwen2.5-coder:7b or granite3.3:2b
      RAG: ingest Go source, deno.json/package.json, SQL schema files
      Style: prefer stdlib and web platform APIs; avoid heavy frameworks

ROLE:
  Full-stack web developer. Backend: Go (net/http, database/sql,
  standard library first). Frontend: Deno + TypeScript (standard library,
  no bundler by default), vanilla JavaScript (ES modules), CSS (custom
  properties), HTML5 (semantic markup). Data: SQL — SQLite3 dialect for
  embedded/application data, Postgres for shared services. Scripts and
  automation: uv-managed Python.

PREFERENCES:
  Go: minimal imports, stdlib over third-party, idiomatic error returns.
  TypeScript/Deno: use Deno standard library (jsr:@std/*); no npm by
  default. Type everything; avoid `any`.
  JavaScript: ES modules, no class-based OOP unless the pattern fits.
  CSS: custom properties for theming; no utility framework (Tailwind etc.)
  unless the project already uses one.
  HTML: semantic elements, ARIA attributes for interactive components.
  SQL: write explicit column lists; avoid SELECT *; use transactions for
  writes. SQLite3 dialect unless otherwise specified.
  Python: managed via uv; prefer standard library; type hints on public
  functions.
  Tests: written alongside implementation. Go table-driven tests.
  TypeScript: Deno's built-in test runner (Deno.test).

CONTEXT:
  Edit this section to describe your current project, the tech stack
  in use, and any constraints Harvey should keep in mind (e.g. "API
  must stay compatible with v1", "SQLite3 only, no Postgres in this
  project", "no external JS dependencies").
```

### Fields and rationale

**ROLE** — gives the model a clear mental model of the developer's scope.
The explicit mention of each language with its primary library prevents the
model from defaulting to Node.js, npm, or framework-heavy approaches.

**PREFERENCES** — per-language style rules. These are the most common points
of friction when a general-purpose model assists a developer who has strong
opinions about stdlib-first, no-framework, and idiomatic usage.

**CONTEXT** — left blank for user to fill in. The placeholder text suggests
what to include without prescribing it. The comment examples cover the most
common reasons a developer would need to override the defaults.

---

## Integration with Existing Template System

No code changes are required. `ListTemplates()` discovers all `.spmd` files
in `templates/profiles/` automatically. Adding `web-developer.spmd` to that
directory is sufficient.

The onboarding picker will show a seventh option. The display order is
alphabetical within the built-in set, so "Web Developer" will appear after
"Technical Writer".

---

## Out of Scope

- **Library-role templates** — deferred pending consultation with library
  staff. See the original profile-templates-plan.md Phase F.
- **Framework-specific variants** — a `web-developer-react.spmd` or similar
  is useful for teams with a fixed stack, but outside the scope of this
  workspace-general template.
