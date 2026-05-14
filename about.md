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
version: 0.0.3b
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.2


date_released: 2026-05-12
---

About this software
===================

## harvey 0.0.3b

Working proof of concept.

- Fixed RAG startup error (SQLITE_CANTOPEN) when agents/rag/ directory does not exist; NewRagStore now creates parent directories automatically
- Fixed /rag on and /rag off not persisting the enabled state to harvey.yaml
- Fixed session model not being restored on resume when the prior session had no chat turns; model is now written to the Fountain title page at recording start
- Added context window usage to the post-response stat line: shows current/max tokens and percentage (e.g. 1840/32768 ctx (5%)); /status also shows the percentage; limit is sourced from --context flag or model cache automatically
- Added TAGGED column to /ollama list showing whether each model reliably emits path-tagged code blocks (used by auto-execute)
- Startup model picker now displays the same capability table as /ollama list with numbered rows for selection
- Added /rename NAME command to rename the active session file without ending the session
- Added /file-tree [PATH] command to display a tree-style workspace listing
- Added /model alias list|set|remove — manage aliases interactively; set/remove save to harvey.yaml immediately
- Improved and tightened up tool support
- Added /read-dir to read in an entire directory contents into the current context
- Improved keyboard handling
- Added support for skill sets, `/skill-set`, which can be loaded and unloaded together

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


