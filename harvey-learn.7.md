%harvey(7) user manual | version 0.0.20 cd480d8
% R. S. Doiel
% 2026-10-20

# NAME

LEARN — how Harvey accumulates and retrieves knowledge

# DESCRIPTION

Harvey stores knowledge in two independent silos that are unified at
retrieval time. Understanding which silo to use for which kind of content
is the key to getting consistently good results.


# THE ONE DECISION RULE

  Have a text file or document?        →  /kb document ingest <file>
  Something useful happened in a session? →  /memory mine
  Making an observation about an experiment? →  /kb observation add


# THE SILOS

  Silo            What belongs here          How to add       How it arrives
  ─────────────── ─────────────────────────  ───────────────  ─────────────────────
  Memory store    Patterns observed during   /memory mine     Session-start via
                  sessions: what worked,     (interactive)    /memory recall
                  what the model got wrong,  auto-mines on
                  user preferences           session exit

  Knowledge base  Reference documents,       /kb document     On-demand via
                  research notes, named      ingest, /kb      /memory recall
                  experiments, concepts      observation add  and /kb search

Retrieval from both silos is unified:

  /memory recall <query>   — search both silos, print ranked results
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

  /status          — shows active memories and unmined sessions
  /hint            — prints actionable suggestions for improving results
  /memory status   — detailed memory store stats and budget advice


# COMMON WORKFLOWS

Ingest a reference document before starting a coding session:

  /kb document ingest Reference/papers/oberon2.md

Mine learnings from last session before starting the next:

  /memory mine

Record an observation about a running experiment:

  /kb observe "Qwen3.5 handles nil pointer chains correctly after explicit cast"

Check what would improve the current session:

  /hint


# SEE ALSO

  /profile        — alias for /memory profile (manage workspace profile)
  /recall QUERY   — alias for /memory recall (search all silos)
  /help memory    — full memory command reference
  /help kb        — full knowledge base reference

