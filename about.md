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
version: 0.0.12
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go >= 1.26.3


date_released: 2026-06-11
---

About this software
===================

## harvey 0.0.12

- added `create_dir` built-in tool so models can create directories without `run_command mkdir`
- added `/safe` and `/safe_mode` as aliases for `/safemode`
- unknown slash commands now highlighted in yellow
- llamafile: fixed exec format error on macOS (APE binaries now launched via `/bin/sh`)
- llamafile: added `--server` flag for headless mode (llamafile v0.10.3 API change)
- llamafile: added `-ngl` GPU layer offload support with `gpu_layers` config option (default 99, maximises Metal/CUDA)
- llamafile: `startup_timeout` config option (default 120s); fast-fail on process exit with stderr surfaced in error
- llamafile: debug log now wired to new client after `/llamafile use` model switch
- tool result compaction: prior tool-call rounds are compacted in `RunToolLoop` before each new LLM turn, keeping context bounded during multi-step tasks
- `/plan` command: generate a GFM checklist plan, execute each step with fresh bounded context, track progress in `agents/plan.md`
- `multi-file` skill: auto-detects multi-file creation requests and generates a plan via the compiled script path
- skill trigger regex: fixed `/pattern/flags` format (trailing flag suffix no longer breaks regex mode)
- skill dispatch: compilation failure now falls back to LLM context-injection path instead of erroring out
- skill dispatch: `HARVEY_API_BASE` env var added to compiled script environment
- skill dispatch: LLM-fallback skills now trigger an LLM response turn instead of silently continuing
- plan execution: steps with blocked or failed tool calls are no longer auto-marked complete

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


