package harvey

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

/** Embedder produces vector embeddings for text strings.
 *
 * Methods:
 *   Embed(text string) ([]float64, error) — return a vector for text.
 *   Name() string                         — return the embedding model name.
 *
 * Example:
 *   var e Embedder = NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")
 *   vec, err := e.Embed("hello world")
 */
type Embedder interface {
	Embed(text string) ([]float64, error)
	Name() string
}

/** RagStore is a SQLite-backed store for text chunks and their vector
 * embeddings. The database is scoped to one embedding model; mixing
 * models in the same database is prevented by an explicit consistency check.
 *
 * Example:
 *   store, err := NewRagStore("rag_nomic_v1.db", "nomic-embed-text")
 *   if err != nil { log.Fatal(err) }
 */
type RagStore struct {
	db             *sql.DB
	embeddingModel string
}

/** Chunk is a retrieved text chunk returned by RagStore.Query.
 *
 * Fields:
 *   ID      (int64)   — row ID in the chunks table.
 *   Content (string)  — the original ingested text.
 *   Score   (float64) — cosine similarity score [0,1]; 0 when not from Query.
 *   Source  (string)  — source file path set at ingest time; empty when unknown.
 *
 * Example:
 *   chunks, _ := store.Query("sky colour", embedder, 3)
 *   for _, c := range chunks { fmt.Printf("[%.2f] %s\n", c.Score, c.Content) }
 */
type Chunk struct {
	ID          int64
	Content     string
	Score       float64
	Source      string
	SourceURL   string
	SourceDOI   string
	SourceTitle string
}

/** NewRagStore opens (or creates) the RAG SQLite database at dbPath and
 * associates it with embeddingModel. The schema is applied on every open so
 * that the table is created on first use without manual migration.
 *
 * Parameters:
 *   dbPath         (string) — path to the SQLite database file.
 *   embeddingModel (string) — name of the embedding model used to produce
 *                             vectors in this database; used for consistency
 *                             checks on every Ingest and Query call.
 *
 * Returns:
 *   *RagStore — ready-to-use store.
 *   error     — if the file cannot be opened or the schema cannot be applied.
 *
 * Example:
 *   store, err := NewRagStore("rag_nomic_v1.db", "nomic-embed-text")
 */
func NewRagStore(dbPath, embeddingModel string) (*RagStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`); err != nil {
		db.Close()
		return nil, err
	}

	const schema = `
	CREATE TABLE IF NOT EXISTS chunks (
	    id        INTEGER PRIMARY KEY,
	    content   TEXT NOT NULL,
	    embedding BLOB NOT NULL,
	    source    TEXT NOT NULL DEFAULT ''
	);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	// Lazy migrations: add columns to databases created before they were introduced.
	// SQLite returns "duplicate column name" if a column already exists; ignore that.
	_, _ = db.Exec(`ALTER TABLE chunks ADD COLUMN source TEXT NOT NULL DEFAULT ''`)
	for _, stmt := range provenanceAlterStmts {
		_, _ = db.Exec(stmt)
	}

	return &RagStore{db: db, embeddingModel: embeddingModel}, nil
}

/** Ingest embeds each text string using embedder and stores the resulting
 * vectors in a single transaction. Returns an error if the embedder's name
 * does not match the store's embedding model.
 *
 * Parameters:
 *   source   (string)   — file path or identifier recorded alongside each chunk;
 *                         pass "" when the source is not known.
 *   texts    ([]string) — text strings to embed and store.
 *   embedder (Embedder) — must satisfy embedder.Name() == store's model name.
 *
 * Returns:
 *   error — on model mismatch, embedding failure, or database write failure.
 *
 * Example:
 *   err := store.Ingest("harvey/README.md", []string{"The sky is blue"}, embedder)
 */
func (r *RagStore) Ingest(source string, texts []string, embedder Embedder, meta ...ProvenanceMeta) error {
	if embedder.Name() != r.embeddingModel {
		return errors.New("embedding model mismatch")
	}

	m := ProvenanceMeta{}
	if len(meta) > 0 {
		m = meta[0]
	}

	// Collect content hashes for all incoming chunks so we can remove stale rows.
	type entry struct {
		text string
		hash string
	}
	entries := make([]entry, len(texts))
	for i, t := range texts {
		h := sha256.Sum256([]byte(t))
		entries[i] = entry{text: t, hash: hex.EncodeToString(h[:])}
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Remove stale chunks: rows from this source whose hash is not in the new set.
	if len(entries) > 0 {
		placeholders := make([]string, len(entries))
		args := make([]any, 1+len(entries))
		args[0] = source
		for i, e := range entries {
			placeholders[i] = "?"
			args[i+1] = e.hash
		}
		_, _ = tx.Exec(
			fmt.Sprintf("DELETE FROM chunks WHERE source = ? AND content_hash NOT IN (%s)",
				strings.Join(placeholders, ",")),
			args...,
		)
	}

	const insertQ = `INSERT INTO chunks(content, embedding, source, content_hash, source_url, source_doi, source_title, source_version, rights)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM chunks WHERE source = ? AND content_hash = ?)`
	stmt, err := tx.Prepare(insertQ)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		vec, err := embedder.Embed(e.text)
		if err != nil {
			return err
		}
		blob, err := serialize(vec)
		if err != nil {
			return err
		}
		if _, err = stmt.Exec(
			e.text, blob, source, e.hash, m.URL, m.DOI, m.Title, m.Version, m.Rights,
			source, e.hash,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// enrichedAlterStmts are the lazy-migration ALTER TABLE statements that add
// source-location and semantic columns to pre-existing RAG stores.
// SQLite ignores "duplicate column name" errors via _, _ = db.Exec(...).
var enrichedAlterStmts = []string{
	`ALTER TABLE chunks ADD COLUMN start_line INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE chunks ADD COLUMN start_col  INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE chunks ADD COLUMN end_line   INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE chunks ADD COLUMN end_col    INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE chunks ADD COLUMN chunk_type TEXT    NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN symbols    TEXT    NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN docs       TEXT    NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN identifiers TEXT NOT NULL DEFAULT '{}'`,
	`ALTER TABLE chunks ADD COLUMN citations   TEXT NOT NULL DEFAULT ''`,
}

// provenanceAlterStmts add scholarly-provenance columns to pre-existing RAG
// stores. Applied eagerly in NewRagStore; SQLite duplicate-column errors are ignored.
var provenanceAlterStmts = []string{
	`ALTER TABLE chunks ADD COLUMN indexed_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`,
	`ALTER TABLE chunks ADD COLUMN content_hash   TEXT     NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN source_url     TEXT     NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN source_doi     TEXT     NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN source_title   TEXT     NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN source_version TEXT     NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN rights         TEXT     NOT NULL DEFAULT ''`,
	`ALTER TABLE chunks ADD COLUMN retracted      INTEGER  NOT NULL DEFAULT 0`,
	`ALTER TABLE chunks ADD COLUMN retraction_note TEXT    NOT NULL DEFAULT ''`,
}

/** ProvenanceMeta carries scholarly-provenance metadata for a batch of chunks
 * ingested from a single source. All fields are optional; empty values are
 * stored as empty strings.
 *
 * Fields:
 *   URL     (string) — source URL.
 *   DOI     (string) — Digital Object Identifier, e.g. "10.1234/example".
 *   Title   (string) — human-readable title of the source document.
 *   Version (string) — version or edition of the source.
 *   Rights  (string) — licence or rights statement.
 *
 * Example:
 *   meta := ProvenanceMeta{DOI: "10.1234/sparql", Title: "SPARQL 1.1"}
 *   err  := store.Ingest("spec.md", chunks, embedder, meta)
 */
type ProvenanceMeta struct {
	URL     string
	DOI     string
	Title   string
	Version string
	Rights  string
}

/** IngestEnriched embeds each EnrichedChunk using embedder and stores the
 * resulting vectors alongside source-location and semantic metadata.  The
 * chunks table is lazily migrated with ALTER TABLE … ADD COLUMN on first use,
 * so existing RAG stores without the new columns continue to work.
 * Returns an error if the embedder's name does not match the store's model.
 *
 * Parameters:
 *   source   (string)         — file path or identifier recorded with each chunk.
 *   chunks   ([]EnrichedChunk)— enriched chunks to embed and store.
 *   embedder (Embedder)       — must satisfy embedder.Name() == store's model name.
 *
 * Returns:
 *   error — on model mismatch, embedding failure, or database write failure.
 *
 * Example:
 *   err := store.IngestEnriched("main.c", chunks, embedder)
 */
func (r *RagStore) IngestEnriched(source string, chunks []EnrichedChunk, embedder Embedder, meta ...ProvenanceMeta) error {
	if embedder.Name() != r.embeddingModel {
		return errors.New("embedding model mismatch")
	}
	// Lazy schema migration — ignore "duplicate column name" errors.
	for _, stmt := range enrichedAlterStmts {
		_, _ = r.db.Exec(stmt)
	}

	m := ProvenanceMeta{}
	if len(meta) > 0 {
		m = meta[0]
	}

	// Collect content hashes so we can remove stale rows from this source.
	type entry struct {
		chunk EnrichedChunk
		hash  string
	}
	entries := make([]entry, len(chunks))
	for i, c := range chunks {
		h := sha256.Sum256([]byte(c.Content))
		entries[i] = entry{chunk: c, hash: hex.EncodeToString(h[:])}
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Remove stale chunks: rows from this source whose hash is not in the new set.
	if len(entries) > 0 {
		placeholders := make([]string, len(entries))
		args := make([]any, 1+len(entries))
		args[0] = source
		for i, e := range entries {
			placeholders[i] = "?"
			args[i+1] = e.hash
		}
		_, _ = tx.Exec(
			fmt.Sprintf("DELETE FROM chunks WHERE source = ? AND content_hash NOT IN (%s)",
				strings.Join(placeholders, ",")),
			args...,
		)
	}

	const q = `INSERT INTO chunks(content, embedding, source,
		start_line, start_col, end_line, end_col, chunk_type, symbols, docs,
		identifiers, citations, content_hash, source_url, source_doi, source_title, source_version, rights)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM chunks WHERE source = ? AND content_hash = ?)`
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		vec, err := embedder.Embed(e.chunk.Content)
		if err != nil {
			return err
		}
		blob, err := serialize(vec)
		if err != nil {
			return err
		}
		syms := strings.Join(e.chunk.Symbols, ",")
		identifiers := "{}"
		if e.chunk.Identifiers != nil {
			b, err := json.Marshal(e.chunk.Identifiers)
			if err != nil {
				return err
			}
			identifiers = string(b)
		}
		citations := strings.Join(e.chunk.Citations, ",")
		if _, err = stmt.Exec(
			e.chunk.Content, blob, source,
			e.chunk.StartLine, e.chunk.StartCol,
			e.chunk.EndLine, e.chunk.EndCol,
			e.chunk.ChunkType, syms, e.chunk.Docs,
			identifiers, citations,
			e.hash, m.URL, m.DOI, m.Title, m.Version, m.Rights,
			source, e.hash,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

/** Query embeds query using embedder, computes cosine similarity against every
 * stored chunk, and returns the topK highest-scoring chunks in descending order.
 * Returns an error if the embedder's name does not match the store's model.
 *
 * Parameters:
 *   query    (string)   — text to find similar chunks for.
 *   embedder (Embedder) — must satisfy embedder.Name() == store's model name.
 *   topK     (int)      — maximum number of chunks to return.
 *
 * Returns:
 *   []Chunk — up to topK chunks ordered by descending similarity score.
 *   error   — on model mismatch, embedding failure, or database read failure.
 *
 * Example:
 *   chunks, err := store.Query("What colour is the sky?", embedder, 3)
 */
func (r *RagStore) Query(query string, embedder Embedder, topK int) ([]Chunk, error) {
	if embedder.Name() != r.embeddingModel {
		return nil, errors.New("embedding model mismatch")
	}

	queryVec, err := embedder.Embed(query)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(
		"SELECT id, content, embedding, source, source_url, source_doi, source_title FROM chunks",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		chunk Chunk
		score float64
	}

	var results []scored
	for rows.Next() {
		var id int64
		var content, source, sourceURL, sourceDOI, sourceTitle string
		var blob []byte
		if err := rows.Scan(&id, &content, &blob, &source, &sourceURL, &sourceDOI, &sourceTitle); err != nil {
			return nil, err
		}
		vec, err := deserialize(blob)
		if err != nil {
			return nil, err
		}
		results = append(results, scored{
			chunk: Chunk{
				ID: id, Content: content, Source: source,
				SourceURL: sourceURL, SourceDOI: sourceDOI, SourceTitle: sourceTitle,
			},
			score: cosineSimilarity(queryVec, vec),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort descending by score.
	slices.SortFunc(results, func(a, b scored) int {
		return cmp.Compare(b.score, a.score)
	})

	if topK > len(results) {
		topK = len(results)
	}
	out := make([]Chunk, topK)
	for i := range out {
		out[i] = results[i].chunk
		out[i].Score = results[i].score
	}
	return out, nil
}

// cosineSimilarity returns the cosine similarity of two equal-length vectors.
// Returns 0 when either vector has zero magnitude or the lengths differ.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// serialize encodes a float64 slice as [int32 length][float64...] in
// little-endian byte order.
func serialize(vec []float64) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, int32(len(vec))); err != nil {
		return nil, err
	}
	for _, v := range vec {
		if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// deserialize decodes a byte slice produced by serialize into a float64 slice.
func deserialize(data []byte) ([]float64, error) {
	buf := bytes.NewReader(data)
	var length int32
	if err := binary.Read(buf, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	vec := make([]float64, length)
	for i := range vec {
		if err := binary.Read(buf, binary.LittleEndian, &vec[i]); err != nil {
			return nil, err
		}
	}
	return vec, nil
}

/** NewEmbedderForEntry constructs the correct Embedder for entry based on its
 * EmbedderKind field. "encoderfile" uses EncoderfileEmbedder; anything else
 * (including "" and "ollama") uses OllamaEmbedder.
 *
 * Parameters:
 *   entry     (*RagStoreEntry) — store configuration entry.
 *   ollamaURL (string)         — Ollama base URL, used when kind is "ollama".
 *
 * Returns:
 *   Embedder — the appropriate embedder for this entry.
 *
 * Example:
 *   emb := NewEmbedderForEntry(cfg.Memory.ActiveRagStore(), cfg.OllamaURL)
 *   vec, err := emb.Embed("hello world")
 */
func NewEmbedderForEntry(entry *RagStoreEntry, ollamaURL string) Embedder {
	if entry.EmbedderKind == "encoderfile" {
		return NewEncoderfileEmbedder(entry.EmbedderURL, entry.EmbeddingModel)
	}
	return NewOllamaEmbedder(ollamaURL, entry.EmbeddingModel)
}

/** Count returns the total number of chunks stored in the database.
 *
 * Returns:
 *   int64 — total chunk count.
 *   error — on database failure.
 *
 * Example:
 *   n, _ := store.Count()
 *   fmt.Printf("store has %d chunks\n", n)
 */
func (r *RagStore) Count() (int64, error) {
	var n int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&n)
	return n, err
}

/** Close releases the database connection held by the RagStore.
 *
 * Returns:
 *   error — if the database cannot be closed cleanly.
 *
 * Example:
 *   if err := store.Close(); err != nil {
 *       log.Println("rag store close:", err)
 *   }
 */
func (r *RagStore) Close() error {
	return r.db.Close()
}

// ragChunk splits text into paragraph-sized chunks of at most ~500 characters,
// further splitting oversized paragraphs at sentence boundaries.
func ragChunk(text string) []string {
	const maxChunk = 500

	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	var chunks []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) <= maxChunk {
			chunks = append(chunks, p)
			continue
		}
		// Split long paragraphs at sentence ends.
		sentences := strings.FieldsFunc(p, func(r rune) bool {
			return r == '.' || r == '!' || r == '?'
		})
		var buf strings.Builder
		for _, s := range sentences {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if buf.Len()+len(s)+2 > maxChunk && buf.Len() > 0 {
				chunks = append(chunks, buf.String())
				buf.Reset()
			}
			if buf.Len() > 0 {
				buf.WriteString(". ")
			}
			buf.WriteString(s)
		}
		if buf.Len() > 0 {
			chunks = append(chunks, buf.String())
		}
	}
	return chunks
}

// ragMinScore is the minimum cosine similarity a chunk must have to be injected
// as context. Chunks scoring below this threshold are discarded so that irrelevant
// results don't waste the limited context window of small models.
const ragMinScore = 0.3

// ragAugment prepends relevant RAG chunks to prompt when RAG is enabled.
// Returns the original prompt unchanged when RAG is off, unconfigured, or
// when no chunks are retrieved. Errors are silently swallowed so a RAG
// failure never blocks the chat turn.
func (a *Agent) ragAugment(prompt string) (string, *RAGAugmentInfo) {
	if !a.RagOn || a.Rag == nil {
		return prompt, nil
	}
	entry := a.Config.Memory.ActiveRagStore()
	if entry == nil || entry.EmbeddingModel == "" {
		return prompt, nil
	}
	if entry.SkipPerPrompt {
		return prompt, nil
	}

	// Resolve embedding model for the current generation model.
	embedModel := entry.EmbeddingModel
	if entry.ModelMap != nil {
		if mapped, ok := entry.ModelMap[a.Config.OllamaModel]; ok && mapped != "" {
			embedModel = mapped
		}
	}

	embedder := NewOllamaEmbedder(a.Config.OllamaURL, embedModel)
	chunks, err := a.Rag.Query(prompt, embedder, 5)
	if err != nil || len(chunks) == 0 {
		return prompt, nil
	}

	// Discard chunks below the relevance threshold; they confuse small models
	// and waste context tokens without adding useful information.
	var relevant []Chunk
	for _, c := range chunks {
		if c.Score >= ragMinScore {
			relevant = append(relevant, c)
		}
	}
	if len(relevant) == 0 {
		return prompt, nil
	}

	topScore := relevant[0].Score
	a.DebugLog.LogRAGInject(entry.Name, prompt, len(relevant), topScore)

	var sb strings.Builder
	sb.WriteString("### Context (from knowledge base)\n\n")
	for i, c := range relevant {
		if c.Source != "" {
			fmt.Fprintf(&sb, "[%d] (source: %s)\n%s\n\n", i+1, c.Source, c.Content)
		} else {
			fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, c.Content)
		}
	}
	sb.WriteString("---\n\n")
	sb.WriteString(prompt)

	// Deduplicate sources by file path, preserving retrieval rank order.
	seen := map[string]bool{}
	var sources []RAGChunkRef
	for _, c := range relevant {
		key := c.Source
		if key == "" {
			key = c.Content
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		sources = append(sources, RAGChunkRef{
			Source:      c.Source,
			SourceURL:   c.SourceURL,
			SourceDOI:   c.SourceDOI,
			SourceTitle: c.SourceTitle,
		})
	}
	return sb.String(), &RAGAugmentInfo{
		StoreName: entry.Name,
		Chunks:    len(relevant),
		TopScore:  topScore,
		Sources:   sources,
	}
}
