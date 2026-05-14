%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

RUN — execute a shell command inside the workspace

# SYNOPSIS

/run COMMAND [ARGS...]

# DESCRIPTION

/run executes COMMAND with the given ARGS in the workspace root directory.
The command's combined stdout and stderr are printed to the Harvey REPL.
Output is truncated at 64 KiB to protect the context window.

Shell metacharacters (;, |, &, >, <, $, backtick, (, ), {}, []) are rejected.
This means /run is not a shell — you cannot pipe commands or use
redirection. Use the ! prefix for multi-word shell lines when you need
that, subject to the same Safe Mode restrictions.

SAFE MODE

When Safe Mode is on, only commands in the allowlist may be executed.
The default allowlist is: ls, cat, grep, head, tail, wc, find, stat,
jq, htmlq, bat, batcat. Use /safemode allow CMD to extend it.

If /run is blocked by Safe Mode, Harvey prints the allowlist and
suggests /safemode allow CMD. See /help security for full details.

ENVIRONMENT FILTERING

API keys and other sensitive environment variables are stripped from
the child process before it runs. The child process inherits the rest
of the Harvey environment.

TIMEOUT

The default run timeout is 5 minutes. Override with run_timeout in
agents/harvey.yaml (e.g. run_timeout: "2m").

# EXAMPLES

~~~
  harvey > /run go test ./...
  harvey > /run ls -la src/
  harvey > /run grep -r "TODO" .
~~~

# SEE ALSO

  /git <status|diff|...>   — read-only git commands (always allowed)
  /safemode                — configure the command allowlist
  /help security           — Safe Mode, permissions, and audit log

