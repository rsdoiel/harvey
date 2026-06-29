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
version: 0.0.15a
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.4


date_released: 2026-06-26
---

About this software
===================

## harvey 0.0.15a

- `/model mode [MODEL] {structured|prose|inject|none}`: set or display the tool-execution strategy for a model; persisted in the model cache and survives re-probes
- File-reference injection: when a model does not reliably call tools, Harvey pre-injects the content of workspace files mentioned in the prompt as `### File:` blocks
- Cannot-read retry: if a model responds indicating it cannot access a file, Harvey retries once with file content pre-loaded; retry uses RunToolLoop when in structured mode
- `ModelCapability.ToolMode` field and `ToolMode*` constants added; `tool_mode` column added to the model cache database with automatic migration
- Bug fix: `FastProbeModel` no longer overwrites a user-set tool mode on re-probe
- Bug fix: option-2 retry now clears stale `toolCallRecords` before history rollback, preventing inconsistent session transcripts
- Bug fix: `noToolCalls` computed correctly after option-2 retry using pre-rollback `hadToolCalls` flag
- Bug fix: option-2 retry uses `RunToolLoop` (not bare `Client.Chat`) when in structured-tools mode
- Bug fix: `cantReadPhrases` entry tightened from `"please provide the file"` to `"please provide the file content"` to avoid spurious retries

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


