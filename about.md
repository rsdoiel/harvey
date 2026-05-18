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
version: 0.0.4
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.2


date_released: 2026-05-18
---

About this software
===================

## harvey 0.0.4

Bug fixes, security hardening, and UX improvements.

- Removed /model command; model switching is now /ollama use NAME (with alias resolution) and remote dispatch uses @mention syntax to preserve cost-awareness for cloud providers
- Fixed /ollama use not resolving model aliases before connecting to Ollama
- Fixed /ollama probe not resolving model aliases before probing
- Fixed route (@mention) response streaming into the spinner status line; response is now buffered and printed cleanly after the spinner stops
- Fixed session file being truncated when the recording path collides with the resumed session path
- Fixed replay (--replay / /session replay) bypassing sensitive-file and agents/ directory write guards; replayWriteBlocks now uses the same resolveWorkspacePath checks as the tool-call path
- Safe mode now defaults to ON; set safe_mode: false in agents/harvey.yaml to disable
- Alias collision check: /ollama alias set now rejects aliases that clash with an installed Ollama model name, and warns when updating an existing alias to a new value
- Tab completion: model names, aliases, route @names, and slash commands now complete on Tab in the REPL; first Tab shows all matches and fills the longest common prefix, subsequent Tabs cycle through candidates
- Added improved route functionality
  - `/route models` will let you identify the models available by our route provider
  - You can set tooling on or off per route
  - Improved route list output

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


