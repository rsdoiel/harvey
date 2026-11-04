package harvey

import (
	"io"
	"strings"
	"testing"
)

// DR-0049: naming a model on a configured service sends the session to that
// service's URL, as that service's kind, under that service's name.

func serviceAgent(t *testing.T) (*Agent, string, string) {
	t.Helper()
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	pi2, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, ol.URL, "", false, false)
	mc, err := OpenModelCache(a.Workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mc.Close() })
	a.ModelCache = mc
	a.Config.Services = map[string]ServiceConfig{"hat2": {URL: pi2.URL, Engine: "hailo"}}
	return a, ol.URL, pi2.URL
}

func TestStartupModel_OnConfiguredService(t *testing.T) {
	a, olURL, piURL := serviceAgent(t)
	if err := a.useStartupModel("hat2/qwen2.5-coder:1.5b", io.Discard); err != nil {
		t.Fatal(err)
	}
	c, ok := a.Client.(*OllamaChatClient)
	if !ok {
		t.Fatalf("client is %T", a.Client)
	}
	if c.BackendURL() != piURL || c.Service() != "hat2" || c.EngineKind() != "hailo" || c.ModelName() != "qwen2.5-coder:1.5b" {
		t.Errorf("client = url %s service %s engine %s model %s", c.BackendURL(), c.Service(), c.EngineKind(), c.ModelName())
	}
	if a.Backend == nil || a.Backend.Name() != "hailo" || a.Backend.BaseURL() != piURL {
		t.Errorf("backend = %+v", a.Backend)
	}
	if a.Config.Ollama.URL != olURL {
		t.Errorf("ollama.url changed to %s; it is the Ollama default's, not the session's", a.Config.Ollama.URL)
	}
	// The capability row is keyed by service, not by the kind (item 8).
	if got := a.modelKey("qwen2.5-coder:1.5b"); got != "hat2/qwen2.5-coder:1.5b" {
		t.Errorf("modelKey = %q", got)
	}
	if cap, _ := a.ModelCache.Get("hat2/qwen2.5-coder:1.5b"); cap == nil {
		t.Error("no capability row under hat2/…")
	}
	if cap, _ := a.ModelCache.Get("hailo/qwen2.5-coder:1.5b"); cap != nil {
		t.Error("a row was written under the default hailo's key")
	}
}

func TestStartupModel_ConfiguredServiceDownIsExit69(t *testing.T) {
	a := familyAgent(t, "http://127.0.0.1:1", "", false, false)
	a.Config.Services = map[string]ServiceConfig{"hat2": {URL: "http://127.0.0.1:1", Engine: "hailo"}}
	a.setOllamaModel("phi4")
	before := a.Client.Name()
	err := a.setServiceModel("hat2", "qwen2.5-coder:1.5b")
	if ExitCodeFor(err) != ClassUnavailable || !strings.Contains(err.Error(), "hat2") {
		t.Fatalf("err = %v (%v), want unavailable naming hat2", err, ExitCodeFor(err))
	}
	if a.Client.Name() != before {
		t.Error("the session's model changed although the service is down")
	}
}

// /model use hat2/NAME and the picker reach the same place.
func TestUseSelectedModel_OnConfiguredService(t *testing.T) {
	a, _, piURL := serviceAgent(t)
	m := ModelSummary{Name: "qwen2.5-coder:1.5b", Service: "hat2", Engine: "hailo"}
	if err := useSelectedModel(a, m, io.Discard, false); err != nil {
		t.Fatal(err)
	}
	if c := a.Client.(*OllamaChatClient); c.BackendURL() != piURL || c.Service() != "hat2" {
		t.Errorf("client = %s %s", c.BackendURL(), c.Service())
	}
}

// An alias whose engine field names a service (DR-0049 item 4).
func TestAlias_OnConfiguredService(t *testing.T) {
	a, _, piURL := serviceAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{"coder": {Model: "qwen2.5-coder:1.5b", Engine: "hat2"}}
	var out strings.Builder
	handled, err := attemptModelSwitch(a, "coder", &out)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if c := a.Client.(*OllamaChatClient); c.BackendURL() != piURL || c.Service() != "hat2" {
		t.Errorf("client = %s %s", c.BackendURL(), c.Service())
	}
}
