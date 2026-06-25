# Harvey Scholarly Provenance — Design

**Status (2026-06-25):** Design draft for v0.0.15. See
[scholarly-provenance-plan.md](scholarly-provenance-plan.md) for the
phased implementation plan.

**References:**

- "Attribution, Provenance, Reference, Citation, and AI for Research
  Applications: Understanding the Differences" — *The Scholarly Kitchen*,
  2026-06-17. Establishes the conceptual distinctions between attribution,
  provenance, reference, and citation in AI research contexts. Identifies
  citation-after-generation as a failure of scholarly integrity.

- "Making AI Use of Scholarly Content Traceable, Measurable, and
  Trustworthy" — Meeting report from the Cambridge Scholarly AI Workshop
  (Cambridge University Press, COUNTER, NISO), *The Scholarly Kitchen*,
  2026-06-25. Identifies practical interventions at content retrieval
  points and defines a minimum provenance payload for AI-retrieved
  scholarly content.

---

## Motivation

The Cambridge workshop identified that AI tools interact with scholarly
content at two distinct points: **training time** (content absorbed into
model weights) and **inference time** (content retrieved and injected at
query time). The workshop explicitly classified training-time attribution
as technically intractable and concentrated its recommendations on
inference-time retrieval — specifically, the RAG pattern that Harvey
already uses.

The workshop's minimum provenance payload for retrieved content includes:

- Persistent identifiers and source locations
- Timestamps and content hashes
- Version information
- Copyright and rights information
- Retraction flags and expressions of concern

Harvey's RAG system (`rag_support.go`, `agents/rag/*.db`) sits precisely
at this intervention point. Harvey also accumulates scholarly observations
in `agents/knowledge.db`. Both systems have provenance gaps that prevent
session files and knowledge-base entries from serving as traceable,
verifiable scholarly records.

The Scholarly Kitchen article adds a second concern: AI systems often
**generate first, then layer citations on afterward** — inverting the
scholarly process. The evidential chain is broken when citations appear
post-hoc rather than as the basis for a claim. Harvey's RAG pattern
retrieve-then-generate should be codified and visible in session records.

---

## Current state gap analysis

### RAG chunk store (`chunks` table in `harvey.db`, `sparqlset.db`)

| Column | Present | Gap |
|--------|---------|-----|
| `source` | ✓ (file path) | No URL, DOI, or external identifier |
| `start_line` … `end_col` | ✓ (code location) | No `indexed_at` timestamp |
| `content` | ✓ | No `content_hash` for change detection |
| `chunk_type`, `symbols`, `docs` | ✓ | No version, rights, or retraction flag |

All provenance fields beyond the local file path are absent. A chunk
from a retracted paper, an outdated preprint, or a proprietary document
is indistinguishable from any other chunk.

### Knowledge base (`agents/knowledge.db`)

| Feature | Present | Gap |
|---------|---------|-----|
| `observations.source_doi` | ✓ (single DOI text column) | One source per observation; DOI-only; no title, authors, rights, retraction |
| `concepts.identifier_type/value` | ✓ (proto-PID pattern) | Per-concept, not per-source; no observation→source join |
| `kb_fts.source_type/source_id` | ✓ (FTS index) | Search index only; not an authority source registry |

There is no source registry. Multiple sources per observation cannot be
expressed. The `source_doi` field is frequently empty because the
ingestion workflow provides no friction-free path to record it.

### Fountain session files

The v1.2 audit trail (introduced in v0.0.15) records:

```
[[rag: 3 chunks from sparqlset.db, top score 0.87]]
```

This note captures aggregate retrieval stats but not which documents the
chunks came from. A session auditor cannot determine whether the model's
answer was informed by a primary source, a Wikipedia mirror, or a
retracted preprint.

---

## Design goals

1. **Minimum provenance payload in RAG chunks.** Add metadata fields to
   the `chunks` table aligned with the Cambridge workshop recommendations.
   All fields are optional with empty/zero defaults so existing stores
   upgrade without migration data loss.

2. **Authority source registry.** Add a `sources` table to `knowledge.db`
   as the single authority for source metadata. Sources are registered
   once and referenced by observations and RAG chunks via foreign keys.

3. **Observation-to-source attribution chain.** Replace the single
   `observations.source_doi` column with a proper `observation_sources`
   join table supporting multiple sources per observation, multiple
   identifier types, and relationship classification.

4. **Source-level Fountain notes.** Extend the `[[rag: ...]]` note to
   enumerate distinct source documents retrieved in each turn, including
   any DOI or title metadata available, so session files serve as
   citable records of what evidence informed each response.

5. **Retrieval discipline in system context.** Document in `HARVEY.md`
   that retrieved content must be attributed at the point of use, not
   post-hoc, so the model internalises this as a behavioral principle.

---

## Architecture

### S1 — RAG Chunk Provenance Schema

**Current state.** The `chunks` table (shared schema in both
`harvey.db` and `sparqlset.db`) has no timestamp, no content hash,
and no external source identifier. `source` stores only a local file
path or URL string without structure.

**Change.** Add provenance columns via `ALTER TABLE`. All new columns
have safe defaults so existing stores are immediately queryable after
the schema migration without re-ingesting content.

```sql
ALTER TABLE chunks ADD COLUMN indexed_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE chunks ADD COLUMN content_hash   TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_url     TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_doi     TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_title   TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN source_version TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN rights         TEXT     NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN retracted      INTEGER  NOT NULL DEFAULT 0;
ALTER TABLE chunks ADD COLUMN retraction_note TEXT    NOT NULL DEFAULT '';
```

**`content_hash` computation.** SHA-256 of the raw `content` string,
encoded as lowercase hex. Computed by `RagStore.Ingest` before writing.
When a subsequent ingest of the same `source` finds a matching
`content_hash`, the chunk is skipped as unchanged. When the hash
differs, the old chunk is replaced and `indexed_at` is updated.

**`/rag ingest` flags (new).** Source metadata cannot be inferred from
file content alone for scholarly documents. New optional flags allow the
user to annotate at ingest time:

```
/rag ingest FILE [--title TITLE] [--doi DOI] [--url URL]
                 [--version VERSION] [--rights RIGHTS]
```

When `--doi` is given, `source_doi` is set on all chunks from that
ingest run. When `--url` is given and `source` is already a URL
(remote ingest), `source_url` copies it; for local files `source_url`
is the provided flag value. `source_title` defaults to the filename
stem when not specified and no `--title` flag is given.

### S2 — Source Registry in `knowledge.db`

**Current state.** `observations.source_doi` is a single TEXT column
with no joins, no title or author metadata, and no retraction tracking.
`concepts.identifier_type/value` supports PIDs per concept but not per
source document.

**Change.** Add two new tables to `knowledge.db`.

```sql
CREATE TABLE sources (
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

CREATE TABLE observation_sources (
    observation_id INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    source_id      INTEGER NOT NULL REFERENCES sources(id)      ON DELETE RESTRICT,
    relationship   TEXT    NOT NULL DEFAULT 'cited',
    PRIMARY KEY (observation_id, source_id)
);
```

`identifier_type` values follow the existing `concepts` convention:
`doi`, `url`, `isbn`, `issn`, `arxiv`, `urn`, `handle`. The pair
(`identifier_type`, `identifier_value`) must be unique when both are
non-empty — enforced by a partial unique index.

`relationship` in `observation_sources` classifies how the source
relates to the observation:
- `cited` — user explicitly named this source when recording the observation
- `retrieved` — Harvey's RAG system returned chunks from this source during the turn when the observation was recorded
- `inferred` — the model drew on this source without explicit retrieval (rare; for manual annotation only)

**Migration.** Existing `observations` rows where `source_doi != ''`
are migrated: a row is inserted into `sources` (with `identifier_type =
'doi'`, `identifier_value = source_doi`) and linked via
`observation_sources` with `relationship = 'cited'`. The `source_doi`
column is retained for backward compatibility but treated as read-only
after migration.

**New `/kb source` command family.**

| Command | Action |
|---------|--------|
| `/kb source add` | Interactive prompts for title, identifier, authors, date, rights |
| `/kb source add --doi DOI --title TITLE [--authors A] [--published DATE] [--rights R]` | Non-interactive add |
| `/kb source list` | Tabular list: id, title, identifier, retracted flag |
| `/kb source show ID` | Full metadata for one source |
| `/kb source remove ID` | Remove source if no linked observations |
| `/kb retract ID --note NOTE` | Set `retracted = 1` and `retraction_note` |

### S3 — Source-Level Fountain Notes

**Current state.** `[[rag: 3 chunks from sparqlset.db, top score
0.87]]` captures aggregate retrieval stats but not per-document source
attribution.

**Change.** Extend `RAGAugmentInfo` to carry per-chunk source
references. Add a second class of note, `[[rag-source: ...]]`, emitted
once per distinct source document referenced in the retrieved chunks.

```go
type RAGChunkRef struct {
    Source  string  // file path or URL (the existing chunks.source value)
    DOI     string  // chunks.source_doi if set
    Title   string  // chunks.source_title if set
    Lines   string  // "12–45" for code chunks; empty for prose
}

// RAGAugmentInfo gains a Sources field (existing fields unchanged):
type RAGAugmentInfo struct {
    StoreName string
    Chunks    int
    TopScore  float64
    Sources   []RAGChunkRef  // deduplicated by Source value
}
```

The `[[rag: ...]]` summary note is unchanged. When `Sources` is
non-empty, one `[[rag-source: ...]]` note follows per distinct source:

```
[[rag: 3 chunks from sparqlset.db, top score 0.87]]
[[rag-source: sparql-spec.md:45–72 (SPARQL 1.1 Query Language, doi:10.1234/sparql)]]
[[rag-source: example-queries.go:12–30]]
[[rag-source: README.md]]
```

Format rule: `SOURCE[:LINES] [(TITLE[, doi:DOI])]`. The title and DOI
parenthetical is omitted when both are empty (existing ingests without
metadata). Lines are omitted for prose chunks. Sources are deduplicated
and sorted by descending similarity score — the most relevant source
first.

**Where notes appear.** The `[[rag-source:]]` notes follow immediately
after the existing `[[rag: ...]]` aggregate note, inside the
`INT. HARVEY AND … TALKING` scene for the turn where RAG fired, before
the user's dialogue line. No change to placement logic in
`RecordTurnWithStats`.

### S4 — Observation Attribution in `/kb observe`

**Current state.** `/kb observe` records a free-text observation and
asks for a `source_doi`. No connection exists between the observation's
source and the RAG chunks retrieved in the preceding turn.

**Change.** When `/kb observe` runs and the preceding turn had a
non-nil `RAGAugmentInfo.Sources`, Harvey offers to auto-link the
observation to the RAG-retrieved sources:

```
Observation recorded (id=42).
RAG retrieved 3 chunks from sparqlset.db this turn.
  Source: sparql-spec.md (doi:10.1234/sparql)
  Source: example-queries.go
Link this observation to these sources? [Y/n]
```

If yes, Harvey looks up or creates `sources` entries for each
`RAGChunkRef` and inserts `observation_sources` rows with
`relationship = 'retrieved'`. The user can also add explicit citations
with `/kb cite SOURCE_ID [--obs OBSERVATION_ID]` (defaults to the most
recent observation).

**`/kb show` output.** When sources are linked, `/kb show` appends a
Sources section:

```
Observation #42 [finding] (2026-06-25 14:23:11)
  SPARQL federation queries require SERVICE blocks…

Sources:
  [1] SPARQL 1.1 Query Language (doi:10.1234/sparql) — retrieved
  [2] example-queries.go — retrieved
```

---

## Retrieval discipline in `HARVEY.md`

Add a short **Provenance** section to `HARVEY.md` (the system prompt)
documenting the intended behavioral norm:

> When you answer a question using content from the RAG context block,
> attribute that content to its source at the point you use it. Do not
> generate claims and then search for citations afterward. If you cannot
> identify a source for a claim, say so explicitly rather than asserting
> it as fact.

This is a behavioral guideline enforced through the system prompt, not
a code constraint. The guideline will be more effective with smaller
models when RAG retrieval also surfaces clear source metadata (S3).

---

## Alternatives considered

**Training-time attribution.** Tracking which documents went into the
model's weights is technically intractable, as the Cambridge workshop
concluded. This design focuses entirely on inference-time provenance,
where Harvey has full observability through the RAG pipeline.

**Single `source_doi` column vs. source registry.** The existing
`observations.source_doi` is a single DOI text field. It cannot express
multiple sources per observation, non-DOI identifiers (URLs, ISBNs,
arXiv IDs), or source metadata (title, authors, rights, retraction
status). A proper `sources` authority table with `observation_sources`
join table adds one more schema layer but enables correct attribution
semantics. The single column is retained as a migration path and
backward-compat read.

**Separate provenance database.** A dedicated `provenance.db` alongside
`knowledge.db` would isolate the schema change. Rejected because `sources`
needs to join against `observations`, `concepts`, and `kb_fts`, which are
all in `knowledge.db`. Cross-database joins in SQLite require `ATTACH` and
cannot use foreign keys. A single database with multiple tables is the
correct SQLite idiom.

**Per-chunk vs. per-source Fountain notes.** Per-chunk notes would emit
one line for every retrieved chunk — up to 10 or more per turn — making
session files verbose and harder to mine. Per-source deduplication (one
note per distinct source document) captures the provenance question that
matters: "which works informed this answer?" without the noise of which
paragraph within the work was retrieved. A session with 10 chunks from
3 source documents produces 3 `[[rag-source:]]` notes, not 10.

**Content hash as cryptographic proof.** SHA-256 of the chunk content is
not a security guarantee (the file could be tampered before ingest) but
is sufficient for the primary use case: detecting whether a source has
changed since it was last ingested. When a re-ingest finds a changed
hash, Harvey can warn the user that cached chunks may be stale.

**Retrieval-first enforcement in code.** The ideal system would reject
any response that makes a citation without a matched RAG source and
inject a correction. This requires deep integration with the chat loop
and reliable citation detection, both of which are beyond the scope of
this work. The behavioral norm in `HARVEY.md` achieves partial effect
with small models and will improve as RAG source metadata becomes richer
through S1–S3.

**Retraction monitoring service.** Automatically checking registered
DOIs against retraction databases (Retraction Watch, CrossRef) on a
schedule would surface stale scholarly content proactively. Deferred:
this requires outbound network calls, a new background process, and
policy decisions about frequency and authority. The schema includes
`retracted` and `retraction_note` fields so manual retraction marking
works immediately, and the monitoring service can be added later without
schema changes.
