---
title: harvey
abstract: |-
  Harvey is an agent REPL written in Go and designed to use Llamafile models or Ollama server to access language models locally. It is a terminal based application.

  The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and my personal language model agent.  Many people think of agents only in the context of very big companies. I think small models running on small or tiny computers are an opportunity. Harvey, is a small agent for small and tiny computers and is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.
authors:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903



repository_code: https://github.com/rsdoiel/harvey
version: 0.0.14
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.4


date_released: 2026-06-21
---

About this software
===================

## harvey 0.0.14

- Llamafile is now the primary language model backend: startup sequence shows registered llamafiles first, auto-selects on preferred model match, then falls back to Ollama
- Explicit connection feedback: "Connecting to NAME (llamafile)… ✓" shown in terminal on backend selection
- Stale server adoption: Harvey detects a running llamafile server it did not start, probes its model via `/v1/models`, warns on mismatch, and adopts it rather than refusing to start
- `/llamafile show [NAME]`: displays name, path, file size, and configured context length for a registered model
- `/rag show [NAME]`: displays store name, database path, embedding model, chunk count, and model map
- Remote RAG ingest extended: `sftp://`, `scp://`, `http://`, and `https://` URIs now supported alongside `s3://`
- `/read` auto-detects `.pdf` files and extracts text via poppler (pdfinfo + pdftotext + pdfimages), consistent with the `read_file` built-in tool
- `/status` backend tag and token-count estimate now work for llamafile (was Ollama-only)
- Pipeline context-utilization display now works for llamafile via character estimate (was Ollama-only)
- Context utilization hint `[ctx: N%]` added to spinner label when estimated token usage reaches 50% of the model's context window
- Routing feedback in spinner: shows `@route · model` during routed turns when routing is active
- Model provenance recorded in Fountain session header: `Model:` field now stores `NAME (backend)` (e.g., `QWEN-CODING (llamafile)`) for session replay and audit
- Health check on `--resume`/`--continue`: session model is extracted before backend selection; a mismatch warning is shown when the resumed model differs from the active backend
- `@mention` dispatch: routing is tried first when routing is enabled; falls back to local model switch via `attemptModelSwitch`; case-insensitive for both llamafile names and model aliases
- Help system: all 41 help constants documented and reordered into 11 logical groups; `ModelHelpText` dispatch bug fixed (topic was unreachable); `harvey-model.7.md` man page added
- Documentation rewritten with llamafile-first framing and natural-language-programming / scholarly-work positioning: `overview.md`, `getting_started.md`, `user_manual.md`, `CONFIGURATION.md`

## Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)






Harvey is an agent REPL written in Go and designed to use Llamafile models or Ollama server to access language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and my personal language model agent.  Many people think of agents only in the context of very big companies. I think small models running on small or tiny computers are an opportunity. Harvey, is a small agent for small and tiny computers and is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.

- [License](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Code Repository](https://github.com/rsdoiel/harvey)
  - [Issue Tracker](https://github.com/rsdoiel/harvey/issues)

## Programming languages

- Go >= 1.26.4




## Software Requirements

- Llamafile v0.10 models or Ollama plus Ollama models


## Software Suggestions

- Go >= 1.26.4
- CMTools >= 0.0.45
- Pandoc >= 3.9
- GNU Make >= 3.8


