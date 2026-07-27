# `experiments` → `projects` Data Migration — Design

**Status (2026-07-27):** Decisions confirmed. See
[experiments-migration-plan.md](experiments-migration-plan.md) for the
phased (W1–W6) implementation plan.

**References:**
- `../knowledge_db_merge_design.md` — umbrella cross-machine sync design.
  This document is a newly-discovered prerequisite for that sequencing's
  step 2 (SQL/ATTACH merge), found while running a real `bin/kbmerge`
  against macmini-rd.local's actual `knowledge.db`.
- `DECISIONS.md`, 2026-07-27 entries — the audit trail that led here
  (UUID migration correction, `concepts.created_at` fix, this finding).

---

## Motivation, corrected

The original framing (in `TODO.md` and the first draft of this thread) was
"harvey renamed `experiments` to `projects` at some point and never
migrated existing databases." **That's wrong — worth stating plainly since
it changes the right fix.** Checked directly:

```
git log --diff-filter=A --oneline --all -- knowledge.go   →  5b1d802 (3rd commit in harvey's whole history)
git log --all -p -- knowledge.go | grep -c experiment      →  0
```

`harvey/knowledge.go` has used `projects`/`project_id` since its very first
commit. It has never contained the word "experiment." The `experiments`
schema exists only in root `CLAUDE.md`'s Knowledge Base section (which
documents `experiments`/`experiment_concepts`/`experiment_summary` and
gives example `sqlite3` commands using `experiment_id`) — a description of
an *earlier, hand-run* convention for `agents/knowledge.db`, predating
`harvey`'s SQL-backed implementation, never updated after `harvey` took
over as the tool that actually reads and writes the file. This matches an
existing memory (`knowledge_db_schema_stale.md`, filed 2026-07-05) that
already flagged the docs as stale — this audit shows it's not just docs:
one real database is still living proof of the older convention.

**Confirmed scope:** checked every `knowledge.db` reachable from this
machine. Only **macmini-rd.local's** has `experiments` tables:

| Database | `experiments` tables present? |
|---|---|
| `~/Laboratory/agents/knowledge.db` (wren, live) | No |
| `harvey/agents/knowledge.db` (wren) | No |
| `harvey/.harvey/knowledge.db` (wren) | No |
| `macmini-rd.local-agents/knowledge.db` (copy of macmini's live db) | **Yes** |

So this is not a general harvey defect that will recur on every new
workspace — it's a one-time bridge needed for whichever machine(s) still
carry the pre-`harvey` hand-authored schema. Worth writing generally
(detect-and-migrate, no-op if absent) rather than a one-off script, in case
another machine (or a future one) turns out to have the same shape — but
the blast radius today is exactly one file.

## Current schema (macmini-rd.local, verified 2026-07-27)

```sql
CREATE TABLE experiments (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    name     TEXT NOT NULL,
    status   TEXT NOT NULL DEFAULT 'concept',
    language TEXT,
    repo_url TEXT
);
CREATE TABLE observations (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    experiment_id INTEGER NOT NULL REFERENCES experiments(id),
    kind          TEXT NOT NULL,
    body          TEXT NOT NULL,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    source_doi TEXT NOT NULL DEFAULT '', uuid TEXT NOT NULL DEFAULT '', origin_host TEXT NOT NULL DEFAULT ''
);
CREATE TABLE experiment_concepts (
    experiment_id INTEGER NOT NULL REFERENCES experiments(id),
    concept_id    INTEGER NOT NULL REFERENCES concepts(id),
    PRIMARY KEY (experiment_id, concept_id)
);
-- plus experiment_summary, a VIEW joining the three above
```

`observations` here has **no `project_id` column at all** — not "empty",
genuinely absent, because `CREATE TABLE IF NOT EXISTS` (in `harvey/knowledge.go`'s
`schema` const) is a no-op against a table that already exists under any
shape. This is why `bin/kbmerge` fails with `no such column: o.project_id`
rather than a join producing zero rows.

`projects` on macmini today has exactly **one** row (`henry`, inserted at
some point after `harvey` started being used there — 0 observations
attached). `experiments` holds the real historical content:

| id | name | status | language | repo_url | observations (real count) |
|---|---|---|---|---|---|
| 1 | harvey | active | Go | `https://github.com/rsdoiel/Laboratory` | — |
| 2 | sparqlset | active | Go | `github.com/rsdoiel/sparqlset` | — |
| 3 | henry | active | bash, make | (empty) | — |
| 4 | audiobox | active | Go, TypeScript | `git@github.com:rsdoiel/audiobox.git` | — |

(93 observations and 35 concepts total across these four, per the
2026-07-27 audit; exact per-experiment split not yet queried.)

**The name collision:** `henry` exists in *both* `experiments` (id=3, real
data) and `projects` (id=1, empty stub) on the same database. This is the
central case the migration must get right — the two decisions below follow
directly from it.

## Decisions (draft — need your confirmation)

1. **Detect, don't assume.** Guard the whole migration on
   `SELECT name FROM sqlite_master WHERE type='table' AND name='experiments'`.
   If absent, the migration step is a complete no-op — every database that
   only ever went through `harvey`'s `OpenKnowledgeBase` (i.e. everything
   except macmini's) is unaffected, with zero risk of the migration path
   ever running against data it wasn't designed for.

2. **`observations.project_id` needs an `ALTER`, unconditionally.** Add
   `ALTER TABLE observations ADD COLUMN project_id INTEGER REFERENCES projects(id)`
   to `kbAlterStmts` (no default needed — it's nullable, no non-empty-table
   restriction applies to a plain nullable column add). This is safe to run
   on every database unconditionally, same as every other `kbAlterStmts`
   entry: on databases that already have `project_id` (i.e. all of them
   except macmini's), SQLite returns "duplicate column name", already
   silently ignored by the existing `_, _ = db.Exec(stmt)` pattern.

3. **Match `experiments.name` to an existing `projects.name` row; create
   one only if no match exists.** This is what correctly resolves the
   `henry` collision: macmini's real `henry` observations end up attached
   to the *existing* `henry` projects row (id=1) rather than creating a
   second, duplicate "henry" project. For `harvey`, `sparqlset`, `audiobox`
   — no existing `projects` row shares that name on macmini today — a new
   `projects` row is created for each, via the existing `AddProject`
   codepath (so it gets a real `uuid`/`origin_host` like any other project).

4. **Translate `observations.experiment_id` → `project_id` via the name
   mapping from (3), for every row where `project_id IS NULL`.** Idempotent
   by construction: a second run finds no rows left to update. Symmetric
   step for `experiment_concepts` → `project_concepts`, via `INSERT OR
   IGNORE` (the composite primary key already dedups a second run).

5. **Leave `experiments`/`experiment_concepts`/`experiment_summary` in
   place, permanently, never dropped.** They become inert once (4) has run
   — nothing in `harvey` reads them today, and nothing will start doing so
   after this migration. Rejected: `DROP TABLE` — no correctness reason to
   remove them (they cost nothing sitting unused), and dropping is
   one-way while every other choice here is reversible by re-running the
   migration logic or, worst case, restoring from a backup. This also
   sidesteps `observations.experiment_id`'s `NOT NULL REFERENCES
   experiments(id)` constraint entirely — no `ALTER`/`DROP COLUMN`
   dance needed on a column nothing will read again.

6. **`experiments.language`/`experiments.repo_url` fold into the new
   project's `description`.** `projects` has no equivalent columns
   (only `name`/`description`/`status`), so for each *newly-created*
   project only (`harvey`, `sparqlset`, `audiobox` — `henry` already
   exists as a `projects` row and keeps its own `description`, untouched),
   format as `"<language> — <repo_url>"` (omit either side if empty; if
   both are empty, `description` stays `""`) and pass it as `AddProject`'s
   `description` argument. This preserves the information rather than
   silently dropping it, without inventing a new schema column for data
   that only exists on one legacy database.

## Testing (per this workspace's TDD-first convention)

Mirrors `TestOpenKnowledgeBase_BackfillsLegacyConceptsCreatedAt`'s
approach: build a raw legacy-shape database in a test (the exact DDL
above), seed it with a case that exercises the collision (`henry` present
in both `experiments` and `projects`) and a case that doesn't (`harvey`,
present only in `experiments`), then open it via `OpenKnowledgeBase` and
assert:
- Exactly one `henry` row remains in `projects` (no duplicate created),
  and the migrated `henry` observations' `project_id` points at it.
- A new `harvey` row is created in `projects`, and its migrated
  observations' `project_id` points at the new row.
- Every `observations` row that had a non-null `experiment_id` now also
  has a non-null `project_id`.
- Idempotent: re-running `OpenKnowledgeBase` a second time changes nothing
  (no duplicate projects, no duplicate `project_concepts` rows, same
  `project_id` values).
- `experiments`/`experiment_concepts` tables still exist and are unchanged
  (decision 5) — migration only adds rows elsewhere, never touches or
  drops the legacy tables.

## Known edge case for continuity

Only after this migration completes on macmini (and gets applied to its
real live `agents/knowledge.db`, not just the copy on `wren`) does a real
`bin/kbmerge -a <wren> -b <macmini>` run become possible without hitting
`no such column: o.project_id`. That was the original trigger for this
whole design (see `DECISIONS.md`, 2026-07-27, "concepts.created_at" entry).
