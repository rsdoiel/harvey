// Package harvey — commands_skill.go implements the /skill and /skill-set
// slash command families for managing Harvey agent skills.
package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ─── /skill ──────────────────────────────────────────────────────────────────

/** cmdSkill lists or loads Agent Skills from the catalog discovered at startup.
 *
 * Subcommands:
 *   list           — list all available skills with name and description.
 *   load NAME      — inject the full skill body into the conversation as context.
 *   info NAME      — show path, source, license, and compatibility for a skill.
 *   status         — show total skill count broken down by scope.
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   args ([]string)  — subcommand and its arguments.
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — always nil (errors are reported inline).
 *
 * Example:
 *   /skill list
 *   /skill load go-review
 *   /skill info go-review
 */
/** cmdSkill handles Agent Skills management and execution. Skills are
 * structured tasks defined in SKILL.md files that can be loaded into
 * context, executed directly, or triggered automatically.
 *
 * Subcommands:
 *   list    — List all discovered skills in the skills directory
 *   load NAME — Load a skill's instructions into the conversation context
 *   info NAME — Show metadata (description, version, author, etc.) for a skill
 *   status  — Show skills directory paths and loaded skill status
 *   new     — Create a new skill interactively via the skill wizard
 *   run NAME — Execute a compiled skill directly
 *
 * Skills are discovered from the agents/skills/ directory tree on startup.
 * Each skill is defined in a SKILL.md file with YAML frontmatter containing
 * metadata (name, description, trigger, etc.) and Markdown body containing
 * instructions.
 *
 * Skills can be triggered automatically when a user's prompt matches the
 * skill's trigger pattern (see skill_dispatch.go for trigger matching logic).
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with skills catalog.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdSkill(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 || strings.ToLower(args[0]) == "list" {
		return skillList(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "load":
		if len(args) < 2 {
			names := skillNameCandidates(a)
			if len(names) == 0 {
				fmt.Fprintln(out, "Usage: /skill load NAME")
				return nil
			}
			chosen, err := SelectFromStrings(names, fmt.Sprintf("Load which skill [1-%d] or Enter to cancel: ", len(names)), a.In, out)
			if err != nil || chosen == "" {
				return err
			}
			args = append(args, chosen)
		}
		return skillLoad(a, args[1], out)
	case "info", "show":
		if len(args) < 2 {
			names := skillNameCandidates(a)
			if len(names) == 0 {
				fmt.Fprintln(out, "Usage: /skill show NAME")
				return nil
			}
			chosen, err := SelectFromStrings(names, fmt.Sprintf("Show which skill [1-%d] or Enter to cancel: ", len(names)), a.In, out)
			if err != nil || chosen == "" {
				return err
			}
			args = append(args, chosen)
		}
		return skillInfo(a, args[1], out)
	case "status":
		return skillStatus(a, out)
	case "new":
		return skillNew(a, out)
	case "run":
		if len(args) < 2 {
			names := skillNameCandidates(a)
			if len(names) == 0 {
				fmt.Fprintln(out, "Usage: /skill run NAME")
				return nil
			}
			chosen, err := SelectFromStrings(names, fmt.Sprintf("Run which skill [1-%d] or Enter to cancel: ", len(names)), a.In, out)
			if err != nil || chosen == "" {
				return err
			}
			args = append(args, chosen)
		}
		return skillRun(a, args[1], out)
	default:
		fmt.Fprintf(out, "Unknown skill subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /skill <list|load NAME|info NAME|status|new|run NAME>")
	}
	return nil
}

func skillList(a *Agent, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills discovered. See /help skills for setup instructions.")
		return nil
	}
	names := make([]string, 0, len(a.Skills))
	for n := range a.Skills {
		names = append(names, n)
	}
	sort.Strings(names)

	model := "(no model)"
	if a.Client != nil {
		model = a.Client.Name()
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Current model: %s\n", model)
	fmt.Fprintln(out)
	for _, n := range names {
		s := a.Skills[n]
		fmt.Fprintf(out, "  %-28s [%s]\n", n, s.Source)
		fmt.Fprintf(out, "    %s\n", s.Description)
		if s.Compatibility != "" {
			fmt.Fprintf(out, "    Compatibility: %s\n", s.Compatibility)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Use /skill load NAME to activate a skill.")
	return nil
}

func skillLoad(a *Agent, name string, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills available. See /help skills for setup instructions.")
		return nil
	}
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	if skill.Body == "" {
		fmt.Fprintf(out, "  Skill %q has no body content.\n", name)
		return nil
	}
	a.AddMessage("user", fmt.Sprintf("[skill: %s]\n\n%s", name, skill.Body))
	a.ActiveSkill = name
	if a.Recorder != nil {
		_ = a.Recorder.RecordSkillLoad(name, skill.Description, skill.Body)
	}
	fmt.Fprintf(out, "  ✓ Skill %q loaded into context (%d chars).\n", name, len(skill.Body))
	return nil
}

func skillInfo(a *Agent, name string, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills available.")
		return nil
	}
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	fmt.Fprintf(out, "  Name:          %s\n", skill.Name)
	fmt.Fprintf(out, "  Description:   %s\n", skill.Description)
	fmt.Fprintf(out, "  Source:        %s\n", skill.Source)
	fmt.Fprintf(out, "  Path:          %s\n", skill.Path)
	if skill.License != "" {
		fmt.Fprintf(out, "  License:       %s\n", skill.License)
	}
	if skill.Compatibility != "" {
		fmt.Fprintf(out, "  Compatibility: %s\n", skill.Compatibility)
	}
	if len(skill.Metadata) > 0 {
		keys := make([]string, 0, len(skill.Metadata))
		for k := range skill.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(out, "  Metadata:")
		for _, k := range keys {
			fmt.Fprintf(out, "    %s: %s\n", k, skill.Metadata[k])
		}
	}
	return nil
}

func skillStatus(a *Agent, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills discovered. See /help skills for setup instructions.")
		return nil
	}
	proj, user := 0, 0
	for _, s := range a.Skills {
		if s.Source == SkillSourceProject {
			proj++
		} else {
			user++
		}
	}
	fmt.Fprintf(out, "  Total: %d skill(s)\n", len(a.Skills))
	if proj > 0 {
		fmt.Fprintf(out, "    Project scope: %d\n", proj)
	}
	if user > 0 {
		fmt.Fprintf(out, "    User scope:    %d\n", user)
	}
	return nil
}

// skillNew runs the interactive skill wizard via /skill new.
func skillNew(a *Agent, out io.Writer) error {
	reader := bufio.NewReaderSize(a.In, 1)
	relPath, err := RunSkillWizard(a.Workspace, a.Config.AgentsDir, reader, out)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, green("✓")+" Skill created: %s\n", relPath)
	fmt.Fprintln(out, "  To make it runnable, add compiled.bash / compiled.ps1 under scripts/.")
	a.Skills = ScanSkills(a.Workspace.Root, a.Config.AgentsDir)
	a.registerSkillCommands()
	return nil
}

// skillCompile compiles a named skill to compiled.bash and compiled.ps1.
func skillCompile(a *Agent, name string, out io.Writer) error {
	if a.Client == nil {
		fmt.Fprintln(out, "  No backend connected. Use /ollama start first.")
		return nil
	}
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	fmt.Fprintf(out, "  Compiling skill %q...\n", name)
	sp := newSpinner(out, 0, a.spinnerLabel()+" · compiling")
	err := CompileSkill(context.Background(), a.Client, skill, io.Discard)
	sp.stop()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, green("✓")+" Compiled: scripts/compiled.bash and scripts/compiled.ps1")
	a.Skills = ScanSkills(a.Workspace.Root, a.Config.AgentsDir)
	a.registerSkillCommands()
	fmt.Fprintf(out, "  Tip: you can now run it as /%s\n", name)
	return nil
}

// skillRun dispatches a named skill using DispatchSkill.
func skillRun(a *Agent, name string, out io.Writer) error {
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	warnIfSkillStale(skill, out)
	reader := bufio.NewReaderSize(a.In, 1)
	_, err := DispatchSkill(context.Background(), a, skill, "", reader, out)
	return err
}

// warnIfSkillStale prints a warning when SKILL.md is newer than the compiled scripts.
func warnIfSkillStale(skill *SkillMeta, out io.Writer) {
	stale, err := IsStale(skill)
	if err == nil && stale {
		fmt.Fprintf(out, "  Warning: %s/SKILL.md has been updated since it was compiled.\n", skill.Name)
		fmt.Fprintln(out, "  Running the old compiled version. Recompile on a capable system to pick up changes.")
	}
}

// ─── /skill-set ──────────────────────────────────────────────────────────────

/** cmdSkillSet manages named YAML bundles of skills stored in
 * agents/skill-sets/. Subcommands: list, load, info, create, status, unload.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent.
 *   args ([]string)  — subcommand and optional name.
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — non-nil on unexpected I/O errors only; user errors are printed.
 *
 * Example:
 *   /skill-set load fountain
 */
func cmdSkillSet(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 || strings.ToLower(args[0]) == "list" {
		return skillSetList(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "load":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /skill-set load NAME")
			return nil
		}
		return skillSetLoad(a, args[1], out)
	case "info", "show":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /skill-set show NAME")
			return nil
		}
		return skillSetInfo(a, args[1], out)
	case "create", "new":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /skill-set new NAME")
			return nil
		}
		return skillSetCreate(a, args[1], out)
	case "status":
		return skillSetStatus(a, out)
	case "unload":
		return skillSetUnload(a, out)
	default:
		fmt.Fprintf(out, "  Unknown subcommand %q. Usage: /skill-set <list|load NAME|info NAME|create NAME|status|unload>\n", args[0])
	}
	return nil
}

func skillSetList(a *Agent, out io.Writer) error {
	names, err := listSkillSetNames(a.Workspace)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "  No skill-sets found in agents/skill-sets/.")
		fmt.Fprintln(out, "  Create one with: /skill-set create NAME")
		return nil
	}
	fmt.Fprintln(out)
	for _, name := range names {
		path := filepath.Join(skillSetDir(a.Workspace), name+".yaml")
		ss, err := ParseSkillSet(path)
		if err != nil {
			fmt.Fprintf(out, "  %-24s  (parse error: %v)\n", name, err)
			continue
		}
		desc := ss.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		active := ""
		if a.ActiveSkillSet == name {
			active = " ✓"
		}
		fmt.Fprintf(out, "  %-24s%s  %s\n", name, active, desc)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Use /skill-set load NAME to activate a bundle.")
	return nil
}

func skillSetLoad(a *Agent, name string, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills discovered. Run Harvey from a workspace with agents/skills/.")
		return nil
	}

	ss, err := findSkillSet(a.Workspace, name)
	if err != nil {
		fmt.Fprintf(out, "  %v\n", err)
		return nil
	}

	if err := validateSkillSet(ss, a.Skills); err != nil {
		fmt.Fprintf(out, "  %v\n", err)
		return nil
	}

	// Count tokens for the combined skill bodies.
	var combined strings.Builder
	for _, skillName := range ss.Skills {
		combined.WriteString(a.Skills[skillName].Body)
		combined.WriteString("\n")
	}
	ctx := context.Background()
	model := ""
	if a.Client != nil {
		model = a.Client.Name()
	}
	tokens, exact := CountTokens(ctx, a.Config.Ollama.URL, model, combined.String())
	contextLimit := a.effectiveContextLimit()

	if contextLimit > 0 {
		pct := tokens * 100 / contextLimit
		switch {
		case pct >= 100:
			fmt.Fprintf(out, "  ✗ Skill-set %q would use ~%d tokens (%d%% of %d-token context) — too large to load.\n",
				ss.Name, tokens, pct, contextLimit)
			fmt.Fprintln(out, "  Use /skill-set info to review the bundle and reduce its skills.")
			return nil
		case pct >= 50:
			fmt.Fprintf(out, "  ⚠ Skill-set %q uses ~%d tokens (%d%% of %d-token context).\n",
				ss.Name, tokens, pct, contextLimit)
		}
	}

	// Load each skill in order.
	loaded := 0
	for _, skillName := range ss.Skills {
		if err := skillLoad(a, skillName, out); err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", skillName, err)
		} else {
			loaded++
		}
	}

	a.ActiveSkillSet = ss.Name
	a.ActiveSkill = "" // skill-set takes priority in the status line

	exactStr := "~"
	if exact {
		exactStr = ""
	}
	fmt.Fprintf(out, "  Skill-set %q loaded: %d/%d skills, %s%d tokens",
		ss.Name, loaded, len(ss.Skills), exactStr, tokens)
	if contextLimit > 0 {
		fmt.Fprintf(out, " (%d%% of context)", tokens*100/contextLimit)
	}
	fmt.Fprintln(out, ".")
	return nil
}

func skillSetInfo(a *Agent, name string, out io.Writer) error {
	ss, err := findSkillSet(a.Workspace, name)
	if err != nil {
		fmt.Fprintf(out, "  %v\n", err)
		return nil
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Name:        %s\n", ss.Name)
	if ss.Description != "" {
		fmt.Fprintf(out, "  Description: %s\n", strings.ReplaceAll(strings.TrimRight(ss.Description, "\n"), "\n", "\n               "))
	}
	if len(ss.Metadata) > 0 {
		keys := make([]string, 0, len(ss.Metadata))
		for k := range ss.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(out, "  %-12s %s\n", k+":", ss.Metadata[k])
		}
	}
	fmt.Fprintf(out, "  Skills (%d):\n", len(ss.Skills))
	for _, skillName := range ss.Skills {
		status := "✓ found"
		desc := ""
		if a.Skills != nil {
			if meta, ok := a.Skills[skillName]; ok {
				d := meta.Description
				if len(d) > 60 {
					d = d[:57] + "..."
				}
				desc = " — " + d
			} else {
				status = "✗ not found"
			}
		}
		fmt.Fprintf(out, "    [%s] %s%s\n", status, skillName, desc)
	}
	fmt.Fprintln(out)
	return nil
}

func skillSetCreate(a *Agent, name string, out io.Writer) error {
	dir := skillSetDir(a.Workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("skill-set create: %w", err)
	}
	path := filepath.Join(dir, name+".yaml")
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(out, "  Skill-set %q already exists at %s\n", name, path)
		return nil
	}
	content := fmt.Sprintf(`name: %s
description: |
  Describe when to use this skill bundle.
skills:
  - skill-name-here
metadata:
  version: "1.0"
`, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("skill-set create: %w", err)
	}
	fmt.Fprintf(out, "  Created %s\n", path)
	fmt.Fprintln(out, "  Edit the file to list the skills you want in this bundle.")
	return nil
}

func skillSetStatus(a *Agent, out io.Writer) error {
	if a.ActiveSkillSet == "" {
		fmt.Fprintln(out, "  No skill-set loaded.")
		if a.ActiveSkill != "" {
			fmt.Fprintf(out, "  Individual skill active: %s\n", a.ActiveSkill)
		}
		return nil
	}
	fmt.Fprintf(out, "  Active skill-set: %s\n", a.ActiveSkillSet)
	if ss, err := findSkillSet(a.Workspace, a.ActiveSkillSet); err == nil {
		for _, skillName := range ss.Skills {
			fmt.Fprintf(out, "    - %s\n", skillName)
		}
	}
	return nil
}

func skillSetUnload(a *Agent, out io.Writer) error {
	if a.ActiveSkillSet == "" && a.ActiveSkill == "" {
		fmt.Fprintln(out, "  No skill-set or skill currently active.")
		return nil
	}
	prev := a.ActiveSkillSet
	a.ActiveSkillSet = ""
	a.ActiveSkill = ""
	if prev != "" {
		fmt.Fprintf(out, "  Skill-set %q unloaded from status indicator.\n", prev)
	} else {
		fmt.Fprintln(out, "  Skill indicator cleared.")
	}
	fmt.Fprintln(out, "  Note: skill content already injected into context history remains.")
	fmt.Fprintln(out, "  Use /clear for a clean slate.")
	return nil
}
