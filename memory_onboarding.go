package harvey

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
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

/** RunOnboarding prompts the user for workspace identity and project facts,
 * then writes a workspace_profile memory and, when detectable, a project_fact
 * memory. The flow is skipped automatically on subsequent starts because
 * NeedsOnboarding returns false once the profile exists.
 *
 * Parameters:
 *   a        (*Agent)      — the Harvey agent (used for workspace root).
 *   store    (*MemoryStore) — the open memory store for the workspace.
 *   embedder (Embedder)    — used to compute embedding vectors; may be nil.
 *   out      (io.Writer)   — output writer for prompts and status.
 *   in       (io.Reader)   — input reader for user responses.
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
	sc := bufio.NewReader(in)
	ts := time.Now().UTC().Format("2006-01-02 15:04:05")

	fmt.Fprintln(out, "\nHarvey: I don't have a workspace profile yet. A few quick questions:")
	fmt.Fprintln(out)

	name := promptLine(sc, out, "> What should I call you in this workspace? ")
	role := promptLine(sc, out, "> What is your role here? (developer / researcher / writer / other) ")
	langs := promptLine(sc, out, "> Primary language(s) or tools? e.g. Go, TypeScript, Python ")
	notes := promptLine(sc, out, "> Anything else I should know about this project? (Enter to skip) ")

	// Build description and summary from answers.
	var parts []string
	if name != "" {
		parts = append(parts, name)
	}
	if role != "" {
		parts = append(parts, role)
	}
	if langs != "" {
		parts = append(parts, langs)
	}
	description := strings.Join(parts, " — ")
	if description == "" {
		description = "workspace profile"
	}
	summary := description
	if notes != "" {
		summary += ". " + notes
	}

	tags := []string{"workspace_profile", "onboarding"}
	if name != "" {
		if fields := strings.Fields(name); len(fields) > 0 {
			tags = append(tags, strings.ToLower(fields[0]))
		}
	}

	id := GenerateMemoryID(MemoryTypeWorkspaceProfile)
	profileDoc := NewMemoryDoc(id, MemoryTypeWorkspaceProfile, description, summary, tags)
	profileDoc.FountainBody = BuildFountainBody(ts, [][2]string{
		{"HARVEY", "Workspace profile captured during onboarding."},
		{"USER", fmt.Sprintf("Name: %s. Role: %s. Languages: %s. Notes: %s.", name, role, langs, notes)},
	})
	if err := store.Save(profileDoc, embedder); err != nil {
		return fmt.Errorf("save workspace_profile: %w", err)
	}
	fmt.Fprintln(out, green("✓")+" Workspace profile saved.")

	// Auto-extract project_fact or ask the user.
	wsRoot := ""
	if a.Workspace != nil {
		wsRoot = a.Workspace.Root
	}
	projectFact := extractProjectFact(wsRoot)
	if projectFact == "" {
		projectFact = promptLine(sc, out, "> Brief project description (e.g. language, purpose) — Enter to skip: ")
	} else {
		fmt.Fprintf(out, "  Project fact auto-detected: %s\n", projectFact)
	}

	if projectFact != "" {
		pfID := GenerateMemoryID(MemoryTypeProjectFact)
		wsName := filepath.Base(wsRoot)
		pfDesc := "Project: " + wsName
		pfDoc := NewMemoryDoc(pfID, MemoryTypeProjectFact, pfDesc, projectFact, []string{"project_fact", "auto"})
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

	fmt.Fprintln(out, dim("  Use /memory profile show to review or /memory profile update to edit."))
	return nil
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
