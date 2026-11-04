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
