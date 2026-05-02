---
title: harvey
abstract: "Harvey is an agent similar to Claude Code but written in Go and designed to use Ollama server to access large language models locally or if configurated access [publicai.co](https://publicai.co) and the Abertus model. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent. Most people think only of the over hyped bug companies wasting billions. I think of my little computers and what they can accomplish. Harvey in the play is a mythic creature. Harvey is a Púca, my Harvey is similar in this commercial AI hype craze. How I did learned about Harvey?  When I was young I saw the film on television called [Harvey](https://en.wikipedia.org/wiki/Harvey_(1950_film)) featuring James Stewart. I remember really liking the film as much as I like another old film called Topper. Today I like the idea of a software Harvey that those who take time to see it, or in the case run it on a little computer, can have an adventure and some fun with it."
authors:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903



repository_code: https://github.com/rsdoiel/harvey
version: 0.0.1a
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go &gt;&#x3D; 1.26.2



---

About this software
===================

## harvey 0.0.1a

Proof of concept.

— harvey directory, session overhaul, keyboard shortcuts, KB search & inject
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

### Authors

- R. S. Doiel, <https://orcid.org/0000-0003-0900-6903>






Harvey is an agent similar to Claude Code but written in Go and designed to use Ollama server to access large language models locally or if configurated access [publicai.co](https://publicai.co) and the Abertus model. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent. Most people think only of the over hyped bug companies wasting billions. I think of my little computers and what they can accomplish. Harvey in the play is a mythic creature. Harvey is a Púca, my Harvey is similar in this commercial AI hype craze. How I did learned about Harvey?  When I was young I saw the film on television called [Harvey](https://en.wikipedia.org/wiki/Harvey_(1950_film)) featuring James Stewart. I remember really liking the film as much as I like another old film called Topper. Today I like the idea of a software Harvey that those who take time to see it, or in the case run it on a little computer, can have an adventure and some fun with it.

- License: <https://www.gnu.org/licenses/agpl-3.0.txt>
- GitHub: <https://github.com/rsdoiel/harvey>
- Issues: <https://github.com/rsdoiel/harvey/issues>

### Programming languages

- Go >= 1.26.2




### Software Requirements

- Go >= 1.26.2


### Software Suggestions

- CMTools &gt;&#x3D; 0.0.40
- Pandoc &gt;&#x3D; 3.1
- GNU Make &gt;&#x3D; 3


