# Harvey Web Developer Template — Implementation Plan

See [web-developer-template-design.md](web-developer-template-design.md) for
the full design rationale.

This is a single-phase change: add one `.spmd` file to `templates/profiles/`.
No code changes are required — `ListTemplates()` discovers all `.spmd` files
in that directory automatically.

---

## Phase A — Add `web-developer.spmd`

**Goal:** Add the web developer profile template to the embedded template set.

### Files to create

| File | Purpose |
|------|---------|
| `templates/profiles/web-developer.spmd` | Web developer profile template |

### Template content

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
  in use, and any constraints Harvey should keep in mind (e.g.
  "API must stay compatible with v1", "SQLite3 only, no Postgres
  in this project", "no external JS dependencies").
```

### Acceptance criteria

- `go build ./...` and `go test ./...` pass (binary recompilation picks up
  the new embedded file via `//go:embed templates`).
- `ListTemplates()` returns an entry with `Name == "Web Developer"` and
  `File == "web-developer.spmd"`.
- The onboarding picker shows "Web Developer" as an option.
- `/profile use web-developer` opens the template in `$EDITOR`.
- The `templates_test.go` test for `ListTemplates` either already passes
  with the new file (if it counts entries by index) or needs a count update.

### Test update

If `templates_test.go` asserts a specific count of built-in templates,
update the expected count from 6 to 7.

---

## No Dependency

This phase has no dependencies on other open work items. It can be committed
before or after any other change.

---

## Open Questions

None as of 2026-06-18.
