---
name: update-knowledge-base
description: |
  Adds or updates records in knowledge_base.db. Supports adding experiments,
  observations, concepts, and linking concepts to experiments or observations.
  Use when the user asks to "add an experiment", "record an observation",
  "add a concept", or "link a concept to an experiment".
compatibility: claude-code, harvey
metadata:
  author: R. S. Doiel
  version: 0.1.0
variables:
  DB_PATH:
    type: string
    description: Path to the SQLite3 database file. Defaults to knowledge_base.db.
    example: "knowledge_base.db"
  ACTION:
    type: string
    description: "What to add: experiment | observation | concept | link-experiment-concept | link-observation-concept"
    example: "observation"
---

This skill inserts or updates records in `knowledge_base.db`. It determines
what to add, prompts for the required fields, and confirms success. Run
`setup-knowledge-base` first if the database does not yet exist.

---

## Variables

| Variable  | Type   | Description                                    | Default             | Prompted? |
|-----------|--------|------------------------------------------------|---------------------|-----------|
| `DB_PATH` | string | Path to the SQLite3 database file.             | `knowledge_base.db` | No        |
| `ACTION`  | string | The type of record to add (see table below).   | —                   | Yes       |

### ACTION values

| Value                       | What it does                                                  |
|-----------------------------|---------------------------------------------------------------|
| `experiment`                | Inserts a row into `experiments`                              |
| `observation`               | Inserts a row into `observations` for a named experiment      |
| `concept`                   | Inserts a row into `concepts`                                 |
| `link-experiment-concept`   | Inserts a row into `experiment_concepts`                      |
| `link-observation-concept`  | Inserts a row into `observation_concepts`                     |

---

## Step 1 — Resolve DB_PATH and verify database exists

```bash
DB_PATH="${DB_PATH:-knowledge_base.db}"
```

If the file does not exist, report the error and suggest running `setup-knowledge-base`.

---

## Step 2 — Determine ACTION

If `ACTION` is not set, ask the user:

> What would you like to add?
> 1. experiment
> 2. observation
> 3. concept
> 4. link-experiment-concept
> 5. link-observation-concept

---

## Step 3 — Gather fields and insert

### ACTION = experiment

Prompt for:
- `NAME` — unique short identifier (filesystem-safe, no spaces)
- `DESCRIPTION` — one-sentence description (optional)
- `STATUS` — one of: `concept`, `active`, `paused`, `concluded` (default: `concept`)
- `LANGUAGE` — primary language(s) (optional, e.g. `TypeScript, SQL`)
- `REPO_URL` — git repository URL (optional)

```sql
INSERT INTO experiments (name, description, status, language, repo_url)
VALUES ('NAME', 'DESCRIPTION', 'STATUS', 'LANGUAGE', 'REPO_URL');
```

---

### ACTION = observation

First show the experiment list so the user can pick one:

```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT id, name, status FROM experiments ORDER BY id;"
```

Prompt for:
- `EXPERIMENT_ID` — integer id from the list above
- `KIND` — one of: `note`, `finding`, `decision`, `question`, `hypothesis` (default: `note`)
- `BODY` — the observation text (may be multi-line; collect until the user signals done)

```sql
INSERT INTO observations (experiment_id, kind, body)
VALUES (EXPERIMENT_ID, 'KIND', 'BODY');
```

---

### ACTION = concept

Prompt for:
- `CONCEPT_NAME` — unique name for the concept
- `DEFINITION` — one-sentence definition (optional)

```sql
INSERT INTO concepts (name, definition)
VALUES ('CONCEPT_NAME', 'DEFINITION');
```

If the name already exists, report the conflict and offer to update the definition instead:

```sql
UPDATE concepts SET definition = 'DEFINITION'
WHERE name = 'CONCEPT_NAME';
```

---

### ACTION = link-experiment-concept

Show experiments and concepts so the user can pick:

```bash
sqlite3 -header -column "${DB_PATH}" "SELECT id, name FROM experiments ORDER BY id;"
sqlite3 -header -column "${DB_PATH}" "SELECT id, name FROM concepts ORDER BY id;"
```

Prompt for:
- `EXPERIMENT_ID`
- `CONCEPT_ID`

```sql
INSERT OR IGNORE INTO experiment_concepts (experiment_id, concept_id)
VALUES (EXPERIMENT_ID, CONCEPT_ID);
```

---

### ACTION = link-observation-concept

Show recent observations and concepts:

```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT o.id, e.name AS experiment, o.kind, substr(o.body,1,60) AS body_preview
   FROM observations o JOIN experiments e ON e.id = o.experiment_id
   ORDER BY o.id DESC LIMIT 20;"
sqlite3 -header -column "${DB_PATH}" "SELECT id, name FROM concepts ORDER BY id;"
```

Prompt for:
- `OBSERVATION_ID`
- `CONCEPT_ID`

```sql
INSERT OR IGNORE INTO observation_concepts (observation_id, concept_id)
VALUES (OBSERVATION_ID, CONCEPT_ID);
```

---

## Step 4 — Confirm success

After each insert, report the row that was written:

```bash
# For experiments:
sqlite3 -header -column "${DB_PATH}" \
  "SELECT * FROM experiments WHERE name = 'NAME';"

# For observations:
sqlite3 -header -column "${DB_PATH}" \
  "SELECT * FROM observations WHERE id = last_insert_rowid();"

# For concepts:
sqlite3 -header -column "${DB_PATH}" \
  "SELECT * FROM concepts WHERE name = 'CONCEPT_NAME';"
```

---

## Error handling

| Condition                        | Action                                                              |
|----------------------------------|---------------------------------------------------------------------|
| `DB_PATH` does not exist         | Report; suggest running `setup-knowledge-base` first               |
| `sqlite3` not found              | Report that SQLite3 must be installed                               |
| UNIQUE constraint violation      | Report the duplicate and offer to update or skip                    |
| Invalid `STATUS` or `KIND` value | Report valid values and prompt again                                |
| `EXPERIMENT_ID` not found        | Show the experiment list again and prompt for a valid id            |
