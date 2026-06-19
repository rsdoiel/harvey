%harvey(7) user manual | version 0.0.13 aa87def
% R. S. Doiel
% 2026-06-19

# NAME

HINT — show actionable suggestions for improving Harvey's results

# SYNOPSIS

/hint

# DESCRIPTION

/hint inspects the three memory silos — the session-experience memory
store, the active RAG store, and the knowledge base — and prints a short
list of suggestions for things that would help Harvey give better answers
in this workspace. It takes no arguments and makes no changes; it only
reports what it finds and suggests a command to run next.

# CHECKS

Unmined sessions
  If memory is enabled and there are recorded sessions that have not yet
  been mined for experiential memories, /hint reports the count and
  suggests:

    Run: /memory mine

RAG store
  No store configured
    Suggests creating one and ingesting reference documents:

      Run: /rag new NAME   then   /rag ingest <file>
      See: /help learn

  Store configured but empty
    Suggests ingesting documents into the active store:

      Run: /rag ingest <file>   (PDF, .md, .txt, .go, .ts, ...)
      See: /help rag

  Store has chunks but RAG is off
    Suggests turning RAG on so chunks are prepended to prompts:

      Run: /rag on

  Store configured but not open
    Suggests checking its status:

      Run: /rag status

Knowledge base
  If no SQLite knowledge base is open, suggests recording experiment
  findings so they persist across sessions:

    See: /help kb

# OUTPUT

If every check passes — RAG is on with chunks, sessions are mined, and the
knowledge base is open — /hint prints a single confirmation line and
points to /help learn for the full memory overview.

# EXAMPLES

~~~
  harvey > /hint
    Sessions unmined: 3
      Harvey can extract learnings from these sessions.
      Run: /memory mine

    RAG is off but store "default" has 142 chunk(s).
      Enabling RAG prepends relevant chunks to each prompt.
      Run: /rag on
~~~

~~~
  harvey > /hint
    Everything looks good — RAG is on with chunks, sessions are mined, KB is open.
    Use /help learn for the full memory overview.
~~~

# SEE ALSO

  /help learn     — overview of all three memory silos
  /memory status  — summary of the experiential memory store
  /rag status     — summary of the active RAG store
  /kb status      — summary of the knowledge base
