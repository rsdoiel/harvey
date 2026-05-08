---
title: harvey
abstract: "Harvey is an agent similar to Claude Code but written in Go and designed to use Ollama server to access large language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent. Most people think only of the over hyped big companies wasting billions. I think of my little computers and what they can accomplish. Harvey in the play is a mythic creature. Harvey is a Púca, my Harvey is similar in this commercial AI hype craze. How I did learned about Harvey? When I was young I saw the film on television called [Harvey](https://en.wikipedia.org/wiki/Harvey_(1950_film)) featuring James Stewart. I remember really liking the film as much as I like another old film called Topper. Today I like the idea of a software Harvey that those who take time to see it, or in the case run it on a little computer, can have an adventure and some fun with it."
authors:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903



repository_code: https://github.com/rsdoiel/harvey
version: 0.0.2
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go &gt;&#x3D; 1.26.2


date_released: 2026-05-08
---

About this software
===================

## harvey 0.0.2

Working proof of concept.

- harvey directory, session overhaul, keyboard shortcuts, KB search & inject
- Renamed state directory from .harvey/ to harvey/ (workspace-local and ~/harvey/ globally)
- Dropped SQLite sessions.db in favour of Fountain/SPMD session files; sessions now live in harvey/sessions/ and persist as .spmd recordings automatically on every run
- Session resume prompt now appears before Ollama model selection (default: No); selecting a prior session pre-selects the model recorded in that file
- Added .spmd as the primary session file extension; .fountain files from other LLM systems are also accepted everywhere Harvey reads sessions
- Added harvey/harvey.yaml for per-workspace overrides: knowledge_db, sessions_dir, agents_dir, auto_record
- Added Ctrl+X Ctrl+E chord to the termlib line editor: opens $EDITOR (→ $VISUAL → vi) with the current prompt buffer; saved content is submitted when the editor exits
- Added FTS5 full-text search to the knowledge base: /kb search TERM ranks results across observations, projects, and concepts using bm25(); supports FTS5 phrase and prefix syntax
- Added /kb inject [PROJECT-NAME]: formats knowledge base content as Markdown and adds it to the conversation context so the LLM can reason about it
- Added keyboard-shortcuts section to getting_started.md and LINE EDITING section to --help / man page
- Added RAG support and enhance metadata handling for the models pull from ollama
- Switch from custom LLMClient to Mozilla's any-llm-go.
- Dropped support for publicai.co's API (couldn't access it for testing)
- Security hardening: added safe mode command allowlist (/safemode), path-based workspace permissions (/permissions), in-memory audit log (/audit), and API key filtering for all child processes
- Security fix: removed hard-coded HTTP timeout on Ollama provider (was 120 s, caused failures on slow hardware such as Raspberry Pi); timeout is now configurable via ollama_timeout in harvey.yaml, defaulting to unlimited
- Configurable shell-command timeout via run_timeout in harvey.yaml (default 5 minutes, supports Go duration strings such as "10m" or "1m30s")
- Safe mode and allowlist changes now persist across sessions in harvey.yaml
- Fixed RAG startup error (SQLITE_CANTOPEN) when agents/rag/ directory does not exist; NewRagStore now creates parent directories automatically
- Fixed /rag on and /rag off not persisting the enabled state to harvey.yaml
- Fixed session model not being restored on resume when the prior session had no chat turns; model is now written to the Fountain title page at recording start
- Added context window usage to the post-response stat line: shows current/max tokens and percentage (e.g. 1840/32768 ctx (5%)); /status also shows the percentage; limit is sourced from --context flag or model cache automatically
- Added TAGGED column to /ollama list showing whether each model reliably emits path-tagged code blocks (used by auto-execute)
- Startup model picker now displays the same capability table as /ollama list with numbered rows for selection
- Added /rename NAME command to rename the active session file without ending the session
- Added /file-tree [PATH] command to display a tree-style workspace listing

## Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)






Harvey is an agent similar to Claude Code but written in Go and designed to use Ollama server to access large language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent. Most people think only of the over hyped big companies wasting billions. I think of my little computers and what they can accomplish. Harvey in the play is a mythic creature. Harvey is a Púca, my Harvey is similar in this commercial AI hype craze. How I did learned about Harvey? When I was young I saw the film on television called [Harvey](https://en.wikipedia.org/wiki/Harvey_(1950_film)) featuring James Stewart. I remember really liking the film as much as I like another old film called Topper. Today I like the idea of a software Harvey that those who take time to see it, or in the case run it on a little computer, can have an adventure and some fun with it.

- [License](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Code Repository](https://github.com/rsdoiel/harvey)
  - [Issue Tracker](https://github.com/rsdoiel/harvey/issues)

## Programming languages

- Go >= 1.26.2




## Software Requirements

- Go >= 1.26.2


## Software Suggestions

- CMTools >= 0.0.43
- Pandoc >= 3.9
- GNU Make >= 3.8


