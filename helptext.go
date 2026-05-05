package harvey

var (
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
    author: rsdoiel
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

Staleness: if SKILL.md is modified after the scripts were compiled, Harvey
warns you when the skill is invoked and runs the old compiled version.
Recompile the skill on a capable system to pick up the changes.

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
  /skill info NAME         show path, compatibility, and license
  /skill status            count skills by scope
  /skill new               interactive wizard to create a new skill
  /skill run NAME          run a skill (dispatches compiled scripts if available)
~~~

`

	RoutingHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

ROUTING

# SYNOPSIS

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
  # Register a Pi cluster node
  /route add pi2 ollama://192.168.1.12:11434 llama3.1:8b

  # Register the Anthropic cloud endpoint
  /route add claude anthropic:// claude-sonnet-4-20250514

  # Enable routing
  /route on

  # Dispatch a complex task to the cloud
  @claude refactor this module to use the repository pattern

  # Run a quick task on a Pi node
  @pi2 write a unit test for the Parse function

  # Local model handles everything else (no @mention)
  what does this error mean?
~~~

# SLASH COMMANDS

~~~
  /route add NAME URL [MODEL]   register a remote endpoint
                                  @pi2    ollama://192.168.1.12:11434 llama3.1:8b
                                  @claude anthropic:// claude-sonnet-4-20250514
  /route rm NAME                remove a registered endpoint
  /route list                   show all endpoints with reachability status
  /route on                     enable @mention dispatch (persisted)
  /route off                    disable @mention dispatch (persisted)
  /route status                 show routing state and endpoint count
~~~

Registered endpoints and the on/off state persist across sessions in
`+"`"+`<workspace>/agents/routes.json.`+"`"+`

`

	OllamaHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

OLLAMA COMMANDS

# SYNOPSIS

/ollama SUBCOMMAND [ARGS...]

# DESCRIPTION

The /ollama command controls the local Ollama service and manages models
from inside the Harvey REPL. Every subcommand maps directly to an ollama
CLI operation.

# SUBCOMMANDS

Service control:

  /ollama start
    Launch ollama serve in the background.

  /ollama stop
    Print a reminder to stop Ollama via your system's service manager
    (e.g. systemctl stop ollama). Harvey does not stop the daemon itself.

  /ollama status
    Check whether the Ollama daemon is reachable at the configured URL.

  /ollama logs
    Tail the Ollama service log. Tries ollama logs first, falls back
    to journalctl -u ollama.

  /ollama env
    Show the Ollama environment variables (OLLAMA_HOST, etc.) as seen
    by the Harvey process.

Model management:

  /ollama list
    List all installed models. The model currently in use is marked with *.

  /ollama ps
    Show which models are loaded in memory (delegates to ollama ps).

  /ollama pull MODEL
    Download a model from the Ollama registry (e.g. /ollama pull mistral).

  /ollama push MODEL
    Upload a model to the Ollama registry.

  /ollama show MODEL
    Display a model's Modelfile, parameters, and template.

  /ollama create NAME [-f MODELFILE]
    Create a new model from a Modelfile. Passes all arguments directly
    to ollama create.

  /ollama cp SOURCE DEST
    Copy an installed model to a new name.

  /ollama rm MODEL [MODEL...]
    Remove one or more installed models.

  /ollama use MODEL
    Switch the active model to MODEL for the current session without
    restarting Harvey.

  /ollama run MODEL [PROMPT]
    Launch an interactive ollama run session inside the terminal.
    stdin, stdout, and stderr are passed through directly.

`

	ApplyHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

APPLY — write tagged code blocks to the workspace

# SYNOPSIS

/apply

# DESCRIPTION

/apply scans the most recent assistant reply for fenced code blocks that
carry a file path in their opening fence line, then writes each block to
the named path inside the workspace.

Harvey auto-applies tagged blocks immediately after every assistant reply,
so you usually do not need to run /apply manually. Use it when:

  - You cancelled the auto-apply prompt and want to apply later.
  - You resumed a session and want to apply blocks from the loaded history.
  - You want to re-apply blocks from a previous reply after reviewing them.

# TAGGED BLOCK FORMAT

A code block is "tagged" when its opening fence line includes a file path.
Two formats are recognised:

~~~
  Colon-separated (lang:path):
    ` + "```" + `go:harvey/spinner.go
    ... code ...
    ` + "```" + `

  Space-separated (lang path):
    ` + "```" + `go harvey/spinner.go
    ... code ...
    ` + "```" + `
~~~

The language hint (go, bash, ts, …) is optional but recommended. The path
must contain a directory separator (/) or end with a recognised extension
(.go, .ts, .md, .sh, etc.) to be detected as a path rather than a language
name.

Blocks without a path tag are ignored by /apply.

# CONFIRMATION

/apply lists every tagged block with its target path and byte count, then
asks before writing:

~~~
  Found 2 tagged block(s):
    harvey/spinner.go (1842 bytes)
    harvey/spinner_test.go (640 bytes)
  Apply all? [Y/n]
~~~

Press Enter or type y to write all blocks. Type n to abort without writing
any files.

# WORKSPACE CONSTRAINT

All paths are resolved relative to the workspace root (--workdir or ".").
Paths that would escape the workspace (e.g. ../../etc/passwd) are rejected.

`

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

	SessionHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

SESSION — continue or replay recorded conversations

# SYNOPSIS

/session continue FILE
/session replay   FILE [OUTPUT]

# DESCRIPTION

Harvey saves every conversation to a Fountain .spmd file. The /session
command lets you reload those files in two distinct ways:

  continue  — restore the conversation history and keep chatting.
  replay    — re-send the original user turns to the current model and
              record fresh responses.

# CONTINUE

/session continue FILE loads all turns from FILE into the current history
and returns you to the REPL. The model sees the full prior conversation as
if it had been running the whole time.

Use continue to:

  - Resume work across Harvey restarts.
  - Switch to a different model and pick up mid-conversation.
  - Inspect and then extend a session that was auto-saved.

Harvey also offers to continue the most recently saved session at startup;
pressing Enter at that prompt is equivalent to /session continue.

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
File naming:          harvey-session-YYYYMMDD-HHMMSS.spmd

# CLI FLAGS

The same operations are available as startup flags:

  harvey --continue FILE         load history then open REPL
  harvey --replay FILE           replay without entering REPL
  harvey --replay-output FILE    write replay output to FILE

# SEE ALSO

  /record          — start or stop session recording manually
  harvey --help    — full CLI flag reference

`

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

`

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

	RagHelpText = `%{app_name}(7) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

RAG — Retrieval-Augmented Generation

# SYNOPSIS

/rag <list|new NAME|switch NAME|drop NAME|setup|ingest PATH|status|query TEXT|on|off>

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
memory. The active store can be changed with /rag switch at any time.

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
  /rag switch golang

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

/rag switch NAME
  Close the currently open store and activate NAME. The change is persisted
  to agents/harvey.yaml.

/rag drop NAME
  Remove a store from the registry after confirmation. The .db file is NOT
  deleted automatically — the path is printed so you can remove it manually.

/rag setup
  Backward-compatible alias for /rag new using the current active store
  name, or "default" when no store is configured. Use /rag new NAME to
  give the store a meaningful name.

/rag ingest PATH [PATH...]
  Reads each file or directory (recursively), splits text into
  paragraph-sized chunks (~500 characters each), embeds them using the
  active store's embedding model, and stores the vectors in the database.
  Re-ingest any file after it changes to keep the store current.
  Supported text formats: .md, .txt, .go, .ts, and most plain-text files.

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

`

	HelpText = `%{app_name}(1) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

{app_name}

# SYNOPSIS

{app_name} [OPTIONS] 

# DESCRIPTION

{app_name} is a terminal agent for local large language models. It was
inspired by Claude Code but focused on working with large language models
in small computer environments like a Raspberry Pi computer running
Raspberry Pi OS. While the inspiration was to run an agent locally with
Ollama it can also be run on larger computers like Linux, macOS and Windows
systems you find on desktop and laptop computers. It should compile for most
systems where Ollama is available and Go is supported (example: *BSD).

{app_name} looks for HARVEY.md in the current directory and uses it as a
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

# ENVIRONMENT

ANTHROPIC_API_KEY   API key for Anthropic Claude (optional, for /route add NAME anthropic://)
DEEPSEEK_API_KEY    API key for DeepSeek (optional, for /route add NAME deepseek://)
GEMINI_API_KEY      API key for Google Gemini (optional; GOOGLE_API_KEY also accepted)
MISTRAL_API_KEY     API key for Mistral (optional, for /route add NAME mistral://)
OPENAI_API_KEY      API key for OpenAI (optional, for /route add NAME openai://)

All of the above API key variables are filtered out of every child process
environment — they are never passed to commands run via ! or /run.

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

`
)
