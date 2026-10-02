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
version: 0.0.18
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.4


date_released: 2026-10-02
---

About this software
===================

## harvey 0.0.18

- Breaking for scripts: every slash command now returns a classed error instead of printing a failure and carrying on, so a non-interactive `harvey` session (piped stdin, or `--replay` without `--replay-continue`) exits with the class of its first failed command, where it used to exit 0 or 70. An unknown command is 2, a missing file or workspace 66, no matches or nothing to act on 1, a path or action refused by the permission rules 77, an unreachable backend 69, a file of the wrong kind 65, a setting that cannot be saved 74. `harvey/CHANGES.md` has the table for each command family. A session at a terminal is unaffected apart from the message now reading `Error: ...`. `/loop`, `/read`, `/format`, `/kb cite` and `/rag ingest` still try every item, then return the first failure
- `/model` and `/security` with an unknown subcommand are now usage errors; `/model NAME` used to show the active model and ignore the name
- Bug fix: `/workspace init FROM_PATH` never imported anything, because the handler read its subcommand from the wrong argument; `/workspace bogus` quietly showed the status
- Bug fix: `/learn` found no hand-offs after the workspace moved them to `agents/projects/<project>/hand-off/`; it now scans each project's `hand-off/` directory and accepts Markdown hand-offs
- `/profile use`'s automatic hand-off is now written as Markdown per `HANDOFF_FORMAT.md` instead of Fountain; `recallKB` retrieves through the knowledge library's `RecallByText`
- Design briefs, plans, decision records and notes moved out of this repository into `agents/projects/harvey/` (harvey DR-0008); 21 earlier decisions from `DECISIONS.md` are now records DR-0009 to DR-0029
- `knowledge` bumped to the released v0.0.15

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


