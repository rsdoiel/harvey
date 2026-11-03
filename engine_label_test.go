package harvey

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// One engine label for every record of a model call (TODO "Two model services
// on one machine"). For a hailo client the banner said "(hailo)" while
// session_start, model_request and the /kb draft header said "ollama".

func TestEngineLabel_AgreesEverywhereForAHailoClient(t *testing.T) {
	// Two services, so the banner names the service (DR-0049 item 6).
	a, _, _ := bothEnginesAgent(t)
	if err := a.setHailoModel("llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	// The log opens the way a session opens it: from the connected client.
	a.openSessionLog(filepath.Join(t.TempDir(), "20261103-1200-harvey.spmd"), "", io.Discard)
	t.Cleanup(func() { a.closeSessionLog("test") })
	path := a.SessionLog.Path()

	a.AddMessage("system", "You are Harvey.")
	a.logUserInput("one")
	a.logModelRequest(withCallPurpose(t.Context(), purposeTurn, nil), a.History, 0)

	backendOf := func(kind EventKind) string {
		evs := eventsOf(t, a, path, kind)
		if len(evs) == 0 {
			t.Fatalf("no %s event", kind)
		}
		var d struct{ Backend string }
		_ = json.Unmarshal(evs[0].Data, &d)
		return d.Backend
	}
	labels := map[string]string{
		"banner":        activeModelLabel(a),
		"session_start": backendOf(KindSessionStart),
		"model_request": backendOf(KindModelRequest),
		"draft header":  a.drafterDisplay(newLLMDrafter(a.Client, "")),
		"draft record":  newLLMDrafter(a.Client, "").Name(),
	}
	for where, got := range labels {
		if !strings.Contains(strings.ToLower(got), "hailo") {
			t.Errorf("%s says %q, want hailo (all: %v)", where, got, labels)
		}
	}
}

func TestEngineLabel_PlainOllamaIsStillOllama(t *testing.T) {
	c := newOllamaLLMClient("http://127.0.0.1:1", "m", 0)
	if c.Name() != "ollama (m)" {
		t.Errorf("Name = %q", c.Name())
	}
	c.SetEngine("ollama")
	if c.Name() != "ollama (m)" {
		t.Errorf("Name with engine ollama = %q", c.Name())
	}
}

// /kb draft @NAME switches the model and restores it afterwards. The restore
// used to rebuild an Ollama client at ollama.url even when hailo was active.
func TestDispatchTarget_RestoreKeepsTheHailoEngine(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, ol.URL, hl.URL, true, true)
	if err := a.setHailoModel("llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	a.attemptModelSwitchOverride = func(n string, _ io.Writer) (bool, error) {
		a.useModelAt("ollama", ol.URL, n)
		return true, nil
	}
	target, ok, err := resolveDispatchTarget(a, "phi4", io.Discard)
	if err != nil || !ok {
		t.Fatalf("resolve: %v %v", ok, err)
	}
	target.Restore()
	ac, isChat := a.Client.(*OllamaChatClient)
	if !isChat || ac.Engine() != "hailo" || ac.BackendURL() != hl.URL || ac.ModelName() != "llama3.2:3b" {
		t.Errorf("after restore: client = %s at %s, engine %q; want llama3.2:3b on hailo at %s",
			a.Client.Name(), ac.BackendURL(), ac.Engine(), hl.URL)
	}
}

func TestPipelineMention_KeepsTheClientsEngine(t *testing.T) {
	a := newTestAgent(t)
	c := newOllamaLLMClient("http://127.0.0.1:1", "llama3.2:3b", 0)
	c.SetEngine("hailo")
	a.Client = c
	got, err := resolvePipelineClient(a, "qwen2.5-coder:1.5b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != "hailo (qwen2.5-coder:1.5b)" {
		t.Errorf("mention client = %q, want hailo (qwen2.5-coder:1.5b)", got.Name())
	}
}
