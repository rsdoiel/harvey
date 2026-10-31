package harvey

import "testing"

// DR-0037 item 6: one memory budget, from the active model's window.
func TestMemoryBudget_FromTheModelsWindow(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "")
	a.Config.Memory.BudgetPct = 0.25

	// An explicit window wins.
	a.Config.Ollama.ContextLength = 4000
	if got := a.memoryBudget(); got != 1000 {
		t.Errorf("explicit window: budget = %d, want 1000", got)
	}

	// With none set, the model cache's window is used (the old code fell to 512 here).
	a.Config.Ollama.ContextLength = 0
	a.Client = newOllamaLLMClient("http://127.0.0.1:1", "llama3", 0)
	_ = mc.Set(&ModelCapability{Name: a.modelKey("llama3"), ContextLength: 8000})
	if got := a.memoryBudget(); got != 2000 {
		t.Errorf("cached window: budget = %d, want 2000", got)
	}
}

func TestMemoryBudget_FallsBackWhenTheWindowOrShareIsUnknown(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Ollama.ContextLength = 0
	a.Config.Memory.BudgetPct = 0.25
	if got := a.memoryBudget(); got != defaultMemoryBudget {
		t.Errorf("unknown window: budget = %d, want %d", got, defaultMemoryBudget)
	}
	a.Config.Ollama.ContextLength = 4000
	a.Config.Memory.BudgetPct = 0
	if got := a.memoryBudget(); got != defaultMemoryBudget {
		t.Errorf("no share: budget = %d, want %d", got, defaultMemoryBudget)
	}
}
