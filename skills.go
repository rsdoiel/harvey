package harvey

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillSource labels the discovery scope that produced a skill entry.
type SkillSource string

const (
	SkillSourceUser    SkillSource = "user"
	SkillSourceProject SkillSource = "project"
)

/** SkillMeta holds the parsed contents of a single SKILL.md file.
 *
 * Fields:
 *   Name          (string)            — skill identifier from frontmatter (or inferred from dir).
 *   Description   (string)            — when/why to use this skill; injected into the catalog.
 *   License       (string)            — optional license identifier.
 *   Compatibility (string)            — optional environment requirements.
 *   AllowedTools  (string)            — optional space-separated tool allowlist (experimental).
 *   Metadata      (map[string]string) — arbitrary key-value metadata from frontmatter.
 *   Path          (string)            — absolute path to the SKILL.md file.
 *   Body          (string)            — markdown body after the YAML frontmatter.
 *   Source        (SkillSource)       — "user" or "project" (for precedence tracking).
 *
 * Example:
 *   meta, err := ParseSkillFile("/home/user/.harvey/skills/pdf-processing/SKILL.md")
 *   fmt.Println(meta.Name, meta.Description)
 */
type SkillMeta struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	Metadata      map[string]string
	Path          string
	Body          string
	Source        SkillSource
}

/** SkillCatalog maps skill names to their parsed metadata.
 * Project-scope entries override user-scope entries when names collide.
 *
 * Example:
 *   cat := ScanSkills("/home/user/myproject")
 *   skill, ok := cat["pdf-processing"]
 */
type SkillCatalog map[string]*SkillMeta

/** SkillSearchDir pairs a filesystem path with its discovery scope.
 *
 * Fields:
 *   Path   (string)      — directory to scan for skill subdirectories.
 *   Source (SkillSource) — "user" or "project".
 *
 * Example:
 *   dirs := []SkillSearchDir{
 *       {Path: "/tmp/skills", Source: SkillSourceProject},
 *   }
 *   cat := ScanSkillDirs(dirs)
 */
type SkillSearchDir struct {
	Path   string
	Source SkillSource
}

/** ParseSkillFile reads and parses a SKILL.md file.
 *
 * The file must begin with YAML frontmatter (--- ... ---) containing at
 * minimum a non-empty description field. If the name field is absent the
 * parent directory name is used instead. Malformed frontmatter (unparseable
 * YAML, missing description) returns a non-nil error and the skill is skipped
 * by callers.
 *
 * Parameters:
 *   path (string) — absolute path to a SKILL.md file.
 *
 * Returns:
 *   *SkillMeta — parsed skill; nil on error.
 *   error      — parse failure; nil on success.
 *
 * Example:
 *   meta, err := ParseSkillFile("/home/user/.harvey/skills/my-skill/SKILL.md")
 *   if err != nil { log.Println("skip:", err) }
 */
func ParseSkillFile(path string) (*SkillMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skill: read %s: %w", path, err)
	}

	fm, body, err := extractFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("skill: %s: %w", path, err)
	}

	fields, metadata := parseSimpleYAML(fm)

	description := fields["description"]
	if description == "" {
		return nil, fmt.Errorf("skill: %s: description is required", path)
	}

	name := fields["name"]
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}

	return &SkillMeta{
		Name:          name,
		Description:   description,
		License:       fields["license"],
		Compatibility: fields["compatibility"],
		AllowedTools:  fields["allowed-tools"],
		Metadata:      metadata,
		Path:          path,
		Body:          body,
	}, nil
}

/** ScanSkillDirs discovers skills from an explicit list of search directories.
 * Directories are scanned in order; later entries override earlier ones on
 * name collision, so callers should place higher-priority paths last.
 *
 * Parameters:
 *   dirs ([]SkillSearchDir) — ordered list of directories to scan.
 *
 * Returns:
 *   SkillCatalog — all discovered skills, keyed by name.
 *
 * Example:
 *   dirs := []SkillSearchDir{
 *       {Path: "/home/user/.harvey/skills", Source: SkillSourceUser},
 *       {Path: "/proj/.harvey/skills",      Source: SkillSourceProject},
 *   }
 *   cat := ScanSkillDirs(dirs)
 */
func ScanSkillDirs(dirs []SkillSearchDir) SkillCatalog {
	cat := make(SkillCatalog)
	for _, sd := range dirs {
		scanOneSkillDir(sd.Path, sd.Source, cat)
	}
	return cat
}

/** ScanSkills discovers skills from the standard paths relative to workDir
 * and the user home directory. Project-scope skills override user-scope
 * skills when names collide.
 *
 * Paths scanned (in precedence order, lowest first):
 *   ~/.harvey/skills/          user, Harvey-native
 *   ~/.agents/skills/          user, cross-client
 *   <workDir>/.harvey/skills/  project, Harvey-native
 *   <workDir>/.agents/skills/  project, cross-client
 *
 * Parameters:
 *   workDir (string) — absolute path to the Harvey workspace root.
 *
 * Returns:
 *   SkillCatalog — all discovered skills.
 *
 * Example:
 *   cat := ScanSkills("/home/user/myproject")
 *   fmt.Println(len(cat), "skills found")
 */
func ScanSkills(workDir string) SkillCatalog {
	return ScanSkillDirs(standardSkillDirs(workDir))
}

// standardSkillDirs returns the ordered list of search dirs for ScanSkills.
// User paths come first so project paths can override them.
func standardSkillDirs(workDir string) []SkillSearchDir {
	home, _ := os.UserHomeDir()
	var dirs []SkillSearchDir
	if home != "" {
		dirs = append(dirs,
			SkillSearchDir{filepath.Join(home, ".harvey", "skills"), SkillSourceUser},
			SkillSearchDir{filepath.Join(home, ".agents", "skills"), SkillSourceUser},
		)
	}
	dirs = append(dirs,
		SkillSearchDir{filepath.Join(workDir, ".harvey", "skills"), SkillSourceProject},
		SkillSearchDir{filepath.Join(workDir, ".agents", "skills"), SkillSourceProject},
	)
	return dirs
}

// scanOneSkillDir scans dir for skill subdirectories (each containing SKILL.md)
// and adds valid skills to cat. Hidden directories are skipped.
// A later call always overwrites an earlier one on name collision.
func scanOneSkillDir(dir string, source SkillSource, cat SkillCatalog) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		skillMD := filepath.Join(dir, e.Name(), "SKILL.md")
		meta, err := ParseSkillFile(skillMD)
		if err != nil {
			continue
		}
		meta.Source = source
		cat[meta.Name] = meta
	}
}

/** CatalogSystemPromptBlock returns the XML catalog block and load
 * instructions to append to Harvey's system prompt. Returns an empty string
 * when cat is empty so callers need not special-case the no-skills case.
 *
 * Parameters:
 *   cat (SkillCatalog) — skills to include; may be nil or empty.
 *
 * Returns:
 *   string — system-prompt fragment; "" if no skills.
 *
 * Example:
 *   block := CatalogSystemPromptBlock(cat)
 *   if block != "" {
 *       agent.AddMessage("system", block)
 *   }
 */
func CatalogSystemPromptBlock(cat SkillCatalog) string {
	if len(cat) == 0 {
		return ""
	}

	names := make([]string, 0, len(cat))
	for n := range cat {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("<available_skills>\n")
	for _, n := range names {
		s := cat[n]
		sb.WriteString("  <skill>\n")
		fmt.Fprintf(&sb, "    <name>%s</name>\n", xmlEscape(s.Name))
		fmt.Fprintf(&sb, "    <description>%s</description>\n", xmlEscape(s.Description))
		fmt.Fprintf(&sb, "    <location>%s</location>\n", xmlEscape(s.Path))
		sb.WriteString("  </skill>\n")
	}
	sb.WriteString("</available_skills>\n\n")
	sb.WriteString("When a task matches a skill's description, the user will type " +
		"`/skill load <name>` to inject the full instructions into context. " +
		"Once loaded, follow the skill's instructions for that task.\n")

	return sb.String()
}

// ─── internal parsing helpers ────────────────────────────────────────────────

// extractFrontmatter splits a SKILL.md string into its YAML frontmatter block
// and the markdown body. Content must begin with "---\n"; the next "---" line
// closes the frontmatter.
func extractFrontmatter(content string) (frontmatter, body string, err error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", content, fmt.Errorf("no YAML frontmatter")
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			fm := strings.Join(lines[1:i], "\n")
			bd := strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			return fm, bd, nil
		}
	}
	return "", content, fmt.Errorf("frontmatter not closed")
}

// parseSimpleYAML parses a flat YAML block of the form used in SKILL.md
// frontmatter. It handles:
//   - simple "key: value" pairs (including unquoted values containing colons)
//   - single- and double-quoted values
//   - a one-level "metadata:" mapping (indented key-value pairs)
//
// Returns the top-level fields and any metadata sub-keys.
func parseSimpleYAML(text string) (fields map[string]string, metadata map[string]string) {
	fields = make(map[string]string)
	metadata = make(map[string]string)

	inMetadata := false
	for _, rawLine := range strings.Split(text, "\n") {
		isIndented := len(rawLine) > 0 && (rawLine[0] == ' ' || rawLine[0] == '\t')
		line := strings.TrimRight(rawLine, "\r")

		if isIndented {
			if inMetadata {
				k, v := splitYAMLKV(strings.TrimSpace(line))
				if k != "" {
					metadata[k] = v
				}
			}
			continue
		}

		inMetadata = false
		if strings.TrimSpace(line) == "" {
			continue
		}

		k, v := splitYAMLKV(line)
		if k == "" {
			continue
		}
		if k == "metadata" && v == "" {
			inMetadata = true
			continue
		}
		fields[k] = v
	}
	return
}

// splitYAMLKV splits "key: value" on the first colon. Surrounding quotes
// (single or double) are stripped from the value. Returns ("", "") when no
// colon is present.
func splitYAMLKV(line string) (key, val string) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", ""
	}
	key = strings.TrimSpace(line[:idx])
	val = strings.TrimSpace(line[idx+1:])
	if len(val) >= 2 &&
		((val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '\'' && val[len(val)-1] == '\'')) {
		val = val[1 : len(val)-1]
	}
	return key, val
}

// xmlEscape replaces &, <, and > with XML entities.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
