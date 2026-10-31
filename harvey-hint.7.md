%harvey(7) user manual | version 0.0.20 cd480d8
% R. S. Doiel
% 2026-10-20

# NAME

HINT — show actionable suggestions for improving Harvey's results

# SYNOPSIS

/hint

# DESCRIPTION

/hint inspects the two memory silos — the session-experience memory
store and the knowledge base — and prints a short
list of suggestions for things that would help Harvey give better answers
in this workspace. It takes no arguments and makes no changes; it only
reports what it finds and suggests a command to run next.

# CHECKS

Unmined sessions
  If memory is enabled and there are recorded sessions that have not yet
  been mined for experiential memories, /hint reports the count and
  suggests:

    Run: /memory mine

Knowledge base
  If no SQLite knowledge base is open, suggests recording experiment
  findings so they persist across sessions:

    See: /help kb

# OUTPUT

If every check passes — sessions are mined and the
knowledge base is open — /hint prints a single confirmation line and
points to /help learn for the full memory overview.

# EXAMPLES

~~~
  harvey > /hint
    Sessions unmined: 3
      Harvey can extract learnings from these sessions.
      Run: /memory mine

    Knowledge base not open.
      See: /help kb
~~~

~~~
  harvey > /hint
    Everything looks good — sessions are mined, KB is open.
    Use /help learn for the full memory overview.
~~~

# SEE ALSO

  /help learn     — overview of the memory silos
  /memory status  — summary of the experiential memory store
  /kb stats       — summary of the knowledge base
