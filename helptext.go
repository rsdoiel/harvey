package harvey

var (
	// ─── Main man page ──────────────────────────────────────────────────────────

	// HelpText is the primary harvey(1) man page. Displayed by harvey --help
	// or harvey help (no topic). Generates harvey.1.md.
	HelpText = `%{app_name}(1) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

{app_name}

# SYNOPSIS

{app_name} [OPTIONS] 

# DESCRIPTION

{app_name} is a tool for scholarly work using natural language programming.
It was inspired by Claude Code but designed for local language model systems
running on small computers like a Raspberry Pi. Language model systems are
commonly called "AI models" or "AI"; {app_name} treats them as a programmable
interface for deliberate, documented work. {app_name} supports language model
systems via llamafile (self-contained executables from Mozilla) and Ollama,
and scales from resource-constrained hardware to more capable computers.
{app_name} can be compiled to run on any system supported by Go. The project
distributes executables for Linux, macOS, and Windows on x86_64 and aarch64.

{app_name} looks for HARVEY.md in the current directory and uses it as a
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

`

	// FirstRunWizardText is shown at startup when no backend is reachable and
	// no backend has been configured. It guides new users to their first model.
	FirstRunWizardText = `
Harvey couldn't find a model to connect to.

To get started with a local model (no internet required after download):

  1. Download a llamafile from:
       https://huggingface.co/Mozilla/llamafile-models
     Recommended:   Qwen2.5-Coder-7B-Q5_K_S.llamafile  (~5 GB)
     Low-memory:    Phi-3.5-mini-instruct-Q4_K_M.llamafile  (~2 GB)

  2. Place it in ~/Models/ (or any directory).

  3. Run Harvey again — it will find the file automatically.

Alternatively, install Ollama (https://ollama.com) and pull a model:
  ollama pull qwen2.5-coder:7b

`

	// ─── Model backends ─────────────────────────────────────────────────────────

	// ModelHelpText is shown by /help model and harvey --help model.
	ModelHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

MODEL — backend-agnostic model management and inline switching

# SYNOPSIS

/model [list|use [NAME]|show [NAME]|status|mode [MODEL] MODE|alias ...]

@NAME  [prompt...]
@TAG   [prompt...]

# DESCRIPTION

The /model command manages models across all backends — llamafile, Ollama,
and llama.cpp — using a single consistent interface.

The @NAME prefix switches the active model inline, within the current prompt.
@TAG resolves via purpose tags: if an alias is tagged "code", @code switches
to it. History is preserved — the switch is like a new character entering
the scene.

# SUBCOMMANDS

  /model
  /model show [NAME]
    Print the currently active model and backend. If NAME is given, show
    details for that model.

  /model list
    List all registered models across llamafile, Ollama, and llama.cpp,
    marking the active entry with an arrow. Purpose tags are shown where
    defined.

  /model use [NAME]
    Switch to the named model. If NAME is omitted, Harvey shows a combined
    numbered picker of all locally available models across all backends. On
    first selection of a model without an alias, you are prompted for an alias
    name and optional purpose tags; the choice is saved to agents/harvey.yaml.
    If NAME is provided, Harvey resolves it as an alias, model name, or purpose
    tag (in that order) and starts the required backend if needed.

  /model status
    Show whether the active backend is reachable.

  /model mode
    Show the tool-execution mode for the active model.

  /model mode MODE
    Set the tool-execution mode for the active model. MODE must be one of:

      auto         Clear any previously set override and return to the
                   capability-detected default. Use this to undo a manual
                   mode setting.

      structured   Force OpenAI-style tool_calls (RunToolLoop). Use when the
                   model supports structured tool calls but the auto-probe
                   disagrees.

      prose        Force the prose JSON-fence fallback path. The model is
                   expected to emit JSON fenced blocks instead of structured
                   tool_calls.

      inject       Disable tool calling entirely; Harvey pre-injects the
                   content of any files mentioned in the prompt directly into
                   the context window. Best for models that ignore the tools
                   schema.

      none         Disable tools and file injection; plain text only.

  /model mode MODEL MODE
    Set the tool-execution mode for a named model (need not be the active one).
    Useful for pre-configuring a model before switching to it.

  The mode is persisted in the model cache (agents/model_cache.db) and survives
  across sessions. It overrides the auto-detected capability from capability probing.

  /model stop
    Stop the active llamafile or llama.cpp server if Harvey started it.
    Backends not started by Harvey are left running.

# AT-MENTION SWITCHING

Prefix any prompt with @NAME to switch to that model for this turn and all
subsequent turns:

  @phi-mini summarise this in under 100 words
  @code     rewrite this function to avoid the allocation

Resolution order:
  1. Named route (/route list)
  2. Model alias or exact model name
  3. Purpose tag — if an alias carries the tag "code", @code resolves to
     the first alphabetically matching alias with that tag.

If NAME matches none of the above, the @ prefix is treated as part of the
prompt and no switch occurs. History is never cleared on a switch.

Switch notes are written to the session file:
  [[model switch: phi-mini (llamafile) at 2026-06-20 14:32:11]]

# MODEL ALIASES

  /model alias list
    List all defined aliases with full model names and purpose tags.

  /model alias add ALIAS FULL_NAME [--tags TAG,TAG,...]
    Define an alias. Optional --tags lists purpose labels (e.g. code, chat,
    embed) that enable @TAG routing. Also accepts: set.

  /model alias tags ALIAS TAG [TAG...]
    Replace the purpose tags on an existing alias.

  /model alias remove ALIAS
    Remove an alias (also accepts: rm, delete).

Aliases are persisted to agents/harvey.yaml under model_aliases:. Purpose
tags also resolve at @mention time: @code dispatches to any alias tagged
"code".

# SEE ALSO

  harvey-routing(7), /help model-alias, /help workspace

`

	// ModelAliasHelpText is shown by /help model-alias and harvey --help model-alias.
	// It covers @mention inline switching and the /model alias subcommands.
	ModelAliasHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

MODEL ALIAS — inline model switching, purpose tags, and short-name aliases

# SYNOPSIS

@NAME [prompt...]
@TAG  [prompt...]

/model alias [list|add ALIAS FULLNAME [--tags T,T]|tags ALIAS TAG...|set ALIAS FULLNAME|remove ALIAS]

# DESCRIPTION

Harvey supports two ways to work with multiple models in a session:

  @NAME / @TAG syntax — switch the active model inline, as part of a prompt.
  /model alias        — define short names for long model identifiers,
                        optionally annotated with purpose tags.

Both are preserved in the session recording as Fountain notes so the
memory miner can attribute turns to the correct model.

# AT-MENTION SWITCHING

Prefix any prompt with @NAME to switch to that model for this turn and
all subsequent turns:

  @phi-mini summarise this function in one sentence
  @code     rewrite the loop to avoid the allocation
  @qwen-coding refactor using the repository pattern

Resolution order for @NAME:
  1. Named route (/route list)
  2. Model alias or exact model name
  3. Purpose tag — if any alias carries the tag NAME, Harvey resolves to
     the first (alphabetically) such alias.

If NAME matches none of the above, the @ prefix is treated as part of the
normal prompt — no error, no switch.

Conversation history is preserved unchanged across a switch. The model
switch is recorded in the session file as:

  [[model switch: NAME (BACKEND) at TIMESTAMP]]

Use @NAME with no trailing text to switch model without sending a prompt:

  @phi-mini

# MODEL ALIASES

Aliases let you use short, memorable names for long model identifiers.
Purpose tags let @TAG routing find the right model by role rather than name.

They are stored in agents/harvey.yaml under model_aliases: and persist
across sessions:

~~~yaml
model_aliases:
  coder:
    model: qwen2.5-coder:7b
    tags: [code, review]
  fast:
    model: phi3:mini
    tags: [chat, fast]
  embed-default:
    model: nomic-embed-text:latest
    tags: [embed]
~~~

  @coder tell me about this function    # resolves by alias name
  @code  rewrite this loop              # resolves by purpose tag

## Subcommands

  /model alias list
    List all defined aliases with their full model names and purpose tags.

  /model alias add ALIAS FULLNAME [--tags TAG,TAG,...]
    Define a new alias. FULLNAME is an Ollama model identifier, a registered
    llamafile name, or a llama.cpp model path. Optional --tags lists purpose
    labels that enable @TAG routing (comma-separated, no spaces).
    "add" is the preferred form; "set" is also accepted for compatibility.

  /model alias tags ALIAS TAG [TAG...]
    Replace the purpose tags on an existing alias. Overwrites any previous
    tags; pass no tags to clear them.

  /model alias set ALIAS FULLNAME
    Alias for "add".

  /model alias remove ALIAS
    Remove an alias. Also accepted: rm, delete.

# SEE ALSO

  /help model        — unified /model command reference
  /help routing      — named remote endpoints and @mention dispatch
  /help workspace    — seed aliases from another workspace (harvey init)

`

	// RoutingHelpText is shown by /help routing and harvey --help routing.
	// Generates harvey-routing.7.md.
	RoutingHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

ROUTING

# SYNOPSIS

/route <add NAME URL [MODEL]|rm NAME|models URL|probe NAME|set NAME tools on|off|list|on|off|status>

@name prompt text

# DESCRIPTION

Harvey can dispatch individual prompts to remote LLM endpoints — other Ollama
instances on a Pi cluster, Llamafile servers, or cloud providers — using
@mention syntax. Prefix any prompt with @name to send it to the named endpoint instead
of the local model. The reply is streamed back and lands in the local
conversation history so future turns retain full context.

Routing is explicitly user-driven: there is no automatic classification.
You choose which endpoint handles each prompt by using (or omitting) an
@mention.

# CONTEXT WINDOW

When a prompt is dispatched to a remote endpoint, Harvey sends the last
10 non-system messages from the local history alongside it. System messages
are excluded. This gives the remote model enough context to be useful without
sending the entire conversation over the network. The window size is a
starting point and will be tuned over time.

# ENDPOINT TYPES

Local providers (no API key):

  ollama://host:port    A remote Ollama server (also accepts http:// and https://).
  llamafile://host:port A Llamafile binary server (OpenAI-compatible, port 8080).
  llamacpp://host:port  A llama.cpp server (OpenAI-compatible, port 8080).

Cloud providers (API key read from environment):

  anthropic://  Anthropic Claude  (ANTHROPIC_API_KEY)
  deepseek://   DeepSeek          (DEEPSEEK_API_KEY)
  gemini://     Google Gemini     (GEMINI_API_KEY or GOOGLE_API_KEY)
  mistral://    Mistral           (MISTRAL_API_KEY)
  openai://     OpenAI            (OPENAI_API_KEY)

# EXAMPLE SESSION

~~~
  # Register a local llamafile server (running on default port 8080)
  /route add lf llamafile://localhost:8080 bonsai-8b

  # Register a local llama.cpp server (llama-server on port 8081)
  /route add cpp llamacpp://localhost:8081 phi4-Q4_K_M

  # Register a Pi cluster node running Ollama
  /route add pi2 ollama://192.168.1.12:11434 llama3.1:8b

  # Register the Anthropic cloud endpoint
  /route add claude anthropic:// claude-sonnet-4-20250514

  # Enable routing
  /route on

  # Send a task to the llamafile server
  @lf summarise this function in one sentence

  # Send a task to the llama.cpp server
  @cpp write a unit test for the Parse function

  # Run a quick task on a Pi node
  @pi2 what does this error mean?

  # Dispatch a complex task to the cloud
  @claude refactor this module to use the repository pattern

  # Local model handles everything else (no @mention)
  what is the overall structure of this package?
~~~

# SLASH COMMANDS

~~~
  /route add NAME URL [MODEL]        register a remote endpoint
                                       @lf     llamafile://localhost:8080 bonsai-8b
                                       @cpp    llamacpp://localhost:8081 phi4-Q4_K_M
                                       @pi2    ollama://192.168.1.12:11434 llama3.1:8b
                                       @claude anthropic:// claude-sonnet-4-20250514
  /route remove NAME                 unregister an endpoint (alias: rm)
  /route rm NAME                     alias for remove
  /route use [NAME]                  set NAME as the sticky route so all prompts
                                       dispatch there without an @mention; omit NAME
                                       to clear the sticky route
  /route models URL                  list models available at a provider URL
                                       useful before /route add to choose a model
  /route probe NAME                  show reachability, model, and tool-call capability
                                       for a registered endpoint
  /route set NAME tools on|off       toggle tool calling for a registered endpoint
                                       (only for providers that support tool use)
  /route list                        show all endpoints with reachability status
  /route on                          enable @mention dispatch (persisted)
  /route off                         disable @mention dispatch (persisted)
  /route status                      show routing state and endpoint count
~~~

Registered endpoints and the on/off state persist across sessions in
`+"`"+`<workspace>/agents/routes.json.`+"`"+`

# SEE ALSO

  /model use NAME        — backend-agnostic model switching
  /help model            — list, start, and switch local models
  /help security         — API key filtering for cloud endpoints

`

	// ─── Workspace & file operations ────────────────────────────────────────────

	// FilesHelpText is shown by /help files and harvey --help files.
	// Generates harvey-files.7.md.
	FilesHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

FILES — list workspace directory contents

# SYNOPSIS

/files [PATH]

# DESCRIPTION

/files lists the contents of a directory inside the workspace. Directories
are shown with a trailing "/". Hidden entries (names beginning with ".") are
not suppressed — all entries returned by the OS are shown.

PATH is relative to the workspace root. When omitted, the workspace root
itself is listed.

/files does not recurse. Use /file-tree to display a recursive tree, or
/read-dir to read all files in a directory into context.

Harvey will not list directories outside the workspace root.

# EXAMPLES

List the workspace root:

~~~
  harvey > /files
~~~

List a subdirectory:

~~~
  harvey > /files src/
  harvey > /files docs
~~~

# SEE ALSO

  /file-tree [PATH]   — recursive directory tree display
  /read-dir [PATH]    — read all files in a directory into context
  /read FILE...       — read specific files into context
  /help file-tree
  /help read-dir

`

	// FileTreeHelpText is shown by /help file-tree and harvey --help file-tree.
	// Generates harvey-file-tree.7.md.
	FileTreeHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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

`

	// ReadHelpText is shown by /help read and harvey --help read.
	// Generates harvey-read.7.md.
	ReadHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

READ — inject file contents into the conversation context

# SYNOPSIS

/read FILE [FILE...]

# DESCRIPTION

/read loads one or more workspace files and injects their contents into the
conversation as a user-role message. The model sees the file contents in the
very next turn and can answer questions about them, suggest edits, or use
them as reference material.

Multiple files are concatenated with a blank line between each, all in a
single injected message. Each file's content is preceded by a header showing
its workspace-relative path.

FILE is a path relative to the workspace root. Absolute paths outside the
workspace are rejected. Symlinks are not followed. Sensitive files
(e.g. .env, id_rsa, harvey.yaml) are blocked regardless of permissions.

The agents/ directory is off-limits to /read to prevent skills and
configuration from being inadvertently exposed.

PDF support: when FILE has a .pdf extension, /read automatically extracts
the text using the poppler utilities (pdfinfo, pdftotext, pdfimages) and
injects the result as plain text — no binary data is sent to the model.
Use /read-pdf FILE PAGES to extract a specific page range from a large PDF.

Context window impact: reading large files can quickly consume the model's
context window. Check /status after reading to see the token impact.

# EXAMPLES

Read a single file:

~~~
  harvey > /read src/main.go
~~~

Read several files at once:

~~~
  harvey > /read README.md docs/ARCHITECTURE.md
~~~

Read then ask a question:

~~~
  harvey > /read harvey.go
  harvey > What does the ragAugment function do?
~~~

# SEE ALSO

  /read-dir [PATH]        — read all files in a directory
  /read-pdf FILE [PAGES]  — extract and inject text from a PDF file
  /attach FILE            — attach an image, PDF, or text file (auto-detects format)
  /files [PATH]           — list directory contents
  /status                 — check remaining context window space
  /help read-dir
  /help read-pdf
  /help attach

`

	// ReadDirHelpText is shown by /help read-dir and harvey --help read-dir.
	// Generates harvey-read-dir.7.md.
	ReadDirHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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

  /read FILE...           — load specific files into context
  /attach FILE            — attach an image, PDF, or text file (auto-detects format)
  /file-tree              — display directory structure without loading files
  /search                 — search for a pattern across workspace files

`

	// ReadPDFHelpText is shown by /help read-pdf and harvey --help read-pdf.
	// Generates harvey-read-pdf.7.md.
	ReadPDFHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

READ-PDF — extract text from a PDF and inject it into the conversation context

# SYNOPSIS

/read-pdf FILE [PAGES]

# DESCRIPTION

/read-pdf extracts text from a PDF file using the poppler utilities (pdfinfo,
pdftotext, pdfimages) and injects the result into the conversation as a
user-role context message. The model can then reason about the content
immediately.

FILE may be an absolute path, a path relative to the workspace root, or a
home-relative path beginning with ~/. Unlike /read, /read-pdf is not
restricted to workspace files.

PAGES is an optional page range in the form FIRST-LAST (e.g. 40-55) or a
single page number (e.g. 10). When omitted, the entire document is extracted.
A hard limit of 20 pages per call applies to keep the context window
manageable; specify a range if the document is larger.

Three poppler tools are used in sequence:

  pdfinfo    — document metadata (title, author, page count, creation date)
  pdftotext  — text extraction preserving spatial layout (-layout flag)
  pdfimages  — raster-image inventory used to detect diagram-only pages

Pages that have sparse text and no raster images are flagged as likely
vector-diagram pages. Those pages cannot be extracted by any text tool; the
output will note them so you can follow up with a vision-capable model.

The injected content is ephemeral — it is added to the current conversation
and is not written to disk or stored in the RAG database. Use /rag ingest
if you want to index a PDF for retrieval.

# EXAMPLES

Read the first ten pages of a PDF:

~~~
  harvey > /read-pdf ~/docs/oberon2.pdf 1-10
~~~

Read a specific section by page range:

~~~
  harvey > /read-pdf ~/docs/oberon2.pdf 49-63
  harvey > Summarise the module system described in those pages.
~~~

Read a short PDF (≤ 20 pages) in full:

~~~
  harvey > /read-pdf project/spec.pdf
~~~

# SEE ALSO

  /read FILE...       — inject plain-text workspace files into context
  /rag ingest PATH    — index a file into the RAG store for retrieval
  /status             — check remaining context window space
  /help read
  /help rag

`

	// AttachHelpText is shown by /help attach and harvey --help attach.
	// Generates harvey-attach.7.md.
	AttachHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

ATTACH — attach a file to the conversation in the most useful form

# SYNOPSIS

/attach FILE

# DESCRIPTION

/attach reads FILE and injects it into the conversation as a user-role
message, choosing the representation that best matches what the active
route can accept:

  Image files (JPEG, PNG, GIF, WebP)
    If the active route reports vision capability, the image is encoded
    as a base64 data-URL ContentPart and sent natively — the model sees
    the actual pixels. If the route has no vision capability, a text
    description (filename, MIME type, file size) is injected instead so
    the turn still records that an attachment was attempted, and a tip is
    printed suggesting an @mention route for vision.

  PDF files
    Text is extracted using the poppler utilities (pdfinfo, pdftotext,
    pdfimages) and injected into the conversation. A 20-page cap applies
    to keep context window usage reasonable; specify /read-pdf FILE PAGES
    for a specific range. Diagram-only pages are flagged as incomplete.

  Text and source-code files (≤ 256 KB)
    The file content is injected as plain text, identical to /read.
    Files larger than 256 KB are rejected; use /rag ingest for large
    text corpora.

  Binary files
    Rejected with a clear error. Use an appropriate converter first.

FILE may be an absolute path, a home-relative path (~/...), or a path
relative to the current working directory. Unlike /read, /attach is not
restricted to the workspace.

# EXAMPLES

Attach an image to the next turn on a vision-capable route:

~~~
  harvey > /route add claude https://api.anthropic.com claude-opus-4-7
  harvey > /attach ~/screenshots/error.png
  harvey > @claude What does this error message say?
~~~

Attach a PDF for the model to reason about:

~~~
  harvey > /attach ~/docs/spec.pdf
  harvey > Summarise the module system described in this document.
~~~

Attach a local source file:

~~~
  harvey > /attach src/main.go
  harvey > What does the main function do?
~~~

# SEE ALSO

  /read FILE...       — inject workspace text files (workspace-scoped)
  /read-pdf FILE PAGES — inject a specific page range from a PDF
  /rag ingest PATH    — index a file into the RAG store for retrieval
  /route              — manage named remote endpoints
  /help read-pdf
  /help rag

`

	// WorkspaceHelpText is shown by /help workspace and harvey --help workspace.
	// Generates harvey-workspace.7.md.
	WorkspaceHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

WORKSPACE — workspace settings and cross-workspace alias seeding

# SYNOPSIS

/workspace <status|init [PATH]>

harvey init <source>

# DESCRIPTION

The /workspace command shows workspace state and seeds model aliases from
another workspace or YAML file. It complements /model alias by handling
bulk import of an alias registry from an existing project.

# SUBCOMMANDS

  /workspace status
    Show the workspace root path, number of defined model aliases, the name
    of the active workspace profile (if any), and whether
    memory.inject_on_start is on or off.

  /workspace init [PATH]
    Copy model aliases from the workspace directory or YAML file at PATH into
    this workspace. Only aliases that do not already exist here are imported —
    gap-fill only; existing aliases are never overwritten. Changes are saved
    immediately to agents/harvey.yaml.

    PATH may be:
      A workspace directory — reads agents/harvey.yaml inside it.
      A .yaml or .yml file  — reads model_aliases: from that file directly.

    With no PATH, /workspace init prints a status summary and a tip.

# CLI EQUIVALENT

  harvey init <source>

  Runs the same import logic and exits without starting the REPL. Use this
  to bootstrap a new project from an existing one.

# EXAMPLES

~~~
  harvey > /workspace status
  harvey > /workspace init /other/project
  harvey > /workspace init ~/shared-aliases.yaml
~~~

  $ harvey init /other/project    # CLI form, exits after import

# SEE ALSO

  /help model-alias   — define and tag model aliases
  /help model         — unified model management
  /help memory        — workspace profile injection

`

	// WriteHelpText is shown by /help write and harvey --help write.
	// Generates harvey-write.7.md.
	WriteHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

WRITE — save the last assistant reply to a file

# SYNOPSIS

/write PATH

# DESCRIPTION

/write extracts content from the most recent assistant message and writes it
to PATH inside the workspace.

Content extraction follows this rule: if the reply contains a fenced code
block (~~~ ... ~~~), the contents of the first such block are written.
Otherwise the full reply text is written. This means you can ask the model
to produce a file, inspect the reply, and then /write it without needing to
copy and paste.

PATH is relative to the workspace root. Parent directories must already
exist — /write will not create them. The file is created or overwritten.
Symlinks are not followed. Workspace permissions are checked before writing.

# EXAMPLES

Ask the model to write a Go function and save it:

~~~
  harvey > Write a Go function that parses ISO 8601 dates.
  harvey > /write src/dateparse.go
~~~

Save a full reply (no code block) as a markdown file:

~~~
  harvey > Summarize the three main design decisions in this codebase.
  harvey > /write docs/design-summary.md
~~~

# SEE ALSO

  /read FILE...     — inject file contents into context
  /run COMMAND      — run a shell command after writing
  /help read

`

	// FormatHelpText is shown by /help format and harvey --help format.
	// Generates harvey-format.7.md.
	FormatHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

FORMAT — format source files in-place using language-specific tools

# SYNOPSIS

/format FILE [FILE...]

# DESCRIPTION

/format reads each FILE from the workspace, detects its language from the
file extension, runs the registered formatter for that language, and writes
the result back to FILE if formatting changed it.

Each file is processed independently and reported on its own line, so a
single /format call can format files in several languages at once.

# SUPPORTED LANGUAGES

~~~
  Language    Extensions               Formatter
  ─────────── ──────────────────────── ─────────────────────────────
  Go          .go .mod .sum            gofmt
  C           .c .h                    clang-format
  C++         .cpp .cc .cxx .hpp .hh   clang-format
  Python      .py                      black
  Rust        .rs                      rustfmt
  JavaScript  .js                      prettier
  TypeScript  .ts                      prettier
  Pascal      .pas .p                  built-in normaliser
  Oberon      .obn (.Mod)              built-in normaliser
  BASIC       .bas .bi                 built-in normaliser
~~~

External formatters (gofmt, clang-format, black, rustfmt, prettier) read the
file's content on stdin and write the formatted result on stdout — Harvey
only overwrites the file on disk after the formatter succeeds. If the tool is
not installed, /format reports the file as already formatted rather than
failing.

The Pascal, Oberon, and BASIC formatters are built into Harvey and only
normalise whitespace and line endings; no external tool is required.

# OUTPUT

For each FILE, /format prints one of:

  FILE: formatted (N → M bytes)
    The formatter changed the file; it was rewritten with the new size.

  FILE: already formatted
    The formatter ran but produced identical output, or the formatter's
    external tool is not installed.

  FILE: no language registered for extension ".ext"
    The file extension is not recognised by Harvey's language registry.

  FILE: no formatter registered for "LANG"
    The language is recognised but has no formatter configured.

  FILE: read error: ...
  FILE: path error: ...
  FILE: write error: ...
    A filesystem error occurred; the file is left unchanged.

# SAFE MODE

A formatter that rewrites a file in place (rather than via stdin/stdout) is
blocked while Safe Mode is on, and /format reports:

  FILE: file-mode formatter requires safe mode off (/safemode off)

None of the default formatters listed above use this mode, so /format
behaves the same with Safe Mode on or off. See /help security for details.

# EXAMPLES

Format a single Go file:

~~~
  harvey > /format harvey/spinner.go
    harvey/spinner.go: formatted (4312 → 4298 bytes)
~~~

Format several files across languages in one call:

~~~
  harvey > /format cmd/harvey/main.go agents/scripts/build.py
    cmd/harvey/main.go: already formatted
    agents/scripts/build.py: formatted (1820 → 1795 bytes)
~~~

# SEE ALSO

  /write PATH       — save the last assistant reply to a file
  /run COMMAND      — run a shell command (e.g. a formatter not yet wired up)
  /help security    — Safe Mode and file-mode formatter restrictions

`

	// SearchHelpText is shown by /help search and harvey --help search.
	// Generates harvey-search.7.md.
	SearchHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SEARCH — regex search across workspace files

# SYNOPSIS

/search PATTERN [PATH]

# DESCRIPTION

/search walks the workspace (or a subdirectory) and prints every line that
matches PATTERN. PATTERN is a Go regular expression.

Results are shown in the format:

  file.go:42: matching line text

Hidden directories (names beginning with ".") are skipped. Results are
capped at 200 matches to prevent flooding the context window. If the cap
is reached, a truncation notice is printed.

PATH is relative to the workspace root. When omitted, the entire workspace
is searched.

/search is useful for finding where a symbol is defined or used before
asking the model to explain or modify it. The results are printed to the
REPL but are not automatically injected into the conversation — paste the
relevant lines or use /read to load the file.

# EXAMPLES

Search for a function name:

~~~
  harvey > /search ragAugment
~~~

Search for a TODO comment in a subdirectory:

~~~
  harvey > /search "TODO|FIXME" src/
~~~

Case-insensitive search:

~~~
  harvey > /search "(?i)context.length"
~~~

# SEE ALSO

  /read FILE...    — load a file into context after finding it
  /files [PATH]    — list directory contents

`

	// GitHelpText is shown by /help git and harvey --help git.
	// Generates harvey-git.7.md.
	GitHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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

`

	// RunHelpText is shown by /help run and harvey --help run.
	// Generates harvey-run.7.md.
	RunHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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

The default run timeout is 5 minutes. Override via the security section in
agents/harvey.yaml:

~~~yaml
security:
  run_timeout: 2m
~~~

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

`

	// ─── REPL & session control ─────────────────────────────────────────────────

	// ClearHelpText is shown by /help clear and harvey --help clear.
	// Generates harvey-clear.7.md.
	ClearHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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

`

	// ContextHelpText is shown by /help context and harvey --help context.
	// Generates harvey-context.7.md.
	ContextHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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

`

	// SummarizeHelpText is shown by /help summarize and harvey --help summarize.
	// Also dispatched for the "compact" alias. Generates harvey-summarize.7.md.
	SummarizeHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SUMMARIZE — condense conversation history to free context window space

# SYNOPSIS

/summarize
/compact

# DESCRIPTION

/summarize (alias: /compact) asks the active model to produce a concise
summary of the current conversation, then replaces the entire history with:

  1. The system prompt (re-injected automatically).
  2. Any pinned context set with /context add.
  3. A single user message containing the summary.

This frees up the context window so you can continue a long session without
hitting the model's token limit or degrading response quality from a
very-full context.

The summary is generated by the same model you are currently using. Small or
instruction-poor models may produce lower-quality summaries. If the summary
is empty (e.g. the model refused or failed), history is left unchanged.

At least two non-system messages must exist in history; otherwise the
command does nothing.

/compact is an exact alias for /summarize — both names work identically.

# WORKFLOW

~~~
  harvey > /status
  Tokens: ~98 000 / 131 072 (74%)

  harvey > /summarize
  History condensed to 847 chars.

  harvey > /status
  Tokens: ~312 / 131 072 (0%)
~~~

# WHAT SURVIVES SUMMARIZE

  System prompt    — re-injected automatically.
  Pinned context   — re-injected as the first user message.
  RAG state        — unaffected.
  Recording        — the running session continues; summary is not
                     written back to the Fountain file.

# SEE ALSO

  /context add TEXT   — pin a summary you compose manually
  /clear              — discard history entirely (no summary generated)
  /status             — check context window usage before and after
  /help context
  /help clear

`

	// EditingHelpText is shown by /help editing and harvey --help editing.
	// Also dispatched for "edit" and "keys" aliases. Generates harvey-editing.7.md.
	EditingHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

EDITING — line editing and multi-line input

# SYNOPSIS

Type at the "harvey >" prompt. Use key bindings below to navigate and edit.
For multi-line input, press Ctrl+X Ctrl+E to open an external editor.

# LINE EDITING

Harvey's prompt supports readline-style single-line editing.

Navigation:

  Left / Right arrows    move cursor one character
  Home / Ctrl+A          jump to beginning of line
  End  / Ctrl+E          jump to end of line
  Up / Down arrows       cycle through prompt history

Editing:

  Backspace              delete character before cursor
  Ctrl+D                 delete character under cursor; exits on empty line
  Ctrl+K                 delete from cursor to end of line

Actions:

  Enter                  submit the prompt to the model
  Ctrl+C                 cancel current input and return to prompt

# MULTI-LINE INPUT WITH $EDITOR

Press Ctrl+X then Ctrl+E to open the current line in your preferred editor.
Harvey reads the environment variables in this order to find the editor:

  1. $EDITOR
  2. $VISUAL
  3. vi  (hard fallback)

Write or paste your multi-line text in the editor, then save and quit.
Harvey reads the file on exit and submits the full contents as your prompt.
This is the recommended approach for long prompts, pasted code, or anything
with embedded newlines.

# TIPS

  - Up/Down arrows recall previous prompts, including multi-line ones
    that were composed in $EDITOR.
  - Ctrl+C on an empty line has no effect (Harvey does not exit on ^C).
    Use /exit, /quit, or /bye to end the session.
  - If $EDITOR is not set, export it in your shell profile:
      export EDITOR=nano    # or vim, emacs, hx, micro, etc.

`

	// StatusHelpText is shown by /help status and harvey --help status.
	// Generates harvey-status.7.md.
	StatusHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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

Spinner (transient, not shown by /status)
  While a prompt is being processed, Harvey shows an animated spinner with an
  estimated completion time. During tool-call execution a second transient line
  appears: "Calling tool: <name>…" — this clears automatically when the call
  returns and is not part of /status output.

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
  /hint            — actionable suggestions for improving results
  /help ollama     — Ollama server and model management
  /help record     — session recording

`

	// HintHelpText is shown by /help hint and harvey --help hint.
	// Generates harvey-hint.7.md.
	HintHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

HINT — show actionable suggestions for improving Harvey's results

# SYNOPSIS

/hint

# DESCRIPTION

/hint inspects the three memory silos — the session-experience memory
store, the active RAG store, and the knowledge base — and prints a short
list of suggestions for things that would help Harvey give better answers
in this workspace. It takes no arguments and makes no changes; it only
reports what it finds and suggests a command to run next.

# CHECKS

Unmined sessions
  If memory is enabled and there are recorded sessions that have not yet
  been mined for experiential memories, /hint reports the count and
  suggests:

    Run: /memory mine

RAG store
  No store configured
    Suggests creating one and ingesting reference documents:

      Run: /rag new NAME   then   /rag ingest <file>
      See: /help learn

  Store configured but empty
    Suggests ingesting documents into the active store:

      Run: /rag ingest <file>   (PDF, .md, .txt, .go, .ts, ...)
      See: /help rag

  Store has chunks but RAG is off
    Suggests turning RAG on so chunks are prepended to prompts:

      Run: /rag on

  Store configured but not open
    Suggests checking its status:

      Run: /rag status

Knowledge base
  If no SQLite knowledge base is open, suggests recording experiment
  findings so they persist across sessions:

    See: /help kb

# OUTPUT

If every check passes — RAG is on with chunks, sessions are mined, and the
knowledge base is open — /hint prints a single confirmation line and
points to /help learn for the full memory overview.

# EXAMPLES

~~~
  harvey > /hint
    Sessions unmined: 3
      Harvey can extract learnings from these sessions.
      Run: /memory mine

    RAG is off but store "default" has 142 chunk(s).
      Enabling RAG prepends relevant chunks to each prompt.
      Run: /rag on
~~~

~~~
  harvey > /hint
    Everything looks good — RAG is on with chunks, sessions are mined, KB is open.
    Use /help learn for the full memory overview.
~~~

# SEE ALSO

  /help learn     — overview of all three memory silos
  /memory status  — summary of the experiential memory store
  /rag status     — summary of the active RAG store
  /kb status      — summary of the knowledge base
`

	// ─── Session management ─────────────────────────────────────────────────────

	// RecordHelpText is shown by /help record and harvey --help record.
	// Generates harvey-record.7.md.
	RecordHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

RECORD — record session exchanges to a Fountain file

# SYNOPSIS

/record start [FILE]
/record stop
/record status

# DESCRIPTION

/record saves each user prompt and assistant reply to a plain-text Fountain
.spmd file as the conversation progresses. Recording is on by default: Harvey
starts recording automatically at startup and prints "Recording to …" in the
startup banner.

Recorded files can be loaded later with /session continue (to resume) or
/session replay (to re-run against a different model).

# AUTO-RECORD

Harvey records automatically unless auto-record is disabled. The session
file is created in <workdir>/agents/sessions/ with a timestamped name:

  harvey-session-YYYYMMDD-HHMMSS.spmd

The path is shown in the startup banner. When you exit Harvey the banner
confirms the file was saved:

  Session saved to agents/sessions/harvey-session-20260501-143200.spmd

# SUBCOMMANDS

/record start [FILE]
  Begin recording to FILE. If FILE is omitted, Harvey generates a
  timestamped name in the sessions directory. Returns an error if a
  recording is already active — use /record stop first.

/record stop
  Close the current recording file. The path is printed on exit.
  Harvey continues running; only the recording ends.

/record status
  Show the path of the active recording, or report that no recording
  is in progress.

# CLI FLAGS

  harvey --record             auto-record with a generated filename
  harvey --record-file FILE   auto-record to a specific path

# SEE ALSO

  /session   — continue or replay a recorded session
  /help session

`

	// RenameHelpText is shown by /help rename and harvey --help rename.
	// Generates harvey-rename.7.md.
	RenameHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

RENAME — rename the active session recording file

# SYNOPSIS

/rename NAME

# DESCRIPTION

/rename changes the filename of the session currently being recorded without
ending the session. Recording continues to the new file seamlessly.

NAME is a bare filename — do not include a directory path. Harvey places the
renamed file in the same directory as the original (agents/sessions/ by
default). A .spmd extension is added automatically if omitted.

# EXAMPLES

Give the session a meaningful name while it is still running:

  /rename my-feature-session

This renames the current harvey-session-YYYYMMDD-HHMMSS.spmd to
my-feature-session.spmd in agents/sessions/.

  /rename rag-fix-and-context-display

# SEE ALSO

  /record   — start or stop session recording
  /session  — continue or replay a recorded session
  /help record
  /help session

`

	// SessionHelpText is shown by /help session and harvey --help session.
	// Generates harvey-session.7.md.
	SessionHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SESSION — list, inspect, continue, or replay recorded conversations

# SYNOPSIS

/session list
/session show   [FILE]
/session use    FILE
/session continue FILE
/session replay   FILE [OUTPUT]

# DESCRIPTION

Harvey saves every conversation to a Fountain .spmd file. The /session
command lets you browse those files and reload them in two distinct ways:

  continue / use — restore the conversation history and keep chatting.
  replay         — re-send the original user turns to the current model and
                   record fresh responses.

# LIST

/session list prints the recorded sessions in <workdir>/agents/sessions/,
one per line with the filename and last-modified timestamp.

# SHOW

/session show [FILE] displays metadata for FILE without loading it: date,
model used, turn count, and the opening user prompt. If FILE is omitted and
a recording is currently active, the active file is shown.

# CONTINUE / USE

/session use FILE (alias: /session continue FILE) loads all turns from FILE
into the current history and returns you to the REPL. The model sees the
full prior conversation as if it had been running the whole time.

Use continue to:

  - Resume work across Harvey restarts.
  - Switch to a different model and pick up mid-conversation.
  - Inspect and then extend a session that was auto-saved.

Harvey also offers to continue the most recently saved session at startup;
pressing Enter at that prompt is equivalent to /session use.

# REPLAY

/session replay FILE [OUTPUT] re-runs a session by sending each user turn
to the currently connected model in sequence. The model's fresh responses
are recorded to OUTPUT (default: an auto-named file in the sessions
directory).

Use replay to:

  - Run an old conversation through a new or different model for comparison.
  - Re-generate responses after changing the system prompt (HARVEY.md).
  - Benchmark how different models handle the same sequence of prompts.

Replay does not show the original assistant responses — it only shows the
new ones produced by the current model.

# SESSION FILE FORMAT

Session files use the Fountain screenplay format with a .spmd extension.
Each exchange is an INT scene with speaker labels (RSDOIEL, HARVEY, model
name). These files are plain text and human-readable.

Default save location: <workdir>/agents/sessions/
File naming:
harvey-session-YYYYMMDD-HHMMSS.spmd

# CLI FLAGS

The same operations are available as startup flags:

  harvey --resume                resume the most recent session automatically
                                 (no path needed; prints a notice if no sessions found)
  harvey --continue FILE         load history from FILE then open REPL
  harvey --replay FILE           replay without entering REPL
  harvey --replay-output FILE    write replay output to FILE

# SEE ALSO

  /record          — start or stop session recording manually
  harvey --resume  — resume the most recent session at startup (no path needed)
  harvey --help    — full CLI flag reference

`

	// ─── Knowledge & memory ─────────────────────────────────────────────────────

	// RagHelpText is shown by /help rag and harvey --help rag.
	// Generates harvey-rag.7.md.
	RagHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

RAG — Retrieval-Augmented Generation

# SYNOPSIS

/rag <list|new NAME|use NAME|show [NAME]|remove NAME|ingest PATH|status|query TEXT|on|off>

# DESCRIPTION

RAG lets Harvey find relevant snippets from a local knowledge store and
inject them into the context window before each prompt is sent to the model.
This grounds the model's replies in documents you have ingested, reducing
hallucination and allowing it to answer questions about your own codebase,
notes, and reference material without needing those files to be manually
read into context with /read.

When RAG is on, every user prompt is silently augmented with a short block
of matching text retrieved from the store. Only chunks that score above the
relevance threshold (0.3 cosine similarity) are injected; if nothing scores
high enough, the prompt is sent unmodified.

# NAMED STORES

Harvey supports multiple named RAG stores. Each store is an independent
SQLite database bound to one embedding model — you cannot mix vectors from
different embedding models in the same store, and Harvey enforces this at
every ingest and query operation.

Named stores let you maintain separate, topically focused knowledge bases
and switch between them as your work changes:

  golang        Go standard library docs, idioms, and project code
  deno          Deno/TypeScript docs and project source
  web-frontend  MDN references, CSS specs, web-component guides
  writing       Style guides, project drafts, editorial notes
  research-X    Papers and notes for a specific research topic

Only the active store is open at any time, so inactive stores consume no
memory. The active store can be changed with /rag use at any time.

On storage-constrained hardware (e.g. a Raspberry Pi), keep stores small
and topical: a focused 5 000-chunk store retrieves better than a bloated
50 000-chunk general store, and it fits in RAM.

# EMBEDDING MODEL CHOICE

RAG depends on a separate embedding model — a small neural network that
converts text to vectors. The quality of retrieval depends heavily on the
embedding model used. The models are ranked here from best to least suitable:

  nomic-embed-text        (~274 MB) best general-purpose retrieval
  mxbai-embed-large       (~670 MB) high quality, larger
  bge-small-en-v1.5        (~46 MB) small but retrieval-optimised
  bge-m3                  (~1.2 GB) multilingual
  all-minilm-l6-v2         (~46 MB) avoid — similarity-tuned, not retrieval-tuned

The critical distinction: models like all-MiniLM were trained on
sentence-similarity tasks (NLI, STS), not on document retrieval. On standard
retrieval benchmarks (MTEB), all-MiniLM-L6-v2 scores around 56% while
bge-small-en-v1.5 scores around 62% and nomic-embed-text around 68%. Use a
retrieval-optimised model whenever possible.

The /rag new wizard detects which embedding models are installed and
proposes the best available one, preferring nomic-embed-text > mxbai-embed-large
> bge- > all-minilm. If none are installed, it prints a list of recommended
models you can pull with /ollama pull.

Each store is bound to one embedding model at creation time. If you want to
try a different embedding model for the same topic, create a new store and
re-ingest the documents.

# WORKFLOW — FIRST STORE

~~~
  # Step 1 — choose an embedding model (one-time)
  /ollama pull nomic-embed-text

  # Step 2 — create and name a store
  /rag new golang

  # Step 3 — ingest the documents you want Harvey to retrieve from
  /rag ingest agents/
  /rag ingest HARVEY.md
  /rag ingest docs/

  # Step 4 — verify retrieval quality before trusting answers
  /rag query what license does Harvey use?
  /rag query how does routing work?

  # RAG is now on automatically — ask questions normally
  what license does Harvey use?
~~~

# WORKFLOW — MULTIPLE STORES

~~~
  # Create a writing store alongside the golang store
  /rag new writing

  # Ingest style guides and project drafts
  /rag ingest ~/writing/style-guide.md
  /rag ingest ~/projects/novel/drafts/

  # Switch back to golang when you return to code
  /rag use golang

  # See all registered stores
  /rag list
~~~

# DIAGNOSING POOR RETRIEVAL

Use /rag query to inspect what the store would return for a given question
before sending it to the model. The output shows each chunk with its cosine
similarity score (0.0–1.0) and source file:

~~~
  /rag query what license does Harvey use?

  Top 5 result(s) for "what license does Harvey use?":

    [1] score=0.712  source=/home/user/Laboratory/harvey/LICENSE
        GNU AFFERO GENERAL PUBLIC LICENSE...

    [2] score=0.431  source=/home/user/Laboratory/harvey/README.md
        Harvey is licensed under AGPL-3.0...
~~~

Scores below 0.3 are dropped from the injected context. If the top scores
are all low (< 0.3) for a question you expect the store to answer, consider:

  1. Switching to a better embedding model (see Embedding Model Choice above)
     then creating a new store and re-ingesting.
  2. Ingesting the missing documents with /rag ingest PATH.
  3. Rephrasing the question to be closer to the language used in the docs.

# SUBCOMMANDS

/rag list
  List all registered stores with a * marking the active one.

/rag new NAME
  Interactive wizard to create a named store. Detects installed embedding
  models, proposes the best one, shows the proposed generation-model →
  embedding-model mapping, and asks for confirmation. Creates the database
  at agents/rag/NAME.db and saves the config to agents/harvey.yaml.
  The new store is immediately set as active.

/rag use NAME
  Close the currently open store and activate NAME. The change is persisted
  to agents/harvey.yaml.

/rag show [NAME]
  Show details for the named (or active) store: database path, embedding
  model, chunk count, and model map. If NAME is omitted, the active store
  is shown.

/rag remove NAME
  Remove a store from the registry after confirmation. The .db file is NOT
  deleted automatically — the path is printed so you can remove it manually.
  Alias: drop.

/rag drop NAME
  Alias for remove.

/rag ingest PATH [PATH...]
  Reads each file or directory (recursively), splits text into
  paragraph-sized chunks (~500 characters each), embeds them using the
  active store's embedding model, and stores the vectors in the database.
  Re-ingest any file after it changes to keep the store current.

  Remote URIs are supported alongside local paths:
    s3://bucket/prefix/     — list and ingest all ingestable objects (requires
                              AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY)
    sftp://[user@]host/path/ — list and ingest via SFTP over SSH (reads
                              SFTP_PASSWORD or SFTP_KEY_PATH from environment)
    scp://[user@]host/path/  — alias for sftp://
    http://host/file.ext    — download and ingest a single file
    https://host/file.ext   — same, with TLS (optional HTTP_BEARER_TOKEN or
                              HTTP_BASIC_USER / HTTP_BASIC_PASSWORD)

  Supported formats:
    Plain text — .md, .txt, .go, .ts, .py, .yaml, .toml, .sql, and other
                 text files.
    PDF        — .pdf files are extracted with the poppler utilities
                 (pdfinfo, pdftotext, pdfimages; must be installed). Each
                 page is chunked separately, and every chunk is prefixed
                 with the document title and page number so retrieval
                 results always carry their source. Pages that contain
                 only vector graphics (no extractable text) are stored
                 with an incomplete-content marker so the model knows to
                 ask for a vision-capable route for those pages.

/rag query TEXT
  Retrieves the top 5 matching chunks for TEXT from the active store and
  prints each one with its cosine similarity score and source path.

/rag status
  Shows the active store (database path, embedding model, chunk count) and
  a summary list of all registered stores.

/rag on
  Enable automatic context injection for the current session.

/rag off
  Disable automatic context injection for the current session.
  The database and configuration are preserved; /rag on re-enables it.

# CONFIGURATION

RAG configuration is persisted in agents/harvey.yaml. Example with two stores:

~~~yaml
  rag:
    enabled: true
    active: golang
    stores:
      - name: golang
        db_path: agents/rag/golang.db
        embedding_model: nomic-embed-text
        model_map:
          llama3.1:latest: nomic-embed-text
          granite3.3:2b:   nomic-embed-text
      - name: writing
        db_path: agents/rag/writing.db
        embedding_model: nomic-embed-text
~~~

Each store has its own db_path and embedding_model. The model_map lets
different generation models share the same embedding model; entries are
populated automatically by /rag new.

Old single-store configurations (db_path at the top level of rag:) are
automatically migrated to a store named "default" on first load.

# SEE ALSO

  /memory recall         — search all knowledge silos including RAG
  /help learn            — overview of all three memory silos
  /help kb               — structured knowledge base for experiments

`

	// KBHelpText is shown by /help kb and harvey --help kb.
	// Also dispatched for "knowledge" and "knowledge-base" aliases. Generates harvey-kb.7.md.
	KBHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

KB — knowledge base management

# SYNOPSIS

/kb [status]
/kb search TERM [TERM...]
/kb inject [PROJECT]
/kb project <list|add NAME [DESC]|use ID>
/kb observe [KIND] TEXT
/kb concept <list|add NAME [DESC]>

# DESCRIPTION

Harvey keeps a SQLite knowledge base at <workdir>/agents/knowledge.db.
It stores structured notes about experiments and concepts so you can
search and inject that context into conversations without relying on
the model's general knowledge.

The knowledge base is independent of the RAG store (/help rag). KB holds
hand-authored structured records; RAG holds embedded chunks from ingested
documents. Use both: /kb inject to bring structured records into context,
and RAG to retrieve relevant document passages automatically.

# CONCEPTS

  Project     — a named container for a body of work. One project can be
                "active" at a time; /kb observe attaches to the active project.

  Observation — a timestamped note attached to a project. Each observation
                has a kind:

                  note        — general remark
                  finding     — empirical result
                  decision    — a choice made and its rationale
                  question    — open question to return to
                  hypothesis  — testable prediction

  Concept     — a named idea or term that can be referenced across multiple
                projects and observations.

# SUBCOMMANDS

/kb status
  Show the database path, project count, and observation count.

/kb search TERM [TERM...]
  Full-text search (FTS5) across all observations and concepts. Supports
  quoted phrases and prefix wildcards:

~~~
  /kb search RAG embedding
  /kb search "context window"
  /kb search grpc*
~~~

/kb inject [PROJECT]
  Format the knowledge base as Markdown and add it to the conversation
  as a user message. With no argument, injects the active project (or all
  projects if none is active). With a project name, injects only that project.

~~~
  /kb inject
  /kb inject harvey
~~~

/kb project list
  List all projects with ID, name, and status. The active project is
  marked with *.

/kb project add NAME [DESCRIPTION]
  Create a project and set it as the active project.

~~~
  /kb project add harvey "terminal coding agent for Ollama"
~~~

/kb project use ID
  Set an existing project as the active project by numeric ID.

/kb observe [KIND] TEXT
  Record an observation against the active project. KIND defaults to
  "note" if omitted. Valid kinds: note, finding, decision, question,
  hypothesis.

~~~
  /kb observe finding RAG threshold of 0.3 eliminates noise on granite3-moe
  /kb observe decision switched embedding model to nomic-embed-text
  /kb observe question does bge-m3 outperform nomic on code retrieval?
~~~

/kb concept list
  List all concepts with ID and description.

/kb concept add NAME [DESCRIPTION]
  Add a named concept to the knowledge base.

~~~
  /kb concept add RAG "retrieval-augmented generation"
  /kb concept add "context window" "token budget for a single LLM call"
~~~

# WORKFLOW EXAMPLE

~~~
  /kb project add myapp "Go CLI for processing audio files"
  /kb observe decision using ffmpeg via exec.Command, not a Go binding
  /kb observe finding ffmpeg probe takes ~80 ms per file on Pi 4
  /kb observe question can we batch probe calls to reduce overhead?
  /kb concept add ffmpeg "audio/video processing CLI"
  /kb inject
~~~

After /kb inject the model sees the full project record as context and can
answer questions about it, suggest next steps, or help resolve open questions.

# SEE ALSO

  /memory recall         — search all knowledge silos including the KB
  /rag ingest            — embed documents for semantic retrieval
  /help learn            — overview of all three memory silos

`

	// MemoryHelpText is shown by /help memory and harvey --help memory.
	// Also dispatched for "profile" and "recall" aliases. Generates harvey-memory.7.md.
	MemoryHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

MEMORY — mine session recordings for memories and manage the memory store

# SYNOPSIS

/memory <mine|list|show|flag|forget|status|recall|profile> [args...]

# DESCRIPTION

/memory provides a semi-manual system for extracting useful knowledge from
Harvey's Fountain session recordings (.spmd files) and injecting
that knowledge into future sessions. Memories persist across sessions as
Fountain files in agents/memories/ inside the workspace.

# SUBCOMMANDS

  mine [FILE] [--force]
        Scan unmined session files for memories. The LLM proposes memories
        via one-shot JSON extraction; you review each interactively
        (accept / edit / replace / skip / quit). Use --force to re-mine
        sessions that have already been processed.

  list [--type TYPE] [--kind KIND]
        List stored memories. Optional --type filters by memory type:
        tool_use, workflow, user_preference, workspace_profile, project_fact.
        Optional --kind filters by enrichment kind:
        pitfall, workaround, recommendation, pattern.
        Output shows type, kind, and confidence for each memory.

  show ID
        Display the full Fountain source for one memory by its ID slug.

  flag ID
        Reduce a memory's confidence by 0.1. When confidence falls to or
        below 0.2 the memory is automatically archived. Use this to signal
        that a memory has turned out to be wrong or outdated without
        permanently deleting it.

  forget ID
        Archive a memory immediately (moves it to agents/memories/archive/
        — not deleted).

  status
        Show memory store location, total count, and breakdown by type.

  recall QUERY
        Query all memory silos (workspace profile, project facts, experiential
        memories, RAG chunks, and KB observations) and print grouped results.
        Uses FTS5 full-text search plus cosine similarity when a RAG store is
        configured. No token budget is applied — all matching results are shown.

  profile show|update|use [name]
        Manage the workspace profile.
        "list"   — list active and archived profiles (IDs + descriptions).
        "show"   — print the full content of the active profile document.
        "edit"   — open the active profile in $EDITOR and re-save on close.
        "use"    — switch to a new profile: writes a handoff document to
                   agents/hand-off/, archives the current profile, selects a
                   template (by name or interactive picker), saves it as the
                   new profile, and resets history so the new context injects
                   on the next turn. Alias: /profile use [name].
        "rename" — rename the workspace in the active profile document.
        "update" — deprecated alias for "edit".

# MEMORY TYPES

  tool_use          A tool or command trick that worked (e.g. a useful flag,
                    a workaround for a known bug).
  workflow          A repeatable multi-step process (e.g. how to publish a release).
  user_preference   A stated or demonstrated preference (e.g. preferred coding style).
  workspace_profile Factual description of the workspace: what it is, its purpose,
                    its primary language and tools. Always injected first.
  project_fact      A key fact about the current project: deadlines, conventions,
                    constraints. Always injected second.

# MEMORY ENRICHMENT FIELDS

Each memory carries three enrichment fields set at mining time:

  kind          Why this knowledge matters. One of:
                  pitfall        — a mistake to avoid
                  workaround     — a fix for a known limitation
                  recommendation — a practice that consistently works well
                  pattern        — a repeatable structure or approach

  action        An imperative step a future agent should take when this
                memory is relevant. Included in the embedding text so
                semantic search retrieves it for related prompts.

  confidence    A score from 0.0 to 1.0 (default 0.5 at mining time).
                Retrieval scores are weighted multiplicatively:
                  final_score = cosine_similarity × confidence
                Use /memory flag ID to lower confidence when a memory
                proves wrong. Memories at or below 0.2 are auto-archived.

# MEMORY INJECTION

When a session starts, Harvey injects a [memory context] block into the
conversation. Factual types (workspace_profile, project_fact) always appear
first. Experiential memories (tool_use, workflow, user_preference) are ranked
by FTS5 full-text search and optionally cosine similarity weighted by the
memory's confidence score. RAG chunks and KB observations follow if token
budget permits.

The budget is controlled by memory.budget_pct in harvey.yaml (default 0.25 of
the context window). Memory injection is OFF by default; enable it with
memory.inject_on_start: true in harvey.yaml.

# DIGEST

Harvey automatically writes agents/memories/DIGEST.md every time a memory
is saved, archived, or auto-mined. The digest is a plain Markdown index of
all active memories — readable by any LLM without a SQLite client.

Other agents (Claude Code, Vibe, etc.) can use this digest via the
agents/skills/harvey-memory/SKILL.md cross-agent skill, which explains when
and how to consult it. See /help skills for skill loading.

# ROLLING SUMMARY

When a session grows long, Harvey automatically compresses older turns once the
history token count reaches memory.rolling_summary.warn_at_pct of the context
window (default 80%). Harvey prints:

  [context ~82% full — compressing older turns]

then asks the current model to produce a 150-token summary of the older turns.
That summary replaces the older history; the last memory.rolling_summary.keep_turns
turns (default 6) are preserved verbatim. The session recording on disk retains
the full pre-compression history.

  rolling_summary.enabled     — true (default) / false to disable
  rolling_summary.warn_at_pct — fraction of context window that triggers
                                 compression (default 0.80)
  rolling_summary.keep_turns  — turns kept verbatim after compression (default 6)

# PRIVACY

Workspace paths are normalised to <workspace> before review. Credential
patterns (password, token, Bearer, api_key, -----BEGIN, etc.) are flagged
for human review but never auto-redacted. A scrub pass runs on every proposed
memory before the review card is displayed.

# EXAMPLES

  /memory mine
  /memory mine agents/sessions/harvey-session-20260525-140251.spmd
  /memory list --type workflow
  /memory list --type workspace_profile
  /memory list --kind pitfall
  /memory list --type tool_use --kind workaround
  /memory show pipeline_confidence_extraction
  /memory flag old_pattern_a1b2c3
  /memory forget old_pattern_a1b2c3
  /memory status
  /memory recall git repository error
  /memory profile list
  /memory profile show
  /memory profile edit
  /memory profile use web-developer
  /memory profile rename "Harvey Web Developer"

# SEE ALSO

  /profile               — alias for /memory profile
  /recall QUERY          — alias for /memory recall
  /help learn            — overview of all three memory silos
  /help rag              — RAG store management
  /help kb               — knowledge base management

`

	// LearnHelpText is shown by /help learn and harvey --help learn.
	// Generates harvey-learn.7.md.
	LearnHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

LEARN — how Harvey accumulates and retrieves knowledge

# DESCRIPTION

Harvey stores knowledge in three independent silos that are unified at
retrieval time. Understanding which silo to use for which kind of content
is the key to getting consistently good results.


# THE ONE DECISION RULE

  Have a text file or document?        →  /rag ingest <file>
  Something useful happened in a session? →  /memory mine
  Making an observation about an experiment? →  /kb observe


# THE THREE SILOS

  Silo            What belongs here          How to add       How it arrives
  ─────────────── ─────────────────────────  ───────────────  ─────────────────────
  RAG store       Reference docs, API specs, /rag ingest      Per-prompt via
                  code examples, PDF papers  <file>           /rag on (context)

  Memory store    Patterns observed during   /memory mine     Session-start via
                  sessions: what worked,     (interactive)    /memory recall
                  what the model got wrong,  auto-mines on
                  user preferences           session exit

  Knowledge base  Research notes, named      /kb observe      On-demand via
                  experiments, cross-project /kb project      /memory recall
                  concepts and hypotheses    /kb concept

Retrieval from all three silos is unified:

  /memory recall <query>   — search all three silos, print ranked results
  /recall <query>          — alias for /memory recall

  /profile <list|show|edit|use|rename> [args...]
                           — alias for /memory profile (manage workspace profile)
  /profile list            — list active and archived profiles
  /profile show            — print full content of the active profile
  /profile edit            — open active profile in $EDITOR
  /profile use [name]      — switch profile: saves handoff, archives old profile,
                             selects new template, resets history
  /profile rename NAME     — rename the workspace in the active profile


# CHECKING WHAT YOU HAVE

  /status          — shows active memories, unmined sessions, RAG chunk count
  /hint            — prints actionable suggestions for improving results
  /memory status   — detailed memory store stats and budget advice
  /rag status      — RAG store details: active store, chunk count, on/off


# COMMON WORKFLOWS

Ingest a PDF reference before starting a coding session:

  /rag ingest Reference/papers/oberon2.pdf
  /rag on

Mine learnings from last session before starting the next:

  /memory mine

Record an observation about a running experiment:

  /kb observe "Qwen3.5 handles nil pointer chains correctly after explicit cast"

Check what would improve the current session:

  /hint


# SEE ALSO

  /profile        — alias for /memory profile (manage workspace profile)
  /recall QUERY   — alias for /memory recall (search all silos)
  /help rag       — full RAG command reference
  /help memory    — full memory command reference
  /help kb        — full knowledge base reference

`

	// ─── Skills ─────────────────────────────────────────────────────────────────

	// SkillsHelpText explains the Agent Skills feature and is displayed by
	// /help skills (REPL) or harvey --help skills (CLI).
	SkillsHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SKILLS

# SYNOPSIS

Skills allow agents to carry out uniform structured tasks. The SKILL.md file
is a standard proposed by Anthropic.

# DESCRIPTION

Skills are Markdown files that inject specialised instructions into Harvey's
context on demand. Harvey scans for skills at startup but only loads a
skill's full instructions when you explicitly ask for it. SKILL.md
is documented at <https://agentskills.io/home>.


# HOW SKILLS WORK

  1. Discovery — Harvey scans the standard paths below and builds a catalog
     of (name, description) pairs. The catalog is added to the system prompt
     so the model knows what skills are available.

  2. Activation — type /skill load <name> to inject the full skill body into
     the conversation. The model then follows the skill's instructions for
     the current task.

  3. Resources — a skill directory may also contain scripts/, references/,
     and assets/ subdirectories. Use /read to bring any of those files into
     context when the skill's instructions call for them.


# SKILL DIRECTORY STRUCTURE

~~~
  my-skill/
  ├── SKILL.md          required: metadata + instructions
  ├── scripts/          optional: runnable code
  ├── references/       optional: extra documentation
  └── assets/           optional: templates, data files
~~~


# SKILL.md FORMAT

~~~markdown
  ---
  name: my-skill
  description: One or two sentences on what this skill does and when to use it.
  license: Apache-2.0          (optional)
  compatibility: Requires git  (optional)
  trigger: pdf extract         (optional: keyword list or /regexp/)
  metadata:                    (optional)
    author: you
    version: "1.0"
  ---

  # My Skill

  Instructions in plain Markdown. The model reads this entire block
  when the skill is activated.

~~~

Required frontmatter fields: name, description.
The name must be lowercase letters, numbers, and hyphens only, and must
match the parent directory name.


# EXAMPLE — the bundled go-review skill

  Location: agents/skills/go-review/SKILL.md

~~~markdown
  ---
  name: go-review
  description: Review Go source code for correctness, style, and idiomatic
    patterns. Use when the user asks to review, audit, or critique Go code,
    or when checking a Go file before committing.
  license: AGPL-3.0
  compatibility: Designed for Harvey (or any agent working in a Go codebase)
  metadata:
    author: you
    version: "1.0"
  ---

  Activate it with:   /skill load go-review
  Then ask Harvey:    Please review cmd/harvey/main.go
~~~

# COMPILED SKILLS

A compiled skill has executable scripts (compiled.bash for Linux/macOS/BSD,
compiled.ps1 for Windows) in the skill's scripts/ directory. When a compiled
skill is invoked, Harvey runs the script directly — no LLM round-trip needed —
and injects the output into context.

Compiling a skill requires a large capable model (e.g. Claude or Mistral) that
is not typically available on resource-constrained hardware. Compile skills on
a capable system and commit the resulting scripts alongside SKILL.md.

Compiled skill directory layout:

~~~
  my-skill/
  ├── SKILL.md
  └── scripts/
      ├── compiled.bash
      └── compiled.ps1
~~~

HARVEY_* environment variables set before each script run:

  HARVEY_PROMPT      the user's exact prompt text
  HARVEY_WORKDIR     absolute path to the workspace root
  HARVEY_MODEL       the name of the currently active LLM model
  HARVEY_SESSION_ID  the current session ID as a string
  HARVEY_API_BASE    base URL of the currently active LLM backend
                     (e.g. http://localhost:11434 for Ollama,
                     https://api.anthropic.com for Anthropic)
                     Use this to make LLM API calls from a compiled
                     script without hard-coding a provider URL.

Staleness: if SKILL.md is modified after the scripts were compiled, Harvey
warns you when the skill is invoked and runs the old compiled version.
Recompile the skill on a capable system to pick up the changes.

Dispatch fallback: if a compiled skill's script cannot be executed (e.g.
the script was compiled on a different OS or was not committed), Harvey
falls back to the LLM context-injection path — the skill body is injected
into the conversation and the model is asked to respond — instead of
erroring out. The fallback triggers a model response turn so the session
continues without interruption.

TRIGGER field: add an optional trigger field to SKILL.md frontmatter to enable
automatic skill dispatch when user input matches:

  trigger: pdf extract document   (keyword mode — any word triggers)
  trigger: /\bpdf\b/              (regexp mode — wrap pattern in slashes)

When Harvey receives a user prompt matching a trigger, it invokes the compiled
skill directly instead of sending the prompt to the LLM. First alphabetically
matching trigger wins.


## DISCOVERY PATHS  (project overrides user on name collision)

~~~
  Project scope
    <workspace>/agents/skills/           Harvey-native (and shared clients)
~~~

Skills placed in agents/skills/ are visible to any agent
that follows the Agent Skills specification (https://agentskills.io/home).


# SLASH COMMANDS

~~~
  /skill                   list all discovered skills
  /skill list              same as above
  /skill load NAME         inject the full skill body into context
  /skill show NAME         show path, compatibility, and license (alias: info)
  /skill info NAME         alias for show
  /skill status            count skills by scope
  /skill new               interactive wizard to create a new skill
  /skill run NAME          run a skill (dispatches compiled scripts if available)
~~~

# SEE ALSO

  /skill-set load NAME   — load a named bundle of skills
  /help skill-set        — skill bundle management

`

	// SkillSetHelpText is shown by /help skill-set and harvey --help skill-set.
	// Generates harvey-skill-set.7.md.
	SkillSetHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SKILL-SET — load and manage named bundles of Harvey skills

# SYNOPSIS

/skill-set <list|load NAME|show NAME|new NAME|status|unload>

# DESCRIPTION

/skill-set groups multiple skills into a named YAML bundle stored in
agents/skill-sets/. Loading a bundle injects every skill in the bundle
into the current conversation context in one step.

Skill-set YAML files live in agents/skill-sets/ and reference skills by
the name field in their SKILL.md frontmatter (e.g. "fountain-analysis").

# SUBCOMMANDS

list
  List all YAML files found in agents/skill-sets/.

load NAME
  Parse NAME.yaml, validate every skill exists in agents/skills/, count
  tokens for the combined bodies, and load each skill into context. Warns
  when combined tokens exceed 50 % of the active context window; errors
  when they exceed 100 %.

show NAME
  Show the skill-set description and the skills it contains without loading.
  Alias: info.

info NAME
  Alias for show.

new NAME
  Scaffold a new NAME.yaml in agents/skill-sets/ with placeholder content.
  Alias: create.

create NAME
  Alias for new.

status
  Show the currently loaded skill-set (if any).

unload
  Clear the active skill-set indicator. The injected context remains in
  history; use /clear if you need a clean slate.

# YAML FORMAT

  name: go-dev
  description: |
    Skills for Go development sessions.
  skills:
    - fountain-analysis
    - review-knowledge-base
  metadata:
    version: "1.0"
    author: "R. S. Doiel"

# EXAMPLES

List available bundles:

  /skill-set list

Load the fountain bundle:

  /skill-set load fountain

Show bundle contents without loading:

  /skill-set show fountain

Check what is active:

  /skill-set status

Create a new bundle:

  /skill-set new my-bundle

# SEE ALSO

  /skill load NAME — load a single skill
  /skill list      — list individual skills
  /help skills     — skills system overview

`

	// ─── Security ───────────────────────────────────────────────────────────────

	// SecurityHelpText is shown by /help security and harvey --help security.
	// Also dispatched for "safemode", "safe", "permissions", "audit" aliases.
	// Generates harvey-security.7.md.
	SecurityHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SECURITY — Safe Mode, workspace permissions, and audit logging

# SYNOPSIS

/safemode <on|off|status|allow CMD|deny CMD|reset>
/safe <on|off|status|allow CMD|deny CMD|reset>    (alias for /safemode)
/permissions <list [PATH]|set PATH PERMS|reset>
/audit <show [N]|clear|status>
/security status

# DESCRIPTION

Harvey includes four complementary security controls. All settings survive
restart when persisted via the commands below. Run /security status for a
unified view of the current security posture.

## SAFE MODE (/safemode)

Safe Mode restricts which programs the model may execute via the ! prefix
and /run. When enabled, only commands in the allowlist are permitted.

Default allowlist: ls, cat, grep, head, tail, wc, find, stat, jq, htmlq,
bat, batcat.

Alias: /safe is an exact alias for /safemode. Both names accept the same
subcommands and behave identically. Use whichever you prefer.

Subcommands:

  /safemode on   (or: /safe on)
    Enable Safe Mode. Commands not in the allowlist are blocked and
    audit-logged.

  /safemode off   (or: /safe off)
    Disable Safe Mode. All commands accepted by the shell metacharacter
    filter are permitted.

  /safemode status   (or: /safe status)
    Show whether Safe Mode is on or off and list the current allowlist.

  /safemode allow CMD   (or: /safe allow CMD)
    Add CMD to the allowlist. Persisted to agents/harvey.yaml.

  /safemode deny CMD   (or: /safe deny CMD)
    Remove CMD from the allowlist. Persisted to agents/harvey.yaml.

  /safemode reset   (or: /safe reset)
    Restore the default allowlist.

## WORKSPACE PERMISSIONS (/permissions)

Workspace permissions give fine-grained read/write/exec/delete control per
path prefix within the workspace. Permissions are persisted in
agents/harvey.yaml under the security.permissions: key.

~~~yaml
security:
  safe_mode: true
  allowed_commands: [ls, cat, grep, find, stat, jq]
  run_timeout: 5m
  permissions:
    ./:
      - read
      - write
      - exec
      - delete
~~~

Permission values: read, write, exec, delete (comma-separated).

Subcommands:

  /permissions list [PATH]
    List permissions for all prefixes, or for a specific PATH.

  /permissions set PATH PERMS
    Set permissions for PATH. PERMS is a comma-separated list of values.
    Example: /permissions set src/ read,write

  /permissions reset
    Remove all custom permissions.

## AUDIT LOG (/audit)

Harvey maintains an in-memory ring buffer of the last 1 000 events covering
command execution, file reads, file writes, file deletes, file listings,
skill runs, and security denials. The log resets when Harvey exits.

Subcommands:

  /audit show [N]
    Print the most recent N events (default 20).

  /audit clear
    Clear the in-memory audit buffer.

  /audit status
    Show the buffer size and event count.

## SECURITY OVERVIEW (/security status)

/security status prints a single unified view of: Safe Mode state and
allowlist, workspace permissions, and audit buffer status.

# EXAMPLES

~~~
  harvey > /safemode on
  harvey > /safemode allow make
  harvey > /permissions set src/ read,write
  harvey > /audit show 10
  harvey > /security status
~~~

# SEE ALSO

  /safe          — exact alias for /safemode
  /help run      — shell command execution and timeout
  /help routing  — remote endpoint security

`

	// ─── Automation ─────────────────────────────────────────────────────────────

	// PipelineHelpText is shown by /help pipeline and harvey --help pipeline.
	// Generates harvey-pipeline.7.md.
	PipelineHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

PIPELINE — chain Markdown prompt files through models with confidence gating

# SYNOPSIS

/pipeline <CONFIDENCE%> FILE [FILE ...]

# DESCRIPTION

/pipeline executes a sequence of Markdown prompt files as discrete steps,
passing each step's response as input to the next. A confidence threshold
gates progression: if a step's measured confidence score falls below the
threshold the pipeline stops immediately and leaves conversation history
unchanged.

# ARGUMENTS

  CONFIDENCE%   Required first argument. Integer or decimal percentage in
                (0, 100]. The pipeline stops if any step's confidence score
                (0.0–1.0) is below this value divided by 100.

  FILE ...      One or more workspace-relative paths to Markdown files.
                Each file is one pipeline step, executed in order.

# PIPELINE FILE FORMAT

Each FILE is a plain Markdown document. Its body is sent verbatim to the
model as the user message.

  @mention — Model routing

  The first occurrence of @token in the file body selects the model for
  that step. The token is matched against registered routes (/route list)
  first, then used as a model name override on the same provider backend.
  Later @mentions are passed as-is to the model. If the mention cannot be
  resolved the pipeline stops before executing any steps.

# CONTEXT FLOW

  Step 1    carries Harvey's full current conversation history so the first
            model has session context.

  Step N>1  starts a fresh conversation (system prompt only) and receives
            the previous step's response as the user message, followed by
            the step file body. This keeps context usage minimal.

# CONFIDENCE EXTRACTION

After each step Harvey attempts to extract a confidence score using three
methods in priority order:

  1. JSON block — parse {"confidence": X.X, ...} at end of response.
  2. Follow-up — ask the model to rate its confidence 0.0–1.0.
  3. Keyword scan — hedging phrases → 0.30; no hedging → 0.80.

The confidence block is stripped from the response before it is displayed
or forwarded to the next step.

# EXAMPLES

  /pipeline 85% review.md summarise.md
  /pipeline 90% setup.md step1.md step2.md finalise.md

# SESSION STATE

On success the final step's response is appended to conversation history
as an assistant turn. On any failure history and the active model are
unchanged.

# SEE ALSO

  /plan TASK             — generate and execute a step-by-step task plan
  /loop INTERVAL PROMPT  — repeat a prompt on a fixed interval
  /help routing          — route pipeline steps to remote models via @mention

`

	// PlanHelpText is shown by /help plan and harvey --help plan.
	// Generates harvey-plan.7.md.
	PlanHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

PLAN — generate and execute step-by-step task plans with bounded context

# SYNOPSIS

/plan TASK
/plan next
/plan status
/plan show
/plan clear

# DESCRIPTION

/plan breaks a complex task into a numbered GFM checklist, saves it to
agents/plan.md, and executes each step using a bounded context — only the
system prompt and the current plan state are sent to the model per step.
This keeps per-step turn times constant regardless of conversation length
and allows large multi-step tasks without filling the context window.

The plan persists to agents/plan.md across /clear and Harvey restarts.
Use /plan status at any time to review progress.

# SUBCOMMANDS

  /plan TASK
    Ask the model to generate a step-by-step GFM checklist for TASK and
    save it to agents/plan.md. Each step becomes a checkbox item:
      - [ ] Step description
    An existing plan is overwritten; use /plan clear first if you want
    a clean start on a different task.

  /plan next
    Execute the next uncompleted step. Harvey sends only the system
    prompt and the current plan state — not the full conversation
    history — keeping context usage bounded. When a step's tool calls
    are blocked or fail, the step is NOT auto-marked complete; fix
    the underlying issue and run /plan next again.

  /plan status
    Print the plan checklist with completion markers, showing which
    steps are done and which remain.

  /plan show
    Print the raw agents/plan.md file.

  /plan clear
    Delete agents/plan.md. Does not affect conversation history.

# BOUNDED CONTEXT MODEL

Each /plan next call sends only:
  1. The system prompt (HARVEY.md).
  2. The current agents/plan.md content.

The full conversation history is NOT included. This means:
  - Per-step token usage is constant regardless of plan length.
  - The model has no memory of earlier steps beyond the plan file.
  - For steps that need context from prior steps, inject that context
    explicitly via /context add before running /plan next, or note it
    directly in agents/plan.md.

# WORKFLOW EXAMPLE

~~~
  # Break a large task into a plan
  /plan Refactor the auth package to use short-lived JWT tokens

  # Review the generated steps
  /plan status

  # Execute one step at a time
  /plan next
  /plan next

  # Check progress
  /plan status

  # If a step gets stuck, investigate and retry
  /plan next
~~~

# FILES

  agents/plan.md   — persisted plan checklist; human-editable

# SEE ALSO

  /help pipeline   — chain Markdown prompt files with confidence gating
  /help context    — inject context that persists across /plan next calls
  /help skills     — skills for structured multi-step task automation

`

	// LoopHelpText is shown by /help loop and harvey --help loop.
	// Generates harvey-loop.7.md.
	LoopHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

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
`

	// ─── Reference & built-in tools ────────────────────────────────────────────

	// BuiltinToolsHelpText is shown by /help builtin-tools and harvey --help builtin-tools.
	// Also dispatched for "tools" and "builtins" aliases. Generates harvey-builtin-tools.7.md.
	BuiltinToolsHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

BUILT-IN TOOLS — tools Harvey exposes to capable LLM models

# DESCRIPTION

Harvey registers a set of built-in tools that are made available to
language models with structured tool-calling support. The model may invoke
these tools during a conversation turn; Harvey executes them and returns
results before the next model call. All file operations are constrained to
the workspace root; paths outside the workspace are rejected.

Workspace permissions (/permissions) and Safe Mode (/safemode) apply
where noted below.

# FILE TOOLS

read_file PATH [pages]
  Read a file and return its contents. PATH is relative to the workspace
  root. PDF files (.pdf) are automatically extracted to plain text using
  the poppler utilities; use the optional pages parameter to limit
  extraction (e.g. "1-10"). Binary files return an error.

write_file PATH CONTENT
  Write CONTENT to PATH inside the workspace. The file is created or
  overwritten. Parent directories must already exist. Respects workspace
  write permissions.

create_dir PATH
  Create a directory (and any missing parents) at PATH inside the
  workspace. Equivalent to mkdir -p but constrained to the workspace.
  Use this when a task requires creating a new directory tree without
  dropping to run_command.

list_files [PATH]
  List the entries in a workspace directory. Directories are shown with
  a trailing "/". PATH defaults to the workspace root.

file_tree [PATH]
  Display a recursive tree of the workspace (or a subdirectory), skipping
  hidden files and directories.

search_files PATTERN [PATH]
  Search workspace files for lines matching PATTERN (Go regexp syntax).
  Results are capped at 200 matches.

# COMMAND TOOLS

run_command COMMAND [ARGS...]
  Execute a command in the workspace root. Subject to Safe Mode: when
  Safe Mode is on, only allowlisted commands are permitted. Shell
  metacharacters are rejected. Output is capped at 64 KiB.

git_command SUBCOMMAND [ARGS...]
  Run a read-only git subcommand (status, diff, log, show, blame).
  Write operations are blocked regardless of Safe Mode.

# DATE AND TIME TOOLS

current_datetime [format]
  Return the current local date, time, timezone, and UTC equivalent.
  Optional format: "human" (default), "rfc3339", or "unix".

datetime_diff FROM [TO]
  Compute the duration between two datetime strings. TO defaults to now.
  Accepted input formats: RFC3339, YYYY-MM-DD HH:MM:SS, YYYY-MM-DD,
  "Jan 2 2006", "January 2 2006".

format_datetime DATETIME FORMAT
  Parse a datetime string and reformat it. Output formats: "rfc3339",
  "human", "unix", "date" (YYYY-MM-DD), "time" (HH:MM:SS).

# IDENTITY TOOLS

whoami
  Return the current OS username, git user name and email, and hostname.
  Useful when the model is authoring commit messages or project documents
  that need the author's identity.

# SEE ALSO

  /help security   — Safe Mode and workspace permissions
  /help run        — running shell commands interactively
  /tools           — toggle tool calling on/off for the current session

`

	// InspectHelpText is shown by /help inspect and harvey --help inspect.
	// Generates harvey-inspect.7.md.
	InspectHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

INSPECT — show detailed Ollama model information

# SYNOPSIS

/inspect
/inspect MODEL

# DESCRIPTION

/inspect queries the local Ollama server for detailed information about
installed models. Requires an Ollama backend; use /ollama start first if
Ollama is not running.

Without a MODEL argument, /inspect shows a summary table of all installed
models: name, disk size, family, context length, and capability flags
(tools, embed, tagged-blocks). This is identical to /ollama list.

With a MODEL argument, /inspect shows the full detail view for that model:
family, parameter count, quantization level, disk size, context length,
and all Modelfile parameter lines (e.g. temperature, system prompt).

# EXAMPLES

Summary of all installed models:

~~~
  harvey > /inspect
~~~

Detail view for a specific model:

~~~
  harvey > /inspect gemma4:e2b
  Model:        gemma4:e2b [loaded]
  Family:       gemma4
  Parameters:   2.5B
  Quantization: Q4_K_M
  Context:      131072 tokens
  Disk size:    1.7 GiB

  Modelfile parameters:
    stop "<end_of_turn>"
~~~

# SEE ALSO

  /ollama list        — model table (same as /inspect with no args)
  /ollama probe       — test and cache capability flags
  /ollama show MODEL  — raw Modelfile via the ollama CLI
  /help ollama

`

	// ─── Developer tools ────────────────────────────────────────────────────────

	// AssayHelpText is the help page for the assay evaluation harness.
	// Displayed by: assay --help
	AssayHelpText = `%{app_name}(1) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

{app_name}

# SYNOPSIS

{app_name} [OPTIONS]

# DESCRIPTION

{app_name} is an LLM evaluation harness for Harvey. It runs a corpus of prompts
against one or more Ollama models (or a llamafile binary) and produces a
Markdown report plus a JSON results file for human review and automated
checking.

The prompt corpus is defined in a YAML file (default:
agents/assay/corpus.yaml). Each entry specifies a category, a prompt,
automated checks (contains, not_contains, compiles, go_vet), and
human-review questions. {app_name} sends each prompt to each model, records
the response, runs all automated checks, and writes a summary table plus
per-prompt results to the output directory.

When a RAG store is provided via -rag-db, {app_name} embeds each prompt and
injects the top-k retrieved chunks as context before calling the model.
With -rag-compare, every prompt is run twice (once without RAG, once with)
and a per-check delta table is appended to the report.

# OPTIONS

-corpus PATH
: Path to the corpus YAML file.
  Default: agents/assay/corpus.yaml

-models MODEL[,MODEL...]
: Comma-separated list of Ollama model names to evaluate.
  Default: all models currently available on the Ollama server.

-category NAME
: Run only prompts in the named category. Omit to run all categories.

-llamafile PATH
: Path to a llamafile binary to evaluate. {app_name} starts the llamafile
  server automatically before the run and stops it when finished.
  The model name is derived from the binary filename.
  Cannot be combined with -models.

-ollama URL
: Base URL of the Ollama server.
  Default: http://localhost:11434

-output PATH
: Directory to write the report (report.md) and results (results.json).
  Default: $WORKSPACE/assay-results/assay-TIMESTAMP/ if run inside a
  Harvey workspace, or assay-results/assay-TIMESTAMP/ otherwise.

-rag-db PATH
: Path to a Harvey RAG store (SQLite). When set, {app_name} embeds each
  prompt and prepends retrieved context chunks before calling the model.

-rag-top-k N
: Number of RAG chunks to retrieve per prompt when -rag-db is set.
  Default: 3

-rag-embed-model MODEL
: Ollama embedding model used to embed prompts for RAG retrieval.
  Default: nomic-embed-text

-rag-compare
: Run each prompt twice — once without RAG context and once with — and
  append a per-check delta table to the report. Requires -rag-db.

-h, -help, --help
: Display this help message.

-v, -version, --version
: Display version information.

# OUTPUT

{app_name} writes two files to the output directory:

report.md
: Markdown report containing a summary table (model x prompts x
  auto-pass rate x average tok/s) followed by per-prompt result blocks
  with automated check outcomes and space for human review notes.

results.json
: Machine-readable JSON array of all prompt results, including the full
  model response, individual check outcomes, and timing data.

# EXAMPLES

Run all prompts against all local Ollama models:

~~~
  assay
~~~

Run only the go-crosswalk category against a specific model:

~~~
  assay -models qwen2.5-coder:7b -category go-crosswalk
~~~

Evaluate a llamafile binary:

~~~
  assay -llamafile ~/models/qwen2.5-coder-7b.llamafile
~~~

Run with RAG context injection and comparison:

~~~
  assay -models llama3.1:8b -rag-db agents/rag/harvey.db -rag-compare
~~~

Write results to a custom directory:

~~~
  assay -models granite3-moe:3b -output testout/granite-run/
~~~

# SEE ALSO

harvey(1), harvey-rag(7)
`

)
