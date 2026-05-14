%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

GIT — run read-only git commands in the workspace

# SYNOPSIS

/git <status|diff|log|show|blame> [ARGS...]

# DESCRIPTION

/git runs read-only git subcommands in the workspace root and prints their
output to the REPL. Only the five safe, non-mutating subcommands are
permitted; write operations such as commit, push, checkout, and reset are
blocked.

ARGS are passed directly to the underlying git command, so all the usual
flags and path arguments work.

Output is capped at 64 KiB. Sensitive environment variables are filtered
from the git process.

/git operates on whatever repository contains the workspace root. If the
workspace is not inside a git repository, git will report an error.

# SUBCOMMANDS

/git status [ARGS...]
  Show the working tree status.

/git diff [ARGS...]
  Show unstaged or staged changes. Pass --staged for staged-only.

/git log [ARGS...]
  Show the commit log. Useful flags: --oneline, -n N, --since DATE.

/git show [REF]
  Show a commit, tag, or tree object.

/git blame FILE
  Show what revision last modified each line of FILE.

# EXAMPLES

~~~
  harvey > /git status
  harvey > /git diff HEAD~1
  harvey > /git log --oneline -10
  harvey > /git blame src/main.go
~~~

After reviewing changes, ask the model:

~~~
  harvey > /git diff
  harvey > Explain what changed and whether there are any risks.
~~~

# SEE ALSO

  /run COMMAND   — run arbitrary (safe-mode-checked) commands
  /help run

