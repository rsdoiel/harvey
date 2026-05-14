%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

READ — inject file contents into the conversation context

# SYNOPSIS

/read FILE [FILE...]

# DESCRIPTION

/read loads one or more workspace files and injects their contents into the
conversation as a user-role message. The model sees the file contents in the
very next turn and can answer questions about them, suggest edits, or use
them as reference material.

Multiple files are concatenated with a blank line between each, all in a
single injected message. Each file's content is preceded by a header showing
its workspace-relative path.

FILE is a path relative to the workspace root. Absolute paths outside the
workspace are rejected. Symlinks are not followed. Sensitive files
(e.g. .env, id_rsa, harvey.yaml) are blocked regardless of permissions.

The agents/ directory is off-limits to /read to prevent skills and
configuration from being inadvertently exposed.

Context window impact: reading large files can quickly consume the model's
context window. Check /status after reading to see the token impact.

# EXAMPLES

Read a single file:

~~~
  harvey > /read src/main.go
~~~

Read several files at once:

~~~
  harvey > /read README.md docs/ARCHITECTURE.md
~~~

Read then ask a question:

~~~
  harvey > /read harvey.go
  harvey > What does the ragAugment function do?
~~~

# SEE ALSO

  /read-dir [PATH]   — read all files in a directory
  /files [PATH]      — list directory contents
  /status            — check remaining context window space
  /help read-dir

