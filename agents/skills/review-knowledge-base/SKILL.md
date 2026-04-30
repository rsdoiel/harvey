---
name: review-knowledge-base
description: |
  Queries knowledge_base.db and delivers a structured report covering
  experiments (by status), recent observations, and concepts. Can drill into
  a single experiment for full detail. Use when the user asks to "review the
  knowledge base", "show experiments", "summarize the knowledge base", or
  "what's in the knowledge base".
compatibility: claude-code, harvey
metadata:
  author: R. S. Doiel
  version: 0.1.0
variables:
  DB_PATH:
    type: string
    description: Path to the SQLite3 database file. Defaults to knowledge_base.db.
    example: "knowledge_base.db"
  FOCUS:
    type: string
    description: "What to report: all | experiments | observations | concepts | <experiment-name>"
    example: "all"
---

This skill reads `knowledge_base.db` and delivers a structured report.
By default (`FOCUS=all`) it covers every section. Set `FOCUS` to a specific
section name or to an experiment name for a focused report.

---

## Variables

| Variable  | Type   | Description                                              | Default             | Prompted? |
|-----------|--------|----------------------------------------------------------|---------------------|-----------|
| `DB_PATH` | string | Path to the SQLite3 database file.                       | `knowledge_base.db` | No        |
| `FOCUS`   | string | Scope of the report (see values below).                  | `all`               | No        |

### FOCUS values

| Value                  | Report scope                                               |
|------------------------|------------------------------------------------------------|
| `all`                  | Full report: experiments, recent observations, concepts    |
| `experiments`          | Experiment summary table only                              |
| `observations`         | Last 20 observations across all experiments               |
| `concepts`             | All concepts with definitions                              |
| `<experiment-name>`    | Full detail for one experiment: metadata + all observations + linked concepts |

---

## Step 1 — Resolve DB_PATH and verify

```bash
DB_PATH="${DB_PATH:-knowledge_base.db}"
FOCUS="${FOCUS:-all}"
```

If the file does not exist, report the error and suggest running `setup-knowledge-base`.

---

## Step 2 — Run queries

### Experiments section (`FOCUS` = `all` or `experiments`)

```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT id, name, status, language, description, concepts
   FROM experiment_summary
   ORDER BY status, name;"
```

Group the output by status and label each group:
- **Active** — `status = 'active'`
- **Concept** — `status = 'concept'`
- **Paused** — `status = 'paused'`
- **Concluded** — `status = 'concluded'`

---

### Observations section (`FOCUS` = `all` or `observations`)

Show the 20 most recent observations across all experiments:

```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT o.id,
          e.name AS experiment,
          o.kind,
          o.recorded_at,
          substr(o.body, 1, 80) AS body_preview
   FROM observations o
   JOIN experiments e ON e.id = o.experiment_id
   ORDER BY o.recorded_at DESC
   LIMIT 20;"
```

For each observation where `body` was truncated, note that the user can ask to
see the full text.

---

### Concepts section (`FOCUS` = `all` or `concepts`)

```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT c.name,
          c.definition,
          count(DISTINCT ec.experiment_id) AS experiments_linked,
          count(DISTINCT oc.observation_id) AS observations_linked
   FROM concepts c
   LEFT JOIN experiment_concepts ec ON ec.concept_id = c.id
   LEFT JOIN observation_concepts oc ON oc.concept_id = c.id
   GROUP BY c.id
   ORDER BY c.name;"
```

---

### Single-experiment detail (`FOCUS` = `<experiment-name>`)

Resolve the experiment id:

```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT * FROM experiments WHERE name = 'FOCUS';"
```

If not found, list all experiment names and ask the user to pick one.

Then run in sequence:

**Linked concepts:**
```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT c.name, c.definition
   FROM concepts c
   JOIN experiment_concepts ec ON ec.concept_id = c.id
   JOIN experiments e ON e.id = ec.experiment_id
   WHERE e.name = 'FOCUS';"
```

**All observations (chronological):**
```bash
sqlite3 -header -column "${DB_PATH}" \
  "SELECT o.id, o.kind, o.recorded_at, o.body
   FROM observations o
   JOIN experiments e ON e.id = o.experiment_id
   WHERE e.name = 'FOCUS'
   ORDER BY o.recorded_at ASC;"
```

---

## Step 3 — Deliver the report

Structure the output as:

```
## Knowledge Base Report  (<date>)

### Experiments  (N total)

**Active (N)**
  …

**Concept (N)**
  …

**Paused / Concluded (N)**
  …

---

### Recent Observations  (last 20)

| id | experiment | kind | recorded_at | body_preview |
|----|------------|------|-------------|--------------|
…

---

### Concepts  (N total)

| name | definition | experiments_linked | observations_linked |
|------|------------|--------------------|---------------------|
…
```

For a single-experiment report use:

```
## Experiment: <name>

Status: … | Language: … | Repo: …
Description: …
Concepts: …

### Observations  (N total)

[chronological list]
```

---

## Step 4 — Offer follow-up actions

After the report, offer:

> Would you like to:
> - Add an observation or experiment? → run `update-knowledge-base`
> - See the full text of a specific observation? Just ask with its id.
> - Drill into a specific experiment? Ask me to review `<name>`.

---

## Error handling

| Condition                  | Action                                                       |
|----------------------------|--------------------------------------------------------------|
| `DB_PATH` does not exist   | Report; suggest running `setup-knowledge-base` first         |
| `sqlite3` not found        | Report that SQLite3 must be installed                        |
| Database is empty          | Report "No records yet" for each section; offer to add data  |
| Experiment name not found  | List all experiment names and ask the user to choose         |
