# JATS Document Support Design for Harvey

**Status:** Draft (v1)
**Date:** 2026-06-12

## Overview

[JATS](https://jats.nlm.nih.gov/) (Journal Article Tag Suite, NISO Z39.96) is
the XML vocabulary used to represent journal articles for archiving,
publishing, and authoring. PubMed Central's Open Access subset distributes
full-text articles as JATS XML (commonly with a `.nxml` extension), and many
publishers and repositories produce JATS or JATS-derived XML (BITS for books
uses the same element vocabulary).

For Harvey, JATS is the most *structured* scholarly source it will encounter
— more so than PDF. Where `scholarly_pdf.go` has to **infer** section
boundaries (`classifySectionHeader`) and **scan** for identifiers with regex
(`FindIdentifiers`), a JATS document **declares** its sections
(`<sec sec-type="...">`), its identifiers (`<article-id pub-id-type="doi">`,
`<contrib-id contrib-id-type="orcid">`, `<institution-id
institution-id-type="ror">`, ...), and its references
(`<ref-list>/<ref>/<pub-id>`) as explicit, machine-readable tags.

This design proposes JATS as a **new entry in Harvey's existing
chunker/extractor → `EnrichedChunk` pattern** (per
`towards_a_scholarly_memory_design.md`, "next step #6"), plus a new ephemeral
read surface reached through `/read`'s type-aware dispatch (see
`document_reader_registry_design.md`, a companion design covering the
`DocumentReader` interface and `/read`/`/attach` dispatch shared by JATS, PDF,
and future document formats). No new storage engine, no new memory silo —
same `chunks` table, same `Identifiers`/`Citations`/`ChunkType` columns
already added in the v3 scholarly-memory work.

## Grounding in Harvey's current architecture

1. **Chunker/extractor registry** (`language_registry.go`): each language has
   a `CodeChunker` producing `[]EnrichedChunk` and an optional `DocExtractor`.
   `ragIngestFile` dispatches on `globalRegistry.DetectFromExtension(ext)` →
   `GetChunker(langID)`. JATS becomes language ID `"jats"`, registered for
   `.nxml`.

2. **`EnrichedChunk`** (`language_registry.go`) already has everything this
   design needs:
   ```go
   type EnrichedChunk struct {
       Content     string
       StartLine, StartCol, EndLine, EndCol int
       ChunkType   string               // "front_matter", "abstract", "introduction", ... "references", "body"
       Symbols     []string
       Docs        string
       Identifiers map[string][]string  // this document's own identifiers, keyed by IdentifierType
       Citations   []string             // identifiers found in this chunk pointing to OTHER works
   }
   ```
   JATS populates `ChunkType`, `Identifiers`, and `Citations` **structurally**
   (from tags) rather than heuristically (from regex/section-header text).

3. **`scholarly_pdf.go`** is the closest analog and is directly reusable:
   - `classifySectionHeader(line string) (chunkType string, ok bool)` and the
     `sectionHeaders` map become the **fallback** for JATS `<sec>` elements
     that have no recognized `sec-type` attribute — JATS authors frequently
     omit `sec-type` and rely on `<title>Introduction</title>` text alone.
   - `ragChunk(text string) []string` (the ~500-char splitter) is reused to
     split long `<sec>` text into multiple `EnrichedChunk`s, same as
     `scholarlyChunk` does per PDF section.
   - `diagramPageWarning`-style inline placeholders are the model for how to
     handle MathML (see "MathML handling" below).
   - `isPaperLike`/PDF "paper detection" has **no JATS equivalent** — a
     `.nxml` file with an `<article>` root *is* the structure signal. Every
     JATS document goes through section-aware chunking; there's no flat
     fallback path.

4. **`scholarly_identifiers.go`**: `FindIdentifiers(text) map[IdentifierType][]string`
   plus `Normalize*`/`Validate*` (via `github.com/caltechlibrary/metadatatools`
   v0.1.1) remain useful as a **supplementary** pass over free text (abstract,
   body paragraphs) for in-text identifier mentions not captured by
   `<article-id>`/`<pub-id>` tags. But for JATS, the **primary** identifier
   source is structural extraction straight from tags, normalized through the
   same `Normalize*` functions for canonical-form consistency with the PDF
   path.

5. **`pdf_extract.go` + `ragIngestPDF` (`commands.go`) + `/read-pdf`
   (`cmdReadPDF`)** establish the dual-path pattern this design mirrors:
   - A pure parsing function (`pdfExtract` → here, `jatsExtract`) with no
     workspace/agent dependencies, easy to unit-test against fixtures.
   - `ragIngestX(store, embedder, path)` — parse → chunk → `IngestEnriched`.
   - `cmdReadX` — parse → render a human/LLM-readable summary → inject as a
     `user`-role context message (ephemeral, not indexed).

6. **`ragIngestableExts` / `ragCollectFiles` / `ragIngest`** (`commands.go`):
   the extension-keyed map that gates `/rag ingest` and directory walks.
   `.pdf` is special-cased to `ragIngestPDF`; `.nxml` will be special-cased to
   `ragIngestJATS` the same way.

## JATS structural primer (the subset Harvey parses)

```
<article>
  <front>
    <journal-meta>
      <issn pub-type="...">...</issn>
      <journal-title-group><journal-title>...</journal-title></journal-title-group>
    </journal-meta>
    <article-meta>
      <article-id pub-id-type="doi|pmid|pmc|...">...</article-id>          (repeatable)
      <title-group><article-title>...</article-title></title-group>
      <contrib-group>
        <contrib contrib-type="author">
          <name><surname/><given-names/></name>
          <contrib-id contrib-id-type="orcid">...</contrib-id>
          <xref ref-type="aff" rid="aff1"/>
        </contrib>
      </contrib-group>
      <aff id="aff1">
        <institution-wrap><institution-id institution-id-type="ror">...</institution-id></institution-wrap>
      </aff>
      <pub-date>...</pub-date>
      <abstract>...<p>...</p></abstract>
      <kwd-group><kwd>...</kwd></kwd-group>
      <funding-group>
        <award-group>
          <funding-source><institution-wrap><institution-id institution-id-type="fundref|doi">...</institution-id></institution-wrap></funding-source>
          <award-id>...</award-id>
        </award-group>
      </funding-group>
    </article-meta>
  </front>
  <body>
    <sec sec-type="intro" id="sec1"><title>Introduction</title><p>...</p>
      <sec>...nested subsection...</sec>
    </sec>
    <sec sec-type="methods">...</sec>
    <sec sec-type="results">...</sec>
    <sec sec-type="discussion">...</sec>
  </body>
  <back>
    <ack><title>Acknowledgements</title><p>...</p></ack>
    <ref-list>
      <ref id="bib1">
        <element-citation>  <!-- or <mixed-citation> -->
          <article-title>...</article-title>
          <pub-id pub-id-type="doi">...</pub-id>
          <pub-id pub-id-type="pmid">...</pub-id>
        </element-citation>
      </ref>
    </ref-list>
  </back>
</article>
```

| JATS element | Harvey concept |
|---|---|
| `<article-id pub-id-type="doi\|pmid\|pmc\|...">` | `Identifiers["doi"\|"pmid"\|"pmcid"\|...]` (document's own) |
| `<contrib-id contrib-id-type="orcid">` | `Identifiers["orcid"]` (one per author, deduped) |
| `<institution-id institution-id-type="ror">` | `Identifiers["ror"]` (one per affiliation, deduped) |
| `<institution-id institution-id-type="fundref"\|"doi">` (in `funding-group`) | `Identifiers["fundref"]` |
| `<issn>` | `Identifiers["issn"]` |
| `<title-group>`, `<contrib-group>`, `<journal-meta>`, `<kwd-group>`, `<funding-group>`, `<pub-date>` | `ChunkType = "front_matter"` (synthetic summary chunk, §2) |
| `<abstract>` | `ChunkType = "abstract"` (structural — no heuristic needed) |
| `<sec sec-type="...">` / `<sec><title>...` | `ChunkType` via mapping table + `classifySectionHeader` fallback |
| `<ack>` | `ChunkType = "body"` (matches existing `sectionHeaders["acknowledgements"]`) |
| `<ref-list>/<ref>` | `ChunkType = "references"`; each `<pub-id>` → `Citations` |

## Proposed design

### 1. `jats_extract.go` — pure stdlib XML parser

A new file mirroring `pdf_extract.go`'s shape: no workspace/agent
dependencies, `encoding/xml` only (zero new dependencies, consistent with the
v3 "pure stdlib + metadatatools" theme).

```go
// JATSDocument is the parsed structure of a JATS article, holding the subset
// of front/body/back content Harvey's chunker and identifier extraction need.
type JATSDocument struct {
    Title        string
    Journal      string
    ArticleIDs   map[string]string   // pub-id-type -> value, e.g. "doi" -> "10.1234/..."
    AuthorORCIDs []string            // deduped, normalized
    AffiliationRORs []string         // deduped, normalized
    FunderIDs    []string            // deduped, normalized (fundref/DOI form)
    ISSNs        []string
    Abstract     string              // flattened text
    Keywords     []string
    Sections     []JATSSection       // body, in document order, recursively nested
    References   []JATSReference
}

// JATSSection is one <sec> (or <abstract>/<ack>), with nested subsections
// flattened by jatsChunk (see below) rather than here.
type JATSSection struct {
    SecType  string   // raw sec-type attribute, "" if absent
    Title    string   // <title> text, "" if absent
    Text     string   // flattened <p> text for this section only (not children)
    Children []JATSSection
}

// JATSReference is one <ref>, with any <pub-id> values extracted.
type JATSReference struct {
    ID      string            // <ref id="...">
    Text    string            // flattened citation text (best-effort, for display)
    PubIDs  map[string]string // pub-id-type -> value, e.g. "doi", "pmid"
}

// ParseJATS parses JATS XML into a JATSDocument. It does not validate against
// any DTD/schema — encoding/xml ignores DOCTYPE — and tolerates missing
// optional elements throughout.
func ParseJATS(data []byte) (*JATSDocument, error)
```

**Text flattening for `<p>`, `<title>`, `<abstract>`:** JATS inline markup
(`<italic>`, `<bold>`, `<sub>`, `<sup>`, `<xref>`, `<mml:math>`,
`<tex-math>`, ...) is mixed with text content. A small helper
(`flattenJATSText(innerXML []byte) string`) walks the XML tokens, keeps text
content and drops markup tags. For `<disp-formula>`/`<inline-formula>`
(commonly an `<alternatives>` wrapping both `<mml:math>` and `<tex-math>`):
if a `<tex-math>` sibling is present, its LaTeX source is embedded directly
(e.g. `$E=mc^2$`) — compact and legible to embeddings and LLMs alike; if only
`<mml:math>` is present (no TeX alternative), it falls back to a `[MATH]`
placeholder — the same "don't lose the reader, flag what's missing" approach
as `diagramPageWarning` for PDF diagram pages.

**Author/affiliation linkage is *not* resolved in v1.** `<contrib-id
contrib-id-type="orcid">` and `<institution-id institution-id-type="ror">`
values are collected as flat deduped lists (`AuthorORCIDs`,
`AffiliationRORs`) regardless of which `<contrib>`/`<aff>` they belong to.
Resolving `<xref ref-type="aff" rid="aff1">` → `<aff id="aff1">` to produce
per-author affiliation links is real complexity with no current consumer
(Harvey's `Identifiers` map has no per-person structure) — confirmed deferred
as a future "Open research item," same pattern as the v3 doc's C2PA spike.

### 2. Section-aware chunking — `jats_chunker.go`

```go
// jatsSectionTypes maps JATS sec-type attribute values to EnrichedChunk.ChunkType.
// Sections with an unrecognized or absent sec-type fall back to
// classifySectionHeader(section.Title) (from scholarly_pdf.go), then to "body".
var jatsSectionTypes = map[string]string{
    "intro":                 "introduction",
    "methods":               "methods",
    "materials|methods":     "methods",
    "materials":             "methods",
    "results":               "results",
    "discussion":            "discussion",
    "conclusions":           "conclusion",
    "supplementary-material": "body",
    "data-availability":      "body",
    "abbreviations":          "body",
}

// jatsChunk converts a parsed JATSDocument into section-tagged EnrichedChunks,
// in document order: a synthetic front_matter chunk, abstract, body sections
// (flattened, nested sections inherit the parent's ChunkType unless their own
// sec-type/title resolves to something else), acknowledgements, then
// references. Every chunk shares docIdentifiers (the document's own
// DOI/ORCID/ROR/FundRef/ISSN/PMID/PMCID). Reference-list chunks get Citations
// from each <ref>'s PubIDs.
func jatsChunk(doc *JATSDocument, title string) []EnrichedChunk
```

- **Nesting**: a `<sec>` with no recognized type **inherits its parent's**
  `ChunkType` (e.g., a "Statistical analysis" subsection under "Methods" with
  no `sec-type` of its own stays `"methods"`). This mirrors how PDF
  subsections between two recognized headings fall into the preceding
  section's type.
- **Splitting**: each resolved section's flattened text is passed through
  `ragChunk` (≤ ~500 chars/piece), same as `scholarlyChunk`.
- **Provenance header**: each chunk's `Content` is prefixed
  `[JATS: %q, journal: %s, section: %s]\n\n%s` (parallel to PDF's
  `[PDF: %q, section: %s, page %d-%d of %d]`).
- **`front_matter` chunk** (decided): `jatsChunk` emits one leading chunk with
  `ChunkType = "front_matter"`, `section: front_matter` in the provenance
  header, containing a rendered summary — title, author names (+ORCID),
  journal/ISSN, pub date, keywords, and funding sources/award IDs — built the
  same way as `JATSReader`'s default summary (§6) but scoped to front matter
  only. This has no PDF equivalent; it exists because JATS makes this metadata
  structurally available, and makes "who wrote / who funded this" directly
  retrievable without pulling the abstract chunk. It shares `docIdentifiers`
  like every other chunk, so DOI/ORCID/ROR/FundRef/ISSN/PMID/PMCID are all
  searchable from it.

### 3. Identifier and citation extraction — structural-first

- `doc.ArticleIDs["doi"|"pmid"|"pmcid"|...]`, `doc.AuthorORCIDs`,
  `doc.AffiliationRORs`, `doc.FunderIDs`, `doc.ISSNs` are normalized via the
  same `metadatatools.Normalize*` functions `scholarly_identifiers.go` already
  wraps (e.g. `NormalizeDOI`, `NormalizeORCID`, `NormalizeROR`,
  `NormalizeFundRef`, `NormalizeISSN`), then merged into
  `EnrichedChunk.Identifiers map[string][]string` — same shape as the PDF
  path, so no schema changes.
- **Supplementary regex pass**: `FindIdentifiers` still runs over `Abstract`
  and each section's flattened text, in case an in-text DOI/arXiv mention
  isn't also present as a structured `<pub-id>` (e.g., a dataset DOI
  mentioned only in a "Data availability" paragraph). Results are merged into
  `docIdentifiers`, deduped via `appendUnique` (already exported from
  `scholarly_identifiers.go`).
- **Citations**: for "references"-type chunks, each `<ref>`'s `PubIDs` values
  (normalized) populate `Citations` directly — no regex needed, and notably
  **more reliable** than the PDF path's regex-over-reference-list-text, since
  `<pub-id pub-id-type="doi">` is unambiguous.
- **In-text citation linkage** (confirmed deferred): JATS body text contains
  `<xref ref-type="bibr" rid="bib12">` pointing into `<ref-list>`. Resolving
  these would let a *body* chunk's `Citations` include the specific works it
  cites (richer than today's "citations only appear in the references
  chunk"). Same "Open research item" treatment as author/affiliation linkage
  (§1); both are KB-linkage-shaped problems best tackled together in a later
  cycle.

### 4. Registry integration (`language_registry.go`)

```go
var langJATS = LanguageInfo{
    ID:          "jats",
    Name:        "JATS",
    Extensions:  []string{".nxml"},
    HasChunking: true,
}
```

Registered via `r.RegisterLanguage(langJATS, nil, NewJATSChunker(), nil, nil, nil)`
in `initChunkers` (or a new `initJATS`, following the existing per-phase init
function convention). No `DocExtractor`, `CodeFormatter`, or
`SyntaxHighlighter` — those concepts don't map to JATS.

In addition, `initReaders` (see `document_reader_registry_design.md`)
registers `r.SetReader("jats", NewJATSReader())` against the same `langJATS`
ID — this is what makes `/read FILE.nxml [SECTION]` work (§6 below). The two
registrations are independent: the chunker drives RAG ingest (§5), the reader
drives ephemeral `/read`/`/attach`.

**`.xml` is deliberately out of scope for v1.** Generic `.xml` is ambiguous
(could be anything); `.nxml` is the de facto PMC/JATS convention and requires
no content-sniffing. Recognizing `.xml` files that happen to be JATS (via
root-element/DOCTYPE sniffing, similar to how `isPaperLike` sniffs PDF text)
is a natural v2 extension once `.nxml` handling is proven.

### 5. RAG ingest wiring (`commands.go`)

- Add `".nxml": true` to `ragIngestableExts`.
- `ragIngestJATS(store *RagStore, embedder Embedder, path string) (int, error)`:
  read file → `ParseJATS` → `jatsChunk` → `store.IngestEnriched(path, chunks, embedder)`.
  Parallel structure to `ragIngestPDF`, but simpler — no diagram-page tracking,
  no `isPaperLike` branch (every JATS file is "paper-like" by definition).
- In `ragIngest`'s per-file loop, branch on `filepath.Ext(absFile) == ".nxml"`
  → `ragIngestJATS`, same as the existing `.pdf` branch.
- **No new `chunks` columns** — `identifiers`/`citations`/`chunk_type` already
  exist from the v3 `enrichedAlterStmts` migration.

### 6. Ephemeral read surface — `JATSReader`

JATS's ephemeral read surface is **not** a JATS-specific command. Per
`document_reader_registry_design.md`, JATS registers a `JATSReader` —
implementing the shared `DocumentReader` interface — via
`SetReader("jats", NewJATSReader())`. `/read FILE.nxml [SECTION]` and
`/attach FILE.nxml` reach it through the same dispatch as PDF and any future
format; no `/read-jats` command, man page, or JATS-specific builtin tool is
needed.

`JATSReader.Render(data, path, opts)`:
- parses `data` with `ParseJATS`;
- if `opts.Selector` is empty, calls `renderJATSSummary(doc *JATSDocument)
  string` — a structured Markdown-ish summary: title, authors (+ORCID),
  journal/ISSN, identifiers (DOI/PMID/PMCID), abstract, section outline (type
  + title for each top-level section), keyword list, funding, and reference
  count (the JATS analog of `PDFReader`'s default whole-document summary);
- if `opts.Selector` is non-empty, `Render` resolves it against sections in
  two passes — first a case-insensitive match against `jatsSectionTypes`-style
  `sec-type` values (e.g. `"methods"`), then, if nothing matches, a
  case-insensitive match against each section's `<title>` text (e.g.
  `"Materials and Methods"`) — and returns that section's full flattened text
  instead of the summary. This is the JATS analog of `PDFReader`'s page-range
  selector, and reuses the same normalization as `jatsSectionTypes`/
  `classifySectionHeader` (§2).

`/attach` and `cmdAttachRemote` need no JATS-specific branch: the
`DetectFromExtension` + `GetReader` lookup in
`document_reader_registry_design.md` §4 covers `.nxml` the same way it covers
`.pdf`.

## Decisions (resolved 2026-06-12)

All open questions from this design pass, plus the companion
`document_reader_registry_design.md`'s, were resolved in one pass:

1. **`front_matter` chunk type** (§2) — added. A new `ChunkType =
   "front_matter"` synthetic chunk carries title/authors/journal/keywords/
   funding, emitted first by `jatsChunk`.
2. **MathML placeholder** (§1) — prefer `<tex-math>` (embed LaTeX source
   directly) when present in an `<alternatives>` block; fall back to `[MATH]`
   for `<mml:math>`-only formulas.
3. **`JATSReader` selector matching** (§6) — both: `sec-type` match first,
   then `<title>` text match (case-insensitive, reusing
   `jatsSectionTypes`/`classifySectionHeader`-style normalization).
4. **In-text citation / author-affiliation linkage** (§3, §1) — confirmed
   deferred as an "Open research item," consistent with how the v3 doc handled
   C2PA.
5. **Test fixtures** — both: hand-crafted `.nxml` snippets covering each
   element in the structural primer table, plus 1-2 real PMC OA articles for
   integration tests.

From `document_reader_registry_design.md`:

6. **`/read-pdf` fate** — removed entirely once `/read FILE.pdf [PAGES]`
   works via the registry dispatch; `harvey-read-pdf.7.md` is folded into
   `/read`'s docs.
7. **ODT/archive formats and `Render(data []byte, ...)`** — interface
   unchanged; `archive/zip.NewReader` works directly against `bytes.NewReader
   (data)`, no path-based variant needed.
8. **Binary-file warning fix** — bundled into the `/read` dispatch rewrite
   (same `cmdRead` edit, not a separate change).
9. **`.xml` disambiguation** — confirmed deferred; `DetectFromExtension(".xml")`
   stays unregistered until a future format's design needs content-sniffing.

## Testing strategy (for the implementation plan)

- `testdata/jats/*.nxml` — both hand-crafted snippets and 1-2 real PMC OA
  articles:
  - hand-crafted, covering each element precisely:
    - minimal `<article-meta>` exercising every `<article-id>`/`<contrib-id>`/
      `<institution-id>` type Harvey extracts (DOI, PMID, PMCID, ORCID, ROR,
      FundRef, ISSN);
    - nested `<sec>` with mixed `sec-type` presence/absence (recognized type,
      title-only, neither — to exercise the inheritance fallback);
    - `<ref-list>` with `<element-citation>` and `<mixed-citation>` variants,
      each with `<pub-id>` DOI/PMID;
    - a `<p>` containing `<disp-formula>`/`<inline-formula>` with
      `<alternatives>{<mml:math>, <tex-math>}` and `<mml:math>`-only, plus
      `<italic>`/`<xref>`, for the text-flattening helper;
  - 1-2 real PMC OA `.nxml` articles (CC-licensed), used as integration-test
    fixtures for `ragIngestJATS`/`JATSReader` end-to-end.
- `jats_extract_test.go` — `ParseJATS` against fixtures; `flattenJATSText`
  unit cases.
- `jats_chunker_test.go` — `jatsChunk` → `ChunkType`/`Identifiers`/`Citations`
  assertions, including the nesting-inheritance and front-matter cases.
- `ragIngestJATS` integration test (mirrors `pdf_extract_test.go`'s ingest
  tests) — verifies `IngestEnriched` round-trip with the existing
  `identifiers`/`citations` columns.
- `JATSReader.Render` tests (`jats_reader_test.go` or alongside
  `jats_extract_test.go`) — summary output for a fixture with no `Selector`,
  full section text for a fixture with `Selector` set, and error handling for
  malformed input; mirrors however `PDFReader.Render` is tested per
  `document_reader_registry_design.md`.

## Next steps

1. ~~Resolve the open questions above~~ — done (see "Decisions" above).
2. Write `jats_support_plan.md` — an implementation plan sequencing:
   `jats_extract.go` (+ tests against fixtures) → `jats_chunker.go` +
   registry integration (+ tests) → RAG ingest wiring (+ tests) →
   `JATSReader` (+ tests), slotted into
   `document_reader_registry_design.md`'s overall sequencing (`DocumentReader`
   interface/registry → `PDFReader` retrofit → `/read`/`/attach` dispatch →
   `JATSReader`).
3. Build `testdata/jats/*.nxml` fixtures alongside step 1, so later steps'
   tests have real data to work against from the start (same approach the v3
   scholarly-memory work used).
