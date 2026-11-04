package harvey

import (
	"bytes"
	"strings"
	"testing"
)

// DR-0049 item 6: the service is shown only when there is a choice. With one
// service the display is the bare model; with two or more every place that
// names a model also names its service, as service/model.

func TestServiceDisplay_OneServiceShowsNoServiceWord(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "gemma3:4b", "phi4")
	a := familyAgent(t, ol.URL, "http://127.0.0.1:1", false, false) // CPU-only: no card, no hailo.url
	a.setOllamaModel("gemma3:4b")

	if got := activeModelLabel(a); got != "gemma3:4b" {
		t.Errorf("activeModelLabel = %q, want the bare model", got)
	}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"list"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "ollama") {
		t.Errorf("/model list prints the word ollama on a CPU-only machine:\n%s", out.String())
	}
	out.Reset()
	if err := cmdModel(a, nil, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "ollama") {
		t.Errorf("/model prints the word ollama on a CPU-only machine:\n%s", out.String())
	}
}

func TestServiceDisplay_TwoServicesAlwaysNameTheService(t *testing.T) {
	a, _, _ := bothEnginesAgent(t)
	if err := a.setHailoModel("llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	if got := activeModelLabel(a); got != "hailo/llama3.2:3b" {
		t.Errorf("activeModelLabel on hailo = %q, want hailo/llama3.2:3b", got)
	}
	a.setOllamaModel("phi4")
	if got := activeModelLabel(a); got != "ollama/phi4" {
		t.Errorf("activeModelLabel on ollama = %q, want ollama/phi4", got)
	}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"list"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ollama/phi4", "hailo/llama3.2:3b", "ollama/llama3.2:3b"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("/model list should name %q:\n%s", want, out.String())
		}
	}
}

// A hailo-only machine (Ollama not running) has one service: no word either.
func TestServiceDisplay_HailoOnlyShowsNoServiceWord(t *testing.T) {
	hl, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, "http://127.0.0.1:1", hl.URL, true, true)
	if err := a.setHailoModel("qwen2.5-coder:1.5b"); err != nil {
		t.Fatal(err)
	}
	if got := activeModelLabel(a); got != "qwen2.5-coder:1.5b" {
		t.Errorf("activeModelLabel = %q, want the bare model", got)
	}
}

// The words a person types back must work: the displayed name selects itself.
func TestServiceDisplay_DisplayedNameIsUsable(t *testing.T) {
	a, _, hl := bothEnginesAgent(t)
	if err := a.setHailoModel("llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	shown := activeModelLabel(a)
	a.setOllamaModel("phi4")
	if err := cmdModel(a, []string{"use", shown}, &bytes.Buffer{}); err != nil {
		t.Fatalf("/model use %s: %v", shown, err)
	}
	if a.Backend.Name() != "hailo" || a.Backend.BaseURL() != hl {
		t.Errorf("backend = %s at %s, want hailo at %s", a.Backend.Name(), a.Backend.BaseURL(), hl)
	}
}

func TestServiceDisplay_StatusAndPickerFollowTheRule(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "gemma3:4b")
	one := familyAgent(t, ol.URL, "http://127.0.0.1:1", false, false)
	one.setOllamaModel("gemma3:4b")
	var out bytes.Buffer
	if err := cmdStatus(one, nil, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "ollama (") || !strings.Contains(out.String(), "gemma3:4b") {
		t.Errorf("/status on one service should show the bare model:\n%s", out.String())
	}

	two, _, _ := bothEnginesAgent(t)
	if err := two.setHailoModel("llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cmdStatus(two, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hailo/llama3.2:3b") {
		t.Errorf("/status on two services should name hailo/llama3.2:3b:\n%s", out.String())
	}
	if got := two.clientDisplay(two.Client); got != "hailo/llama3.2:3b" {
		t.Errorf("clientDisplay = %q", got)
	}
	if got := one.clientDisplay(one.Client); got != "gemma3:4b" {
		t.Errorf("clientDisplay on one service = %q", got)
	}
}

// A configured service counts as a service (DR-0049 item 6): Ollama plus hat2
// is a choice, so every place that names a model names its service, and the
// service is hat2 (its name), not hailo (its kind).
func TestServiceDisplay_ConfiguredServiceIsACountedService(t *testing.T) {
	a, _, _ := serviceAgent(t) // ollama default (phi4) + hat2 (hailo kind, qwen2.5-coder:1.5b)
	if n := a.serviceCount(); n != 2 {
		t.Fatalf("serviceCount = %d, want 2", n)
	}
	if err := a.setServiceModel("hat2", "qwen2.5-coder:1.5b"); err != nil {
		t.Fatal(err)
	}
	if got := activeModelLabel(a); got != "hat2/qwen2.5-coder:1.5b" {
		t.Errorf("activeModelLabel = %q, want hat2/qwen2.5-coder:1.5b", got)
	}
	if got := a.clientDisplay(a.Client); got != "hat2/qwen2.5-coder:1.5b" {
		t.Errorf("clientDisplay = %q", got)
	}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"list"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hat2/qwen2.5-coder:1.5b", "ollama/phi4"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("/model list should name %q:\n%s", want, out.String())
		}
	}
}

// A configured service that is down is not a choice: one service answers, so
// no service word.
func TestServiceDisplay_ConfiguredServiceDownIsNotCounted(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	a := familyAgent(t, ol.URL, "", false, false)
	a.Config.Services = map[string]ServiceConfig{"hat2": {URL: "http://127.0.0.1:1", Engine: "hailo"}}
	if n := a.serviceCount(); n != 1 {
		t.Errorf("serviceCount = %d, want 1", n)
	}
}

// Two configured services of the same kind are two services.
func TestServiceDisplay_TwoConfiguredServicesOfOneKind(t *testing.T) {
	p2, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	p3, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, "http://127.0.0.1:1", "", false, false) // no default service answers
	a.Config.Services = map[string]ServiceConfig{
		"hat2": {URL: p2.URL, Engine: "hailo"},
		"hat3": {URL: p3.URL, Engine: "hailo"},
	}
	if n := a.serviceCount(); n != 2 {
		t.Fatalf("serviceCount = %d, want 2", n)
	}
	if err := a.setServiceModel("hat3", "qwen2.5-coder:1.5b"); err != nil {
		t.Fatal(err)
	}
	if got := activeModelLabel(a); got != "hat3/qwen2.5-coder:1.5b" {
		t.Errorf("activeModelLabel = %q, want the service name, not the kind", got)
	}
}
