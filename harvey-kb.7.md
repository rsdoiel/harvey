%harvey(7) user manual | version 0.0.16 66875a0
% R. S. Doiel
% 2026-09-15

# NAME

KB — knowledge base management

# SYNOPSIS

/kb [status]
/kb search TERM [TERM...]
/kb inject [PROJECT]
/kb project <list|add NAME [DESC]|use ID>
/kb observe [KIND] TEXT
/kb concept <list|add NAME [DESC]>
/kb learn [ingest|draft|review|concepts] [ARGS]

# DESCRIPTION

Harvey keeps a SQLite knowledge base at <workdir>/agents/knowledge.db.
It stores structured notes about experiments and concepts so you can
search and inject that context into conversations without relying on
the model's general knowledge.

The knowledge base is independent of the RAG store (/help rag). KB holds
hand-authored structured records; RAG holds embedded chunks from ingested
documents. Use both: /kb inject to bring structured records into context,
and RAG to retrieve relevant document passages automatically.

# CONCEPTS

  Project     — a named container for a body of work. One project can be
                "active" at a time; /kb observe attaches to the active project.

  Observation — a timestamped note attached to a project. Each observation
                has a kind:

                  note        — general remark
                  finding     — empirical result
                  decision    — a choice made and its rationale
                  question    — open question to return to
                  hypothesis  — testable prediction

  Concept     — a named idea or term that can be referenced across multiple
                projects and observations.

# SUBCOMMANDS

/kb status
  Show the database path, project count, and observation count.

/kb search TERM [TERM...]
  Full-text search (FTS5) across all observations and concepts. Supports
  quoted phrases and prefix wildcards:

~~~
  /kb search RAG embedding
  /kb search "context window"
  /kb search grpc*
~~~

/kb inject [PROJECT]
  Format the knowledge base as Markdown and add it to the conversation
  as a user message. With no argument, injects the active project (or all
  projects if none is active). With a project name, injects only that project.

~~~
  /kb inject
  /kb inject harvey
~~~

/kb project list
  List all projects with ID, name, and status. The active project is
  marked with *.

/kb project add NAME [DESCRIPTION]
  Create a project and set it as the active project.

~~~
  /kb project add harvey "terminal coding agent for Ollama"
~~~

/kb project use ID
  Set an existing project as the active project by numeric ID.

/kb observe [KIND] TEXT
  Record an observation against the active project. KIND defaults to
  "note" if omitted. Valid kinds: note, finding, decision, question,
  hypothesis.

~~~
  /kb observe finding RAG threshold of 0.3 eliminates noise on granite3-moe
  /kb observe decision switched embedding model to nomic-embed-text
  /kb observe question does bge-m3 outperform nomic on code retrieval?
~~~

/kb concept list
  List all concepts with ID and description.

/kb concept add NAME [DESCRIPTION]
  Add a named concept to the knowledge base.

~~~
  /kb concept add RAG "retrieval-augmented generation"
  /kb concept add "context window" "token budget for a single LLM call"
~~~

# LEARNING MODE

/kb learn turns Harvey's own recorded sessions and hand-off notes into
searchable knowledge. Every step ends with a human decision: nothing a model
writes is trusted, or found by /kb search, until you accept it. All four
steps work on the active project (/kb project use ID).

/kb learn ingest [--min-words N] [--all] [--dry-run]
  List the hand-off notes and recorded sessions that are not yet in the
  knowledge base, newest first, and ingest the ones you choose. Hand-offs
  are always offered; a session must have at least 200 words (--min-words
  changes that, --all offers every session). New sections start
  unsummarized.

/kb learn draft [--limit N] [--dry-run] [@model]
  Ask a model to draft a summary of each unsummarized section, then of each
  whole document from its section summaries. The model is @model if given,
  else learn_model in agents/harvey.yaml, else the active model. Default
  limit is 25 items (--limit 0 drafts everything queued); a small local model
  takes about a minute per item, so a full run over many documents is long.
  Each draft gets a confidence score, which is only how many of the known
  concepts in the source the draft also names. It is not a measure of
  whether the draft is faithful.

/kb learn review [@model]
  Walk the drafts one at a time, showing the source excerpt, size, known
  concepts mentioned, confidence and which model wrote it:

~~~
  [a]ccept   trust the summary; it becomes searchable
  [e]dit     open $EDITOR on the draft (the edit is recorded as "human")
  [r]edraft  ask the model again
  [s]kip     leave it drafted (Enter also skips)
  [q]uit
~~~

  Bare /kb learn drafts, then reviews. A summary whose source changed after
  it was written is marked STALE.

/kb learn concepts [--limit N]
  Suggest new concepts from the ingested documents (most distinctive terms
  that are not yet concepts; default 20). You pick which become concepts,
  by number, range (1-3), all or none. Each pick is added to the knowledge
  base at once. Then, for each project document that mentions a picked
  concept more than once, Harvey shows the changed lines and asks before
  writing: [[Concept]] links, and footnotes for near-miss spellings.

  A write goes through the permissions table (/permissions) like any other
  file write, and a path Harvey may not write is refused before you are
  asked. The document is re-ingested straight afterwards; if that fails,
  the file is put back. Fountain documents (sessions and hand-offs) are
  never rewritten: they are records, and [[...]] in them reads as a note.
  Tagging changes source text, so summaries of changed sections are marked
  stale; the closing line says how many.

# WORKFLOW EXAMPLE

~~~
  /kb project add myapp "Go CLI for processing audio files"
  /kb observe decision using ffmpeg via exec.Command, not a Go binding
  /kb observe finding ffmpeg probe takes ~80 ms per file on Pi 4
  /kb observe question can we batch probe calls to reduce overhead?
  /kb concept add ffmpeg "audio/video processing CLI"
  /kb inject
~~~

After /kb inject the model sees the full project record as context and can
answer questions about it, suggest next steps, or help resolve open questions.

# SEE ALSO

  /memory recall         — search all knowledge silos including the KB
  /rag ingest            — embed documents for semantic retrieval
  /help learn            — overview of all three memory silos

