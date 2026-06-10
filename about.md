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
version: 0.0.10
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.3


date_released: 2026-06-09
---

About this software
===================

## harvey 0.0.10

- Added multi-language code-aware chunking for RAG ingestion (C, C++, Pascal, Oberon, Lisp, Basic)
- Added documentation extraction: comment and docstring association with symbols for C, C++, Pascal, Oberon, Lisp, Basic
- Added ANSI syntax highlighting of code blocks in LLM responses (13 languages: C, C++, Pascal, Oberon, Lisp, Basic, Go, Python, JavaScript, TypeScript, Rust, Shell, SQL); configurable via `syntax_highlight` in harvey.yaml
- Added automatic code formatting on `write_file`: built-in formatters for Pascal, Oberon, Basic; external pipe-mode formatters for Go (gofmt), C/C++ (clang-format), Python (black), Rust (rustfmt), JavaScript/TypeScript (prettier); configurable via `auto_format` in harvey.yaml
- Added `/format FILE [FILE...]` command to manually format workspace source files in-place

## Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)






Harvey is an agent REPL written in Go and designed to use Ollama server to access language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent.  Many people think of agents only in the context of very big companies. I think of my little computers and what they can accomplish with their own agent. Harvey, as a small agent for small and tiny computers, is a play on a mythic creature. Harvey is a Púca, a software Púca. Harvey can be fun for those who take time for it. It runs on a little computers. Have an adventure and some fun with Harvey.

- [License](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Code Repository](https://github.com/rsdoiel/harvey)
  - [Issue Tracker](https://github.com/rsdoiel/harvey/issues)

## Programming languages

- Go >= 1.26.3




## Software Requirements

- Go >= 1.26.3


## Software Suggestions

- CMTools >= 0.0.45b
- Pandoc >= 3.9
- GNU Make >= 3.8


