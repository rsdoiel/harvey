%harvey(7) user manual | version 0.0.12 a6c32e5
% R. S. Doiel
% 2026-06-17

# NAME

READ-PDF — extract text from a PDF and inject it into the conversation context

# SYNOPSIS

/read-pdf FILE [PAGES]

# DESCRIPTION

/read-pdf extracts text from a PDF file using the poppler utilities (pdfinfo,
pdftotext, pdfimages) and injects the result into the conversation as a
user-role context message. The model can then reason about the content
immediately.

FILE may be an absolute path, a path relative to the workspace root, or a
home-relative path beginning with ~/. Unlike /read, /read-pdf is not
restricted to workspace files.

PAGES is an optional page range in the form FIRST-LAST (e.g. 40-55) or a
single page number (e.g. 10). When omitted, the entire document is extracted.
A hard limit of 20 pages per call applies to keep the context window
manageable; specify a range if the document is larger.

Three poppler tools are used in sequence:

  pdfinfo    — document metadata (title, author, page count, creation date)
  pdftotext  — text extraction preserving spatial layout (-layout flag)
  pdfimages  — raster-image inventory used to detect diagram-only pages

Pages that have sparse text and no raster images are flagged as likely
vector-diagram pages. Those pages cannot be extracted by any text tool; the
output will note them so you can follow up with a vision-capable model.

The injected content is ephemeral — it is added to the current conversation
and is not written to disk or stored in the RAG database. Use /rag ingest
if you want to index a PDF for retrieval.

# EXAMPLES

Read the first ten pages of a PDF:

~~~
  harvey > /read-pdf ~/docs/oberon2.pdf 1-10
~~~

Read a specific section by page range:

~~~
  harvey > /read-pdf ~/docs/oberon2.pdf 49-63
  harvey > Summarise the module system described in those pages.
~~~

Read a short PDF (≤ 20 pages) in full:

~~~
  harvey > /read-pdf project/spec.pdf
~~~

# SEE ALSO

  /read FILE...       — inject plain-text workspace files into context
  /rag ingest PATH    — index a file into the RAG store for retrieval
  /status             — check remaining context window space
  /help read
  /help rag

