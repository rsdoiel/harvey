package harvey

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite" // pure-Go SQLite driver; registers "sqlite" with database/sql
)

// schema is the DDL applied to a new or existing knowledge base.
const schema = `
CREATE TABLE IF NOT EXISTS projects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL DEFAULT 'active',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS observations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL DEFAULT 'note',
    body       TEXT    NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS concepts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS observation_concepts (
    observation_id INTEGER REFERENCES observations(id) ON DELETE CASCADE,
    concept_id     INTEGER REFERENCES concepts(id)     ON DELETE CASCADE,
    PRIMARY KEY (observation_id, concept_id)
);

CREATE TABLE IF NOT EXISTS project_concepts (
    project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
    concept_id INTEGER REFERENCES concepts(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, concept_id)
);

CREATE VIEW IF NOT EXISTS project_summary AS
    SELECT p.id,
           p.name,
           p.status,
           p.description,
           GROUP_CONCAT(c.name, ', ') AS concepts
    FROM   projects p
    LEFT JOIN project_concepts pc ON pc.project_id = p.id
    LEFT JOIN concepts c          ON c.id = pc.concept_id
    GROUP BY p.id;

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
`

// ftsSchema creates the FTS5 virtual table used by Search. It is applied
// separately from the main schema so that a missing FTS5 compile flag does not
// prevent the knowledge base from opening.
const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS kb_fts USING fts5(
    body,
    kind,
    label       UNINDEXED,
    descr       UNINDEXED,
    source_type UNINDEXED,
    source_id   UNINDEXED,
    project_id  UNINDEXED
);
`

/** KnowledgeBase is a SQLite3-backed store for projects, observations, and
 * concepts within a Harvey workspace. The database file lives at
 * <workspace>/harvey/knowledge.db (or a path overridden in harvey.yaml) and
 * is created automatically on first use.
 *
 * Example:
 *   kb, err := OpenKnowledgeBase(ws, "")
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   defer kb.Close()
 */
type KnowledgeBase struct {
	db           *sql.DB
	path         string // absolute path to the SQLite file
	ftsAvailable bool   // true when the FTS5 virtual table was successfully created
}

// Path returns the absolute path of the open knowledge base file.
func (kb *KnowledgeBase) Path() string { return kb.path }

/** Project represents a single project row in the knowledge base.
 *
 * Example:
 *   projects, err := kb.Projects()
 *   for _, p := range projects {
 *       fmt.Printf("%d  %s  [%s]\n", p.ID, p.Name, p.Status)
 *   }
 */
type Project struct {
	ID          int64
	Name        string
	Description string
	Status      string
	CreatedAt   time.Time
}

/** Observation represents a single timestamped note, finding, decision,
 * question, or hypothesis attached to a project.
 *
 * Example:
 *   obs, err := kb.Observations(projectID)
 *   for _, o := range obs {
 *       fmt.Printf("[%s] %s\n", o.Kind, o.Body)
 *   }
 */
type Observation struct {
	ID        int64
	ProjectID int64
	Kind      string
	Body      string
	CreatedAt time.Time
}

/** Concept represents a named idea or term that can be linked to projects and
 * observations.
 *
 * Example:
 *   concepts, err := kb.Concepts()
 *   for _, c := range concepts {
 *       fmt.Println(c.Name, "-", c.Description)
 *   }
 */
type Concept struct {
	ID          int64
	Name        string
	Description string
}

/** OpenKnowledgeBase opens (or creates) the SQLite knowledge base. customPath
 * overrides the default location (harvey/knowledge.db inside the workspace);
 * pass an empty string to use the default. The schema is applied on every open
 * so that tables are created on first use without manual migration.
 *
 * Parameters:
 *   ws         (*Workspace) — the Harvey workspace that owns the database file.
 *   customPath (string)     — override path; empty = harvey/knowledge.db.
 *
 * Returns:
 *   *KnowledgeBase — ready-to-use knowledge base handle.
 *   error          — if the database file cannot be opened or the schema
 *                    cannot be applied.
 *
 * Example:
 *   kb, err := OpenKnowledgeBase(ws, "")
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   defer kb.Close()
 */
func OpenKnowledgeBase(ws *Workspace, customPath string) (*KnowledgeBase, error) {
	var dbPath string
	if customPath != "" {
		if filepath.IsAbs(customPath) {
			dbPath = customPath
		} else {
			var err error
			dbPath, err = ws.AbsPath(customPath)
			if err != nil {
				return nil, err
			}
		}
	} else {
		var err error
		dbPath, err = ws.AbsPath(harveySubdir + "/knowledge.db")
		if err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge: open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1) // SQLite WAL works best with a single writer
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("knowledge: apply schema: %w", err)
	}
	kb := &KnowledgeBase{db: db, path: dbPath}
	if _, err := db.Exec(ftsSchema); err == nil {
		kb.ftsAvailable = true
		_ = kb.rebuildFTSIfNeeded()
	}
	return kb, nil
}

/** Close releases the database connection. It should be deferred immediately
 * after a successful OpenKnowledgeBase call.
 *
 * Returns:
 *   error — from the underlying sql.DB.Close call.
 *
 * Example:
 *   kb, _ := OpenKnowledgeBase(ws)
 *   defer kb.Close()
 */
func (kb *KnowledgeBase) Close() error {
	return kb.db.Close()
}

// ─── Projects ────────────────────────────────────────────────────────────────

/** AddProject inserts a new project row and returns its auto-assigned ID. If a
 * project with the same name already exists, its ID is returned instead.
 *
 * Parameters:
 *   name        (string) — unique project name.
 *   description (string) — short human-readable description.
 *
 * Returns:
 *   int64 — ID of the inserted or existing project.
 *   error — on database failure.
 *
 * Example:
 *   id, err := kb.AddProject("harvey", "Terminal coding agent backed by Ollama")
 */
func (kb *KnowledgeBase) AddProject(name, description string) (int64, error) {
	var id int64
	err := kb.db.QueryRow(
		`INSERT INTO projects (name, description) VALUES (?, ?)
		 ON CONFLICT(name) DO UPDATE SET updated_at = CURRENT_TIMESTAMP
		 RETURNING id`,
		name, description,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("knowledge: add project: %w", err)
	}
	if kb.ftsAvailable {
		_, _ = kb.db.Exec(
			`DELETE FROM kb_fts WHERE source_type = 'project' AND source_id = ?`, id)
		_, _ = kb.db.Exec(
			`INSERT INTO kb_fts(body, kind, label, descr, source_type, source_id, project_id)
			 VALUES (?, 'project', ?, ?, 'project', ?, ?)`,
			name+" "+description, name, description, id, id)
	}
	return id, nil
}

/** Projects returns all projects ordered by creation date.
 *
 * Returns:
 *   []Project — slice of all project rows; empty (not nil) if none exist.
 *   error     — on database failure.
 *
 * Example:
 *   projects, err := kb.Projects()
 *   for _, p := range projects {
 *       fmt.Println(p.Name, p.Status)
 *   }
 */
func (kb *KnowledgeBase) Projects() ([]Project, error) {
	rows, err := kb.db.Query(
		`SELECT id, name, description, status, created_at FROM projects ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		var ts string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &ts); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		out = append(out, p)
	}
	if out == nil {
		out = []Project{}
	}
	return out, rows.Err()
}

// ─── Observations ────────────────────────────────────────────────────────────

// ValidObservationKinds lists the accepted values for Observation.Kind.
var ValidObservationKinds = []string{"note", "finding", "decision", "question", "hypothesis"}

/** AddObservation inserts a new observation for a project and returns its ID.
 *
 * Parameters:
 *   projectID (int64)  — ID of the owning project.
 *   kind      (string) — one of: note, finding, decision, question, hypothesis.
 *   body      (string) — the observation text.
 *
 * Returns:
 *   int64 — ID of the new observation.
 *   error — if kind is invalid or the insert fails.
 *
 * Example:
 *   id, err := kb.AddObservation(1, "finding", "WAL mode doubles write throughput")
 */
func (kb *KnowledgeBase) AddObservation(projectID int64, kind, body string) (int64, error) {
	if !isValidKind(kind) {
		return 0, fmt.Errorf("knowledge: invalid kind %q; must be one of: %s",
			kind, strings.Join(ValidObservationKinds, ", "))
	}
	res, err := kb.db.Exec(
		`INSERT INTO observations (project_id, kind, body) VALUES (?, ?, ?)`,
		projectID, kind, body,
	)
	if err != nil {
		return 0, fmt.Errorf("knowledge: add observation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if kb.ftsAvailable {
		_, _ = kb.db.Exec(
			`INSERT INTO kb_fts(body, kind, label, descr, source_type, source_id, project_id)
			 VALUES (?, ?, '', '', 'observation', ?, ?)`,
			body, kind, id, projectID)
	}
	return id, nil
}

/** Observations returns all observations for a project, newest first.
 *
 * Parameters:
 *   projectID (int64) — ID of the project to query.
 *
 * Returns:
 *   []Observation — slice of matching rows; empty (not nil) if none exist.
 *   error         — on database failure.
 *
 * Example:
 *   obs, err := kb.Observations(1)
 *   for _, o := range obs {
 *       fmt.Printf("[%s] %s\n", o.Kind, o.Body)
 *   }
 */
func (kb *KnowledgeBase) Observations(projectID int64) ([]Observation, error) {
	rows, err := kb.db.Query(
		`SELECT id, project_id, kind, body, created_at
		 FROM   observations
		 WHERE  project_id = ?
		 ORDER  BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Observation
	for rows.Next() {
		var o Observation
		var ts string
		if err := rows.Scan(&o.ID, &o.ProjectID, &o.Kind, &o.Body, &ts); err != nil {
			return nil, err
		}
		o.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		out = append(out, o)
	}
	if out == nil {
		out = []Observation{}
	}
	return out, rows.Err()
}

// ─── Concepts ────────────────────────────────────────────────────────────────

/** AddConcept inserts a new concept or, if a concept with the same name exists,
 * returns its ID unchanged.
 *
 * Parameters:
 *   name        (string) — unique concept name.
 *   description (string) — human-readable explanation of the concept.
 *
 * Returns:
 *   int64 — ID of the inserted or existing concept.
 *   error — on database failure.
 *
 * Example:
 *   id, err := kb.AddConcept("WAL mode", "SQLite write-ahead logging for concurrency")
 */
func (kb *KnowledgeBase) AddConcept(name, description string) (int64, error) {
	res, err := kb.db.Exec(
		`INSERT INTO concepts (name, description) VALUES (?, ?)
		 ON CONFLICT(name) DO UPDATE SET description = excluded.description`,
		name, description,
	)
	if err != nil {
		return 0, fmt.Errorf("knowledge: add concept: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		kb.db.QueryRow(`SELECT id FROM concepts WHERE name = ?`, name).Scan(&id)
	}
	if kb.ftsAvailable {
		_, _ = kb.db.Exec(
			`DELETE FROM kb_fts WHERE source_type = 'concept' AND source_id = ?`, id)
		_, _ = kb.db.Exec(
			`INSERT INTO kb_fts(body, kind, label, descr, source_type, source_id, project_id)
			 VALUES (?, 'concept', ?, ?, 'concept', ?, 0)`,
			name+" "+description, name, description, id)
	}
	return id, nil
}

/** Concepts returns all concepts ordered by name.
 *
 * Returns:
 *   []Concept — all concept rows; empty (not nil) if none exist.
 *   error     — on database failure.
 *
 * Example:
 *   concepts, err := kb.Concepts()
 *   for _, c := range concepts {
 *       fmt.Println(c.Name)
 *   }
 */
func (kb *KnowledgeBase) Concepts() ([]Concept, error) {
	rows, err := kb.db.Query(`SELECT id, name, description FROM concepts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Concept
	for rows.Next() {
		var c Concept
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []Concept{}
	}
	return out, rows.Err()
}

// ─── Link helpers ─────────────────────────────────────────────────────────────

/** LinkObservationConcept associates an observation with a concept. Duplicate
 * links are silently ignored.
 *
 * Parameters:
 *   observationID (int64) — ID of the observation.
 *   conceptID     (int64) — ID of the concept.
 *
 * Returns:
 *   error — on database failure.
 *
 * Example:
 *   err := kb.LinkObservationConcept(obsID, conceptID)
 */
func (kb *KnowledgeBase) LinkObservationConcept(observationID, conceptID int64) error {
	_, err := kb.db.Exec(
		`INSERT OR IGNORE INTO observation_concepts (observation_id, concept_id) VALUES (?, ?)`,
		observationID, conceptID,
	)
	return err
}

/** LinkProjectConcept associates a project with a concept. Duplicate links are
 * silently ignored.
 *
 * Parameters:
 *   projectID (int64) — ID of the project.
 *   conceptID (int64) — ID of the concept.
 *
 * Returns:
 *   error — on database failure.
 *
 * Example:
 *   err := kb.LinkProjectConcept(projectID, conceptID)
 */
func (kb *KnowledgeBase) LinkProjectConcept(projectID, conceptID int64) error {
	_, err := kb.db.Exec(
		`INSERT OR IGNORE INTO project_concepts (project_id, concept_id) VALUES (?, ?)`,
		projectID, conceptID,
	)
	return err
}

// ─── Summary ──────────────────────────────────────────────────────────────────

/** Summary returns a human-readable text summary of all projects and their
 * recent observations, suitable for printing in the Harvey REPL.
 *
 * Returns:
 *   string — formatted multi-line summary.
 *   error  — on database failure.
 *
 * Example:
 *   s, err := kb.Summary()
 *   fmt.Print(s)
 */
func (kb *KnowledgeBase) Summary() (string, error) {
	rows, err := kb.db.Query(
		`SELECT id, name, status, description, COALESCE(concepts,'') FROM project_summary ORDER BY id`,
	)
	if err != nil {
		return "", err
	}

	// Drain all project rows before closing — a second Query inside
	// recentObservations would deadlock with MaxOpenConns(1) if rows is
	// still open.
	type projectRow struct {
		id       int64
		name     string
		status   string
		desc     string
		concepts string
	}
	var projects []projectRow
	for rows.Next() {
		var p projectRow
		if err := rows.Scan(&p.id, &p.name, &p.status, &p.desc, &p.concepts); err != nil {
			rows.Close()
			return "", err
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	rows.Close()

	var b strings.Builder
	for _, p := range projects {
		fmt.Fprintf(&b, "  [%d] %s  (%s)\n", p.id, p.name, p.status)
		if p.desc != "" {
			fmt.Fprintf(&b, "      %s\n", p.desc)
		}
		if p.concepts != "" {
			fmt.Fprintf(&b, "      concepts: %s\n", p.concepts)
		}
		obs, err := kb.recentObservations(p.id, 3)
		if err != nil {
			return "", err
		}
		for _, o := range obs {
			fmt.Fprintf(&b, "      [%s] %s\n", o.Kind, o.Body)
		}
		fmt.Fprintln(&b)
	}
	if b.Len() == 0 {
		b.WriteString("  (no projects — use /kb project add <name> to create one)\n")
	}
	return b.String(), nil
}

// recentObservations returns the n most recent observations for a project.
func (kb *KnowledgeBase) recentObservations(projectID int64, n int) ([]Observation, error) {
	rows, err := kb.db.Query(
		`SELECT id, project_id, kind, body, created_at
		 FROM   observations
		 WHERE  project_id = ?
		 ORDER  BY created_at DESC
		 LIMIT  ?`,
		projectID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Observation
	for rows.Next() {
		var o Observation
		var ts string
		if err := rows.Scan(&o.ID, &o.ProjectID, &o.Kind, &o.Body, &ts); err != nil {
			return nil, err
		}
		o.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		out = append(out, o)
	}
	return out, rows.Err()
}

// isValidKind returns true if kind is in ValidObservationKinds.
func isValidKind(kind string) bool {
	for _, v := range ValidObservationKinds {
		if v == kind {
			return true
		}
	}
	return false
}

// ─── FTS5 helpers ─────────────────────────────────────────────────────────────

// rebuildFTSIfNeeded populates kb_fts from the source tables when the FTS
// index is empty but the source tables contain data. This handles databases
// created before FTS5 support was added.
func (kb *KnowledgeBase) rebuildFTSIfNeeded() error {
	var ftsCount int
	if err := kb.db.QueryRow(`SELECT COUNT(*) FROM kb_fts`).Scan(&ftsCount); err != nil {
		return err
	}
	if ftsCount > 0 {
		return nil
	}
	var total int
	kb.db.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&total)
	kb.db.QueryRow(`SELECT COUNT(*) + ? FROM projects`, total).Scan(&total)
	kb.db.QueryRow(`SELECT COUNT(*) + ? FROM concepts`, total).Scan(&total)
	if total == 0 {
		return nil
	}
	if _, err := kb.db.Exec(`
		INSERT INTO kb_fts(body, kind, label, descr, source_type, source_id, project_id)
		SELECT body, kind, '', '', 'observation', id, project_id FROM observations`); err != nil {
		return fmt.Errorf("fts rebuild observations: %w", err)
	}
	if _, err := kb.db.Exec(`
		INSERT INTO kb_fts(body, kind, label, descr, source_type, source_id, project_id)
		SELECT name || ' ' || description, 'project', name, description, 'project', id, id
		FROM projects`); err != nil {
		return fmt.Errorf("fts rebuild projects: %w", err)
	}
	if _, err := kb.db.Exec(`
		INSERT INTO kb_fts(body, kind, label, descr, source_type, source_id, project_id)
		SELECT name || ' ' || description, 'concept', name, description, 'concept', id, 0
		FROM concepts`); err != nil {
		return fmt.Errorf("fts rebuild concepts: %w", err)
	}
	return nil
}

// ─── Search ───────────────────────────────────────────────────────────────────

/** KBSearchResult holds one row returned by Search.
 *
 * Fields:
 *   Kind    (string) — observation kind ("note", "finding", etc.) or "project" / "concept".
 *   Label   (string) — project name for observations; entity name for projects and concepts.
 *   Snippet (string) — observation body; or description for projects and concepts.
 *
 * Example:
 *   results, _ := kb.Search("WAL mode")
 *   for _, r := range results {
 *       fmt.Printf("[%s] %s — %s\n", r.Kind, r.Label, r.Snippet)
 *   }
 */
type KBSearchResult struct {
	Kind    string
	Label   string
	Snippet string
}

/** Search performs a full-text search across observations, projects, and concepts
 * using the FTS5 index. Results are ranked by relevance (best match first).
 * Returns an error wrapping ErrFTSUnavailable when the FTS index is not present.
 *
 * The term uses standard FTS5 query syntax: multiple words are ANDed, phrases
 * can be quoted ("WAL mode"), and prefix search is supported (docker*).
 *
 * Parameters:
 *   term (string) — FTS5 query term.
 *
 * Returns:
 *   []KBSearchResult — ranked results; nil if none found.
 *   error            — on query failure or when FTS is unavailable.
 *
 * Example:
 *   results, err := kb.Search("docker")
 *   for _, r := range results {
 *       fmt.Printf("[%-10s] %s — %s\n", r.Kind, r.Label, r.Snippet)
 *   }
 */
func (kb *KnowledgeBase) Search(term string) ([]KBSearchResult, error) {
	if !kb.ftsAvailable {
		return nil, fmt.Errorf("full-text search is not available (FTS5 not compiled in)")
	}
	rows, err := kb.db.Query(`
		SELECT kb_fts.kind,
		       CASE WHEN kb_fts.source_type = 'observation'
		            THEN COALESCE(p.name, '') ELSE kb_fts.label END,
		       CASE WHEN kb_fts.source_type = 'observation'
		            THEN kb_fts.body ELSE kb_fts.descr END
		FROM   kb_fts
		LEFT JOIN projects p ON kb_fts.source_type = 'observation'
		                     AND p.id = kb_fts.project_id
		WHERE  kb_fts MATCH ?
		ORDER  BY bm25(kb_fts)
		LIMIT  50
	`, term)
	if err != nil {
		return nil, fmt.Errorf("knowledge: search %q: %w", term, err)
	}
	defer rows.Close()
	var out []KBSearchResult
	for rows.Next() {
		var r KBSearchResult
		if err := rows.Scan(&r.Kind, &r.Label, &r.Snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ─── Lookup helpers ───────────────────────────────────────────────────────────

/** ProjectByName returns the project with the given name, or nil if not found.
 *
 * Parameters:
 *   name (string) — exact project name.
 *
 * Returns:
 *   *Project — the matching project, or nil.
 *   error    — on database failure.
 *
 * Example:
 *   p, err := kb.ProjectByName("harvey")
 */
func (kb *KnowledgeBase) ProjectByName(name string) (*Project, error) {
	var p Project
	var ts string
	err := kb.db.QueryRow(
		`SELECT id, name, description, status, created_at FROM projects WHERE name = ?`, name,
	).Scan(&p.ID, &p.Name, &p.Description, &p.Status, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge: project by name: %w", err)
	}
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
	return &p, nil
}

// projectByID returns the project with the given ID, or nil if not found.
func (kb *KnowledgeBase) projectByID(id int64) (*Project, error) {
	var p Project
	var ts string
	err := kb.db.QueryRow(
		`SELECT id, name, description, status, created_at FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Description, &p.Status, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge: project by id: %w", err)
	}
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
	return &p, nil
}

/** ProjectConcepts returns all concepts linked to a project, ordered by name.
 *
 * Parameters:
 *   projectID (int64) — ID of the project.
 *
 * Returns:
 *   []Concept — linked concepts; empty (not nil) if none.
 *   error     — on database failure.
 *
 * Example:
 *   concepts, err := kb.ProjectConcepts(1)
 */
func (kb *KnowledgeBase) ProjectConcepts(projectID int64) ([]Concept, error) {
	rows, err := kb.db.Query(`
		SELECT c.id, c.name, c.description
		FROM   concepts c
		JOIN   project_concepts pc ON pc.concept_id = c.id
		WHERE  pc.project_id = ?
		ORDER  BY c.name
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("knowledge: project concepts: %w", err)
	}
	defer rows.Close()
	var out []Concept
	for rows.Next() {
		var c Concept
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []Concept{}
	}
	return out, rows.Err()
}

// ─── Markdown export ──────────────────────────────────────────────────────────

/** FormatMarkdown returns the knowledge base contents as Markdown, suitable for
 * injecting into a conversation as context. When projectID > 0 only that project
 * is included; when projectID == 0 all projects are included. Each project gets
 * a ## heading, and observations are listed with their kind in bold.
 *
 * Parameters:
 *   projectID (int64) — project to export; 0 = all projects.
 *
 * Returns:
 *   string — Markdown-formatted knowledge base contents; "" if no data.
 *   error  — on database failure.
 *
 * Example:
 *   md, err := kb.FormatMarkdown(0) // all projects
 *   md, err := kb.FormatMarkdown(1) // project id=1 only
 */
func (kb *KnowledgeBase) FormatMarkdown(projectID int64) (string, error) {
	var projects []Project
	if projectID > 0 {
		p, err := kb.projectByID(projectID)
		if err != nil {
			return "", err
		}
		if p == nil {
			return "", nil
		}
		projects = []Project{*p}
	} else {
		var err error
		projects, err = kb.Projects()
		if err != nil {
			return "", err
		}
	}
	if len(projects) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("# Knowledge Base\n")

	for _, p := range projects {
		fmt.Fprintf(&b, "\n## Project: %s\n\n", p.Name)
		if p.Description != "" {
			fmt.Fprintf(&b, "%s\n\n", p.Description)
		}

		concepts, err := kb.ProjectConcepts(p.ID)
		if err != nil {
			return "", err
		}
		if len(concepts) > 0 {
			names := make([]string, len(concepts))
			for i, c := range concepts {
				names[i] = c.Name
			}
			fmt.Fprintf(&b, "**Concepts:** %s\n\n", strings.Join(names, ", "))
		}

		obs, err := kb.Observations(p.ID)
		if err != nil {
			return "", err
		}
		if len(obs) == 0 {
			b.WriteString("_No observations recorded._\n")
		} else {
			for _, o := range obs {
				fmt.Fprintf(&b, "**[%s]** %s\n\n", o.Kind, o.Body)
			}
		}
	}

	return b.String(), nil
}
