%harvey(7) user manual | version 0.0.13 b9900f6
% R. S. Doiel
% 2026-06-19

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
the context window). Setting memory.inject_on_start: false disables injection.

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
