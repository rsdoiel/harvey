# knowledge.db UUID Migration — Design

**Status (2026-07-26):** Design confirmed, ready for TDD implementation.
See [uuid-migration-plan.md](uuid-migration-plan.md) for the phased
(W1–W6) implementation plan.

**References:**
- `../knowledge_db_merge_design.md` — the umbrella design for
  cross-machine `knowledge.db` sync (this document's parent). Covers
  the full sequencing: UUID migration (this doc) → SQL/ATTACH merge →
  knowledge-base module extraction → JSON-L export (deferred).
- `../knowledge_db_jsonl_export_design.md` — deferred sibling, not in
  scope here.

---

## Motivation

`agents/knowledge.db` exists independently on at least two machines
(`macmini-rd.local`, `wren.local`) and is not checked into git. Every
table (`projects`, `observations`, `concepts`, `sources`) uses a plain
autoincrement integer primary key with no cross-machine identity, so
the two databases drift with no way to reconcile them: `.dump`-and-load
either collides on ids or silently cross-wires foreign keys in the join
tables, since the same integer id means a different row on each
machine.

This document scopes the fix that unblocks everything downstream of
it: give every row a stable, globally-unique identifier at creation
time, so a future merge becomes an idempotent set-union
(`INSERT OR IGNORE` keyed on `uuid`) instead of best-effort content
matching.

## Scope

In scope: `harvey/knowledge.go` only — schema, backfill, and the four
insert paths (`AddProject`, `AddObservationWithSource`,
`AddConceptWithIdentifier`, `AddSource`).

Out of scope (deferred to later design/plan documents per the
sequencing in `../knowledge_db_merge_design.md`):
- The SQL/`ATTACH DATABASE` merge tool itself.
- Extracting `knowledge.go` into its own standalone Go module.
- JSON-L export/import.

## Current schema (verified 2026-07-26)

| Table | PK | Notes |
|---|---|---|
| `projects` | `id INTEGER PRIMARY KEY AUTOINCREMENT` | `name TEXT NOT NULL UNIQUE` |
| `observations` | `id INTEGER PRIMARY KEY AUTOINCREMENT` | FK `project_id → projects(id)` |
| `concepts` | `id INTEGER PRIMARY KEY AUTOINCREMENT` | `name TEXT NOT NULL UNIQUE` |
| `sources` | `id INTEGER PRIMARY KEY AUTOINCREMENT` | partial unique index on `(identifier_type, identifier_value)` |
| `observation_concepts`, `project_concepts`, `observation_sources` | composite PK of two FK ids | pure join tables — no identity of their own needed |

`concepts.name` and `projects.name` already have real uniqueness
constraints; `observations` and `sources` (without an identifier) do
not — nothing in the schema reliably identifies "the same row created
independently on two machines."

## Decisions

1. **`uuid TEXT NOT NULL DEFAULT ''`** added to `projects`,
   `observations`, `concepts`, `sources` — not the join tables, which
   don't need their own identity (a future merge translates their FKs
   through the parent rows' `uuid`s directly in SQL).
2. **UUID version 7** uniformly across all four tables.
   `github.com/google/uuid` v1.6.0 is already a `go.mod` dependency
   (currently marked `// indirect`) and supports `uuid.NewV7()`.
   Time-ordered ids are a free bonus; no downside for internal ids.
3. **Backfill in Go, not SQL** — SQLite has no built-in UUID generator.
   Mirrors the existing one-time `source_doi → sources` backfill
   already in `OpenKnowledgeBase`: naturally idempotent via a
   `WHERE uuid = ''` guard, so every subsequent open is a no-op.
4. **Unique index only after backfill** — `ADD COLUMN` sets every
   existing row to the same `''` default at once; an index created
   before backfill completes would fail immediately on the first
   duplicate.
5. **`origin_host TEXT NOT NULL DEFAULT ''`**, added the same way, for
   provenance/debugging. Existing rows backfill to an explicit
   `"unknown"` sentinel — never the current machine's `os.Hostname()`,
   which would misattribute every pre-migration row (including ones
   actually created on the other machine) to whoever runs the
   migration first. Only new rows going forward stamp the real
   hostname, at insert time.
6. **Insert-path changes preserve first-write-wins on conflict** —
   `AddProject`'s existing `ON CONFLICT(name) DO UPDATE` path does not
   touch `uuid`/`origin_host` on conflict, so a project's identity is
   fixed at its first creation, consistent with how the merge design
   already treats `concepts.description` conflicts (silently keeps the
   first-inserted value).

## Testing

Per this workspace's TDD-first convention: write the backfill and
idempotency tests before touching `OpenKnowledgeBase` or any `Add*`
function. See `uuid-migration-plan.md` W1 for the specific test list.
Confirm red, then implement W2–W5 in order, confirm green in W6.

## Known edge case (downstream, noted here for continuity)

Not fixed by this migration, but relevant to why it's shaped this way:
the `harvey` project row (and likely others) was created independently
on both machines *before* any of this existed, so it already has two
different values for what's logically "the same" row. The eventual
merge tool needs a pre-merge collision report for `projects.name` /
`concepts.name` rather than silently trusting `INSERT OR IGNORE` to
pick the "right" one — full detail in `../knowledge_db_merge_design.md`.
