# Document Reader Registry & `/read` Dispatch Design for Harvey

**Status:** Draft (v1)
**Date:** 2026-06-12

## Overview

Harvey's `/read` command (`cmdRead`, `commands.go:2387`) currently has **no
type awareness**: it reads raw bytes and wraps them in a fenced code block,
regardless of file type. For text/code/Markdown this is fine. For a PDF, it
would inject raw binary bytes into the conversation — which is presumably why
`/read-pdf` exists as a *separate* command with its own extraction pipeline
(`pdfExtract`). `/attach` (`cmdAttach`, `commands.go:2839`) has its own
hardcoded `.pdf` special case for the same reason.

This is a smaller-scale version of the same problem
`jats_support_design.md` is solving for JATS: every new structured document
type (JATS now; Final Draft, Open Screenplay Format, FadeIn, ODT, ... over
time) would otherwise need its own `/read-X` command and its own special case
in `/attach`.

This design proposes a **`DocumentReader`** handler — registered per-language
in the existing `LanguageRegistry` (`language_registry.go`), following the
exact `SetExtractor`/`GetExtractor`/`initExtractors` pattern already
established in `doc_extractors.go` for `CodeChunker`'s sibling,
`DocExtractor`. `/read` and `/attach` both look up a `DocumentReader` for the
detected file type before falling back to today's behavior. PDF (retrofitting
`cmdReadPDF`) and JATS (`jats_support_design.md`) become the first two
registered readers; the interface is the contract future formats implement.

## Grounding in current architecture

1. **`LanguageRegistry`** (`language_registry.go`) already does extension-keyed
   dispatch: `DetectFromExtension(ext) (langID, ok)`, then
   `GetChunker`/`GetExtractor`/`GetFormatter`/`GetHighlighter`. Adding
   `GetReader` is the same shape.

2. **`SetExtractor`/`initExtractors`** (`doc_extractors.go`) is the precedent
   for adding a handler *without* changing `RegisterLanguage`'s signature (which
   has ~17 call sites, all currently passing positional `nil`s for unused
   handlers):
   ```go
   func (r *LanguageRegistry) SetExtractor(id string, e DocExtractor) {
       r.extractors[id] = e
   }
   func initExtractors(r *LanguageRegistry) {
       r.SetExtractor("c", NewCDocExtractor("c"))
       // ...
   }
   ```
   `SetReader`/`GetReader`/`initReaders` follow this exact shape — no changes
   to `RegisterLanguage` or its call sites.

3. **`cmdRead`** (`commands.go:2387`): for each argument, reads bytes
   (workspace-relative or via `NewRemoteReader` for URIs), wraps in a fenced
   ` ```path\n...\n``` ` block, appends to one `user`-role context message.
   This is the **fallback path** for types with no registered reader — it
   stays as-is for source/Markdown/text.

4. **`cmdReadPDF`** (`commands.go:2677`) + **`pdfExtract`** (`pdf_extract.go`):
   the existing PDF-specific pipeline — page-range selection
   (`readPDFMaxPages` cap), poppler-based extraction, diagram-page detection.
   This logic becomes `PDFReader.Render`.

5. **`cmdAttach`** (`commands.go:2839`): has an explicit
   `if strings.ToLower(filepath.Ext(absPath)) == ".pdf" { return cmdReadPDF(...) }`
   branch (line 2873) before its generic MIME-based image/text routing
   (`attachDetectMIME`/`attachIsImageMIME`/`attachImage`/`attachText`). This
   branch becomes a `GetReader` lookup; image handling is unaffected (images
   aren't "documents with renderable text" — they stay on the vision/MIME
   path).

6. **`jats_support_design.md` §6** proposed a JATS-specific `/read-jats` +
   `read_jats` tool. Under this design, JATS instead registers a
   `JATSReader` via `SetReader("jats", ...)` — `/read FILE.nxml [SECTION]`
   reaches it through the same dispatch as PDF. (§6 of that doc is updated to
   point here.)

7. **`LanguageDetector.Detect(filePath, content) (lang string, confidence
   float64)`** (`language_registry.go`) already exists as a *content-based*
   detection interface for code, used when extension alone is ambiguous. This
   is the natural extension point for formats that share an extension (see
   §7 below) — no new interface needed, just new implementations.

## Proposed design

### 1. `DocumentReader` interface

```go
// ReadOptions carries format-specific selection/limits for DocumentReader.Render.
// Selector is free-form per format: a PDF page range ("40-55"), a JATS
// section type or title, etc. MaxBytes of 0 means "use the reader's default".
type ReadOptions struct {
    Selector string
    MaxBytes int
}

// DocumentReader renders a structured document's raw bytes into a
// human/LLM-readable string suitable for injection as conversation context.
// Render does not touch the filesystem or workspace — path is used only for
// provenance text (e.g. "[PDF: \"path\", page 1-10 of 42]").
type DocumentReader interface {
    Render(data []byte, path string, opts ReadOptions) (string, error)

    // Language returns the language/format ID this reader handles,
    // matching the ID it was registered under.
    Language() string
}
```

### 2. Registry wiring

```go
func (r *LanguageRegistry) SetReader(id string, dr DocumentReader) {
    r.readers[id] = dr
}

func (r *LanguageRegistry) GetReader(id string) DocumentReader {
    return r.readers[id] // nil if absent
}

// initReaders wires DocumentReaders for registered formats.
func initReaders(r *LanguageRegistry) {
    r.SetReader("pdf", NewPDFReader())
    r.SetReader("jats", NewJATSReader())
}
```

`LanguageRegistry.readers map[string]DocumentReader` is added to the struct
and initialized in `NewLanguageRegistry`, mirroring `extractors`.

`.pdf` and `.nxml` need entries in `LanguageRegistry`'s extension table even
though PDF/JATS aren't "programming languages" — `RegisterLanguage` is already
used for non-code formats (Markdown, JSON, YAML, etc.), so:
```go
var langPDF  = LanguageInfo{ID: "pdf",  Name: "PDF",  Extensions: []string{".pdf"}}
var langJATS = LanguageInfo{ID: "jats", Name: "JATS", Extensions: []string{".nxml"}}
```

### 3. `/read` dispatch (`cmdRead`)

For each file argument:
1. Resolve path, check read permission (unchanged).
2. `ext := filepath.Ext(rel)`; `langID, ok := globalRegistry.DetectFromExtension(ext)`.
3. If `ok` and `reader := globalRegistry.GetReader(langID)` is non-nil:
   - Read raw bytes, call `reader.Render(data, rel, ReadOptions{Selector: optionalArg})`.
   - Append the rendered string directly (it already carries its own
     provenance header — no extra fenced-block wrapping).
4. Otherwise (today's behavior, with one fix): read raw bytes;
   - if `isTextContent(data)` (already used by `ragIngestFile`), wrap in a
     fenced ` ```path ` block as today;
   - if **not** text and no reader is registered, print
     `✗ path: binary file, no reader registered for "<ext>"` instead of
     injecting raw bytes — this is the concrete bug `/read foo.pdf` has today
     (before PDF gets `PDFReader`, and for any future binary format without a
     reader yet).

`SECTION`/`PAGES`-style optional arguments: `/read FILE.pdf 40-55` and
`/read FILE.nxml "Methods"` both pass their trailing argument as
`ReadOptions.Selector` — each reader interprets it in its own format (page
range vs. section name/type). `/read FILE` with no selector means "whole
document" (subject to the reader's own size caps, e.g. PDF's 20-page limit).

### 4. `/attach` simplification (`cmdAttach`)

Replace the hardcoded:
```go
if strings.ToLower(filepath.Ext(absPath)) == ".pdf" {
    return cmdReadPDF(a, []string{absPath}, out)
}
```
with:
```go
if langID, ok := globalRegistry.DetectFromExtension(filepath.Ext(absPath)); ok {
    if reader := globalRegistry.GetReader(langID); reader != nil {
        data, err := os.ReadFile(absPath)
        // ... render via reader, inject, return
    }
}
```
falling through to the existing MIME-based image/text routing when no reader
matches — unchanged for images and plain text/code attachments.
`cmdAttachRemote` gets the analogous change (currently has its own `.pdf`
special case at line 2911).

### 5. PDF retrofit — `PDFReader`

`PDFReader.Render` wraps the existing `cmdReadPDF` logic almost verbatim:
parse `opts.Selector` as a page range (or "whole doc" up to
`readPDFMaxPages`), call `pdfExtract(path-derived-tempfile-or-data, pages)`,
return the same formatted text `cmdReadPDF` currently writes to `out` and
injects via `a.AddMessage`.

One wrinkle: `pdfExtract` (`pdf_extract.go:171`) takes a **file path**, not
bytes (it shells out to `pdftotext`/`pdfinfo`/`pdfimages`). `Render(data
[]byte, path string, ...)` would need either (a) `pdfExtract` to accept a
path directly when available (the common case — `/read`/`/attach` operate on
workspace/local files) with `data` unused for PDF, or (b) write `data` to a
temp file when `path` isn't a real filesystem path (e.g. the remote-download
case `cmdAttachRemote` already temp-files PDFs for this reason). Proposal:
**`DocumentReader.Render` receives both `data` and `path`**; PDF's
implementation uses `path` when it's a real file and falls back to a temp
file from `data` otherwise (same as `cmdAttachRemote` does today) — no
interface change needed, just documented in `PDFReader`'s implementation.

**`/read-pdf` is removed** once `/read FILE.pdf [PAGES]` works via the
registry dispatch — `/read` fully replaces it (decided, see "Decisions"
below). `harvey-read-pdf.7.md` is folded into `/read`'s man page/help text
rather than kept as a separate page.

### 6. JATS reader — `JATSReader`

Per `jats_support_design.md` §1/§6: `JATSReader.Render` calls `ParseJATS`;
`opts.Selector`, if non-empty, is resolved in two passes — first against
`sec-type` (e.g. `"methods"`), then against `<title>` text (e.g. `"Materials
and Methods"`), case-insensitive — returning that section's full flattened
text. If `opts.Selector` is empty, `Render` returns the structured summary
(title/authors/identifiers/abstract/section outline/keywords/funding/
reference count) — the JATS analog of PDF's page range. No `/read-jats`
command needed; `/read FILE.nxml ["Methods"]` is the entry point.

### 7. Ambiguous extensions (e.g. `.xml`)

`.nxml` (JATS) and (per the roadmap below) likely `.fdx` (Final Draft / Open
Screenplay Format) are distinct enough to register directly. Plain `.xml` is
ambiguous — JATS, FDX-family, or generic config/data XML could all use it.
**Confirmed deferred** (decided, see "Decisions" below):
`DetectFromExtension(".xml")` stays unregistered (falls to today's fallback)
until a format that commonly ships as `.xml` is actually designed. At that
point, the existing `LanguageDetector.Detect` content-sniffing interface (root
element / namespace / DOCTYPE check) is the extension point — `cmdRead`'s
dispatch in §3 would need to call `Detect` as a tiebreaker when
`DetectFromExtension` returns an ID with no reader, or returns an
ambiguous/generic ID.

## Roadmap — future document readers

| Format | Typical extension(s) | Notes |
|---|---|---|
| PDF | `.pdf` | this design — retrofit of `pdfExtract`/`cmdReadPDF` |
| JATS | `.nxml` | `jats_support_design.md` |
| Final Draft | `.fdx` | XML-based screenplay format — verify current schema/version before design |
| Open Screenplay Format | likely `.fdx` or `.osf` (verify) | possible extension collision with Final Draft and/or generic `.xml` — needs §7 |
| Fade In | `.fadein` | verify container format (zip vs. flat XML/JSON) before design |
| OpenDocument Text | `.odt` | zip-based ODF package (`content.xml` + manifest) — first format in this list that isn't flat text/XML; `Render(data []byte, ...)` is confirmed sufficient (decided), implementation unzips `data` in-memory via `archive/zip.NewReader(bytes.NewReader(data), len(data))` |

Each future format gets its own `*_support_design.md` (per the
`jats_support_design.md` precedent) defining its `DocumentReader`
implementation against this shared interface/dispatch. Given Harvey's
existing Fountain (`.spmd`) screenplay format and `FOUNTAIN_FORMAT.md`, the
screenplay-format readers (FDX/OSF/FadeIn) may eventually be interesting as
*conversion* sources (→ Fountain) — noted for context, not designed here.

## Decisions (resolved 2026-06-12)

1. **`/read-pdf` fate** — `/read FILE [SELECTOR]` fully replaces it.
   `/read-pdf` is removed (§5); `harvey-read-pdf.7.md` is folded into `/read`'s
   docs. No JATS-specific `/read-jats` command was ever introduced (§6).
2. **ODT / archive formats** — `Render(data []byte, path string, opts)
   (string, error)` is unchanged; `archive/zip.NewReader` works directly
   against `bytes.NewReader(data)`, so no path-based variant is needed (§
   roadmap).
3. **Binary-file warning** (§3, item 4) — bundled into the `/read` dispatch
   rewrite (same `cmdRead` edit), not shipped as a separate fix.
4. **`.xml` disambiguation** (§7) — confirmed deferred until a format that
   needs it is actually designed.
5. **Selector syntax & `ReadOptions` shape** (§1, §3) — superseded by
   `document_reader_registry_plan.md`'s "Plan-level decisions" A and B,
   discovered while grounding the plan in `cmdRead`'s actual code: `/read`
   selectors use `FILE#SELECTOR` fragment syntax (not §3's trailing
   argument — ambiguous with `/read`'s multi-file form), and `ReadOptions`
   has only `Selector` (`MaxBytes` from §1 dropped as unused by both PDF and
   JATS readers). §1 and §3's prose below are left as the original v1
   proposal for context; the plan is authoritative.

`jats_support_design.md`'s own five open questions (front_matter chunk type,
MathML/TeX handling, `JATSReader` selector matching, citation/affiliation
linkage deferral, test fixtures) were resolved in the same pass — see that
doc's "Decisions" section.

## Next steps

1. ~~Resolve open questions above~~ — done (see "Decisions" above).
2. ~~Update `jats_support_design.md` §6 (and the registry-integration
   section)~~ — done; JATS registers `JATSReader` via `SetReader` (no
   `/read-jats` command/`read_jats` tool).
3. Both design docs are now reconciled and fully resolved. Next: write an
   implementation plan covering the sequencing below (e.g. as part of, or
   alongside, `jats_support_plan.md`):
   `DocumentReader` interface + `SetReader`/`GetReader`/`initReaders` (+
   tests) → `PDFReader` retrofit (+ tests; `/read-pdf` removed, behavior moves
   to `/read FILE.pdf [PAGES]`) → `/read` dispatch in `cmdRead` (+ tests,
   including the binary-file-warning fix) → `/attach`/`cmdAttachRemote`
   simplification (+ tests) → `JATSReader` (per `jats_support_design.md`,
   sequenced after its own `jats_extract.go`/`jats_chunker.go` steps).
