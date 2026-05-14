%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

STATUS — show Harvey's current runtime state

# SYNOPSIS

/status

# DESCRIPTION

/status prints a snapshot of the active Harvey session. It is the fastest
way to confirm what model you are talking to, how full the context window is,
and whether optional subsystems (RAG, routing, recording) are active.

# OUTPUT FIELDS

Backend
  The active LLM provider and model name, e.g. "ollama (gemma4:e2b)".
  When the Ollama backend was started by Harvey this session the tag
  [Harvey] is appended. When Ollama was already running before Harvey
  connected the tag [external] is appended.

Debug
  Shown only when Harvey was started with --debug. Prints the path of the
  JSONL diagnostic log file being written this session.

History
  Number of messages in the current conversation history (all roles).

Tokens
  Estimated token count for the current history, the model's context
  window size, and the percentage used. An exact count is shown when the
  Ollama tokenizer API responds; otherwise an approximation prefixed with
  "~" is shown. Not shown when the context window size is unknown.

Routing
  "on (N endpoint(s))" when remote routes are configured and enabled.
  "off" otherwise. See /help routing for details.

Workspace
  Absolute path of the workspace root Harvey was started in.

KB
  "open (PATH)" when a SQLite knowledge base is open; "not open" otherwise.

Sessions
  Absolute path of the sessions directory.

Recording
  Path of the active Fountain session file, or "off" when not recording.

# EXAMPLES

~~~
  harvey > /status
  Backend:   ollama (gemma4:e2b) [external]
  History:   5 messages
  Tokens:    ~1 247 / 131 072 (0%)
  Routing:   off
  Workspace: /Users/alice/myproject
  KB:        open (/Users/alice/myproject/agents/knowledge.db)
  Sessions:  /Users/alice/myproject/agents/sessions
  Recording: /Users/alice/myproject/agents/sessions/harvey-session-20260514-094620.spmd
~~~

# SEE ALSO

  /ollama status   — check whether the Ollama daemon is reachable
  /help ollama     — Ollama server and model management
  /help record     — session recording

