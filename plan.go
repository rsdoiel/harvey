// Package harvey — plan.go implements bounded-context plan-execute support.
// Plans are stored as GFM checklists in agents/plan.md so they survive
// /clear and session restarts. Each step is executed with a fresh context
// containing only the system prompt + current plan state, keeping turn times
// flat regardless of how many steps have been completed.
package harvey

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const planFileName = "plan.md"

/** PlanStep represents a single actionable step within a Plan.
 *
 * Fields:
 *   Index (int)    — zero-based position in the plan.
 *   Done  (bool)   — true when the step has been executed successfully.
 *   Title (string) — one-line description of the action to perform.
 *
 * Example:
 *   step := PlanStep{Index: 0, Done: false, Title: "Create demo/ directory"}
 */
type PlanStep struct {
	Index int
	Done  bool
	Title string
}

/** Plan represents a multi-step task decomposed into ordered, checkable steps.
 *
 * Fields:
 *   Goal    (string)    — short description of the overall objective.
 *   Steps   ([]PlanStep) — ordered list of steps.
 *   Created (time.Time) — when the plan was first generated.
 *   Updated (time.Time) — when the plan was last modified.
 *
 * Example:
 *   p := &Plan{Goal: "Create a web component demo", Steps: []PlanStep{...}}
 */
type Plan struct {
	Goal    string
	Steps   []PlanStep
	Created time.Time
	Updated time.Time
}

/** NextStep returns the first uncompleted step, or nil if all are done.
 *
 * Returns:
 *   *PlanStep — pointer to the first step where Done is false; nil when all done.
 *
 * Example:
 *   if s := p.NextStep(); s != nil { fmt.Println("next:", s.Title) }
 */
func (p *Plan) NextStep() *PlanStep {
	for i := range p.Steps {
		if !p.Steps[i].Done {
			return &p.Steps[i]
		}
	}
	return nil
}

/** MarkDone marks step at index i as complete and updates the Updated timestamp.
 *
 * Parameters:
 *   i (int) — zero-based step index to mark done.
 *
 * Example:
 *   p.MarkDone(0)
 */
func (p *Plan) MarkDone(i int) {
	if i >= 0 && i < len(p.Steps) {
		p.Steps[i].Done = true
		p.Updated = time.Now().UTC()
	}
}

/** Summary returns a compact one-line description of plan state suitable for
 * injecting into a bounded LLM context.
 *
 * Returns:
 *   string — e.g. "Plan: Create demo [2/5 done] — next: Write styles.css"
 *
 * Example:
 *   fmt.Println(p.Summary())
 */
func (p *Plan) Summary() string {
	done := 0
	for _, s := range p.Steps {
		if s.Done {
			done++
		}
	}
	next := p.NextStep()
	nextTitle := "(all done)"
	if next != nil {
		nextTitle = next.Title
	}
	return fmt.Sprintf("Plan: %s [%d/%d done] — next: %s",
		p.Goal, done, len(p.Steps), nextTitle)
}

/** AllDone reports whether every step in the plan is marked complete.
 *
 * Returns:
 *   bool — true when all steps are done.
 *
 * Example:
 *   if p.AllDone() { fmt.Println("Task complete") }
 */
func (p *Plan) AllDone() bool {
	return p.NextStep() == nil
}

/** LoadPlan reads agents/plan.md from the workspace and returns the parsed Plan.
 * Returns an error when the file does not exist or cannot be parsed.
 *
 * Parameters:
 *   ws (*Workspace) — workspace whose agents/ directory is read.
 *
 * Returns:
 *   *Plan — parsed plan.
 *   error — if file is missing or unreadable.
 *
 * Example:
 *   p, err := LoadPlan(agent.Workspace)
 */
func LoadPlan(ws *Workspace) (*Plan, error) {
	absPath, err := ws.AbsPath(filepath.Join(harveySubdir, planFileName))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	return parsePlan(string(data))
}

/** SavePlan writes the plan to agents/plan.md as a GFM checklist.
 *
 * Parameters:
 *   ws (*Workspace) — workspace whose agents/ directory is written.
 *   p  (*Plan)      — plan to persist.
 *
 * Returns:
 *   error — on path resolution or file write failure.
 *
 * Example:
 *   err := SavePlan(agent.Workspace, p)
 */
func SavePlan(ws *Workspace, p *Plan) error {
	absPath, err := ws.AbsPath(filepath.Join(harveySubdir, planFileName))
	if err != nil {
		return err
	}
	return os.WriteFile(absPath, []byte(formatPlan(p)), 0o644)
}

/** PlanFromLLMResponse parses a Plan from raw LLM output. It looks for a
 * GFM checklist (lines beginning with "- [ ]" or "- [x]") and an optional
 * goal heading ("# Plan: ..." or "## Goal: ..."). Falls back to a numbered
 * list ("1. Step") when no checklist is found. goal is used as a fallback
 * when the response contains no heading.
 *
 * Parameters:
 *   text (string) — raw model response.
 *   goal (string) — fallback goal text when none is found in the response.
 *
 * Returns:
 *   *Plan — parsed plan; may have zero steps if none could be extracted.
 *   error — always nil; included for future validation.
 *
 * Example:
 *   p, _ := PlanFromLLMResponse(response, "build the web component demo")
 */
func PlanFromLLMResponse(text, goal string) (*Plan, error) {
	p := &Plan{
		Goal:    goal,
		Created: time.Now().UTC(),
		Updated: time.Now().UTC(),
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	idx := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Extract goal from heading lines.
		if strings.HasPrefix(line, "# Plan:") {
			p.Goal = strings.TrimSpace(strings.TrimPrefix(line, "# Plan:"))
			continue
		}
		if strings.HasPrefix(line, "## Goal:") {
			p.Goal = strings.TrimSpace(strings.TrimPrefix(line, "## Goal:"))
			continue
		}

		// GFM unchecked: "- [ ] Title"
		if strings.HasPrefix(line, "- [ ] ") {
			p.Steps = append(p.Steps, PlanStep{
				Index: idx,
				Done:  false,
				Title: strings.TrimPrefix(line, "- [ ] "),
			})
			idx++
			continue
		}
		// GFM checked: "- [x] Title" or "- [X] Title"
		if strings.HasPrefix(line, "- [x] ") || strings.HasPrefix(line, "- [X] ") {
			title := line[6:]
			p.Steps = append(p.Steps, PlanStep{Index: idx, Done: true, Title: title})
			idx++
			continue
		}
	}

	// Fallback: numbered list "1. Step" if no checklist found.
	if len(p.Steps) == 0 {
		scanner2 := bufio.NewScanner(strings.NewReader(text))
		for scanner2.Scan() {
			line := strings.TrimSpace(scanner2.Text())
			if len(line) < 3 {
				continue
			}
			// Match "1." "2." etc.
			if line[0] >= '1' && line[0] <= '9' && line[1] == '.' && line[2] == ' ' {
				p.Steps = append(p.Steps, PlanStep{
					Index: idx,
					Done:  false,
					Title: strings.TrimSpace(line[3:]),
				})
				idx++
			}
		}
	}

	return p, nil
}

// formatPlan renders a Plan as a GFM checklist suitable for agents/plan.md.
func formatPlan(p *Plan) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Plan: %s\n\n", p.Goal)
	if !p.Created.IsZero() {
		fmt.Fprintf(&sb, "<!-- created: %s -->\n", p.Created.Format(time.RFC3339))
	}
	if !p.Updated.IsZero() {
		fmt.Fprintf(&sb, "<!-- updated: %s -->\n", p.Updated.Format(time.RFC3339))
	}
	sb.WriteString("\n")
	for _, s := range p.Steps {
		check := " "
		if s.Done {
			check = "x"
		}
		fmt.Fprintf(&sb, "- [%s] %s\n", check, s.Title)
	}
	return sb.String()
}

// parsePlan reads a GFM checklist from plan file content.
func parsePlan(content string) (*Plan, error) {
	p := &Plan{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	idx := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# Plan:") {
			p.Goal = strings.TrimSpace(strings.TrimPrefix(line, "# Plan:"))
			continue
		}
		if strings.HasPrefix(line, "<!-- created:") {
			ts := strings.TrimSuffix(strings.TrimPrefix(line, "<!-- created: "), " -->")
			p.Created, _ = time.Parse(time.RFC3339, ts)
			continue
		}
		if strings.HasPrefix(line, "<!-- updated:") {
			ts := strings.TrimSuffix(strings.TrimPrefix(line, "<!-- updated: "), " -->")
			p.Updated, _ = time.Parse(time.RFC3339, ts)
			continue
		}
		if strings.HasPrefix(line, "- [ ] ") {
			p.Steps = append(p.Steps, PlanStep{Index: idx, Done: false, Title: line[6:]})
			idx++
		} else if strings.HasPrefix(line, "- [x] ") || strings.HasPrefix(line, "- [X] ") {
			p.Steps = append(p.Steps, PlanStep{Index: idx, Done: true, Title: line[6:]})
			idx++
		}
	}
	if p.Goal == "" && len(p.Steps) == 0 {
		return nil, fmt.Errorf("no plan found in file")
	}
	return p, nil
}

/** PrintPlan writes a human-readable plan checklist to out.
 *
 * Parameters:
 *   p   (*Plan)    — plan to display.
 *   out (io.Writer) — destination for output.
 *
 * Example:
 *   PrintPlan(p, os.Stdout)
 */
func PrintPlan(p *Plan, out io.Writer) {
	done := 0
	for _, s := range p.Steps {
		if s.Done {
			done++
		}
	}
	fmt.Fprintf(out, "  Goal: %s\n", p.Goal)
	fmt.Fprintf(out, "  Progress: %d/%d steps done\n\n", done, len(p.Steps))
	for _, s := range p.Steps {
		mark := dim("○")
		if s.Done {
			mark = green("✓")
		}
		fmt.Fprintf(out, "  %s %s\n", mark, s.Title)
	}
}
