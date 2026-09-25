package harvey

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOllamaShow serves /api/show for any model with the given capabilities
// and counts the requests. Any other path is a 404.
func fakeOllamaShow(t *testing.T, caps []string, hits *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		*hits++
		json.NewEncoder(w).Encode(map[string]any{
			"details":      map[string]any{"family": "llama", "parameter_size": "3B", "quantization_level": "Q4_K_M"},
			"capabilities": caps,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// selectAgent returns a test agent with an open model cache and Ollama at url.
func selectAgent(t *testing.T, url string) *Agent {
	t.Helper()
	a := newTestAgent(t)
	cache, err := OpenModelCache(a.Workspace, "")
	if err != nil {
		t.Fatalf("OpenModelCache: %v", err)
	}
	t.Cleanup(func() { cache.Close() })
	a.ModelCache = cache
	a.Config.Ollama.URL = url
	return a
}

// A model chosen without ever saving an alias used to have no cache entry, so
// toolsReliable() answered false and Harvey injected files instead of using
// tools. Selecting an Ollama model now probes and caches it.
func TestSetOllamaModel_ProbesAnUncachedModel(t *testing.T) {
	var hits int
	a := selectAgent(t, fakeOllamaShow(t, []string{"completion", "tools"}, &hits).URL)

	a.setOllamaModel("llama3.2:latest")

	got, err := a.ModelCache.Get("llama3.2:latest")
	if err != nil || got == nil {
		t.Fatalf("no cache entry after selecting the model: %v", err)
	}
	if got.ProbeLevel != "fast" || got.SupportsTools != CapYes {
		t.Errorf("entry = level %q tools %v, want fast / yes", got.ProbeLevel, got.SupportsTools)
	}
}

// Selecting again refreshes the entry, so a model changed in Ollama is
// picked up without a separate probe command.
func TestSetOllamaModel_RefreshesAStaleEntry(t *testing.T) {
	var hits int
	a := selectAgent(t, fakeOllamaShow(t, []string{"completion", "tools"}, &hits).URL)
	if err := a.ModelCache.Set(&ModelCapability{Name: "m:1", SupportsTools: CapNo, ProbeLevel: "fast"}); err != nil {
		t.Fatal(err)
	}

	a.setOllamaModel("m:1")

	got, _ := a.ModelCache.Get("m:1")
	if got == nil || got.SupportsTools != CapYes {
		t.Errorf("stale entry not refreshed: %+v", got)
	}
}

// A tool mode chosen with /model mode is the user's, and a probe must not
// erase it. The probe's own ToolMode is "" (auto), which Set writes as is.
func TestSetOllamaModel_KeepsAToolModeOverride(t *testing.T) {
	var hits int
	a := selectAgent(t, fakeOllamaShow(t, []string{"completion"}, &hits).URL)
	if err := a.ModelCache.Set(&ModelCapability{Name: "phi4:latest", ToolMode: ToolModeInject, ProbeLevel: "none"}); err != nil {
		t.Fatal(err)
	}

	a.setOllamaModel("phi4:latest")

	got, _ := a.ModelCache.Get("phi4:latest")
	if got == nil || got.ToolMode != ToolModeInject {
		t.Errorf("tool mode = %q after the probe, want %q", got.ToolMode, ToolModeInject)
	}
	if got.ProbeLevel != "fast" {
		t.Errorf("probe level = %q, want the entry probed", got.ProbeLevel)
	}
}

// A server that cannot be reached leaves the cache exactly as it was and
// does not stop the model from being selected.
func TestSetOllamaModel_UnreachableServerLeavesTheCacheAlone(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // now nothing listens there
	a := selectAgent(t, url)
	if err := a.ModelCache.Set(&ModelCapability{Name: "m:1", SupportsTools: CapYes, ProbeLevel: "fast"}); err != nil {
		t.Fatal(err)
	}

	a.setOllamaModel("m:1")

	if a.Config.Ollama.Model != "m:1" || a.Client == nil {
		t.Error("the model was not selected when the probe failed")
	}
	got, _ := a.ModelCache.Get("m:1")
	if got == nil || got.SupportsTools != CapYes {
		t.Errorf("entry changed by a failed probe: %+v", got)
	}
}

// Without a cache there is nothing to write, and nothing to fail.
func TestSetOllamaModel_NoCacheIsFine(t *testing.T) {
	a := newTestAgent(t)
	a.setOllamaModel("m:1")
	if a.Config.Ollama.Model != "m:1" {
		t.Error("model not selected")
	}
}
