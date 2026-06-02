---
title: harvey
abstract: |-
  Harvey is an agent REPL written in Go and designed to use Ollama server to access language models locally. It is a terminal based application.

  The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent.  Many people think of agents only in the context of very big companies. I think of my little computers and what they can accomplish with their own agent. Harvey, as a small agent for small and tiny computers, is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.
authors:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903



repository_code: https://github.com/rsdoiel/harvey
version: 0.0.7
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.2


date_released: 2026-06-02
---

About this software
===================

## harvey 0.0.7

- Added `assay` evaluation tool for running prompt corpus against Ollama models
- Added `--rag-db` and `--rag-compare` flags to `assay` for side-by-side RAG vs baseline evaluation
- Added `/attach` command for attaching images, PDFs, and text files with route-aware representation
- Added `/read-pdf` command for page-range PDF text extraction (requires poppler)
- Added `/hint` command for actionable improvement suggestions (RAG, memory, KB)
- Added `/memory` commands and memory store implementation
- Added `/pipeline` support and implementation
- Added `/recall` alias for `/memory recall` (searches all three knowledge silos)
- Unified memory, RAG and Knowledge base support
- Bug fixes catching write_file failure in model responses
- Added support for remote access for S3, HTTP and SFTP for reading and ingesting content

## Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)






Harvey is an agent REPL written in Go and designed to use Ollama server to access language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent.  Many people think of agents only in the context of very big companies. I think of my little computers and what they can accomplish with their own agent. Harvey, as a small agent for small and tiny computers, is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.

- [License](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Code Repository](https://github.com/rsdoiel/harvey)
  - [Issue Tracker](https://github.com/rsdoiel/harvey/issues)

## Programming languages

- Go >= 1.26.2




## Software Requirements

- Go >= 1.26.2


## Software Suggestions

- CMTools >= 0.0.45b
- Pandoc >= 3.9
- GNU Make >= 3.8


