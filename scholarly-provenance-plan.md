# Harvey Scholarly Provenance — Implementation Plan

See [scholarly-provenance-design.md](scholarly-provenance-design.md)
for the full design rationale, gap analysis, and alternatives
considered.

Target version: **v0.0.15**

Work items are labeled S1 → S4. S1 (RAG schema) and S2 (knowledge.db
source registry) are independent and can proceed in parallel. S3
(Fountain notes) depends on S1. S4 (observation attribution) depends on
both S2 and S3.

---

## S1 — RAG Chunk Provenance Schema

**Goal:** Add minimum provenance fields to the `chunks` table in every
RAG store, and surface metadata flags on `/rag ingest` so users can
annotate scholarly content at ingest time.

### Schema migration

Harvey opens each RAG store with `RagStore.Open`. Add a
`migrateChunksSchema(db *sql.DB) error` helper called from `Open`
(immediately after the `CREATE TABLE IF NOT EXISTS chunks ...`
statement). The helper applies each `ALTER TABLE` inside its own
`IF NOT EXISTS` check pattern (try/ignore SQLITE_ERROR 1):

```sql
ALTER TABLE chunks ADD COLUMN indexed_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE chunks ADD COLUMN content_hash    TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_url      TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_doi      TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_title    TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_version  TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN rights          TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN retracted       INTEGER  NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN retraction_note TEXT     NOT NULL DEFAULT '';
```

SQLite's `ALTER TABLE ADD COLUMN` is idempotent with respect to
duplicate column names — it fails with `duplicate column name` which
the migration helper catches and ignores. Existing stores are upgraded
on first open; no manual migration step is needed.

### Files to modify

| File | Change |
|------|--------|
| `rag_support.go` | Add `migrateChunksSchema(db)` call in `Open`. Add `ProvenanceMeta` struct (fields: `URL`, `DOI`, `Title`, `Version`, `Rights`) passed through `Ingest`. Compute SHA-256 `content_hash` in `Ingest` before insert. Add `indexed_at = CURRENT_TIMESTAMP` on insert. |
| `commands.go` | `/rag ingest` gains `--title`, `--doi`, `--url`, `--version`, `--rights` flags. Pass values as `ProvenanceMeta` to `Ingest`. |
| `rag_support_test.go` | Add `TestMigrateChunksSchema_Idempotent` (run migration twice; no error). Add `TestIngest_ContentHash` (ingest same content twice; verify second call is a no-op; verify hash matches). Add `TestIngest_ProvenanceMeta` (ingest with DOI; query back and verify `source_doi` set). |

### New `ProvenanceMeta` struct (in `rag_support.go`)

```go
type ProvenanceMeta struct {
    URL     string
    DOI     string
    Title   string
    Version string
    Rights  string
}
```

`Ingest` gains a variadic `meta ...ProvenanceMeta` parameter to keep
existing callers (tests, assay) unchanged. When no meta is provided, all
provenance fields default to empty.

### Content hash logic

```go
h := sha256.Sum256([]byte(chunk.Content))
hash := hex.EncodeToString(h[:])
```

On insert, check for an existing row with matching `source` and
`content_hash`. If found, skip the insert (chunk unchanged). If found
with a different hash, delete the old row and insert fresh (source
content changed; `indexed_at` resets to now).

### Acceptance criteria

- `go test ./...` passes.
- Opening an existing `harvey.db` or `sparqlset.db` adds the new
  columns without error or data loss.
- `/rag ingest README.md --doi 10.1234/x --title "Harvey README"` sets
  `source_doi` and `source_title` on all ingested chunks.
- Re-ingesting an unchanged file skips the insert (no duplicate chunks).
- Re-ingesting a changed file replaces old chunks.

---

## S2 — Source Registry in `knowledge.db`

**Goal:** Add a `sources` authority table and `observation_sources` join
table to `knowledge.db`. Provide `/kb source` commands for managing the
registry. Migrate existing `source_doi` values.

### Schema migration

Add to `KnowledgeDB.Open` (after existing `CREATE TABLE IF NOT EXISTS`
statements):

```sql
CREATE TABLE IF NOT EXISTS sources (
    id               INTEGER  PRIMARY KEY AUTOINCREMENT,
    title            TEXT     NOT NULL,
    identifier_type  TEXT     NOT NULL DEFAULT '',
    identifier_value TEXT     NOT NULL DEFAULT '',
    authors          TEXT     NOT NULL DEFAULT '',
    published_date   TEXT     NOT NULL DEFAULT '',
    publisher        TEXT     NOT NULL DEFAULT '',
    rights           TEXT     NOT NULL DEFAULT '',
    version          TEXT     NOT NULL DEFAULT '',
    retracted        INTEGER  NOT NULL DEFAULT 0,
    retraction_note  TEXT     NOT NULL DEFAULT '',
    first_seen_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_checked_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_identifier
    ON sources(identifier_type, identifier_value)
    WHERE identifier_type != '' AND identifier_value != '';

CREATE TABLE IF NOT EXISTS observation_sources (
    observation_id INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    source_id      INTEGER NOT NULL REFERENCES sources(id)      ON DELETE RESTRICT,
    relationship   TEXT    NOT NULL DEFAULT 'cited',
    PRIMARY KEY (observation_id, source_id)
);
```

`CREATE TABLE IF NOT EXISTS` is already idempotent. No `ALTER TABLE`
needed; these are new tables.

### Data migration

After the schema migration, run a one-time data migration:

```sql
INSERT OR IGNORE INTO sources (title, identifier_type, identifier_value)
SELECT 'Source (DOI: ' || source_doi || ')', 'doi', source_doi
FROM observations
WHERE source_doi != ''
ON CONFLICT DO NOTHING;

INSERT OR IGNORE INTO observation_sources (observation_id, source_id, relationship)
SELECT o.id, s.id, 'cited'
FROM observations o
JOIN sources s ON s.identifier_value = o.source_doi
WHERE o.source_doi != '';
```

The `source_doi` column on `observations` is kept read-only after
migration. New code reads from `observation_sources`; old code (or
direct SQL queries by the user) continues to see `source_doi`.

### Files to modify

| File | Change |
|------|--------|
| `knowledge.go` | Add `sources` and `observation_sources` DDL to `Open`. Add data migration helper. Add `AddSource`, `LinkObservationSource`, `ListSources`, `ShowSource`, `RemoveSource`, `RetractSource` functions. |
| `commands.go` | Add `/kb source` command family dispatcher. Wire `cmdKBSource{Add,List,Show,Remove,Retract}` handlers. |
| `helptext.go` | Add `KBSourceHelpText`. |
| `knowledge_test.go` | Add tests: `TestAddSource`, `TestLinkObservationSource`, `TestSourceMigration`, `TestRetractSource`. |

### `/kb source` command detail

`/kb source add` with no flags opens an interactive prompts sequence
(using the existing `SelectFrom`/`promptLine` pattern):

```
Title: _
Identifier type (doi/url/isbn/issn/arxiv/urn/none): _
Identifier value: _
Authors (optional): _
Published date (YYYY-MM-DD, optional): _
Publisher (optional): _
Rights (optional): _
Version (optional): _
```

With flags, skips prompts for supplied values and defaults the rest.

`/kb source list` output (tabular):

```
ID  Title                            Identifier          Retracted
1   SPARQL 1.1 Query Language        doi:10.1234/sparql  no
2   Harvey README                    —                   no
```

`/kb retract ID --note NOTE` sets `retracted = 1` and stores the note.
A subsequent `/kb source list` marks retracted rows with `[RETRACTED]`.
The observation_sources rows are NOT deleted — the historical attribution
is preserved; only the source's current validity status changes.

### Acceptance criteria

- `go test ./...` passes.
- Opening `knowledge.db` with existing `source_doi` observations
  migrates them into `sources` and `observation_sources`.
- `/kb source add --doi 10.1234/sparql --title "SPARQL 1.1"` inserts
  a row and prints the assigned ID.
- `/kb source list` shows the registered source.
- `/kb retract 1 --note "Retracted 2026-07-01"` sets the flag.
- A subsequent `/kb source list` shows `[RETRACTED]` next to id=1.
- `/kb source remove 1` fails (linked observations) with a clear error.

---

## S3 — Source-Level Fountain Notes

**Goal:** Extend `RAGAugmentInfo` with per-chunk source references and
emit `[[rag-source: ...]]` notes in session files after the existing
`[[rag: ...]]` aggregate note.

This work item depends on S1 (provenance columns must exist to populate
`RAGChunkRef`).

### Files to modify

| File | Change |
|------|--------|
| `recorder.go` | Add `RAGChunkRef struct { Source, DOI, Title, Lines string }`. Add `Sources []RAGChunkRef` field to `RAGAugmentInfo`. In `RecordTurnWithStats`, after writing `[[rag: ...]]`, iterate `ragInfo.Sources` and emit one `[[rag-source: ...]]` note per entry. |
| `rag_support.go` | Extend `Query` to return `[]EnrichedChunk` with provenance fields populated from the new columns. Populate `RAGChunkRef` per returned chunk in `ragAugment`. Deduplicate by `Source` value before populating `RAGAugmentInfo.Sources`. |
| `terminal.go` | `ragAugment` already returns `*RAGAugmentInfo`; add source-ref population logic. |
| `recorder_test.go` | Add `TestRecordTurnWithStats_RAGSourceNotes` — non-nil Sources slice; verify one `[[rag-source: ...]]` line per entry, in order, after `[[rag: ...]]`. |

### `[[rag-source: ...]]` format function

```go
func formatRAGSourceNote(r RAGChunkRef) string {
    loc := r.Source
    if r.Lines != "" {
        loc += ":" + r.Lines
    }
    if r.Title == "" && r.DOI == "" {
        return "rag-source: " + loc
    }
    meta := r.Title
    if r.DOI != "" {
        if meta != "" {
            meta += ", doi:" + r.DOI
        } else {
            meta = "doi:" + r.DOI
        }
    }
    return "rag-source: " + loc + " (" + meta + ")"
}
```

### Deduplication logic (in `ragAugment`)

After collecting all returned chunks, build a `seen map[string]bool`
keyed on `chunk.Source`. Only append the first `RAGChunkRef` for each
distinct source. Sort by descending similarity score before
deduplication so the highest-scoring source for each document appears.

### Expected Fountain output

```
[[rag: 3 chunks from sparqlset.db, top score 0.87]]
[[rag-source: sparql-spec.md:45–72 (SPARQL 1.1 Query Language, doi:10.1234/sparql)]]
[[rag-source: example-queries.go:12–30]]
[[rag-source: README.md]]
```

Existing stores with no provenance metadata (all empty strings) produce:

```
[[rag: 3 chunks from sparqlset.db, top score 0.87]]
[[rag-source: sparql-spec.md:45–72]]
[[rag-source: example-queries.go:12–30]]
[[rag-source: README.md]]
```

### Acceptance criteria

- `go test ./...` passes.
- After S1 migration, ingesting a file with `--doi` and re-running a
  RAG-augmented turn produces `[[rag-source: ...]]` notes with DOI.
- Existing stores (no provenance metadata) produce source notes with
  only file paths — no empty parenthetical.
- Duplicate chunks from the same source file produce a single
  `[[rag-source:]]` note, not multiple.

---

## S4 — Observation Attribution

**Goal:** Connect `/kb observe` to the RAG sources retrieved in the
preceding turn, and add `/kb cite` for manual source linking. Display
sources in `/kb show`.

This work item depends on both S2 (source registry) and S3 (per-turn
source refs stored in `RAGAugmentInfo`).

### State threading

The agent needs to carry the most recent `RAGAugmentInfo` across from a
chat turn into the subsequent `/kb observe` command. Add a
`lastRAGInfo *RAGAugmentInfo` field to the `Agent` struct. Set it in
`runChatTurn` after `ragAugment` returns. Reset to `nil` on clear
(`/clear`).

### Files to modify

| File | Change |
|------|--------|
| `harvey.go` | Add `lastRAGInfo *RAGAugmentInfo` to `Agent`. |
| `terminal.go` | Set `a.lastRAGInfo = ragInfo` after `ragAugment` call in `runChatTurn`. Clear on `/clear`. |
| `commands.go` | In `cmdKBObserve`: after recording observation, check `a.lastRAGInfo`. If non-nil and non-empty sources, print source list and prompt `Link? [Y/n]`. On yes, call `a.KB.FindOrCreateSource` per RAGChunkRef and `a.KB.LinkObservationSource`. Add `cmdKBCite` for `/kb cite SOURCE_ID [--obs N]`. Extend `cmdKBShow` to query `observation_sources JOIN sources`. |
| `knowledge.go` | Add `FindOrCreateSource(ref RAGChunkRef) (int64, error)` — upsert by (identifier_type, identifier_value) when DOI present; otherwise insert new source with `title = source` path. |

### `/kb cite` usage

```
/kb cite 1           # link source id=1 to the most recent observation, relationship=cited
/kb cite 1 --obs 42  # link source id=1 to observation id=42
```

### `/kb show` output with sources

```
Observation #42 [finding] (2026-06-25 14:23:11)
  SPARQL federation queries require SERVICE blocks to reference
  remote endpoints…

Sources:
  [1] SPARQL 1.1 Query Language (doi:10.1234/sparql) — retrieved
  [2] example-queries.go — retrieved
```

When a source is marked retracted, its line gets a `[RETRACTED]` suffix
and a one-line warning is printed:

```
  [1] SPARQL 1.1 Query Language (doi:10.1234/sparql) — retrieved [RETRACTED]
⚠ Source 1 is marked retracted: "Retracted 2026-07-01"
```

### Acceptance criteria

- `go test ./...` passes.
- After a RAG-augmented turn, `/kb observe "my note"` prompts with the
  retrieved source list and links on confirmation.
- `/kb show 42` lists linked sources with relationship labels.
- `/kb cite 1` links source 1 to the last observation.
- A retracted source displays a warning in `/kb show`.
- `a.lastRAGInfo` is `nil` (no prompt) when RAG did not fire.

---

## Full test run (after all four work items)

```bash
cd harvey
go test ./...
go test -race
go build -o bin/harvey cmd/harvey/*.go
```

Manual smoke test:

1. Ingest a file with provenance: `/rag ingest docs/paper.md --doi 10.5555/test --title "Test Paper"`
2. Ask a question that retrieves chunks from that file.
3. Inspect the session `.spmd` file:
   - `[[rag: N chunks ...]]` aggregate note present
   - `[[rag-source: docs/paper.md (Test Paper, doi:10.5555/test)]]` note present
4. Run `/kb observe "key finding"` — see source prompt, confirm.
5. Run `/kb show <id>` — see source listed as `retrieved`.
6. Run `/kb source list` — see the source registered.
7. Run `/kb retract <id> --note "test retraction"`.
8. Run `/kb show <obs-id>` — see `[RETRACTED]` warning.
