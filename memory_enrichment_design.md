# Memory Enrichment Design

## Overview

This document describes three design changes to Harvey's memory system, cherry-picked from the CQ (Mozilla AI) knowledge-commons proposal, plus a cross-agent interoperability mechanism so tools like Mistral Vibe can consume Harvey's accumulated knowledge.

The changes are additive and backward-compatible. All three fields default to safe empty/zero values so existing memory files and databases continue to work without migration errors.

---

## Background

Harvey stores experience memories in three independent silos, merged at retrieval time by `UnifiedMemory.Recall()`:

| Silo | Files | Content |
|---|---|---|
| Memory store | `memory_store.go`, `agents/memories/` | Typed experience records in Fountain + YAML |
| RAG store | `rag_support.go`, `agents/rag/*.db` | Vector-embedded document chunks |
| Knowledge base | `knowledge.go`, `agents/knowledge.db` | Hand-authored experiments/observations |

This design touches only the **memory store**. The RAG store and knowledge base are unchanged.

---

## Change 1: Tripartite Insight — add `Action` field

### Problem

`MemoryMeta` has `description` (one sentence) and `summary` (2-3 sentences optimised for embedding). Both fields describe *what happened*. Neither makes the prescriptive step explicit: *what should a future agent do when it encounters this situation?*

### Solution

Add an `action` string field to `MemoryMeta`. It is the imperative, concrete step a future agent should take. It complements `summary` rather than replacing it.

```yaml
# Example memory front matter after change
id: git_fix_a3f891
type: tool_use
kind: pitfall
confidence: 0.9
description: "Run git init when git reports 'not a repository'"
summary: >
  Harvey encountered 'fatal: not a git repository' while running a git command.
  Running git init in the project root resolved the error immediately.
action: "Run `git init` in the project root, then retry the original git command."
tags: [git, init, error]
```

`action` is included in `EmbedText()` so it contributes to semantic retrieval scoring.

The memory miner's `proposedMemory` struct and `minerSystemPrompt` are updated to elicit an `action` value for each extracted memory.

### Backward compatibility

`action` is `omitempty` in YAML. Existing memory files without it parse without error and get an empty string. Empty action fields are silently skipped in DIGEST.md rendering and `EmbedText()`.

---

## Change 2: Lifecycle Kinds — add `Kind` field

### Problem

`MemoryMeta.Type` (tool_use, workflow, user_preference, workspace_profile, project_fact) classifies *topic*. It answers "what kind of thing is this memory about?" It does not answer "why does this knowledge matter?" or "how permanent is it?"

### Solution

Add a `kind` string field. `kind` and `type` are orthogonal: a `tool_use` memory can be a `pitfall` or a `workaround`; a `workflow` memory can be a `recommendation`. The four kind values are:

| Kind | Meaning |
|---|---|
| `pitfall` | A permanent gotcha — an API quirk, undocumented behaviour, or subtle invariant that no tool can abstract away. Unlikely to become obsolete. |
| `workaround` | Useful now but represents a gap in tooling or documentation. May be superseded when better tooling exists. |
| `recommendation` | Points toward the right approach, tool, or pattern. "Prefer X over Y for Z." |
| `pattern` | A recurring successful approach worth repeating. Harvey-specific extension beyond CQ. |
| `""` | Unclassified. The default. Safe fallback for existing memories and for miner output on ambiguous cases. |

`kind` is indexed in `memories_fts` so queries like `SearchFTS("pitfall git")` work. The `/memory list` command gains a `--kind` filter.

### Miner assignment

The miner prompt is updated with the four kind values and their descriptions. The miner assigns the most appropriate kind for each extracted memory; it defaults to empty string when classification is uncertain rather than guessing.

### Backward compatibility

`kind` is `omitempty` in YAML. Empty kind values are valid and display as "unclassified" in DIGEST.md.

---

## Change 3: Confidence — add `Confidence float64` field

### Problem

Harvey has no signal for *how reliable* a memory is. All memories are treated equally at retrieval time regardless of whether they have been validated, questioned, or found to be outdated.

### Solution

Add `confidence float64` to `MemoryMeta`, stored as a `REAL` column in `memories.db`.

| Value | Meaning |
|---|---|
| `1.0` | Fully trusted. Manually confirmed or very frequently validated. |
| `0.5` | Default for new memories. Neutral confidence. |
| `0.2` | Auto-archive threshold. Memory is treated as unreliable. |
| `0.0–0.19` | Not reachable in normal operation; `SetConfidence` clamps to 0.0 and archives. |

#### Retrieval weighting

`Query()` multiplies the cosine similarity score by `confidence` before ranking. `SearchFTS()` multiplies the FTS rank by `confidence`. A high-cosine but low-confidence memory ranks below a moderate-cosine high-confidence memory.

```
final_score = cosine_similarity × confidence
```

#### `/memory flag <id>`

Changes from "archive this memory" to "reduce confidence by 0.1". When confidence drops to or below 0.2, `SetConfidence` calls `Archive` automatically. The user is shown the new confidence value and notified if auto-archival triggered.

```
/memory flag git_fix_a3f891
→ git_fix_a3f891: confidence 0.9 → 0.8
```

```
/memory flag git_fix_a3f891   (after several flags)
→ git_fix_a3f891: confidence 0.2 → archived (below threshold)
```

Explicit archival remains available as `/memory archive <id>` for immediate removal.

#### Future: confidence boost

When a memory is retrieved and injected into a session that the user rates successful (or when a session ends without the memory being contradicted), confidence may be auto-boosted. This is out of scope for this change; the field exists to support it.

### DB migration

Three `ALTER TABLE` statements are added to `NewMemoryStore()` using the lazy migration pattern already in use for the `source` column:

```sql
ALTER TABLE memories ADD COLUMN kind       TEXT NOT NULL DEFAULT '';
ALTER TABLE memories ADD COLUMN action     TEXT NOT NULL DEFAULT '';
ALTER TABLE memories ADD COLUMN confidence REAL NOT NULL DEFAULT 0.5;
```

These are idempotent — SQLite ignores "duplicate column name" errors.

### Backward compatibility

`confidence` is `omitempty` in YAML but defaults to `0.5` in `ParseMemoryDoc` when absent. The DB migration sets `0.5` for all existing rows.

---

## Change 4: Cross-Agent Digest — `DIGEST.md` + harvey-memory skill

### Problem

Harvey's memories are inaccessible to other agents (Vibe, Claude Code) without a SQLite client. Vibe can read individual `.fountain` files but has no index. The SQLite index is opaque without `sqlite3`. Each agent must independently rediscover what Harvey already knows about the workspace.

### Solution: `DIGEST.md`

Harvey auto-writes `agents/memories/DIGEST.md` — a flat Markdown file summarising all active memories — whenever memories are saved, archived, or auto-mined. The file is plain text, human-readable, and readable by any LLM with no dependencies.

#### Format

```markdown
# Harvey Memory Digest
*Updated: 2026-06-17T14:32:00Z — 12 active memories*

## Pitfalls
- **[tool_use]** `git_fix_a3f891` (confidence: 0.9) — Run git init when git reports 'not a repository'
  **Action:** Run `git init` in project root, then retry.
  Tags: `git` `init` `error`

## Workarounds
- **[workflow]** `build_flag_b7c221` (confidence: 0.7) — Pass -race to go test before any commit
  **Action:** Add `-race` flag; fix data races before committing.
  Tags: `go` `race` `test`

## Recommendations
...

## Patterns
...

## Unclassified
...
```

Sections with no entries are omitted. Entries within a section are sorted by confidence descending.

#### When written

`WriteDigest(path string)` is called automatically from:
- `MemoryStore.Save()` — after every memory write
- `MemoryStore.Archive()` — after every archive
- `MineAuto()` — after the batch save loop completes

The path is `filepath.Join(s.dir, "DIGEST.md")` — i.e., `agents/memories/DIGEST.md`.

### Solution: `agents/skills/harvey-memory/SKILL.md`

A new skill file teaches agents *when* to read the digest and *how* to use it. This is the same pattern CQ uses for Claude Code: a `SKILL.md` that shapes agent behaviour without requiring any code integration.

The skill:
- Triggers at session start or before making project changes
- Instructs the agent to read `agents/memories/DIGEST.md`
- Explains memory types, kinds, and confidence semantics
- Explains that low-confidence memories should be treated as suggestions, not facts

### VIBE.md and CLAUDE.md updates

Both instruction files gain a "Harvey memories" section that:
- Lists the skill and its purpose
- Points directly to `agents/memories/DIGEST.md` as the readable knowledge index
- Explains how to interpret confidence values

---

## What is not changed

- **`RagStore`** — document chunk embeddings are workspace-specific and not experience memories. No interop problem to solve.
- **`knowledge.go` (Lab KB)** — the experiments/observations database is already queryable via `sqlite3` as documented in VIBE.md and CLAUDE.md.
- **`UnifiedMemory` retrieval ordering** — the priority (`workspace_profile` + `project_fact` first, then experiential, then RAG, then KB) is unchanged. Confidence affects ranking *within* each silo, not between silos.
- **Memory types** — the five existing types (tool_use, workflow, user_preference, workspace_profile, project_fact) are unchanged. `kind` is a new orthogonal dimension.
- **External dependencies** — no new Go modules, no new binary dependencies.

---

## Data model summary

### `MemoryMeta` after change

```go
type MemoryMeta struct {
    ID            string         `yaml:"id"`
    Type          MemoryType     `yaml:"type"`
    Kind          string         `yaml:"kind,omitempty"`
    CreatedAt     string         `yaml:"created_at"`
    UpdatedAt     string         `yaml:"updated_at"`
    Supersedes    []string       `yaml:"supersedes"`
    Tags          []string       `yaml:"tags"`
    Description   string         `yaml:"description"`
    Summary       string         `yaml:"summary"`
    Action        string         `yaml:"action,omitempty"`
    Confidence    float64        `yaml:"confidence,omitempty"`
    SourceSession string         `yaml:"source_session,omitempty"`
    Metadata      map[string]any `yaml:"metadata,omitempty"`
}
```

### `memories` table after migration

```sql
CREATE TABLE IF NOT EXISTS memories (
    id             TEXT    PRIMARY KEY,
    type           TEXT    NOT NULL,
    kind           TEXT    NOT NULL DEFAULT '',
    description    TEXT    NOT NULL,
    summary        TEXT    NOT NULL,
    action         TEXT    NOT NULL DEFAULT '',
    tags           TEXT    NOT NULL DEFAULT '[]',
    source_session TEXT    NOT NULL DEFAULT '',
    file_path      TEXT    NOT NULL,
    created_at     TEXT    NOT NULL,
    updated_at     TEXT    NOT NULL,
    archived       INTEGER NOT NULL DEFAULT 0,
    confidence     REAL    NOT NULL DEFAULT 0.5,
    embedding      BLOB    NOT NULL
);
```

### `memories_fts` table after migration

`kind` and `action` are added as indexed columns.

---

## Design decisions

**Why `kind` as a free string rather than a typed constant?**
Using `string` with documented values (rather than a Go `const` block) keeps the YAML representation simple and allows future values to be added without a Go recompile. Validation happens at mine-time via the miner prompt; invalid kind values in hand-authored files are treated as unclassified.

**Why multiply confidence into the score rather than filtering?**
Filtering (e.g., "only return memories with confidence > 0.3") creates hard cliffs and discards potentially useful context. Multiplicative weighting preserves low-confidence memories in the result set at reduced rank. The user still sees them but they don't dominate.

**Why DIGEST.md rather than a JSON export?**
LLMs read Markdown naturally. A JSON array of metadata structs requires the consuming agent to parse and format it before it's useful. DIGEST.md is immediately readable and conveys priority through its structure (pitfalls before workarounds, sorted by confidence). A JSON export can be added later if programmatic consumption is needed.

**Why a skill file rather than updating VIBE.md directly?**
A skill can be loaded on demand and versioned independently. Multiple agent instruction files (VIBE.md, CLAUDE.md, future agent files) can reference the same skill without duplicating content. The skill is also loadable within Harvey itself via `/skill load harvey-memory`.
