package harvey

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"

	_ "github.com/glebarez/go-sqlite"
)

// ---- Interfaces ----

type Embedder interface {
	Embed(text string) ([]float64, error)
	Name() string
}

// ---- Core Types ----

type RagStore struct {
	db             *sql.DB
	embeddingModel string
}

type Chunk struct {
	ID        int64
	Content   string
	Embedding []float64
}

// ---- DB Initialization ----

func NewRagStore(dbPath, embeddingModel string) (*RagStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Optional but recommended pragmas
	pragmas := `
    PRAGMA journal_mode=WAL;
    PRAGMA synchronous=NORMAL;
    `

	if _, err := db.Exec(pragmas); err != nil {
		return nil, err
	}

	schema := `
    CREATE TABLE IF NOT EXISTS chunks (
        id INTEGER PRIMARY KEY,
        content TEXT NOT NULL,
        embedding BLOB NOT NULL
    );
    `

	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	return &RagStore{
		db:             db,
		embeddingModel: embeddingModel,
	}, nil
}

// ---- Ingest ----

func (r *RagStore) Ingest(texts []string, embedder Embedder) error {
	if embedder.Name() != r.embeddingModel {
		return errors.New("embedding model mismatch")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO chunks(content, embedding) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, text := range texts {
		vec, err := embedder.Embed(text)
		if err != nil {
			return err
		}

		blob, err := serialize(vec)
		if err != nil {
			return err
		}

		if _, err = stmt.Exec(text, blob); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ---- Query ----

func (r *RagStore) Query(query string, embedder Embedder, topK int) ([]Chunk, error) {
	if embedder.Name() != r.embeddingModel {
		return nil, errors.New("embedding model mismatch")
	}

	queryVec, err := embedder.Embed(query)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query("SELECT id, content, embedding FROM chunks")
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
		var content string
		var blob []byte

		if err := rows.Scan(&id, &content, &blob); err != nil {
			return nil, err
		}

		vec, err := deserialize(blob)
		if err != nil {
			return nil, err
		}

		score := cosineSimilarity(queryVec, vec)

		results = append(results, scored{
			chunk: Chunk{
				ID:      id,
				Content: content,
			},
			score: score,
		})
	}

	// simple sort (fine for small KBs)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if topK > len(results) {
		topK = len(results)
	}

	out := make([]Chunk, topK)
	for i := 0; i < topK; i++ {
		out[i] = results[i].chunk
	}

	return out, nil
}

// ---- Math ----

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

// ---- Serialization ----

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
