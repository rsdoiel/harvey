%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

FILE-TREE — display a tree listing of the workspace

# SYNOPSIS

/file-tree [PATH]

# DESCRIPTION

/file-tree prints the workspace directory structure using tree-style
box-drawing characters (├──, └──). Hidden files and directories (names
starting with ".") are excluded.

An optional PATH restricts the listing to a subdirectory of the workspace
root. Paths outside the workspace are rejected.

# EXAMPLES

Show the full workspace:

  /file-tree

Show only the harvey/ subdirectory:

  /file-tree harvey/

# OUTPUT FORMAT

  .
  ├── harvey/
  │   ├── commands.go
  │   └── harvey.go
  └── agents/
      └── harvey.yaml

# SEE ALSO

  /read   — read a file into context
  /status — show workspace path

