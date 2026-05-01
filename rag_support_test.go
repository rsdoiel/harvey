package harvey

import (
    "os"
    "testing"
)

type mockEmbedder struct {
    name string
}

func (m *mockEmbedder) Name() string {
    return m.name
}

func (m *mockEmbedder) Embed(text string) ([]float64, error) {
    vec := make([]float64, 4)
    for i, r := range text {
        vec[i%4] += float64(r)
    }
    return vec, nil
}

func TestIngestAndQuery(t *testing.T) {
    dbPath := "test_rag.db"
    defer os.Remove(dbPath)

    store, err := NewRagStore(dbPath, "mock-embed")
    if err != nil {
        t.Fatal(err)
    }

    embedder := &mockEmbedder{name: "mock-embed"}

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
        t.Fatal("expected results")
    }

    found := false
    for _, r := range results {
        if r.Content == "The sky is blue" {
            found = true
        }
    }

    if !found {
        t.Error("expected relevant result not found")
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
        t.Error("expected mismatch error")
    }

    err = store.Ingest([]string{"hello world"}, embedA)
    if err != nil {
        t.Fatal(err)
    }

    _, err = store.Query("hello", embedB, 1)
    if err == nil {
        t.Error("expected mismatch error on query")
    }
}

