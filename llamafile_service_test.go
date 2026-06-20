package harvey

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeLlamafile_unreachable(t *testing.T) {
	if ProbeLlamafile("http://127.0.0.1:19999") {
		t.Fatal("expected false for unreachable server")
	}
}

func TestProbeLlamafile_reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if !ProbeLlamafile(srv.URL) {
		t.Fatal("expected true for reachable server")
	}
}

func TestProbeLlamafileContextLength_validResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Real response shape observed during testing.
		w.Write([]byte(`{"data":[{"id":"Qwen3.5-4B-Q5_K_S.gguf","meta":{"n_ctx":16384,"n_ctx_train":262144}}],"object":"list"}`))
	}))
	defer srv.Close()

	got := ProbeLlamafileContextLength(srv.URL)
	if got != 16384 {
		t.Errorf("got %d want 16384", got)
	}
}

func TestProbeLlamafileContextLength_missingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// n_ctx absent from meta.
		w.Write([]byte(`{"data":[{"id":"model.gguf","meta":{}}],"object":"list"}`))
	}))
	defer srv.Close()

	if got := ProbeLlamafileContextLength(srv.URL); got != 0 {
		t.Errorf("expected 0 when n_ctx missing, got %d", got)
	}
}

func TestProbeLlamafileContextLength_emptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"object":"list"}`))
	}))
	defer srv.Close()

	if got := ProbeLlamafileContextLength(srv.URL); got != 0 {
		t.Errorf("expected 0 for empty data, got %d", got)
	}
}

func TestProbeLlamafileContextLength_unreachable(t *testing.T) {
	if got := ProbeLlamafileContextLength("http://127.0.0.1:19994"); got != 0 {
		t.Errorf("expected 0 for unreachable server, got %d", got)
	}
}

func TestStartLlamafileService_badPath(t *testing.T) {
	// Use a port unlikely to be occupied so ProbeLlamafile never returns true.
	proc, err := StartLlamafileService("/nonexistent/model.llamafile", "http://localhost:19876", "", 0, -1, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent binary path")
	}
	if proc != nil {
		t.Fatal("expected nil process on failure")
	}
}
