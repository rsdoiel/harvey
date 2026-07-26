# knowledge.db Cross-Machine Merge Tool — Design

**Status (2026-07-26):** Design confirmed, ready for TDD implementation.
See [merge-tool-plan.md](merge-tool-plan.md) for the phased implementation
plan. Depends on [uuid-migration-design.md](uuid-migration-design.md),
already shipped (`harvey/DECISIONS.md`, 2026-07-26 entry).

**References:**
- `../knowledge_db_merge_design.md` — umbrella design; "Merge script
  design" section is this document's source, corrected in place on
  2026-07-26 (see that document's step 4 for the correction this design
  applies).

---

## Motivation

Step 1 (UUID migration) gives every row in `projects`, `observations`,
`concepts`, `sources` a stable `uuid`. That unblocks step 2: an actual
tool to reconcile `agents/knowledge.db` after it has drifted
independently on `macmini-rd.local` and `wren.local`. Manual/on-demand
cadence — nothing here needs to be scheduled.

## Scope

In scope: a new `knowledge_merge.go` in this package, plus a thin
`cmd/kbmerge` binary, following the same relationship `cmd/assay` has to
the root `harvey` package (`../HARVEY.md`/`CLAUDE.md`'s existing
two-executable convention) — core merge logic is a tested, importable
function; the binary is orchestration only (checkpoint, file copy,
backup, printing the summary).

Out of scope: the knowledge-base module extraction and JSON-L
export/import, both still deferred per `../knowledge_db_merge_design.md`.

## Design

### Why SQL/`ATTACH DATABASE`, not JSON-L

Already decided in the umbrella document: once every table carries a
`uuid`, join-table id translation is a plain SQL join, so a
generalized dump/load format buys nothing here.

### Library function surface (`knowledge_merge.go`, package `harvey`)

```go
// NameCollision is a projects.name or concepts.name value that exists
// independently in both source databases under two different uuids —
// almost certainly the same real-world entity, created before the UUID
// migration, now indistinguishable by name alone.
type NameCollision struct {
    Table string // "projects" or "concepts"
    Name  string
    UUIDA string
    UUIDB string
}

// CollisionReport opens aPath and bPath read-only and reports every
// projects/concepts name that exists in both under different uuids.
// Callers should review (and resolve, out of band) any collisions
// before calling MergeKnowledgeBases — a collision is silently resolved
// "first insert wins" by MergeKnowledgeBases's INSERT OR IGNORE, which
// may not be the row the caller wants to keep.
func CollisionReport(aPath, bPath string) ([]NameCollision, error)

// MergeTableSummary reports, per table, how many rows came from each
// source and how many rows the merged table ended up with (less than
// FromA+FromB when uuid or name collisions caused an intentional drop).
type MergeTableSummary struct {
    Table  string
    FromA  int
    FromB  int
    Merged int
}

// MergeKnowledgeBases creates a fresh knowledge base at mergedPath (must
// not already exist) containing the set union of aPath and bPath, deduped
// by uuid (and, for projects/concepts, by the pre-existing name UNIQUE
// constraint). aPath and bPath are opened read-only via ATTACH; neither
// is modified.
func MergeKnowledgeBases(aPath, bPath, mergedPath string) ([]MergeTableSummary, error)

// ReconcileCollisions rewrites, in bPath, the uuid of every row named in
// collisions to match its counterpart's uuid ("a" wins) — see "Collision
// handling" below for why this must run before MergeKnowledgeBases, not
// be skipped in favor of just letting INSERT OR IGNORE resolve it.
func ReconcileCollisions(bPath string, collisions []NameCollision) error
```

### Collision handling (corrected 2026-07-26, caught during manual smoke-testing)

The original design assumed an unresolved collision was low-stakes: `INSERT
OR IGNORE` would keep whichever row it saw first and drop the duplicate,
same trade-off already accepted for conflicting `concepts.description`
values elsewhere. **That was wrong.** A dropped `projects`/`concepts` row
isn't just a dropped metadata duplicate — every child row (observations
via `project_id`, links via the join tables) that references it through a
`uuid` join has no merged parent to attach to once the losing row is
gone, and is *itself* silently dropped by the same `INSERT OR IGNORE`
mechanism (no matching `JOIN` row = nothing to insert). Verified with a
real smoke test through `cmd/kbmerge`: two databases each independently
had a `"smoketest"` project with one observation; a same-name collision
resolved via "first wins" produced a merge with only 1 of the 2
observations — the second machine's real content silently gone, not
just its duplicate project row.

**Fix:** `ReconcileCollisions` runs (when `-force` is passed to
`cmd/kbmerge`) *before* `MergeKnowledgeBases`, rewriting `b`'s row to
share `a`'s `uuid`. Both sides' observations/links then correctly
translate through the same `uuid` in `MergeKnowledgeBases`'s existing
join-based copy logic — no special-casing needed there at all. Without
`-force`, `cmd/kbmerge` still aborts on any collision and prints the
report, same as before.

### `MergeKnowledgeBases` steps

1. Reject if `mergedPath` already exists — merging into a live or
   previously-populated database is out of scope; the caller is
   responsible for choosing a fresh scratch path (the `cmd/kbmerge`
   binary handles checkpoint/copy/backup around this call — see below).
2. `OpenKnowledgeBase(ws, mergedPath)` then immediately `Close()` — reuses
   the real `schema`/`sourcesSchema`/`kbAlterStmts`/`kbSourcesAlterStmts`
   constants instead of duplicating `CREATE TABLE` statements, and leaves
   `mergedPath` as a fully-migrated, empty knowledge base (`kb_fts` empty
   too, which matters for step 6).
3. Open a second, raw `*sql.DB` against `mergedPath`. `ATTACH DATABASE ?
   AS a` / `... AS b` with `aPath`/`bPath` bound as parameters — avoids
   shell-style quoting fragility for paths containing spaces.
4. Parent tables (`projects`, `concepts`, `sources`, order doesn't
   matter): explicit column list **excluding `id`** (see the correction
   in `../knowledge_db_merge_design.md` step 4) —
   ```sql
   INSERT OR IGNORE INTO projects (name, description, status, created_at, updated_at, uuid, origin_host)
   SELECT name, description, status, created_at, updated_at, uuid, origin_host FROM a.projects;
   -- repeat against b.projects, then the analogous pair for concepts and sources
   ```
   Dedup is automatic via the `uuid` UNIQUE index (added in the UUID
   migration) and, for `projects`/`concepts`, the pre-existing `name`
   UNIQUE — which is exactly how the known pre-migration name-collision
   edge case gets caught (silently drops the second row; `CollisionReport`
   is the caller's chance to know about it beforehand).
5. `observations`, translating `project_id` through a `uuid` join instead
   of copying the raw id (already correct in the umbrella doc — no
   correction needed here):
   ```sql
   INSERT OR IGNORE INTO observations (project_id, kind, body, created_at, source_doi, uuid, origin_host)
   SELECT mp.id, ao.kind, ao.body, ao.created_at, ao.source_doi, ao.uuid, ao.origin_host
   FROM a.observations ao
   JOIN a.projects ap ON ap.id = ao.project_id
   JOIN projects mp ON mp.uuid = ap.uuid;
   -- mirror for b
   ```
6. Join tables (`observation_concepts`, `project_concepts`,
   `observation_sources`), joining through `uuid` on **both** sides in one
   `INSERT ... SELECT`, mirrored for `a` and `b` — six statements total,
   full SQL in `merge-tool-plan.md`.
7. Collect `MergeTableSummary` per table via `SELECT COUNT(*)` against
   `a.<table>`, `b.<table>`, and the merged table, while still attached.
8. `DETACH DATABASE a; DETACH DATABASE b`, close the raw connection.
9. `OpenKnowledgeBase(ws, mergedPath)` **again** — `rebuildFTSIfNeeded`
   (`knowledge.go:811`) only rebuilds when `kb_fts` is empty, which it
   still is (the raw-`*sql.DB` merge inserts never touch `kb_fts` — only
   the `Add*` methods populate it, and none were called), so this reopen
   is what actually builds the merged FTS index. Also a free no-op
   re-run of the backfill loop, confirming every merged row already has
   a `uuid`. Close.

### `cmd/kbmerge` binary — orchestration only

Not covered by unit tests (touches real files); covered by the manual
sanity-check step in `merge-tool-plan.md` instead.

1. `PRAGMA wal_checkpoint(FULL)` against each source `knowledge.db` (or
   confirm Harvey isn't running against it), copy the `.db` file plus any
   `-wal`/`-shm` sidecars to a scratch directory.
2. Run `CollisionReport`; print any hits. Without `-force`, abort. With
   `-force`, call `ReconcileCollisions` against the scratch copy of `b`
   first (see "Collision handling" above), then proceed.
3. Run `MergeKnowledgeBases` against a scratch `merged.db` path; print
   the `[]MergeTableSummary` table.
4. Back up each machine's live `knowledge.db` (timestamped copy), then
   replace it with `merged.db`. This step is the only genuinely
   destructive one — flagged for explicit user confirmation before
   running against real data, same as any other risky/hard-to-reverse
   action.

## Testing

TDD-first, per this workspace's convention. `knowledge_merge_test.go`
builds each source knowledge base via two real `KnowledgeBase` handles
(`openTestKB`-style, at distinct temp paths) so every row already has a
real `uuid`/`origin_host` from the `Add*` insert paths (W5 of the UUID
migration) — no need to hand-seed pre-migration rows the way
`knowledge_test.go`'s backfill tests do. See `merge-tool-plan.md` for the
specific test list per work item.

## Known edge case (carried over, now concretely testable)

The `harvey` project row exists independently on both machines today,
predating the UUID migration, so it already has two different `uuid`
values for what's logically one project. `CollisionReport` is exactly
the mechanism designed to catch this before a real merge runs — a
dedicated test (`TestCollisionReport_DetectsNameUUIDMismatch`) proves it
fires, using two synthetic databases with a same-name/different-uuid
project row.
