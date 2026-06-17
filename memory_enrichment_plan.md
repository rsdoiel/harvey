# Memory Enrichment Plan

Implements the design in `memory_enrichment_design.md`. Seven steps, each independently testable. Steps 1–5 are within the harvey Go package and must be done in order. Steps 6–7 are file-only and can be done in parallel with testing.

---

## Step 1 — MemoryMeta schema (`memory.go`)

### Changes

Add three fields to `MemoryMeta`:

```go
Kind       string  `yaml:"kind,omitempty"`
Action     string  `yaml:"action,omitempty"`
Confidence float64 `yaml:"confidence,omitempty"`
```

Position `Kind` after `Type`; position `Action` after `Summary`; position `Confidence` after `Action`.

Update `EmbedText()`: append `m.Meta.Action` to the parts slice when non-empty.

Update `NewMemoryDoc()`: set `Confidence: 0.5` in the returned struct.

Update `ParseMemoryDoc()`: after YAML unmarshal, if `meta.Confidence == 0` set `meta.Confidence = 0.5` (handles files that predate this field).

Update all `/** ... */` doc comments for `MemoryMeta`, `EmbedText`, and `NewMemoryDoc` to document the new fields and the default confidence behaviour.

### Tests to run

```bash
go test -run TestMemoryDoc
go test -run TestParseMemoryDoc
go test -run TestNewMemoryDoc
```

Existing tests should pass unchanged because `omitempty` fields are not required. Add one test case to `ParseMemoryDoc` tests: a file with no `confidence:` line should return `Confidence == 0.5`.

---

## Step 2 — DB migration and CRUD (`memory_store.go`)

### Schema constant

Update `memoriesSchema` to include the three new columns in the `CREATE TABLE` statement for new databases.

### Lazy migration

Add three statements to a new `memoriesEnrichAlterStmts` slice (same pattern as `enrichedAlterStmts` in `rag_support.go`). Apply them in `NewMemoryStore()` immediately after `memoriesSchema` is applied:

```go
var memoriesEnrichAlterStmts = []string{
    `ALTER TABLE memories ADD COLUMN kind       TEXT NOT NULL DEFAULT ''`,
    `ALTER TABLE memories ADD COLUMN action     TEXT NOT NULL DEFAULT ''`,
    `ALTER TABLE memories ADD COLUMN confidence REAL NOT NULL DEFAULT 0.5`,
}
```

### FTS schema

Add `kind` and `action` as indexed columns to `memoriesFTSSchema`. Because FTS tables cannot use `ALTER TABLE`, handle the migration by checking for column existence via `PRAGMA table_info(memories_fts)` and recreating the virtual table if the new columns are absent. This is a one-time migration that fires only on databases created before this change.

Note: recreating the FTS table loses its content; `rebuildIfNeeded()` already handles re-indexing from files, so call it unconditionally after any FTS recreation.

### `Save()`

Add `kind`, `action`, and `confidence` to the `INSERT OR REPLACE` statement and the corresponding FTS insert.

### `Query()`

Multiply `cosineSimilarity(queryVec, vec)` by the memory's confidence value when building `candidates`. Fetch confidence from the row scan:

```go
rows, err := s.db.Query(
    `SELECT id, file_path, embedding, confidence FROM memories WHERE archived=0`,
)
```

### `SearchFTS()`

Fetch `m.confidence` from the `memories` join and multiply `score` by it before appending to `out`.

### `SetConfidence()`

New method:

```go
func (s *MemoryStore) SetConfidence(id string, delta float64) (float64, error)
```

- Reads current confidence from DB.
- Adds delta and clamps to [0.0, 1.0].
- Writes clamped value back.
- If result <= 0.2, calls `s.Archive(id)` and returns the clamped value with a sentinel error `ErrAutoArchived`.
- Updates `updated_at` to now.

Define `ErrAutoArchived = errors.New("memory auto-archived: confidence below threshold")` as a package-level error so callers can distinguish it.

### `WriteDigest()`

New method:

```go
func (s *MemoryStore) WriteDigest(path string) error
```

- Queries all non-archived memories ordered by confidence descending within each kind.
- Groups by kind in display order: pitfall → workaround → recommendation → pattern → "" (unclassified).
- Writes Markdown to `path` using `os.WriteFile`.
- Skips the file write (returns nil) if there are zero active memories.
- Header format:

```markdown
# Harvey Memory Digest
*Updated: <RFC3339> — <N> active memories*
```

- Entry format per memory:

```markdown
- **[<type>]** `<id>` (confidence: <X.X>) — <description>
  **Action:** <action>
  Tags: `tag1` `tag2` ...
```

The `**Action:**` line is omitted when action is empty.

### Auto-call `WriteDigest`

Add `s.WriteDigest(filepath.Join(s.dir, "DIGEST.md"))` at the end of `Save()` and `Archive()`. Ignore the error (digest is best-effort; a failure to write DIGEST.md must not fail a memory save).

### `rebuildIfNeeded()`

Update the `INSERT OR IGNORE` statement to include `kind`, `action`, and `confidence` columns. Default confidence to `0.5`.

### Tests to run

```bash
go test -run TestMemoryStore
go test -run TestMemoryQuery
go test -run TestSetConfidence
go test -run TestWriteDigest
```

Add tests for:
- `SetConfidence` positive delta, negative delta, clamp at 1.0, clamp at 0.0
- `SetConfidence` auto-archive at threshold (check `ErrAutoArchived` returned)
- `Query` with two memories at different confidence values — lower-confidence memory must rank below higher-confidence memory even if cosine scores are equal
- `WriteDigest` with empty store (no file written), with memories of each kind, with memories missing action field

---

## Step 3 — Miner alignment (`memory_miner.go`)

### `proposedMemory` struct

Add two fields:

```go
type proposedMemory struct {
    Type         string   `json:"type"`
    Kind         string   `json:"kind"`
    Description  string   `json:"description"`
    Summary      string   `json:"summary"`
    Action       string   `json:"action"`
    Tags         []string `json:"tags"`
    FountainBody string   `json:"fountain_body"`
}
```

### `minerSystemPrompt`

Rewrite to add `kind` and `action` fields and explain the four kind values. Keep the existing rules (return [], avoid one-offs, durable knowledge only). The updated schema block:

```
Return a JSON array of objects with these fields:
  type          string  — one of: "tool_use", "workflow", "user_preference"
  kind          string  — one of: "pitfall", "workaround", "recommendation", "pattern", or ""
  description   string  — one sentence, action-oriented
  summary       string  — 2-3 sentences explaining what happened and why it matters
  action        string  — imperative sentence: the concrete step a future agent should take
                          (empty string "" if no clear action applies)
  tags          array   — 3-7 lowercase keywords
  fountain_body string  — Fountain dialogue ...

Kind values:
  pitfall        — a permanent gotcha (API quirk, undocumented behaviour, subtle invariant)
  workaround     — useful now; may become obsolete when better tooling exists
  recommendation — points to the right approach, tool, or pattern
  pattern        — a recurring successful approach worth repeating
  ""             — leave empty when the memory does not clearly fit any category
```

### Conversion from `proposedMemory` to `MemoryDoc`

In the loop that converts `proposedMemory` → `MemoryDoc` (find it in `Mine()` or `MineAuto()`), set:

```go
doc.Meta.Kind       = p.Kind
doc.Meta.Action     = p.Action
doc.Meta.Confidence = 0.5
```

### `MineAuto()` digest call

Add `s.WriteDigest(filepath.Join(s.dir, "DIGEST.md"))` after the batch save loop in `MineAuto()`.

### Tests to run

```bash
go test -run TestMiner
go test -run TestMineAuto
```

Add a test that feeds a session containing a clear pitfall (e.g., a git error and resolution) to a mock LLM that returns a `kind: "pitfall"` and non-empty `action`; verify both fields are present on the saved `MemoryDoc`.

---

## Step 4 — Terminal command update (`terminal.go`)

### Find `/memory flag`

Search `terminal.go` for the dispatch that handles `/memory flag`. It currently calls `store.Archive(id)` or similar.

### Update behaviour

Replace the archive call with:

```go
newConf, err := store.SetConfidence(id, -0.1)
if errors.Is(err, ErrAutoArchived) {
    fmt.Fprintf(out, "%s: confidence → archived (fell below threshold)\n", id)
} else if err != nil {
    fmt.Fprintf(out, "error: %v\n", err)
} else {
    fmt.Fprintf(out, "%s: confidence → %.1f\n", id, newConf)
}
```

### Update `/memory archive`

If the existing command for explicit archival is `/memory flag --force` or similar, rename or add `/memory archive <id>` that calls `store.Archive(id)` directly. If a separate archive command already exists, leave it unchanged.

### Tests to run

```bash
go test -run TestMemoryCommands
go test -run TestTerminal
```

Confirm that `/memory flag` on a memory with confidence 0.5 leaves it active with confidence 0.4, and that repeated flags eventually archive it.

---

## Step 5 — Integration smoke test

Run the full test suite from inside `harvey/`:

```bash
go test ./...
go test -race ./...
```

Confirm:
- All existing tests pass
- No data races
- The three new fields round-trip through `Bytes()` / `ParseMemoryDoc()`
- `DIGEST.md` is written to a temp dir and contains expected sections

---

## Step 6 — Harvey memory skill (new file)

Create `agents/skills/harvey-memory/SKILL.md`:

```markdown
---
name: harvey-memory
description: Load Harvey's accumulated project memories before working on this workspace.
trigger: "before making changes to this project | at session start | when asked about project history"
---

# Harvey Memory Skill

Harvey (the local coding agent for this workspace) accumulates experience memories across sessions. These memories capture tool-use pitfalls, workflow patterns, user preferences, and project facts specific to this workspace.

## When to use this skill

Load this skill:
- At the start of a session when working on the Laboratory or harvey sub-project
- Before making changes to files Harvey has worked on previously
- When the user asks about past decisions or accumulated knowledge

## Reading the memory digest

The live memory index is at `agents/memories/DIGEST.md`. Read it with your file-read tool. It is updated automatically by Harvey whenever memories are saved or archived.

## Memory structure

Each memory has:
- **type** — the topic category (tool_use, workflow, user_preference, workspace_profile, project_fact)
- **kind** — why this knowledge matters:
  - `pitfall` — a permanent gotcha; treat as a hard constraint
  - `workaround` — useful now; verify it still applies before acting on it
  - `recommendation` — preferred approach; follow unless you have a strong reason not to
  - `pattern` — a successful recurring approach; use as a template
  - unclassified — treat with neutral confidence
- **confidence** — reliability score (0.0–1.0). Default for new memories is 0.5.
  - 0.8–1.0: high confidence; act on it directly
  - 0.5–0.7: moderate; use as guidance but verify
  - below 0.5: low; treat as a weak suggestion

## Acting on memories

- `pitfall` memories with confidence >= 0.8: check for this condition before acting
- `workaround` memories: apply the action, but note that a better approach may exist
- `recommendation` memories: follow the action unless context clearly overrides it
- Low-confidence memories (< 0.5): mention to the user before acting on them

## Source files

Individual memory documents are in `agents/memories/<type>/<id>.fountain`. They contain the full conversation context in Fountain screenplay format.

## Flagging stale memories

If a memory appears incorrect, tell the user:
> "Memory `<id>` may be stale. You can update it with `/memory flag <id>` in Harvey."

Do not modify memory files directly.
```

---

## Step 7 — Update agent instruction files

### `VIBE.md`

Add a new section "Harvey memories" after the existing "Knowledge base" section:

```markdown
### Harvey memories

Harvey accumulates experience memories across sessions. These capture workspace-specific
tool-use pitfalls, workflow patterns, user preferences, and project facts.

**Quick reference:** Read `agents/memories/DIGEST.md` — a plain-text index updated
automatically whenever Harvey mines or saves a memory. Each entry shows the memory's
type, kind (pitfall/workaround/recommendation/pattern), confidence score, description,
and action.

**Full skill:** `agents/skills/harvey-memory/SKILL.md` — load this at session start
when working on Harvey or Laboratory experiments for full guidance on interpreting
confidence values and acting on different memory kinds.

Memory types:
- `tool_use` — tool-specific tricks and error fixes
- `workflow` — multi-step process patterns
- `user_preference` — how the user likes to work
- `workspace_profile` — key facts about this workspace
- `project_fact` — important project decisions and constraints
```

### `CLAUDE.md` (root)

Add the same "Harvey memories" section under the existing "Knowledge base" section in the Claude.md at the Laboratory root. The wording can be identical to the VIBE.md addition.

---

## Checklist

- [ ] Step 1: `memory.go` — add Kind, Action, Confidence; update EmbedText, NewMemoryDoc, ParseMemoryDoc
- [ ] Step 2: `memory_store.go` — schema, migration, Save, Query, SearchFTS, SetConfidence, WriteDigest, rebuildIfNeeded
- [ ] Step 3: `memory_miner.go` — proposedMemory, minerSystemPrompt, conversion loop, MineAuto digest call
- [ ] Step 4: `terminal.go` — `/memory flag` confidence adjustment
- [ ] Step 5: `go test -race ./...` passes clean
- [ ] Step 6: `agents/skills/harvey-memory/SKILL.md` created
- [ ] Step 7: `VIBE.md` and root `CLAUDE.md` updated
