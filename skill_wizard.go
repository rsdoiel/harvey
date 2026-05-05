// Package harvey — skill_wizard.go provides an interactive wizard for creating
// new skills. The wizard guides the user through defining skill metadata (name,
// description, license, etc.) and editing the skill instructions in their
// preferred editor ($EDITOR or $VISUAL, falling back to vi).
//
// The wizard performs validation on each field:
//   - name: lowercase letters, numbers, hyphens only
//   - description: required field
//   - All other fields have sensible defaults
//
// After collecting all fields, the wizard assembles a complete SKILL.md file
// with proper YAML frontmatter and writes it to .agents/skills/<name>/SKILL.md.
// This file is invoked by /skill new when no arguments are provided.

package harvey

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// allowedEditorPrefixes contains prefixes that are allowed for editor paths.
// This prevents command injection via EDITOR/VISUAL environment variables.
var allowedEditorPrefixes = []string{
	"/usr/bin/",
	"/usr/local/bin/",
	"/bin/",
	"/opt/",
	"/home/",
	"/usr/sbin/",
	"/sbin/",
}

// allowedEditorNames contains simple editor names that are allowed.
var allowedEditorNames = map[string]bool{
	"vi":      true,
	"vim":     true,
	"nano":    true,
	"emacs":   true,
	"ed":      true,
	"code":    true, // VS Code
	"subl":    true, // Sublime Text
	"atom":    true,
	"gedit":   true,
	"kate":    true,
	"mousepad": true,
	"pluma":   true,
	"geany":   true,
	"notepad":  true,
	"notepad++": true,
}

/** validateEditorPath validates that an editor path is safe to execute.
 * It prevents command injection via EDITOR/VISUAL environment variables by:
 *   1. Rejecting editors containing shell metacharacters (;, |, &, >, <, etc.)
 *   2. Rejecting editors with path traversal (..)
 *   3. Only allowing known editor names or paths in standard locations
 *
 * Parameters:
 *   editor (string) — the editor path to validate.
 *
 * Returns:
 *   string — the validated editor path (cleaned).
 *   error  — if the editor path is not allowed.
 */
func validateEditorPath(editor string) (string, error) {
	if editor == "" {
		return "", fmt.Errorf("empty editor path")
	}

	// Check for shell metacharacters
	shellMetaChars := []string{";", "|", "&", ">", "<", "$", "`", "(", ")", "{", "}", "[", "]"}
	for _, ch := range shellMetaChars {
		if strings.Contains(editor, ch) {
			return "", fmt.Errorf("editor path contains disallowed character %q", ch)
		}
	}

	// Check for path traversal
	if strings.Contains(editor, "..") {
		return "", fmt.Errorf("editor path contains path traversal (..)")
	}

	// Clean the path
	cleaned := filepath.Clean(editor)

	// Check if it's a simple known editor name
	baseName := filepath.Base(cleaned)
	if allowedEditorNames[baseName] {
		return cleaned, nil
	}

	// Check if it's in an allowed prefix
	for _, prefix := range allowedEditorPrefixes {
		if strings.HasPrefix(cleaned, prefix) {
			// Verify the path exists and is a regular file
			info, err := os.Stat(cleaned)
			if err != nil {
				return "", fmt.Errorf("editor path %q does not exist", cleaned)
			}
			if info.IsDir() {
				return "", fmt.Errorf("editor path %q is a directory", cleaned)
			}
			return cleaned, nil
		}
	}

	// If it's a relative path, reject it
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("relative editor paths are not allowed (use absolute path or known editor name)")
	}

	return "", fmt.Errorf("editor %q is not in an allowed location", editor)
}

/** filterSkillWizardEnvironment returns a filtered copy of the environment
 * for the editor process. Sensitive variables are excluded.
 *
 * Parameters:
 *   env ([]string) — the original environment in "KEY=VALUE" format.
 *
 * Returns:
 *   []string — filtered environment.
 */
func filterSkillWizardEnvironment(env []string) []string {
	sensitiveVars := map[string]bool{
		"ANTHROPIC_API_KEY": true,
		"DEEPSEEK_API_KEY":  true,
		"GEMINI_API_KEY":    true,
		"GOOGLE_API_KEY":    true,
		"MISTRAL_API_KEY":   true,
		"OPENAI_API_KEY":    true,
	}

	var result []string
	for _, e := range env {
		idx := strings.IndexByte(e, '=')
		if idx == -1 {
			continue
		}
		varName := e[:idx]

		// Exclude sensitive variables
		if sensitiveVars[varName] {
			continue
		}

		// Include HARVEY_* and OLLAMA_* variables
		if strings.HasPrefix(varName, "HARVEY_") || strings.HasPrefix(varName, "OLLAMA_") {
			result = append(result, e)
			continue
		}

		// Include standard environment variables that editors might need
		switch varName {
		case "PATH", "HOME", "USER", "USERNAME", "SHELL", "TERM",
			"LANG", "LC_ALL", "LC_MESSAGES", "PWD", "TMPDIR", "TEMP":
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

// defaultSkillBody is the template written to the editor for the skill body.
const defaultSkillBody = `# Skill Name

## When to use this skill

Describe the scenarios where this skill should be activated.

## Instructions

Step-by-step instructions for the model to follow when this skill is active.
`

/** RunSkillWizard interactively collects skill metadata from the user,
 * opens $EDITOR/$VISUAL for the instructions body, and writes the resulting
 * SKILL.md into ws at .agents/skills/<name>/SKILL.md.
 *
 * Short fields (name, description, license, compatibility, author, version,
 * trigger) are collected via the provided reader. The instructions body is
 * edited in $VISUAL or $EDITOR (falling back to vi).
 *
 * Parameters:
 *   ws     (*Workspace)    — workspace for path resolution and file writes.
 *   reader (*bufio.Reader) — reads short field responses; use bufio.NewReaderSize(os.Stdin, 1).
 *   out    (io.Writer)     — destination for prompts and status messages.
 *
 * Returns:
 *   relPath (string) — relative path to the created SKILL.md, e.g. ".agents/skills/my-skill/SKILL.md".
 *   err     (error)  — on validation failure, editor error, or write failure.
 *
 * Example:
 *   relPath, err := RunSkillWizard(ws, bufio.NewReaderSize(os.Stdin, 1), os.Stdout)
 *   if err != nil { log.Fatal(err) }
 *   fmt.Println("created:", relPath)
 */
func RunSkillWizard(ws *Workspace, reader *bufio.Reader, out io.Writer) (string, error) {
	// ── name ────────────────────────────────────────────────────────────────
	fmt.Fprint(out, "  Skill name (lowercase letters, numbers, hyphens): ")
	name, err := readLine(reader)
	if err != nil {
		return "", fmt.Errorf("skill wizard: read name: %w", err)
	}
	if !ValidSkillName(name) {
		return "", fmt.Errorf("skill wizard: invalid name %q (use lowercase letters, numbers, hyphens only)", name)
	}

	relMD := ".agents/skills/" + name + "/SKILL.md"
	abs, err := ws.AbsPath(relMD)
	if err != nil {
		return "", fmt.Errorf("skill wizard: %w", err)
	}
	if _, statErr := os.Stat(abs); statErr == nil {
		return "", fmt.Errorf("skill wizard: skill %q already exists at %s", name, abs)
	}

	// ── short fields ────────────────────────────────────────────────────────
	description, err := promptRequiredField(reader, out, "description")
	if err != nil {
		return "", err
	}
	license := promptField(reader, out, "license", "Apache-2.0")
	compatibility := promptField(reader, out, "compatibility", "")
	defaultAuthor := os.Getenv("USER")
	if defaultAuthor == "" {
		defaultAuthor = os.Getenv("USERNAME") // Windows fallback
	}
	author := promptField(reader, out, "author", defaultAuthor)
	version := promptField(reader, out, "version", "1.0")
	trigger := promptField(reader, out, "trigger (optional regex or keywords)", "")

	// ── editor for body ─────────────────────────────────────────────────────
	tmp, err := os.CreateTemp("", "harvey-skill-*.md")
	if err != nil {
		return "", fmt.Errorf("skill wizard: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(defaultSkillBody); err != nil {
		tmp.Close()
		return "", fmt.Errorf("skill wizard: write temp file: %w", err)
	}
	tmp.Close()

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	// Validate editor path for security - only allow simple binary names or
	// paths within standard locations, prevent command injection
	validatedEditor, err := validateEditorPath(editor)
	if err != nil {
		return "", fmt.Errorf("skill wizard: invalid editor %q: %w", editor, err)
	}
	editor = validatedEditor

	fmt.Fprintf(out, "  Opening %s for skill body...\n", editor)
	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	// Filter environment to prevent sensitive data leakage
	editorCmd.Env = filterSkillWizardEnvironment(os.Environ())
	if err := editorCmd.Run(); err != nil {
		return "", fmt.Errorf("skill wizard: editor %q: %w", editor, err)
	}

	bodyBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("skill wizard: read edited body: %w", err)
	}
	body := string(bodyBytes)

	// ── assemble SKILL.md ────────────────────────────────────────────────────
	content := buildSkillMD(name, description, license, compatibility, trigger, author, version, body)

	if err := ws.WriteFile(relMD, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("skill wizard: write SKILL.md: %w", err)
	}
	return relMD, nil
}

// buildSkillMD assembles the SKILL.md content string from its parts.
// Optional fields (compatibility, trigger) are omitted when empty.
func buildSkillMD(name, description, license, compatibility, trigger, author, version, body string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s\n", name)
	fmt.Fprintf(&sb, "description: %s\n", description)
	fmt.Fprintf(&sb, "license: %s\n", license)
	if compatibility != "" {
		fmt.Fprintf(&sb, "compatibility: %s\n", compatibility)
	}
	if trigger != "" {
		fmt.Fprintf(&sb, "trigger: %s\n", trigger)
	}
	sb.WriteString("metadata:\n")
	fmt.Fprintf(&sb, "  author: %s\n", author)
	fmt.Fprintf(&sb, "  version: \"%s\"\n", version)
	sb.WriteString("---\n\n")
	sb.WriteString(body)
	return sb.String()
}

// readLine reads a trimmed line from reader.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	return strings.TrimSpace(line), err
}

// promptField prints a prompt with an optional default, reads a line, and
// returns the input trimmed; returns defaultVal when input is empty.
func promptField(r *bufio.Reader, out io.Writer, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Fprintf(out, "  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Fprintf(out, "  %s: ", label)
	}
	line, _ := readLine(r)
	if line == "" {
		return defaultVal
	}
	return line
}

// promptRequiredField prompts for a field that must be non-empty, re-prompting
// once if the first response is blank.
func promptRequiredField(r *bufio.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprintf(out, "  %s (required): ", label)
	line, err := readLine(r)
	if err != nil {
		return "", fmt.Errorf("skill wizard: read %s: %w", label, err)
	}
	if line == "" {
		fmt.Fprintf(out, "  %s cannot be empty. %s: ", label, strings.Title(label))
		line, err = readLine(r)
		if err != nil {
			return "", fmt.Errorf("skill wizard: read %s: %w", label, err)
		}
	}
	if line == "" {
		return "", fmt.Errorf("skill wizard: %s is required", label)
	}
	return line, nil
}
