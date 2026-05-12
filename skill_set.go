// Package harvey — skill_set.go implements the /skill-set command, which
// lets the user load, inspect, and manage named YAML bundles of skills stored
// in agents/skill-sets/.
package harvey

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// skillSetYAML is the raw unmarshaled form of a skill-set YAML file.
type skillSetYAML struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Skills      []string          `yaml:"skills"`
	Metadata    map[string]string `yaml:"metadata"`
}

/** SkillSetMeta holds the parsed contents of a skill-set YAML file.
 *
 * Fields:
 *   Name        (string)            — identifier used in /skill-set load.
 *   Description (string)            — human-readable purpose of the bundle.
 *   Skills      ([]string)          — ordered list of skill names to load.
 *   Metadata    (map[string]string) — arbitrary key-value pairs (version, author…).
 *   Path        (string)            — absolute path to the source YAML file.
 *
 * Example:
 *   ss, err := ParseSkillSet("/path/to/agents/skill-sets/go-dev.yaml")
 *   fmt.Println(ss.Name, ss.Skills)
 */
type SkillSetMeta struct {
	Name        string
	Description string
	Skills      []string
	Metadata    map[string]string
	Path        string
}

/** ParseSkillSet reads and parses a skill-set YAML file at path.
 *
 * Parameters:
 *   path (string) — absolute path to the .yaml file.
 *
 * Returns:
 *   *SkillSetMeta — parsed skill-set.
 *   error         — if the file cannot be read or parsed.
 *
 * Example:
 *   ss, err := ParseSkillSet("/agents/skill-sets/fountain.yaml")
 */
func ParseSkillSet(path string) (*SkillSetMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw skillSetYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse skill-set %s: %w", filepath.Base(path), err)
	}
	name := raw.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}
	return &SkillSetMeta{
		Name:        name,
		Description: strings.TrimSpace(raw.Description),
		Skills:      raw.Skills,
		Metadata:    raw.Metadata,
		Path:        path,
	}, nil
}

/** skillSetDir returns the absolute path of the agents/skill-sets/ directory
 * for the given workspace.
 *
 * Parameters:
 *   ws (*Workspace) — the active workspace.
 *
 * Returns:
 *   string — absolute path to agents/skill-sets/.
 *
 * Example:
 *   dir := skillSetDir(ws)
 */
func skillSetDir(ws *Workspace) string {
	return filepath.Join(ws.Root, "agents", "skill-sets")
}

/** listSkillSetNames returns the base names (without .yaml extension) of all
 * skill-set YAML files found in agents/skill-sets/.
 *
 * Parameters:
 *   ws (*Workspace) — the active workspace.
 *
 * Returns:
 *   []string — sorted list of skill-set names; empty if the directory is absent.
 *   error    — only non-nil on unexpected I/O errors (missing dir returns nil, nil).
 *
 * Example:
 *   names, err := listSkillSetNames(ws)
 */
func listSkillSetNames(ws *Workspace) ([]string, error) {
	dir := skillSetDir(ws)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	return names, nil
}

/** findSkillSet searches agents/skill-sets/ for a YAML file matching name
 * (with or without .yaml extension) and parses it.
 *
 * Parameters:
 *   ws   (*Workspace) — the active workspace.
 *   name (string)     — skill-set name or filename.
 *
 * Returns:
 *   *SkillSetMeta — parsed skill-set.
 *   error         — if not found or unparseable.
 *
 * Example:
 *   ss, err := findSkillSet(ws, "fountain")
 */
func findSkillSet(ws *Workspace, name string) (*SkillSetMeta, error) {
	clean := strings.TrimSuffix(name, ".yaml")
	path := filepath.Join(skillSetDir(ws), clean+".yaml")
	ss, err := ParseSkillSet(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("skill-set %q not found (looked in %s)", clean, skillSetDir(ws))
	}
	return ss, err
}

/** validateSkillSet checks that every skill name in ss.Skills exists in cat
 * and that there are no duplicates.
 *
 * Parameters:
 *   ss  (*SkillSetMeta) — skill-set to validate.
 *   cat (SkillCatalog)  — discovered skills to validate against.
 *
 * Returns:
 *   error — describes all validation failures (missing skills, duplicates).
 *
 * Example:
 *   if err := validateSkillSet(ss, a.Skills); err != nil { ... }
 */
func validateSkillSet(ss *SkillSetMeta, cat SkillCatalog) error {
	if len(ss.Skills) == 0 {
		return fmt.Errorf("skill-set %q has no skills listed", ss.Name)
	}
	seen := make(map[string]bool)
	var errs []string
	for _, name := range ss.Skills {
		if seen[name] {
			errs = append(errs, fmt.Sprintf("duplicate skill %q", name))
			continue
		}
		seen[name] = true
		if _, ok := cat[name]; !ok {
			errs = append(errs, fmt.Sprintf("skill %q not found in agents/skills/", name))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("skill-set %q validation failed:\n  %s", ss.Name, strings.Join(errs, "\n  "))
	}
	return nil
}
