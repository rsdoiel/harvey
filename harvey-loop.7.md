%harvey(7) user manual | version 0.0.13 b877e04
% R. S. Doiel
% 2026-06-19

# NAME

LOOP — repeat a prompt or slash command at a fixed interval

# SYNOPSIS

/loop INTERVAL [--count N] PROMPT
/loop INTERVAL [--count N] /COMMAND [ARGS...]

# DESCRIPTION

/loop repeats PROMPT or /COMMAND every INTERVAL for up to N iterations,
blocking the REPL until finished or cancelled. It is designed for
workflows like "check the build every 5 minutes" or "run /git status
every 30 seconds while I refactor."

A single Ctrl+C cancels the current iteration and any pending sleep,
then returns to the Harvey prompt.

# ARGUMENTS

  INTERVAL      Required. Parsed by parseDurationString: a plain integer is
                treated as seconds (e.g. 300 → 5 minutes); Go duration
                strings such as 30s, 5m, and 1h30m are also accepted.
                Must be positive.

  --count N     Optional. Number of iterations, integer in [1, 100].
                Default: 10.

  PROMPT        Free text sent as a chat turn each iteration. The same
                RAG augmentation, tool-loop execution, and recording
                that apply to normal chat apply here.

  /COMMAND      A slash command dispatched each iteration, exactly as if
                typed at the prompt. The command's own safe-mode checks,
                audit logging, and recording are preserved. /exit, /quit,
                and /bye are recognised and stop the loop rather than
                exiting Harvey.

# ITERATION BEHAVIOUR

Chat iterations use the same model call as normal chat — same RAG
augmentation, same tool-loop-or-plain-chat branch, same stats recording
and Fountain recording — so looping a prompt behaves identically to
typing it by hand repeatedly. Two things are deliberately excluded:

  Interactive write-offers — the fenced-code-block "write to file?"
    prompts and autoExecuteReply are skipped, because an unattended
    loop must never block waiting for stdin input.

  Skill auto-trigger — the trigger-word dispatch that redirects prompts
    to registered skills is skipped, because a looped prompt should
    reach the model directly and consistently on every iteration.

A transient error in one iteration (e.g. a model timeout) is printed
inline but does not stop the loop — only Ctrl+C or the count limit does.

# EXAMPLES

  Check the build every five minutes, up to the default 10 times:
    /loop 5m Check the build and summarise any failures.

  Run git status every 30 seconds, 3 times:
    /loop 30s --count 3 /git status

  Ask the model to review recent log entries once per minute, 20 times:
    /loop 60s --count 20 Summarise any new errors in the log.

# SEE ALSO

  /help pipeline  — chain prompts with confidence gating
  /help run       — execute shell commands from the REPL
