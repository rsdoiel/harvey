# JATS Document Support — Implementation Plan

**Status:** Plan (v1)
**Date:** 2026-06-12
**Implements:** `jats_support_design.md` (all "Decisions" resolved 2026-06-12)

This plan sequences the JATS work into four steps, matching
`jats_support_design.md`'s "Next steps" ordering: `jats_extract.go` →
`jats_chunker.go` + registry integration → RAG ingest wiring → `JATSReader`.
Step 4 depends on `document_reader_registry_plan.md` Step 1 (the
`DocumentReader` interface, `SetReader`/`GetReader`/`initReaders`) — see that
plan's "Cross-plan sequencing" section. Steps 1-3 have no such dependency and
can proceed independently/in parallel with that plan.

## Plan-level decisions (not covered by the design doc)

Three implementation details came up while grounding this plan in
`scholarly_pdf.go`/`scholarly_identifiers.go` and the JATS structural primer:

### A. JATS `pub-id-type`/`*-id-type` values → Harvey `IdentifierType` strings

JATS attribute values don't always match `scholarly_identifiers.go`'s
`IdentifierType` string constants directly. The main mismatch: JATS uses
`pub-id-type="pmc"` for the PubMed Central ID, but Harvey's identifier type
is `IdentifierPMCID` (string value `"pmcid"`). **Decided:** `jats_extract.go`
applies a small lookup table when populating `JATSDocument.ArticleIDs` and
when converting to `EnrichedChunk.Identifiers`/`JATSReference.PubIDs`:
```go
// jatsIDTypeAliases maps JATS pub-id-type/contrib-id-type/institution-id-type
// values that differ from Harvey's IdentifierType strings to their
// IdentifierType equivalent. Types not listed pass through unchanged
// (e.g. "doi" -> "doi", "orcid" -> "orcid", "ror" -> "ror").
var jatsIDTypeAliases = map[string]string{
    "pmc": "pmcid",
}
```
Unrecognized `pub-id-type`/`*-id-type` values (e.g. `"publisher-id"`,
`"coden"`, `"sici"`) are passed through as-is into `ArticleIDs`/`PubIDs` but
are **not** normalized via `metadatatools.Normalize*` (no matching
`IdentifierType`) and are excluded when building
`EnrichedChunk.Identifiers`/`Citations` (which only include recognized
`IdentifierType`s, per `convertIdentifierMap`'s existing contract).

### B. Section `ChunkType` resolution order

`jats_chunker.go`'s `jatsChunk` resolves each `<sec>`'s `ChunkType` in this
order (first match wins):
1. `jatsSectionTypes[section.SecType]` (exact, case-sensitive — JATS
   `sec-type` values are conventionally lowercase-hyphenated).
2. `classifySectionHeader(section.Title)` (from `scholarly_pdf.go` —
   case-insensitive, strips outline-number prefixes/trailing punctuation).
3. The resolved `ChunkType` of the nearest ancestor `<sec>` (inheritance, per
   design §2's "Statistical analysis under Methods" example).
4. `"body"` (top-level `<sec>` with no recognized type/title and no parent).

This order is not explicitly stated in the design doc's prose but is implied
by §2's bullet list — written out here so the implementation and its tests
agree on tie-breaking (e.g. a subsection titled "Discussion" inside a
`sec-type="results"` parent gets `"discussion"` via rule 2, not `"results"`
via rule 3 — title match outranks inheritance).

### C. "Full flattened text" for `JATSReader`'s selector match (§6)

When `opts.Selector` matches a section (by `sec-type` or title, per design
§6), `Render` returns that section's **own** `Text` plus every descendant
section's `Text`, concatenated in document order with each descendant's
`<title>` (if any) rendered as a Markdown-style heading one level deeper than
its parent. This gives a complete, readable excerpt of "Methods and its
subsections" rather than just the top-level `<sec>`'s lead-in paragraph
before its first subsection.

## Step 1 — `jats_extract.go` + fixtures

**New file `jats_extract.go`**, pure `encoding/xml`, no workspace/agent
dependencies (mirrors `pdf_extract.go`'s independence):

```go
type JATSDocument struct {
    Title           string
    Journal         string
    ArticleIDs      map[string]string // IdentifierType string -> value, e.g. "doi" -> "10.1234/..."
    AuthorORCIDs    []string          // deduped, normalized via metadatatools.NormalizeORCID
    AffiliationRORs []string          // deduped, normalized via metadatatools.NormalizeROR
    FunderIDs       []string          // deduped, normalized via metadatatools.NormalizeFundRef
    ISSNs           []string          // normalized via metadatatools.NormalizeISSN
    Abstract        string            // flattened text
    Keywords        []string
    Sections        []JATSSection     // body, document order, recursively nested
    References      []JATSReference
}

type JATSSection struct {
    SecType  string // raw sec-type attribute, "" if absent
    Title    string // <title> text, flattened, "" if absent
    Text     string // flattened <p> text for THIS section only (not children)
    Children []JATSSection
}

type JATSReference struct {
    ID     string            // <ref id="...">
    Text   string            // flattened citation text (best-effort, for display)
    PubIDs map[string]string // IdentifierType string -> value (via jatsIDTypeAliases, decision A)
}

// ParseJATS parses JATS XML into a JATSDocument. Tolerates missing optional
// elements; does not validate against any DTD/schema.
func ParseJATS(data []byte) (*JATSDocument, error)

// flattenJATSText walks the XML tokens in innerXML and returns flattened
// text: markup tags are dropped, their text content kept. <tex-math> chardata
// is wrapped as "$...$"; a standalone <mml:math> (no <tex-math> sibling in an
// enclosing <alternatives>) becomes the literal "[MATH]" placeholder, and its
// subtree is skipped. Within <alternatives>, <tex-math> is preferred and any
// sibling <mml:math> is skipped entirely.
func flattenJATSText(innerXML []byte) string
```

**Parsing approach** (internal, not exported): unexported `xml*` struct types
mirror the structural primer table —
`xmlArticleMeta{ArticleIDs []xmlPubID, TitleGroup, ContribGroup []xmlContrib,
Aff []xmlAff, PubDate, Abstract xmlRaw, KwdGroup, FundingGroup}`,
`xmlContrib{ContribType string, ContribIDs []xmlPubID}`,
`xmlAff{ID string, InstitutionIDs []xmlPubID via institution-wrap>institution-id}`,
`xmlFundingGroup{AwardGroups []struct{FundingSources []xmlAff-like, AwardIDs []string}}`,
`xmlSec{SecType string, Title xmlRaw, Paragraphs []xmlRaw, Subsecs []xmlSec
  ` + "`xml:\"sec\"`" + `}` (recursive), `xmlRefList{Refs []xmlRef}`,
`xmlRef{ID string, ElementCitation, MixedCitation xmlCitation}`,
`xmlCitation{PubIDs []xmlPubID, Raw xmlRaw}`. A shared `xmlPubID{Type string
` + "`xml:\"...-id-type,attr\"`" + `, Value string ` + "`xml:\",chardata\"`" + `}`
covers `<article-id>`/`<contrib-id>`/`<institution-id>`/`<pub-id>` (same
shape, different attribute names per JATS element — each gets its own struct
field tag but shares the `Type`/`Value` shape). `xmlRaw{Inner []byte
` + "`xml:\",innerxml\"`" + `}` captures raw inner XML for `<title>`/`<p>`/
`<abstract>`/citation text, passed to `flattenJATSText`. `ParseJATS` decodes
into a top-level `xmlArticle{Front, Body xmlBody, Back xmlBack}`, then maps
each piece into `JATSDocument`'s public shape, applying decision A's
`jatsIDTypeAliases` and `metadatatools.Normalize*`/`appendUnique` (reused from
`scholarly_identifiers.go`) while building `ArticleIDs`/`AuthorORCIDs`/
`AffiliationRORs`/`FunderIDs`/`ISSNs`.

**`testdata/jats/` fixtures** (hand-crafted, one concern per file so test
failures are easy to localize):
- `minimal.nxml` — smallest valid `<article>`: title, one author with ORCID,
  one affiliation with ROR, DOI/PMID/PMC article-ids (exercises decision A's
  `"pmc"`→`"pmcid"` alias), one ISSN, abstract, 2-3 keywords, one
  `funding-group`/`award-group` with FundRef ID and award-id, one top-level
  `<sec sec-type="intro">` and one `<sec sec-type="methods">`, no `<back>`.
- `nested-sections.nxml` — `<sec sec-type="methods">` containing a child
  `<sec>` with no `sec-type` but `<title>Statistical Analysis</title>`
  (inheritance case, decision B rule 3) and another child `<sec>` with
  `<title>Discussion</title>` and no `sec-type` (title-overrides-inheritance
  case, decision B rule 2 vs. 3) nested under the `methods` parent — plus one
  top-level `<sec>` with neither `sec-type` nor a recognized `<title>`
  (decision B rule 4, falls to `"body"`).
- `references.nxml` — `<ref-list>` with one `<ref>` using
  `<element-citation>` (DOI + PMID `<pub-id>`s) and one using
  `<mixed-citation>` (DOI only), to exercise both citation element variants.
- `math.nxml` — one `<p>` with `<disp-formula><alternatives><mml:math>...
  </mml:math><tex-math>E=mc^2</tex-math></alternatives></disp-formula>`
  (expect `$E=mc^2$` in flattened text) and a second `<p>` with
  `<inline-formula><mml:math>...</mml:math></inline-formula>` and no
  `tex-math` sibling (expect `[MATH]`), plus `<italic>`/`<xref ref-type="bibr"
  rid="bib1">` inline markup mixed with plain text in the same `<p>`, to
  verify both are dropped (xref's text content kept, tag dropped).
- `pmc-oa-*.nxml` — 1-2 real PMC Open Access articles (CC-BY licensed),
  obtained separately (not generated as part of this plan — flagged as a
  prerequisite for the integration tests in Steps 3-4; any small CC-BY PMC OA
  `.nxml` article works, e.g. a short correspondence/letter to keep fixture
  size down).

**Tests — `jats_extract_test.go`:**
- `TestParseJATS_minimal` — every field of `JATSDocument` populated from
  `minimal.nxml` matches expected values, including the `"pmc"`→`"pmcid"`
  alias and normalized ORCID/ROR/FundRef/ISSN forms.
- `TestParseJATS_nestedSections` — `nested-sections.nxml`'s `Sections` tree
  shape (children present, `SecType`/`Title` per node) — `ChunkType`
  resolution itself is tested in Step 2, this test only checks the parsed
  tree structure is correct.
- `TestParseJATS_references` — `references.nxml`'s `References` slice, both
  citation variants produce the same `PubIDs` shape.
- `TestFlattenJATSText_mathTexPreferred`, `TestFlattenJATSText_mathMLOnly`,
  `TestFlattenJATSText_inlineMarkup` — against `math.nxml`'s `<p>` innerXML
  (and small inline snippets), covering decision in design §1.
- `TestParseJATS_malformedXML` — invalid XML returns a non-nil `error`.
- `TestParseJATS_pmcOA` — `pmc-oa-*.nxml` parses without error and produces a
  non-empty `Title`, `Sections`, and at least one `ArticleIDs["doi"]`.

## Step 2 — `jats_chunker.go` + registry integration

**New file `jats_chunker.go`**:

```go
// jatsSectionTypes maps JATS sec-type attribute values to EnrichedChunk.ChunkType.
var jatsSectionTypes = map[string]string{
    "intro":                  "introduction",
    "methods":                "methods",
    "materials|methods":      "methods",
    "materials":              "methods",
    "results":                "results",
    "discussion":             "discussion",
    "conclusions":            "conclusion",
    "supplementary-material":  "body",
    "data-availability":       "body",
    "abbreviations":           "body",
}

// jatsChunk converts doc into section-tagged EnrichedChunks: a leading
// front_matter chunk, then abstract, then body sections in document order
// (flattened via resolveChunkType + ragChunk, decision B for type
// resolution), then references. Every chunk shares docIdentifiers — the
// document's own normalized DOI/ORCID/ROR/FundRef/ISSN/PMID/PMCID
// (ArticleIDs + AuthorORCIDs + AffiliationRORs + FunderIDs + ISSNs, converted
// to map[string][]string). Reference chunks' Citations come from each
// JATSReference.PubIDs (normalized, decision A) — supplemented by
// findCitations(text, docIdentifiers) for any in-text identifier mentions
// not captured structurally (design §3's "supplementary regex pass").
func jatsChunk(doc *JATSDocument, title string) []EnrichedChunk

// resolveChunkType implements decision B's 4-rule resolution order for one
// section, given its already-resolved parent ChunkType ("" for top-level).
func resolveChunkType(sec *JATSSection, parentType string) string

// flattenSectionTree depth-first walks sec and its children, returning one
// EnrichedChunk per ragChunk piece of every node's own Text (front_matter and
// abstract are handled separately by jatsChunk, not via this walk).
func flattenSectionTree(sec *JATSSection, chunkType string, title, journal string, docIdentifiers map[string][]string) []EnrichedChunk

// renderFrontMatter builds the front_matter chunk's Content: title, author
// names (+ORCID), journal/ISSN, pub date, keywords, funding sources/award
// IDs — shared with JATSReader's renderJATSSummary (Step 4), factored as a
// shared helper (see Step 4).
func renderFrontMatter(doc *JATSDocument) string
```

Provenance header for body/abstract/references chunks:
`[JATS: %q, journal: %s, section: %s]\n\n%s` (per design §2). The
`front_matter` chunk's header is `[JATS: %q, journal: %s, section:
front_matter]\n\n%s`.

**Registry integration (`language_registry.go`):**
```go
var langJATS = LanguageInfo{
    ID:          "jats",
    Name:        "JATS",
    Extensions:  []string{".nxml"},
    HasChunking: true,
}
```
Registered via `r.RegisterLanguage(langJATS, nil, NewJATSChunker(), nil, nil,
nil)` inside `initChunkers` (existing per-phase init function — no new
`initJATS` needed, this is one more call alongside the other
`RegisterLanguage` calls already there). `NewJATSChunker()` returns a
`CodeChunker` implementation whose `Chunk` method calls `ParseJATS` then
`jatsChunk`. Because `initChunkers` runs before `initReaders`
(`document_reader_registry_plan.md` Step 1), `langJATS` is registered before
`initReaders`'s `SetReader("jats", NewJATSReader())` (Step 4) runs.

**Tests — `jats_chunker_test.go`:**
- `TestResolveChunkType_secTypeMatch`, `_titleMatch`, `_inheritance`,
  `_fallbackBody` — one test per decision-B rule, using small hand-built
  `JATSSection` values (no XML needed).
- `TestJatsChunk_frontMatter` — first chunk from `minimal.nxml` has
  `ChunkType == "front_matter"`, contains author/journal/funding info, and
  `Identifiers` matches the document's normalized identifiers.
- `TestJatsChunk_nestedSections` — `nested-sections.nxml` produces chunks
  with `ChunkType`s matching decision B's worked examples (methods parent +
  inherited-methods child + discussion child).
- `TestJatsChunk_references` — `references.nxml`'s references chunk(s) have
  `ChunkType == "references"` and `Citations` populated from `PubIDs`.
- `TestJatsChunk_sharedIdentifiers` — every chunk from `minimal.nxml` shares
  an identical `Identifiers` map (same map value or deep-equal).
- `TestNewJATSChunker_registered` — `globalRegistry.GetChunker("jats")` is
  non-nil and `DetectFromExtension(".nxml")` returns `("jats", true)`.

## Step 3 — RAG ingest wiring (`commands.go`)

- Add `".nxml": true` to `ragIngestableExts` (`commands.go:4983-4987`).
- New function, placed near `ragIngestPDF`:
  ```go
  // ragIngestJATS reads path, parses it with ParseJATS, chunks it with
  // jatsChunk, and stores the result via store.IngestEnriched. Returns the
  // number of chunks ingested.
  func ragIngestJATS(store *RagStore, embedder Embedder, path string) (int, error)
  ```
  Body: `os.ReadFile` → `ParseJATS` → `jatsChunk(doc, doc.Title)` →
  `store.IngestEnriched(path, chunks, embedder)` → `len(chunks), err`. No
  diagram-page tracking, no `isPaperLike` branch (every `.nxml` file is
  paper-like by construction, per design §6 grounding item 3).
- `ragIngest`'s per-file loop (`commands.go:~5104-5127`): add an
  `ext == ".nxml"` branch calling `ragIngestJATS`, parallel to the existing
  `.pdf`/`ragIngestPDF` branch.
- `ragIngestS3Prefix` (`commands.go:~5171`): same `.nxml`/`ragIngestJATS`
  branch alongside its existing `.pdf` check.

**Tests:**
- `TestRagIngestJATS_minimal` — ingest `minimal.nxml` into a temp `RagStore`
  (mirrors `pdf_extract_test.go`'s ingest test setup), then query the store
  and verify at least one result has `ChunkType == "front_matter"` and
  `Identifiers["doi"]` set.
- `TestRagIngestJATS_pmcOA` — ingest a `pmc-oa-*.nxml` fixture, verify
  non-zero chunk count and that a `"references"`-typed chunk has non-empty
  `Citations`.
- `TestRagIngest_nxmlExtensionDispatch` — `ragIngestableExts[".nxml"]` is
  `true`; `ragIngest` on a directory containing a `.nxml` file calls
  `ragIngestJATS` (can be verified via the resulting store contents rather
  than mocking).
- `TestRagIngestS3Prefix_nxml` — same dispatch check for the S3 path, using
  whatever in-memory/mock S3 fixture `pdf_extract_test.go`'s S3 tests (if any)
  already establish; if no S3 test infrastructure exists yet for PDF, this
  test is skipped with the same `skip` mechanism and a one-line note that it
  mirrors the PDF gap.

## Step 4 — `JATSReader`

Depends on `document_reader_registry_plan.md` Step 1
(`DocumentReader`/`ReadOptions`/`SetReader`/`GetReader`/`initReaders`).

**New file `jats_reader.go`**:

```go
type JATSReader struct{}

func NewJATSReader() *JATSReader { return &JATSReader{} }

func (j *JATSReader) Language() string { return "jats" }

// Render parses data with ParseJATS. If opts.Selector is empty, returns
// renderJATSSummary(doc) — a Markdown-ish overview: title, authors (+ORCID),
// journal/ISSN, identifiers (DOI/PMID/PMCID), abstract, section outline
// (type + title per top-level section), keywords, funding, reference count.
// If opts.Selector is non-empty, findSection(doc, opts.Selector) resolves it
// (two-pass: sec-type exact match, then <title> exact match,
// case-insensitive — design §6) and Render returns that section's full
// flattened text per decision C (own Text + descendants, descendant titles
// rendered as deeper Markdown headings). If no section matches, Render
// returns an error naming the selector and listing available sec-types/titles
// (so /read's error message is actionable).
func (j *JATSReader) Render(data []byte, path string, opts ReadOptions) (string, error)

// renderJATSSummary builds the default (no-selector) rendering. Its
// front-matter portion (title/authors/journal/identifiers/keywords/funding)
// reuses jats_chunker.go's renderFrontMatter (Step 2) so the front_matter
// RAG chunk and /read's default summary stay textually consistent; this
// function adds the abstract, section outline, and reference count on top.
func renderJATSSummary(doc *JATSDocument) string

// findSection implements design §6's two-pass selector resolution across
// doc.Sections (recursively): pass 1 matches selector against each section's
// SecType (case-insensitive exact match against jatsSectionTypes' keys AND
// against the raw SecType value, so both "methods" and any sec-type alias
// resolve); pass 2, if pass 1 finds nothing, matches selector against each
// section's Title (case-insensitive exact match, whitespace-trimmed).
// Returns the first match in document order, or nil.
func findSection(doc *JATSDocument, selector string) *JATSSection
```

Registration — `initReaders` (`document_reader_registry_plan.md` Step 1)
gains the line already stubbed there:
```go
r.SetReader("jats", NewJATSReader())
```
No `langJATS` `RegisterLanguage` call is needed here — Step 2 already
registered it via `initChunkers`, which runs before `initReaders`.

**`/read`/`/attach` integration**: no JATS-specific code in `commands.go` —
`document_reader_registry_plan.md` Step 3's `cmdRead` rewrite and Step 4's
`cmdAttach`/`cmdAttachRemote` simplification dispatch to `JATSReader` purely
via `DetectFromExtension(".nxml")` → `GetReader("jats")`, identically to PDF.
`/read paper.nxml` → `Render(data, path, ReadOptions{})` (summary);
`/read paper.nxml#Methods` → `path, selector, _ := strings.Cut(arg, "#")`
gives `selector = "Methods"` → `Render(data, path,
ReadOptions{Selector: "Methods"})` (full Methods section text, found via
`findSection`'s title pass since `"Methods"` capitalized rarely matches a raw
lowercase `sec-type`).

**Tests — `jats_reader_test.go`:**
- `TestJATSReader_Render_summaryNoSelector` — `minimal.nxml`, empty
  `Selector`; output contains `Title:`, author names, `DOI:`, abstract text,
  and a section-outline line per top-level section.
- `TestJATSReader_Render_secTypeSelector` — `Selector: "methods"` on
  `nested-sections.nxml` returns the methods section's text plus its
  "Statistical Analysis" and "Discussion" subsections (decision C), each
  subsection title rendered as a heading.
- `TestJATSReader_Render_titleSelector` — `Selector: "Materials and Methods"`
  (a fixture where a section's `<title>` is exactly that, with no matching
  `sec-type`) falls through to the title pass and returns the same section.
- `TestJATSReader_Render_noMatch` — `Selector: "nonexistent"` returns an
  error mentioning the selector and listing available section types/titles.
- `TestJATSReader_Render_malformedXML` — non-JATS/invalid bytes returns an
  error from `ParseJATS`, propagated by `Render`.
- `TestFindSection_secTypeBeforeTitle` — a fixture where one section's
  `sec-type` equals another section's `<title>` text (adversarial overlap);
  `findSection` returns the `sec-type` match (pass 1 wins), confirming
  pass-ordering.

## Cross-plan integration checkpoint

Once this plan's Step 4 and `document_reader_registry_plan.md`'s Steps 1+3
are both done, add one end-to-end test (either file is fine —
`jats_reader_test.go` is suggested) exercising the full `/read` path:
`/read testdata/jats/minimal.nxml` and `/read
testdata/jats/nested-sections.nxml#methods` through `cmdRead` itself (not
just `JATSReader.Render` directly), verifying the injected message has no
fenced-code wrapper (readers supply their own provenance header, per
`document_reader_registry_design.md` §3).
