package harvey

import (
	"database/sql"
	"fmt"
	"io"
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

// sourcesSchema creates the sources authority table and observation_sources join
// table. Applied after the main schema; CREATE TABLE IF NOT EXISTS is idempotent.
const sourcesSchema = `
CREATE TABLE IF NOT EXISTS sources (
    id               INTEGER  PRIMARY KEY AUTOINCREMENT,
    title            TEXT     NOT NULL,
    identifier_type  TEXT     NOT NULL DEFAULT '',
    identifier_value TEXT     NOT NULL DEFAULT '',
    authors          TEXT     NOT NULL DEFAULT '',
    published_date   TEXT     NOT NULL DEFAULT '',
    publisher        TEXT     NOT NULL DEFAULT '',
    rights           TEXT     NOT NULL DEFAULT '',
    version          TEXT     NOT NULL DEFAULT '',
    retracted        INTEGER  NOT NULL DEFAULT 0,
    retraction_note  TEXT     NOT NULL DEFAULT '',
    first_seen_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_checked_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_identifier
    ON sources(identifier_type, identifier_value)
    WHERE identifier_type != '' AND identifier_value != '';

CREATE TABLE IF NOT EXISTS observation_sources (
    observation_id INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    source_id      INTEGER NOT NULL REFERENCES sources(id)      ON DELETE RESTRICT,
    relationship   TEXT    NOT NULL DEFAULT 'cited',
    PRIMARY KEY (observation_id, source_id)
);
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
 * <workspace>/agents/knowledge.db (or a path overridden in harvey.yaml) and
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
 * question, or hypothesis attached to a project. SourceDOI records the
 * normalized DOI of the paper the observation was extracted from, if any;
 * it is "" for observations not tied to a specific source document.
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
	SourceDOI string
	CreatedAt time.Time
}

/** Concept represents a named idea or term that can be linked to projects and
 * observations. A concept may also represent a scholarly entity — a paper,
 * person, institution, or funder — in which case IdentifierType is one of
 * the IdentifierType values (e.g. "doi", "orcid", "ror", "fundref") and
 * IdentifierValue is that identifier's normalized (extended) form. Both
 * fields are "" for concepts that are plain ideas/terms, not entities.
 *
 * Example:
 *   concepts, err := kb.Concepts()
 *   for _, c := range concepts {
 *       fmt.Println(c.Name, "-", c.Description)
 *       if c.IdentifierType != "" {
 *           fmt.Printf("  %s: %s\n", c.IdentifierType, c.IdentifierValue)
 *       }
 *   }
 */
type Concept struct {
	ID              int64
	Name            string
	Description     string
	IdentifierType  string
	IdentifierValue string
}

// kbAlterStmts are lazy-migration ALTER TABLE statements that add
// scholarly-identifier columns to pre-existing knowledge bases: a paper's
// DOI for observations extracted from it, and an identifier type/value pair
// (e.g. ORCID, ROR, FundRef — see IdentifierType) for concepts that
// represent scholarly entities. SQLite ignores "duplicate column name"
// errors via _, _ = db.Exec(...).
var kbAlterStmts = []string{
	`ALTER TABLE observations ADD COLUMN source_doi TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE concepts ADD COLUMN identifier_type TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE concepts ADD COLUMN identifier_value TEXT NOT NULL DEFAULT ''`,
}

/** OpenKnowledgeBase opens (or creates) the SQLite knowledge base. customPath
 * overrides the default location (agents/knowledge.db inside the workspace);
 * pass an empty string to use the default. The schema is applied on every open
 * so that tables are created on first use without manual migration.
 *
 * Parameters:
 *   ws         (*Workspace) — the Harvey workspace that owns the database file.
 *   customPath (string)     — override path; empty = agents/knowledge.db.
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
	for _, stmt := range kbAlterStmts {
		_, _ = db.Exec(stmt)
	}
	if _, err := db.Exec(sourcesSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("knowledge: apply sources schema: %w", err)
	}
	// One-time data migration: promote existing source_doi values into the
	// sources authority table and link them via observation_sources.
	_, _ = db.Exec(`
		INSERT OR IGNORE INTO sources (title, identifier_type, identifier_value)
		SELECT 'Source (DOI: ' || source_doi || ')', 'doi', source_doi
		FROM observations WHERE source_doi != ''`)
	_, _ = db.Exec(`
		INSERT OR IGNORE INTO observation_sources (observation_id, source_id, relationship)
		SELECT o.id, s.id, 'cited'
		FROM observations o JOIN sources s ON s.identifier_value = o.source_doi
		WHERE o.source_doi != ''`)
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
 * It is equivalent to calling AddObservationWithSource with sourceDOI = "".
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
	return kb.AddObservationWithSource(projectID, kind, body, "")
}

/** AddObservationWithSource inserts a new observation for a project,
 * recording the normalized DOI of the paper it was extracted from, and
 * returns its ID.
 *
 * Parameters:
 *   projectID (int64)  — ID of the owning project.
 *   kind      (string) — one of: note, finding, decision, question, hypothesis.
 *   body      (string) — the observation text.
 *   sourceDOI (string) — normalized DOI of the source paper, or "" if none.
 *
 * Returns:
 *   int64 — ID of the new observation.
 *   error — if kind is invalid or the insert fails.
 *
 * Example:
 *   id, err := kb.AddObservationWithSource(1, "finding",
 *       "This paper found X", "https://doi.org/10.1234/abcd.5678")
 */
func (kb *KnowledgeBase) AddObservationWithSource(projectID int64, kind, body, sourceDOI string) (int64, error) {
	if !isValidKind(kind) {
		return 0, fmt.Errorf("knowledge: invalid kind %q; must be one of: %s",
			kind, strings.Join(ValidObservationKinds, ", "))
	}
	res, err := kb.db.Exec(
		`INSERT INTO observations (project_id, kind, body, source_doi) VALUES (?, ?, ?, ?)`,
		projectID, kind, body, sourceDOI,
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
		`SELECT id, project_id, kind, body, source_doi, created_at
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
		if err := rows.Scan(&o.ID, &o.ProjectID, &o.Kind, &o.Body, &o.SourceDOI, &ts); err != nil {
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
 * returns its ID unchanged. It is equivalent to calling AddConceptWithIdentifier
 * with identifierType = identifierValue = "", which leaves any identifier
 * already recorded for an existing concept untouched.
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
	return kb.AddConceptWithIdentifier(name, description, "", "")
}

/** AddConceptWithIdentifier inserts a new concept, or updates an existing
 * concept with the same name, optionally recording a scholarly identifier
 * (e.g. a paper's DOI, a person's ORCID, an institution's ROR) that the
 * concept represents. If identifierType or identifierValue is "" on an
 * update, the existing stored value (if any) is preserved rather than
 * cleared.
 *
 * Parameters:
 *   name            (string) — unique concept name.
 *   description     (string) — human-readable explanation of the concept.
 *   identifierType  (string) — one of the IdentifierType values (e.g. "doi", "orcid"), or "".
 *   identifierValue (string) — normalized (extended) identifier value, or "".
 *
 * Returns:
 *   int64 — ID of the inserted or existing concept.
 *   error — on database failure.
 *
 * Example:
 *   id, err := kb.AddConceptWithIdentifier("Jane Doe", "paper author",
 *       string(IdentifierORCID), "0000-0003-0900-6903")
 */
func (kb *KnowledgeBase) AddConceptWithIdentifier(name, description, identifierType, identifierValue string) (int64, error) {
	res, err := kb.db.Exec(
		`INSERT INTO concepts (name, description, identifier_type, identifier_value) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		     description = excluded.description,
		     identifier_type = CASE WHEN excluded.identifier_type = '' THEN concepts.identifier_type ELSE excluded.identifier_type END,
		     identifier_value = CASE WHEN excluded.identifier_value = '' THEN concepts.identifier_value ELSE excluded.identifier_value END`,
		name, description, identifierType, identifierValue,
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
	rows, err := kb.db.Query(`SELECT id, name, description, identifier_type, identifier_value FROM concepts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Concept
	for rows.Next() {
		var c Concept
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.IdentifierType, &c.IdentifierValue); err != nil {
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
		`SELECT id, project_id, kind, body, source_doi, created_at
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
		if err := rows.Scan(&o.ID, &o.ProjectID, &o.Kind, &o.Body, &o.SourceDOI, &ts); err != nil {
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
		SELECT c.id, c.name, c.description, c.identifier_type, c.identifier_value
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
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.IdentifierType, &c.IdentifierValue); err != nil {
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

// ─── Sources ─────────────────────────────────────────────────────────────────

/** Source is a row in the sources authority table. Each source represents a
 * citable document or resource that may be linked to observations.
 *
 * Fields:
 *   ID              (int64)  — auto-assigned primary key.
 *   Title           (string) — human-readable title.
 *   IdentifierType  (string) — "doi", "url", "isbn", "issn", "arxiv", "urn", or "".
 *   IdentifierValue (string) — the identifier string, e.g. "10.1234/example".
 *   Authors         (string) — comma-separated author names.
 *   PublishedDate   (string) — publication date, YYYY-MM-DD format.
 *   Publisher       (string) — publisher name.
 *   Rights          (string) — licence or rights statement.
 *   Version         (string) — edition or version.
 *   Retracted       (bool)   — true when the source has been retracted.
 *   RetractionNote  (string) — free-text note about the retraction.
 *
 * Example:
 *   s := Source{Title: "SPARQL 1.1", IdentifierType: "doi", IdentifierValue: "10.1234/sparql"}
 *   id, err := kb.AddSource(s)
 */
type Source struct {
	ID              int64
	Title           string
	IdentifierType  string
	IdentifierValue string
	Authors         string
	PublishedDate   string
	Publisher       string
	Rights          string
	Version         string
	Retracted       bool
	RetractionNote  string
}

/** AddSource inserts a new source row and returns its auto-assigned ID.
 * When identifier_type and identifier_value are both non-empty, an existing
 * row with the same (type, value) pair is returned instead of creating a
 * duplicate.
 *
 * Parameters:
 *   s (Source) — source metadata; ID field is ignored.
 *
 * Returns:
 *   int64 — the ID of the inserted or existing row.
 *   error — on database failure.
 *
 * Example:
 *   id, err := kb.AddSource(Source{Title: "SPARQL 1.1", IdentifierType: "doi", IdentifierValue: "10.1234/sparql"})
 */
func (kb *KnowledgeBase) AddSource(s Source) (int64, error) {
	if s.IdentifierType != "" && s.IdentifierValue != "" {
		var id int64
		err := kb.db.QueryRow(
			`SELECT id FROM sources WHERE identifier_type = ? AND identifier_value = ?`,
			s.IdentifierType, s.IdentifierValue,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
	}
	res, err := kb.db.Exec(
		`INSERT INTO sources (title, identifier_type, identifier_value, authors, published_date, publisher, rights, version)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Title, s.IdentifierType, s.IdentifierValue,
		s.Authors, s.PublishedDate, s.Publisher, s.Rights, s.Version,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

/** ListSources returns all rows in the sources table, ordered by id.
 *
 * Returns:
 *   []Source — all sources; empty slice when none exist.
 *   error    — on database failure.
 *
 * Example:
 *   sources, err := kb.ListSources()
 */
func (kb *KnowledgeBase) ListSources() ([]Source, error) {
	rows, err := kb.db.Query(
		`SELECT id, title, identifier_type, identifier_value, retracted, retraction_note
		 FROM sources ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var s Source
		var retracted int
		if err := rows.Scan(&s.ID, &s.Title, &s.IdentifierType, &s.IdentifierValue, &retracted, &s.RetractionNote); err != nil {
			return nil, err
		}
		s.Retracted = retracted != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

/** ShowSource returns the full source row for the given id, or an error if
 * not found.
 *
 * Parameters:
 *   id (int64) — source primary key.
 *
 * Returns:
 *   *Source — the source row.
 *   error   — sql.ErrNoRows when not found; other errors on db failure.
 *
 * Example:
 *   s, err := kb.ShowSource(1)
 */
func (kb *KnowledgeBase) ShowSource(id int64) (*Source, error) {
	var s Source
	var retracted int
	err := kb.db.QueryRow(
		`SELECT id, title, identifier_type, identifier_value, authors, published_date,
		        publisher, rights, version, retracted, retraction_note
		 FROM sources WHERE id = ?`, id,
	).Scan(&s.ID, &s.Title, &s.IdentifierType, &s.IdentifierValue,
		&s.Authors, &s.PublishedDate, &s.Publisher, &s.Rights, &s.Version,
		&retracted, &s.RetractionNote)
	if err != nil {
		return nil, err
	}
	s.Retracted = retracted != 0
	return &s, nil
}

/** RemoveSource deletes the source with the given id. Returns an error if the
 * source is linked to any observations.
 *
 * Parameters:
 *   id (int64) — source primary key.
 *
 * Returns:
 *   error — if the source is linked or not found.
 *
 * Example:
 *   err := kb.RemoveSource(1) // fails if linked
 */
func (kb *KnowledgeBase) RemoveSource(id int64) error {
	var count int
	if err := kb.db.QueryRow(
		`SELECT COUNT(*) FROM observation_sources WHERE source_id = ?`, id,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("source %d is linked to %d observation(s); unlink first", id, count)
	}
	_, err := kb.db.Exec(`DELETE FROM sources WHERE id = ?`, id)
	return err
}

/** RetractSource sets retracted=1 and records a retraction note on the source
 * with the given id.
 *
 * Parameters:
 *   id   (int64)  — source primary key.
 *   note (string) — free-text retraction note.
 *
 * Returns:
 *   error — on database failure.
 *
 * Example:
 *   err := kb.RetractSource(1, "Retracted 2026-07-01 by publisher")
 */
func (kb *KnowledgeBase) RetractSource(id int64, note string) error {
	_, err := kb.db.Exec(
		`UPDATE sources SET retracted = 1, retraction_note = ? WHERE id = ?`,
		note, id,
	)
	return err
}

/** LinkObservationSource creates an observation_sources row linking an
 * observation to a source. Duplicate links are silently ignored.
 *
 * Parameters:
 *   observationID (int64)  — observation primary key.
 *   sourceID      (int64)  — source primary key.
 *   relationship  (string) — label, e.g. "cited" or "retrieved".
 *
 * Returns:
 *   error — on database failure.
 *
 * Example:
 *   err := kb.LinkObservationSource(42, 1, "retrieved")
 */
func (kb *KnowledgeBase) LinkObservationSource(observationID, sourceID int64, relationship string) error {
	_, err := kb.db.Exec(
		`INSERT OR IGNORE INTO observation_sources (observation_id, source_id, relationship)
		 VALUES (?, ?, ?)`,
		observationID, sourceID, relationship,
	)
	return err
}

/** ObservationSources returns all sources linked to the given observation id,
 * including retraction state, ordered by source id.
 *
 * Parameters:
 *   observationID (int64) — observation primary key.
 *
 * Returns:
 *   []Source — linked sources; empty slice when none.
 *   error    — on database failure.
 *
 * Example:
 *   sources, err := kb.ObservationSources(42)
 */
func (kb *KnowledgeBase) ObservationSources(observationID int64) ([]Source, error) {
	rows, err := kb.db.Query(
		`SELECT s.id, s.title, s.identifier_type, s.identifier_value,
		        s.retracted, s.retraction_note
		 FROM sources s
		 JOIN observation_sources os ON os.source_id = s.id
		 WHERE os.observation_id = ?
		 ORDER BY s.id`,
		observationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var s Source
		var retracted int
		if err := rows.Scan(&s.ID, &s.Title, &s.IdentifierType, &s.IdentifierValue,
			&retracted, &s.RetractionNote); err != nil {
			return nil, err
		}
		s.Retracted = retracted != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

/** FindOrCreateSource upserts a source by identifier when one is provided, or
 * inserts a new source with the given title when no identifier is known.
 *
 * Parameters:
 *   title           (string) — human-readable title or file path.
 *   identifierType  (string) — "doi", "url", etc.; empty = no identifier.
 *   identifierValue (string) — the identifier value; empty = no identifier.
 *
 * Returns:
 *   int64 — the ID of the found or created source.
 *   error — on database failure.
 *
 * Example:
 *   id, err := kb.FindOrCreateSource("spec.md", "doi", "10.1234/example")
 */
func (kb *KnowledgeBase) FindOrCreateSource(title, identifierType, identifierValue string) (int64, error) {
	return kb.AddSource(Source{
		Title:           title,
		IdentifierType:  identifierType,
		IdentifierValue: identifierValue,
	})
}

/** CheckRetractions queries checker for every non-retracted source with
 * identifier_type = "doi" and marks any hits as retracted. It also updates
 * last_checked_at for every source it queries. Progress is written to out.
 *
 * Parameters:
 *   checker (func(doi string) (bool, string, error)) — returns (retracted,
 *             note, err) for a given DOI. Use CheckDOIRetraction for production.
 *   out     (io.Writer) — destination for per-source status lines.
 *
 * Returns:
 *   checked (int)   — number of DOI sources queried.
 *   updated (int)   — number of sources newly marked as retracted.
 *   error           — on database failure (checker errors are logged, not fatal).
 *
 * Example:
 *   checked, updated, err := kb.CheckRetractions(
 *       func(doi string) (bool, string, error) {
 *           return CheckDOIRetraction(doi, DefaultRetractionWatchURL)
 *       }, os.Stdout)
 */
func (kb *KnowledgeBase) CheckRetractions(
	checker func(doi string) (retracted bool, note string, err error),
	out io.Writer,
) (checked, updated int, err error) {
	rows, err := kb.db.Query(
		`SELECT id, title, identifier_value FROM sources
		 WHERE identifier_type = 'doi' AND retracted = 0`,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("check retractions: query sources: %w", err)
	}
	defer rows.Close()

	type doiSource struct {
		id    int64
		title string
		doi   string
	}
	var sources []doiSource
	for rows.Next() {
		var s doiSource
		if err := rows.Scan(&s.id, &s.title, &s.doi); err != nil {
			return 0, 0, fmt.Errorf("check retractions: scan: %w", err)
		}
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("check retractions: rows: %w", err)
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	for _, s := range sources {
		retracted, note, cerr := checker(s.doi)
		checked++

		// Update last_checked_at regardless of outcome.
		_, _ = kb.db.Exec(
			`UPDATE sources SET last_checked_at = ? WHERE id = ?`, now, s.id,
		)

		if cerr != nil {
			fmt.Fprintf(out, "  [skip] %s (%s): %v\n", s.doi, s.title, cerr)
			continue
		}
		if retracted {
			if rerr := kb.RetractSource(s.id, note); rerr != nil {
				fmt.Fprintf(out, "  [error] could not retract %s: %v\n", s.doi, rerr)
				continue
			}
			updated++
			fmt.Fprintf(out, "  [RETRACTED] %s — %s: %s\n", s.doi, s.title, note)
		} else {
			fmt.Fprintf(out, "  [ok] %s — %s\n", s.doi, s.title)
		}
	}
	return checked, updated, nil
}
