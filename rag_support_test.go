package harvey

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// mockEmbedder satisfies the Embedder interface for mismatch-protection tests.
// It uses a character-accumulation hash which has no semantic meaning; use
// precomputedEmbedder when retrieval quality matters.
type mockEmbedder struct {
	name string
}

func (m *mockEmbedder) Name() string { return m.name }
func (m *mockEmbedder) Embed(text string) ([]float64, error) {
	vec := make([]float64, 4)
	for i, r := range text {
		vec[i%4] += float64(r)
	}
	return vec, nil
}

// precomputedEmbedder returns a fixed vector for each known input, making
// cosine-similarity ranking deterministic and semantically intentional.
// It panics on unknown input so tests must explicitly register every text
// they pass through it.
type precomputedEmbedder struct {
	name    string
	vectors map[string][]float64
}

func (p *precomputedEmbedder) Name() string { return p.name }
func (p *precomputedEmbedder) Embed(text string) ([]float64, error) {
	v, ok := p.vectors[text]
	if !ok {
		return nil, fmt.Errorf("precomputedEmbedder: no vector registered for %q", text)
	}
	return v, nil
}

// TestIngestAndQuery verifies that the top-K query result contains the most
// semantically similar chunk. Vectors are hand-designed so that
// "The sky is blue" is nearest to "What color is the sky?":
//
//	sky cluster  ≈ [1.0, 0.1, 0.0, 0.0]
//	sun cluster  ≈ [0.6, 0.8, 0.0, 0.0]
//	code cluster ≈ [0.0, 0.0, 1.0, 0.0]
func TestIngestAndQuery(t *testing.T) {
	dbPath := "test_rag.db"
	defer os.Remove(dbPath)

	store, err := NewRagStore(dbPath, "semantic-mock")
	if err != nil {
		t.Fatal(err)
	}

	embedder := &precomputedEmbedder{
		name: "semantic-mock",
		vectors: map[string][]float64{
			"The sky is blue":              {1.0, 0.1, 0.0, 0.0},
			"The sun is bright":            {0.6, 0.8, 0.0, 0.0},
			"Go is a programming language": {0.0, 0.0, 1.0, 0.0},
			"What color is the sky?":       {0.9, 0.1, 0.0, 0.0},
		},
	}

	err = store.Ingest("", []string{
		"The sky is blue",
		"The sun is bright",
		"Go is a programming language",
	}, embedder)
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.Query("What color is the sky?", embedder, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}

	found := false
	for _, r := range results {
		if r.Content == "The sky is blue" {
			found = true
			if r.Score <= 0 {
				t.Errorf("expected positive Score for top result, got %f", r.Score)
			}
		}
	}
	if !found {
		t.Errorf("expected 'The sky is blue' in top-2 results; got: %v", results)
	}
}

// TestIngestWithSource verifies that the source path round-trips through Ingest and Query.
func TestIngestWithSource(t *testing.T) {
	dbPath := "test_rag_source.db"
	defer os.Remove(dbPath)

	store, err := NewRagStore(dbPath, "semantic-mock")
	if err != nil {
		t.Fatal(err)
	}

	embedder := &precomputedEmbedder{
		name: "semantic-mock",
		vectors: map[string][]float64{
			"Harvey is licensed under AGPL-3.0": {1.0, 0.0, 0.0, 0.0},
			"license query":                     {0.9, 0.1, 0.0, 0.0},
		},
	}

	if err := store.Ingest("harvey/LICENSE", []string{"Harvey is licensed under AGPL-3.0"}, embedder); err != nil {
		t.Fatal(err)
	}

	results, err := store.Query("license query", embedder, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected a result")
	}
	if results[0].Source != "harvey/LICENSE" {
		t.Errorf("Source = %q, want %q", results[0].Source, "harvey/LICENSE")
	}
	if results[0].Score <= 0 {
		t.Errorf("expected positive Score, got %f", results[0].Score)
	}
}

// TestSemanticRanking verifies that cosine similarity correctly ranks all three
// clusters when queried from a known direction.
func TestSemanticRanking(t *testing.T) {
	dbPath := "test_rag_rank.db"
	defer os.Remove(dbPath)

	store, err := NewRagStore(dbPath, "semantic-mock")
	if err != nil {
		t.Fatal(err)
	}

	embedder := &precomputedEmbedder{
		name: "semantic-mock",
		vectors: map[string][]float64{
			"The sky is blue":              {1.0, 0.0, 0.0},
			"The sun is bright":            {0.6, 0.8, 0.0},
			"Go is a programming language": {0.0, 0.0, 1.0},
			"query: sky":                   {1.0, 0.0, 0.0},
			"query: code":                  {0.0, 0.0, 1.0},
		},
	}

	err = store.Ingest("", []string{
		"The sky is blue",
		"The sun is bright",
		"Go is a programming language",
	}, embedder)
	if err != nil {
		t.Fatal(err)
	}

	// Sky query → sky chunk should rank first.
	skyResults, err := store.Query("query: sky", embedder, 3)
	if err != nil {
		t.Fatal(err)
	}
	if skyResults[0].Content != "The sky is blue" {
		t.Errorf("sky query: expected 'The sky is blue' first, got %q", skyResults[0].Content)
	}

	// Code query → Go chunk should rank first.
	codeResults, err := store.Query("query: code", embedder, 3)
	if err != nil {
		t.Fatal(err)
	}
	if codeResults[0].Content != "Go is a programming language" {
		t.Errorf("code query: expected 'Go is a programming language' first, got %q", codeResults[0].Content)
	}
}

func TestEmbeddingMismatch(t *testing.T) {
	dbPath := "test_rag2.db"
	defer os.Remove(dbPath)

	store, _ := NewRagStore(dbPath, "embed-A")

	embedA := &mockEmbedder{name: "embed-A"}
	embedB := &mockEmbedder{name: "embed-B"}

	err := store.Ingest("", []string{"hello world"}, embedB)
	if err == nil {
		t.Error("expected mismatch error when ingesting with wrong embedder")
	}

	err = store.Ingest("", []string{"hello world"}, embedA)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Query("hello", embedB, 1)
	if err == nil {
		t.Error("expected mismatch error when querying with wrong embedder")
	}
}

// ─── S1 provenance schema ─────────────────────────────────────────────────────

func TestMigrateChunksSchema_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Open once — applies all migrations.
	store1, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = store1.db.Close()

	// Open again — must not fail even though columns now exist.
	store2, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatalf("second open (idempotency check): %v", err)
	}
	defer store2.db.Close()
}

func TestIngest_ContentHash(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "test.db"), "stub")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	emb := stubEmbedder{"stub"}
	const text = "package main"

	if err := store.Ingest("src.go", []string{text}, emb); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	// Second ingest of the same content must be a no-op (skip, not duplicate).
	if err := store.Ingest("src.go", []string{text}, emb); err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM chunks WHERE source = 'src.go'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 chunk after two identical ingests, got %d", count)
	}

	// Verify the content_hash column is populated.
	var hash string
	if err := store.db.QueryRow(`SELECT content_hash FROM chunks WHERE source = 'src.go'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Error("expected non-empty content_hash after ingest")
	}
}

func TestIngest_ProvenanceMeta(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "test.db"), "stub")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	emb := stubEmbedder{"stub"}
	meta := ProvenanceMeta{
		DOI:   "10.1234/example",
		Title: "Example Paper",
		URL:   "https://example.org/paper",
	}
	if err := store.Ingest("paper.md", []string{"Abstract text"}, emb, meta); err != nil {
		t.Fatalf("ingest with meta: %v", err)
	}

	var doi, title, url string
	if err := store.db.QueryRow(
		`SELECT source_doi, source_title, source_url FROM chunks WHERE source = 'paper.md'`,
	).Scan(&doi, &title, &url); err != nil {
		t.Fatalf("query provenance fields: %v", err)
	}
	if doi != "10.1234/example" {
		t.Errorf("source_doi: got %q, want %q", doi, "10.1234/example")
	}
	if title != "Example Paper" {
		t.Errorf("source_title: got %q, want %q", title, "Example Paper")
	}
	if url != "https://example.org/paper" {
		t.Errorf("source_url: got %q, want %q", url, "https://example.org/paper")
	}
}

// ─── S3 provenance fields in Query ───────────────────────────────────────────

func TestQuery_ProvenanceFields(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "test.db"), "stub")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	emb := stubEmbedder{"stub"}
	meta := ProvenanceMeta{
		DOI:   "10.1234/prov-query",
		Title: "Provenance Query Test",
		URL:   "https://example.org/prov",
	}
	if err := store.Ingest("paper.md", []string{"some content"}, emb, meta); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	chunks, err := store.Query("some content", emb, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least one result")
	}
	c := chunks[0]
	if c.SourceDOI != "10.1234/prov-query" {
		t.Errorf("SourceDOI = %q, want %q", c.SourceDOI, "10.1234/prov-query")
	}
	if c.SourceTitle != "Provenance Query Test" {
		t.Errorf("SourceTitle = %q, want %q", c.SourceTitle, "Provenance Query Test")
	}
	if c.SourceURL != "https://example.org/prov" {
		t.Errorf("SourceURL = %q, want %q", c.SourceURL, "https://example.org/prov")
	}
}
