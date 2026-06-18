%harvey(7) user manual | version 0.0.12 9cc2c0a
% R. S. Doiel
% 2026-06-17

# NAME

BUILT-IN TOOLS — tools Harvey exposes to capable LLM models

# DESCRIPTION

Harvey registers a set of built-in tools that are made available to
models with structured tool-calling support. The model may invoke these
tools during a conversation turn; Harvey executes them and returns results
before the next LLM call. All file operations are constrained to the
workspace root; paths outside the workspace are rejected.

Workspace permissions (/permissions) and Safe Mode (/safemode) apply
where noted below.

# FILE TOOLS

read_file PATH [pages]
  Read a file and return its contents. PATH is relative to the workspace
  root. PDF files (.pdf) are automatically extracted to plain text using
  the poppler utilities; use the optional pages parameter to limit
  extraction (e.g. "1-10"). Binary files return an error.

write_file PATH CONTENT
  Write CONTENT to PATH inside the workspace. The file is created or
  overwritten. Parent directories must already exist. Respects workspace
  write permissions.

create_dir PATH
  Create a directory (and any missing parents) at PATH inside the
  workspace. Equivalent to mkdir -p but constrained to the workspace.
  Use this when a task requires creating a new directory tree without
  dropping to run_command.

list_files [PATH]
  List the entries in a workspace directory. Directories are shown with
  a trailing "/". PATH defaults to the workspace root.

file_tree [PATH]
  Display a recursive tree of the workspace (or a subdirectory), skipping
  hidden files and directories.

search_files PATTERN [PATH]
  Search workspace files for lines matching PATTERN (Go regexp syntax).
  Results are capped at 200 matches.

# COMMAND TOOLS

run_command COMMAND [ARGS...]
  Execute a command in the workspace root. Subject to Safe Mode: when
  Safe Mode is on, only allowlisted commands are permitted. Shell
  metacharacters are rejected. Output is capped at 64 KiB.

git_command SUBCOMMAND [ARGS...]
  Run a read-only git subcommand (status, diff, log, show, blame).
  Write operations are blocked regardless of Safe Mode.

# DATE AND TIME TOOLS

current_datetime [format]
  Return the current local date, time, timezone, and UTC equivalent.
  Optional format: "human" (default), "rfc3339", or "unix".

datetime_diff FROM [TO]
  Compute the duration between two datetime strings. TO defaults to now.
  Accepted input formats: RFC3339, YYYY-MM-DD HH:MM:SS, YYYY-MM-DD,
  "Jan 2 2006", "January 2 2006".

format_datetime DATETIME FORMAT
  Parse a datetime string and reformat it. Output formats: "rfc3339",
  "human", "unix", "date" (YYYY-MM-DD), "time" (HH:MM:SS).

# IDENTITY TOOLS

whoami
  Return the current OS username, git user name and email, and hostname.
  Useful when the model is authoring commit messages or project documents
  that need the author's identity.

# SEE ALSO

  /help security   — Safe Mode and workspace permissions
  /help run        — running shell commands interactively
  /tools           — toggle tool calling on/off for the current session

