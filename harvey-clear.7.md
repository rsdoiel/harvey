%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

CLEAR — reset the conversation history

# SYNOPSIS

/clear

# DESCRIPTION

/clear discards all messages in the current conversation and starts a fresh
context window. The system prompt (HARVEY.md) is re-injected automatically
so the model retains its role and workspace awareness.

Use /clear when you want to start a new topic without restarting Harvey.
The model has no memory of the previous conversation after /clear.

# WHAT SURVIVES /CLEAR

  System prompt   — re-injected from HARVEY.md automatically.
  Pinned context  — any text set with /context add is re-injected as the
                    first user message, keeping persistent constraints
                    visible to the model across topic changes.
  Recording       — an active /record session keeps running; the cleared
                    conversation is not written to the session file.
  RAG             — the RAG store and its on/off state are unaffected.
  Skills          — the skill catalog remains loaded; /skill load must be
                    re-run to activate a skill in the new context.

# WHAT /CLEAR DOES NOT DO

  - It does not switch models or disconnect the backend.
  - It does not delete session files already written to disk.
  - It does not clear the knowledge base (/kb).

# SEE ALSO

  /context   — manage pinned context that survives /clear
  /summarize — condense history into a summary instead of discarding it

