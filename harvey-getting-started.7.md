# Getting Started with Harvey

Harvey is a terminal coding agent that runs language models locally.
No cloud account or API key required — models run on your machine.

---

## Step 1 — Get a model

### Option A: Llamafile (recommended — simplest setup)

A llamafile is a single self-contained file that runs a language model with
its own built-in server. No separate install required.

**Download a model** from:

    https://huggingface.co/Mozilla/llamafile-models

Recommended choices:

| File | Size | Best for |
|---|---|---|
| Qwen2.5-Coder-7B-Q5_K_S.llamafile | 5.1 GB | Code generation on most hardware |
| Qwen2.5-Coder-1.5B-Q4_K_M.llamafile | 1.4 GB | Low-VRAM or CPU-only machines |
| Phi-3.5-mini-instruct-Q4_K_M.llamafile | 2.3 GB | General tasks, compact |

**Place the file in `~/Models/`** (Harvey scans this directory automatically).

Make the file executable (Linux / macOS):

    chmod +x ~/Models/Qwen2.5-Coder-7B-Q5_K_S.llamafile

That's it. Skip to Step 2.

### Option B: Ollama (advanced — persistent server, wider model library)

Install Ollama from https://ollama.com/download, then pull a starter model:

    ollama pull qwen2.5-coder:7b

Verify it works:

    ollama list

See `/help ollama` for Harvey's Ollama command reference.

---

## Step 2 — Install PDF tools (optional)

Harvey uses `pdftotext` to extract text from PDF files for the knowledge
store. Harvey works fine without it — you just cannot read PDFs directly.

**macOS:**

    brew install poppler

**Linux (Debian/Ubuntu):**

    sudo apt install poppler-utils

**Linux (Fedora/RHEL):**

    sudo dnf install poppler-utils

**Windows:** Download from https://github.com/oschwartz10612/poppler-windows/releases
and add the `bin/` directory to your PATH.

---

## Step 3 — Start Harvey

Change to your project directory and run:

    cd ~/myproject
    harvey

On first start Harvey will:
1. Scan `~/Models/` for llamafile binaries and offer to connect
2. Fall back to Ollama if no llamafile is found
3. Ask you to set up a workspace profile (pick a template)
4. Drop you into the REPL: `harvey >`

If no model is found at all, Harvey prints download guidance and lets you
enter a path to a llamafile before starting.

---

## Step 4 — First commands

    /help             — list all slash commands
    /status           — show backend, context, and recording status
    /hint             — suggest next steps for knowledge and memory
    /llamafile list   — show registered local models
    /model list       — show all models across all backends

### Switch models mid-session

Prefix any prompt with `@model-name` to switch model inline:

    @phi-mini summarise this function in one sentence

The conversation history is preserved across the switch.

### Common workflows

    /read main.go                  — load a file into context
    /search "TODO"                 — search the workspace
    /git diff HEAD                 — see recent changes
    /plan refactor the auth layer  — generate a step-by-step plan
    /loop 30s check test results   — repeat a prompt on an interval

---

## More help

    /help llamafile      — llamafile backend commands
    /help ollama         — Ollama backend commands
    /help model          — unified model management and @mention switching
    /help memory         — how Harvey remembers things across sessions
    /help rag            — ingesting documents for retrieval
    /help plan           — multi-step task planning
    /help loop           — repeating prompts on an interval
    /help pdf-tools      — PDF tools detail and troubleshooting

# SEE ALSO

  harvey-llamafile(7), harvey-ollama(7), harvey-model-alias(7),
  harvey-memory(7), harvey-rag(7), harvey-plan(7), harvey-loop(7),
  harvey-hint(7)
