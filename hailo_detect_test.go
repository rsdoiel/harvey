package harvey

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// Step 1 of the Hailo brief (DR-0030): the card and the server are detected
// apart, and a machine with neither is never probed.

// hailoListServer serves GET /hailo/v1/list with body and counts every request.
func hailoListServer(t *testing.T, status int, body string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/hailo/v1/list" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// fakeDevice returns a path that exists (a card) or does not (no card).
func fakeDevice(t *testing.T, present bool) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hailo0")
	if present {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

const hailoListBody = `{"models":["llama3.2:3b","qwen2.5-coder:1.5b"]}`

func TestHailoConfig_DefaultsToPort8000AndNotSet(t *testing.T) {
	h := DefaultConfig().Hailo
	if h.URL != "http://localhost:8000" {
		t.Errorf("default hailo URL = %q, want http://localhost:8000", h.URL)
	}
	if h.URLSet {
		t.Error("default hailo URL must not count as set")
	}
}

func TestLoadHarveyYAML_HailoURL(t *testing.T) {
	cfg, err := loadSkillsYAMLForTest(t, "hailo:\n  url: http://pi5.local:8000\n")
	if err != nil {
		t.Fatalf("LoadHarveyYAML: %v", err)
	}
	if cfg.Hailo.URL != "http://pi5.local:8000" || !cfg.Hailo.URLSet {
		t.Errorf("hailo = %+v, want URL http://pi5.local:8000 and URLSet", cfg.Hailo)
	}
}

func TestLoadHarveyYAML_HailoUnsetKeepsDefault(t *testing.T) {
	cfg, err := loadSkillsYAMLForTest(t, "ollama:\n  model: x\n")
	if err != nil {
		t.Fatalf("LoadHarveyYAML: %v", err)
	}
	if cfg.Hailo.URL != "http://localhost:8000" || cfg.Hailo.URLSet {
		t.Errorf("hailo = %+v, want the default and not set", cfg.Hailo)
	}
}

// The whole point of the probe rule: neither a card nor a configured URL means
// no request, so a machine without the HAT pays nothing at start.
func TestDetectHailo_NoCardNoURL_MakesNoRequest(t *testing.T) {
	srv, hits := hailoListServer(t, 200, hailoListBody)
	st := DetectHailo(HailoConfig{URL: srv.URL}, fakeDevice(t, false))
	if st.Probed || st.CardPresent || st.ServerUp {
		t.Errorf("status = %+v, want nothing detected or probed", st)
	}
	if n := atomic.LoadInt32(hits); n != 0 {
		t.Errorf("made %d request(s), want none", n)
	}
}

func TestDetectHailo_CardProbesDefaultURL(t *testing.T) {
	srv, hits := hailoListServer(t, 200, hailoListBody)
	st := DetectHailo(HailoConfig{URL: srv.URL}, fakeDevice(t, true))
	if !st.CardPresent || !st.Probed || !st.ServerUp {
		t.Fatalf("status = %+v, want card, probed and up", st)
	}
	if st.URL != srv.URL {
		t.Errorf("URL = %q, want %q", st.URL, srv.URL)
	}
	if len(st.Catalog) != 2 || st.Catalog[0] != "llama3.2:3b" {
		t.Errorf("catalog = %v", st.Catalog)
	}
	if atomic.LoadInt32(hits) != 1 {
		t.Errorf("hits = %d, want 1", atomic.LoadInt32(hits))
	}
}

// A remote hailo-ollama has no card here; the configured URL is enough.
func TestDetectHailo_ConfiguredURLWithoutCard(t *testing.T) {
	srv, _ := hailoListServer(t, 200, hailoListBody)
	st := DetectHailo(HailoConfig{URL: srv.URL, URLSet: true}, fakeDevice(t, false))
	if st.CardPresent {
		t.Error("CardPresent = true, want false")
	}
	if !st.Probed || !st.ServerUp {
		t.Errorf("status = %+v, want probed and up", st)
	}
}

// A card with the service stopped is reported as such, not as absence.
func TestDetectHailo_CardWithServerDown(t *testing.T) {
	srv, _ := hailoListServer(t, 200, hailoListBody)
	url := srv.URL
	srv.Close()
	st := DetectHailo(HailoConfig{URL: url}, fakeDevice(t, true))
	if !st.CardPresent || !st.Probed || st.ServerUp {
		t.Errorf("status = %+v, want card present, probed, server down", st)
	}
}

// /hailo/v1/list is non-standard: a 404 (regular Ollama), a non-200, or a
// changed shape all mean "not Hailo", never an error.
func TestDetectHailo_NotHailoShapes(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
	}{
		"404 as Ollama answers": {404, "404 page not found"},
		"server error":          {500, "boom"},
		"not JSON":              {200, "<html>"},
		"no models key":         {200, `{"items":[]}`},
		"models not an array":   {200, `{"models":"llama3.2:3b"}`},
		"models of objects":     {200, `{"models":[{"name":"x"}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := hailoListServer(t, c.status, c.body)
			st := DetectHailo(HailoConfig{URL: srv.URL, URLSet: true}, fakeDevice(t, false))
			if !st.Probed || st.ServerUp {
				t.Errorf("status = %+v, want probed and not up", st)
			}
			if len(st.Catalog) != 0 {
				t.Errorf("catalog = %v, want empty", st.Catalog)
			}
		})
	}
}

// SaveMemoryConfig rewrites harvey.yaml from what it read; the hailo: section
// must come through it.
func TestSaveMemoryConfig_KeepsHailoSection(t *testing.T) {
	dir := t.TempDir()
	ws := &Workspace{Root: dir}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agents", "harvey.yaml")
	if err := os.WriteFile(path, []byte("hailo:\n  url: http://pi5.local:8000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveMemoryConfig(ws, DefaultConfig()); err != nil {
		t.Fatalf("SaveMemoryConfig: %v", err)
	}
	cfg := DefaultConfig()
	if err := LoadHarveyYAML(ws, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Hailo.URL != "http://pi5.local:8000" || !cfg.Hailo.URLSet {
		t.Errorf("hailo after save = %+v", cfg.Hailo)
	}
}
