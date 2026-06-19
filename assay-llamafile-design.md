# Harvey Assay — Llamafile Support & Output Location — Design

**Status (2026-06-18):** Design settled. See
[assay-llamafile-plan.md](assay-llamafile-plan.md) for the implementation plan.

This document covers two related improvements to `bin/assay`:
1. Support for evaluating Llamafile models alongside Ollama models.
2. Moving evaluation output to a workspace-level directory.

---

## Background

`bin/assay` is Harvey's LLM evaluation harness. It reads a corpus of prompts
from `agents/assay/corpus.yaml`, sends each prompt to a model backend, scores
the response against `contains:` checks, and writes a report to `testout/`.

Currently, only the Ollama backend is supported. Harvey also supports
Llamafile — a self-contained, runnable binary that serves a llamafile.cpp
model on a local HTTP port (the same OpenAI-compatible API as Ollama). Users
who want to compare a Llamafile model against an Ollama model must manually
start the Llamafile process, find the port, and pass `--ollama-url
http://localhost:PORT` to assay — a fragile workaround that isn't documented.

---

## 1. Llamafile Backend Support

### Goals

- `bin/assay --llamafile PATH` starts the llamafile binary automatically,
  runs the full evaluation suite against it, and terminates the process on
  exit.
- The report header identifies the model as a llamafile with its binary path.
- RAG evaluation (`--rag-db`, `--rag-compare`) continues to work; embeddings
  still use Ollama (the only embedding backend Harvey has today).
- `--rag-db` + `--llamafile` fails fast with a clear error if the RAG store's
  recorded embedding model is not reachable via Ollama.

### Flag interface

```
bin/assay --llamafile PATH [--model NAME] [other flags]
```

`PATH` — absolute or relative path to a llamafile binary. The name is
derived from the binary filename using `llamafileModelName` (already in the
package). The optional `--model NAME` overrides the derived name in the
report.

`--ollama-url` is silently ignored when `--llamafile` is provided, because
the llamafile process binds its own port.

### Process lifecycle

1. At startup (after flag parsing, before corpus loading), assay calls
   `startLlamafile(path, port)` — the same function used by Harvey's
   `/llamafile add` command (`llamafile_service.go`).
2. The port is selected by calling `findFreePort()` (also in
   `llamafile_service.go`), distinct from Harvey's default llamafile port to
   allow assay and an interactive Harvey session to coexist.
3. On process start, assay waits up to 30 seconds for the llamafile HTTP
   server to respond (same health-check loop used in the interactive path).
4. All evaluation prompts are sent to `http://localhost:PORT/v1/chat/completions`
   using the same `anyllm_client.go` client already used by the Ollama path.
5. On exit (normal or panic), a `defer` stops the llamafile process. If the
   process has already exited, the stop is a no-op.

### Port selection

`findFreePort()` asks the OS for an available port by opening and immediately
closing a listener on `:0`. This is already implemented in
`llamafile_service.go`. No new port logic is needed.

### Embedding with Llamafile

Llamafile does not expose an embedding endpoint. When `--rag-db` is also
given and `--llamafile` is the backend, embeddings go to Ollama at the
default URL (`--ollama-url` or `http://localhost:11434`). If Ollama is not
running, assay prints:

```
  RAG evaluation requires Ollama for embeddings (llamafile has no
  embedding endpoint). Start Ollama or use --ollama-url to specify
  a running instance.
```

and exits with a non-zero status.

---

## 2. Workspace-Level Output Directory

### Problem

Assay writes to `testout/` in the `harvey/` source repository. This is
gitignored, but models reading the file tree (via `/file-tree` or `read_dir`)
see the directory and misinterpret stale evaluation results as current test
failures.

### Solution

The default output directory becomes `$WORKSPACE/assay-results/assay-<timestamp>/`
where `$WORKSPACE` is the Harvey workspace root (the directory containing
`agents/harvey.yaml`).

```
$WORKSPACE/
  agents/
    harvey.yaml
  assay-results/
    assay-20260618-143022/
      report.md
      results.json
```

The `assay-results/` directory is not inside the harvey source tree, so it
does not appear in the harvey file tree. It is at the workspace root, next
to `agents/`, making it easy to find.

### Workspace discovery

`findWorkspaceRoot(start string) string` walks up the directory tree from
`start` (default: cwd) looking for a directory containing `agents/harvey.yaml`.
Returns the empty string if none is found.

This is a standalone function in `cmd/assay/main.go`. It does not import the
Harvey package's `NewWorkspace` to keep the assay binary's dependency surface
small.

### Fallback

If no workspace is found, the default output is `assay-results/assay-<timestamp>/`
in the current working directory. The `--help` text documents this fallback.

### Override

`--output PATH` fully overrides the default directory. Users with existing
scripts that pass `--output testout/myrun` are unaffected.

---

## Report Header Changes

The report header is extended to include backend information:

```markdown
# Assay Report — 2026-06-18 14:30:22

| Field       | Value                              |
|-------------|-----------------------------------|
| Backend     | Llamafile                         |
| Model       | Llama-3.2-1B                      |
| Binary      | /home/user/Models/Llama-3.2-1B.llamafile |
| Ollama URL  | (not used)                        |
| RAG store   | go-source (1,204 chunks)          |
| Output dir  | /workspace/assay-results/assay-20260618-143022 |
```

For Ollama runs, "Backend" shows "Ollama" and "Binary" is omitted.

---

## Interaction Between the Two Changes

The output directory change and the Llamafile backend change are independent.
Either can be shipped without the other. The plan treats them as separate
phases for this reason.

---

## Out of Scope

- **Llamafile embedding support** — not feasible until llamafile exposes an
  embedding endpoint. Track as a future enhancement.
- **Side-by-side Ollama vs. Llamafile comparison** — `--rag-compare` already
  shows base vs. RAG delta; a backend-comparison mode would require running
  the corpus twice with different backends and diffing results. Deferred.
- **Corpus management CLI** — adding/editing corpus entries via CLI flags.
  Corpus is currently a hand-edited YAML file, which is sufficient.
