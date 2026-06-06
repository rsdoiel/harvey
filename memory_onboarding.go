package harvey

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

/** NeedsOnboarding reports whether the workspace has no workspace_profile
 * memory documents. When true, RunOnboarding should be called before the
 * REPL starts.
 *
 * Parameters:
 *   store (*MemoryStore) — the open memory store for the workspace.
 *
 * Returns:
 *   bool — true when workspace_profile/ contains no .fountain files.
 *
 * Example:
 *   if NeedsOnboarding(store) {
 *       RunOnboarding(a, store, embedder, os.Stdout, os.Stdin)
 *   }
 */
func NeedsOnboarding(store *MemoryStore) bool {
	dir := filepath.Join(store.Dir(), string(MemoryTypeWorkspaceProfile))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".fountain") {
			return false
		}
	}
	return true
}

/** RunOnboarding presents a profile template picker, optionally opens the
 * chosen template in $EDITOR, and saves the result as a workspace_profile
 * memory document. A project_fact is also extracted and saved when the
 * workspace contains a recognisable manifest file.
 *
 * The editor step is skipped when stdin is not an interactive terminal
 * (e.g. replay mode, pipes, or test runners). The selected template is
 * saved as-is in that case.
 *
 * Parameters:
 *   a        (*Agent)       — the Harvey agent (provides workspace root).
 *   store    (*MemoryStore) — the open memory store for the workspace.
 *   embedder (Embedder)     — used to compute embedding vectors; may be nil.
 *   out      (io.Writer)    — output writer for prompts and status.
 *   in       (io.Reader)    — input reader for user responses.
 *
 * Returns:
 *   error — if the workspace_profile cannot be saved.
 *
 * Example:
 *   if NeedsOnboarding(store) {
 *       err := RunOnboarding(a, store, embedder, os.Stdout, os.Stdin)
 *   }
 */
func RunOnboarding(a *Agent, store *MemoryStore, embedder Embedder, out io.Writer, in io.Reader) error {
	wsRoot := ""
	if a.Workspace != nil {
		wsRoot = a.Workspace.Root
	}
	wsName := filepath.Base(wsRoot)
	if wsName == "" || wsName == "." {
		wsName = "workspace"
	}

	templates := ListTemplates(wsRoot)

	fmt.Fprintf(out, "\nHarvey: I don't have a workspace profile yet. Choose a starting point:\n\n")
	for i, t := range templates {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, t.Name)
		if t.Recommended != "" {
			fmt.Fprintf(out, "      %s\n", t.Recommended)
		}
	}
	fmt.Fprintln(out)

	sc := bufio.NewReader(in)
	line := promptLine(sc, out, fmt.Sprintf("Select [1-%d] or press Enter for Blank: ", len(templates)))

	// Resolve the chosen template content.
	chosen, templateName := resolveTemplateChoice(line, templates, wsRoot)

	// Open in $EDITOR only when running interactively.
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	if interactive {
		edited, err := editTemplateRaw(chosen, out)
		if err != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Editor: %v — saving template as-is.\n", err)
		} else {
			chosen = edited
		}
	}

	// Build and save the workspace_profile MemoryDoc.
	id := GenerateMemoryID(MemoryTypeWorkspaceProfile)
	description := templateName + " — " + wsName
	summary := templateBodySummary(chosen)
	if summary == "" {
		summary = description
	}
	tags := []string{
		"workspace_profile",
		"template:" + strings.ToLower(strings.ReplaceAll(templateName, " ", "-")),
		strings.ToLower(wsName),
	}

	ts := time.Now().UTC().Format("2006-01-02 15:04:05")
	doc := NewMemoryDoc(id, MemoryTypeWorkspaceProfile, description, summary, tags)
	doc.FountainBody = buildProfileFountainBody(ts, templateName, chosen)

	if err := store.Save(doc, embedder); err != nil {
		return fmt.Errorf("save workspace_profile: %w", err)
	}
	fmt.Fprintln(out, green("✓")+" Workspace profile saved.")

	// Auto-extract and save a project_fact.
	projectFact := extractProjectFact(wsRoot)
	if projectFact == "" && interactive {
		projectFact = promptLine(sc, out, "> Brief project description (e.g. language, purpose) — Enter to skip: ")
	} else if projectFact != "" {
		fmt.Fprintf(out, "  Project fact auto-detected: %s\n", projectFact)
	}

	if projectFact != "" {
		pfID := GenerateMemoryID(MemoryTypeProjectFact)
		pfDoc := NewMemoryDoc(pfID, MemoryTypeProjectFact, "Project: "+wsName, projectFact, []string{"project_fact", "auto"})
		pfDoc.FountainBody = BuildFountainBody(ts, [][2]string{
			{"HARVEY", "Project fact captured during onboarding."},
			{"SYSTEM", projectFact},
		})
		if err := store.Save(pfDoc, embedder); err != nil {
			fmt.Fprintf(out, "%s Could not save project_fact: %v\n", yellow("  ✗"), err)
		} else {
			fmt.Fprintln(out, green("✓")+" Project fact saved.")
		}
	}

	fmt.Fprintln(out, dim("  Use /profile show to review or /profile update to edit."))
	return nil
}

// resolveTemplateChoice selects template content based on the user's line input.
// Returns the raw template bytes and the display name of the chosen template.
func resolveTemplateChoice(line string, templates []TemplateEntry, wsRoot string) ([]byte, string) {
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err == nil && n >= 1 && n <= len(templates) {
		entry := templates[n-1]
		var data []byte
		if entry.Source == "workspace" && wsRoot != "" {
			localPath := filepath.Join(wsRoot, "agents", "templates", "profiles", entry.File)
			data, err = os.ReadFile(localPath)
		}
		if data == nil || err != nil {
			data, err = LoadTemplate(entry.File)
		}
		if err == nil {
			return data, entry.Name
		}
	}
	// Invalid input or load error → blank.
	data, _ := LoadTemplate("blank")
	return data, "Blank"
}

// editTemplateRaw writes raw template bytes to a temp file, opens $EDITOR,
// and returns the saved result. The caller is responsible for falling back
// to the original content on error.
func editTemplateRaw(content []byte, out io.Writer) ([]byte, error) {
	tmp, err := os.CreateTemp("", "harvey-profile-*.fountain")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	editor := findEditor()
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor %q: %w", editor, err)
	}
	return os.ReadFile(tmpPath)
}

// buildProfileFountainBody wraps edited template content in a Fountain scene.
// The NOTE: block is stripped because it is template metadata, not profile content.
func buildProfileFountainBody(ts, templateName string, content []byte) string {
	body := stripTemplateNoteField(content)
	return fmt.Sprintf(
		"FADE IN:\n\nINT. WORKSPACE PROFILE - %s\n\nHARVEY\nWorkspace profile from template: %s.\n\nUSER\n%s\n\nTHE END.\n",
		ts, templateName, strings.TrimSpace(body),
	)
}

// stripTemplateNoteField returns the template content with the NOTE: block
// removed. All other fields and their content are kept unchanged.
func stripTemplateNoteField(content []byte) string {
	lines := strings.Split(string(content), "\n")
	var result []string
	inNote := false
	for _, line := range lines {
		if strings.HasPrefix(line, "NOTE:") {
			inNote = true
			continue
		}
		if inNote {
			if isTemplateField(line) {
				inNote = false
				result = append(result, line)
			}
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// templateBodySummary extracts a one-line summary from the ROLE: field of a
// template document. Returns "" when no ROLE: field is present.
func templateBodySummary(content []byte) string {
	lines := strings.Split(string(content), "\n")
	inRole := false
	for _, line := range lines {
		if strings.HasPrefix(line, "ROLE:") {
			inRole = true
			rest := strings.TrimSpace(strings.TrimPrefix(line, "ROLE:"))
			if rest != "" {
				return rest
			}
			continue
		}
		if inRole {
			if isTemplateField(line) {
				break
			}
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// promptLine writes prompt to out, then reads and trims one line from r.
func promptLine(r *bufio.Reader, out io.Writer, prompt string) string {
	fmt.Fprint(out, prompt)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

/** extractProjectFact scans the workspace root for known project manifests and
 * returns a concise human-readable summary. Checks codemeta.json, go.mod,
 * package.json, and .git/config in order. Returns "" when nothing is found.
 *
 * Parameters:
 *   wsRoot (string) — absolute path to the workspace root.
 *
 * Returns:
 *   string — one-line project summary, or "" if undetectable.
 *
 * Example:
 *   fact := extractProjectFact("/home/user/harvey")
 *   // "harvey: terminal coding agent; language: Go; status: active"
 */
func extractProjectFact(wsRoot string) string {
	if wsRoot == "" {
		return ""
	}
	if s := extractCodemeta(wsRoot); s != "" {
		return s
	}
	if s := extractGoMod(wsRoot); s != "" {
		return s
	}
	if s := extractPackageJSON(wsRoot); s != "" {
		return s
	}
	if s := extractGitOrigin(wsRoot); s != "" {
		return s
	}
	return ""
}

// codemetaDoc mirrors the relevant fields from a codemeta.json file.
type codemetaDoc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Lang        json.RawMessage `json:"programmingLanguage"`
	Status      string          `json:"developmentStatus"`
}

func extractCodemeta(wsRoot string) string {
	data, err := os.ReadFile(filepath.Join(wsRoot, "codemeta.json"))
	if err != nil {
		return ""
	}
	var cm codemetaDoc
	if err := json.Unmarshal(data, &cm); err != nil {
		return ""
	}
	var parts []string
	if cm.Name != "" {
		p := cm.Name
		if cm.Description != "" {
			p += ": " + cm.Description
		}
		parts = append(parts, p)
	}
	if lang := parseLang(cm.Lang); lang != "" {
		parts = append(parts, "language: "+lang)
	}
	if cm.Status != "" {
		parts = append(parts, "status: "+cm.Status)
	}
	return strings.Join(parts, "; ")
}

// parseLang decodes the codemeta programmingLanguage field, which may be a
// string, an array of strings, or an array of {"name": "..."} objects.
func parseLang(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil && len(ss) > 0 {
		return strings.Join(ss, ", ")
	}
	var objs []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &objs) == nil {
		var names []string
		for _, o := range objs {
			if o.Name != "" {
				names = append(names, o.Name)
			}
		}
		return strings.Join(names, ", ")
	}
	return ""
}

func extractGoMod(wsRoot string) string {
	data, err := os.ReadFile(filepath.Join(wsRoot, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			mod := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			return "Go module: " + mod
		}
	}
	return ""
}

func extractPackageJSON(wsRoot string) string {
	data, err := os.ReadFile(filepath.Join(wsRoot, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || pkg.Name == "" {
		return ""
	}
	if pkg.Description != "" {
		return pkg.Name + ": " + pkg.Description
	}
	return pkg.Name
}

func extractGitOrigin(wsRoot string) string {
	data, err := os.ReadFile(filepath.Join(wsRoot, ".git", "config"))
	if err != nil {
		return ""
	}
	inOrigin := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == `[remote "origin"]` {
			inOrigin = true
			continue
		}
		if inOrigin {
			if strings.HasPrefix(trimmed, "[") {
				break
			}
			if strings.HasPrefix(trimmed, "url") {
				if idx := strings.Index(trimmed, "="); idx >= 0 {
					return "Git repository: " + strings.TrimSpace(trimmed[idx+1:])
				}
			}
		}
	}
	return ""
}
