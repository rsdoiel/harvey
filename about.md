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
version: 0.0.19
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.4


date_released: 2026-10-10
---

About this software
===================

## harvey 0.0.19

- Behaviour changes, see CHANGES.md upgrade notes: llama.cpp models are looked for in llama.cpp's cache (`$LLAMA_CACHE`, else `~/.cache/llama.cpp`) and `~/Models` is for llamafiles only, so set `llama_cpp.models_dir` to keep the old directory (harvey DR-0031, proposed); `skills.catalog` is off by default (set `skills: catalog: full` to restore it); the command line now beats `harvey.yaml` for `--llamafile-url`, `--llamafile-dir`, `-r`, `--record-file`, `--resume`, `--continue`, `--replay`, `--replay-output` and `--replay-continue` as well as `--ollama` and `-m`, and `HARVEY_LLAMAFILE_DIR` beats `llamafile.models_dir`
- A chat stream that ends before its final chunk is an error (`ErrStreamTruncated`, exit 74) instead of an empty reply recorded as a success
- Hailo (AI HAT+ 2) is an engine beside Ollama (harvey DR-0030): a `hailo:` section in `harvey.yaml`, card and server detected apart, `engine/model` names (a bare name on both engines asks, or is exit 2 unattended), the catalog shown as `not pulled` with `/model pull [--yes] NAME`, and a capability table so harvey stops sending `tools` or asking for embeddings where Hailo has none
- Per-model prompt limits: `/model limit`, the system prompt and history fitted to the limit, a turn over it refused before sending, and a failed turn retried on smaller prompts that teaches the limit; the model cache is keyed by engine and model
- `/memory mine` reads a session too large for the model in parts, records an empty session as `empty`, and remembers a model that failed on a session so auto-mine does not retry it; a RAG retrieval failure is reported once per session instead of silently
- `harvey completion bash|powershell [-install]` for shell completion of verbs, options and help topics
- A write offer no longer treats a command, end of input or arbitrary text as an answer; piping `/exit` used to create a file named `exit`
- Switching to a llama.cpp model mid-session keeps the `--debug` log and records the switch, as the Ollama paths already did
- `make test-models` runs live model tests and skips a missing model system with the reason; `make build` no longer runs `cmt`, `make generate` does
- `knowledge` bumped to the released v0.0.20

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


