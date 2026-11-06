package harvey

import (
	"testing"
	"time"
)

// The card reads 2,048 tokens. Between that and the point where the server
// answers HTTP 500 (13,000 to 15,000 characters) a prompt is silently not read
// (notes/hailo-ollama-api.md, 2026-11-03). A prompt limit learned from a retry
// after an HTTP 500, or set by hand, above the window must not be what the
// budget uses on hailo.

func hailoLimitAgent(t *testing.T, model string, stored int) *Agent {
	t.Helper()
	a, mc := newTestAgentWithCache(t, "")
	a.Backend = engineStub{engine: "hailo"}
	a.Client = hailoClient("http://127.0.0.1:1", model)
	if err := mc.Set(&ModelCapability{Name: "hailo/" + model, MaxPromptTokens: stored, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPromptTokenLimit_HailoNeverAboveTheWindowShare(t *testing.T) {
	a := hailoLimitAgent(t, "qwen2.5-coder:1.5b", 3700)
	if got, max := a.promptTokenLimit(), 1740; got > max || got == 0 {
		t.Errorf("limit = %d, want 1..%d (a stored 3,700 is past the window)", got, max)
	}
	if n, src := a.assembleLimit(); n > 1740 {
		t.Errorf("assembleLimit = %d (%s), past the window share", n, src)
	}
}

func TestPromptTokenLimit_HailoLowerStoredLimitStands(t *testing.T) {
	a := hailoLimitAgent(t, "qwen2.5-coder:1.5b", 500)
	if got := a.promptTokenLimit(); got != 500 {
		t.Errorf("limit = %d, want the stored 500", got)
	}
}

func TestPromptTokenLimit_OllamaStoredLimitUntouched(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "llama3.2:3b")
	a.Backend = engineStub{engine: "ollama"}
	_ = mc.Set(&ModelCapability{Name: "llama3.2:3b", MaxPromptTokens: 3700, ProbeLevel: "fast", ProbedAt: time.Now()})
	if got := a.promptTokenLimit(); got != 3700 {
		t.Errorf("limit = %d, want 3700 on Ollama", got)
	}
}

// qwen3 on hailo is sent tool definitions (about 400 tokens) that the history
// estimate does not count; the ceiling leaves room for them.
func TestPromptTokenLimit_HailoNativeToolsTakeTheirShare(t *testing.T) {
	a := hailoLimitAgent(t, "qwen3:1.7b", 3700)
	reg := NewToolRegistry()
	for _, n := range []string{"read_file", "list_files", "search_files", "run_command"} {
		reg.RegisterTool(n, "does "+n+" and says so at some length so the definition is not tiny", map[string]any{"type": "object"}, nil)
	}
	a.Tools = reg
	a.Config.ToolsEnabled = true
	plain := hailoLimitAgent(t, "qwen2.5-coder:1.5b", 3700).promptTokenLimit()
	if got := a.promptTokenLimit(); got >= plain {
		t.Errorf("limit with native tools = %d, want less than %d without them", got, plain)
	}
}
