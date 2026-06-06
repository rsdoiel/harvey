# PDF Tools

Harvey uses `pdftotext` (part of the Poppler suite) to extract text
from PDF files. This is used by `/read-pdf` and by `/rag ingest` when
ingesting PDF documents into a RAG store.

Harvey works without PDF tools installed — you simply cannot read or
ingest PDF files.

---

## Installation

**macOS:**

    brew install poppler

**Linux (Debian/Ubuntu):**

    sudo apt install poppler-utils

**Linux (Fedora/RHEL):**

    sudo dnf install poppler-utils

**Windows:** Download poppler for Windows from
https://github.com/oschwartz10612/poppler-windows/releases

Extract the archive and add the `Library/bin/` folder to your PATH.
Restart your terminal after editing PATH.

---

## Verify Installation

    pdftotext -v

You should see a version line. If you see "command not found",
the tools are not on your PATH.

---

## Troubleshooting

**Harvey says "pdftotext not found":**
Install Poppler using the instructions above, then restart Harvey.

**Text extracted looks garbled:**
The PDF may use a non-standard font encoding. Try:

    pdftotext -layout yourfile.pdf -

The `-layout` flag preserves column structure, which helps with
tables and multi-column documents.

**Scanned PDFs produce no text:**
`pdftotext` only works on PDFs with embedded text. Scanned images
require OCR (e.g. `tesseract`), which Harvey does not currently
support.

---

## See Also

    /help getting-started   — full prerequisites and first-run guide
    /help rag               — ingesting documents into Harvey's knowledge store
