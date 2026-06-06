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
version: 0.0.8
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.2


date_released: 2026-06-05
---

About this software
===================

## harvey 0.0.8

- Added profile template system: 5 developer role templates (back-end, front-end, dataset, data-scientist, technical-writer) plus blank, embedded in the binary via go:embed
- Added `/profile use [name]` command for mid-session context switching: writes a handoff summary, archives the old profile, runs the template picker, and resets conversation history
- Added `/profile` as a top-level alias for `/memory profile`
- Added embedded help guides for Ollama setup and PDF tools (`/help getting-started`, `/help pdf-tools`)
- `/status` now shows the active workspace profile name in the Memory section
- Missing-backend and missing-poppler error messages now include pointers to the relevant help guides
- Template picker replaces blank onboarding form: new users choose a role template and optionally edit it in `$EDITOR` before their first session

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


