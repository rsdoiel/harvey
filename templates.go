package harvey

import (
	"embed"
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
