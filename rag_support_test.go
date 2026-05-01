package harvey

import (
	"fmt"
	"os"
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

	err = store.Ingest([]string{
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
		}
	}
	if !found {
		t.Errorf("expected 'The sky is blue' in top-2 results; got: %v", results)
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

	err = store.Ingest([]string{
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

	err := store.Ingest([]string{"hello world"}, embedB)
	if err == nil {
		t.Error("expected mismatch error when ingesting with wrong embedder")
	}

	err = store.Ingest([]string{"hello world"}, embedA)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Query("hello", embedB, 1)
	if err == nil {
		t.Error("expected mismatch error when querying with wrong embedder")
	}
}
