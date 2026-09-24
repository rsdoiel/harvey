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
version: 0.0.16
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.4


date_released: 2026-09-15
---

About this software
===================

## harvey 0.0.16

- Full agentic-memory tool suite: `retrieve_memory`, `add_memory`, `update_memory`, `delete_memory`, `filter_context`, `summary_context` builtin tools, plus proactive STM-budget warnings
- Unified `/model` command: `/llamafile` and `/llamacpp` merged into one backend-agnostic facade; `@mention` switches the active model while preserving history; unregistered `.llamafile`/`.gguf` models are now found via disk scan
- `knowledge` module extraction: knowledge-base code split into its own module (`github.com/rsdoiel/knowledge`), now consumed at a v0.0.13 pre-release (`fd588ef`); `recallKB` tries concept-tag matching (`MatchConceptNames`/`RecallByConceptNames`) before falling back to substring search, and now surfaces decision records and reviewed document summaries, not just observations
- Cross-machine `knowledge.db` sync: UUID-based merge tool (`bin/kbmerge`), legacy `experiments`→`projects` migration
- Retraction-checking for cited sources in the knowledge base
- `/read-chunks`: explicit chunked document analysis, independent of context-overflow triggers
- Knowledge learning mode: `/kb learn ingest|draft|review|concepts` turns recorded sessions and hand-off notes into searchable, human-reviewed knowledge. A model drafts summaries (`@model`, else `learn_model` in `agents/harvey.yaml`, else the active model); nothing is trusted or searchable until you accept it; `$EDITOR` edits are recorded as human. `/kb learn concepts` suggests new concepts, previews each affected document as a diff of changed lines, and writes only after a yes, through the permissions table. Fountain sessions and hand-offs are never rewritten
- Bug fix: chunk-prompt guard now triggers correctly on models with an unknown context limit (previously never fired) — fixed and live-verified against Gemma-4-E4B
- Bug fix: Llamafile `GPULayers` now defaults to 0 (CPU-only) instead of 99, fixing an apparent multi-hour "hang" on Raspberry Pi hardware with no GPU backend
- Bug fix: `pickBackend` startup picker now lists `.gguf`/llama.cpp models, not just llamafiles and Ollama
- Bug fix: file-write confirmations no longer treat end of input (Ctrl-D, a closed pipe) as "yes", and tagged code-block writes now honour the `permissions:` table (previously only the untagged fallback did)
- Bug fix: `/kb search` no longer fails on hyphenated terms such as `map-reduce` (picked up from `knowledge`; the release is built against a v0.0.13 pre-release commit, `fd588ef`)
- `kb` and `man` added to the safe-mode default command allowlist
- Removed a stale local `replace github.com/rsdoiel/termlib => ../termlib` that was silently masking a broken build for anyone without a local `../termlib` checkout; now consumes the tagged `v0.0.9`

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


