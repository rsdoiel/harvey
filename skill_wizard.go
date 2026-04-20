package harvey

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

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

	fmt.Fprintf(out, "  Opening %s for skill body...\n", editor)
	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
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
