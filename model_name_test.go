package harvey

import (
	"io"
	"os"
	"strings"
	"testing"
)

// Section 7 of the Hailo brief (DR-0030): a model is named bare or as
// engine/model. A bare name on two engines is never picked silently.

func TestParseQualifiedModel(t *testing.T) {
	for _, c := range []struct{ in, engine, model string }{
		{"hailo/llama3.2:3b", "hailo", "llama3.2:3b"},
		{"Ollama/qwen2.5-coder:1.5b", "ollama", "qwen2.5-coder:1.5b"},
		{"llamacpp/Phi4-Q4", "llamacpp", "Phi4-Q4"},
		{"llamafile/phi4", "llamafile", "phi4"},
		{"llama3.2:3b", "", "llama3.2:3b"},
		{"user/model:tag", "", "user/model:tag"}, // a slash in a name is not an engine
		{"hailo/", "", "hailo/"},                 // nothing after the engine
		{"", "", ""},
	} {
		e, m := parseQualifiedModel(c.in)
		if e != c.engine || m != c.model {
			t.Errorf("parseQualifiedModel(%q) = (%q, %q), want (%q, %q)", c.in, e, m, c.engine, c.model)
		}
	}
}

func TestMatchModel_QualifiedNames(t *testing.T) {
	models := []ModelSummary{
		{Name: "llama3.2:3b", Engine: "ollama"},
		{Name: "llama3.2:3b", Engine: "hailo"},
		{Name: "qwen2.5-coder:1.5b", Engine: "hailo"},
	}
	m, amb, ok := matchModel(models, "hailo/llama3.2:3b")
	if !ok || m.Engine != "hailo" || len(amb) != 0 {
		t.Errorf("hailo/llama3.2:3b = %+v, %v, %v", m, amb, ok)
	}
	m, _, ok = matchModel(models, "ollama/llama3.2:3b")
	if !ok || m.Engine != "ollama" {
		t.Errorf("ollama/llama3.2:3b = %+v, %v", m, ok)
	}
	m, _, ok = matchModel(models, "hailo/qwen")
	if !ok || m.Name != "qwen2.5-coder:1.5b" {
		t.Errorf("qualified prefix = %+v, %v", m, ok)
	}
	if _, _, ok = matchModel(models, "ollama/qwen"); ok {
		t.Error("ollama/qwen matched a model that only exists on hailo")
	}
	if _, amb, ok = matchModel(models, "llama3.2:3b"); ok || len(amb) != 2 {
		t.Errorf("bare name on two engines: ok=%v, %d candidates; want ambiguous (2)", ok, len(amb))
	}
}

// attended makes the session look like it has a person at a terminal.
func attended(t *testing.T, a *Agent, input string) {
	t.Helper()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devnull.Close() })
	a.stdin = devnull
	a.In = strings.NewReader(input)
	old := isTerminal
	isTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { isTerminal = old })
}

func bothEnginesAgent(t *testing.T) (*Agent, string, string) {
	t.Helper()
	ol, _ := ollamaFamilyServer(t, false, "llama3.2:3b", "phi4")
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	return familyAgent(t, ol.URL, hl.URL, true, true), ol.URL, hl.URL
}

func TestModelUse_QualifiedName(t *testing.T) {
	a, ol, hl := bothEnginesAgent(t)
	if err := cmdModel(a, []string{"use", "hailo/llama3.2:3b"}, io.Discard); err != nil {
		t.Fatalf("/model use hailo/...: %v", err)
	}
	if a.Backend.Name() != "hailo" || a.Backend.BaseURL() != hl {
		t.Errorf("backend = %s at %s, want hailo at %s", a.Backend.Name(), a.Backend.BaseURL(), hl)
	}
	if err := cmdModel(a, []string{"use", "ollama/llama3.2:3b"}, io.Discard); err != nil {
		t.Fatalf("/model use ollama/...: %v", err)
	}
	if a.Backend.Name() != "ollama" || a.Backend.BaseURL() != ol {
		t.Errorf("backend = %s at %s, want ollama at %s", a.Backend.Name(), a.Backend.BaseURL(), ol)
	}
}

// Unattended, a bare name on two engines is a usage error naming both forms.
func TestModelUse_BareNameOnTwoEnginesUnattendedIsUsage(t *testing.T) {
	a, _, _ := bothEnginesAgent(t)
	err := cmdModel(a, []string{"use", "llama3.2:3b"}, io.Discard)
	if ExitCodeFor(err) != ClassUsage {
		t.Fatalf("exit class = %v (%v), want usage", ExitCodeFor(err), err)
	}
	for _, want := range []string{"hailo/llama3.2:3b", "ollama/llama3.2:3b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %s", err, want)
		}
	}
	if a.Backend != nil {
		t.Errorf("a model was chosen anyway: %s", a.Backend.Name())
	}
}

// At a terminal the same name asks.
func TestModelUse_BareNameOnTwoEnginesAtATerminalAsks(t *testing.T) {
	a, _, hl := bothEnginesAgent(t)
	attended(t, a, "2\n") // 1 ollama/llama3.2:3b, 2 hailo/llama3.2:3b
	var out strings.Builder
	if err := cmdModel(a, []string{"use", "llama3.2:3b"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	if !strings.Contains(out.String(), "ollama/llama3.2:3b") || !strings.Contains(out.String(), "hailo/llama3.2:3b") {
		t.Errorf("the question did not show the qualified forms:\n%s", out.String())
	}
	if a.Backend == nil || a.Backend.Name() != "hailo" || a.Backend.BaseURL() != hl {
		t.Errorf("backend = %v, want hailo (the second choice)", a.Backend)
	}
}

func TestModelUse_BareNameOnOneEngineNeedsNoQuestion(t *testing.T) {
	a, _, _ := bothEnginesAgent(t)
	if err := cmdModel(a, []string{"use", "phi4"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if a.Backend.Name() != "ollama" {
		t.Errorf("backend = %s, want ollama", a.Backend.Name())
	}
}

// ─── -m / ollama.model at start-up ───────────────────────────────────────────

func TestStartupModelFlag_Qualified(t *testing.T) {
	a, _, hl := bothEnginesAgent(t)
	a.Config.Ollama.Model = "hailo/llama3.2:3b"
	if err := a.pickOllamaModel(newTestBufioReader(""), io.Discard, ""); err != nil {
		t.Fatal(err)
	}
	if a.Backend.Name() != "hailo" || a.Backend.BaseURL() != hl || a.Backend.ActiveModel() != "llama3.2:3b" {
		t.Errorf("backend = %s at %s model %s", a.Backend.Name(), a.Backend.BaseURL(), a.Backend.ActiveModel())
	}

	b, _, _ := bothEnginesAgent(t)
	b.Config.Ollama.Model = "ollama/llama3.2:3b"
	if err := b.pickOllamaModel(newTestBufioReader(""), io.Discard, ""); err != nil {
		t.Fatal(err)
	}
	if b.Backend.Name() != "ollama" || b.Backend.ActiveModel() != "llama3.2:3b" {
		t.Errorf("backend = %s model %s", b.Backend.Name(), b.Backend.ActiveModel())
	}
}

func TestStartupModelFlag_BareOnTwoEnginesUnattendedIsUsage(t *testing.T) {
	a, _, _ := bothEnginesAgent(t)
	a.Config.Ollama.Model = "llama3.2:3b"
	err := a.pickOllamaModel(newTestBufioReader(""), io.Discard, "")
	if ExitCodeFor(err) != ClassUsage {
		t.Fatalf("exit class = %v (%v), want usage", ExitCodeFor(err), err)
	}
	if !strings.Contains(err.Error(), "hailo/llama3.2:3b") || !strings.Contains(err.Error(), "ollama/llama3.2:3b") {
		t.Errorf("error should name both forms: %v", err)
	}
	if a.Backend != nil {
		t.Errorf("a model was chosen anyway: %s", a.Backend.Name())
	}
}

func TestStartupModelFlag_BareOnTwoEnginesAtATerminalAsks(t *testing.T) {
	a, _, _ := bothEnginesAgent(t)
	a.Config.Ollama.Model = "llama3.2:3b"
	attended(t, a, "1\n")
	if err := a.pickOllamaModel(newTestBufioReader(""), io.Discard, ""); err != nil {
		t.Fatal(err)
	}
	if a.Backend == nil || a.Backend.Name() != "ollama" {
		t.Errorf("backend = %v, want ollama (first choice)", a.Backend)
	}
}

// A bare name that only the Hailo server has is used on it.
func TestStartupModelFlag_BareNameOnlyOnHailo(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	hl, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, ol.URL, hl.URL, true, true)
	a.Config.Ollama.Model = "qwen2.5-coder:1.5b"
	if err := a.pickOllamaModel(newTestBufioReader(""), io.Discard, ""); err != nil {
		t.Fatal(err)
	}
	if a.Backend.Name() != "hailo" {
		t.Errorf("backend = %s, want hailo", a.Backend.Name())
	}
}

// Without a card or hailo.url nothing about the flag changes: it goes to Ollama.
func TestStartupModelFlag_PlainOllamaUnchanged(t *testing.T) {
	ol, hits := ollamaFamilyServer(t, false, "phi4")
	a := familyAgent(t, ol.URL, "http://127.0.0.1:1", false, false)
	a.Config.Ollama.Model = "not-pulled:1b"
	if err := a.pickOllamaModel(newTestBufioReader(""), io.Discard, ""); err != nil {
		t.Fatal(err)
	}
	if a.Backend == nil || a.Backend.Name() != "ollama" || a.Backend.ActiveModel() != "not-pulled:1b" {
		t.Errorf("backend = %v", a.Backend)
	}
	_ = hits
}

// /model limit and /model mode with an explicit MODEL reach any engine's row.
func TestModelLimit_QualifiedNameKeysTheEngineRow(t *testing.T) {
	a, _, _ := bothEnginesAgent(t)
	mc, _ := OpenModelCache(a.Workspace, "")
	defer mc.Close()
	a.ModelCache = mc
	if err := cmdModelLimit(a, []string{"hailo/llama3.2:3b", "650"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if cap, _ := mc.Get("hailo/llama3.2:3b"); cap == nil || cap.MaxPromptTokens != 650 {
		t.Errorf("hailo row = %+v", cap)
	}
	if cap, _ := mc.Get("llama3.2:3b"); cap != nil {
		t.Errorf("the Ollama row was written: %+v", cap)
	}
}
