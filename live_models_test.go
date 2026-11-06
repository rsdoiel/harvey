package harvey

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Live model tests: they talk to the real system on this machine and are
// skipped, with the reason shown, when it is not there. They check the
// assumptions Harvey's model discovery rests on. Run them with
// `make test-models` (go test -v -run '^TestLive').

func TestLive_Ollama_ListModelsMatchesCLI(t *testing.T) {
	cfg := requireModelSystem(t, "ollama")
	models, err := NewOllamaBackend(cfg.Ollama.URL, 10*time.Second, t.TempDir()).ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	got := map[string]bool{}
	for _, m := range models {
		if m.Engine != "ollama" {
			t.Errorf("%s: Engine = %q, want ollama", m.Name, m.Engine)
		}
		got[m.Name] = true
	}
	bin, err := exec.LookPath("ollama")
	if err != nil {
		t.Logf("ollama CLI not on PATH; compared nothing, %d models listed over HTTP", len(models))
		return
	}
	out, err := exec.Command(bin, "list").Output()
	if err != nil {
		t.Skipf("`ollama list` failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, ln := range lines[1:] { // first line is the header
		if f := strings.Fields(ln); len(f) > 0 && !got[f[0]] {
			t.Errorf("`ollama list` shows %q, which /api/tags did not", f[0])
		}
	}
	if len(lines)-1 != len(models) {
		t.Errorf("`ollama list` has %d models, HTTP listing has %d", len(lines)-1, len(models))
	}
}

func TestLive_Hailo_ListModels(t *testing.T) {
	cfg := requireModelSystem(t, "hailo")
	models, err := NewHailoBackend(cfg.Hailo.URL, 10*time.Second, t.TempDir()).ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	for _, m := range models {
		if m.Engine != "hailo" {
			t.Errorf("%s: Engine = %q, want hailo", m.Name, m.Engine)
		}
	}
	t.Logf("hailo-ollama lists %d models", len(models))
}
