%harvey(7) user manual | version 0.0.20 cd480d8
% R. S. Doiel
% 2026-10-20

# NAME

KB — the knowledge base, through the installed kb

# SYNOPSIS

/kb [OPTIONS] VERB [ARGS...]
/kb inject [PROJECT]
/kb project use ID
/kb learn [ingest|draft|review|concepts] [ARGS]
/kb draft [--project NAME] [--limit N] [@route] [SESSION]

# DESCRIPTION

/kb runs the installed kb(1) on this workspace's knowledge base,
<workdir>/agents/knowledge.db, and shows its output. The verbs, options and
exit codes are kb's own, so anything the command line can do, /kb can, and
"/kb help" shows kb's manual. harvey passes -db for the workspace's database
unless you give one. kb runs in the workspace with no terminal input, so a
verb kb keeps for a terminal (accepting a decision record, for example)
refuses; run those yourself with ! kb ... .

kb must be installed:

~~~
  go install github.com/rsdoiel/knowledge/cmd/kb@latest
~~~

KB holds hand-authored structured records and ingested documents.

Add -json before the verb for machine-readable output. harvey does not add it.

# EXAMPLES

~~~
  /kb search "context window"
  /kb project list
  /kb observation add --project harvey finding "chunk size 500 removes noise"
  /kb record show harvey/DR-0048
  /kb stats
~~~

# HARVEY'S OWN VERBS

Three things only harvey can do are not sent to kb.

/kb inject [PROJECT]
  Format the knowledge base as Markdown and add it to the conversation
  as a user message. With no argument, injects the current project (or all
  projects if none is current). With a project name, injects only that project.

/kb project use ID
  Set the current project by numeric ID. inject, learn and memory recall use
  it. (/kb project list shows the IDs.)

/kb learn
  See LEARNING MODE.

# LEARNING MODE

/kb learn turns Harvey's own recorded sessions and hand-off notes into
searchable knowledge. Every step ends with a human decision: nothing a model
writes is trusted, or found by /kb search, until you accept it. All four
steps work on the current project (/kb project use ID).

/kb learn ingest [--min-words N] [--all] [--dry-run]
  List the hand-off notes and recorded sessions that are not yet in the
  knowledge base, newest first, and ingest the ones you choose. Hand-offs
  are always offered; a session must have at least 200 words (--min-words
  changes that, --all offers every session). New sections start
  unsummarized.

/kb learn draft [--limit N] [--dry-run] [@model]
  Ask a model to draft a summary of each unsummarized section, then of each
  whole document from its section summaries. The model is @model if given,
  else learn_model in agents/harvey.yaml, else the active model. Default
  limit is 25 items (--limit 0 drafts everything queued); a small local model
  takes about a minute per item, so a full run over many documents is long.
  Each draft gets a confidence score, which is only how many of the known
  concepts in the source the draft also names. It is not a measure of
  whether the draft is faithful.

/kb learn review [@model]
  Walk the drafts one at a time, showing the source excerpt, size, known
  concepts mentioned, confidence and which model wrote it:

~~~
  [a]ccept   trust the summary; it becomes searchable
  [e]dit     open $EDITOR on the draft (the edit is recorded as "human")
  [r]edraft  ask the model again
  [s]kip     leave it drafted (Enter also skips)
  [q]uit
~~~

  Bare /kb learn drafts, then reviews. A summary whose source changed after
  it was written is marked STALE.

/kb learn concepts [--limit N]
  Suggest new concepts from the ingested documents (most distinctive terms
  that are not yet concepts; default 20). You pick which become concepts,
  by number, range (1-3), all or none. Each pick is added to the knowledge
  base at once. Then, for each project document that mentions a picked
  concept more than once, Harvey shows the changed lines and asks before
  writing: [[Concept]] links, and footnotes for near-miss spellings.

  A write goes through the permissions table (/permissions) like any other
  file write, and a path Harvey may not write is refused before you are
  asked. The document is re-ingested straight afterwards; if that fails,
  the file is put back. Fountain documents (sessions and hand-offs) are
  never rewritten: they are records, and [[...]] in them reads as a note.
  Tagging changes source text, so summaries of changed sections are marked
  stale; the closing line says how many.

# DRAFTING OBSERVATIONS

/kb draft [--project NAME] [--limit N] [@route] [SESSION]
  Read a recorded session and ask a model what in it is worth remembering,
  then write each answer to the knowledge base as a draft observation. A draft
  is not searchable, not recalled into a prompt and not counted until you
  promote it; harvey never promotes one. Nothing runs on its own: you type
  this when you want it.

  SESSION is a session name (harvey-session-20261030-143612) or the path of a
  .jsonl log; with none it reads the session you are in. It reads what you
  and the model said, and each tool call with its status and the start of its
  output, in passes that fit the drafting model's context window. Skill and
  knowledge-base text added to prompts is not read, and neither is anything
  the log recorded at debug level.

  The model is @route if given, else the active model (learn_model is for
  /kb learn only). Each draft is signed "harvey MODEL", so the review queue
  shows which model wrote it, and ends with a line "Drafted from SESSION#a..#b."
  naming the events it came from.

  The project is --project, else kb: {project: NAME} in agents/harvey.yaml,
  else the workspace directory's name when a kb project has it; otherwise
  /kb draft stops and lists the projects.

  At most 5 drafts are written (--limit N changes that; --limit 0 writes all).
  A proposal that repeats an observation already in the project, in any
  status, or an earlier proposal of the run, is skipped and the report says
  what it matched. A pass whose model call fails is reported and does not stop
  the others; the exit code is that of the first failure. Ctrl-C once stops
  after the pass in flight (its answer is kept, with what was already
  written); Ctrl-C again cancels the request now, for a model that is stuck.
  Tab after /kb draft offers the recorded sessions.

  Each request asks the model for at most 512 tokens, room for the handful of
  lines a pass wants. Without a limit a small model on hailo-ollama ran on to
  the server's own stopping point and repeated its five lines eight times.

  Expect to discard most drafts from a small model. On one real session,
  qwen2.5:3b, llama3.2:3b and granite4 proposed between 1 and 5 drafts each,
  and about one in four or five was usable as written; none stated the lesson
  of the session cleanly. A hailo-ollama model is slow (about 2.6 tokens a
  second for llama3.2:3b), and its context window is not known to harvey, so a
  pass is sized for 2,048 tokens. The review step is where a draft becomes
  knowledge; do not skip it.

  Review the drafts in a normal shell, because promoting needs a terminal:

~~~
  kb observation review list --project NAME
  kb observation review promote ID
~~~

  This needs kb 0.0.30 or newer.

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
  /kb document ingest    — make a document searchable (the installed kb)
  /help learn            — overview of the memory silos

