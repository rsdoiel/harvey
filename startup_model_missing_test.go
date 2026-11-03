package harvey

import (
	"bytes"
	"strings"
	"testing"
)

// A model named by -m or ollama.model that the server does not list used to
// start as "Connected" and fail on the first prompt (HTTP 404). Found
// 2026-11-01 (mistyped qwen2.5:rb) and again 2026-11-03 when the hailo-ollama
// 5.4.0 upgrade removed llama3.2:3b from the card. DR-0003: a named input that
// is missing is no_input (66).

func TestStartupModel_NotInstalledUnattendedIsNoInput(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4", "gemma3:4b")
	hl, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	for _, c := range []struct{ name, listed, notListed string }{
		{"qwen2.5:rb", "hailo/qwen2.5-coder:1.5b", ""},
		{"hailo/llama3.2:3b", "qwen2.5-coder:1.5b", "phi4"},
		{"ollama/qwen2.5:rb", "phi4", "qwen2.5-coder"},
	} {
		name := c.name
		a := familyAgent(t, ol.URL, hl.URL, true, true)
		a.Config.Ollama.Model = name
		var out bytes.Buffer
		err := a.pickOllamaModel(newTestBufioReader(""), &out, "")
		if ExitCodeFor(err) != ClassNoInput {
			t.Fatalf("%s: exit class = %v (%v), want no_input", name, ExitCodeFor(err), err)
		}
		msg := err.Error()
		_, bare := parseQualifiedModel(name)
		for _, want := range []string{bare, c.listed} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: error should name %q: %v", name, want, err)
			}
		}
		if c.notListed != "" && strings.Contains(msg, c.notListed) {
			t.Errorf("%s: error lists %q from the other engine: %v", name, c.notListed, err)
		}
		if a.Backend != nil {
			t.Errorf("%s: a backend was connected anyway: %s", name, a.Backend.Name())
		}
	}
}

func TestStartupModel_NotInstalledOnlyOllama(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	a := newTestAgent(t)
	a.Config.Ollama.URL = ol.URL
	a.Config.Ollama.Model = "qwen2.5:rb"
	err := a.pickOllamaModel(newTestBufioReader(""), &bytes.Buffer{}, "")
	if ExitCodeFor(err) != ClassNoInput || !strings.Contains(err.Error(), "phi4") {
		t.Fatalf("err = %v (class %v), want no_input naming phi4", err, ExitCodeFor(err))
	}
}

// The check must not reject a model the server does list.
func TestStartupModel_InstalledStillStarts(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	hl, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, ol.URL, hl.URL, true, true)
	a.Config.Ollama.Model = "hailo/qwen2.5-coder:1.5b"
	if err := a.pickOllamaModel(newTestBufioReader(""), &bytes.Buffer{}, ""); err != nil {
		t.Fatalf("an installed model must still start: %v", err)
	}
}

// At a terminal the person is told, then offered the installed models.
func TestStartupModel_NotInstalledAtTerminalOffersPicker(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	a := newTestAgent(t)
	a.Config.Ollama.URL = ol.URL
	a.Config.Ollama.Model = "qwen2.5:rb"
	attended(t, a, "")
	var out bytes.Buffer
	if err := a.pickOllamaModel(newTestBufioReader("1\n"), &out, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"qwen2.5:rb" is not installed`) {
		t.Errorf("output should say the model is not installed:\n%s", out.String())
	}
	if a.Backend == nil || a.Backend.ActiveModel() != "phi4" {
		t.Errorf("backend = %v, want phi4 chosen from the picker", a.Backend)
	}
}

// A server that cannot be asked is not evidence of absence: the old path stays.
func TestStartupModel_ServerSilentKeepsOldBehaviour(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Ollama.URL = "http://127.0.0.1:1"
	a.Config.Ollama.Model = "qwen2.5:rb"
	if err := a.pickOllamaModel(newTestBufioReader(""), &bytes.Buffer{}, ""); err != nil {
		t.Fatalf("unreachable server must not turn into no_input: %v", err)
	}
}

// A named engine whose server is not running is unavailable (69, setHailoModel),
// not "installed: none".
func TestStartupModel_NamedEngineDownIsNotNoInput(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	a := familyAgent(t, ol.URL, "http://127.0.0.1:1", false, false)
	a.Config.Ollama.Model = "hailo/qwen2.5-coder:1.5b"
	err := a.pickOllamaModel(newTestBufioReader(""), &bytes.Buffer{}, "")
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("class = %v (%v), want unavailable", ExitCodeFor(err), err)
	}
}
