// Package harvey — skill_suggestor.go implements /skill suggest, which reads a
// session transcript and proposes multi-step workflows as skill candidates.
package harvey

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// skillSuggestorPrompt is the system prompt used to extract skill candidates
// from a session transcript.
const skillSuggestorPrompt = `You are a skill extraction assistant for Harvey, a terminal coding agent.

OUTPUT FORMAT — STRICT: Your entire response must be a valid JSON array.
- Start your response with [ and end with ]
- No prose, no explanation, no markdown fences
- If nothing qualifies, respond with exactly: []

Your task: read the Harvey session transcript and propose 0-3 reusable, named
skills that appear as multi-step workflows in the session.

A skill candidate must have ALL of the following properties:
  - At least 3 distinct steps that a user would repeat across sessions
  - A clear, stable purpose that is not session-specific
  - Benefits from having named variables (e.g. experiment name, model name)

Do NOT propose:
  - One-off debugging sessions
  - Single-step operations
  - Workflows that are already obvious from the tool documentation

Each object in the JSON array must have exactly these fields:
  name             string  — short kebab-case identifier (e.g. "setup-experiment")
  description      string  — one sentence; used as the SKILL.md name/description field
  long_description string  — 2-3 sentences explaining what the skill does and when to use it
  variables        array   — 0-N variable objects (see below); empty array if none needed
  steps            array   — ordered list of step descriptions as plain strings

Each variable object must have exactly these fields:
  name        string — UPPER_SNAKE_CASE identifier (e.g. "EXPERIMENT_NAME")
  type        string — always "string" for now
  description string — one sentence describing what the variable holds
  example     string — a realistic example value

Example of valid output (one candidate):
[{
  "name": "setup-experiment",
  "description": "Initialise a new Laboratory experiment with git, codemeta.json, and agents/ structure.",
  "long_description": "Creates the directory, runs git init, copies a codemeta.json template, and creates the agents/ directory. Use when starting a new Harvey experiment in the Laboratory workspace.",
  "variables": [
    {"name": "EXPERIMENT_NAME", "type": "string", "description": "Name of the new experiment directory", "example": "harvey"}
  ],
  "steps": [
    "Create the experiment directory under Laboratory/",
    "Run git init inside the new directory",
    "Copy codemeta.json template and fill in project metadata",
    "Create agents/ directory for Harvey workspace files"
  ]
}]`

// SkillCandidate holds a single skill proposal extracted from a session
// transcript by the LLM.
type SkillCandidate struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	LongDescription string          `json:"long_description"`
	Variables       []SkillVariable `json:"variables"`
	Steps           []string        `json:"steps"`
}

/** Suggestor drives the /skill suggest pipeline: it reads a session transcript,
 * asks the LLM to propose skill candidates, and interactively writes accepted
 * candidates as SKILL.md files to the workspace.
 *
 * Parameters:
 *   ws (*Workspace) — workspace used for file I/O and sessions directory.
 *
 * Returns:
 *   *Suggestor — ready to call Suggest.
 *
 * Example:
 *   sg := NewSuggestor(a.Workspace)
 *   err := sg.Suggest(ctx, "", a, os.Stdout, os.Stdin)
 */
type Suggestor struct {
	ws *Workspace
}

/** NewSuggestor creates a Suggestor anchored to the given workspace.
 *
 * Parameters:
 *   ws (*Workspace) — workspace providing the sessions dir and skill output path.
 *
 * Returns:
 *   *Suggestor
 *
 * Example:
 *   sg := NewSuggestor(a.Workspace)
 */
func NewSuggestor(ws *Workspace) *Suggestor {
	return &Suggestor{ws: ws}
}

/** Suggest reads sessionPath (or the most recent session when empty), asks the
 * LLM to propose skill candidates, and writes accepted candidates as SKILL.md
 * files under agents/skills/<name>/.
 *
 * Parameters:
 *   ctx         (context.Context) — context for the LLM call.
 *   sessionPath (string)          — path to a .spmd file; "" = most recent session.
 *   agent       (*Agent)          — running agent (provides Client and Workspace).
 *   out         (io.Writer)       — progress and prompt output.
 *   in          (io.Reader)       — user input for the interactive review.
 *
 * Returns:
 *   error — non-nil on I/O or LLM failure.
 *
 * Example:
 *   err := sg.Suggest(ctx, "agents/sessions/foo.spmd", a, os.Stdout, os.Stdin)
 */
func (s *Suggestor) Suggest(ctx context.Context, sessionPath string, agent *Agent, out io.Writer, in io.Reader) error {
	if agent.Client == nil {
		return fmt.Errorf("no LLM backend connected; start a model first")
	}
	if s.ws == nil {
		return fmt.Errorf("no workspace open")
	}

	// Resolve session path.
	if sessionPath == "" {
		sessDir := filepath.Join(s.ws.HarveyDir(), "sessions")
		var err error
		sessionPath, err = latestSessionFile(sessDir)
		if err != nil {
			return fmt.Errorf("skill suggest: %w", err)
		}
	}

	sessionText, err := os.ReadFile(sessionPath)
	if err != nil {
		return fmt.Errorf("read session %s: %w", sessionPath, err)
	}

	fmt.Fprintf(out, "Analysing session: %s\n", filepath.Base(sessionPath))

	// Ask LLM to extract skill candidates.
	var buf strings.Builder
	if _, err := agent.Client.Chat(ctx, []Message{
		{Role: "system", Content: skillSuggestorPrompt},
		{Role: "user", Content: string(sessionText)},
	}, &buf); err != nil {
		return fmt.Errorf("skill suggest: LLM call failed: %w", err)
	}

	// Parse candidates.
	jsonStr, ok := extractJSON(buf.String())
	if !ok {
		fmt.Fprintln(out, "No skill candidates found in this session.")
		return nil
	}
	var candidates []SkillCandidate
	if err := json.Unmarshal([]byte(jsonStr), &candidates); err != nil || len(candidates) == 0 {
		fmt.Fprintln(out, "No skill candidates found in this session.")
		return nil
	}

	// Interactive review.
	br := bufio.NewReader(in)
	for _, c := range candidates {
		fmt.Fprintf(out, "\nProposed skill: %s\n", c.Name)
		fmt.Fprintf(out, "  %s\n", c.Description)
		if len(c.Variables) > 0 {
			names := make([]string, len(c.Variables))
			for i, v := range c.Variables {
				names[i] = v.Name
			}
			fmt.Fprintf(out, "  Variables: %s\n", strings.Join(names, ", "))
		}
		fmt.Fprintf(out, "  Steps: %d\n", len(c.Steps))
		fmt.Fprint(out, "Accept? [y/n/q] ")

		line, _ := br.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			if err := writeSkillMD(s.ws, c); err != nil {
				fmt.Fprintf(out, "  Error writing skill: %v\n", err)
			} else {
				fmt.Fprintf(out, "  Written: agents/skills/%s/SKILL.md\n", c.Name)
			}
		case "n", "no":
			fmt.Fprintln(out, "  Skipped.")
		default: // "q", "quit", "" — stop
			fmt.Fprintln(out, "  Quit.")
			return nil
		}
	}
	return nil
}

/** writeSkillMD creates agents/skills/<name>/SKILL.md (and the scripts/
 * subdirectory) inside ws, rendering the candidate with renderSkillMD.
 *
 * Parameters:
 *   ws (Workspace)       — workspace that owns the skills directory.
 *   c  (SkillCandidate) — candidate to write.
 *
 * Returns:
 *   error — non-nil on I/O failure.
 *
 * Example:
 *   err := writeSkillMD(a.Workspace, candidate)
 */
func writeSkillMD(ws *Workspace, c SkillCandidate) error {
	skillDir := filepath.Join("agents", "skills", c.Name)
	if err := ws.MkdirAll(filepath.Join(skillDir, "scripts")); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	content := renderSkillMD(c, gitAuthor())
	return ws.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644)
}

// gitAuthor returns the git user.name for the current repository, or
// "unknown" when git is unavailable or unconfigured.
func gitAuthor() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return "unknown"
	}
	if name := strings.TrimSpace(string(out)); name != "" {
		return name
	}
	return "unknown"
}

/** renderSkillMD renders a SkillCandidate as a complete SKILL.md file body.
 *
 * Parameters:
 *   c      (SkillCandidate) — the candidate to render.
 *   author (string)         — author name for the metadata block (e.g. from git config).
 *
 * Returns:
 *   string — the full SKILL.md content ready to write to disk.
 *
 * Example:
 *   md := renderSkillMD(candidate, "R. S. Doiel")
 *   ws.WriteFile("agents/skills/setup-experiment/SKILL.md", []byte(md), 0644)
 */
func renderSkillMD(c SkillCandidate, author string) string {
	var sb strings.Builder

	// YAML frontmatter
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s\n", c.Name)
	sb.WriteString("description: |\n")
	sb.WriteString("  ")
	sb.WriteString(c.LongDescription)
	sb.WriteByte('\n')
	sb.WriteString("license: AGPL-3.0\n")
	sb.WriteString("compatibility: harvey\n")
	sb.WriteString("metadata:\n")
	fmt.Fprintf(&sb, "  author: %s\n", author)
	sb.WriteString("  version: 0.1.0\n")
	if len(c.Variables) > 0 {
		sb.WriteString("variables:\n")
		for _, v := range c.Variables {
			fmt.Fprintf(&sb, "  %s:\n", v.Name)
			sb.WriteString("    type: string\n")
			fmt.Fprintf(&sb, "    description: %s\n", v.Description)
			fmt.Fprintf(&sb, "    example: %q\n", v.Example)
		}
	}
	sb.WriteString("---\n\n")

	// Body
	sb.WriteString(c.LongDescription)
	sb.WriteString("\n\n## Steps\n\n")
	for i, step := range c.Steps {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, step)
	}
	sb.WriteString("\n---\n\n")
	sb.WriteString("*Generated by `/skill suggest`. Review and refine before use.*\n")

	return sb.String()
}

/** latestSessionFile returns the path of the most recently modified .spmd file
 * in sessionsDir. Returns an error if the directory is empty or unreadable.
 *
 * Parameters:
 *   sessionsDir (string) — directory to search (e.g. agents/sessions/).
 *
 * Returns:
 *   string — absolute path to the newest .spmd file.
 *   error  — non-nil if no .spmd files are found or the directory cannot be read.
 *
 * Example:
 *   path, err := latestSessionFile("/workspace/agents/sessions")
 */
func latestSessionFile(sessionsDir string) (string, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return "", fmt.Errorf("read sessions dir: %w", err)
	}

	var latest string
	var latestMod int64 // Unix nanoseconds

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".spmd" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if ns := info.ModTime().UnixNano(); ns > latestMod {
			latestMod = ns
			latest = filepath.Join(sessionsDir, e.Name())
		}
	}

	if latest == "" {
		return "", fmt.Errorf("no session files found in %s", sessionsDir)
	}
	return latest, nil
}
