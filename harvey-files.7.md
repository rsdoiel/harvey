%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

FILES — list workspace directory contents

# SYNOPSIS

/files [PATH]

# DESCRIPTION

/files lists the contents of a directory inside the workspace. Directories
are shown with a trailing "/". Hidden entries (names beginning with ".") are
not suppressed — all entries returned by the OS are shown.

PATH is relative to the workspace root. When omitted, the workspace root
itself is listed.

/files does not recurse. Use /file-tree to display a recursive tree, or
/read-dir to read all files in a directory into context.

Harvey will not list directories outside the workspace root.

# EXAMPLES

List the workspace root:

~~~
  harvey > /files
~~~

List a subdirectory:

~~~
  harvey > /files src/
  harvey > /files docs
~~~

# SEE ALSO

  /file-tree [PATH]   — recursive directory tree display
  /read-dir [PATH]    — read all files in a directory into context
  /read FILE...       — read specific files into context
  /help file-tree
  /help read-dir

