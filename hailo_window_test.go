package harvey

import "testing"

// hailo-ollama gives no window through /api/show, but every 5.4.0 model is
// compiled for 2,048 tokens (Hailo's docs/MODELS.rst; measured 2026-11-03 in
// notes/hailo-ollama-api.md). The drafter used to say "window not known, 2048
// assumed".
func TestHailoWindow_IsTheEnginesKnown2048(t *testing.T) {
	hl, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, "http://127.0.0.1:1", hl.URL, true, true)
	if err := a.setHailoModel("qwen2.5-coder:1.5b"); err != nil {
		t.Fatal(err)
	}
	w := a.windowLimitFor("qwen2.5-coder:1.5b")
	if w.N != 2048 || w.Reason != "model" {
		t.Errorf("window = %+v, want N 2048 from the model's own limit", w)
	}
	if got := a.logWindow(); got.N != 2048 || got.Source != "model" {
		t.Errorf("logged window = %+v, want 2048 from model", got)
	}
	if _, assumed := DraftBudget(a.effectiveContextLimit()); assumed {
		t.Error("the drafter still assumes the window for a hailo model")
	}
}

// Ollama models are untouched: their window still comes from the probe.
func TestHailoWindow_OllamaUnchanged(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "phi4")
	a := familyAgent(t, ol.URL, "http://127.0.0.1:1", false, false)
	a.setOllamaModel("phi4")
	if w := a.windowLimitFor("phi4"); w.N == 2048 && w.Reason == "model" {
		t.Errorf("an Ollama model got the hailo window: %+v", w)
	}
}
