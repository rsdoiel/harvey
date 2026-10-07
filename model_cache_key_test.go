package harvey

import (
	"io"
	"testing"
)

// Capability rows and learned prompt limits are keyed by engine and model, not
// by model name alone (hailo brief, section 7; DR-0030). The same name on two
// engines is two models: different weights, speed and limits.

// engineStub is a ManagedBackend that only knows its engine name.
type engineStub struct {
	ManagedBackend
	engine string
}

func (e engineStub) Name() string { return e.engine }

func TestPromptLimit_LearnedOnOneEngineIsNotAppliedToAnother(t *testing.T) {
	a, _ := newTestAgentWithCache(t, "llama3.2:3b")

	a.Backend = engineStub{engine: "hailo"}
	a.learnPromptLimit(500, io.Discard)
	if got := a.promptTokenLimit(); got != 500 {
		t.Fatalf("hailo limit = %d, want 500", got)
	}

	a.Backend = engineStub{engine: "ollama"}
	if got := a.promptTokenLimit(); got != 0 {
		t.Errorf("ollama limit for the same name = %d, want 0 (a hailo limit leaked)", got)
	}

	a.Backend = engineStub{engine: "hailo"}
	if got := a.promptTokenLimit(); got != 500 {
		t.Errorf("hailo limit after switching back = %d, want 500", got)
	}
}

func TestModelKey(t *testing.T) {
	for _, c := range []struct{ engine, model, want string }{
		{"", "llama3.2:3b", "llama3.2:3b"},
		{"ollama", "llama3.2:3b", "llama3.2:3b"}, // legacy rows stay valid
		{"hailo", "llama3.2:3b", "hailo/llama3.2:3b"},
		{"llamacpp", "Qwen3", "llamacpp/Qwen3"},
	} {
		if got := modelKey(c.engine, c.model); got != c.want {
			t.Errorf("modelKey(%q, %q) = %q, want %q", c.engine, c.model, got, c.want)
		}
	}
}
