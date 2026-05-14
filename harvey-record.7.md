%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

RECORD — record session exchanges to a Fountain file

# SYNOPSIS

/record start [FILE]
/record stop
/record status

# DESCRIPTION

/record saves each user prompt and assistant reply to a plain-text Fountain
.spmd file as the conversation progresses. Recording is on by default: Harvey
starts recording automatically at startup and prints "Recording to …" in the
startup banner.

Recorded files can be loaded later with /session continue (to resume) or
/session replay (to re-run against a different model).

# AUTO-RECORD

Harvey records automatically unless auto-record is disabled. The session
file is created in <workdir>/agents/sessions/ with a timestamped name:

  harvey-session-YYYYMMDD-HHMMSS.spmd

The path is shown in the startup banner. When you exit Harvey the banner
confirms the file was saved:

  Session saved to agents/sessions/harvey-session-20260501-143200.spmd

# SUBCOMMANDS

/record start [FILE]
  Begin recording to FILE. If FILE is omitted, Harvey generates a
  timestamped name in the sessions directory. Returns an error if a
  recording is already active — use /record stop first.

/record stop
  Close the current recording file. The path is printed on exit.
  Harvey continues running; only the recording ends.

/record status
  Show the path of the active recording, or report that no recording
  is in progress.

# CLI FLAGS

  harvey --record             auto-record with a generated filename
  harvey --record-file FILE   auto-record to a specific path

# SEE ALSO

  /session   — continue or replay a recorded session
  /help session

