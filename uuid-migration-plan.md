# knowledge.db UUID Migration — Implementation Plan

See [uuid-migration-design.md](uuid-migration-design.md) for the full
design rationale and decisions, and `../knowledge_db_merge_design.md`
for how this fits the larger cross-machine-sync sequencing (UUID
migration → SQL/ATTACH merge → module split → JSON-L export, in that
order). This document covers only step 1: the UUID migration inside
this repo's `knowledge.go`.

Verified against the live code on 2026-07-26: no `uuid` column exists on
any table yet, `github.com/google/uuid v1.6.0` is present in `go.mod` but
marked `// indirect` (line 50), and there is no UUID test coverage yet in
`knowledge_test.go`.

Work items are ordered W1 → W6. Per this workspace's TDD-first
convention, W1 (the test) is written and confirmed **red** before any of
W2–W5 are implemented.

---

## W1 — Backfill/idempotency test (red)

**Goal:** Lock in the migration's observable contract before writing it.

### File to modify

`knowledge_test.go` (new test function(s), reusing the existing
`OpenKnowledgeBase`-against-a-temp-file pattern already used elsewhere in
that file).

### Tests to add

- `TestUUIDBackfill_AssignsDistinctUUIDs` — seed a `KnowledgeBase` at the
  pre-migration schema (insert rows into `projects`, `observations`,
  `concepts`, `sources` directly via `kb.db.Exec`, bypassing the `Add*`
  helpers so the row starts with no `uuid`), call `OpenKnowledgeBase`
  again (or just query after the seeding open, since the migration runs
  on every open), and assert every row in all four tables has a non-empty
  `uuid` and that they're pairwise distinct.
- `TestUUIDBackfill_Idempotent` — after the first backfill, capture every
  row's `uuid`, close and reopen the `KnowledgeBase` against the same
  file, and assert the values are byte-for-byte unchanged (guards the
  `WHERE uuid = ''` idempotency guarantee from the design doc).
- `TestUUIDBackfill_PreservesExistingIDsAndFKs` — seed a project +
  observation + concept + an `observation_concepts` link before
  migration; after backfill, assert the integer `id` values and the join
  row are untouched (only `uuid`/`origin_host` columns changed).
- `TestUUIDBackfill_OriginHostSentinel` — assert pre-existing rows get
  `origin_host = "unknown"`, never the current machine's `os.Hostname()`.

Confirm all four fail to compile or fail assertions (the `uuid` column
doesn't exist yet) before starting W2.

---

## W2 — Schema: add `uuid` and `origin_host` columns

### Files to modify

| File | Change |
|---|---|
| `knowledge.go:200-204` (`kbAlterStmts`) | Add four `ALTER TABLE` statements for `projects`, `observations`, `concepts` (uuid + origin_host each) |
| `knowledge.go:258` area (`sourcesSchema` application) | Add a separate two-statement alter for `sources`, run immediately after `db.Exec(sourcesSchema)` succeeds — `sources` doesn't exist yet when the main `kbAlterStmts` batch runs at line 255 |

```go
var kbAlterStmts = []string{
	`ALTER TABLE observations ADD COLUMN source_doi TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE concepts ADD COLUMN identifier_type TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE concepts ADD COLUMN identifier_value TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE projects ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE projects ADD COLUMN origin_host TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE observations ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE observations ADD COLUMN origin_host TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE concepts ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE concepts ADD COLUMN origin_host TEXT NOT NULL DEFAULT ''`,
}

var kbSourcesAlterStmts = []string{
	`ALTER TABLE sources ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN origin_host TEXT NOT NULL DEFAULT ''`,
}
```

In `OpenKnowledgeBase`, right after the existing `db.Exec(sourcesSchema)`
block (line ~261):

```go
for _, stmt := range kbSourcesAlterStmts {
	_, _ = db.Exec(stmt)
}
```

No index yet — that's W4, deliberately after backfill (W3).

### Acceptance criteria

- `go build ./...` succeeds.
- Opening an existing pre-migration `knowledge.db` doesn't error (SQLite
  "duplicate column" errors from `ADD COLUMN` are already swallowed via
  `_, _ = db.Exec(...)`, matching the existing idiom).

---

## W3 — Go-side backfill

### File to modify

`knowledge.go`, new unexported helper called from `OpenKnowledgeBase`
after both alter-statement blocks (W2) have run and before the FTS
block at line 274.

```go
func backfillUUIDs(db *sql.DB, table string) error {
	rows, err := db.Query(`SELECT id FROM ` + table + ` WHERE uuid = ''`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		u, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := db.Exec(
			`UPDATE `+table+` SET uuid = ?, origin_host = 'unknown' WHERE id = ? AND uuid = ''`,
			u.String(), id,
		); err != nil {
			return err
		}
	}
	return nil
}
```

Call for all four tables in `OpenKnowledgeBase`:

```go
for _, t := range []string{"projects", "observations", "concepts", "sources"} {
	if err := backfillUUIDs(db, t); err != nil {
		db.Close()
		return nil, fmt.Errorf("knowledge: backfill uuid on %s: %w", t, err)
	}
}
```

`table` is always one of the four hardcoded literals above — never
user input — so string-concatenating it into the query is safe.

The `AND uuid = ''` in the `UPDATE` (redundant with the `SELECT` filter
under `SetMaxOpenConns(1)`, but cheap insurance) is what makes a second
open a no-op: every row already has a non-empty `uuid`, so the initial
`SELECT` returns nothing and the loop body never runs.

### Acceptance criteria

- W1's `TestUUIDBackfill_AssignsDistinctUUIDs` and
  `TestUUIDBackfill_Idempotent` pass.
- `TestUUIDBackfill_OriginHostSentinel` passes (sentinel, not
  `os.Hostname()`, at backfill time).

---

## W4 — Unique indexes

### File to modify

`knowledge.go`, immediately after the W3 backfill loop in
`OpenKnowledgeBase`.

```go
for _, t := range []string{"projects", "observations", "concepts", "sources"} {
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_` + t + `_uuid ON ` + t + `(uuid)`)
}
```

Placed after backfill completes without error, so the index build never
sees duplicate `''` values.

### Acceptance criteria

- `TestUUIDBackfill_PreservesExistingIDsAndFKs` still passes (index
  creation doesn't touch existing rows).
- Manually confirm via `sqlite3 agents/knowledge.db ".schema projects"`
  that `idx_projects_uuid` etc. exist after a real `harvey` run.

---

## W5 — Insert-path updates

### Files to modify

| File | Function | Change |
|---|---|---|
| `knowledge.go:311` | `AddProject` | Generate `uuid.NewV7()` + `os.Hostname()`, add to `INSERT`/`RETURNING` |
| `knowledge.go:411` | `AddObservationWithSource` | Same, add to `INSERT` |
| `knowledge.go:522` | `AddConceptWithIdentifier` | Same, add to `INSERT ... ON CONFLICT` |
| `knowledge.go:1058` | `AddSource` | Same, add to `INSERT` (the early-return existing-row path at line 1059-1067 keeps that row's original `uuid`/`origin_host`, unchanged — same "silently keeps first" precedent the design doc already accepts) |
| `knowledge.go:1-12` (imports) | Add `"os"` and `"github.com/google/uuid"` |
| `go.mod:50` | Drop the `// indirect` marker on `github.com/google/uuid` (run `go mod tidy` after the code change rather than hand-editing) |

`AddProject`'s existing `ON CONFLICT(name) DO UPDATE` path (line 314-317)
intentionally does **not** touch `uuid`/`origin_host` on conflict — an
existing project keeps its original identity, matching the "first row
wins" merge semantics already decided for name collisions.

```go
u, err := uuid.NewV7()
if err != nil {
	return 0, fmt.Errorf("knowledge: generate uuid: %w", err)
}
host, _ := os.Hostname()
```

`os.Hostname()`'s error is deliberately ignored (falls back to `""`) —
matches the design doc's framing of `origin_host` as
provenance/debugging metadata, not a value anything depends on for
correctness.

### Tests to add

- `TestAddProject_SetsUUIDAndOriginHost`
- `TestAddObservationWithSource_SetsUUIDAndOriginHost`
- `TestAddConceptWithIdentifier_SetsUUIDAndOriginHost`
- `TestAddSource_SetsUUIDAndOriginHost`
- `TestAddProject_ConflictPreservesOriginalUUID` — call `AddProject`
  twice with the same name; assert the `uuid` returned/stored is from
  the *first* call.

### Acceptance criteria

- All new tests pass.
- `go vet ./...` clean (no unused `os`/`uuid` import warnings on any
  path).

---

## W6 — Verify green

```bash
go test ./...
go test -race
go build -o bin/harvey cmd/harvey/*.go
```

Manual sanity check against the real workspace database (back it up
first):

```bash
cp agents/knowledge.db /tmp/knowledge.db.bak
./bin/harvey   # or any command path that calls OpenKnowledgeBase
sqlite3 agents/knowledge.db "SELECT count(*) FROM projects WHERE uuid = '';"   # expect 0
sqlite3 agents/knowledge.db "SELECT count(*) FROM observations WHERE uuid = '';" # expect 0
sqlite3 agents/knowledge.db "SELECT origin_host, count(*) FROM projects GROUP BY origin_host;"
```

### Acceptance criteria

- `go test -race` passes.
- Every row in `agents/knowledge.db` on this machine has a non-empty
  `uuid` after one real run.
- Pre-existing rows show `origin_host = 'unknown'`; nothing shows the
  current machine's real hostname except rows inserted after this
  change.

---

## Out of scope here

The SQL/`ATTACH DATABASE` merge tool, the knowledge-base module
extraction, and JSON-L export are all deferred to later design/plan
documents per the sequencing in `../knowledge_db_merge_design.md` — not
part of this plan.
