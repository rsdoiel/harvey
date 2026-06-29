// Package harvey — skill_dispatch.go handles runtime activation and execution
// of skills. When a user's prompt matches a skill's trigger pattern, this file
// manages the complete dispatch lifecycle:
//
//   - Trigger matching (MatchesTrigger) via regex or keyword patterns
//   - Skill lookup in the catalog (SortedSkillNames for deterministic ordering)
//   - Full dispatch workflow (DispatchSkill): check compiled scripts, handle staleness,
//     offer recompilation, and execute or fall back to LLM context loading
//   - Compiled script execution (runCompiledScript) with HARVEY_* environment variables
//
// Skills can be triggered automatically based on their Trigger pattern or invoked
// explicitly via /skill run. The dispatcher prefers compiled scripts when available
// and falls back to loading the skill body into LLM context.

package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// sensitiveSkillEnvVars contains environment variable names that should be
// EXCLUDED from skill execution to prevent sensitive data leakage.
//
// Note: filterSkillEnvironment uses a whitelist approach, so variables not
// matching a safe prefix are already blocked. This list is defence-in-depth.
var sensitiveSkillEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"COHERE_API_KEY",
	"DEEPSEEK_API_KEY",
	"FIREWORKS_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"GROQ_API_KEY",
	"HUGGINGFACE_TOKEN",
	"MISTRAL_API_KEY",
	"OPENAI_API_KEY",
	"PERPLEXITY_API_KEY",
	"PUBLICAI_API_KEY",
	"REPLICATE_API_KEY",
	"TOGETHER_API_KEY",
	"XAI_API_KEY",
}

// safeSkillEnvPrefixes contains environment variable name prefixes that are
// safe to pass to skill processes.
var safeSkillEnvPrefixes = []string{
	"PATH",
	"HOME",
	"USER",
	"USERNAME",
	"SHELL",
	"TERM",
	"LANG",
	"LC_",
	"PWD",
	"TMPDIR",
	"TEMP",
}

/** filterSkillEnvironment returns a filtered copy of the environment for
 * skill script execution. Sensitive variables (API keys) are explicitly
 * excluded, and only safe variables plus HARVEY_* variables are included.
 *
 * Parameters:
 *   env ([]string) — the original environment in "KEY=VALUE" format.
 *
 * Returns:
 *   []string — filtered environment with only safe variables.
 */
func filterSkillEnvironment(env []string) []string {
	sensitiveMap := make(map[string]bool)
	for _, v := range sensitiveSkillEnvVars {
		sensitiveMap[v] = true
	}

	safeMap := make(map[string]bool)
	for _, p := range safeSkillEnvPrefixes {
		safeMap[p] = true
	}

	var result []string
	for _, e := range env {
		idx := strings.IndexByte(e, '=')
		if idx == -1 {
			continue
		}
		varName := e[:idx]

		// Exclude sensitive variables
		if sensitiveMap[varName] {
			continue
		}

		// Include safe variables
		isSafe := false
		for prefix := range safeMap {
			if varName == prefix || strings.HasPrefix(varName, prefix+"_") {
				isSafe = true
				break
			}
		}
		
		if isSafe {
			result = append(result, e)
		}
	}

	// Always ensure PATH is set
	pathFound := false
	for _, e := range result {
		if strings.HasPrefix(e, "PATH=") {
			pathFound = true
			break
		}
	}
	if !pathFound {
		if path := os.Getenv("PATH"); path != "" {
			result = append(result, "PATH="+path)
		}
	}

	return result
}

/** MatchesTrigger reports whether prompt matches the trigger pattern stored in
 * skill.Trigger. Returns false when skill.Trigger is empty.
 *
 * Two trigger modes are supported:
 *   Regexp mode  — trigger value is wrapped in slashes: /pattern/
 *                  Matched case-insensitively via (?i).
 *   Keyword mode — trigger value is a whitespace-separated list of words.
 *                  Any single word present in the lowercased prompt triggers.
 *
 * A malformed regexp returns false rather than panicking.
 *
 * Parameters:
 *   skill  (*SkillMeta) — skill with optional Trigger field.
 *   prompt (string)     — raw user input to match against.
 *
 * Returns:
 *   bool — true when prompt matches the trigger.
 *
 * Example:
 *   skill.Trigger = "pdf extract"
 *   MatchesTrigger(skill, "please extract the pdf") // true
 */
func MatchesTrigger(skill *SkillMeta, prompt string) bool {
	if skill.Trigger == "" {
		return false
	}
	lower := strings.ToLower(prompt)
	t := strings.TrimSpace(skill.Trigger)

	if strings.HasPrefix(t, "/") {
		// Support /pattern/ and /pattern/flags — find the closing slash.
		lastSlash := strings.LastIndex(t[1:], "/")
		if lastSlash >= 0 {
			pattern := t[1 : lastSlash+1]
			re, err := regexp.Compile("(?i)" + pattern)
			if err != nil {
				return false
			}
			return re.MatchString(lower)
		}
	}

	// Build a single word-boundary regex from all keywords so that short words
	// like "at" don't fire on substrings like "data" inside a filename.
	var parts []string
	for _, kw := range strings.Fields(strings.ToLower(t)) {
		parts = append(parts, regexp.QuoteMeta(kw))
	}
	if len(parts) == 0 {
		return false
	}
	re, err := regexp.Compile(`(?i)\b(` + strings.Join(parts, "|") + `)\b`)
	if err != nil {
		return false
	}
	return re.MatchString(lower)
}

/** SortedSkillNames returns the skill names from cat in sorted order.
 * Used to provide deterministic first-match semantics when scanning triggers.
 *
 * Parameters:
 *   cat (SkillCatalog) — the catalog to sort.
 *
 * Returns:
 *   []string — skill names in ascending alphabetical order.
 *
 * Example:
 *   for _, name := range SortedSkillNames(a.Skills) {
 *       if MatchesTrigger(a.Skills[name], input) { ... }
 *   }
 */
func SortedSkillNames(cat SkillCatalog) []string {
	names := make([]string, 0, len(cat))
	for n := range cat {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

/** DispatchSkill handles the full lifecycle when a skill is triggered:
 * checks for compiled scripts, handles staleness prompts, offers compilation,
 * and either runs the compiled script or falls back to LLM loading.
 *
 * Dispatch states:
 *   1. No compiled scripts → offer to compile; if declined fall back to LLM.
 *   2. Scripts exist but SKILL.md is newer → offer to recompile; then run.
 *   3. Scripts exist and up to date → run directly.
 *
 * Parameters:
 *   ctx    (context.Context) — for LLM calls if compilation is needed.
 *   a      (*Agent)          — the running agent (client, workspace, history).
 *   skill  (*SkillMeta)      — the skill to dispatch.
 *   prompt (string)          — the user's triggering prompt text (HARVEY_PROMPT).
 *   reader (*bufio.Reader)   — reads Y/n confirmations from the user.
 *   out    (io.Writer)       — destination for status messages and script output.
 *
 * Returns:
 *   bool  — true when the skill used the LLM fallback path and the caller should
 *            send the enriched history to the LLM to generate a response.
 *            false when a compiled script handled everything autonomously.
 *   error — on unexpected I/O or execution failure; non-zero script exit is reported but not returned.
 *
 * Example:
 *   llmNeeded, err := DispatchSkill(ctx, agent, skill, input, reader, os.Stdout)
 */
func DispatchSkill(ctx context.Context, a *Agent, skill *SkillMeta, prompt string, reader *bufio.Reader, out io.Writer) (bool, error) {
	a.DebugLog.LogSkillDispatch(skill.Name, "dispatch")
	bashPath := CompiledBashPath(skill.Path)
	ps1Path := CompiledPS1Path(skill.Path)

	scriptPath := bashPath
	if runtime.GOOS == "windows" {
		scriptPath = ps1Path
	}

	_, bashErr := os.Stat(bashPath)
	_, ps1Err := os.Stat(ps1Path)
	hasScripts := bashErr == nil && ps1Err == nil

	// State 1 — no compiled scripts.
	if !hasScripts {
		fmt.Fprintf(out, "  Skill %q has not been compiled yet.\n", skill.Name)
		if !askYesNo(reader, out, "  Compile now? [Y/n] ", true) {
			// Fall back: load skill body into LLM context; caller should call LLM.
			return true, skillLoadByMeta(a, skill, out)
		}
		fmt.Fprintln(out)
		if err := CompileSkill(ctx, a.Client, skill, out); err != nil {
			fmt.Fprintf(out, yellow("  ⚠ Compilation failed: %v — falling back to LLM mode.\n"), err)
			return true, skillLoadByMeta(a, skill, out)
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, green("✓")+" Compiled %q.\n", skill.Name)
		if !askYesNo(reader, out, "  Run now? [Y/n] ", true) {
			return false, nil
		}
		return false, runCompiledScript(ctx, a, skill, scriptPath, prompt, out)
	}

	// State 2 — scripts exist but may be stale.
	stale, err := IsStale(skill)
	if err != nil {
		return false, err
	}
	if stale {
		fmt.Fprintf(out, "  SKILL.md has changed since last compilation of %q.\n", skill.Name)
		if askYesNo(reader, out, "  Recompile? [Y/n] ", true) {
			fmt.Fprintln(out)
			if err := CompileSkill(ctx, a.Client, skill, out); err != nil {
				return false, err
			}
			fmt.Fprintln(out)
			fmt.Fprintf(out, green("✓")+" Recompiled %q.\n", skill.Name)
		}
	}

	// State 3 (or stale but user declined recompile) — run scripts autonomously.
	return false, runCompiledScript(ctx, a, skill, scriptPath, prompt, out)
}

// runCompiledScript executes the compiled script at scriptPath with HARVEY_*
// environment variables injected. Script stdout is written to out and injected
// into the conversation as a user context message. Non-zero exit is reported
// but does not return an error so the REPL can continue.
func runCompiledScript(ctx context.Context, a *Agent, skill *SkillMeta, scriptPath, prompt string, out io.Writer) error {
	workDir := ""
	if a.Workspace != nil {
		workDir = a.Workspace.Root
	}
	modelName := ""
	if a.Client != nil {
		modelName = a.Client.Name()
	}
	sessionID := ""
	// Derive API base URL from the active backend.
	apiBase := a.Config.Ollama.URL
	if a.Config.Llamafile.Active != "" {
		apiBase = a.Config.Llamafile.URL
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	} else {
		cmd = exec.CommandContext(ctx, "bash", scriptPath)
	}
	cmd.Dir = workDir
	// Build a restricted environment for skill execution
	// Start with filtered base environment, then add HARVEY_* variables
	cmd.Env = filterSkillEnvironment(os.Environ())
	cmd.Env = append(cmd.Env,
		"HARVEY_PROMPT="+prompt,
		"HARVEY_WORKDIR="+workDir,
		"HARVEY_MODEL="+modelName,
		"HARVEY_SESSION_ID="+sessionID,
		"HARVEY_API_BASE="+apiBase,
	)

	var buf strings.Builder
	mw := io.MultiWriter(out, &buf)
	cmd.Stdout = mw
	cmd.Stderr = out

	fmt.Fprintf(out, "  [running compiled skill: %s]\n", skill.Name)
	// Log skill execution
	if a.AuditBuffer != nil {
		a.AuditBuffer.Log(ActionSkillRun, skill.Name+": "+prompt, StatusAllowed)
	}
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(out, "  [exit: %v]\n", err)
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionSkillRun, skill.Name+": "+prompt, StatusError)
		}
	} else if a.AuditBuffer != nil {
		// Update status to success if no error
		a.AuditBuffer.Log(ActionSkillRun, skill.Name+": completed", StatusSuccess)
	}

	if buf.Len() > 0 {
		a.AddMessage("user", fmt.Sprintf("[skill:%s output]\n\n```\n%s\n```\n", skill.Name, buf.String()))
	}
	return nil
}

// skillLoadByMeta injects the skill body into the LLM context (LLM fallback path).
func skillLoadByMeta(a *Agent, skill *SkillMeta, out io.Writer) error {
	a.AddMessage("user", "[skill: "+skill.Name+"]\n\n"+skill.Body)
	fmt.Fprintf(out, "  Loaded skill %q into context (LLM fallback).\n", skill.Name)
	return nil
}
