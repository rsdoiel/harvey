# Towards a Scholarly Memory Design for Harvey

**Status:** Draft (v3)
**Date:** 2026-06-10 (v1), revised 2026-06-11 (v3)

## Overview

Harvey is a **secure, local language model agent** that can be useful in
library, archival, and research settings through support for working directly
with digital objects (PDFs, images) and their metadata. This document proposes
making Harvey **scholar-aware** by enhancing the **ingest tools and data
structures Harvey already has**

The enhancement planned is designed at multiple layers. At the tools layers
Harvey should include metadata identifier detection when reading content whether
a plain text, Markdown or PDFs. Additionally PDFs of journal articles have a
predictable structure such as title, author info, abstract and article content.
If the PDF is implemented as PDF UA then additional metadata may be avaiable in
the PDF's properties. Source code may also embed metadata in their comments such
as authorship, copyright information and even DOI depending on the nature of
of the content. Some can be infirred when found in a software repository either
in the codemeta.json or CITATION.cff files often included in Open Source projects.

I believe this additional contextual information can be useful in forming conversational
memories as well as RAG and knowledge bases that Harvey currently supports.

### Context papers

Todd Toler's paper provides specific suggestions for enhancing RAG and by
extension knowledge based through focusing enhancing citation context of the
data.

- Toler, Todd. (2025). *Preserving Scholarly Signals in RAG Systems.* —
  **the key reference**: identifies that naive RAG chunking discards
  citations, version status (preprint vs. published), peer-review status,
  disciplinary conventions, and retraction status. This design's RAG
  changes are a direct response to that paper.

The following two papers provide additional context of the challenges faced
in language model based search as well as moving towards a more useful set of
scholarly tools enhanced by langauge model integration.

- Tay, Aaron. (2026). *What a year of testing & thinking about AI academic
  search taught me*. https://doi.org/10.59350/rcm1j-5nx94
- Tay, Aaron. (2026). *How should academic retrieval augmented generation
  (RAG) systems handle retracted works?*
  https://aarontay.substack.com/p/how-should-academic-retrieval-augmented
- PurePub.AI. (2025). *Infrastructure as the Foundation of Trustworthy AI.*

## Grounding in Harvey's current architecture

Three things already exist that this design builds on directly:

1. **The chunker/extractor registry pattern** (`language_registry.go`,
   `code_chunkers.go`, `doc_extractors.go`): each language has a `Chunker`
   that produces `EnrichedChunk`s (with `ChunkType`, `Symbols`, `Docs`,
   precise line/col ranges) and a `DocExtractor` that pulls doc-comments.
   `RagStore.IngestEnriched` stores these via lazily-migrated columns
   (`enrichedAlterStmts`). **Scholarly documents become a new entry in this
   pattern**, not a new system.

2. **PDF ingest** (`pdf_extract.go`'s `pdfExtract` + `commands.go`'s
   `ragIngestPDF`): already shells out to poppler (`pdfinfo`, `pdftotext`,
   `pdfimages`) for metadata, full text, and per-page splitting, and already
   flags diagram-only pages. This is the natural place to add identifier
   scanning and section-aware chunking.

3. **Knowledge base** (`knowledge.go`, `agents/knowledge.db`): relational
   SQLite with `projects`, `observations` (kind: note/finding/decision/
   question/hypothesis), `concepts`, and link tables
   `observation_concepts`/`project_concepts`. Identifiers extend these
   tables rather than requiring a new "sources" table.

4. **Workspace onboarding** (`memory_onboarding.go`): already auto-extracts
   a `project_fact` `MemoryDoc` on first run per workspace. This is the
   natural place to read `codemeta.json`/`CITATION.cff` for the workspace's
   own scholarly identifiers (per CLAUDE.md's existing convention that
   `codemeta.json` is the source of truth for project metadata).

## v0.0.11 vertical slice

### 1. `scholarly_identifiers.go` — full metadatatools identifier surface

Depends on `github.com/caltechlibrary/metadatatools` **v0.1.1** (tagged and
pushed to GitHub, zero external dependencies — pure stdlib):

```bash
go get github.com/caltechlibrary/metadatatools@v0.1.1
```

v0.1.1 provides `Normalize*`/`Validate*` for 14 identifier types relevant to
scholarly content:

DOI, ORCID, ROR, RAiD, ArXiv, FundRef, ISBN, ISSN, ISNI, PMID, PMCID, VIAF,
SNAC, LCNAF.

(metadatatools also has Email, Tel, UUID, EAN — general-purpose, not
scholarly identifiers, **out of scope** here.)

metadatatools does **not** provide text-scanning extraction (`Find*`) —
that's still Harvey's work:

```go
type IdentifierType string

const (
    IdentifierDOI     IdentifierType = "doi"
    IdentifierORCID   IdentifierType = "orcid"
    IdentifierROR     IdentifierType = "ror"
    IdentifierRAiD    IdentifierType = "raid"
    IdentifierArXiv   IdentifierType = "arxiv"
    IdentifierFundRef IdentifierType = "fundref"
    IdentifierISBN    IdentifierType = "isbn"
    IdentifierISSN    IdentifierType = "issn"
    IdentifierISNI    IdentifierType = "isni"
    IdentifierPMID    IdentifierType = "pmid"
    IdentifierPMCID   IdentifierType = "pmcid"
    IdentifierVIAF    IdentifierType = "viaf"
    IdentifierSNAC    IdentifierType = "snac"
    IdentifierLCNAF   IdentifierType = "lcnaf"
)

// FindIdentifiers scans text and returns every identifier found, grouped
// by type. Each match is normalized via the corresponding
// metadatatools.Normalize* function before being returned, so callers
// always see canonical forms (e.g. extended https://doi.org/... URLs).
func FindIdentifiers(text string) map[IdentifierType][]string

// Per-type helpers (FindDOIs, FindORCIDs, ...) wrap FindIdentifiers for
// unit-test fixtures and call sites that only care about one type.
func FindDOIs(text string) []string
func FindORCIDs(text string) []string
// ... etc, one per IdentifierType
```

#### Extraction principle: match the bare/local form, normalize on the way out

For every identifier type **except RAiD** (see below), the bare/local form
of the identifier — without any URL or CURIE wrapper — is the common case
in running text, and `metadatatools`'s `Normalize*` functions already accept
either a bare identifier or a URL-wrapped one as input. So `Find*` should
match the **local identifier pattern itself**, optionally preceded by a
label/prefix/URL, and let `Normalize*` produce the canonical stored form
(which varies by type — see "Per-type canonical forms" below).

**DOI** — papers overwhelmingly cite their own DOI in short form, e.g.
`DOI: 10.1234/abcd.5678`, `doi:10.1234/abcd.5678`, or a bare
`10.1234/abcd.5678` — full `https://doi.org/...` URLs are more common in
reference lists than in running text. `FindDOIs` must match all of:

- `https?://doi\.org/10\.\d{4,9}/\S+` (extended/full URL form)
- `doi:\s*10\.\d{4,9}/\S+` (CURIE form, case-insensitive `doi:` prefix)
- `(?i)DOI:?\s*10\.\d{4,9}/\S+` (label-prefixed form common in papers)
- bare `10\.\d{4,9}/\S+` (no label/prefix at all)

**ORCID** — author bylines list ORCID iDs in **bare hyphenated form**
(`0000-0003-0900-6903`), typically next to an ORCID icon, with no URL.
`FindORCIDs` must match the bare `\d{4}-\d{4}-\d{4}-\d{3}[\dX]` pattern
directly (in addition to `https://orcid.org/...` URLs and `orcid:`
CURIEs). The trailing checksum digit (ISO 7064 Mod 11-2, enforced by
`ValidateORCID`) keeps false-positive risk from incidental 16-digit groups
low — any candidate that fails the checksum is discarded.

Every match — regardless of which form it was found in — is normalized via
the type's `Normalize*` function (e.g. `metadatatools.NormalizeDOI()`,
`NormalizeORCID()`) before being stored or compared. This keeps
storage/comparison uniform while extraction stays permissive about input
shape. The same "match bare-or-wrapped, normalize on output" approach
applies to the remaining types (ArXiv, ISBN, ISSN, ISNI, PMID, PMCID,
FundRef, VIAF, SNAC, LCNAF) — each has its own bare local-identifier
pattern that is at least as common as any URL form.

#### Per-type canonical (stored) forms

`Normalize*` output shape differs by type — `Find*` returns whatever
`Normalize*` produces, unchanged:

| Type | Canonical form | Example |
|---|---|---|
| DOI | extended URL | `https://doi.org/10.5281/zenodo.1234` |
| RAiD | extended URL | `https://raid.org/10.83962/fb5be317` |
| ORCID | bare, hyphenated | `0000-0003-0900-6903` |
| ROR | extended URL | `https://ror.org/05dxps055` |
| ArXiv | CURIE | `arxiv:2412.03631` |
| ISNI | bare, space-grouped | `0000 0003 0900 6903` |
| FundRef | bare (DOI-shaped) | `10.13039/100006961` |

(Other types follow the same "ask `Normalize*`, store its output" rule —
not exhaustively listed here.)

#### DOI / RAiD disambiguation

RAiD and DOI are format-identical (`10.\d{4,9}/suffix` — a RAiD *is* a
DataCite DOI per ISO 23527). metadatatools' `ValidateRAiD`/`ValidateDOI`
both return `true` for the same bare string, so format alone can't
disambiguate.

**Convention** (matches emerging community practice — full resolver URLs
for DOI/RAiD, short form assumed DOI):

- `FindRAiDs` matches **only** `https://raid\.org/10\.\d{4,9}/\S+` — the
  full `raid.org` resolver URL is required.
- `FindDOIs` matches the full-URL, CURIE, label-prefixed, and bare forms
  above — **including** the short form, per "DOI extraction" above.
- A short-form `10.xxxx/yyyy` (with or without a `DOI:`/`doi:` label) is
  **never** classified as a RAiD candidate, even though `ValidateRAiD`
  would accept it. Only an explicit `raid.org/...` URL is.

This means RAiD extraction will be rare in practice (most papers don't cite
`raid.org` URLs directly), but it's correct and requires no live lookup to
disambiguate. RAiD matches are normalized via `metadatatools.NormalizeRAiD()`
to `https://raid.org/10.xxxx/yyyy`.

These are pure functions with no I/O — easy to unit test against the same
fixtures `metadatatools` uses for each type.

### 2. Scholarly-aware PDF ingest (`pdf_extract.go` + `ragIngestPDF`)

Extend `pdfExtract`/`ragIngestPDF` with a "paper detection" step:

- Heuristic: does the text contain section headers like "Abstract",
  "Introduction", "References"/"Bibliography", or does `FindDOIs` find a
  DOI on the first page or in `pdfinfo` metadata?
- **If paper-like:** chunk by section instead of flat per-page paragraphs.
  New `scholarlyChunk(pageTexts []string) []EnrichedChunk` assigns
  `ChunkType` = `"abstract"|"introduction"|"methods"|"results"|
  "discussion"|"conclusion"|"references"|"body"`.
- **Identifier scan** across the full extracted text + `pdfinfo` metadata,
  via `FindIdentifiers`, populating `EnrichedChunk.Identifiers` (the
  document's own DOI, author ORCIDs, affiliation RORs, funder FundRef IDs,
  etc.) and `EnrichedChunk.Citations` (identifiers — mainly DOIs/ArXiv IDs —
  found in the references section, pointing to *other* works).
- **If not paper-like:** unchanged — existing flat per-page chunking via
  `ragChunk`.

New `EnrichedChunk` fields (mirroring how `Symbols`/`Docs` were added for
code):

```go
type EnrichedChunk struct {
    // ... existing fields (StartLine, ChunkType, Symbols, Docs, ...)
    Identifiers map[string][]string // keyed by IdentifierType string; this chunk/document's own identifiers
    Citations   []string            // identifiers (any type) found in this chunk's text that point to OTHER works
}
```

There are additional structural aspects of journal articles that may be
identifiable through a heuristic approach such as acknowledgements and
funding information.

`RagStore.IngestEnriched` gets new lazily-migrated columns following the
existing `enrichedAlterStmts` pattern. Given the breadth of the identifier
set (14 types), a single JSON column is used rather than one column per
type:

```go
`ALTER TABLE chunks ADD COLUMN identifiers TEXT NOT NULL DEFAULT '{}'`, // JSON: {"doi": ["..."], "orcid": ["..."], ...}
`ALTER TABLE chunks ADD COLUMN citations   TEXT NOT NULL DEFAULT ''`,   // comma-joined identifiers (any type) found in references
```

`section_type` reuses the existing `chunk_type` column — no new column
needed there.

### 3. Knowledge base extensions (`knowledge.go`)

Extend `observations` and `concepts` in place (lazy `ALTER TABLE`, same
style as the RAG migration above):

```sql
ALTER TABLE observations ADD COLUMN source_doi TEXT NOT NULL DEFAULT '';

ALTER TABLE concepts ADD COLUMN identifier_type  TEXT NOT NULL DEFAULT ''; -- one of the 14 IdentifierType values
ALTER TABLE concepts ADD COLUMN identifier_value TEXT NOT NULL DEFAULT ''; -- normalized (extended) form
```

This lets:
- An **observation** (a finding/note) record *which paper it came from*
  via `source_doi`.
- A **concept** represent not just an abstract idea but a *scholarly
  entity* — a paper (DOI), a person (ORCID), an institution (ROR), a
  funder (FundRef), etc. — linked to observations/projects through the
  existing `observation_concepts`/`project_concepts` tables. E.g., "Aaron
  Tay (ORCID 0000-...)" becomes a concept linked to every observation drawn
  from his papers.

No new tables. The `experiment_summary` view (mentioned in CLAUDE.md) can
be extended later to roll up identifiers if useful, but that's not required
for v0.0.11.

It may be worth extending the current knowledge base structure to capture
identifier relationships to the concepts or observations, especially if
discoverd through a combination of conversation, RAG and conversational memory.

### 4. Workspace onboarding (`memory_onboarding.go`)

When generating the auto-extracted `project_fact` `MemoryDoc`, run
`FindIdentifiers` over `codemeta.json` and `CITATION.cff` (if present in
the workspace root) and add the results to `MemoryMeta.Metadata`, e.g.:

```yaml
metadata:
  identifiers:
    doi: ["https://doi.org/10.5281/zenodo.xxxxx"]
    orcid: ["0000-0003-0900-6903"]
    ror: ["https://ror.org/05dxps055"]
    fundref: []
```

(Per-type canonical forms — see the table above. Note ORCID is stored bare/
hyphenated, *not* as an `https://orcid.org/...` URL, even though the source
`codemeta.json` `@id` field is typically a full URL — `NormalizeORCID`
strips it.)

`UnifiedMemory.Recall()` already injects `project_fact` at score 1.0, so
this identity context becomes available to every session with no further
plumbing — directly realizing the "Knowledge of Trusted Sources" idea from
v1 (codemeta.json/CITATION.cff as predictable, structured context for any
Git repo Harvey works in).

## Open research item: C2PA detection (spike, not yet scoped)

Your framing: C2PA is useful **if an ingested digital object already
carries a manifest** (e.g., an image attached via `/attach`, or a PDF/UA
document with embedded Content Credentials) — Harvey reads and surfaces
those claims as part of the same "object identity" extraction step. This is
**read-only detection**, not manifest creation/signing — no key management.

Before this is scoped into a cycle, spike:

- Does `c2patool` (the official CAI CLI, https://github.com/contentauth/c2pa-rs)
  install cleanly and read manifests from common test files?
- What does its JSON output look like, and is it stable enough to parse?
- Can it be wrapped the same way `checkPopplerTools`/`runTool` wrap
  `pdfinfo`/`pdftotext`/`pdfimages` — optional binary, graceful no-op when
  absent or no manifest present?

If the spike succeeds, the natural extension point is: a `c2pa_present
bool` + `c2pa_claims TEXT` (JSON) pair of columns on `chunks`, populated by
the same ingest step that does identifier scanning. If it doesn't pan out
cleanly, this is dropped without affecting the rest of this design — the
identifier-extraction and section-aware-chunking work stands on its own.

## Out of scope for this cycle (possible future cycles)

- **Live Crossref/ORCID/OpenAlex/DataCite/ArXiv/PubMed/ROR/VIAF/SNAC/LCNAF
  lookups** via metadatatools' `Verify*`/`GetObject*` functions, or the
  `github.com/caltechlibrary/crossrefapi` (v1.0.10) /
  `github.com/caltechlibrary/dataciteapi` (v1.1.0) modules — both confirmed
  available, mature, with small dep footprints
  (`caltechlibrary/doitools`, `google/go-querystring`).
  **Explicitly deferred (decided 2026-06-11):** this is a new
  network-egress tool category for Harvey (no precedent besides the opt-in
  S3 remote) and deserves its own design pass once the offline slice is in
  use and its gaps are concrete. When revisited, `crossrefapi`'s `UpdateTo`
  relation (CrossRef's mechanism for signaling retractions/corrections) is
  the most promising target — it directly addresses the Toler/Tay
  retraction-status concern.
- Citation-graph traversal ("find papers that cite X")
- Version intelligence (preprint vs. published vs. corrected)
- C2PA manifest *creation*/signing by Harvey
- Any change to storage engines (stays SQLite + Fountain files)

## Next steps for the planning session

1. Confirm the paper-detection heuristic in `ragIngestPDF` (what counts as
   "paper-like" vs. a regular PDF) — false positives just mean unnecessary
   section-chunking attempts, false negatives just mean falling back to
   today's behavior, so this can be tuned iteratively.
2. ~~Decide whether `scholarly_identifiers.go` ports the `metadatatools`
   regex/checksum logic verbatim or adapts it~~ — resolved: metadatatools
   v0.1.1 is a direct dependency (`Normalize*`/`Validate*` for all 14
   types); Harvey only writes the `Find*` extraction layer on top.
3. ~~Run the C2PA/`c2patool` spike early~~ — still pending, independent of
   items 1-4 below.
4. Sequence: identifiers module → PDF ingest changes → KB schema → workspace
   onboarding, since each later step's tests can use fixtures produced by
   the earlier ones.
5. If the PDF is a journal article the structure is often predictable and
   heuristic techniques may apply for pulling out things like titles,
   authorship, abstract, acknowledgements and funding inforamation.
6. Additional document types like JATS may be worth supporting more directly
   as they present content in a structured manner.
7. ~~When known identifiers are available... CrossRef or DataCite metadata
   record...~~ — resolved: see "Out of scope for this cycle" above. The
   Caltech Library `crossrefapi`/`dataciteapi` modules are confirmed to
   exist and are the right building blocks *when* this is taken up, but not
   this cycle.
