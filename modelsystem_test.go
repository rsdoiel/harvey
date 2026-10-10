package harvey

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A live model test needs a model system on the machine running it: Ollama,
// hailo-ollama, llama.cpp's llama-server, or a llamafile in ~/Models. When the
// system is absent the test is skipped and the skip says what is missing and
// how to provide it. modelSystemAvailable decides; requireModelSystem skips.

func fakeTagsServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestModelSystemAvailable_Ollama(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ollama.URL = fakeTagsServer(t).URL
	if ok, why := modelSystemAvailable("ollama", cfg); !ok {
		t.Errorf("reachable server reported unavailable: %s", why)
	}
	cfg.Ollama.URL = "http://127.0.0.1:1"
	ok, why := modelSystemAvailable("ollama", cfg)
	if ok {
		t.Fatal("unreachable server reported available")
	}
	for _, want := range []string{"Ollama", "http://127.0.0.1:1", "ollama serve"} {
		if !strings.Contains(why, want) {
			t.Errorf("reason %q should mention %q", why, want)
		}
	}
}

func TestModelSystemAvailable_Hailo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Hailo.URL = fakeTagsServer(t).URL
	if ok, why := modelSystemAvailable("hailo", cfg); !ok {
		t.Errorf("reachable server reported unavailable: %s", why)
	}
	cfg.Hailo.URL = "http://127.0.0.1:1"
	ok, why := modelSystemAvailable("hailo", cfg)
	if ok {
		t.Fatal("unreachable server reported available")
	}
	for _, want := range []string{"hailo-ollama", "http://127.0.0.1:1"} {
		if !strings.Contains(why, want) {
			t.Errorf("reason %q should mention %q", why, want)
		}
	}
}

func TestModelSystemAvailable_LlamaCpp(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	cfg := DefaultConfig()
	ok, why := modelSystemAvailable("llamacpp", cfg)
	if ok {
		t.Fatal("llama-server absent from PATH reported available")
	}
	if !strings.Contains(why, "llama-server") || !strings.Contains(why, "PATH") {
		t.Errorf("reason %q should name llama-server and PATH", why)
	}
	if err := os.WriteFile(filepath.Join(bin, "llama-server"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, why := modelSystemAvailable("llamacpp", cfg); !ok {
		t.Errorf("llama-server on PATH reported unavailable: %s", why)
	}
}

func TestModelSystemAvailable_LlamafileNeedsAFileInModelsDir(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Llamafile.ModelsDir = dir
	ok, why := modelSystemAvailable("llamafile", cfg)
	if ok {
		t.Fatal("empty models dir reported available")
	}
	if !strings.Contains(why, dir) || !strings.Contains(why, ".llamafile") {
		t.Errorf("reason %q should name %s and .llamafile", why, dir)
	}
	if err := os.WriteFile(filepath.Join(dir, "m.llamafile"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, why := modelSystemAvailable("llamafile", cfg); !ok {
		t.Errorf("models dir with a llamafile reported unavailable: %s", why)
	}
}

func TestModelSystemAvailable_UnknownSystem(t *testing.T) {
	ok, why := modelSystemAvailable("bogus", DefaultConfig())
	if ok || !strings.Contains(why, "bogus") {
		t.Errorf("unknown system: ok=%v reason=%q, want unavailable naming it", ok, why)
	}
}

// requireModelSystem must skip, not fail, when the system is missing. The
// subtest records whether its body ran past the call.
func TestRequireModelSystem_SkipsWhenMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	reached := false
	t.Run("live", func(t *testing.T) {
		requireModelSystem(t, "llamacpp")
		reached = true
	})
	if reached {
		t.Error("requireModelSystem returned for a missing system; it must skip the test")
	}
}
