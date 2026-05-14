%harvey(1) user manual | version 0.0.3b 51c28d7
% R. S. Doiel
% 2026-05-12

# NAME

harvey

# SYNOPSIS

harvey [OPTIONS] 

# DESCRIPTION

harvey is a terminal agent for local large language models. It was
inspired by Claude Code but focused on working with large language models
in small computer environments like a Raspberry Pi computer running
Raspberry Pi OS. While the inspiration was to run an agent locally with
Ollama it can also be run on larger computers like Linux, macOS and Windows
systems you find on desktop and laptop computers. It should compile for most
systems where Ollama is available and Go is supported (example: *BSD).

harvey looks for HARVEY.md in the current directory and uses it as a
system prompt. It then connects to a local Ollama server and starts an
interactive chat session. Cloud providers (Anthropic, DeepSeek, Gemini,
Mistral, OpenAI) can be added as named routes via /route add.

All file I/O is constrained to the workspace directory (--workdir or ".").
A knowledge base is stored at <workdir>/agents/knowledge.db and is created
automatically on first run. Session recordings (.spmd files) are stored in
<workdir>/agents/sessions/. Both paths can be overridden in agents/harvey.yaml.

Type /help inside the session for available slash commands.

# OPTIONS

-h, --help
: display this help message

-v, --version
: display version information

-l, --license
: display license information

-m, --model
: MODEL   Ollama model to use on startup

--ollama URL
: Ollama base URL (default: http://localhost:11434)
-w, --workdir DIR
: workspace directory (default: current directory)

-r, --record
: start a Fountain recording automatically at startup

--record-file FILE
: path for the auto-recording file (implies --record)

--continue FILE
: load conversation history from a Fountain recording and open the REPL

--replay FILE
: re-send every user turn from FILE to the current model and record fresh responses

--replay-output FILE
: write replay responses to FILE (default: auto-named timestamped file; implies --replay)

--debug
: enable diagnostic mode: sets OLLAMA_DEBUG=1 in the Ollama subprocess and
  writes a JSONL event log to agents/logs/harvey-TIMESTAMP.jsonl covering
  every LLM request/response, RAG injection, tool call, and skill dispatch.
  Use "harvey --help status" to see the log path during a session.

# ENVIRONMENT

ANTHROPIC_API_KEY   API key for Anthropic Claude (optional, for /route add NAME anthropic://)
DEEPSEEK_API_KEY    API key for DeepSeek (optional, for /route add NAME deepseek://)
GEMINI_API_KEY      API key for Google Gemini (optional; GOOGLE_API_KEY also accepted)
MISTRAL_API_KEY     API key for Mistral (optional, for /route add NAME mistral://)
OPENAI_API_KEY      API key for OpenAI (optional, for /route add NAME openai://)

All of the above API key variables are filtered out of every child process
environment — they are never passed to commands run via ! or /run.

# COMMANDS

Type /help TOPIC inside Harvey for the full guide on any topic. All topics
are also available from the shell: harvey --help TOPIC.

**Workspace**

/files [PATH]
: list directory contents inside the workspace

/read FILE [FILE...]
: inject file contents into the conversation as context

/write PATH
: save the last assistant reply (or its first code block) to a file

/read-dir [PATH] [--depth N]
: read all eligible files in a directory tree into context

/file-tree [PATH]
: display a recursive directory tree

/search PATTERN [PATH]
: regex search across workspace files (Go regexp syntax)

/run COMMAND [ARGS...]
: run a shell command; subject to Safe Mode and timeout

/git <status|diff|log|show|blame> [ARGS...]
: read-only git commands in the workspace

**Model and backend**

/model [NAME]
: list all installed models, or switch to NAME

/model alias set ALIAS NAME
: define a short alias for a long model identifier

/ollama <start [debug]|stop|status|list|ps|pull MODEL|push MODEL|show MODEL|create NAME|cp SRC DEST|rm MODEL|probe [MODEL]|logs|use MODEL|env|alias NAME FULLNAME>
: manage the local Ollama server and installed models

/inspect [MODEL]
: show detailed model information (Ollama only)

/route <add NAME URL [MODEL]|rm NAME|list|on|off|status>
: manage named remote LLM endpoints (@mention routing)

**Context and history**

/context <show|add TEXT...|clear>
: manage pinned context that survives /clear

/clear
: reset conversation history (system prompt and pinned context survive)

/summarize
: condense history to a summary, freeing context window space (/compact is an alias)

/status
: show active backend, token usage, routing, recording, and debug state

**Sessions**

/record <start [FILE]|stop|status>
: start or stop Fountain session recording

/rename NAME
: rename the active session file without interrupting recording

/session <continue FILE|replay FILE [OUTPUT]>
: load history from a prior session or replay its turns

**Knowledge base**

/kb <status|search TEXT|inject TEXT|project [ID]|observe KIND BODY|concept NAME>
: query and update the SQLite knowledge base

/rag <list|new NAME|use NAME|drop NAME|setup|ingest PATH|status|query TEXT|on|off>
: manage retrieval-augmented generation stores

**Skills**

/skill <list|load NAME|info NAME|status|new|run NAME>
: discover, load, and run agent skills

/skill-set <list|load NAME|info NAME|create NAME|status|unload>
: manage named bundles of skills

**Security**

/safemode <on|off|status|allow CMD|deny CMD|reset>
: restrict which commands the model may execute

/permissions <list [PATH]|set PATH PERMS|reset>
: fine-grained read/write/exec/delete control per path prefix

/audit <show [N]|clear|status>
: review the in-memory command and file-access audit log

/security status
: unified security posture overview

# SECURITY

Harvey includes several features for controlling what it can do on your system.
All settings survive restart when persisted via the commands below.

Safe mode (/safemode)
: Restricts which commands may be executed via ! and /run to an explicit
  allowlist. Default allowlist: ls, cat, grep, head, tail, wc, find, stat,
  jq, htmlq, bat, batcat.
  Subcommands: on, off, status, allow CMD, deny CMD, reset.

Workspace permissions (/permissions)
: Fine-grained read/write/exec/delete control per path prefix. Persisted
  in agents/harvey.yaml under the permissions: key.
  Subcommands: list [PATH], set PATH PERMS, reset.

Audit log (/audit)
: In-memory ring buffer (1000 events) recording every command, file read,
  file write, and skill invocation.
  Subcommands: show [N], clear, status.

Security overview (/security)
: Displays safe mode state, workspace permissions, and audit buffer status
  in a single view.

# LINE EDITING

Harvey's prompt supports readline-style editing. All key bindings apply
while typing at the "harvey >" prompt.

Navigation:

  Left / Right arrows    move cursor one character
  Home / Ctrl+A          jump to beginning of line
  End  / Ctrl+E          jump to end of line
  Up / Down arrows       cycle through command history

Editing:

  Backspace              delete character before cursor
  Ctrl+D                 delete character under cursor (EOF on empty line)
  Ctrl+K                 delete from cursor to end of line

Actions:

  Ctrl+C                 cancel current input and return to prompt
  Ctrl+X  Ctrl+E         open $EDITOR (then $VISUAL, then vi) to compose
                         a multi-line prompt; content is submitted when
                         the editor exits

