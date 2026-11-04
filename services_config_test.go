package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// services: (DR-0049 item 10) names servers beyond the two defaults: a name, a
// URL and an engine kind. A wrong entry is a configuration error (exit 78).

func loadServicesYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "harvey.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	return cfg, LoadHarveyYAML(&Workspace{Root: dir}, cfg)
}

func TestServicesConfig_Read(t *testing.T) {
	cfg, err := loadServicesYAML(t, `services:
  hat2:
    url: http://pi2.lan:8000
    engine: hailo
  mini:
    url: http://pi3.lan:11434
    engine: ollama
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Services) != 2 {
		t.Fatalf("services = %+v, want 2", cfg.Services)
	}
	got := cfg.Services["hat2"]
	if got.URL != "http://pi2.lan:8000" || got.Engine != "hailo" {
		t.Errorf("hat2 = %+v", got)
	}
	if cfg.Services["mini"].Engine != "ollama" {
		t.Errorf("mini = %+v", cfg.Services["mini"])
	}
}

func TestServicesConfig_AbsentIsEmpty(t *testing.T) {
	cfg, err := loadServicesYAML(t, "ollama:\n  model: phi4\n")
	if err != nil || len(cfg.Services) != 0 {
		t.Fatalf("err=%v services=%v", err, cfg.Services)
	}
}

func TestServicesConfig_WrongEntriesAreExit78(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"no url":          {"services:\n  a:\n    engine: hailo\n", "url"},
		"no engine":       {"services:\n  a:\n    url: http://x:1\n", "engine"},
		"unknown engine":  {"services:\n  a:\n    url: http://x:1\n    engine: llamacpp\n", "engine"},
		"reserved name":   {"services:\n  ollama:\n    url: http://x:1\n    engine: hailo\n", "ollama"},
		"reserved hailo":  {"services:\n  Hailo:\n    url: http://x:1\n    engine: ollama\n", "hailo"},
		"slash in name":   {"services:\n  a/b:\n    url: http://x:1\n    engine: ollama\n", "a/b"},
		"bad url":         {"services:\n  a:\n    url: pi2\n    engine: ollama\n", "url"},
		"same name, case": {"services:\n  Pi:\n    url: http://x:1\n    engine: ollama\n  pi:\n    url: http://y:1\n    engine: ollama\n", "pi"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadServicesYAML(t, c.yaml)
			if err == nil {
				t.Fatal("accepted")
			}
			if ExitCodeFor(err) != ClassConfig {
				t.Errorf("class = %v, want config: %v", ExitCodeFor(err), err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), c.want) {
				t.Errorf("message should mention %q: %v", c.want, err)
			}
		})
	}
}

// Other Save* functions rewrite harvey.yaml from what they read; the services
// section must come through them untouched.
func TestServicesConfig_SurvivesSave(t *testing.T) {
	dir := t.TempDir()
	ws := &Workspace{Root: dir}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "services:\n  hat2:\n    url: http://pi2.lan:8000\n    engine: hailo\n"
	if err := os.WriteFile(filepath.Join(dir, "agents", "harvey.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if err := LoadHarveyYAML(ws, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveMemoryConfig(ws, cfg); err != nil {
		t.Fatal(err)
	}
	back := DefaultConfig()
	if err := LoadHarveyYAML(ws, back); err != nil {
		t.Fatal(err)
	}
	if back.Services["hat2"] != cfg.Services["hat2"] || back.Services["hat2"].Engine != "hailo" {
		t.Errorf("save lost it: %+v", back.Services)
	}
}
