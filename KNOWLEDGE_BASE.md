# Harvey Knowledge Base

*Version 1.0 — Complete guide to Harvey's SQLite-backed knowledge management system*

---

## Overview

Harvey's **Knowledge Base** is a SQLite3-backed store for **projects**, **observations**, and **concepts** within a Harvey workspace. It enables:

- **Project tracking** — Organize work into named projects with status and descriptions
- **Observation recording** — Capture notes, findings, decisions, questions, and hypotheses
- **Concept linking** — Tag entities with named concepts for categorization and discovery
- **Full-text search** — Fast, relevance-ranked search across all content using FTS5
- **Markdown export** — Generate formatted reports for conversation context

### Core Entities

```
┌─────────────────────────────────────────────────────────────────────┐
│                         KNOWLEDGE BASE SCHEMA                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────┐       ┌─────────────────┐       ┌────────────┐    │
│  │   PROJECTS   │       │  OBSERVATIONS   │       │  CONCEPTS  │    │
│  ├──────────────┤       ├─────────────────┤       ├────────────┤    │
│  │ id           │◄──────│ project_id (FK) │       │ id         │    │
│  │ name         │       │ kind            │       │ name       │    │
│  │ description  │       │ body            │       │ description│    │
│  │ status       │       │ created_at      │       │ created_at │    │
│  │ created_at   │       └─────────────────┘       └────────────┘    │
│  │ updated_at   │              ▲ ▲                              ▲   │
│  └──────────────┘              │ │                              │   │
│       ▲                        │ └──────────────────────────────┘   │
│       │                        │                                    │
│       └────────────────────────┘ (project_id FK)                    │
│                                                                     │
│  ┌────────────────────────────────────────┐   ┌──────────────────┐  │
│  │         JUNCTION TABLES                │   │   FTS5 INDEX     │  │
│  ├────────────────────────────────────────┤   ├──────────────────┤  │
│  │ observation_concepts (obs_id, conc_id) │   │ kb_fts           │  │
│  │ project_concepts (proj_id, conc_id)    │   │ (virtual table)  │  │
│  └────────────────────────────────────────┘   └──────────────────┘  │
│                                                                     │
│  ┌────────────────────────────────────────┐                         │
│  │ project_summary (VIEW)                 │                         │
│  │ - Aggregates projects with concepts    │                         │
│  └────────────────────────────────────────┘                         │
└─────────────────────────────────────────────────────────────────────┘
```

### Database Location

The knowledge base file is located at:

```
<workspace>/harvey/knowledge.db
```

Or at a custom path specified in `harvey.yaml`:

```yaml
knowledge_base:
  path: custom/path/knowledge.db
```

## Quick Start

### Create Your First Project

```bash
# In Harvey REPL:
harvey> /kb project add "My Project" "A test project"

# Or programmatically in Go:
kb, _ := OpenKnowledgeBase(ws, "")
defer kb.Close()

projectID, err := kb.AddProject("My Project", "A test project")
```

### Add Observations

```bash
# In Harvey REPL:
harvey> /kb obs add <project-id> finding "Discovered a critical bug in parser"

# Or in Go:
obsID, err := kb.AddObservation(projectID, "finding", "Discovered a critical bug in parser")
```

### Define and Link Concepts

```bash
# In Harvey REPL:
harvey> /kb concept add "parser bug" "Issues with the token parser module"
harvey> /kb link obs <obs-id> concept "parser bug"

# Or in Go:
conceptID, _ := kb.AddConcept("parser bug", "Issues with the token parser module")
err := kb.LinkObservationConcept(obsID, conceptID)
```

### Search the Knowledge Base

```bash
# In Harvey REPL:
harvey> /kb search "parser bug"

# Or in Go:
results, err := kb.Search("parser bug")
for _, r := range results {
    fmt.Printf("[%s] %s — %s\n", r.Kind, r.Label, r.Snippet)
}
```

### View Summary

```bash
# In Harvey REPL:
harvey> /kb summary

# Or in Go:
summary, err := kb.Summary()
fmt.Println(summary)
```

### Export as Markdown

```bash
# In Harvey REPL:
harvey> /kb export

# Or in Go:
md, err := kb.FormatMarkdown(0)  // 0 = all projects
fmt.Println(md)
```

## Database Schema

### Tables

#### `projects`

Stores top-level projects/containers for observations.

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| `id` | INTEGER | NO | AUTOINCREMENT | Primary key |
| `name` | TEXT | NO | - | Unique project name |
| `description` | TEXT | NO | `''` | Human-readable description |
| `status` | TEXT | NO | `'active'` | Project status (active, archived, etc.) |
| `created_at` | DATETIME | NO | CURRENT_TIMESTAMP | Creation timestamp |
| `updated_at` | DATETIME | NO | CURRENT_TIMESTAMP | Last update timestamp |

**Constraints:**
- `name` must be unique
- `status` typical values: `active`, `archived`, `planned`, `completed`

#### `observations`

Stores individual notes, findings, decisions, questions, or hypotheses.

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| `id` | INTEGER | NO | AUTOINCREMENT | Primary key |
| `project_id` | INTEGER | NO | - | Foreign key to projects(id), ON DELETE CASCADE |
| `kind` | TEXT | NO | `'note'` | Observation type |
| `body` | TEXT | NO | - | The observation text content |
| `created_at` | DATETIME | NO | CURRENT_TIMESTAMP | Creation timestamp |

**Valid `kind` values:**
- `note` — General note or comment
- `finding` — Discovered fact or result
- `decision` — Choice made during work
- `question` — Open question to investigate
- `hypothesis` — Proposed explanation or approach

#### `concepts`

Stores named ideas or terms that can be linked to projects and observations.

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| `id` | INTEGER | NO | AUTOINCREMENT | Primary key |
| `name` | TEXT | NO | - | Unique concept name |
| `description` | TEXT | NO | `''` | Human-readable explanation |
| `created_at` | DATETIME | NO | CURRENT_TIMESTAMP | Creation timestamp |

**Constraints:**
- `name` must be unique

### Junction Tables

#### `observation_concepts`

Many-to-many relationship between observations and concepts.

| Column | Type | Description |
|--------|------|-------------|
| `observation_id` | INTEGER | Foreign key to observations(id), ON DELETE CASCADE |
| `concept_id` | INTEGER | Foreign key to concepts(id), ON DELETE CASCADE |

**Primary Key:** `(observation_id, concept_id)` — duplicate links are silently ignored

#### `project_concepts`

Many-to-many relationship between projects and concepts.

| Column | Type | Description |
|--------|------|-------------|
| `project_id` | INTEGER | Foreign key to projects(id), ON DELETE CASCADE |
| `concept_id` | INTEGER | Foreign key to concepts(id), ON DELETE CASCADE |

**Primary Key:** `(project_id, concept_id)` — duplicate links are silently ignored

### Views

#### `project_summary`

Aggregates project data with linked concepts for summary display.

```sql
SELECT p.id,
       p.name,
       p.status,
       p.description,
       GROUP_CONCAT(c.name, ', ') AS concepts
FROM   projects p
LEFT JOIN project_concepts pc ON pc.project_id = p.id
LEFT JOIN concepts c          ON c.id = pc.concept_id
GROUP BY p.id;
```

### Full-Text Search Index

#### `kb_fts` (FTS5 Virtual Table)

Full-text search index across observations, projects, and concepts.

| Column | Type | Indexed | Description |
|--------|------|---------|-------------|
| `body` | TEXT | YES | Main searchable content |
| `kind` | TEXT | YES | Entity type (project, observation, concept) |
| `label` | TEXT | NO | Entity name (unindexed) |
| `descr` | TEXT | NO | Description (unindexed) |
| `source_type` | TEXT | NO | Source entity type (unindexed) |
| `source_id` | INTEGER | NO | Source entity ID (unindexed) |
| `project_id` | INTEGER | NO | Associated project ID (unindexed) |

**FTS5 Configuration:**
- Uses SQLite's FTS5 extension with BM25 ranking
- Tokenizes and indexes `body` and `kind` fields
- Unindexed columns are stored but not searchable
- Automatically rebuilt if empty but source tables have data

## Go API Reference

### Types

#### `KnowledgeBase`

The main handle for the knowledge base.

```go
type KnowledgeBase struct {
    // Internal fields - use methods for access
}

// OpenKnowledgeBase opens (or creates) the SQLite knowledge base.
// customPath overrides the default location (harvey/knowledge.db).
func OpenKnowledgeBase(ws *Workspace, customPath string) (*KnowledgeBase, error)

// Close releases the database connection.
func (kb *KnowledgeBase) Close() error

// Path returns the absolute path of the database file.
func (kb *KnowledgeBase) Path() string
```

#### `Project`

Represents a project row.

```go
type Project struct {
    ID          int64
    Name        string
    Description string
    Status      string
    CreatedAt   time.Time
}
```

#### `Observation`

Represents an observation row.

```go
type Observation struct {
    ID        int64
    ProjectID int64
    Kind      string    // one of: note, finding, decision, question, hypothesis
    Body      string
    CreatedAt time.Time
}
```

#### `Concept`

Represents a concept row.

```go
type Concept struct {
    ID          int64
    Name        string
    Description string
}
```

#### `KBSearchResult`

Represents a full-text search result.

```go
type KBSearchResult struct {
    Kind    string // observation kind or "project" / "concept"
    Label   string // project name for observations; entity name for others
    Snippet string // observation body; or description for projects/concepts
}
```

### Project Operations

| Method | Description |
|--------|-------------|
| `AddProject(name, description string) (int64, error)` | Insert new project, return ID (or existing if name conflicts) |
| `Projects() ([]Project, error)` | Return all projects ordered by creation date |
| `ProjectByName(name string) (*Project, error)` | Return project by exact name match, or nil |
| `ProjectConcepts(projectID int64) ([]Concept, error)` | Return all concepts linked to a project |

### Observation Operations

| Method | Description |
|--------|-------------|
| `AddObservation(projectID int64, kind, body string) (int64, error)` | Insert new observation, return ID |
| `Observations(projectID int64) ([]Observation, error)` | Return all observations for project, newest first |
| `LinkObservationConcept(observationID, conceptID int64) error` | Associate observation with concept |

**Valid Observation Kinds:**
```go
var ValidObservationKinds = []string{"note", "finding", "decision", "question", "hypothesis"}
```

### Concept Operations

| Method | Description |
|--------|-------------|
| `AddConcept(name, description string) (int64, error)` | Insert new concept, return ID (or existing if name conflicts) |
| `Concepts() ([]Concept, error)` | Return all concepts ordered by name |
| `LinkProjectConcept(projectID, conceptID int64) error` | Associate project with concept |

### Query and Export Operations

| Method | Description |
|--------|-------------|
| `Search(term string) ([]KBSearchResult, error)` | Full-text search with BM25 ranking, limited to 50 results |
| `Summary() (string, error)` | Human-readable text summary of all projects and recent observations |
| `FormatMarkdown(projectID int64) (string, error)` | Export as Markdown (projectID=0 for all projects) |

## FTS5 Full-Text Search

### Query Syntax

Harvey uses SQLite's FTS5 with standard query syntax:

| Syntax | Example | Meaning |
|--------|---------|---------|
| Single word | `parser` | Match documents containing "parser" |
| Multiple words | `parser bug` | Match documents containing BOTH words (AND) |
| Phrase | `"parser bug"` | Match exact phrase |
| Prefix | `pars*` | Match words starting with "pars" |
| OR | `parser OR tokenizer` | Match either word |
| NOT | `parser NOT bug` | Match "parser" but not "bug" |

### Ranking

Results are ranked using the **BM25** algorithm, which considers:
- Term frequency (how often the term appears in the document)
- Inverse document frequency (how rare the term is across all documents)
- Document length normalization

### Query Example

```go
results, err := kb.Search("parser bug")
// Returns observations, projects, and concepts containing both "parser" and "bug"
// Sorted by relevance score (highest first)
// Limited to 50 results
```

### FTS5 Availability

FTS5 is compiled into SQLite by default in most distributions. If unavailable:
- The knowledge base will still open and function
- `Search()` will return `ErrFTSUnavailable`
- The FTS index will be automatically rebuilt when FTS5 becomes available

## CLI Commands

Harvey provides CLI commands for knowledge base operations:

### Project Commands

| Command | Description |
|---------|-------------|
| `/kb project add <name> [description]` | Create a new project |
| `/kb project list` | List all projects |
| `/kb project info <name-or-id>` | Show project details |
| `/kb project status <name> <status>` | Update project status |

### Observation Commands

| Command | Description |
|---------|-------------|
| `/kb obs add <project> <kind> <body>` | Add observation to project (by name or ID) |
| `/kb obs list <project>` | List observations for project |
| `/kb obs info <id>` | Show observation details |

**Kind shorthand:**
- `n` or `note` → note
- `f` or `finding` → finding
- `d` or `decision` → decision
- `q` or `question` → question
- `h` or `hypothesis` → hypothesis

### Concept Commands

| Command | Description |
|---------|-------------|
| `/kb concept add <name> [description]` | Create a new concept |
| `/kb concept list` | List all concepts |
| `/kb concept info <name>` | Show concept details |

### Link Commands

| Command | Description |
|---------|-------------|
| `/kb link obs <obs-id> concept <concept-name>` | Link observation to concept |
| `/kb link project <proj-name> concept <concept-name>` | Link project to concept |

### Query Commands

| Command | Description |
|---------|-------------|
| `/kb search <query>` | Full-text search across all content |
| `/kb summary` | Show formatted summary of all projects |
| `/kb export [project]` | Export as Markdown (all projects or specified) |

## Data Model Relationships

### Entity Relationship Diagram (Text)

```
Projects (1) ----(*) Observations
    |                   |
    +------(*)--------+------(*)---- Concepts
    |                               |
    (project_concepts)              (observation_concepts)
```

### Cardinality

- **1 Project** can have **many Observations**
- **1 Project** can be linked to **many Concepts** (via `project_concepts`)
- **1 Observation** belongs to **1 Project**
- **1 Observation** can be linked to **many Concepts** (via `observation_concepts`)
- **1 Concept** can be linked to **many Projects** and **many Observations**

### Cascade Deletion

When a Project is deleted:
- All its Observations are deleted (CASCADE on `observations.project_id`)
- All its links in `project_concepts` are deleted (CASCADE)
- Observation links in `observation_concepts` for those observations are deleted (CASCADE)

When a Concept is deleted:
- All its links in `project_concepts` are deleted (CASCADE)
- All its links in `observation_concepts` are deleted (CASCADE)

**Note:** Concepts and Projects are NOT automatically deleted when their links are removed.

## Examples

### Example 1: Research Project Workflow

```go
// Create a research project
projID, _ := kb.AddProject("LLM Research", "Investigating prompt injection vulnerabilities")

// Add concepts
tokenizerID, _ := kb.AddConcept("Tokenizer", "Text tokenization algorithm")
injectionID, _ := kb.AddConcept("Prompt Injection", "Security vulnerability in LLM prompts")

// Link project to concepts
_ = kb.LinkProjectConcept(projID, tokenizerID)
_ = kb.LinkProjectConcept(projID, injectionID)

// Record observations
obs1, _ := kb.AddObservation(projID, "finding", "Discovered new injection vector using Unicode homoglyphs")
obs2, _ := kb.AddObservation(projID, "hypothesis", "This might affect all transformer models")

// Link observations to concepts
_ = kb.LinkObservationConcept(obs1, injectionID)
_ = kb.LinkObservationConcept(obs2, injectionID)
_ = kb.LinkObservationConcept(obs2, tokenizerID)

// Search for related content
results, _ := kb.Search("injection")
// Returns the project, both observations, and the concept
```

### Example 2: Markdown Export for Context

```go
// Export entire knowledge base as Markdown
md, _ := kb.FormatMarkdown(0)
// Output:
// # Knowledge Base
//
// ## Project: LLM Research
//
// Investigating prompt injection vulnerabilities
//
// **Concepts:** Prompt Injection, Tokenizer
//
// ** [finding]** Discovered new injection vector using Unicode homoglyphs
//
// ** [hypothesis]** This might affect all transformer models

// Use in Harvey REPL context
// /kb export
// (content is injected into conversation)
```

### Example 3: Full-Text Search Patterns

```go
// Simple word search
results, _ := kb.Search("vulnerability")

// Phrase search
results, _ := kb.Search("prompt injection")

// Prefix search for autocomplete
results, _ := kb.Search("injec*")

// Boolean search
results, _ := kb.Search("security AND vulnerability")
results, _ := kb.Search("research NOT completed")

// Rank by relevance (BM25)
// Results are already sorted by score, highest first
for _, r := range results {
    fmt.Printf("[%s] %s\n", r.Kind, r.Label)
}
```

## Best Practices

### Organizing Projects

1. **Use meaningful names** — Project names should be descriptive and unique
2. **Set status appropriately** — Use `active`, `archived`, `planned`, `completed` consistently
3. **Write good descriptions** — Descriptions help with search and understanding
4. **Link concepts early** — Tag projects with relevant concepts as soon as they're created

### Writing Observations

1. **Choose the right kind** — Use `finding` for facts, `decision` for choices, `question` for unknowns
2. **Be concise but complete** — Observations should be self-contained and understandable
3. **Link to concepts** — Always link observations to relevant concepts for discoverability
4. **Use consistent terminology** — This improves full-text search results

### Managing Concepts

1. **Define concepts broadly** — Concepts should be reusable across multiple projects
2. **Write clear descriptions** — Helps others understand what the concept means
3. **Use hierarchical naming** — Consider `Machine Learning::Neural Networks::Transformers` style names
4. **Link comprehensively** — Link concepts to all relevant projects and observations

### Search Tips

1. **Use specific terms** — More specific queries return better results
2. **Use phrases for exact matches** — `"machine learning"` vs `machine learning`
3. **Use prefix search for exploration** — `ml*` to find all machine learning related content
4. **Combine terms** — `security vulnerability` returns results with both words

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| `Search()` returns error about FTS5 | SQLite compiled without FTS5 | Recompile SQLite with FTS5, or update Harvey's SQLite driver |
| Observations not appearing in search | FTS index not rebuilt | Restart Harvey to trigger rebuild, or call `rebuildFTSIfNeeded()` |
| Duplicate project name error | Name must be unique | Use a different name or update the existing project |
| Invalid observation kind | Kind not in allowed list | Use one of: note, finding, decision, question, hypothesis |
| Database locked | Multiple connections with WAL mode | Harvey uses `MaxOpenConns(1)` to prevent this |

### FTS5 Not Available

If FTS5 is not compiled into your SQLite:

1. **Check availability:**
   ```go
   kb, _ := OpenKnowledgeBase(ws, "")
   fmt.Println("FTS Available:", kb.ftsAvailable) // false if unavailable
   ```

2. **Install FTS5-enabled SQLite:**
   ```bash
   # On Ubuntu/Debian
   sudo apt-get install sqlite3 libsqlite3-dev
   
   # On macOS (with Homebrew)
   brew install sqlite
   ```

3. **Harvey's FTS5 fallback:**
   - The knowledge base will still work
   - `Search()` will return an error
   - All other operations function normally
   - FTS index is automatically rebuilt when FTS5 becomes available

### Database Corruption

If the database file is corrupted:

1. **Backup first:**
   ```bash
   cp harvey/knowledge.db harvey/knowledge.db.bak
   ```

2. **Harvey will auto-repair on open:**
   - Missing tables are recreated from schema
   - Missing FTS index is rebuilt from source tables
   - Invalid data may need manual cleanup

3. **Manual recovery:**
   ```bash
   # Export data first
   sqlite3 harvey/knowledge.db ".dump" > knowledge.dump
   
   # Remove corrupted file
   rm harvey/knowledge.db
   
   # Harvey will create a new database on next start
   # Re-import data from dump if needed
   ```

### Performance Issues

1. **Slow searches:**
   - Ensure FTS5 index is built (`kb_fts` table has data)
   - Check SQLite version (`sqlite3 --version`) — use 3.38+ for best FTS5 performance
   - Limit result count (Harvey limits to 50 by default)

2. **Large databases:**
   - SQLite handles up to 140TB, but WAL mode works best with SSDs
   - Consider archiving old projects (set status to `archived`)
   - Vacuum the database periodically: `sqlite3 knowledge.db VACUUM`

## Migration Guide

### From No Knowledge Base (v0.1 and earlier)

Harvey v0.2+ automatically creates the knowledge base on first use:

1. Start Harvey in your workspace
2. Run `/kb project add` to create your first project
3. The database file `harvey/knowledge.db` will be created automatically

### From File-Based Notes

To migrate from flat files or other note-taking systems:

1. **Create projects** corresponding to your note directories
2. **Import notes as observations** with appropriate kinds
3. **Define concepts** for your common tags/categories
4. **Link observations to concepts** for discoverability

Example migration script:

```go
func MigrateFromMarkdown(kb *KnowledgeBase, dir string) error {
    // Walk through markdown files
    files, _ := os.ReadDir(dir)
    for _, f := range files {
        if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
            content, _ := os.ReadFile(filepath.Join(dir, f.Name()))
            
            // Create project from directory name
            projID, _ := kb.AddProject(dir, "Imported from markdown")
            
            // Add observation from file content
            _, _ = kb.AddObservation(projID, "note", string(content))
        }
    }
    return nil
}
```

### Schema Updates

Harvey automatically applies schema updates on open. The schema is defined in `knowledge.go`:

```go
const schema = `
CREATE TABLE IF NOT EXISTS projects (...);
CREATE TABLE IF NOT EXISTS observations (...);
-- etc
`
```

When Harvey opens the database:
1. It executes the schema SQL
2. `IF NOT EXISTS` prevents errors if tables already exist
3. New columns/tables are added automatically
4. Existing data is preserved

## Reference

### SQLite Version Requirements

- **Minimum:** SQLite 3.18.0 (for `INSERT ... ON CONFLICT`)
- **Recommended:** SQLite 3.38.0+ (for best FTS5 performance)
- **Harvey's driver:** Uses `github.com/glebarez/go-sqlite` (pure-Go, embeds SQLite)

### WAL Mode

Harvey enables WAL (Write-Ahead Logging) mode:
- Better concurrency (readers don't block writers)
- Better performance for write-heavy workloads
- Uses `MaxOpenConns(1)` to prevent lock contention

### Foreign Keys

Foreign key constraints are enabled:
- `PRAGMA foreign_keys = ON`
- CASCADE deletion is automatic
- Inserts must respect foreign key constraints

### Character Encoding

- All text is stored as UTF-8
- SQLite natively supports UTF-8
- No special escaping required for international characters

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) — Configuration file reference
- [ROUTING.md](ROUTING.md) — Remote endpoint routing guide
- [SKILLS.md](SKILLS.md) — Agent Skills system documentation
- [User Manual](user_manual.md) — General Harvey usage

*Documentation generated from knowledge.go source code. Version 1.0.*
