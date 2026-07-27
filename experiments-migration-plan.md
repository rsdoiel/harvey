# `experiments` → `projects` Data Migration — Implementation Plan

See [experiments-migration-design.md](experiments-migration-design.md) for
the full audit, rationale, and confirmed decisions. This document covers
only the migration inside `knowledge.go` — no `bin/kbmerge` changes, no
root `CLAUDE.md` edits (not asked for), no dropping of legacy tables.

Verified against the live code on 2026-07-27 (line numbers below):
`kbAlterStmts` at `knowledge.go:203-218`, the alter-loop call site at
`knowledge.go:309-311`, the UUID-backfill loop at `knowledge.go:334-340`,
`kb := &KnowledgeBase{...}` at `knowledge.go:341`. `AddProject` at
`knowledge.go:379-404` (`name, description string) (int64, error)`).

Work items ordered W1 → W6. Per this workspace's TDD-first convention, W1
(the tests) is written and confirmed **red** before W2–W4 are implemented.

---

## W1 — Migration tests (red)

### File to modify

`knowledge_test.go` — new test function(s), following the same raw-DDL
seeding pattern as `TestOpenKnowledgeBase_BackfillsLegacyConceptsCreatedAt`.

### Helper

A small local helper to build the exact legacy shape from the design doc's
"Current schema" section: `experiments`, `experiment_concepts`, and an
`observations` table with `experiment_id` (no `project_id` column) —
opened via raw `sql.Open("sqlite", dbPath)`, not `OpenKnowledgeBase`, so
the seed reflects a genuinely pre-migration file.

### Tests to add

- `TestOpenKnowledgeBase_MigratesExperimentsWithoutCollision` — seed
  `experiments` with one row (`name='harvey', status='active',
  language='Go', repo_url='https://github.com/rsdoiel/Laboratory'`), one
  `observations` row linked via `experiment_id`, no `projects` row named
  `harvey`. After `OpenKnowledgeBase`: assert a new `projects` row named
  `harvey` exists with `description = "Go — https://github.com/rsdoiel/Laboratory"`;
  assert the observation's `project_id` now equals that new project's `id`.
- `TestOpenKnowledgeBase_MigratesExperimentsWithCollision` — seed
  `experiments` with `henry` (real data: one observation, one
  `experiment_concepts` link) **and** seed `projects` with an existing
  `henry` row (empty, as found on macmini). After `OpenKnowledgeBase`:
  assert exactly one `henry` row remains in `projects` (no duplicate);
  assert the migrated observation's `project_id` points at the
  *pre-existing* `henry` project's `id`, not a new one; assert
  `project_concepts` gained the translated link.
- `TestOpenKnowledgeBase_MigrationIdempotent` — run the two seeds above,
  open once, capture every `projects.id`/`project_id` value touched, close
  and reopen against the same file, assert nothing changed (no duplicate
  projects, no duplicate `project_concepts` rows, same `project_id`
  values).
- `TestOpenKnowledgeBase_MigrationLeavesLegacyTablesIntact` — after
  migration, assert `experiments` and `experiment_concepts` still exist
  with their original row counts and column values unchanged (decision 5:
  never dropped, never rewritten).
- `TestOpenKnowledgeBase_NoExperimentsTableIsNoOp` — open a normal,
  already-current-schema `KnowledgeBase` (via `openTestKB(t)`, no
  `experiments` table at all) and assert `OpenKnowledgeBase` succeeds with
  no error and no `experiments`-related side effects (guards decision 1:
  the common case must stay a true no-op).

Confirm all fail (mostly on `no such column: o.project_id` or missing
migrated rows) before starting W2.

---

## W2 — Schema: add `observations.project_id`

### File to modify

`knowledge.go:203-218` (`kbAlterStmts`) — add one line:

```go
var kbAlterStmts = []string{
	`ALTER TABLE observations ADD COLUMN source_doi TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE observations ADD COLUMN project_id INTEGER REFERENCES projects(id)`,
	// ... existing entries unchanged
}
```

Nullable, no default clause needed (not a non-constant-default case —
plain `NULL` is always a valid ADD COLUMN default). Safe to run
unconditionally: on every database that already has `project_id` (i.e.
everything except a legacy `experiments`-shaped file), SQLite's "duplicate
column name" error is already silently swallowed by the existing
`_, _ = db.Exec(stmt)` loop at `knowledge.go:309-311`.

### Acceptance criteria

- `go build ./...` succeeds.
- `TestOpenKnowledgeBase_NoExperimentsTableIsNoOp` passes (this change
  alone shouldn't break the common no-`experiments` case).

---

## W3 — Go-side migration function

### File to modify

`knowledge.go` — new unexported function, called from `OpenKnowledgeBase`
after the W2 alter loop and after the UUID-backfill loop
(`knowledge.go:334-340`) has run, since newly-created projects need real
UUIDs and the migration relies on `AddProject`'s normal insert path for
that (see design decision 3). Call it before `kb := &KnowledgeBase{...}`
(`knowledge.go:341`), so a migration failure can still `db.Close()` and
return an error the same way the UUID-backfill block above it does.

```go
// migrateExperimentsToProjects bridges a pre-harvey, hand-authored
// agents/knowledge.db (experiments/experiment_concepts/experiment_id,
// documented historically in the Laboratory root CLAUDE.md, predating
// this package) onto the current projects/project_id schema. No-op if
// the legacy experiments table doesn't exist. Idempotent: a second call
// finds no unmigrated rows.
func migrateExperimentsToProjects(kb *KnowledgeBase) error {
	var hasExperiments int
	kb.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='experiments'`,
	).Scan(&hasExperiments)
	if hasExperiments == 0 {
		return nil
	}

	rows, err := kb.db.Query(`SELECT id, name, status, language, repo_url FROM experiments`)
	if err != nil {
		return err
	}
	type legacyExperiment struct {
		id                    int64
		name, language, repo string
	}
	var experiments []legacyExperiment
	for rows.Next() {
		var e legacyExperiment
		var status sql.NullString
		if err := rows.Scan(&e.id, &e.name, &status, &e.language, &e.repo); err != nil {
			rows.Close()
			return err
		}
		experiments = append(experiments, e)
	}
	rows.Close()

	for _, e := range experiments {
		var projectID int64
		err := kb.db.QueryRow(`SELECT id FROM projects WHERE name = ?`, e.name).Scan(&projectID)
		if err == sql.ErrNoRows {
			desc := strings.TrimSpace(strings.Join(
				[]string{e.language, e.repo}, " — "))
			// strings.Join always inserts " — " even with one side empty;
			// normalize the two-empty and one-empty cases explicitly.
			switch {
			case e.language == "" && e.repo == "":
				desc = ""
			case e.language == "":
				desc = e.repo
			case e.repo == "":
				desc = e.language
			}
			projectID, err = kb.AddProject(e.name, desc)
			if err != nil {
				return fmt.Errorf("knowledge: migrate experiment %q: %w", e.name, err)
			}
		} else if err != nil {
			return err
		}

		if _, err := kb.db.Exec(
			`UPDATE observations SET project_id = ? WHERE experiment_id = ? AND project_id IS NULL`,
			projectID, e.id,
		); err != nil {
			return fmt.Errorf("knowledge: migrate observations for experiment %q: %w", e.name, err)
		}

		if _, err := kb.db.Exec(
			`INSERT OR IGNORE INTO project_concepts (project_id, concept_id)
			 SELECT ?, concept_id FROM experiment_concepts WHERE experiment_id = ?`,
			projectID, e.id,
		); err != nil {
			return fmt.Errorf("knowledge: migrate concepts for experiment %q: %w", e.name, err)
		}
	}
	return nil
}
```

Call site (right before `kb := &KnowledgeBase{db: db, path: dbPath}` at
`knowledge.go:341`):

```go
kbTmp := &KnowledgeBase{db: db, path: dbPath}
if err := migrateExperimentsToProjects(kbTmp); err != nil {
	db.Close()
	return nil, fmt.Errorf("knowledge: migrate experiments: %w", err)
}
```

(`migrateExperimentsToProjects` takes `*KnowledgeBase` rather than `*sql.DB`
so it can call `kb.AddProject` directly instead of duplicating its
UUID/origin_host/FTS-index logic — construct the real `kb` value one
statement earlier than today rather than introducing a second, temporary
wrapper type.)

### Acceptance criteria

- `TestOpenKnowledgeBase_MigratesExperimentsWithoutCollision` and
  `TestOpenKnowledgeBase_MigratesExperimentsWithCollision` pass.
- `TestOpenKnowledgeBase_MigrationLeavesLegacyTablesIntact` passes (this
  function only ever reads `experiments`/`experiment_concepts`, never
  writes to or drops them).

---

## W4 — Idempotency

No new code expected — idempotency should fall out of W3's guards
(`WHERE name = ?` lookup before insert, `WHERE project_id IS NULL` on the
`UPDATE`, `INSERT OR IGNORE` on the composite PK). This step is purely
verification:

### Acceptance criteria

- `TestOpenKnowledgeBase_MigrationIdempotent` passes.
- If it doesn't on the first attempt, the likely gap is the `UPDATE
  observations SET project_id = ? WHERE experiment_id = ? AND project_id
  IS NULL` guard not actually preventing a second no-op run — check that
  before adding new state-tracking machinery.

---

## W5 — Verify against the real copy

Manual, not automated — this exercises the actual file, not a synthetic
seed.

```bash
cp macmini-rd.local-agents/knowledge.db /tmp/macmini-knowledge.db.bak
go run ./cmd/migrateonce   # throwaway trigger program, same pattern used
                           # earlier this session for the created_at fix —
                           # write, run once, delete; confirm git status
                           # clean afterward
sqlite3 macmini-rd.local-agents/knowledge.db "SELECT name FROM projects;"
# expect: audiobox, harvey, henry, sparqlset (henry pre-existing, three new)
sqlite3 macmini-rd.local-agents/knowledge.db \
  "SELECT COUNT(*) FROM observations WHERE experiment_id IS NOT NULL AND project_id IS NULL;"
# expect: 0
sqlite3 macmini-rd.local-agents/knowledge.db ".tables"
# expect: experiments/experiment_concepts/experiment_summary still present
```

Then re-attempt the real cross-machine merge that motivated this whole
thread:

```bash
go build -o bin/kbmerge ./cmd/kbmerge
./bin/kbmerge -a agents/knowledge.db -b macmini-rd.local-agents/knowledge.db \
  -out /tmp/kbmerge-real-test.db -force
```

### Acceptance criteria

- The migration output matches the expectations above.
- `bin/kbmerge` gets past `observations` (the blocker this design exists
  to remove) — whatever it does next (succeed, or surface a further
  distinct issue) is genuinely new information, not a re-occurrence of
  `no such column: o.project_id`.

---

## W6 — Verify green and document

```bash
go vet ./...
go test ./...
```

(`go test -race` remains blocked on this machine by the pre-existing
Raspberry Pi ThreadSanitizer VMA-width issue noted in the 2026-07-26
`DECISIONS.md` entries — not a new gap introduced here.)

Log the outcome in `DECISIONS.md` (new entry, same convention as the
`concepts.created_at` entry), and check off the relevant `TODO.md` item
once macmini's **live** `agents/knowledge.db` (not just the copy on wren)
has actually been migrated — that's a separate, explicit action over SSH,
not implied by this plan finishing.

---

## Out of scope here

- Applying this migration to macmini's real, live `agents/knowledge.db`
  (only the copy gets exercised in W5) — a deliberate, separate step, same
  as how the `concepts.created_at` fix was scoped.
- Updating root `CLAUDE.md`'s stale Knowledge Base section (documents
  `experiments`/`experiment_*` as current) — not asked for.
- Re-running `bin/kbmerge` against macmini's real live file, or actually
  placing a merged database into position on either machine.
- The knowledge-base module extraction and JSON-L export steps from
  `../knowledge_db_merge_design.md` — unrelated to this fix.
