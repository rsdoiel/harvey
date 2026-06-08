%harvey(7) user manual | version 0.0.9 376117f
% R. S. Doiel
% 2026-06-08

# NAME

MEMORY — mine session recordings for memories and manage the memory store

# SYNOPSIS

/memory <mine|list|show|forget|status|recall|profile> [args...]

# DESCRIPTION

/memory provides a semi-manual system for extracting useful knowledge from
Harvey's Fountain session recordings (.spmd / .fountain files) and injecting
that knowledge into future sessions. Memories persist across sessions as
Fountain files in agents/memories/ inside the workspace.

# SUBCOMMANDS

  mine [FILE] [--force]
        Scan unmined session files for memories. The LLM proposes memories
        via one-shot JSON extraction; you review each interactively
        (accept / edit / replace / skip / quit). Use --force to re-mine
        sessions that have already been processed.

  list [--type TYPE]
        List stored memories. Optional --type filters to one of:
        tool_use, workflow, user_preference, workspace_profile, project_fact.

  show ID
        Display the full Fountain source for one memory by its ID slug.

  forget ID
        Archive a memory (moves it to agents/memories/archive/ — not deleted).

  status
        Show memory store location, total count, and breakdown by type.

  recall QUERY
        Query all memory silos (workspace profile, project facts, experiential
        memories, RAG chunks, and KB observations) and print grouped results.
        Uses FTS5 full-text search plus cosine similarity when a RAG store is
        configured. No token budget is applied — all matching results are shown.

  profile show|update|use [name]
        Manage the workspace profile.
        "show"   — lists workspace_profile memories (equivalent to /memory list
                   --type workspace_profile).
        "update" — opens the most recent profile in $EDITOR and re-saves it.
        "use"    — switches to a new profile: writes a handoff document to
                   agents/hand-off/, archives the current profile, selects a
                   template (by name or interactive picker), saves it as the
                   new profile, and resets history so the new context injects
                   on the next turn. Alias: /profile use [name].

# MEMORY TYPES

  tool_use          A tool or command trick that worked (e.g. a useful flag,
                    a workaround for a known bug).
  workflow          A repeatable multi-step process (e.g. how to publish a release).
  user_preference   A stated or demonstrated preference (e.g. preferred coding style).
  workspace_profile Factual description of the workspace: what it is, its purpose,
                    its primary language and tools. Always injected first.
  project_fact      A key fact about the current project: deadlines, conventions,
                    constraints. Always injected second.

# MEMORY INJECTION

When a session starts, Harvey injects a [memory context] block into the
conversation. Factual types (workspace_profile, project_fact) always appear
first. Experiential memories (tool_use, workflow, user_preference) are ranked
by FTS5 full-text search and optionally cosine similarity. RAG chunks and KB
observations follow if token budget permits.

The budget is controlled by memory.budget_pct in harvey.yaml (default 0.25 of
the context window). Setting memory.inject_on_start: false disables injection.

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
  /memory show pipeline_confidence_extraction
  /memory forget old_pattern_a1b2c3
  /memory status
  /memory recall git repository error
  /memory profile show
  /memory profile update
