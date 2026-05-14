%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

RENAME — rename the active session recording file

# SYNOPSIS

/rename NAME

# DESCRIPTION

/rename changes the filename of the session currently being recorded without
ending the session. Recording continues to the new file seamlessly.

NAME is a bare filename — do not include a directory path. Harvey places the
renamed file in the same directory as the original (agents/sessions/ by
default). A .spmd extension is added automatically if omitted.

# EXAMPLES

Give the session a meaningful name while it is still running:

  /rename my-feature-session

This renames the current harvey-session-YYYYMMDD-HHMMSS.spmd to
my-feature-session.spmd in agents/sessions/.

  /rename rag-fix-and-context-display

# SEE ALSO

  /record   — start or stop session recording
  /session  — continue or replay a recorded session
  /help record
  /help session

