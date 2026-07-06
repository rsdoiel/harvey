%harvey(1) user manual | version 0.0.15a 6742bf6
% R. S. Doiel
% 2026-06-28

# NAME

harvey

# SYNOPSIS

harvey [OPTIONS] 

# DESCRIPTION

harvey is a tool for scholarly work using natural language programming.
It was inspired by Claude Code but designed for local language model systems
running on small computers like a Raspberry Pi. Language model systems are
commonly called "AI models" or "AI"; harvey treats them as a programmable
interface for deliberate, documented work. harvey supports language model
systems via llamafile (self-contained executables from Mozilla) and Ollama,
and scales from resource-constrained hardware to more capable computers.
harvey can be compiled to run on any system supported by Go. The project
distributes executables for Linux, macOS, and Windows on x86_64 and aarch64.

harvey looks for HARVEY.md in the current directory and uses it as a
system prompt. It connects to a local language model system — llamafile or
Ollama — and opens an interactive natural language programming session.
Cloud providers (Anthropic, DeepSeek, Gemini, Mistral, OpenAI) can be
added as named routes via /route add.

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

init <source>
: seed model aliases from another workspace directory or a .yaml file, then exit.
  SOURCE may be a workspace directory (reads agents/harvey.yaml inside it) or a
  standalone .yaml file with a model_aliases: map at the top level.

-m, --model
: MODEL   Ollama model to use on startup

--ollama URL
: Ollama base URL (default: http://localhost:11434)

--llamafile PATH
: connect to PATH for this session (not persisted to harvey.yaml)

--llamafile-url URL
: override the llamafile API base URL (default: http://localhost:8080)

--llamafile-dir PATH
: override the llamafile discovery directory (default: ~/Models)

-w, --workdir DIR
: workspace directory (default: current directory)

-r, --record
: start a Fountain recording automatically at startup

--record-file FILE
: path for the auto-recording file (implies --record)

--resume
: resume the most recent session automatically (no argument needed)

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

/attach FILE
: attach a file (image, PDF, or text) to the next turn; chooses best representation for the active route

/read-pdf FILE [PAGES]
: extract text from a PDF and inject it into context (requires poppler; PAGES e.g. 1-10)

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

/format FILE [FILE...]
: detect and apply language-appropriate formatters to workspace source files

**Model and backend**

/model [list|use [NAME]|show [NAME]|status|stop|clean|mode [MODEL] MODE|alias ...]
: unified model management across llamafile, Ollama, and llama.cpp backends;
  place .llamafile or .gguf files in ~/Models/ — Harvey discovers them automatically;
  Ollama models are listed live when Ollama is running; use ollama CLI for pull/rm

/workspace <status|init [PATH]>
: show workspace root, alias count and profile; seed aliases from another workspace

/inspect [MODEL]
: show detailed model information (Ollama only)

/route <add NAME URL [MODEL]|remove NAME|use [NAME]|list|on|off|status>
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

/hint
: show actionable suggestions for improving results (RAG, memory, KB)

**Sessions**

/record <start [FILE]|stop|status>
: start or stop Fountain session recording

/rename NAME
: rename the active session file without interrupting recording

/session <list|show [FILE]|use FILE|continue FILE|replay FILE [OUTPUT]>
: list, inspect, load, or replay recorded sessions

**Knowledge base**

/kb <status|search TEXT|inject TEXT|project [ID]|observe KIND BODY|concept NAME>
: query and update the SQLite knowledge base

/rag <list|new NAME|use NAME|drop NAME|setup|ingest PATH|status|query TEXT|on|off>
: manage retrieval-augmented generation stores

/memory <mine|list|show|forget|status|recall|profile> [args...]
: manage the session-experience memory store; mine and recall typed patterns

/recall QUERY
: search all knowledge silos (alias for /memory recall)

/profile <list|show|edit|use|rename> [args...]
: manage the workspace profile (alias for /memory profile)

**Skills**

/skill <list|load NAME|info NAME|status|new|run NAME>
: discover, load, and run agent skills

/skill-set <list|load NAME|info NAME|create NAME|status|unload>
: manage named bundles of skills

**Pipelines and automation**

/pipeline <CONFIDENCE%> FILE [FILE ...]
: chain Markdown prompt files as discrete steps with confidence gating

/plan <TASK | next | status | show | clear>
: generate a GFM checklist plan and execute each step with bounded context

/loop INTERVAL [--count N] PROMPT|/COMMAND
: run a prompt or command repeatedly on a fixed interval

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

Safe mode (/safemode, /safe)
: Restricts which commands may be executed via ! and /run to an explicit
  allowlist. Default allowlist: ls, cat, grep, head, tail, wc, find, stat,
  jq, htmlq, bat, batcat. /safe is an exact alias for /safemode.
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

