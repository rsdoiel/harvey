// Package harvey — plan_cmd.go implements the /plan slash command family
// for generating and executing bounded-context task plans.
package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const planHelpText = `Usage: /plan <TASK | next | status | show | clear>

  /plan TASK     Generate a step-by-step plan for TASK and save it to agents/plan.md.
  /plan next     Execute the next uncompleted step using a fresh bounded context.
  /plan status   Show the plan checklist with completion markers.
  /plan show     Print the raw agents/plan.md file.
  /plan clear    Delete the current plan.

Each '/plan next' call sends only the system prompt + current plan state to the
model — not the full conversation history. This keeps turn times constant
regardless of how many steps have been completed.
`

/** cmdPlan dispatches /plan subcommands: TASK, next, status, show, clear.
 *
 * Parameters:
 *   a    (*Agent)    — the running Harvey agent.
 *   args ([]string)  — subcommand and its arguments.
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — non-nil on unexpected failures.
 *
 * Example:
 *   cmdPlan(agent, []string{"next"}, os.Stdout)
 */
func cmdPlan(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, planHelpText)
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "next":
		return cmdPlanNext(a, out)
	case "status":
		return cmdPlanStatus(a, out)
	case "show":
		return cmdPlanShow(a, out)
	case "clear":
		return cmdPlanClear(a, out)
	default:
		// Treat everything else as a task description.
		return cmdPlanCreate(a, strings.Join(args, " "), out)
	}
}

// cmdPlanCreate sends the task to the model with a planning prompt, parses the
// checklist from the response, and saves it to agents/plan.md.
func cmdPlanCreate(a *Agent, task string, out io.Writer) error {
	if a.Client == nil {
		return fmt.Errorf("no backend connected — use /ollama start or /llamafile add first")
	}

	planningPrompt := "You are planning a multi-step task. " +
		"Output ONLY a markdown checklist of concrete steps using GFM format (- [ ] Step). " +
		"Each step must be a single, testable action: create a file, write content, run a command. " +
		"No prose before or after the checklist. Begin with '# Plan: <short goal>'.\n\n" +
		"Task: " + task

	msgs := []Message{
		{Role: "user", Content: planningPrompt},
	}
	if a.Config.SystemPrompt != "" {
		msgs = []Message{
			{Role: "system", Content: a.Config.SystemPrompt},
			{Role: "user", Content: planningPrompt},
		}
	}

	fmt.Fprintln(out, "  Generating plan...")
	var buf strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := a.Client.Chat(ctx, msgs, &buf); err != nil {
		return fmt.Errorf("plan generation failed: %w", err)
	}

	p, err := PlanFromLLMResponse(buf.String(), task)
	if err != nil {
		return fmt.Errorf("could not parse plan from response: %w", err)
	}
	if len(p.Steps) == 0 {
		fmt.Fprintln(out, yellow("  ⚠")+" Model did not produce a checklist. Raw response:")
		fmt.Fprintln(out, buf.String())
		return nil
	}

	if err := SavePlan(a.Workspace, p); err != nil {
		return fmt.Errorf("could not save plan: %w", err)
	}

	fmt.Fprintln(out, green("  ✓")+" Plan saved to agents/plan.md\n")
	PrintPlan(p, out)
	fmt.Fprintln(out, dim("\n  Run /plan next to execute the first step."))
	return nil
}

// cmdPlanNext loads the current plan, executes the next uncompleted step with
// a fresh bounded context, marks it done, and saves the updated plan.
func cmdPlanNext(a *Agent, out io.Writer) error {
	if a.Client == nil {
		return fmt.Errorf("no backend connected — use /ollama start or /llamafile add first")
	}
	if a.Workspace == nil {
		return fmt.Errorf("no workspace available")
	}

	p, err := LoadPlan(a.Workspace)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no plan found — use /plan TASK to create one")
		}
		return fmt.Errorf("could not load plan: %w", err)
	}

	step := p.NextStep()
	if step == nil {
		fmt.Fprintln(out, green("  ✓")+" All steps complete.")
		PrintPlan(p, out)
		return nil
	}

	fmt.Fprintf(out, "  Executing step %d/%d: %s\n", step.Index+1, len(p.Steps), step.Title)

	// Build a fresh bounded context — system prompt + plan state + step instruction.
	// Prior conversation history is intentionally excluded to keep context O(plan_size).
	planContent := formatPlan(p)
	stepPrompt := fmt.Sprintf(
		"Current plan:\n%s\n\nExecute ONLY this step: %s\n\n"+
			"Call the appropriate tool immediately. Do not describe what you will do. "+
			"Do not proceed to subsequent steps.",
		planContent, step.Title,
	)
	freshHistory := []Message{}
	if a.Config.SystemPrompt != "" {
		freshHistory = append(freshHistory, Message{Role: "system", Content: a.Config.SystemPrompt})
	}
	freshHistory = append(freshHistory, Message{Role: "user", Content: stepPrompt})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf strings.Builder
	toolErrors := false
	if a.Tools != nil && a.Config.ToolsEnabled {
		ex := NewToolExecutor(a.Tools, a.Client, a.Config)
		ex.DebugLog = a.DebugLog
		updatedHistory, _, err := ex.RunToolLoop(ctx, freshHistory, &buf)
		if err != nil {
			return fmt.Errorf("step execution failed: %w", err)
		}
		toolErrors = planStepHadErrors(updatedHistory)
	} else {
		if _, err := a.Client.Chat(ctx, freshHistory, &buf); err != nil {
			return fmt.Errorf("step execution failed: %w", err)
		}
	}

	if txt := strings.TrimSpace(buf.String()); txt != "" {
		fmt.Fprintln(out, txt)
	}

	if toolErrors {
		fmt.Fprintln(out, yellow("  ⚠")+" One or more tool calls failed — step not marked done.")
		fmt.Fprintln(out, dim("  Fix the issue and run /plan next to retry, or edit agents/plan.md to skip."))
		return nil
	}

	p.MarkDone(step.Index)
	if err := SavePlan(a.Workspace, p); err != nil {
		fmt.Fprintf(out, yellow("  ⚠ Could not save plan: %v\n"), err)
	}

	if p.AllDone() {
		fmt.Fprintln(out, green("\n  ✓ All steps complete!"))
	} else {
		next := p.NextStep()
		fmt.Fprintln(out, dim("  Next: "+next.Title+"  (run /plan next to continue)"))
	}
	return nil
}

// planStepHadErrors reports whether any uncompacted tool messages in history
// contain an error string — indicating the step did not complete cleanly.
// Compacted messages ([done]) are skipped since prior errors were handled.
func planStepHadErrors(history []Message) bool {
	for _, m := range history {
		if m.Role != "tool" || m.Content == "[done]" {
			continue
		}
		c := m.Content
		if strings.HasPrefix(c, "error:") ||
			strings.Contains(c, "allowlist") ||
			strings.Contains(c, "permission denied") ||
			strings.Contains(c, "no such file") {
			return true
		}
	}
	return false
}

// cmdPlanStatus prints the current plan checklist with completion markers.
func cmdPlanStatus(a *Agent, out io.Writer) error {
	if a.Workspace == nil {
		return fmt.Errorf("no workspace available")
	}
	p, err := LoadPlan(a.Workspace)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(out, "  No plan found. Use /plan TASK to create one.")
			return nil
		}
		return fmt.Errorf("could not load plan: %w", err)
	}
	PrintPlan(p, out)
	return nil
}

// cmdPlanShow prints the raw agents/plan.md file.
func cmdPlanShow(a *Agent, out io.Writer) error {
	if a.Workspace == nil {
		return fmt.Errorf("no workspace available")
	}
	absPath, err := a.Workspace.AbsPath(filepath.Join(harveySubdir, planFileName))
	if err != nil {
		return err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(out, "  No plan found. Use /plan TASK to create one.")
			return nil
		}
		return err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fmt.Fprintf(out, "  %s\n", scanner.Text())
	}
	return nil
}

// cmdPlanClear deletes agents/plan.md after confirmation.
func cmdPlanClear(a *Agent, out io.Writer) error {
	if a.Workspace == nil {
		return fmt.Errorf("no workspace available")
	}
	absPath, err := a.Workspace.AbsPath(filepath.Join(harveySubdir, planFileName))
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		fmt.Fprintln(out, "  No plan to clear.")
		return nil
	}
	if err := os.Remove(absPath); err != nil {
		return fmt.Errorf("could not remove plan: %w", err)
	}
	fmt.Fprintln(out, "  Plan cleared.")
	return nil
}
