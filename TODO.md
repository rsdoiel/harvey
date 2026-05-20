
# Action Items

## Bugs

## Next Steps (upcoming features, v0.0.5)

- [X] PDF extraction — shared internal `pdfExtract(file, pages)` function used by both commands
      below. Uses three poppler utilities in sequence:
      1. `pdfinfo` — metadata (title, author, page count, creation date)
      2. `pdftotext -layout` — text extraction preserving spatial structure (prose, code, tables)
      3. `pdfimages -list` — detect pages with raster images; flag sparse/empty pages that are
         likely vector diagrams so output isn't silently incomplete
      Vector diagrams (path-command graphics) cannot be extracted by any text tool — those pages
      should be flagged for follow-up with a vision-capable route. Poppler (pdftotext, pdfinfo,
      pdfimages) is the chosen backend; LibreOffice and Pandoc are not suitable for PDF input.

- [X] `/read_pdf FILE [PAGES]` slash command — context injection path. Calls `pdfExtract`
      with an optional page range (e.g. `40-55`). Enforces a page cap (~20 pages) to stay
      within context window limits. Output is injected into the conversation as a user message
      so the model can reason about the content immediately. Ephemeral — not stored anywhere.
      Example: `/read_pdf ~/docs/oberon2.pdf 49-63`

- [X] `/rag ingest FILE.pdf` subcommand — RAG ingestion path. Calls `pdfExtract` on the full
      document (no page cap), chunks the output by paragraph/section, attaches per-chunk metadata
      (source file, page number, document title from pdfinfo), embeds, and stores in the active
      RAG store. Pages flagged as diagram-heavy by pdfimages are stored with an incomplete-content
      marker so retrieval results can surface the caveat. Companion to `/read_pdf`; both share
      `pdfExtract` but serve different outputs (chat context vs. RAG database).

- [X] Attached file support — `/attach FILE` command that includes a file as multimodal content
      in the next chat turn. For cloud routes with CompletionPDF/CompletionImage support
      (Anthropic, Mistral, Gemini), sends the raw file bytes as a ContentPart so the model
      processes it natively. For local/text-only models, falls back to text extraction.
      See conversation notes for design details.


## Someday, maybe ideas

- [ ] Add the ability to interact with S3 protocol for remote storage, this will be helpful in library and archival projects if Harvey is helping with processing
