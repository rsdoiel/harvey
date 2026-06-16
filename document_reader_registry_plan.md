# Document Reader Registry & `/read` Dispatch — Implementation Plan

**Status:** Plan (v1)
**Date:** 2026-06-12
**Implements:** `document_reader_registry_design.md` (all "Decisions" resolved
2026-06-12)

This plan sequences the registry/dispatch work into five steps, each ending
in a buildable, tested state. `jats_support_plan.md` depends on Step 1 (the
`DocumentReader` interface) for its own final step (`JATSReader`); the two
plans otherwise proceed independently — see "Cross-plan sequencing" at the
end of this document.

## Plan-level decisions (not covered by the design docs)

Two implementation details came up while grounding this plan in the current
code (`cmdRead` at `commands.go:2387`, `cmdReadPDF` at `commands.go:2677`,
`resolvePDFPath` at `commands.go:2789`) that the design docs didn't pin down:

### A. Selector syntax: `FILE#SELECTOR`

`/read` currently takes `FILE [FILE...]` — multiple files, no per-file
selector. A trailing-argument selector (`/read FILE.pdf 40-55`) is ambiguous
with `/read a.pdf b.pdf` (is `b.pdf` a second file or a selector for `a.pdf`?).
**Decided:** selectors use a `#` fragment, e.g. `/read paper.pdf#40-55` or
`/read paper.nxml#Methods`. This is unambiguous, works per-file even in
multi-file mode (`/read a.pdf#1-5 b.pdf#10-15`), and needs no quoting for
multi-word JATS section titles (spaces after `#` are fine). Each arg is
parsed via `path, selector, _ := strings.Cut(arg, "#")`.

### B. `ReadOptions.MaxBytes` dropped (deviation from design's draft interface)

The design doc's draft interface (`document_reader_registry_design.md` §1)
included `MaxBytes int` ("0 means use the reader's default") alongside
`Selector`. Neither reader planned here needs it: `PDFReader` caps by
*pages* (`readPDFMaxPages`, driven by `opts.Selector`'s page range), and
`JATSReader` (per `jats_support_plan.md`) has no byte-cap requirement —
`ParseJATS` operates on the whole document and `Render` returns either one
section or a bounded summary. Per the "no speculative fields" convention,
`ReadOptions` in this plan has only `Selector`. If a future reader (e.g. ODT)
needs a byte cap, add `MaxBytes` then.

### C. Path resolution for reader-backed files stays arbitrary-path

`cmdRead`'s fallback path uses `a.CheckReadPermission`/`a.Workspace.ReadFile`
(workspace-scoped). `/read-pdf`/`/attach` use `resolvePDFPath` (arbitrary
filesystem paths, `~` expansion, **no** workspace-boundary check — see its
doc comment at `commands.go:2789`). **Decided:** files whose extension has a
registered `DocumentReader` keep `resolvePDFPath`-style resolution — this
preserves `/read-pdf ~/Documents/paper.pdf`'s existing behavior once it
becomes `/read ~/Documents/paper.pdf`. Only the fallback (text/code) path
keeps `CheckReadPermission`/workspace-relative resolution. This is a
preservation of existing behavior, not a new permission relaxation.

## Step 1 — `DocumentReader` interface + registry plumbing

**New file `doc_readers.go`**, mirroring `doc_extractors.go`'s
`SetExtractor`/`GetExtractor`/`initExtractors` shape:

```go
// ReadOptions carries format-specific selection for DocumentReader.Render.
// Selector is free-form per format: a PDF page range ("40-55"), a JATS
// section sec-type or title, etc. Empty means "whole document / default view".
type ReadOptions struct {
    Selector string
}

// DocumentReader renders a structured document's raw bytes into a
// human/LLM-readable string suitable for injection as conversation context.
// path is used only for provenance text and, for formats that shell out to
// external tools (e.g. PDF/poppler), as a real filesystem path when one
// exists — Render must fall back to writing data to a temp file when path is
// not a real file (e.g. content downloaded via /read on a remote URI).
type DocumentReader interface {
    Render(data []byte, path string, opts ReadOptions) (string, error)
    Language() string // language ID this reader is registered under
}

func (r *LanguageRegistry) SetReader(id string, dr DocumentReader) {
    r.readers[id] = dr
}

func (r *LanguageRegistry) GetReader(id string) DocumentReader {
    return r.readers[id]
}

// initReaders wires DocumentReaders for registered formats. Called last in
// init() (language_registry.go) so PDF's own RegisterLanguage call here and
// JATS's RegisterLanguage call (in initChunkers, jats_support_plan.md step 2)
// have both already run.
func initReaders(r *LanguageRegistry) {
    // Step 2 of this plan adds: r.RegisterLanguage(langPDF, nil, nil, nil, nil, nil)
    //                            r.SetReader("pdf", NewPDFReader())
    // jats_support_plan.md step 4 adds: r.SetReader("jats", NewJATSReader())
}
```

**Edits to `language_registry.go`:**
- Add `readers map[string]DocumentReader` to the `LanguageRegistry` struct
  (286-299), alongside `extractors`.
- Initialize `readers: make(map[string]DocumentReader)` in
  `NewLanguageRegistry` (310-321).
- Add `initReaders(globalRegistry)` as the **last** call in `init()` (789-796),
  after `initFormatters`.

**Tests — `doc_readers_test.go`:**
- `TestSetReader_GetReader` — register a stub `DocumentReader` (returns a
  fixed string from `Render`), verify `GetReader` returns it and
  `Render`/`Language` work through the interface.
- `TestGetReader_unregistered` — `GetReader("nonexistent")` returns `nil`.
- `TestNewLanguageRegistry_readersInitialized` — `r.readers` is non-nil and
  empty on a fresh registry.

## Step 2 — `PDFReader` (retrofit of `cmdReadPDF`/`pdfExtract`)

**New file `pdf_reader.go`**, extracting the body of `cmdReadPDF`
(`commands.go:2677-2787`) into a reusable `Render`:

```go
var langPDF = LanguageInfo{ID: "pdf", Name: "PDF", Extensions: []string{".pdf"}}

type PDFReader struct{}

func NewPDFReader() *PDFReader { return &PDFReader{} }

func (p *PDFReader) Language() string { return "pdf" }

// Render extracts text from a PDF via pdfExtract and formats it exactly as
// cmdReadPDF did: Title/Author/Pages/Date header, diagram-page note, then
// extracted text. opts.Selector carries the PAGES range ("40-55"), passed
// straight to pdfExtract/parsePDFPageRange. If path is not a real file (e.g.
// data came from a remote /read), data is written to a temp *.pdf file first
// (same pattern cmdAttachRemote uses today at commands.go:2911-2926).
func (p *PDFReader) Render(data []byte, path string, opts ReadOptions) (string, error)
```

`readPDFMaxPages = 20` and the page-cap enforcement logic (2701-2727) move
into `Render` unchanged. `checkPopplerTools`/`parsePDFPageRange`/
`parsePDFInfo`/`pdfExtract` (`pdf_extract.go`) are reused as-is — no changes
to that file.

`initReaders` (Step 1) gains:
```go
r.RegisterLanguage(langPDF, nil, nil, nil, nil, nil) // registers ".pdf" -> "pdf"
r.SetReader("pdf", NewPDFReader())
```

**Tests — `pdf_reader_test.go`** (use `skipIfNoPopplerTools`, reuse
`pdf_extract_test.go`'s fixture-generation approach):
- `TestPDFReader_Render_wholeDocument` — `opts.Selector=""` on a small fixture
  (≤ `readPDFMaxPages` pages); output contains `Title:`/`Pages:` header and
  extracted text, matches `cmdReadPDF`'s format byte-for-byte on the same
  fixture (regression check).
- `TestPDFReader_Render_pageRange` — `opts.Selector="1-1"`.
- `TestPDFReader_Render_pageCapExceeded` — fixture with more than
  `readPDFMaxPages` pages and `opts.Selector=""` returns the same
  "specify a range" guidance `cmdReadPDF` prints today.
- `TestPDFReader_Render_dataOnly` — `path` is a non-existent path (simulating
  a remote download); `Render` still succeeds via the temp-file fallback.

## Step 3 — `/read` dispatch rewrite (`cmdRead`, `commands.go:2387`)

Per-argument parsing: `path, selector, _ := strings.Cut(arg, "#")` (decision
A above).

Factor a shared helper used by both the local-workspace and remote-URI
branches of `cmdRead`:

```go
// readRenderFile renders one file's bytes for /read injection.
// If a DocumentReader is registered for path's extension, it returns
// reader.Render(data, path, ReadOptions{Selector: selector}) directly (no
// fence — the reader supplies its own provenance header).
// Otherwise: if isTextContent(data), wraps data in a fenced ```path block
// (today's behavior, selector ignored with no warning — plain files don't
// support selectors); if data is binary, returns an error so cmdRead can
// print "binary file, no reader registered for %q" instead of injecting
// raw bytes (fixes the latent bug flagged in document_reader_registry_design.md).
func readRenderFile(data []byte, path, selector string) (string, error)
```

`cmdRead`'s per-argument loop (both the remote-URI branch at 2407-2428 and
the workspace branch at 2430-2456) becomes:
```go
rendered, err := readRenderFile(data, path, selector)
if err != nil {
    fmt.Fprintf(out, "  ✗ %s: %v\n", path, err)
    continue
}
fmt.Fprintf(out, "  ✓ %s (%d bytes)\n", path, len(data))
sb.WriteString(rendered)
ok++
```

**Path resolution per decision B**: before calling `readRenderFile`, check
`globalRegistry.DetectFromExtension(filepath.Ext(path))` →
`globalRegistry.GetReader(langID)`. If non-nil, resolve `path` via
`resolvePDFPath` (arbitrary path, no workspace check) and read with
`os.ReadFile`. If nil, keep today's `CheckReadPermission` +
`a.Workspace.ReadFile` (workspace-scoped). Remote-URI args (`parseURIScheme
(path) != ""`) are unaffected by this check — they already bypass workspace
permissions via `NewRemoteReader`.

Update the `"read"` command table entry (`commands.go:213-217`):
```go
"read": {
    Usage:       "/read FILE[#SELECTOR] [FILE[#SELECTOR]...]",
    Description: "Inject file(s) into context; PDF/JATS/etc. render via their reader, others as fenced text",
    Handler:     cmdRead,
},
```

**Tests — extend `commands_test.go`** (or new `cmd_read_test.go`):
- `TestCmdRead_textFile` — `/read foo.go` unchanged (fenced block).
- `TestCmdRead_multiFile` — `/read foo.go bar.go` unchanged.
- `TestCmdRead_pdfWholeDoc` — `/read foo.pdf` dispatches to `PDFReader`, no
  fence, contains `Title:`.
- `TestCmdRead_pdfSelector` — `/read foo.pdf#1-1` passes `Selector: "1-1"`.
- `TestCmdRead_binaryNoReader` — `/read foo.bin` (non-text, no reader)
  prints `binary file, no reader registered for ".bin"`, not injected.
- `TestCmdRead_remotePDF` — `/read https://example.com/paper.pdf` (mock
  `RemoteReader`) dispatches to `PDFReader` via the temp-file fallback in
  `Render`.

## Step 4 — `/attach`/`cmdAttachRemote` simplification

Replace `cmdAttach`'s hardcoded `.pdf` branch (`commands.go:2873-2875`):
```go
if langID, ok := globalRegistry.DetectFromExtension(filepath.Ext(absPath)); ok {
    if reader := globalRegistry.GetReader(langID); reader != nil {
        data, err := os.ReadFile(absPath)
        if err != nil {
            fmt.Fprintf(out, "  ✗ %s: %v\n", filePath, err)
            return nil
        }
        rendered, err := reader.Render(data, absPath, ReadOptions{})
        if err != nil {
            fmt.Fprintf(out, "  ✗ %s: %v\n", filePath, err)
            return nil
        }
        a.AddMessage("user", rendered)
        fmt.Fprintf(out, "  ✓ %s attached (%s)\n", filePath, langID)
        return nil
    }
}
```
`cmdAttachRemote`'s analogous `.pdf` branch (`commands.go:2911-2926`) gets the
same replacement, using `data` directly — `PDFReader.Render`'s temp-file
fallback (Step 2) handles the non-path case, so the manual
`os.CreateTemp`/`f.Write`/`defer os.Remove` block (2912-2924) is deleted.

Update `cmdAttach`'s usage text (`commands.go:2841-2844`): replace the
PDF-specific line with "Documents with a registered reader (PDF, JATS, ...):
rendered via the same pipeline as /read."

**Tests:**
- `TestCmdAttach_pdf` — regression: `/attach foo.pdf` still produces the same
  injected content as before (now via `PDFReader.Render`).
- `TestCmdAttach_remotePdf` — regression for `cmdAttachRemote`'s `.pdf` path.
- `TestCmdAttach_image`, `TestCmdAttach_text` — unaffected (no reader for
  `.png`/`.txt`, fall through to `attachImage`/`attachText` unchanged).

## Step 5 — Remove `/read-pdf`

Per decision 6 (`/read FILE.pdf[#PAGES]` fully replaces it):

- Delete the `"read-pdf"` command-table entry (`commands.go:223-227`) and the
  `case "read-pdf", "readpdf":` alias (`commands.go:512-516`).
- Delete `cmdReadPDF` (`commands.go:2677-2787`) — its logic now lives in
  `PDFReader.Render` (Step 2). By this point `cmdAttach`/`cmdAttachRemote`
  (Step 4) no longer call it, so it has zero remaining callers.
- `readPDFMaxPages` and `resolvePDFPath` move to (or stay reachable from)
  `pdf_reader.go`/`commands.go` as needed by `PDFReader.Render` and Step 3's
  path resolution respectively.
- Delete `harvey-read-pdf.7.md` (its `dist/` copy regenerates via `make`;
  `MAN_PAGES_7` in the Makefile auto-discovers `*.7.md` via `ls`, no Makefile
  edit needed). Fold its content — PDF extraction details, page-range syntax,
  poppler requirement — into `harvey-read.7.md`, documenting the new
  `FILE[#SELECTOR]` syntax and the PDF/JATS dispatch.
- Update `/help` output (generated from the command table) — no separate
  edit needed beyond Step 3's table update.

**Tests:**
- `TestCmdDispatch_readPdfRemoved` — `/read-pdf foo.pdf` is no longer
  recognized (returns "unknown command" per the existing dispatch error
  path).
- Remove/update any existing tests that call `cmdReadPDF` directly or assert
  on `/read-pdf` dispatch.

## Cross-plan sequencing

`jats_support_plan.md` step 4 (`JATSReader`) needs only this plan's **Step
1** (`DocumentReader` interface, `SetReader`/`GetReader`, `initReaders`) to
exist — it does not depend on Steps 2-5. Steps 2-5 here and
`jats_support_plan.md` steps 1-3 (jats_extract/jats_chunker/RAG ingest) are
otherwise independent and can proceed in any order once Step 1 lands.
Recommended order, since it keeps `/read`/`/attach` working end-to-end at
every step:

1. **This plan, Step 1** (interface + plumbing, no behavior change)
2. **This plan, Steps 2-5** (PDF retrofit through `/read-pdf` removal) —
   *or* in parallel, **`jats_support_plan.md` steps 1-3** (extract/chunker/RAG
   ingest — no `DocumentReader` dependency)
3. **`jats_support_plan.md` step 4** (`JATSReader`, registered in
   `initReaders` alongside PDF) — needs Step 1 here; best tested once this
   plan's Step 3 (`/read` dispatch) also exists, so `/read paper.nxml` and
   `/read paper.nxml#Methods` can be exercised end-to-end.
