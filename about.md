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
version: 0.0.17
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.4


date_released: 2026-09-27
---

About this software
===================

## harvey 0.0.17

- Breaking: `harvey` and `assay` now use the workspace exit-code convention (workspace DR-0003; harvey DR-0005/DR-0006) instead of exiting only 0 or 1. Five cases that exited 0 now fail (`--continue`/`--record-file` naming a bad path, a malformed `harvey.yaml` or no reachable backend in a non-interactive session, and `assay` when every model call fails); every other case exits a more specific code than a bare 1. See `harvey/CHANGES.md`'s v0.0.17 section for the full upgrade table
- `-json`/`--json` on both binaries: a failing startup/flag-parsing error (or, for `harvey`, a failed non-interactive session) prints as `{"error","class","code"}` JSON on stderr instead of text, matching `kb`'s shape
- A non-interactive `harvey` session (piped stdin, or `--replay` without `--replay-continue`) now exits with the class of its first failed slash command or chat turn, instead of always 0; an interactive session at a terminal is unaffected. `/plan`'s errors are the first command family reclassified to give real signal here
- `/rag setup`'s embedding-model auto-pick now confirms its keyword-based guess with one live `/api/embed` call before committing to it, falling back to another candidate (or warning) when the guess is wrong — `ThoroughProbeModel`'s first caller
- Ollama models are now probed on every selection, not just when a new alias is saved, so `/model use NAME` and the startup picker no longer silently degrade to file-injection instead of tool calls; a `/model mode` override survives re-probing (previously erased)
- Removed roughly 30 stale `/ollama`/`/llamafile` references across runtime messages, help text and generated docs; both commands were replaced by `/model` months ago. `stale_commands_test.go` guards against regressions
- Bug fix: closed stdin (Ctrl-D, a closed pipe) at a yes/no prompt no longer defaults to "yes" — affected `Start Ollama now?`, `Restart MODEL?`, and three skill prompts
- Bug fix: a failed memory-embed no longer leaves an orphaned `.fountain` file; the error now says to run `ollama pull MODEL`
- Bug fix: `/model use NAME` now matches any registered model by exact name or unique prefix, not just the picker's list
- Bug fix: the "1-byte" line reader actually buffered 16 bytes (`bufio`'s minimum) and could swallow piped multi-line input after a prompt
- `knowledge` bumped to the released v0.0.14; `termlib` bumped to the released v0.0.10 (a display-corrupting bug fix for prompts wider than the terminal or containing an embedded newline)

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


