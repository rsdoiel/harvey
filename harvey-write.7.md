%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

WRITE — save the last assistant reply to a file

# SYNOPSIS

/write PATH

# DESCRIPTION

/write extracts content from the most recent assistant message and writes it
to PATH inside the workspace.

Content extraction follows this rule: if the reply contains a fenced code
block (~~~ ... ~~~), the contents of the first such block are written.
Otherwise the full reply text is written. This means you can ask the model
to produce a file, inspect the reply, and then /write it without needing to
copy and paste.

PATH is relative to the workspace root. Parent directories must already
exist — /write will not create them. The file is created or overwritten.
Symlinks are not followed. Workspace permissions are checked before writing.

# EXAMPLES

Ask the model to write a Go function and save it:

~~~
  harvey > Write a Go function that parses ISO 8601 dates.
  harvey > /write src/dateparse.go
~~~

Save a full reply (no code block) as a markdown file:

~~~
  harvey > Summarize the three main design decisions in this codebase.
  harvey > /write docs/design-summary.md
~~~

# SEE ALSO

  /read FILE...     — inject file contents into context
  /run COMMAND      — run a shell command after writing
  /help read

