%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

KB — knowledge base management

# SYNOPSIS

/kb [status]
/kb search TERM [TERM...]
/kb inject [PROJECT]
/kb project <list|add NAME [DESC]|use ID>
/kb observe [KIND] TEXT
/kb concept <list|add NAME [DESC]>

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

