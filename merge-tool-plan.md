# knowledge.db Cross-Machine Merge Tool — Implementation Plan

See [merge-tool-design.md](merge-tool-design.md) for the full design and
function signatures. Work items are ordered M1 → M8, TDD-first per this
workspace's convention — each work item's test(s) are written and
confirmed red before its implementation.

All new code lives in `knowledge_merge.go` (package `harvey`) and
`knowledge_merge_test.go`, plus `cmd/kbmerge/main.go` (M7, untested by
`go test` — see M8 for its manual verification).

---

## M1 — `CollisionReport`

**Goal:** Detect the known pre-migration edge case (same `name`, two
different `uuid`s) before any merge runs.

### Tests to add

- `TestCollisionReport_DetectsNameUUIDMismatch` — build two
  `KnowledgeBase`s at distinct temp paths, `AddProject("harvey", ...)` on
  both (distinct real uuids since each `AddProject` call generates its
  own), call `CollisionReport(aPath, bPath)`, assert one `NameCollision{
  Table: "projects", Name: "harvey", ... }` comes back with `UUIDA !=
  UUIDB`.
- `TestCollisionReport_NoCollisionWhenUUIDsMatch` — same name inserted on
  both via `AddProject`, but manually `UPDATE`d on `b` to share `a`'s
  uuid (simulating a row already reconciled by a prior merge) — assert
  zero collisions.
- `TestCollisionReport_ConceptsAlsoChecked` — same as the first test but
  for `AddConcept`.
- `TestCollisionReport_EmptyWhenNoOverlap` — two KBs with disjoint
  project/concept names — assert empty result, no error.

### Implementation

```go
func CollisionReport(aPath, bPath string) ([]NameCollision, error) {
	db, err := sql.Open("sqlite", aPath) // any valid path opens the driver; ATTACH does the real work
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.Exec(`ATTACH DATABASE ? AS b`, bPath); err != nil {
		return nil, fmt.Errorf("knowledge: attach %s: %w", bPath, err)
	}
	var out []NameCollision
	for _, table := range []string{"projects", "concepts"} {
		rows, err := db.Query(
			`SELECT main.name, main.uuid, b.` + table + `.uuid
			 FROM main.` + table + ` JOIN b.` + table + ` USING(name)
			 WHERE main.uuid != b.` + table + `.uuid`,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c NameCollision
			c.Table = table
			if err := rows.Scan(&c.Name, &c.UUIDA, &c.UUIDB); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, c)
		}
		rows.Close()
	}
	return out, nil
}
```

`aPath` doubles as the `main` schema simply by opening it directly —
no third scratch file needed for a read-only report. `bPath` is the only
one that needs an explicit `ATTACH`.

### Acceptance criteria

- All four tests pass.
- Calling `CollisionReport` never modifies either input file (read-only
  `SELECT`s only — no `INSERT`/`UPDATE`/`DELETE` anywhere in this path).

---

## M1.5 — `ReconcileCollisions` (added 2026-07-26, caught during M7 smoke-testing)

**Goal:** Fix a real data-loss bug found by manually running `cmd/kbmerge`
end-to-end: resolving a collision by "first wins" also silently drops
every child row (observations, links) attached to the losing side's row,
not just the duplicate row itself, because those children have no merged
parent to `uuid`-join against once it's gone. See "Collision handling"
in `merge-tool-design.md` for the full writeup.

### Tests to add

- `TestReconcileCollisions_RewritesUUIDToMatchA` — collide two projects
  named `"harvey"`; reconcile; assert `b`'s row now carries `a`'s uuid,
  and a follow-up `CollisionReport` finds nothing.
- `TestReconcileCollisions_PreservesChildObservationsAfterMerge` — same
  collision, each side with one observation on the colliding project;
  reconcile, then merge; assert the merged db has exactly 1 project and
  **both** observations (previously only 1 survived).

### Implementation

```go
func ReconcileCollisions(bPath string, collisions []NameCollision) error {
	if len(collisions) == 0 {
		return nil
	}
	db, err := sql.Open("sqlite", bPath)
	if err != nil {
		return fmt.Errorf("knowledge: open %s: %w", bPath, err)
	}
	defer db.Close()
	for _, c := range collisions {
		if _, err := db.Exec(
			`UPDATE `+c.Table+` SET uuid = ? WHERE name = ? AND uuid = ?`,
			c.UUIDA, c.Name, c.UUIDB,
		); err != nil {
			return fmt.Errorf("knowledge: reconcile %s %q: %w", c.Table, c.Name, err)
		}
	}
	return nil
}
```

`c.Table` is always `"projects"` or `"concepts"` — the two literals
`CollisionReport` hardcodes — never external input, so the concatenation
is safe.

### Acceptance criteria

- Both tests pass.
- `cmd/kbmerge -force` on two databases with a colliding project (each
  with its own observation) produces a merge containing both
  observations, not just one — reconfirmed via manual smoke test after
  wiring M7 to call this.

---

## M2 — `MergeKnowledgeBases` skeleton: fresh target + attach

**Goal:** The scaffolding every later work item builds on: reject an
existing `mergedPath`, create a fresh fully-migrated one, attach both
sources.

### Tests to add

- `TestMergeKnowledgeBases_RejectsExistingMergedPath` — create an empty
  file at `mergedPath`, call `MergeKnowledgeBases`, assert an error and
  that the file is untouched (still zero bytes / not a valid sqlite
  file).
- `TestMergeKnowledgeBases_CreatesMigratedSchema` — call with two empty
  (freshly-`AddProject`-free) source KBs, assert `mergedPath` exists
  afterward and — opened as a plain `KnowledgeBase` — `Projects()`
  returns an empty (not nil) slice, i.e. the schema is fully applied and
  queryable.

### Implementation

```go
func MergeKnowledgeBases(aPath, bPath, mergedPath string) ([]MergeTableSummary, error) {
	if _, err := os.Stat(mergedPath); err == nil {
		return nil, fmt.Errorf("knowledge: merge target %s already exists", mergedPath)
	}
	ws, err := NewWorkspace(os.TempDir())
	if err != nil {
		return nil, err
	}
	kb, err := OpenKnowledgeBase(ws, mergedPath)
	if err != nil {
		return nil, err
	}
	kb.Close()

	db, err := sql.Open("sqlite", mergedPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.Exec(`ATTACH DATABASE ? AS a`, aPath); err != nil {
		return nil, fmt.Errorf("knowledge: attach %s: %w", aPath, err)
	}
	if _, err := db.Exec(`ATTACH DATABASE ? AS b`, bPath); err != nil {
		return nil, fmt.Errorf("knowledge: attach %s: %w", bPath, err)
	}

	// M3–M6 insert into db here.

	if _, err := db.Exec(`DETACH DATABASE a`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`DETACH DATABASE b`); err != nil {
		return nil, err
	}
	db.Close()

	kb2, err := OpenKnowledgeBase(ws, mergedPath) // triggers FTS rebuild, W3 backfill no-op
	if err != nil {
		return nil, err
	}
	defer kb2.Close()

	// M6 also returns the summary collected before DETACH.
	return nil, nil
}
```

### Acceptance criteria

- Both tests pass.
- `go vet ./...` clean (no unused `db` connection paths).

---

## M3 — Parent tables (`projects`, `concepts`, `sources`)

**Goal:** Copy with explicit column lists (never `id`), deduped by
`uuid`/`name`.

### Tests to add

- `TestMergeKnowledgeBases_ProjectsUnion` — `a` has project "one", `b`
  has project "two" (distinct uuids); assert merged has both, with
  fresh (not copied) ids.
- `TestMergeKnowledgeBases_ProjectsDedupSharedUUID` — same project row
  (by uuid) present in both `a` and `b` (simulate: `AddProject` on `a`,
  then manually `INSERT` an identical-uuid row into `b`); assert merged
  has exactly one row for it.
- `TestMergeKnowledgeBases_ConceptsAndSourcesUnion` — same shape, one
  test covering both remaining parent tables.

### Implementation (inside the ATTACH block from M2)

```go
parentCols := map[string]string{
	"projects": "name, description, status, created_at, updated_at, uuid, origin_host",
	"concepts": "name, description, created_at, identifier_type, identifier_value, uuid, origin_host",
	"sources":  "title, identifier_type, identifier_value, authors, published_date, publisher, rights, version, retracted, retraction_note, first_seen_at, last_checked_at, uuid, origin_host",
}
for _, table := range []string{"projects", "concepts", "sources"} {
	cols := parentCols[table]
	for _, src := range []string{"a", "b"} {
		if _, err := db.Exec(fmt.Sprintf(
			`INSERT OR IGNORE INTO %s (%s) SELECT %s FROM %s.%s`,
			table, cols, cols, src, table,
		)); err != nil {
			return nil, fmt.Errorf("knowledge: merge %s from %s: %w", table, src, err)
		}
	}
}
```

`table`/`src`/`cols` are all drawn from the two hardcoded literal slices
above — never external input — so `fmt.Sprintf` into the query text is
safe here, same reasoning as `backfillUUIDs`'s table-name concatenation.

### Acceptance criteria

- All three tests pass.
- `TestMergeKnowledgeBases_CreatesMigratedSchema` (M2) still passes.

---

## M4 — `observations`

**Goal:** Copy with `project_id` translated through the `uuid` join,
never the raw source id.

### Tests to add

- `TestMergeKnowledgeBases_ObservationsTranslateProjectID` — `a` has
  project P + one observation on P; assert the merged observation's
  `project_id` points at P's *merged* id, not P's id in `a`.
- `TestMergeKnowledgeBases_ObservationsDedupSharedUUID` — mirrors the
  M3 dedup test, for observations.

### Implementation

```go
const obsCols = "kind, body, created_at, source_doi, uuid, origin_host"
for _, src := range []string{"a", "b"} {
	if _, err := db.Exec(fmt.Sprintf(`
		INSERT OR IGNORE INTO observations (project_id, %s)
		SELECT mp.id, o.kind, o.body, o.created_at, o.source_doi, o.uuid, o.origin_host
		FROM %s.observations o
		JOIN %s.projects sp ON sp.id = o.project_id
		JOIN projects mp ON mp.uuid = sp.uuid`,
		obsCols, src, src,
	)); err != nil {
		return nil, fmt.Errorf("knowledge: merge observations from %s: %w", src, err)
	}
}
```

### Acceptance criteria

- Both tests pass; M3's tests unaffected.

---

## M5 — Join tables

**Goal:** `observation_concepts`, `project_concepts`,
`observation_sources`, each joined through `uuid` on **both** sides.

### Tests to add

- `TestMergeKnowledgeBases_ObservationConceptsSurvive` — link an
  observation to a concept in `a`; assert the merged link exists,
  pointing at the merged-local ids.
- `TestMergeKnowledgeBases_ProjectConceptsSurvive` — same shape for
  `project_concepts`.
- `TestMergeKnowledgeBases_ObservationSourcesSurvive` — same shape for
  `observation_sources`, including a non-default `relationship` value.

### Implementation

```go
for _, src := range []string{"a", "b"} {
	if _, err := db.Exec(fmt.Sprintf(`
		INSERT OR IGNORE INTO observation_concepts (observation_id, concept_id)
		SELECT mo.id, mc.id
		FROM %s.observation_concepts j
		JOIN %s.observations so ON so.id = j.observation_id
		JOIN %s.concepts     sc ON sc.id = j.concept_id
		JOIN observations mo ON mo.uuid = so.uuid
		JOIN concepts     mc ON mc.uuid = sc.uuid`,
		src, src, src,
	)); err != nil {
		return nil, fmt.Errorf("knowledge: merge observation_concepts from %s: %w", src, err)
	}

	if _, err := db.Exec(fmt.Sprintf(`
		INSERT OR IGNORE INTO project_concepts (project_id, concept_id)
		SELECT mp.id, mc.id
		FROM %s.project_concepts j
		JOIN %s.projects sp ON sp.id = j.project_id
		JOIN %s.concepts sc ON sc.id = j.concept_id
		JOIN projects mp ON mp.uuid = sp.uuid
		JOIN concepts mc ON mc.uuid = sc.uuid`,
		src, src, src,
	)); err != nil {
		return nil, fmt.Errorf("knowledge: merge project_concepts from %s: %w", src, err)
	}

	if _, err := db.Exec(fmt.Sprintf(`
		INSERT OR IGNORE INTO observation_sources (observation_id, source_id, relationship)
		SELECT mo.id, ms.id, j.relationship
		FROM %s.observation_sources j
		JOIN %s.observations so ON so.id = j.observation_id
		JOIN %s.sources      ss ON ss.id = j.source_id
		JOIN observations mo ON mo.uuid = so.uuid
		JOIN sources      ms ON ms.uuid = ss.uuid`,
		src, src, src,
	)); err != nil {
		return nil, fmt.Errorf("knowledge: merge observation_sources from %s: %w", src, err)
	}
}
```

### Acceptance criteria

- All three tests pass; M3/M4 tests unaffected.

---

## M6 — `MergeTableSummary` + FTS rebuild

**Goal:** Wire the row-count summary and confirm the post-merge reopen
actually populates `kb_fts`.

### Tests to add

- `TestMergeKnowledgeBases_SummaryCounts` — known small `a`/`b` fixture;
  assert `[]MergeTableSummary` has one entry per table with correct
  `FromA`/`FromB`/`Merged` counts, including a table with an intentional
  dedup (`Merged < FromA+FromB`).
- `TestMergeKnowledgeBases_FTSPopulatedAfterMerge` — merge two KBs with
  observations, then `Search()` the merged KB for a term from one of
  them; assert it's found (proves `rebuildFTSIfNeeded` actually fired
  post-merge, not just that the row exists in `observations`).

### Implementation

Collect counts (per table: `SELECT COUNT(*) FROM a.<table>`, same for
`b` and the bare merged table name) right before the `DETACH` calls in
M2's skeleton, and return them as the final `[]MergeTableSummary` instead
of `nil, nil`.

### Acceptance criteria

- Both tests pass.
- Full `knowledge_merge_test.go` suite green.

---

## M7 — `cmd/kbmerge` binary

**Goal:** Thin orchestration wrapper — not unit tested (touches real
files/processes); verified manually in M8.

### Files to add

| File | Purpose |
|---|---|
| `cmd/kbmerge/main.go` | Flags: `-a PATH -b PATH -out PATH [-force]`. Runs `PRAGMA wal_checkpoint(FULL)` + file copy (with `-wal`/`-shm` sidecars) into a scratch dir, `CollisionReport` (without `-force`: abort and print the report; with `-force`: `ReconcileCollisions` against the scratch copy of `b`, per M1.5, then proceed), `MergeKnowledgeBases`, prints the summary table. Does **not** auto-replace either machine's live `knowledge.db` — prints the merged path and lets the user copy it into place themselves, matching this workspace's convention of not automating hard-to-reverse file replacement. |

### Acceptance criteria

- `go build -o bin/kbmerge cmd/kbmerge/*.go` succeeds.
- `bin/kbmerge -a testdata/a.db -b testdata/b.db -out /tmp/merged.db`
  (manual run) prints a summary table and exits 0.
- Manual smoke test: two synthetic databases each with an
  independently-created same-name project (each with one observation);
  `-force` run reconciles the collision and the output contains **both**
  observations, not just one (confirms M1.5's fix actually took effect
  end-to-end through the binary, not just in the unit tests).

---

## M8 — Verify green

```bash
go test ./...
go test -race   # expected blocked on this machine by the same pre-existing
                 # ThreadSanitizer/kernel VMA-width issue noted in the
                 # UUID migration's DECISIONS.md entry — not a regression
go vet ./...
go build ./...
```

Manual sanity check: copy the real, already-migrated `agents/knowledge.db`
to two scratch paths, add one distinguishing observation to each copy
via `AddObservation`, run `MergeKnowledgeBases` against them, and confirm
via `sqlite3` that the merged database contains the union — both new
observations, original row counts otherwise intact, no duplicate rows
for anything unchanged between the two copies.

### Acceptance criteria

- `go test ./...`, `go vet ./...`, `go build ./...` all clean.
- Manual two-copy merge produces the expected union with zero duplicates
  and zero data loss.

---

## Out of scope here

Actually running this tool against `wren.local`'s real database (that
machine hasn't had the UUID migration applied yet), the knowledge-base
module extraction, and JSON-L export all remain deferred per
`../knowledge_db_merge_design.md`.
