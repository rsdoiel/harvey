%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

SESSION — continue or replay recorded conversations

# SYNOPSIS

/session continue FILE
/session replay   FILE [OUTPUT]

# DESCRIPTION

Harvey saves every conversation to a Fountain .spmd file. The /session
command lets you reload those files in two distinct ways:

  continue  — restore the conversation history and keep chatting.
  replay    — re-send the original user turns to the current model and
              record fresh responses.

# CONTINUE

/session continue FILE loads all turns from FILE into the current history
and returns you to the REPL. The model sees the full prior conversation as
if it had been running the whole time.

Use continue to:

  - Resume work across Harvey restarts.
  - Switch to a different model and pick up mid-conversation.
  - Inspect and then extend a session that was auto-saved.

Harvey also offers to continue the most recently saved session at startup;
pressing Enter at that prompt is equivalent to /session continue.

# REPLAY

/session replay FILE [OUTPUT] re-runs a session by sending each user turn
to the currently connected model in sequence. The model's fresh responses
are recorded to OUTPUT (default: an auto-named file in the sessions
directory).

Use replay to:

  - Run an old conversation through a new or different model for comparison.
  - Re-generate responses after changing the system prompt (HARVEY.md).
  - Benchmark how different models handle the same sequence of prompts.

Replay does not show the original assistant responses — it only shows the
new ones produced by the current model.

# SESSION FILE FORMAT

Session files use the Fountain screenplay format with a .spmd extension.
Each exchange is an INT scene with speaker labels (RSDOIEL, HARVEY, model
name). These files are plain text and human-readable.

Default save location: <workdir>/agents/sessions/
File naming:          harvey-session-YYYYMMDD-HHMMSS.spmd

# CLI FLAGS

The same operations are available as startup flags:

  harvey --continue FILE         load history then open REPL
  harvey --replay FILE           replay without entering REPL
  harvey --replay-output FILE    write replay output to FILE

# SEE ALSO

  /record          — start or stop session recording manually
  harvey --help    — full CLI flag reference

