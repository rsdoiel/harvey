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

func TestBuildLlamafileArgs_basic(t *testing.T) {
	args := buildLlamafileArgs("/models/test.llamafile", "8080", -1, 0)
	want := []string{"/models/test.llamafile", "--server", "--host", "127.0.0.1", "--port", "8080"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d]: got %q want %q", i, args[i], w)
		}
	}
}

func TestBuildLlamafileArgs_gpuLayers(t *testing.T) {
	args := buildLlamafileArgs("/models/test.llamafile", "8080", 99, 0)
	if len(args) < 2 || args[len(args)-2] != "-ngl" || args[len(args)-1] != "99" {
		t.Fatalf("expected -ngl 99 at end, got %v", args)
	}
}

func TestBuildLlamafileArgs_ctxSize(t *testing.T) {
	args := buildLlamafileArgs("/models/test.llamafile", "8080", -1, 49152)
	if len(args) < 2 || args[len(args)-2] != "-c" || args[len(args)-1] != "49152" {
		t.Fatalf("expected -c 49152 at end, got %v", args)
	}
}

func TestBuildLlamafileArgs_gpuLayersAndCtxSize(t *testing.T) {
	args := buildLlamafileArgs("/models/test.llamafile", "8080", 99, 32768)
	// Both -ngl and -c should be present.
	found := map[string]bool{}
	for i, a := range args {
		if a == "-ngl" && i+1 < len(args) {
			found["-ngl"] = args[i+1] == "99"
		}
		if a == "-c" && i+1 < len(args) {
			found["-c"] = args[i+1] == "32768"
		}
	}
	if !found["-ngl"] {
		t.Errorf("missing -ngl 99 in %v", args)
	}
	if !found["-c"] {
		t.Errorf("missing -c 32768 in %v", args)
	}
}

func TestBuildLlamafileArgs_zeroCtxSizeOmitted(t *testing.T) {
	args := buildLlamafileArgs("/models/test.llamafile", "8080", -1, 0)
	for _, a := range args {
		if a == "-c" {
			t.Errorf("expected -c to be omitted when ctxSize==0, got %v", args)
		}
	}
}

func TestStartLlamafileService_badPath(t *testing.T) {
	// Use a port unlikely to be occupied so ProbeLlamafile never returns true.
	proc, err := StartLlamafileService("/nonexistent/model.llamafile", "http://localhost:19876", "", 0, -1, 0, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent binary path")
	}
	if proc != nil {
		t.Fatal("expected nil process on failure")
	}
}
