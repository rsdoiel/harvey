# Getting Started with Harvey

Harvey is a terminal coding agent that runs language models locally
via Ollama. Two prerequisites must be installed before Harvey can
work: **Ollama** and (optionally) **PDF tools**.

---

## Step 1 — Install Ollama

Ollama runs language models on your machine. Harvey talks to it to
generate responses.

**Download:** https://ollama.com/download

Install for your platform, then pull a starter model:

    ollama pull granite3.3:2b

That model is small (2 GB) and works well on most hardware. For
coding-heavy work, try:

    ollama pull qwen2.5-coder:7b

Verify Ollama is running:

    ollama list

If Harvey says it cannot connect to Ollama, start the service:

- **macOS / Linux:** `ollama serve` (or it may already run as a daemon)
- **Windows:** start the Ollama app from the Start menu

---

## Step 2 — Install PDF Tools (optional)

Harvey uses `pdftotext` to extract text from PDF files for the RAG
store. Harvey works fine without it — you just cannot read PDFs.

**macOS:**

    brew install poppler

**Linux (Debian/Ubuntu):**

    sudo apt install poppler-utils

**Linux (Fedora/RHEL):**

    sudo dnf install poppler-utils

**Windows:** Download poppler for Windows from
https://github.com/oschwartz10612/poppler-windows/releases
and add the `bin/` folder to your PATH.

---

## Step 3 — Start Harvey

    harvey

On first run in a new workspace Harvey will ask you to set up a
profile. Pick a starting template that fits your role.

---

## More Help

    /help ollama       — Harvey's Ollama command reference
    /help pdf-tools    — PDF tools detail and troubleshooting
    /help memory       — how Harvey remembers things across sessions
    /help rag          — how to ingest documents for retrieval
