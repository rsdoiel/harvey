package harvey

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// validSkillNameRE matches names that are lowercase letters, digits, and hyphens only.
var validSkillNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// SkillSource labels the discovery scope that produced a skill entry.
type SkillSource string

const (
	SkillSourceUser    SkillSource = "user"
	SkillSourceProject SkillSource = "project"
)

/** SkillVariable holds metadata for one input variable declared in a skill's
 * SKILL.md frontmatter variables: block.
 *
 * Fields:
 *   Name        (string) — variable name as declared (e.g. "EDIR").
 *   Description (string) — human-readable description of what the variable controls.
 *   Example     (string) — example value shown in help text.
 *
 * Example:
 *   v := SkillVariable{Name: "EDIR", Description: "experiment directory", Example: "my_exp"}
 */
type SkillVariable struct {
	Name        string
	Description string
	Example     string
}

/** SkillMeta holds the parsed contents of a single SKILL.md file.
 *
 * Fields:
 *   Name          (string)            — skill identifier from frontmatter (or inferred from dir).
 *   Description   (string)            — when/why to use this skill; injected into the catalog.
 *   License       (string)            — optional license identifier.
 *   Compatibility (string)            — optional environment requirements.
 *   AllowedTools  (string)            — optional space-separated tool allowlist (experimental).
 *   Trigger       (string)            — optional regex or keywords for auto-dispatch.
 *   Variables     ([]SkillVariable)   — input variables declared in frontmatter variables: block.
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
	Trigger       string
	Variables     []SkillVariable
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
		Trigger:       fields["trigger"],
		Variables:     parseVariablesBlock(fm),
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

/** LooksLikeSkillQuery reports whether the user's input looks like a natural-
 * language question about available skills so the REPL can intercept it and
 * answer directly from the catalog rather than passing it to the LLM.
 *
 * Parameters:
 *   s (string) — the raw user input.
 *
 * Returns:
 *   bool — true when the input matches a known skill-query pattern.
 *
 * Example:
 *   if LooksLikeSkillQuery("what skills do you know?") {
 *       cmdSkill(a, []string{"list"}, out)
 *   }
 */
func LooksLikeSkillQuery(s string) bool {
	lower := strings.ToLower(s)
	phrases := []string{
		"what skills", "which skills", "list skills", "show skills",
		"what can you do", "skills do you", "skills you know",
		"available skills", "your skills", "do you have any skills",
	}
	for _, p := range phrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
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
//   - YAML literal block scalars (key: |) — indented lines are joined with spaces
//   - a one-level "metadata:" mapping (indented key-value pairs)
//
// Returns the top-level fields and any metadata sub-keys.
func parseSimpleYAML(text string) (fields map[string]string, metadata map[string]string) {
	fields = make(map[string]string)
	metadata = make(map[string]string)

	inMetadata := false
	blockKey := ""
	var blockLines []string

	flushBlock := func() {
		if blockKey != "" && len(blockLines) > 0 {
			fields[blockKey] = strings.Join(blockLines, " ")
		}
		blockKey = ""
		blockLines = nil
	}

	for _, rawLine := range strings.Split(text, "\n") {
		isIndented := len(rawLine) > 0 && (rawLine[0] == ' ' || rawLine[0] == '\t')
		line := strings.TrimRight(rawLine, "\r")

		if isIndented {
			if blockKey != "" {
				blockLines = append(blockLines, strings.TrimSpace(line))
				continue
			}
			if inMetadata {
				k, v := splitYAMLKV(strings.TrimSpace(line))
				if k != "" {
					metadata[k] = v
				}
			}
			continue
		}

		flushBlock()
		inMetadata = false
		if strings.TrimSpace(line) == "" {
			continue
		}

		k, v := splitYAMLKV(line)
		if k == "" {
			continue
		}
		if v == "|" {
			blockKey = k
			continue
		}
		if k == "metadata" && v == "" {
			inMetadata = true
			continue
		}
		fields[k] = v
	}
	flushBlock()
	return
}

/** parseVariablesBlock extracts declared input variables from a SKILL.md
 * frontmatter string. It finds the "variables:" block and collects each
 * two-space-indented key as a variable name, reading its "description" and
 * "example" from four-space-indented sub-keys.
 *
 * Parameters:
 *   frontmatter (string) — raw YAML frontmatter text (between --- delimiters).
 *
 * Returns:
 *   []SkillVariable — declared variables in declaration order; nil if none.
 *
 * Example:
 *   vars := parseVariablesBlock(fm)
 *   for _, v := range vars { fmt.Println(v.Name, v.Description) }
 */
func parseVariablesBlock(frontmatter string) []SkillVariable {
	var vars []SkillVariable
	lines := strings.Split(frontmatter, "\n")

	inVars := false
	var cur *SkillVariable

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		// Unindented line: enter or exit the variables: block.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			if trimmed == "variables:" {
				inVars = true
				continue
			}
			if inVars {
				break
			}
			continue
		}

		if !inVars {
			continue
		}

		depth := len(line) - len(strings.TrimLeft(line, " \t"))

		if depth == 2 {
			// Variable name line: "  VARNAME:"
			if cur != nil {
				vars = append(vars, *cur)
			}
			cur = &SkillVariable{Name: strings.TrimSuffix(trimmed, ":")}
		} else if depth >= 4 && cur != nil {
			// Variable property: "    key: value"
			k, v := splitYAMLKV(trimmed)
			switch k {
			case "description":
				cur.Description = v
			case "example":
				cur.Example = v
			}
		}
	}
	if cur != nil {
		vars = append(vars, *cur)
	}
	return vars
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

/** ValidSkillName reports whether name is a valid skill identifier:
 * lowercase letters, digits, and hyphens only, non-empty.
 *
 * Parameters:
 *   name (string) — candidate skill name.
 *
 * Returns:
 *   bool — true when name matches ^[a-z0-9-]+$.
 *
 * Example:
 *   ValidSkillName("go-review")  // true
 *   ValidSkillName("My Skill")   // false
 */
func ValidSkillName(name string) bool {
	return validSkillNameRE.MatchString(name)
}

/** CompiledBashPath returns the absolute path to the compiled bash script
 * for the skill whose SKILL.md lives at skillPath.
 *
 * Parameters:
 *   skillPath (string) — absolute path to a SKILL.md file.
 *
 * Returns:
 *   string — absolute path to scripts/compiled.bash sibling.
 *
 * Example:
 *   p := CompiledBashPath("/proj/.agents/skills/my-skill/SKILL.md")
 *   // returns "/proj/.agents/skills/my-skill/scripts/compiled.bash"
 */
func CompiledBashPath(skillPath string) string {
	return filepath.Join(filepath.Dir(skillPath), "scripts", "compiled.bash")
}

/** CompiledPS1Path returns the absolute path to the compiled PowerShell script
 * for the skill whose SKILL.md lives at skillPath.
 *
 * Parameters:
 *   skillPath (string) — absolute path to a SKILL.md file.
 *
 * Returns:
 *   string — absolute path to scripts/compiled.ps1 sibling.
 *
 * Example:
 *   p := CompiledPS1Path("/proj/.agents/skills/my-skill/SKILL.md")
 *   // returns "/proj/.agents/skills/my-skill/scripts/compiled.ps1"
 */
func CompiledPS1Path(skillPath string) string {
	return filepath.Join(filepath.Dir(skillPath), "scripts", "compiled.ps1")
}

/** IsStale reports whether SKILL.md is newer than the compiled scripts,
 * meaning the scripts need recompilation.
 *
 * Returns (true, nil) when either compiled script does not exist or when
 * SKILL.md has a later modification time than either script.
 * Returns (false, nil) when both scripts exist and are at least as new as SKILL.md.
 * Returns (false, error) when SKILL.md itself cannot be stat'd.
 *
 * Parameters:
 *   skill (*SkillMeta) — skill to check; skill.Path must be set.
 *
 * Returns:
 *   stale (bool)  — true when recompilation is needed.
 *   err   (error) — non-nil only when SKILL.md itself is unreadable.
 *
 * Example:
 *   stale, err := IsStale(skill)
 *   if err != nil { log.Fatal(err) }
 *   if stale { fmt.Println("needs recompile") }
 */
func IsStale(skill *SkillMeta) (stale bool, err error) {
	mdInfo, err := os.Stat(skill.Path)
	if err != nil {
		return false, fmt.Errorf("skill stale check: %w", err)
	}
	mdMod := mdInfo.ModTime()

	bashInfo, bashErr := os.Stat(CompiledBashPath(skill.Path))
	ps1Info, ps1Err := os.Stat(CompiledPS1Path(skill.Path))

	if os.IsNotExist(bashErr) || os.IsNotExist(ps1Err) {
		return true, nil
	}
	if bashErr != nil || ps1Err != nil {
		return true, nil
	}

	return mdMod.After(bashInfo.ModTime()) || mdMod.After(ps1Info.ModTime()), nil
}

// xmlEscape replaces &, <, and > with XML entities.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
