package harvey

import (
	"context"
	"strings"
	"testing"
)

// Layer 2 of the prompt budget: the system prompt is built from layers (the
// agent preamble, HARVEY.md, the skills catalog) and shrinks to fit a model's
// prompt limit, least essential layer first; the history is trimmed to fit
// each turn. Both say so. See prompt_limit_test.go for layer 1.

const (
	fitProject = "# HARVEY.md\n\nFirst paragraph of project rules.\n\nSecond paragraph of project rules.\n\n"
	fitCatalog = "<available_skills>\n<skill><name>a</name></skill>\n</available_skills>\n"
)

// fitFull builds the system prompt the way Run does: preamble, HARVEY.md,
// then the catalog appended by loadSkills.
func fitFull(projectRepeat, catalogRepeat int) (full, catalog string) {
	catalog = strings.Repeat(fitCatalog, catalogRepeat)
	return agentPreamble + strings.Repeat(fitProject, projectRepeat) + "\n\n" + catalog, catalog
}

func TestFitSystemPrompt_FitsUnchanged(t *testing.T) {
	full, cat := fitFull(1, 1)
	got, notes := fitSystemPrompt(full, cat, 100000)
	if got != full || len(notes) != 0 {
		t.Errorf("changed a prompt that fits: notes %v", notes)
	}
	if got, _ := fitSystemPrompt(full, cat, 0); got != full {
		t.Error("budget 0 (unknown) must leave the prompt alone")
	}
}

func TestFitSystemPrompt_DropsCatalogFirst(t *testing.T) {
	full, cat := fitFull(2, 20)
	base := strings.TrimSuffix(full, "\n\n"+cat)
	budget := estimateTokens(base) + 10
	got, notes := fitSystemPrompt(full, cat, budget)
	if got != base {
		t.Errorf("want preamble+project only (%d chars), got %d chars", len(base), len(got))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "skills catalog") {
		t.Errorf("notes = %v", notes)
	}
}

func TestFitSystemPrompt_TruncatesProjectNextAndKeepsPreamble(t *testing.T) {
	full, cat := fitFull(30, 20)
	budget := estimateTokens(agentPreamble) + 80
	got, notes := fitSystemPrompt(full, cat, budget)
	if !strings.HasPrefix(got, agentPreamble) {
		t.Error("the preamble must stay whole while the project layer can still be cut")
	}
	if estimateTokens(got) > budget {
		t.Errorf("%d tokens, over the budget of %d", estimateTokens(got), budget)
	}
	if !strings.Contains(got, "truncated") || !strings.Contains(strings.Join(notes, " "), "HARVEY.md") {
		t.Errorf("truncation not marked: notes %v", notes)
	}
}

func TestFitSystemPrompt_TinyBudgetFallsBackToCompactPreamble(t *testing.T) {
	full, cat := fitFull(30, 20)
	budget := estimateTokens(agentPreamble) / 2
	got, notes := fitSystemPrompt(full, cat, budget)
	if estimateTokens(got) > budget {
		t.Errorf("%d tokens, over the budget of %d", estimateTokens(got), budget)
	}
	for _, want := range []string{"Harvey", "/run"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact preamble lost %q: %q", want, got)
		}
	}
	if !strings.Contains(strings.Join(notes, " "), "preamble") {
		t.Errorf("notes %v", notes)
	}
}

// layeredAgent has a system prompt built in layers and a model with a limit.
func layeredAgent(t *testing.T, limit int) (*Agent, string) {
	t.Helper()
	a := promptLimitAgent(t, limit)
	full, cat := fitFull(30, 20)
	a.Config.SystemPrompt = full
	a.catalogBlock = cat
	a.History = []Message{{Role: "system", Content: full}}
	return a, full
}

func TestRefreshSystemPrompt_ShrinksToTheLimitAndRestores(t *testing.T) {
	a, full := layeredAgent(t, 700)
	var out strings.Builder
	a.refreshSystemPrompt(&out)
	if got := estimateTokens(a.History[0].Content); got > 700*6/10 {
		t.Errorf("system prompt is %d tokens, over its share of the limit", got)
	}
	if !strings.Contains(out.String(), "llama3.2:3b") {
		t.Errorf("no notice naming the model: %q", out.String())
	}
	out.Reset()
	a.refreshSystemPrompt(&out)
	if out.Len() != 0 {
		t.Errorf("the notice repeated: %q", out.String())
	}
	// Switching to a model with no limit puts the full prompt back.
	mc := a.ModelCache
	_ = mc.Set(&ModelCapability{Name: "llama3.2:3b", MaxPromptTokens: 0, ProbeLevel: "fast"})
	a.refreshSystemPrompt(&out)
	if a.History[0].Content != full {
		t.Error("the full system prompt was not restored when the limit went away")
	}
}

func TestRefreshSystemPrompt_NoConfigPromptIsLeftAlone(t *testing.T) {
	a := promptLimitAgent(t, 700)
	a.History = []Message{{Role: "system", Content: strings.Repeat("x", 5000)}}
	a.refreshSystemPrompt(&strings.Builder{})
	if len(a.History[0].Content) != 5000 {
		t.Error("changed a system message that Config.SystemPrompt does not own")
	}
}

func TestClearHistory_AppliesThePromptBudget(t *testing.T) {
	a, _ := layeredAgent(t, 700)
	a.ClearHistory()
	if got := estimateTokens(a.History[0].Content); got > 700*6/10 {
		t.Errorf("after /clear the system prompt is %d tokens, over its share", got)
	}
}

func turn(user, reply string) []Message {
	return []Message{{Role: "user", Content: user}, {Role: "assistant", Content: reply}}
}

func TestTrimHistoryToBudget(t *testing.T) {
	a := promptLimitAgent(t, 700)
	a.History = []Message{{Role: "system", Content: strings.Repeat("s", 400)}}
	for i := 0; i < 6; i++ {
		a.History = append(a.History, turn(strings.Repeat("q", 300)+string(rune('A'+i)), strings.Repeat("a", 300))...)
	}
	var out strings.Builder
	a.trimHistoryToBudget(&out)
	if n := estimateTokens(HistoryText(a.History)); n >= 700 {
		t.Errorf("history is still %d tokens", n)
	}
	if a.History[0].Role != "system" {
		t.Error("the system message was dropped")
	}
	last := a.History[len(a.History)-2]
	if last.Role != "user" || !strings.HasSuffix(last.Content, "F") {
		t.Errorf("the latest turn was not kept: %q", last.Content[len(last.Content)-3:])
	}
	if a.History[1].Content[len(a.History[1].Content)-1] == 'A' {
		t.Error("the oldest turn survived")
	}
	if !strings.Contains(out.String(), "Trimmed") {
		t.Errorf("no notice: %q", out.String())
	}
}

func TestTrimHistoryToBudget_KeepsToolMessagesWithTheirTurn(t *testing.T) {
	a := promptLimitAgent(t, 700)
	a.History = []Message{{Role: "system", Content: "s"}}
	a.History = append(a.History,
		Message{Role: "user", Content: strings.Repeat("q", 1200) + "old"},
		Message{Role: "assistant", Content: "calling"},
		Message{Role: "tool", Content: strings.Repeat("r", 2400)},
		Message{Role: "assistant", Content: "done"},
		Message{Role: "user", Content: "new question"},
	)
	a.trimHistoryToBudget(&strings.Builder{})
	for _, m := range a.History {
		if m.Role == "tool" {
			t.Error("a tool result outlived the turn that asked for it")
		}
	}
	if got := a.History[len(a.History)-1].Content; got != "new question" {
		t.Errorf("last message = %q", got)
	}
}

func TestTrimHistoryToBudget_NoLimitNoChange(t *testing.T) {
	a := promptLimitAgent(t, 0)
	a.History = []Message{{Role: "system", Content: strings.Repeat("s", 100000)}}
	a.History = append(a.History, turn("q", "a")...)
	a.trimHistoryToBudget(&strings.Builder{})
	if len(a.History) != 3 {
		t.Errorf("history has %d messages", len(a.History))
	}
}

// Through the real turn: earlier turns are dropped, the request is still sent.
func TestRunChatTurn_TrimsOldTurnsAndSends(t *testing.T) {
	srv := fakeOllamaChat(t)
	a := promptLimitAgent(t, 700)
	a.Client = newOllamaLLMClient(srv.URL, "llama3.2:3b", 0)
	a.History = []Message{{Role: "system", Content: strings.Repeat("s", 600)}}
	for i := 0; i < 4; i++ {
		a.History = append(a.History, turn(strings.Repeat("q", 400), strings.Repeat("a", 400))...)
	}
	var out strings.Builder
	_, _, err := a.runChatTurn(context.Background(), "next question", &out, nil, false, "")
	if err != nil {
		t.Fatalf("turn failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Trimmed") {
		t.Errorf("no trim notice: %q", out.String())
	}
	if n := len(a.History); n >= 1+8+2 {
		t.Errorf("history has %d messages; nothing was trimmed", n)
	}
}
