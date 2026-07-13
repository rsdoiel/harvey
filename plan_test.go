package harvey

import (
	"io"
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

// ─── model annotation parsing ────────────────────────────────────────────────

func TestExtractStepModel_present(t *testing.T) {
	title, model := extractStepModel("Step 3 [model: phi-mini]: compress the output")
	if model != "phi-mini" {
		t.Errorf("model: got %q want %q", model, "phi-mini")
	}
	if title != "Step 3: compress the output" {
		t.Errorf("title: got %q want %q", title, "Step 3: compress the output")
	}
}

func TestExtractStepModel_absent(t *testing.T) {
	title, model := extractStepModel("Write the README file")
	if model != "" {
		t.Errorf("expected empty model, got %q", model)
	}
	if title != "Write the README file" {
		t.Errorf("title should be unchanged, got %q", title)
	}
}

func TestParsePlan_modelAnnotation(t *testing.T) {
	content := `# Plan: test
- [ ] Step 1 [model: phi-mini]: do something small
- [ ] Step 2: do something normally
`
	p, err := parsePlan(content)
	if err != nil {
		t.Fatal(err)
	}
	if p.Steps[0].Model != "phi-mini" {
		t.Errorf("step 0 model: got %q want %q", p.Steps[0].Model, "phi-mini")
	}
	if p.Steps[1].Model != "" {
		t.Errorf("step 1 model should be empty, got %q", p.Steps[1].Model)
	}
}

// ─── plan executor model switch ───────────────────────────────────────────────

// TestCmdPlanNext_modelAnnotationSwitches is the regression test for Bug 2
// (subagent-dispatch-design.md): the pre-fix restore read
// a.Config.Llamafile.Active AFTER the step's switch had already overwritten
// it to the step's own model, so it never actually returned to the original.
// Using attemptModelSwitchOverride lets the switch genuinely succeed in-test
// (no real llamafile process needed) so this can assert on the *result*,
// not just "didn't panic."
func TestCmdPlanNext_modelAnnotationSwitches(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)
	a.Config.Llamafile.Active = "original-model"
	a.Client = &mockLLMClient{reply: "done"}

	var switchedTo []string
	a.attemptModelSwitchOverride = func(name string, out io.Writer) (bool, error) {
		switchedTo = append(switchedTo, name)
		// Simulate switchLlamafileModel's real mutation (llamafile.go:220).
		a.Config.Llamafile.Active = name
		a.Client = &mockLLMClient{reply: "reply from " + name}
		return true, nil
	}

	// Save a plan with a model-annotated step. The [model: NAME] marker must
	// be embedded in Title itself, not just set on the Model field directly:
	// formatPlan (called by SavePlan) only ever writes s.Title verbatim and
	// never re-emits Model separately, so a Model set without the marker in
	// Title is silently dropped on the SavePlan → LoadPlan round trip
	// cmdPlanNext performs — confirmed by first writing this test the naive
	// way and finding it never actually exercised the switch path at all.
	p := &Plan{
		Goal: "test",
		Steps: []PlanStep{
			{Index: 0, Done: false, Title: "do something small [model: phi-mini]"},
		},
	}
	if err := SavePlan(ws, p); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	if err := cmdPlanNext(a, &buf); err != nil {
		t.Fatalf("cmdPlanNext: %v", err)
	}

	if len(switchedTo) != 2 {
		t.Fatalf("expected 2 switch calls (to phi-mini, then back), got %v", switchedTo)
	}
	if switchedTo[0] != "phi-mini" {
		t.Errorf("first switch: got %q, want %q", switchedTo[0], "phi-mini")
	}
	if switchedTo[1] != "original-model" {
		t.Errorf("restore switch: got %q, want %q (the pre-step model, not the step's own)",
			switchedTo[1], "original-model")
	}
	if a.Config.Llamafile.Active != "original-model" {
		t.Errorf("Llamafile.Active after cmdPlanNext = %q, want %q (restored)",
			a.Config.Llamafile.Active, "original-model")
	}
}

func TestExtractStepModelRoundTrip(t *testing.T) {
	// Verify that parsePlan + extractStepModel produces the expected Model field.
	content := "# Plan: test\n- [ ] Step 1 [model: phi-mini]: do the task\n"
	p, err := parsePlan(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(p.Steps))
	}
	if p.Steps[0].Model != "phi-mini" {
		t.Errorf("Model: got %q want %q", p.Steps[0].Model, "phi-mini")
	}
	if p.Steps[0].Title != "Step 1: do the task" {
		t.Errorf("Title: got %q want %q", p.Steps[0].Title, "Step 1: do the task")
	}
}
