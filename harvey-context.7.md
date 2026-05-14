%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

CONTEXT — manage pinned context that survives /clear

# SYNOPSIS

/context show
/context add TEXT...
/context clear

# DESCRIPTION

Pinned context is a block of text that is always present as the first user
message after the system prompt. It survives /clear so the model keeps
seeing it no matter how many times you reset the conversation.

Use pinned context for information the model should never lose sight of:

  - A project description or goal that frames every question.
  - Key constraints ("do not modify files outside agents/").
  - A running summary you composed to replace a long conversation.
  - Environment facts that are not in HARVEY.md.

Pinned context is stored in memory only; it is not persisted to
agents/harvey.yaml or session files. It resets when Harvey exits.

# SUBCOMMANDS

/context show
  Print the current pinned context and its byte count. If no context is
  set, prints "(pinned context is empty)".

/context add TEXT...
  Append TEXT to the pinned context. Multiple words are joined with a
  space. Calling add again appends a newline then the new text so you can
  build up multi-line context incrementally.

~~~
  /context add This project targets Raspberry Pi OS (armv7l).
  /context add Never use cgo; the binary must be statically linked.
~~~

/context clear
  Remove the pinned context and delete the pinned-context message from the
  conversation history. The model will not see it in subsequent turns.

# RELATION TO /CLEAR

/clear resets the conversation history but keeps pinned context. After
/clear, the system prompt is re-injected, then the pinned context is
re-injected as the first user message, so the model's next turn starts
with both.

# SEE ALSO

  /clear       — reset conversation history (pinned context survives)
  /summarize   — condense history; combine with /context add to preserve
                 a summary across /clear

