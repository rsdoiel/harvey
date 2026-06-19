---
title: harvey
abstract: |-
  Harvey is an agent REPL written in Go and designed to use Llamafile models or Ollama server to access language models locally. It is a terminal based application.

  The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent.  Many people think of agents only in the context of very big companies. I think of my little computers and what they can accomplish with their own agent. Harvey, as a small agent for small and tiny computers, is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.
authors:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903



repository_code: https://github.com/rsdoiel/harvey
version: 0.0.13
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.4


date_released: 2026-06-19
---

About this software
===================

## harvey 0.0.13

- `/profile` command: top-level alias for `/memory profile <list|show|edit|use|rename>`
- `--resume` flag: resumes the most recent session in `agents/sessions/` automatically at startup
- Spinner live status: tool calls now show a transient status line ("Calling tool…") while waiting for results
- `assay --llamafile PATH`: evaluate a llamafile binary directly; assay starts/stops the process and derives the model name from the binary path
- Added web-developer workspace profile template
- `HARVEY.md`: documented native file-reading for PDF and image files so models call `read_file` directly
- S3 remote: improved not-found detection for missing keys and buckets; path-style access fixed for non-AWS endpoints
- `MostRecentSession` helper in `sessions_files.go` for reliable `--resume` behaviour
- Llamafile: `scanLlamafileModels` now discovers binaries by directory scan

## Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)






Harvey is an agent REPL written in Go and designed to use Llamafile models or Ollama server to access language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent.  Many people think of agents only in the context of very big companies. I think of my little computers and what they can accomplish with their own agent. Harvey, as a small agent for small and tiny computers, is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.

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


