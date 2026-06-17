package harvey

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

// ErrAutoArchived is returned by SetConfidence when the memory's confidence
// fell to or below the auto-archive threshold (0.2) and Archive was called.
var ErrAutoArchived = errors.New("memory auto-archived: confidence below threshold")

const memoriesSubdir = "memories"

const memoriesSchema = `
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
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
`

// memoriesEnrichAlterStmts adds the three enrichment columns to databases
// created before this change. SQLite silently ignores duplicate-column errors.
var memoriesEnrichAlterStmts = []string{
	`ALTER TABLE memories ADD COLUMN kind       TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE memories ADD COLUMN action     TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE memories ADD COLUMN confidence REAL NOT NULL DEFAULT 0.5`,
}

const memoriesFTSSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    id,
    type,
    kind,
    tags,
    description,
    summary,
    action,
    file_path UNINDEXED
);
`

const memoriesStatsSchema = `
CREATE TABLE IF NOT EXISTS memory_stats (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id         TEXT    NOT NULL DEFAULT '',
    budget_tokens      INTEGER NOT NULL DEFAULT 0,
    injected_tokens    INTEGER NOT NULL DEFAULT 0,
    compressed         INTEGER NOT NULL DEFAULT 0,
    avg_tokens_per_sec REAL    NOT NULL DEFAULT 0,
    recorded_at        TEXT    NOT NULL
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
		string(MemoryTypeWorkspaceProfile),
		string(MemoryTypeProjectFact),
		filepath.Join("archive", string(MemoryTypeToolUse)),
		filepath.Join("archive", string(MemoryTypeWorkflow)),
		filepath.Join("archive", string(MemoryTypeUserPreference)),
		filepath.Join("archive", string(MemoryTypeWorkspaceProfile)),
		filepath.Join("archive", string(MemoryTypeProjectFact)),
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

	// Lazy-migrate existing databases to add the three enrichment columns.
	for _, stmt := range memoriesEnrichAlterStmts {
		_, _ = db.Exec(stmt)
	}

	s := &MemoryStore{db: db, dir: dir}

	// Ensure FTS table exists with the current schema, migrating if needed.
	s.ftsAvailable = s.ensureFTS()

	// memory_stats is optional from the schema perspective but needed for
	// Phase 2b budget tuning. Ignore the error so existing DBs without the
	// table continue to work; the table is created on first NewMemoryStore
	// call after the upgrade.
	_, _ = db.Exec(memoriesStatsSchema)

	if err := s.rebuildIfNeeded(); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory store: rebuild index: %w", err)
	}

	return s, nil
}

// ensureFTS ensures memories_fts exists with the current schema (including
// kind and action columns). If the table exists with the old schema, it is
// dropped and recreated, then repopulated from the memories table.
func (s *MemoryStore) ensureFTS() bool {
	// Probe for the new columns by running a no-op query.
	if _, err := s.db.Exec(`SELECT kind FROM memories_fts LIMIT 0`); err == nil {
		return true // Already up to date.
	}
	// Table is absent or has old schema — drop and recreate.
	_, _ = s.db.Exec(`DROP TABLE IF EXISTS memories_fts`)
	if _, err := s.db.Exec(memoriesFTSSchema); err != nil {
		return false
	}
	// Repopulate FTS from the memories table (embeddings are not in FTS).
	rows, err := s.db.Query(`
		SELECT id, type, kind, tags, description, summary, action, file_path
		FROM memories WHERE archived=0`)
	if err != nil {
		return true // Table created but empty; acceptable.
	}
	defer rows.Close()
	for rows.Next() {
		var id, typ, kind, tags, desc, summary, action, fp string
		if err := rows.Scan(&id, &typ, &kind, &tags, &desc, &summary, &action, &fp); err != nil {
			continue
		}
		_, _ = s.db.Exec(`
			INSERT INTO memories_fts (id, type, kind, tags, description, summary, action, file_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, typ, kind, tags, desc, summary, action, fp)
	}
	return true
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

	conf := doc.Meta.Confidence
	if conf == 0 {
		conf = 0.5
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO memories
		    (id, type, kind, description, summary, action, tags, source_session,
		     file_path, created_at, updated_at, archived, confidence, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		doc.Meta.ID,
		string(doc.Meta.Type),
		doc.Meta.Kind,
		doc.Meta.Description,
		doc.Meta.Summary,
		doc.Meta.Action,
		string(tagsJSON),
		doc.Meta.SourceSession,
		path,
		doc.Meta.CreatedAt,
		doc.Meta.UpdatedAt,
		conf,
		blob,
	)
	if err != nil {
		return fmt.Errorf("memory store: save: index: %w", err)
	}

	if s.ftsAvailable {
		_, _ = s.db.Exec(`DELETE FROM memories_fts WHERE id = ?`, doc.Meta.ID)
		_, _ = s.db.Exec(`
			INSERT INTO memories_fts (id, type, kind, tags, description, summary, action, file_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			doc.Meta.ID,
			string(doc.Meta.Type),
			doc.Meta.Kind,
			string(tagsJSON),
			doc.Meta.Description,
			doc.Meta.Summary,
			doc.Meta.Action,
			path,
		)
	}

	_ = s.WriteDigest(filepath.Join(s.dir, "DIGEST.md"))
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
	_ = s.WriteDigest(filepath.Join(s.dir, "DIGEST.md"))
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
		`SELECT id, file_path, embedding, confidence FROM memories WHERE archived=0`,
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
		var conf float64
		if err := rows.Scan(&id, &filePath, &blob, &conf); err != nil {
			return nil, err
		}
		vec, err := deserialize(blob)
		if err != nil {
			continue
		}
		if conf == 0 {
			conf = 0.5
		}
		candidates = append(candidates, scored{
			id:       id,
			filePath: filePath,
			score:    cosineSimilarity(queryVec, vec) * conf,
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
			SELECT id, type, kind, description, summary, action, tags, source_session,
			       created_at, updated_at, confidence
			FROM memories WHERE archived=0 AND type=?
			ORDER BY updated_at DESC`, typeFilter)
	} else {
		rows, err = s.db.Query(`
			SELECT id, type, kind, description, summary, action, tags, source_session,
			       created_at, updated_at, confidence
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

/** ScoredDoc is a MemoryDoc paired with a retrieval score.
 *
 * Fields:
 *   Doc   (MemoryDoc) — the parsed memory document.
 *   Score (float64)   — relevance score; higher is a better match.
 *
 * Example:
 *   docs, _ := store.SearchFTS("git repository", 5)
 *   for _, d := range docs { fmt.Printf("[%.2f] %s\n", d.Score, d.Doc.Meta.Description) }
 */
type ScoredDoc struct {
	Doc   MemoryDoc
	Score float64
}

/** SearchFTS returns non-archived memories matching query using FTS5 full-text
 * search. Results are ordered by relevance (best match first). Returns nil when
 * FTS5 is unavailable or query is empty.
 *
 * Parameters:
 *   query (string) — search text passed to the FTS5 MATCH operator.
 *   topK  (int)    — maximum number of results to return.
 *
 * Returns:
 *   []ScoredDoc — up to topK documents ordered by descending relevance.
 *   error       — on database failure.
 *
 * Example:
 *   docs, err := store.SearchFTS("git repository", 5)
 */
func (s *MemoryStore) SearchFTS(query string, topK int) ([]ScoredDoc, error) {
	if !s.ftsAvailable || query == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT m.file_path, -memories_fts.rank AS score, m.confidence
		FROM memories_fts
		JOIN memories m ON memories_fts.id = m.id
		WHERE memories_fts MATCH ? AND m.archived = 0
		ORDER BY memories_fts.rank
		LIMIT ?`, query, topK)
	if err != nil {
		return nil, fmt.Errorf("memory store: search fts: %w", err)
	}
	defer rows.Close()

	var out []ScoredDoc
	for rows.Next() {
		var filePath string
		var score, conf float64
		if err := rows.Scan(&filePath, &score, &conf); err != nil {
			return nil, err
		}
		if conf == 0 {
			conf = 0.5
		}
		score *= conf
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		doc, err := ParseMemoryDoc(data)
		if err != nil {
			continue
		}
		out = append(out, ScoredDoc{Doc: *doc, Score: score})
	}
	return out, rows.Err()
}

/** ListDocs returns full MemoryDoc objects for all non-archived memories.
 * When typeFilter is non-empty only memories of that type are returned.
 * Results are ordered by updated_at descending.
 *
 * Parameters:
 *   typeFilter (string) — memory type to filter on, or "" for all types.
 *
 * Returns:
 *   []MemoryDoc — documents with parsed front matter and Fountain body.
 *   error       — on database or file read failure.
 *
 * Example:
 *   docs, err := store.ListDocs("workspace_profile")
 *   for _, d := range docs { fmt.Println(d.Meta.Description) }
 */
func (s *MemoryStore) ListDocs(typeFilter string) ([]MemoryDoc, error) {
	metas, err := s.List(typeFilter)
	if err != nil {
		return nil, err
	}
	var out []MemoryDoc
	for _, m := range metas {
		path := filepath.Join(s.dir, string(m.Type), m.ID+".fountain")
		data, err := os.ReadFile(path)
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

/** RecordSessionStats appends one row to the memory_stats table for the
 * session that just ended. Called at session exit by the REPL.
 *
 * Parameters:
 *   sessionID      (string)  — filename of the session .spmd file; may be empty.
 *   budgetTokens   (int)     — token budget allocated for memory injection.
 *   injectedTokens (int)     — tokens actually injected this session.
 *   compressed     (bool)    — true if rolling summary fired at least once.
 *   avgToksPerSec  (float64) — average generation throughput across all turns.
 *
 * Returns:
 *   error — on database write failure.
 *
 * Example:
 *   _ = store.RecordSessionStats("harvey-session-20260526.spmd", 512, 123, false, 14.2)
 */
func (s *MemoryStore) RecordSessionStats(sessionID string, budgetTokens, injectedTokens int, compressed bool, avgToksPerSec float64) error {
	compressedInt := 0
	if compressed {
		compressedInt = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO memory_stats
		    (session_id, budget_tokens, injected_tokens, compressed, avg_tokens_per_sec, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, budgetTokens, injectedTokens, compressedInt, avgToksPerSec, now)
	return err
}

/** BudgetStats computes aggregate memory budget statistics from the last n rows
 * of memory_stats. Used by /memory status to generate tuning suggestions.
 *
 * Parameters:
 *   n (int) — number of most-recent sessions to include; e.g. 10.
 *
 * Returns:
 *   avgSaturation  (float64) — mean(injected_tokens/budget_tokens); 0 when budget was 0.
 *   compressionRate (float64) — fraction of sessions where rolling summary fired.
 *   avgToksPerSec  (float64) — mean generation throughput across sessions.
 *   error          — on database failure.
 *
 * Example:
 *   sat, compRate, tps, err := store.BudgetStats(10)
 *   fmt.Printf("avg utilisation %.0f%%\n", sat*100)
 */
func (s *MemoryStore) BudgetStats(n int) (avgSaturation, compressionRate, avgToksPerSec float64, err error) {
	rows, err := s.db.Query(`
		SELECT budget_tokens, injected_tokens, compressed, avg_tokens_per_sec
		FROM memory_stats
		ORDER BY recorded_at DESC
		LIMIT ?`, n)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("memory store: budget stats: %w", err)
	}
	defer rows.Close()

	var count int
	var totalSat, totalTps float64
	var compCount int
	for rows.Next() {
		var budget, injected, comp int
		var tps float64
		if scanErr := rows.Scan(&budget, &injected, &comp, &tps); scanErr != nil {
			return 0, 0, 0, scanErr
		}
		if budget > 0 {
			totalSat += float64(injected) / float64(budget)
		}
		if comp > 0 {
			compCount++
		}
		totalTps += tps
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	if count == 0 {
		return 0, 0, 0, nil
	}
	return totalSat / float64(count),
		float64(compCount) / float64(count),
		totalTps / float64(count),
		nil
}

/** StatsCount returns the total number of rows in the memory_stats table.
 *
 * Returns:
 *   int64 — row count.
 *   error — on database failure.
 *
 * Example:
 *   n, _ := store.StatsCount()
 *   if n >= 10 { /* show advice *‌/ }
 */
func (s *MemoryStore) StatsCount() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_stats`).Scan(&n)
	return n, err
}

/** SetConfidence adjusts the confidence of an active memory by delta and
 * clamps the result to [0.0, 1.0]. Both the database row and the on-disk
 * .fountain file are updated so that a database rebuild preserves the value.
 * If the resulting confidence is <= 0.2 the memory is automatically archived
 * and ErrAutoArchived is returned alongside the final confidence value.
 *
 * Parameters:
 *   id    (string)  — ID of the memory to adjust.
 *   delta (float64) — positive to increase, negative to decrease confidence.
 *
 * Returns:
 *   float64 — the clamped confidence value after adjustment.
 *   error   — ErrAutoArchived when auto-archival triggered; other errors on
 *             database or file failure; nil on success.
 *
 * Example:
 *   newConf, err := store.SetConfidence("git_fix_a3f891", -0.1)
 *   if errors.Is(err, ErrAutoArchived) {
 *       fmt.Println("memory archived due to low confidence")
 *   }
 */
func (s *MemoryStore) SetConfidence(id string, delta float64) (float64, error) {
	var current float64
	err := s.db.QueryRow(
		`SELECT confidence FROM memories WHERE id=? AND archived=0`, id,
	).Scan(&current)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("memory store: set confidence: memory %q not found", id)
	}
	if err != nil {
		return 0, fmt.Errorf("memory store: set confidence: %w", err)
	}

	newConf := current + delta
	if newConf > 1.0 {
		newConf = 1.0
	}
	if newConf < 0.0 {
		newConf = 0.0
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(
		`UPDATE memories SET confidence=?, updated_at=? WHERE id=?`,
		newConf, now, id,
	); err != nil {
		return 0, fmt.Errorf("memory store: set confidence: update db: %w", err)
	}

	// Mirror confidence change into the .fountain file so rebuilds preserve it.
	if doc, ferr := s.ByID(id); ferr == nil && doc != nil {
		doc.Meta.Confidence = newConf
		doc.Meta.UpdatedAt = now
		if data, merr := doc.Bytes(); merr == nil {
			_ = os.WriteFile(doc.FilePath(s.dir), data, 0o644)
		}
	}

	if newConf <= 0.2 {
		if archErr := s.Archive(id); archErr != nil {
			return newConf, fmt.Errorf("%w: %v", ErrAutoArchived, archErr)
		}
		return newConf, ErrAutoArchived
	}

	return newConf, nil
}

/** WriteDigest renders a Markdown summary of all active memories to path.
 * Memories are grouped by kind (pitfall → workaround → recommendation →
 * pattern → unclassified) and sorted by confidence descending within each
 * group. The file is not written when there are no active memories.
 *
 * Parameters:
 *   path (string) — absolute path to write the digest file.
 *
 * Returns:
 *   error — on database query or file write failure.
 *
 * Example:
 *   _ = store.WriteDigest(filepath.Join(store.Dir(), "DIGEST.md"))
 */
func (s *MemoryStore) WriteDigest(path string) error {
	type digestEntry struct {
		id          string
		memType     string
		kind        string
		description string
		action      string
		tags        string
		confidence  float64
	}

	rows, err := s.db.Query(`
		SELECT id, type, kind, description, action, tags, confidence
		FROM memories WHERE archived=0
		ORDER BY confidence DESC`)
	if err != nil {
		return fmt.Errorf("write digest: query: %w", err)
	}
	defer rows.Close()

	var entries []digestEntry
	for rows.Next() {
		var e digestEntry
		if err := rows.Scan(
			&e.id, &e.memType, &e.kind, &e.description, &e.action, &e.tags, &e.confidence,
		); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("write digest: scan: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	kindOrder := []string{"pitfall", "workaround", "recommendation", "pattern", ""}
	kindLabel := map[string]string{
		"pitfall":        "Pitfalls",
		"workaround":     "Workarounds",
		"recommendation": "Recommendations",
		"pattern":        "Patterns",
		"":               "Unclassified",
	}
	groups := make(map[string][]digestEntry)
	for _, e := range entries {
		groups[e.kind] = append(groups[e.kind], e)
	}

	var buf strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(&buf, "# Harvey Memory Digest\n*Updated: %s — %d active memories*\n",
		now, len(entries))

	for _, k := range kindOrder {
		items := groups[k]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\n## %s\n", kindLabel[k])
		for _, e := range items {
			fmt.Fprintf(&buf, "- **[%s]** `%s` (confidence: %.1f) — %s\n",
				e.memType, e.id, e.confidence, e.description)
			if e.action != "" {
				fmt.Fprintf(&buf, "  **Action:** %s\n", e.action)
			}
			var tags []string
			if jsonErr := json.Unmarshal([]byte(e.tags), &tags); jsonErr == nil && len(tags) > 0 {
				quoted := make([]string, len(tags))
				for i, t := range tags {
					quoted[i] = "`" + t + "`"
				}
				fmt.Fprintf(&buf, "  Tags: %s\n", strings.Join(quoted, " "))
			}
		}
	}

	return os.WriteFile(path, []byte(buf.String()), 0o644)
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
			conf := doc.Meta.Confidence
			if conf == 0 {
				conf = 0.5
			}
			_, _ = s.db.Exec(`
				INSERT OR IGNORE INTO memories
				    (id, type, kind, description, summary, action, tags, source_session,
				     file_path, created_at, updated_at, archived, confidence, embedding)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
				doc.Meta.ID,
				string(doc.Meta.Type),
				doc.Meta.Kind,
				doc.Meta.Description,
				doc.Meta.Summary,
				doc.Meta.Action,
				string(tagsJSON),
				doc.Meta.SourceSession,
				path,
				doc.Meta.CreatedAt,
				doc.Meta.UpdatedAt,
				conf,
				zeroBlob,
			)
			if s.ftsAvailable {
				_, _ = s.db.Exec(`
					INSERT OR IGNORE INTO memories_fts
					    (id, type, kind, tags, description, summary, action, file_path)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					doc.Meta.ID,
					string(doc.Meta.Type),
					doc.Meta.Kind,
					string(tagsJSON),
					doc.Meta.Description,
					doc.Meta.Summary,
					doc.Meta.Action,
					path,
				)
			}
		}
	}
	return nil
}

// scanMemoryMetas reads rows from a query that selects
// id, type, kind, description, summary, action, tags, source_session,
// created_at, updated_at, confidence.
func scanMemoryMetas(rows *sql.Rows) ([]MemoryMeta, error) {
	var out []MemoryMeta
	for rows.Next() {
		var m MemoryMeta
		var tagsJSON string
		if err := rows.Scan(
			&m.ID, (*string)(&m.Type), &m.Kind, &m.Description, &m.Summary,
			&m.Action, &tagsJSON, &m.SourceSession, &m.CreatedAt, &m.UpdatedAt,
			&m.Confidence,
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
