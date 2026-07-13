package harvey

import (
	"io"
	"strings"
	"testing"
)

func TestResolveDispatchTarget_RouteEndpoint_NoAgentMutation(t *testing.T) {
	a := newTestAgent(t)
	origClient := &mockLLMClient{reply: "orig"}
	a.Client = origClient
	a.Routes = NewRouteRegistry()
	a.Routes.Add(&RouteEndpoint{Name: "remote", URL: "ollama://example:11434", Model: "llama3.1:8b", Kind: KindOllama})

	var out strings.Builder
	target, ok, err := resolveDispatchTarget(a, "remote", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a registered route")
	}
	if target.Client == LLMClient(origClient) {
		t.Error("expected an independent client for a route endpoint, not a.Client")
	}
	if a.Client != origClient {
		t.Error("route dispatch must not mutate a.Client")
	}
	if a.Backend != nil {
		t.Error("route dispatch must not touch a.Backend")
	}
	target.Restore() // must be safe to call and a true no-op
	if a.Client != origClient {
		t.Error("Restore must not mutate a.Client for a route target")
	}
}

func TestResolveDispatchTarget_AlreadyActive_SkipsSwitch(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Active = "phi-mini"
	origClient := &mockLLMClient{reply: "orig"}
	a.Client = origClient

	calls := 0
	a.attemptModelSwitchOverride = func(name string, out io.Writer) (bool, error) {
		calls++
		return true, nil
	}

	var out strings.Builder
	// Case-insensitive match against the already-active model.
	target, ok, err := resolveDispatchTarget(a, "Phi-Mini", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true when name matches the already-active model")
	}
	if calls != 0 {
		t.Errorf("expected no switch attempted when already active, got %d call(s)", calls)
	}
	if target.Client != LLMClient(origClient) {
		t.Error("expected target.Client to be the already-active a.Client")
	}
}

// TestResolveDispatchTarget_LocalSwitch_RestoreReturnsToOriginal is the direct
// regression test for Bug 2 (subagent-dispatch-design.md): the pre-fix
// cmdPlanNext restored via a.Config.Llamafile.Active read AFTER the switch —
// a field the switch itself had already overwritten to the step's own model.
// This test simulates that exact mutation via attemptModelSwitchOverride and
// asserts Restore() switches back to the name captured BEFORE the switch.
func TestResolveDispatchTarget_LocalSwitch_RestoreReturnsToOriginal(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Llamafile.Active = "original-model"
	a.Client = &mockLLMClient{reply: "orig"}

	var switchedTo []string
	a.attemptModelSwitchOverride = func(name string, out io.Writer) (bool, error) {
		switchedTo = append(switchedTo, name)
		// Simulate switchLlamafileModel's real mutation (llamafile.go:220):
		// it overwrites a.Config.Llamafile.Active to the newly-switched name.
		a.Config.Llamafile.Active = name
		a.Client = &mockLLMClient{reply: "reply from " + name}
		return true, nil
	}

	var out strings.Builder
	target, ok, err := resolveDispatchTarget(a, "step-model", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if a.Config.Llamafile.Active != "step-model" {
		t.Fatalf("expected switch to step-model, got %q", a.Config.Llamafile.Active)
	}

	target.Restore()

	if a.Config.Llamafile.Active != "original-model" {
		t.Errorf("Restore did not return to the original model: got %q, want %q",
			a.Config.Llamafile.Active, "original-model")
	}
	if len(switchedTo) != 2 || switchedTo[1] != "original-model" {
		t.Errorf("expected Restore to switch back via the pre-step name %q, got calls: %v",
			"original-model", switchedTo)
	}
}

func TestResolveDispatchTarget_UnknownName_ReturnsNotFound(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "orig"}
	a.attemptModelSwitchOverride = func(name string, out io.Writer) (bool, error) {
		return false, nil // simulate "not found in any registry"
	}

	var out strings.Builder
	_, ok, err := resolveDispatchTarget(a, "nonexistent", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for an unknown name")
	}
}
