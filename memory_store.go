package harvey

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/glebarez/go-sqlite"
)

const memoriesSubdir = "memories"

const memoriesSchema = `
CREATE TABLE IF NOT EXISTS memories (
    id             TEXT PRIMARY KEY,
    type           TEXT NOT NULL,
    description    TEXT NOT NULL,
    summary        TEXT NOT NULL,
    tags           TEXT NOT NULL DEFAULT '[]',
    source_session TEXT NOT NULL DEFAULT '',
    file_path      TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    archived       INTEGER NOT NULL DEFAULT 0,
    embedding      BLOB NOT NULL
);
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
`

const memoriesFTSSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    id,
    type,
    tags,
    description,
    summary,
    file_path UNINDEXED
);
`

/** MemoryStore manages a workspace's memory documents: the on-disk
 * .fountain files under agents/memories/ and a companion SQLite index
 * (memories.db) that stores metadata, FTS5 tokens, and embedding vectors
 * for fast retrieval.
 *
 * The .fountain files are the source of truth. If memories.db is absent,
 * NewMemoryStore rebuilds it by walking the directory tree.
 *
 * Example:
 *   store, err := NewMemoryStore(ws)
 *   if err != nil { log.Fatal(err) }
 *   defer store.Close()
 */
type MemoryStore struct {
	db           *sql.DB
	dir          string // absolute path to agents/memories/
	ftsAvailable bool
}

/** Dir returns the absolute path to the memories root directory.
 *
 * Returns:
 *   string — e.g. "/home/user/project/agents/memories".
 *
 * Example:
 *   fmt.Println(store.Dir())
 */
func (s *MemoryStore) Dir() string { return s.dir }

/** NewMemoryStore opens (or creates) the memory store for the given
 * workspace. It creates the directory tree, opens memories.db, applies
 * the schema, and rebuilds the index from files if the database is empty.
 *
 * Parameters:
 *   ws (*Workspace) — the Harvey workspace.
 *
 * Returns:
 *   *MemoryStore — ready-to-use store.
 *   error        — if directories cannot be created or the database fails.
 *
 * Example:
 *   store, err := NewMemoryStore(ws)
 *   if err != nil { log.Fatal(err) }
 *   defer store.Close()
 */
func NewMemoryStore(ws *Workspace) (*MemoryStore, error) {
	dir, err := ws.AbsPath(filepath.Join(harveySubdir, memoriesSubdir))
	if err != nil {
		return nil, fmt.Errorf("memory store: resolve dir: %w", err)
	}

	subdirs := []string{
		"",
		string(MemoryTypeToolUse),
		string(MemoryTypeWorkflow),
		string(MemoryTypeUserPreference),
		filepath.Join("archive", string(MemoryTypeToolUse)),
		filepath.Join("archive", string(MemoryTypeWorkflow)),
		filepath.Join("archive", string(MemoryTypeUserPreference)),
	}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("memory store: create dir %s: %w", sub, err)
		}
	}

	dbPath := filepath.Join(dir, "memories.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("memory store: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(memoriesSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory store: apply schema: %w", err)
	}

	s := &MemoryStore{db: db, dir: dir}

	if _, err := db.Exec(memoriesFTSSchema); err == nil {
		s.ftsAvailable = true
	}

	if err := s.rebuildIfNeeded(); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory store: rebuild index: %w", err)
	}

	return s, nil
}

/** Close releases the database connection.
 *
 * Returns:
 *   error — from the underlying sql.DB.Close call.
 *
 * Example:
 *   defer store.Close()
 */
func (s *MemoryStore) Close() error {
	return s.db.Close()
}

/** Save writes a MemoryDoc to disk and indexes it in the database. If a
 * row with the same ID already exists it is replaced. The embedding is
 * computed from doc.EmbedText() using the provided embedder.
 *
 * Parameters:
 *   doc      (*MemoryDoc) — the memory document to persist.
 *   embedder (Embedder)   — used to compute the embedding vector.
 *
 * Returns:
 *   error — on file write, embedding, or database failure.
 *
 * Example:
 *   err := store.Save(doc, embedder)
 */
func (s *MemoryStore) Save(doc *MemoryDoc, embedder Embedder) error {
	if doc.Meta.ID == "" {
		return fmt.Errorf("memory store: save: document has no id")
	}
	if !isValidMemoryType(doc.Meta.Type) {
		return fmt.Errorf("memory store: save: unknown type %q", doc.Meta.Type)
	}

	path := doc.FilePath(s.dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("memory store: save: mkdir: %w", err)
	}
	data, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("memory store: save: serialise: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("memory store: save: write file: %w", err)
	}

	var blob []byte
	if embedder != nil {
		vec, err := embedder.Embed(doc.EmbedText())
		if err != nil {
			return fmt.Errorf("memory store: save: embed: %w", err)
		}
		blob, err = serialize(vec)
		if err != nil {
			return fmt.Errorf("memory store: save: serialize embedding: %w", err)
		}
	} else {
		var err error
		blob, err = serialize(make([]float64, 1))
		if err != nil {
			return fmt.Errorf("memory store: save: serialize zero embedding: %w", err)
		}
	}

	tagsJSON, err := json.Marshal(doc.Meta.Tags)
	if err != nil {
		return fmt.Errorf("memory store: save: marshal tags: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO memories
		    (id, type, description, summary, tags, source_session,
		     file_path, created_at, updated_at, archived, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		doc.Meta.ID,
		string(doc.Meta.Type),
		doc.Meta.Description,
		doc.Meta.Summary,
		string(tagsJSON),
		doc.Meta.SourceSession,
		path,
		doc.Meta.CreatedAt,
		doc.Meta.UpdatedAt,
		blob,
	)
	if err != nil {
		return fmt.Errorf("memory store: save: index: %w", err)
	}

	if s.ftsAvailable {
		_, _ = s.db.Exec(`DELETE FROM memories_fts WHERE id = ?`, doc.Meta.ID)
		_, _ = s.db.Exec(`
			INSERT INTO memories_fts (id, type, tags, description, summary, file_path)
			VALUES (?, ?, ?, ?, ?, ?)`,
			doc.Meta.ID,
			string(doc.Meta.Type),
			string(tagsJSON),
			doc.Meta.Description,
			doc.Meta.Summary,
			path,
		)
	}

	return nil
}

/** Archive moves a memory from the active store to the archive directory
 * and marks it as archived in the database. The original file is removed
 * from its type subdirectory.
 *
 * Parameters:
 *   id (string) — ID of the memory to archive.
 *
 * Returns:
 *   error — if the memory is not found, the file cannot be moved, or the
 *           database update fails.
 *
 * Example:
 *   err := store.Archive("git_fix_a3f891")
 */
func (s *MemoryStore) Archive(id string) error {
	doc, err := s.ByID(id)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("memory store: archive: memory %q not found", id)
	}

	src := doc.FilePath(s.dir)
	dst := doc.ArchivePath(s.dir)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("memory store: archive: mkdir: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("memory store: archive: move file: %w", err)
	}

	_, err = s.db.Exec(
		`UPDATE memories SET archived=1, file_path=? WHERE id=?`,
		dst, id,
	)
	if err != nil {
		return fmt.Errorf("memory store: archive: update db: %w", err)
	}
	if s.ftsAvailable {
		_, _ = s.db.Exec(`DELETE FROM memories_fts WHERE id = ?`, id)
	}
	return nil
}

/** Query embeds the query string and returns the topK non-archived memories
 * with the highest cosine similarity to the query embedding.
 *
 * Parameters:
 *   query    (string)   — the search query.
 *   embedder (Embedder) — used to embed the query.
 *   topK     (int)      — maximum number of results to return.
 *
 * Returns:
 *   []MemoryDoc — up to topK documents ordered by descending similarity.
 *   error       — on embedding or database failure.
 *
 * Example:
 *   docs, err := store.Query("git repository error", embedder, 5)
 */
func (s *MemoryStore) Query(query string, embedder Embedder, topK int) ([]MemoryDoc, error) {
	queryVec, err := embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("memory store: query: embed: %w", err)
	}

	rows, err := s.db.Query(
		`SELECT id, file_path, embedding FROM memories WHERE archived=0`,
	)
	if err != nil {
		return nil, fmt.Errorf("memory store: query: scan: %w", err)
	}
	defer rows.Close()

	type scored struct {
		id       string
		filePath string
		score    float64
	}
	var candidates []scored
	for rows.Next() {
		var id, filePath string
		var blob []byte
		if err := rows.Scan(&id, &filePath, &blob); err != nil {
			return nil, err
		}
		vec, err := deserialize(blob)
		if err != nil {
			continue
		}
		candidates = append(candidates, scored{
			id:       id,
			filePath: filePath,
			score:    cosineSimilarity(queryVec, vec),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort descending by score.
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].score > candidates[j-1].score; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
	if topK > len(candidates) {
		topK = len(candidates)
	}

	var out []MemoryDoc
	for _, c := range candidates[:topK] {
		data, err := os.ReadFile(c.filePath)
		if err != nil {
			continue
		}
		doc, err := ParseMemoryDoc(data)
		if err != nil {
			continue
		}
		out = append(out, *doc)
	}
	return out, nil
}

/** List returns metadata for all non-archived memories. When typeFilter is
 * non-empty only memories of that type are returned. Results are ordered
 * by updated_at descending.
 *
 * Parameters:
 *   typeFilter (string) — memory type to filter on, or "" for all types.
 *
 * Returns:
 *   []MemoryMeta — metadata rows; empty (not nil) if none found.
 *   error        — on database failure.
 *
 * Example:
 *   metas, err := store.List("tool_use")
 *   for _, m := range metas {
 *       fmt.Printf("%s  %s\n", m.ID, m.Description)
 *   }
 */
func (s *MemoryStore) List(typeFilter string) ([]MemoryMeta, error) {
	var rows *sql.Rows
	var err error
	if typeFilter != "" {
		rows, err = s.db.Query(`
			SELECT id, type, description, summary, tags, source_session, created_at, updated_at
			FROM memories WHERE archived=0 AND type=?
			ORDER BY updated_at DESC`, typeFilter)
	} else {
		rows, err = s.db.Query(`
			SELECT id, type, description, summary, tags, source_session, created_at, updated_at
			FROM memories WHERE archived=0
			ORDER BY updated_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("memory store: list: %w", err)
	}
	defer rows.Close()
	return scanMemoryMetas(rows)
}

/** Recent returns the n most recently updated non-archived MemoryDocs,
 * reading each document from its file.
 *
 * Parameters:
 *   n (int) — maximum number of documents to return.
 *
 * Returns:
 *   []MemoryDoc — up to n documents ordered by updated_at descending.
 *   error       — on database or file read failure.
 *
 * Example:
 *   docs, err := store.Recent(5)
 */
func (s *MemoryStore) Recent(n int) ([]MemoryDoc, error) {
	rows, err := s.db.Query(`
		SELECT file_path FROM memories WHERE archived=0
		ORDER BY updated_at DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("memory store: recent: %w", err)
	}
	defer rows.Close()

	var out []MemoryDoc
	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		doc, err := ParseMemoryDoc(data)
		if err != nil {
			continue
		}
		out = append(out, *doc)
	}
	return out, rows.Err()
}

/** ByID returns the MemoryDoc with the given ID, reading it from its file.
 * Returns (nil, nil) when the ID is not found.
 *
 * Parameters:
 *   id (string) — memory ID to look up.
 *
 * Returns:
 *   *MemoryDoc — the memory, or nil if not found.
 *   error      — on database or file read failure.
 *
 * Example:
 *   doc, err := store.ByID("git_fix_a3f891")
 */
func (s *MemoryStore) ByID(id string) (*MemoryDoc, error) {
	var filePath string
	err := s.db.QueryRow(
		`SELECT file_path FROM memories WHERE id=?`, id,
	).Scan(&filePath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory store: by id: %w", err)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("memory store: by id: read file: %w", err)
	}
	return ParseMemoryDoc(data)
}

/** Count returns the number of non-archived memories in the store.
 *
 * Returns:
 *   int64 — total active memory count.
 *   error — on database failure.
 *
 * Example:
 *   n, _ := store.Count()
 *   fmt.Printf("%d memories\n", n)
 */
func (s *MemoryStore) Count() (int64, error) {
	var n int64
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM memories WHERE archived=0`,
	).Scan(&n)
	return n, err
}

// rebuildIfNeeded populates memories.db from .fountain files when the
// database is empty but files exist on disk. This handles the case where
// memories.db was deleted.
func (s *MemoryStore) rebuildIfNeeded() error {
	var count int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Walk the type subdirectories and index any .fountain files found.
	for _, t := range ValidMemoryTypes {
		typeDir := filepath.Join(s.dir, string(t))
		entries, err := os.ReadDir(typeDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".fountain" {
				continue
			}
			path := filepath.Join(typeDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			doc, err := ParseMemoryDoc(data)
			if err != nil {
				continue
			}
			// Insert without embedding (use zero vector as placeholder).
			tagsJSON, _ := json.Marshal(doc.Meta.Tags)
			zeroBlob, _ := serialize(make([]float64, 1))
			_, _ = s.db.Exec(`
				INSERT OR IGNORE INTO memories
				    (id, type, description, summary, tags, source_session,
				     file_path, created_at, updated_at, archived, embedding)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
				doc.Meta.ID,
				string(doc.Meta.Type),
				doc.Meta.Description,
				doc.Meta.Summary,
				string(tagsJSON),
				doc.Meta.SourceSession,
				path,
				doc.Meta.CreatedAt,
				doc.Meta.UpdatedAt,
				zeroBlob,
			)
			if s.ftsAvailable {
				_, _ = s.db.Exec(`
					INSERT OR IGNORE INTO memories_fts
					    (id, type, tags, description, summary, file_path)
					VALUES (?, ?, ?, ?, ?, ?)`,
					doc.Meta.ID,
					string(doc.Meta.Type),
					string(tagsJSON),
					doc.Meta.Description,
					doc.Meta.Summary,
					path,
				)
			}
		}
	}
	return nil
}

// scanMemoryMetas reads rows from a query that selects
// id, type, description, summary, tags, source_session, created_at, updated_at.
func scanMemoryMetas(rows *sql.Rows) ([]MemoryMeta, error) {
	var out []MemoryMeta
	for rows.Next() {
		var m MemoryMeta
		var tagsJSON string
		if err := rows.Scan(
			&m.ID, (*string)(&m.Type), &m.Description, &m.Summary,
			&tagsJSON, &m.SourceSession, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
		if m.Tags == nil {
			m.Tags = []string{}
		}
		m.Supersedes = []string{}
		out = append(out, m)
	}
	if out == nil {
		out = []MemoryMeta{}
	}
	return out, rows.Err()
}
