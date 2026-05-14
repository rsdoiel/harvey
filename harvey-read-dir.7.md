%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

READ-DIR — read all eligible files in a directory into context

# SYNOPSIS

/read-dir [PATH] [--depth N]

# DESCRIPTION

/read-dir walks a workspace directory and injects every readable, non-binary
file into the conversation as a context message, using the same fenced-block
format as /read.

PATH defaults to the current workspace root. --depth (or -d) controls how
many directory levels to descend; the default is 2 (the target directory plus
one level of subdirectories). --depth 0 means unlimited.

Files are skipped when they:

  - are hidden (name starts with ".")
  - are inside the agents/ subtree
  - match sensitive patterns (.env*, *.pem, *.key, *.p12, *.pfx, harvey.yaml)
  - are binary (contain a null byte in the first 512 bytes)
  - exceed the per-file cap of 64 KB (reported as skipped)

The total context injected is capped at 256 KB. If the cap is hit, Harvey
reports how many files were loaded before stopping.

# EXAMPLES

Load all Go source files in the current package:

  /read-dir harvey/

Load only the top-level files in the workspace (no subdirectories):

  /read-dir . --depth 1

Load the entire docs/ tree:

  /read-dir docs/ --depth 0

# SEE ALSO

  /read      — load specific files into context
  /file-tree — display directory structure without loading files
  /search    — search for a pattern across workspace files

