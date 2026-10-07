package harvey

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Bug: --ollama and -m/--model were overwritten by ollama.url and ollama.model
// in agents/harvey.yaml, because Run loads the file after the command line has
// been applied. The command line must win.
func TestRun_CommandLineOllamaBeatsHarveyYAML(t *testing.T) {
	a, ws := runFixture(t, false)
	yaml := "ollama:\n  url: http://127.0.0.1:2\n  model: from-yaml\n"
	if err := os.WriteFile(filepath.Join(ws.HarveyDir(), "harvey.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Config.Ollama.URL = "http://127.0.0.1:1"
	a.Config.Ollama.Model = "from-flag"
	a.Config.Ollama.URLExplicit = true
	a.Config.Ollama.ModelExplicit = true
	_ = a.Run(io.Discard)
	if got := a.Config.Ollama.URL; got != "http://127.0.0.1:1" {
		t.Errorf("Ollama.URL = %q, want the --ollama value", got)
	}
	if got := a.Config.Ollama.Model; got != "from-flag" {
		t.Errorf("Ollama.Model = %q, want the --model value", got)
	}
}

// Without a flag the file still sets them.
func TestRun_HarveyYAMLOllamaAppliesWithoutFlags(t *testing.T) {
	a, ws := runFixture(t, false)
	yaml := "ollama:\n  url: http://127.0.0.1:2\n  model: from-yaml\n"
	if err := os.WriteFile(filepath.Join(ws.HarveyDir(), "harvey.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = a.Run(io.Discard)
	if got := a.Config.Ollama.Model; got != "from-yaml" {
		t.Errorf("Ollama.Model = %q, want from-yaml", got)
	}
}
