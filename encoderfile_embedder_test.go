package harvey_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rsdoiel/harvey"
)

// fakeEncoderfileServer returns a test server that responds to /health, /model,
// and /predict with canned Encoderfile responses.
func fakeEncoderfileServer(t *testing.T, modelID string, embedding []float64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `"OK!"`)

		case "/model":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"model_id":   modelID,
				"model_type": "embedding",
			})

		case "/predict":
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"results": []map[string]any{
					{
						"embeddings": []map[string]any{
							{"embedding": embedding},
						},
					},
				},
				"model_id": modelID,
			}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEncoderfileEmbedder_Embed(t *testing.T) {
	t.Parallel()
	wantVec := []float64{0.1, 0.2, 0.3, 0.4}
	srv := fakeEncoderfileServer(t, "test-embedder", wantVec)

	e := harvey.NewEncoderfileEmbedder(srv.URL, "test-embedder")
	vec, err := e.Embed("hello world")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if len(vec) != len(wantVec) {
		t.Fatalf("Embed() len = %d, want %d", len(vec), len(wantVec))
	}
	for i, v := range vec {
		if v != wantVec[i] {
			t.Errorf("vec[%d] = %f, want %f", i, v, wantVec[i])
		}
	}
}

func TestEncoderfileEmbedder_Name(t *testing.T) {
	t.Parallel()
	e := harvey.NewEncoderfileEmbedder("http://localhost:8080", "my-model")
	if e.Name() != "my-model" {
		t.Errorf("Name() = %q, want %q", e.Name(), "my-model")
	}
}

func TestEncoderfileEmbedder_ImplementsEmbedder(t *testing.T) {
	t.Parallel()
	var _ harvey.Embedder = harvey.NewEncoderfileEmbedder("http://localhost:8080", "m")
}

func TestProbeEncoderfile_reachable(t *testing.T) {
	t.Parallel()
	srv := fakeEncoderfileServer(t, "m", []float64{1, 2})
	if !harvey.ProbeEncoderfile(srv.URL) {
		t.Error("ProbeEncoderfile() = false for reachable server, want true")
	}
}

func TestProbeEncoderfile_unreachable(t *testing.T) {
	t.Parallel()
	if harvey.ProbeEncoderfile("http://localhost:19999") {
		t.Error("ProbeEncoderfile() = true for unreachable port, want false")
	}
}

func TestProbeEncoderfileModel(t *testing.T) {
	t.Parallel()
	srv := fakeEncoderfileServer(t, "nomic-embed-text-v1_5", nil)
	modelID, err := harvey.ProbeEncoderfileModel(srv.URL)
	if err != nil {
		t.Fatalf("ProbeEncoderfileModel() error: %v", err)
	}
	if modelID != "nomic-embed-text-v1_5" {
		t.Errorf("ProbeEncoderfileModel() = %q, want %q", modelID, "nomic-embed-text-v1_5")
	}
}

func TestProbeEncoderfileModel_wrongType(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"model_id":   "classifier",
				"model_type": "sequence_classification",
			})
		}
	}))
	t.Cleanup(srv.Close)

	_, err := harvey.ProbeEncoderfileModel(srv.URL)
	if err == nil {
		t.Error("expected error for non-embedding model type")
	}
}

func TestNewEmbedderForEntry_ollama(t *testing.T) {
	t.Parallel()
	entry := &harvey.RagStoreEntry{
		EmbeddingModel: "nomic-embed-text",
		EmbedderKind:   "",
	}
	emb := harvey.NewEmbedderForEntry(entry, "http://localhost:11434")
	if emb.Name() != "nomic-embed-text" {
		t.Errorf("Name() = %q, want %q", emb.Name(), "nomic-embed-text")
	}
}

func TestNewEmbedderForEntry_encoderfile(t *testing.T) {
	t.Parallel()
	entry := &harvey.RagStoreEntry{
		EmbeddingModel: "my-model",
		EmbedderKind:   "encoderfile",
		EmbedderURL:    "http://localhost:8080",
	}
	emb := harvey.NewEmbedderForEntry(entry, "http://localhost:11434")
	if emb.Name() != "my-model" {
		t.Errorf("Name() = %q, want %q", emb.Name(), "my-model")
	}
	// Confirm it is an EncoderfileEmbedder, not an OllamaEmbedder.
	if _, ok := emb.(*harvey.EncoderfileEmbedder); !ok {
		t.Errorf("NewEmbedderForEntry() returned %T, want *harvey.EncoderfileEmbedder", emb)
	}
}

func TestParseEmbedderFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args     []string
		wantKind string
		wantURL  string
	}{
		{[]string{"--embedder", "encoderfile", "--embedder-url", "http://localhost:8080"}, "encoderfile", "http://localhost:8080"},
		{[]string{"--embedder", "ollama"}, "ollama", ""},
		{[]string{}, "", ""},
		{[]string{"--embedder-url", "http://x:9000"}, "", "http://x:9000"},
	}
	for _, tc := range tests {
		kind, url := harvey.ParseEmbedderFlags(tc.args)
		if kind != tc.wantKind {
			t.Errorf("args=%v: kind=%q, want %q", tc.args, kind, tc.wantKind)
		}
		if url != tc.wantURL {
			t.Errorf("args=%v: url=%q, want %q", tc.args, url, tc.wantURL)
		}
	}
}
