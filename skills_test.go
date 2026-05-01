package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// writeSkillFile creates <dir>/<name>/SKILL.md with the given content.
// Returns the path to the SKILL.md file.
func writeSkillFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", skillDir, err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

// ─── extractFrontmatter ──────────────────────────────────────────────────────

func TestExtractFrontmatter_basic(t *testing.T) {
	content := "---\nname: test\n---\n\n# Body"
	fm, body, err := extractFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fm, "name: test") {
		t.Errorf("frontmatter %q missing expected field", fm)
	}
	if !strings.Contains(body, "# Body") {
		t.Errorf("body %q missing expected content", body)
	}
}

func TestExtractFrontmatter_noMarker(t *testing.T) {
	_, _, err := extractFrontmatter("just some text")
	if err == nil {
		t.Error("expected error for missing frontmatter, got nil")
	}
}

func TestExtractFrontmatter_unclosed(t *testing.T) {
	_, _, err := extractFrontmatter("---\nname: test\n")
	if err == nil {
		t.Error("expected error for unclosed frontmatter, got nil")
	}
}

func TestExtractFrontmatter_emptyBody(t *testing.T) {
	_, body, err := extractFrontmatter("---\nname: test\n---\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

// ─── parseSimpleYAML ─────────────────────────────────────────────────────────

func TestParseSimpleYAML_basic(t *testing.T) {
	fields, _ := parseSimpleYAML("name: my-skill\ndescription: Does things.")
	if fields["name"] != "my-skill" {
		t.Errorf("name = %q, want %q", fields["name"], "my-skill")
	}
	if fields["description"] != "Does things." {
		t.Errorf("description = %q", fields["description"])
	}
}

func TestParseSimpleYAML_colonInValue(t *testing.T) {
	// Cross-client compat: unquoted colon in value must not break parsing.
	fields, _ := parseSimpleYAML("description: Use when: user asks about PDFs.")
	want := "Use when: user asks about PDFs."
	if fields["description"] != want {
		t.Errorf("description = %q, want %q", fields["description"], want)
	}
}

func TestParseSimpleYAML_quotedValue(t *testing.T) {
	fields, _ := parseSimpleYAML(`version: "1.0"`)
	if fields["version"] != "1.0" {
		t.Errorf("version = %q, want %q", fields["version"], "1.0")
	}
}

func TestParseSimpleYAML_metadata(t *testing.T) {
	text := "name: test\nmetadata:\n  author: alice\n  version: \"2.0\"\n"
	fields, meta := parseSimpleYAML(text)
	if fields["name"] != "test" {
		t.Errorf("name = %q", fields["name"])
	}
	if meta["author"] != "alice" {
		t.Errorf("metadata.author = %q", meta["author"])
	}
	if meta["version"] != "2.0" {
		t.Errorf("metadata.version = %q", meta["version"])
	}
}

// ─── ParseSkillFile ───────────────────────────────────────────────────────────

func TestParseSkillFile_minimal(t *testing.T) {
	dir := t.TempDir()
	path := writeSkillFile(t, dir, "my-skill", "---\nname: my-skill\ndescription: Does useful things.\n---\n")

	meta, err := ParseSkillFile(path)
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	if meta.Name != "my-skill" {
		t.Errorf("Name = %q, want %q", meta.Name, "my-skill")
	}
	if meta.Description != "Does useful things." {
		t.Errorf("Description = %q", meta.Description)
	}
	if meta.Path != path {
		t.Errorf("Path = %q, want %q", meta.Path, path)
	}
}

func TestParseSkillFile_fullFields(t *testing.T) {
	content := `---
name: pdf-processing
description: Extract PDF text, fill forms, merge files. Use when handling PDFs.
license: Apache-2.0
compatibility: Requires Python 3.10+
allowed-tools: Bash Read
metadata:
  author: example-org
  version: "1.0"
---

# PDF Processing

Step 1: do the thing.
`
	dir := t.TempDir()
	path := writeSkillFile(t, dir, "pdf-processing", content)

	meta, err := ParseSkillFile(path)
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	if meta.License != "Apache-2.0" {
		t.Errorf("License = %q", meta.License)
	}
	if meta.Compatibility != "Requires Python 3.10+" {
		t.Errorf("Compatibility = %q", meta.Compatibility)
	}
	if meta.AllowedTools != "Bash Read" {
		t.Errorf("AllowedTools = %q", meta.AllowedTools)
	}
	if meta.Metadata["author"] != "example-org" {
		t.Errorf("Metadata[author] = %q", meta.Metadata["author"])
	}
	if meta.Metadata["version"] != "1.0" {
		t.Errorf("Metadata[version] = %q", meta.Metadata["version"])
	}
	if !strings.Contains(meta.Body, "# PDF Processing") {
		t.Errorf("Body missing expected content: %q", meta.Body)
	}
}

func TestParseSkillFile_missingDescription(t *testing.T) {
	dir := t.TempDir()
	path := writeSkillFile(t, dir, "no-desc", "---\nname: no-desc\n---\n")
	_, err := ParseSkillFile(path)
	if err == nil {
		t.Error("expected error for missing description, got nil")
	}
}

func TestParseSkillFile_noFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := writeSkillFile(t, dir, "bare", "# Just markdown, no frontmatter\n")
	_, err := ParseSkillFile(path)
	if err == nil {
		t.Error("expected error for missing frontmatter, got nil")
	}
}

func TestParseSkillFile_nameInferredFromDir(t *testing.T) {
	// No name field → inferred from parent directory name.
	dir := t.TempDir()
	path := writeSkillFile(t, dir, "inferred-name", "---\ndescription: Some skill.\n---\n")

	meta, err := ParseSkillFile(path)
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	if meta.Name != "inferred-name" {
		t.Errorf("Name = %q, want %q", meta.Name, "inferred-name")
	}
}

func TestParseSkillFile_notFound(t *testing.T) {
	_, err := ParseSkillFile("/nonexistent/SKILL.md")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// ─── ScanSkillDirs ────────────────────────────────────────────────────────────

func TestScanSkillDirs_empty(t *testing.T) {
	dir := t.TempDir()
	cat := ScanSkillDirs([]SkillSearchDir{
		{Path: dir, Source: SkillSourceUser},
	})
	if len(cat) != 0 {
		t.Errorf("expected 0 skills, got %d", len(cat))
	}
}

func TestScanSkillDirs_singleSkill(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "code-review", "---\nname: code-review\ndescription: Review code for quality issues.\n---\n")

	cat := ScanSkillDirs([]SkillSearchDir{
		{Path: dir, Source: SkillSourceProject},
	})
	if len(cat) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(cat))
	}
	skill, ok := cat["code-review"]
	if !ok {
		t.Fatal("skill 'code-review' not found in catalog")
	}
	if skill.Source != SkillSourceProject {
		t.Errorf("Source = %q, want %q", skill.Source, SkillSourceProject)
	}
}

func TestScanSkillDirs_projectOverridesUser(t *testing.T) {
	userDir := t.TempDir()
	projDir := t.TempDir()

	writeSkillFile(t, userDir, "data-analysis",
		"---\nname: data-analysis\ndescription: User version.\n---\n")
	writeSkillFile(t, projDir, "data-analysis",
		"---\nname: data-analysis\ndescription: Project version.\n---\n")

	cat := ScanSkillDirs([]SkillSearchDir{
		{Path: userDir, Source: SkillSourceUser},    // scanned first, lower priority
		{Path: projDir, Source: SkillSourceProject}, // scanned last, higher priority
	})

	skill, ok := cat["data-analysis"]
	if !ok {
		t.Fatal("skill not found")
	}
	if skill.Description != "Project version." {
		t.Errorf("Description = %q, want project version", skill.Description)
	}
	if skill.Source != SkillSourceProject {
		t.Errorf("Source = %q, want project", skill.Source)
	}
}

func TestScanSkillDirs_multipleSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "alpha", "---\nname: alpha\ndescription: Alpha skill.\n---\n")
	writeSkillFile(t, dir, "beta", "---\nname: beta\ndescription: Beta skill.\n---\n")
	writeSkillFile(t, dir, "gamma", "---\nname: gamma\ndescription: Gamma skill.\n---\n")

	cat := ScanSkillDirs([]SkillSearchDir{{Path: dir, Source: SkillSourceUser}})
	if len(cat) != 3 {
		t.Errorf("expected 3 skills, got %d", len(cat))
	}
}

func TestScanSkillDirs_skipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	// A hidden directory should be ignored even if it contains SKILL.md.
	writeSkillFile(t, dir, ".hidden-skill", "---\nname: hidden\ndescription: Should be ignored.\n---\n")
	writeSkillFile(t, dir, "visible-skill", "---\nname: visible-skill\ndescription: Should be found.\n---\n")

	cat := ScanSkillDirs([]SkillSearchDir{{Path: dir, Source: SkillSourceUser}})
	if _, ok := cat["hidden"]; ok {
		t.Error("hidden skill should not appear in catalog")
	}
	if len(cat) != 1 {
		t.Errorf("expected 1 skill, got %d", len(cat))
	}
}

func TestScanSkillDirs_skipsInvalidSkills(t *testing.T) {
	dir := t.TempDir()
	// Missing description → should be skipped.
	writeSkillFile(t, dir, "bad-skill", "---\nname: bad-skill\n---\n")
	writeSkillFile(t, dir, "good-skill", "---\nname: good-skill\ndescription: Valid.\n---\n")

	cat := ScanSkillDirs([]SkillSearchDir{{Path: dir, Source: SkillSourceUser}})
	if _, ok := cat["bad-skill"]; ok {
		t.Error("invalid skill should not appear in catalog")
	}
	if len(cat) != 1 {
		t.Errorf("expected 1 skill, got %d", len(cat))
	}
}

func TestScanSkillDirs_nonexistentDirIsNoop(t *testing.T) {
	cat := ScanSkillDirs([]SkillSearchDir{
		{Path: "/nonexistent/path/skills", Source: SkillSourceUser},
	})
	if len(cat) != 0 {
		t.Errorf("expected 0 skills from nonexistent dir, got %d", len(cat))
	}
}

// ─── CatalogSystemPromptBlock ────────────────────────────────────────────────

func TestCatalogSystemPromptBlock_empty(t *testing.T) {
	block := CatalogSystemPromptBlock(make(SkillCatalog))
	if block != "" {
		t.Errorf("expected empty block for empty catalog, got %q", block)
	}
}

func TestCatalogSystemPromptBlock_shape(t *testing.T) {
	cat := SkillCatalog{
		"pdf-processing": {
			Name:        "pdf-processing",
			Description: "Extract PDF text.",
			Path:        "/home/user/harvey/skills/pdf-processing/SKILL.md",
		},
		"code-review": {
			Name:        "code-review",
			Description: "Review code quality.",
			Path:        "/proj/.agents/skills/code-review/SKILL.md",
		},
	}

	block := CatalogSystemPromptBlock(cat)

	checks := []string{
		"<available_skills>",
		"</available_skills>",
		"<name>code-review</name>",
		"<name>pdf-processing</name>",
		"<description>Extract PDF text.</description>",
		"<description>Review code quality.</description>",
		"/skill load",
	}
	for _, want := range checks {
		if !strings.Contains(block, want) {
			t.Errorf("catalog block missing %q\n---\n%s", want, block)
		}
	}
}

func TestCatalogSystemPromptBlock_sorted(t *testing.T) {
	cat := SkillCatalog{
		"zebra": {Name: "zebra", Description: "Z skill.", Path: "/z/SKILL.md"},
		"apple": {Name: "apple", Description: "A skill.", Path: "/a/SKILL.md"},
		"mango": {Name: "mango", Description: "M skill.", Path: "/m/SKILL.md"},
	}
	block := CatalogSystemPromptBlock(cat)
	// "apple" must appear before "mango", which must appear before "zebra".
	iA := strings.Index(block, "apple")
	iM := strings.Index(block, "mango")
	iZ := strings.Index(block, "zebra")
	if !(iA < iM && iM < iZ) {
		t.Errorf("skills not sorted: positions apple=%d mango=%d zebra=%d", iA, iM, iZ)
	}
}

func TestCatalogSystemPromptBlock_xmlEscape(t *testing.T) {
	cat := SkillCatalog{
		"test": {
			Name:        "test",
			Description: "Use for <tags> & \"quotes\".",
			Path:        "/test/SKILL.md",
		},
	}
	block := CatalogSystemPromptBlock(cat)
	if strings.Contains(block, "<tags>") {
		t.Error("XML special chars in description should be escaped")
	}
	if !strings.Contains(block, "&lt;tags&gt;") {
		t.Error("expected &lt;tags&gt; in escaped output")
	}
	if !strings.Contains(block, "&amp;") {
		t.Error("expected &amp; in escaped output")
	}
}

// ─── /skill command ───────────────────────────────────────────────────────────

// newSkillAgent builds a minimal Agent with a populated Skills catalog and a
// temp workspace, ready for /skill command tests.
func newSkillAgent(t *testing.T) *Agent {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	a := NewAgent(DefaultConfig(), ws)
	a.Skills = SkillCatalog{
		"go-review": {
			Name:          "go-review",
			Description:   "Review Go source code for quality issues.",
			License:       "AGPL-3.0",
			Compatibility: "Requires a Go codebase",
			Metadata:      map[string]string{"author": "rsdoiel", "version": "1.0"},
			Path:          "/proj/harvey/skills/go-review/SKILL.md",
			Body:          "# Go Review\n\nCheck for correctness and style.",
			Source:        SkillSourceProject,
		},
		"data-analysis": {
			Name:        "data-analysis",
			Description: "Analyse datasets and produce summary reports.",
			Path:        "/home/user/harvey/skills/data-analysis/SKILL.md",
			Body:        "# Data Analysis\n\nStep 1: load the data.",
			Source:      SkillSourceUser,
		},
	}
	return a
}

func runSkillCmd(t *testing.T, a *Agent, args ...string) string {
	t.Helper()
	var out strings.Builder
	if err := cmdSkill(a, args, &out); err != nil {
		t.Fatalf("cmdSkill(%v): %v", args, err)
	}
	return out.String()
}

func TestCmdSkill_listEmpty(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	out := runSkillCmd(t, a)
	if !strings.Contains(out, "No skills") {
		t.Errorf("expected 'No skills' message, got: %q", out)
	}
}

func TestCmdSkill_listShowsAll(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "list")
	if !strings.Contains(out, "go-review") {
		t.Errorf("output missing go-review: %q", out)
	}
	if !strings.Contains(out, "data-analysis") {
		t.Errorf("output missing data-analysis: %q", out)
	}
}

func TestCmdSkill_listSorted(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "list")
	iD := strings.Index(out, "data-analysis")
	iG := strings.Index(out, "go-review")
	if iD >= iG {
		t.Errorf("expected data-analysis before go-review; positions: %d vs %d", iD, iG)
	}
}

func TestCmdSkill_listShowsSource(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "list")
	if !strings.Contains(out, "project") {
		t.Errorf("output missing source label 'project': %q", out)
	}
	if !strings.Contains(out, "user") {
		t.Errorf("output missing source label 'user': %q", out)
	}
}

func TestCmdSkill_loadNotFound(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "load", "no-such-skill")
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' message, got: %q", out)
	}
}

func TestCmdSkill_loadInjectsBody(t *testing.T) {
	a := newSkillAgent(t)
	before := len(a.History)
	out := runSkillCmd(t, a, "load", "go-review")

	if !strings.Contains(out, "✓") {
		t.Errorf("expected success indicator, got: %q", out)
	}
	if len(a.History) != before+1 {
		t.Fatalf("expected 1 message added, history len %d → %d", before, len(a.History))
	}
	msg := a.History[len(a.History)-1]
	if msg.Role != "user" {
		t.Errorf("loaded skill role = %q, want user", msg.Role)
	}
	if !strings.Contains(msg.Content, "go-review") {
		t.Errorf("message missing skill name: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "# Go Review") {
		t.Errorf("message missing skill body: %q", msg.Content)
	}
}

func TestCmdSkill_loadEmptyBody(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	a.Skills = SkillCatalog{
		"empty": {Name: "empty", Description: "Empty skill.", Body: "", Source: SkillSourceUser},
	}
	out := runSkillCmd(t, a, "load", "empty")
	if !strings.Contains(out, "no body") {
		t.Errorf("expected 'no body' message, got: %q", out)
	}
}

func TestCmdSkill_loadNoArgs(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "load")
	if !strings.Contains(out, "Usage") {
		t.Errorf("expected usage message, got: %q", out)
	}
}

func TestCmdSkill_infoShowsFields(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "info", "go-review")
	for _, want := range []string{"go-review", "AGPL-3.0", "Requires a Go codebase", "rsdoiel", "project"} {
		if !strings.Contains(out, want) {
			t.Errorf("info output missing %q:\n%s", want, out)
		}
	}
}

func TestCmdSkill_infoNotFound(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "info", "nope")
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found', got: %q", out)
	}
}

func TestCmdSkill_infoNoArgs(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "info")
	if !strings.Contains(out, "Usage") {
		t.Errorf("expected usage message, got: %q", out)
	}
}

func TestCmdSkill_status(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "status")
	if !strings.Contains(out, "2") {
		t.Errorf("expected total count 2, got: %q", out)
	}
	if !strings.Contains(out, "Project") {
		t.Errorf("expected project scope in status, got: %q", out)
	}
	if !strings.Contains(out, "User") {
		t.Errorf("expected user scope in status, got: %q", out)
	}
}

func TestCmdSkill_statusEmpty(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	out := runSkillCmd(t, a)
	if !strings.Contains(out, "No skills") {
		t.Errorf("expected 'No skills' for empty catalog, got: %q", out)
	}
}

func TestCmdSkill_unknownSubcommand(t *testing.T) {
	a := newSkillAgent(t)
	out := runSkillCmd(t, a, "frobnicate")
	if !strings.Contains(out, "Unknown") {
		t.Errorf("expected unknown subcommand message, got: %q", out)
	}
}

// ─── loadSkills ───────────────────────────────────────────────────────────────

func TestLoadSkills_injectsIntoSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "harvey", "skills")
	writeSkillFile(t, skillsDir, "test-skill",
		"---\nname: test-skill\ndescription: A test skill for unit testing.\n---\n\n# Test\n\nInstructions here.\n")

	ws, _ := NewWorkspace(dir)
	cfg := DefaultConfig()
	cfg.SystemPrompt = "You are Harvey."
	a := NewAgent(cfg, ws)
	a.AddMessage("system", cfg.SystemPrompt)

	var out strings.Builder
	a.loadSkills(&out)

	if len(a.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(a.Skills))
	}
	if !strings.Contains(out.String(), "1 skill") {
		t.Errorf("startup message missing skill count: %q", out.String())
	}

	// Catalog block must be in the system message in History.
	sysContent := ""
	for _, m := range a.History {
		if m.Role == "system" {
			sysContent = m.Content
			break
		}
	}
	if !strings.Contains(sysContent, "test-skill") {
		t.Errorf("system message missing skill catalog: %q", sysContent)
	}

	// Config.SystemPrompt must also be updated so /clear preserves the catalog.
	if !strings.Contains(a.Config.SystemPrompt, "test-skill") {
		t.Errorf("Config.SystemPrompt missing catalog after loadSkills")
	}
}

func TestLoadSkills_noSkillsIsSilent(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)

	var out strings.Builder
	a.loadSkills(&out)

	if out.Len() != 0 {
		t.Errorf("expected no output when no skills found, got: %q", out.String())
	}
	if len(a.Skills) != 0 {
		t.Errorf("expected empty catalog, got %d skills", len(a.Skills))
	}
}

func TestLoadSkills_createsSystemMessageWhenNone(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "harvey", "skills")
	writeSkillFile(t, skillsDir, "bare-skill",
		"---\nname: bare-skill\ndescription: Skill with no prior system prompt.\n---\n")

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	// No system message added — simulate no HARVEY.md.

	a.loadSkills(&strings.Builder{})

	hasSys := false
	for _, m := range a.History {
		if m.Role == "system" {
			hasSys = true
			if !strings.Contains(m.Content, "bare-skill") {
				t.Errorf("system message missing catalog: %q", m.Content)
			}
		}
	}
	if !hasSys {
		t.Error("expected a system message to be created when none existed")
	}
}
