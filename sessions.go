package harvey

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/glebarez/go-sqlite" // pure-Go SQLite driver
)

// sessionSchema is the DDL for the sessions table.
const sessionSchema = `
CREATE TABLE IF NOT EXISTS sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL DEFAULT '',
    workspace    TEXT    NOT NULL DEFAULT '',
    model        TEXT    NOT NULL DEFAULT '',
    history_json TEXT    NOT NULL DEFAULT '[]',
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_active  DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

/** Session holds the persisted state of a Harvey conversation, allowing work
 * to be resumed across process restarts or model switches.
 *
 * Fields:
 *   ID         (int64)     — database row ID, assigned on Create.
 *   Name       (string)    — optional user-assigned label (e.g. "fixing spinner bug").
 *   Workspace  (string)    — absolute path of the workspace root at last save.
 *   Model      (string)    — name of the model active at last save.
 *   History    ([]Message) — full conversation history, deserialized from storage.
 *   CreatedAt  (time.Time) — when the session was first created.
 *   LastActive (time.Time) — when the session was last saved.
 *
 * Example:
 *   session, err := sm.LoadLast()
 *   if session != nil {
 *       fmt.Printf("Resume %q (%d turns, %s)\n",
 *           session.Name, len(session.History), session.Model)
 *   }
 */
type Session struct {
	ID         int64
	Name       string
	Workspace  string
	Model      string
	History    []Message
	CreatedAt  time.Time
	LastActive time.Time
}

/** SessionManager handles creating, saving, and loading Harvey sessions.
 * Sessions are stored in a dedicated SQLite3 database at
 * <workspace>/.harvey/sessions.db, separate from the knowledge base.
 *
 * Example:
 *   sm, err := OpenSessionManager(ws)
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   defer sm.Close()
 */
type SessionManager struct {
	db *sql.DB
}

/** OpenSessionManager opens (or creates) the session database at
 * <workspace>/.harvey/sessions.db. The schema is applied on every open so
 * that the table is created on first use without manual migration.
 *
 * Parameters:
 *   ws (*Workspace) — the Harvey workspace that owns the database file.
 *
 * Returns:
 *   *SessionManager — ready-to-use session manager.
 *   error           — if the database file cannot be opened or the schema
 *                     cannot be applied.
 *
 * Example:
 *   sm, err := OpenSessionManager(ws)
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   defer sm.Close()
 */
func OpenSessionManager(ws *Workspace) (*SessionManager, error) {
	dbPath, err := ws.AbsPath(".harvey/sessions.db")
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("session: open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("session: set WAL mode: %w", err)
	}
	if _, err := db.Exec(sessionSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("session: apply schema: %w", err)
	}
	return &SessionManager{db: db}, nil
}

/** Close releases the database connection. It should be deferred immediately
 * after a successful OpenSessionManager call.
 *
 * Returns:
 *   error — from the underlying sql.DB.Close call.
 *
 * Example:
 *   sm, _ := OpenSessionManager(ws)
 *   defer sm.Close()
 */
func (sm *SessionManager) Close() error {
	return sm.db.Close()
}

/** Create inserts a new session row and returns its assigned ID. The session
 * starts with the provided model and history (typically empty on a fresh start).
 *
 * Parameters:
 *   workspace (string)    — absolute path of the workspace root.
 *   model     (string)    — name of the active model.
 *   history   ([]Message) — initial conversation history (may be nil or empty).
 *
 * Returns:
 *   int64 — ID of the newly created session.
 *   error — on JSON serialization or database failure.
 *
 * Example:
 *   id, err := sm.Create(ws.Root, "llama3", nil)
 */
func (sm *SessionManager) Create(workspace, model string, history []Message) (int64, error) {
	blob, err := marshalHistory(history)
	if err != nil {
		return 0, err
	}
	res, err := sm.db.Exec(`
		INSERT INTO sessions (workspace, model, history_json)
		VALUES (?, ?, ?)
	`, workspace, model, blob)
	if err != nil {
		return 0, fmt.Errorf("session: create: %w", err)
	}
	return res.LastInsertId()
}

/** Save updates an existing session's model, history, and last_active timestamp.
 * Call this after each successful chat turn to checkpoint progress.
 *
 * Parameters:
 *   id      (int64)    — session ID returned by Create.
 *   model   (string)   — current model name (may have changed since Create).
 *   history ([]Message) — full current conversation history.
 *
 * Returns:
 *   error — on JSON serialization or database failure.
 *
 * Example:
 *   err := sm.Save(sessionID, a.Client.Name(), a.History)
 */
func (sm *SessionManager) Save(id int64, model string, history []Message) error {
	blob, err := marshalHistory(history)
	if err != nil {
		return err
	}
	_, err = sm.db.Exec(`
		UPDATE sessions
		SET    model        = ?,
		       history_json = ?,
		       last_active  = datetime('now')
		WHERE  id = ?
	`, model, blob, id)
	if err != nil {
		return fmt.Errorf("session: save %d: %w", id, err)
	}
	return nil
}

/** Rename sets the human-readable name of an existing session.
 *
 * Parameters:
 *   id   (int64)  — session ID.
 *   name (string) — label to assign (e.g. "fixing spinner bug").
 *
 * Returns:
 *   error — on database failure.
 *
 * Example:
 *   err := sm.Rename(sessionID, "fixing spinner bug")
 */
func (sm *SessionManager) Rename(id int64, name string) error {
	_, err := sm.db.Exec(`UPDATE sessions SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("session: rename %d: %w", id, err)
	}
	return nil
}

/** LoadLast returns the most recently active session, including its full
 * conversation history. Returns (nil, nil) when no sessions exist.
 *
 * Returns:
 *   *Session — most recent session, or nil if none exist.
 *   error    — on database or JSON deserialization failure.
 *
 * Example:
 *   session, err := sm.LoadLast()
 *   if session != nil {
 *       a.History = session.History
 *   }
 */
func (sm *SessionManager) LoadLast() (*Session, error) {
	row := sm.db.QueryRow(`
		SELECT id, name, workspace, model, history_json, created_at, last_active
		FROM   sessions
		ORDER  BY last_active DESC, id DESC
		LIMIT  1
	`)
	return scanSession(row)
}

/** Load returns the session with the given ID, including its full conversation
 * history. Returns (nil, nil) when no session with that ID exists.
 *
 * Parameters:
 *   id (int64) — session ID to load.
 *
 * Returns:
 *   *Session — the session, or nil if not found.
 *   error    — on database or JSON deserialization failure.
 *
 * Example:
 *   session, err := sm.Load(3)
 */
func (sm *SessionManager) Load(id int64) (*Session, error) {
	row := sm.db.QueryRow(`
		SELECT id, name, workspace, model, history_json, created_at, last_active
		FROM   sessions
		WHERE  id = ?
	`, id)
	return scanSession(row)
}

/** List returns summary information for all sessions, ordered most-recent
 * first. History is not populated in the returned sessions — use Load to
 * fetch the full history for a specific session.
 *
 * Returns:
 *   []Session — all sessions without History populated; empty (not nil) if none.
 *   error     — on database failure.
 *
 * Example:
 *   sessions, err := sm.List()
 *   for _, s := range sessions {
 *       fmt.Printf("[%d] %s — %s (%s)\n", s.ID, s.Name, s.Model, s.LastActive.Format("2006-01-02 15:04"))
 *   }
 */
func (sm *SessionManager) List() ([]Session, error) {
	rows, err := sm.db.Query(`
		SELECT id, name, workspace, model, created_at, last_active
		FROM   sessions
		ORDER  BY last_active DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("session: list: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		var createdAt, lastActive string
		if err := rows.Scan(&s.ID, &s.Name, &s.Workspace, &s.Model, &createdAt, &lastActive); err != nil {
			return nil, fmt.Errorf("session: list scan: %w", err)
		}
		s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		s.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActive)
		out = append(out, s)
	}
	if out == nil {
		out = []Session{}
	}
	return out, rows.Err()
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// scanSession reads one session row (with history_json) from a *sql.Row and
// returns a *Session. Returns (nil, nil) on sql.ErrNoRows.
func scanSession(row *sql.Row) (*Session, error) {
	var s Session
	var histJSON, createdAt, lastActive string
	err := row.Scan(&s.ID, &s.Name, &s.Workspace, &s.Model, &histJSON, &createdAt, &lastActive)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session: scan: %w", err)
	}
	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	s.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActive)
	if err := json.Unmarshal([]byte(histJSON), &s.History); err != nil {
		return nil, fmt.Errorf("session: decode history: %w", err)
	}
	return &s, nil
}

// marshalHistory serializes a []Message to a JSON blob for storage. A nil or
// empty slice is stored as "[]".
func marshalHistory(history []Message) (string, error) {
	if len(history) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(history)
	if err != nil {
		return "", fmt.Errorf("session: encode history: %w", err)
	}
	return string(b), nil
}
