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

func TestStartLlamafileService_badPath(t *testing.T) {
	proc, err := StartLlamafileService("/nonexistent/model.llamafile", "http://localhost:8080", "")
	if err == nil {
		t.Fatal("expected error for nonexistent binary path")
	}
	if proc != nil {
		t.Fatal("expected nil process on failure")
	}
}
