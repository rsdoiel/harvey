package harvey

import (
	"strings"
	"testing"
)

// DR-0049 item 7: a model carries the service it is on (the name) and the
// engine (the kind). For the two default services the two are equal.

func TestListFamilyModels_CarriesServiceAndEngine(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	pi2, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	pi3, _ := ollamaFamilyServer(t, false, "llama3:8b")
	a := familyAgent(t, ol.URL, "", false, false)
	a.Config.Services = map[string]ServiceConfig{
		"hat2": {URL: pi2.URL, Engine: "hailo"},
		"mini": {URL: pi3.URL, Engine: "ollama"},
	}
	got := map[string][2]string{}
	for _, m := range a.listFamilyModels() {
		got[m.Name] = [2]string{m.Service, m.Engine}
	}
	want := map[string][2]string{
		"phi4":               {"ollama", "ollama"},
		"qwen2.5-coder:1.5b": {"hat2", "hailo"},
		"llama3:8b":          {"mini", "ollama"},
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: (service, engine) = %v, want %v", name, got[name], w)
		}
	}
}

// A configured service that is down contributes nothing and no error, as the
// defaults do.
func TestListFamilyModels_ConfiguredServiceDownIsSkipped(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	a := familyAgent(t, ol.URL, "", false, false)
	a.Config.Services = map[string]ServiceConfig{"gone": {URL: "http://127.0.0.1:1", Engine: "hailo"}}
	ms := a.listFamilyModels()
	if len(ms) != 1 || ms[0].Name != "phi4" {
		t.Errorf("models = %+v, want just phi4", ms)
	}
}

// The qualified form names the service, so it selects a model on hat2 and not
// the same name on another hailo service.
func TestQualifiedName_UsesTheService(t *testing.T) {
	m := ModelSummary{Name: "llama3.2:3b", Service: "hat2", Engine: "hailo"}
	if got := qualifiedName(m); got != "hat2/llama3.2:3b" {
		t.Errorf("qualifiedName = %q", got)
	}
	d := ModelSummary{Name: "phi4", Engine: "ollama"} // built without Service: the engine is the default service
	if got := qualifiedName(d); got != "ollama/phi4" {
		t.Errorf("qualifiedName = %q, want the engine when Service is empty", got)
	}
}

// A configured service name is a prefix, like hailo/ and ollama/ (DR-0049 item 2).
func TestParseModelRef_KnowsConfiguredServices(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Services = map[string]ServiceConfig{"hat2": {URL: "http://pi2:8000", Engine: "hailo"}}
	cases := []struct{ in, service, model string }{
		{"hat2/qwen2.5-coder:1.5b", "hat2", "qwen2.5-coder:1.5b"},
		{"HAT2/qwen", "hat2", "qwen"},
		{"hailo/llama3.2:3b", "hailo", "llama3.2:3b"},
		{"ollama/phi4", "ollama", "phi4"},
		{"user/model:tag", "", "user/model:tag"}, // a slash that is not a service stays in the name
		{"phi4", "", "phi4"},
	}
	for _, c := range cases {
		s, m := a.parseModelRef(c.in)
		if s != c.service || m != c.model {
			t.Errorf("parseModelRef(%q) = %q, %q; want %q, %q", c.in, s, m, c.service, c.model)
		}
	}
}

// matchModel narrows to one service: hat2/x does not match x on another hailo service.
func TestMatchModel_ServicePrefixSelectsWithinOneService(t *testing.T) {
	models := []ModelSummary{
		{Name: "qwen2.5-coder:1.5b", Service: "hailo", Engine: "hailo"},
		{Name: "qwen2.5-coder:1.5b", Service: "hat2", Engine: "hailo"},
		{Name: "phi4", Service: "ollama", Engine: "ollama"},
	}
	got, _, ok := matchModel(models, "hat2/qwen2.5-coder:1.5b")
	if !ok || got.ServiceName() != "hat2" {
		t.Errorf("got %+v ok=%v, want the hat2 model", got, ok)
	}
	got, _, ok = matchModel(models, "hailo/qwen2.5-coder:1.5b")
	if !ok || got.ServiceName() != "hailo" {
		t.Errorf("got %+v ok=%v, want the hailo model", got, ok)
	}
}

// requireInstalled checks the named service only.
func TestRequireInstalled_ConfiguredService(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	pi2, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, ol.URL, "", false, false)
	a.Config.Services = map[string]ServiceConfig{"hat2": {URL: pi2.URL, Engine: "hailo"}}
	if err := a.requireInstalled("hat2/qwen2.5-coder:1.5b"); err != nil {
		t.Errorf("installed model refused: %v", err)
	}
	err := a.requireInstalled("hat2/phi4") // phi4 is on ollama, not hat2
	if ExitCodeFor(err) != ClassNoInput {
		t.Fatalf("class = %v (%v), want no_input", ExitCodeFor(err), err)
	}
	if !strings.Contains(err.Error(), "qwen2.5-coder:1.5b") {
		t.Errorf("should list what hat2 has: %v", err)
	}
}

// The cache key for a model on a configured service is service/model (DR-0049 item 8).
func TestCacheKeyFor_ConfiguredService(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Services = map[string]ServiceConfig{"hat2": {URL: "http://pi2:8000", Engine: "hailo"}}
	if got := a.cacheKeyFor("hat2/qwen"); got != "hat2/qwen" {
		t.Errorf("key = %q", got)
	}
}
