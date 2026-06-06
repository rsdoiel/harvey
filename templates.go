package harvey

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates
var EmbeddedTemplates embed.FS

// Package-level help text vars loaded from embedded guides at init time.
// These are accessible to cmdHelp alongside the other HelpText vars in helptext.go.
var (
	PDFToolsHelpText    string
	GettingStartedHelpText string
)

func init() {
	if data, err := EmbeddedTemplates.ReadFile("templates/help/pdf-tools.md"); err == nil {
		PDFToolsHelpText = string(data)
	}
	if data, err := EmbeddedTemplates.ReadFile("templates/help/getting-started.md"); err == nil {
		GettingStartedHelpText = string(data)
	}
}

/** TemplateEntry describes one profile template available to the user.
 *
 * Parameters: (none — struct fields)
 *   Name        (string) — human-readable display name, e.g. "Back End Developer".
 *   File        (string) — filename without directory, e.g. "backend-developer.fountain".
 *   Source      (string) — "builtin" for embedded templates, "workspace" for
 *                          templates found in agents/templates/profiles/.
 *   Recommended (string) — content of the NOTE: field, or "" if absent.
 *                          Displayed during template selection as a non-enforced suggestion.
 *
 * Example:
 *   entries := ListTemplates("/home/user/myproject")
 *   for _, e := range entries {
 *       fmt.Printf("[%s] %s\n", e.Source, e.Name)
 *   }
 */
type TemplateEntry struct {
	Name        string
	File        string
	Source      string
	Recommended string
}

/** ListTemplates returns all available profile templates, merging built-in
 * (embedded) templates with any .fountain files found in
 * agents/templates/profiles/ within the workspace. Workspace-local entries
 * appear after built-ins; a workspace entry with the same filename as a
 * built-in shadows the built-in.
 *
 * Parameters:
 *   wsRoot (string) — absolute path to the workspace root. Pass "" to skip
 *                     workspace-local template discovery.
 *
 * Returns:
 *   []TemplateEntry — ordered list: built-ins first (in directory order),
 *                     then workspace-local additions.
 *
 * Example:
 *   entries := ListTemplates(agent.Workspace.Root)
 *   for i, e := range entries {
 *       fmt.Printf("  [%d] %s\n", i+1, e.Name)
 *   }
 */
func ListTemplates(wsRoot string) []TemplateEntry {
	seen := map[string]bool{}
	var entries []TemplateEntry

	// Built-in templates from the embedded FS.
	dirEntries, err := EmbeddedTemplates.ReadDir("templates/profiles")
	if err == nil {
		for _, de := range dirEntries {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".fountain") {
				continue
			}
			data, err := EmbeddedTemplates.ReadFile("templates/profiles/" + de.Name())
			if err != nil {
				continue
			}
			entry := TemplateEntry{
				Name:        templateTitleField(data, de.Name()),
				File:        de.Name(),
				Source:      "builtin",
				Recommended: TemplateNoteField(data),
			}
			entries = append(entries, entry)
			seen[de.Name()] = true
		}
	}

	// Workspace-local templates override or extend the built-in set.
	if wsRoot != "" {
		localDir := filepath.Join(wsRoot, "agents", "templates", "profiles")
		localEntries, err := os.ReadDir(localDir)
		if err == nil {
			for _, de := range localEntries {
				if de.IsDir() || !strings.HasSuffix(de.Name(), ".fountain") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(localDir, de.Name()))
				if err != nil {
					continue
				}
				entry := TemplateEntry{
					Name:        templateTitleField(data, de.Name()),
					File:        de.Name(),
					Source:      "workspace",
					Recommended: TemplateNoteField(data),
				}
				if seen[de.Name()] {
					// Shadow the built-in: replace it in-place.
					for i, e := range entries {
						if e.File == de.Name() {
							entries[i] = entry
							break
						}
					}
				} else {
					entries = append(entries, entry)
					seen[de.Name()] = true
				}
			}
		}
	}

	return entries
}

/** LoadTemplate reads a built-in profile template by stem name (the filename
 * without directory or extension). Returns the raw file bytes for display or
 * editing. Does not load workspace-local templates; callers that need those
 * can read the file directly using the path derived from TemplateEntry.File.
 *
 * Parameters:
 *   name (string) — template stem, e.g. "backend-developer".
 *
 * Returns:
 *   []byte — raw template file contents.
 *   error  — if the template is not found in the embedded FS.
 *
 * Example:
 *   data, err := LoadTemplate("backend-developer")
 */
func LoadTemplate(name string) ([]byte, error) {
	if !strings.HasSuffix(name, ".fountain") {
		name += ".fountain"
	}
	return EmbeddedTemplates.ReadFile("templates/profiles/" + name)
}

/** LoadHelpGuide reads an embedded help guide by stem name (the filename
 * without directory or extension).
 *
 * Parameters:
 *   name (string) — guide stem, e.g. "ollama", "pdf-tools", "getting-started".
 *
 * Returns:
 *   []byte — raw guide file contents (Markdown).
 *   error  — if the guide is not found in the embedded FS.
 *
 * Example:
 *   data, err := LoadHelpGuide("pdf-tools")
 */
func LoadHelpGuide(name string) ([]byte, error) {
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	return EmbeddedTemplates.ReadFile("templates/help/" + name)
}

/** TemplateNoteField extracts the content of the NOTE: field from a template
 * file. The NOTE: block begins at the line starting with "NOTE:" and ends at
 * the next line that starts a new all-caps field (e.g. "NAME:", "ROLE:") or
 * at end of file. Returns "" when no NOTE: field is present.
 *
 * Parameters:
 *   content ([]byte) — raw template file bytes.
 *
 * Returns:
 *   string — trimmed note text, or "" if absent.
 *
 * Example:
 *   data, _ := LoadTemplate("backend-developer")
 *   note := TemplateNoteField(data)
 *   // "Recommended model: qwen2.5-coder:7b · ingest project source and deps"
 */
func TemplateNoteField(content []byte) string {
	lines := strings.Split(string(content), "\n")
	var noteLines []string
	inNote := false

	for _, line := range lines {
		if strings.HasPrefix(line, "NOTE:") {
			inNote = true
			rest := strings.TrimSpace(strings.TrimPrefix(line, "NOTE:"))
			if rest != "" {
				noteLines = append(noteLines, rest)
			}
			continue
		}
		if inNote {
			// A new all-caps field ends the NOTE block.
			if isTemplateField(line) {
				break
			}
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				noteLines = append(noteLines, trimmed)
			}
		}
	}

	return strings.Join(noteLines, " ")
}

// templateTitleField returns the value of the TITLE: field, or a humanized
// version of the filename when no TITLE: field is present.
func templateTitleField(content []byte, filename string) string {
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "TITLE:") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "TITLE:"))
			if title != "" {
				return title
			}
		}
	}
	// Humanize filename: "backend-developer.fountain" → "Backend Developer"
	stem := strings.TrimSuffix(filename, ".fountain")
	parts := strings.Split(stem, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// isTemplateField reports whether line starts a new top-level template field
// (no leading whitespace, an all-caps word immediately followed by a colon).
func isTemplateField(line string) bool {
	if len(line) == 0 || line[0] == ' ' || line[0] == '\t' {
		return false
	}
	colon := strings.Index(line, ":")
	if colon <= 0 {
		return false
	}
	word := line[:colon]
	return word == strings.ToUpper(word) && strings.TrimSpace(word) != ""
}
