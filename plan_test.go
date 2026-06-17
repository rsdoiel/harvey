package harvey

import (
	"strings"
	"testing"
	"time"
)

func TestPlanFromLLMResponse_checklist(t *testing.T) {
	response := `# Plan: Create a web component demo

- [ ] Create demo/ directory
- [ ] Write demo/styles.css
- [ ] Write demo/app.js
- [ ] Write demo/index.html`

	p, err := PlanFromLLMResponse(response, "fallback goal")
	if err != nil {
		t.Fatal(err)
	}
	if p.Goal != "Create a web component demo" {
		t.Errorf("unexpected goal: %q", p.Goal)
	}
	if len(p.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(p.Steps))
	}
	if p.Steps[0].Done {
		t.Error("step 0 should be unchecked")
	}
	if p.Steps[0].Title != "Create demo/ directory" {
		t.Errorf("unexpected step 0 title: %q", p.Steps[0].Title)
	}
}

func TestPlanFromLLMResponse_withChecked(t *testing.T) {
	response := `- [x] Create demo/ directory
- [ ] Write demo/styles.css`

	p, _ := PlanFromLLMResponse(response, "goal")
	if len(p.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(p.Steps))
	}
	if !p.Steps[0].Done {
		t.Error("step 0 should be checked")
	}
	if p.Steps[1].Done {
		t.Error("step 1 should be unchecked")
	}
}

func TestPlanFromLLMResponse_numberedFallback(t *testing.T) {
	response := `Here is your plan:
1. Create the directory
2. Write the CSS file
3. Write the HTML file`

	p, _ := PlanFromLLMResponse(response, "goal")
	if len(p.Steps) != 3 {
		t.Fatalf("expected 3 steps from numbered list, got %d", len(p.Steps))
	}
	if p.Steps[0].Title != "Create the directory" {
		t.Errorf("unexpected title: %q", p.Steps[0].Title)
	}
}

func TestPlanFromLLMResponse_fallbackGoal(t *testing.T) {
	response := "- [ ] Do something"
	p, _ := PlanFromLLMResponse(response, "my task")
	if p.Goal != "my task" {
		t.Errorf("expected fallback goal, got %q", p.Goal)
	}
}

func TestPlanNextStep(t *testing.T) {
	p := &Plan{
		Steps: []PlanStep{
			{Index: 0, Done: true, Title: "done step"},
			{Index: 1, Done: false, Title: "next step"},
			{Index: 2, Done: false, Title: "later step"},
		},
	}
	s := p.NextStep()
	if s == nil {
		t.Fatal("expected a next step")
	}
	if s.Title != "next step" {
		t.Errorf("unexpected next step: %q", s.Title)
	}
}

func TestPlanAllDone(t *testing.T) {
	p := &Plan{
		Steps: []PlanStep{
			{Done: true},
			{Done: true},
		},
	}
	if !p.AllDone() {
		t.Error("expected AllDone to be true")
	}
	p.Steps[0].Done = false
	if p.AllDone() {
		t.Error("expected AllDone to be false")
	}
}

func TestPlanMarkDone(t *testing.T) {
	p := &Plan{
		Steps: []PlanStep{
			{Index: 0, Done: false, Title: "step"},
		},
	}
	p.MarkDone(0)
	if !p.Steps[0].Done {
		t.Error("step should be done after MarkDone")
	}
	if p.Updated.IsZero() {
		t.Error("Updated should be set after MarkDone")
	}
}

func TestPlanSummary(t *testing.T) {
	p := &Plan{
		Goal: "Build demo",
		Steps: []PlanStep{
			{Done: true, Title: "step 1"},
			{Done: false, Title: "step 2"},
			{Done: false, Title: "step 3"},
		},
	}
	s := p.Summary()
	if !strings.Contains(s, "1/3") {
		t.Errorf("summary missing progress: %q", s)
	}
	if !strings.Contains(s, "step 2") {
		t.Errorf("summary missing next step: %q", s)
	}
}

func TestFormatAndParsePlan(t *testing.T) {
	original := &Plan{
		Goal:    "Test plan",
		Created: time.Now().UTC().Truncate(time.Second),
		Updated: time.Now().UTC().Truncate(time.Second),
		Steps: []PlanStep{
			{Index: 0, Done: true, Title: "completed step"},
			{Index: 1, Done: false, Title: "pending step"},
		},
	}

	formatted := formatPlan(original)
	parsed, err := parsePlan(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Goal != original.Goal {
		t.Errorf("goal mismatch: got %q, want %q", parsed.Goal, original.Goal)
	}
	if len(parsed.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(parsed.Steps))
	}
	if !parsed.Steps[0].Done {
		t.Error("step 0 should be done after round-trip")
	}
	if parsed.Steps[1].Done {
		t.Error("step 1 should not be done after round-trip")
	}
}
