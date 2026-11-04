package harvey

import (
	"io"
	"strings"
	"testing"
)

// draft.model (DR-0030 items 8 and 9, DR-0049 item 5): the model that drafts
// is chosen per job. @NAME on the line beats it; unset means the session's own
// model; a service that is down stops the run, never falls back.

func TestDraftModel_SettingIsTheDrafter(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	var asked []string
	e.a.learnDrafterOverride = func(name string, out io.Writer) (Drafter, func(), error) {
		asked = append(asked, name)
		return e.drafts, func() {}, nil
	}
	e.drafts.replies = []string{"note: one", "note: two", "note: three"}

	e.a.Config.Draft.Model = "hailo/llama3.2:3b"
	if _, err := e.run(t, "--project", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t, "@granite", "--project", "demo"); err != nil {
		t.Fatal(err)
	}
	e.a.Config.Draft.Model = ""
	if _, err := e.run(t, "--project", "demo"); err != nil {
		t.Fatal(err)
	}
	want := []string{"hailo/llama3.2:3b", "granite", ""}
	if strings.Join(asked, "|") != strings.Join(want, "|") {
		t.Errorf("drafters asked for %q, want %q (setting, @NAME beats setting, unset = active model)", asked, want)
	}
}

// The service named by draft.model is down: exit 69, naming it, nothing written,
// and the session's own model untouched (no silent fallback).
func TestDraftModel_ServiceDownIsExit69WithNoFallback(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.a.learnDrafterOverride = nil // the real resolution
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	e.a.Config.Ollama.URL = ol.URL
	e.a.Config.Hailo = HailoConfig{URL: "http://127.0.0.1:1", URLSet: true}
	e.a.setOllamaModel("phi4")
	before := e.a.Client.Name()

	e.a.Config.Draft.Model = "hailo/qwen2.5-coder:1.5b"
	_, err := e.run(t, "--project", "demo")
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("class = %v (%v), want unavailable", ExitCodeFor(err), err)
	}
	if !strings.Contains(err.Error(), "hailo") {
		t.Errorf("the error should name the service: %v", err)
	}
	if e.drafts.calls != 0 {
		t.Error("a model was asked anyway")
	}
	if e.a.Client.Name() != before {
		t.Errorf("the session's model changed from %q to %q", before, e.a.Client.Name())
	}
}

func TestLoadHarveyYAML_DraftModel(t *testing.T) {
	cfg, err := loadSkillsYAMLForTest(t, "draft:\n  model: hailo/qwen2.5-coder:1.5b\n")
	if err != nil {
		t.Fatalf("LoadHarveyYAML: %v", err)
	}
	if cfg.Draft.Model != "hailo/qwen2.5-coder:1.5b" {
		t.Errorf("draft.model = %q", cfg.Draft.Model)
	}
	none, err := loadSkillsYAMLForTest(t, "ollama:\n  model: x\n")
	if err != nil || none.Draft.Model != "" {
		t.Errorf("unset draft.model = %q (%v), want empty", none.Draft.Model, err)
	}
}

// draft.model is checked at start-up (DR-0049 item 5): a model its service
// does not have is a warning naming the setting; chat still starts, and
// /kb draft keeps its own hard error.
func TestDraftModel_StartupCheck(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	pi2, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	mk := func(draft string) *Agent {
		a := familyAgent(t, ol.URL, "", false, false)
		a.Config.Services = map[string]ServiceConfig{
			"hat2": {URL: pi2.URL, Engine: "hailo"},
			"gone": {URL: "http://127.0.0.1:1", Engine: "hailo"},
		}
		a.Config.Draft.Model = draft
		return a
	}
	cases := []struct {
		name, draft string
		warn        bool
		mention     string
	}{
		{"unset", "", false, ""},
		{"installed on a configured service", "hat2/qwen2.5-coder:1.5b", false, ""},
		{"installed on a default", "phi4", false, ""},
		{"not on its service", "hat2/phi4", true, "qwen2.5-coder:1.5b"}, // lists what hat2 has
		{"mistyped bare name", "phi5", true, "phi4"},
		{"service down: cannot be asked", "gone/qwen2.5-coder:1.5b", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out strings.Builder
			mk(c.draft).checkDraftModel(&out)
			got := out.String()
			if c.warn != (got != "") {
				t.Fatalf("warn = %v, output %q", c.warn, got)

			}
			if c.warn {
				for _, want := range []string{"draft.model", c.draft, c.mention} {
					if !strings.Contains(got, want) {
						t.Errorf("warning should mention %q: %q", want, got)
					}
				}
			}
		})
	}
}

// The real resolution (no test override) reaches a model on a configured
// service and leaves the session's own model as it was; a model that service
// does not have stops the run with the name in the message and nothing written.
func TestDraftModel_ConfiguredServiceThroughRealResolution(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.a.learnDrafterOverride = nil
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	pi2, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	e.a.Config.Ollama.URL = ol.URL
	e.a.Config.Hailo = HailoConfig{URL: "http://127.0.0.1:1", URLSet: true}
	e.a.Config.Services = map[string]ServiceConfig{"hat2": {URL: pi2.URL, Engine: "hailo"}}
	e.a.setOllamaModel("phi4")
	before := e.a.Client.Name()

	e.a.Config.Draft.Model = "hat2/phi4"
	_, err := e.run(t, "--project", "demo")
	if err == nil || !strings.Contains(err.Error(), "hat2/phi4") {
		t.Fatalf("err = %v, want a stop naming hat2/phi4", err)
	}
	if ExitCodeFor(err) != ClassNoInput {
		t.Errorf("class = %v, want no_input (66)", ExitCodeFor(err))
	}
	if e.drafts.calls != 0 {
		t.Error("a model was asked anyway")
	}
	if e.a.Client.Name() != before {
		t.Errorf("session model changed to %s", e.a.Client.Name())
	}
}

func TestDraftModel_ConfiguredServiceResolvesAndRestores(t *testing.T) {
	a, _, piURL := serviceAgent(t)
	a.setOllamaModel("phi4")
	before := a.Client.Name()
	d, restore, err := resolveDrafter(a, "hat2/qwen2.5-coder:1.5b", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := a.Client.(*OllamaChatClient); !ok || c.BackendURL() != piURL {
		t.Errorf("while drafting the client is %v, want one at %s", a.Client, piURL)
	}
	if d == nil {
		t.Fatal("no drafter")
	}
	if a.Backend == nil || a.Backend.BaseURL() != piURL {
		t.Errorf("while drafting the backend is %+v", a.Backend)
	}
	restore()
	if a.Client.Name() != before {
		t.Errorf("restore left %s, want %s", a.Client.Name(), before)
	}
	if c := a.Client.(*OllamaChatClient); c.Service() != "ollama" {
		t.Errorf("restore left service %s", c.Service())
	}
	if a.Backend == nil || a.Backend.Name() != "ollama" {
		t.Errorf("restore left backend %+v, want the Ollama one", a.Backend)
	}
}
