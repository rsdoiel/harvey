package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillSet writes a skill-set YAML file relative to the workspace root.
func writeSkillSet(t *testing.T, a *Agent, name, content string) string {
	t.Helper()
	dir := skillSetDir(a.Workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll skill-sets: %v", err)
	}
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

// ─── ParseSkillSet ───────────────────────────────────────────────────────────

func TestParseSkillSet_basic(t *testing.T) {
	a := newTestAgent(t)
	writeSkillSet(t, a, "test-bundle", `
name: test-bundle
description: A test bundle.
skills:
  - skill-a
  - skill-b
metadata:
  version: "1.0"
`)
	path := filepath.Join(skillSetDir(a.Workspace), "test-bundle.yaml")
	ss, err := ParseSkillSet(path)
	if err != nil {
		t.Fatalf("ParseSkillSet: %v", err)
	}
	if ss.Name != "test-bundle" {
		t.Errorf("Name: got %q, want %q", ss.Name, "test-bundle")
	}
	if len(ss.Skills) != 2 || ss.Skills[0] != "skill-a" || ss.Skills[1] != "skill-b" {
		t.Errorf("Skills: got %v", ss.Skills)
	}
	if ss.Metadata["version"] != "1.0" {
		t.Errorf("Metadata version: got %q", ss.Metadata["version"])
	}
}

func TestParseSkillSet_inferNameFromFilename(t *testing.T) {
	a := newTestAgent(t)
	writeSkillSet(t, a, "inferred", `
description: no explicit name field
skills:
  - skill-a
`)
	path := filepath.Join(skillSetDir(a.Workspace), "inferred.yaml")
	ss, err := ParseSkillSet(path)
	if err != nil {
		t.Fatalf("ParseSkillSet: %v", err)
	}
	if ss.Name != "inferred" {
		t.Errorf("Name inferred from filename: got %q", ss.Name)
	}
}

func TestParseSkillSet_notFound(t *testing.T) {
	a := newTestAgent(t)
	path := filepath.Join(skillSetDir(a.Workspace), "missing.yaml")
	_, err := ParseSkillSet(path)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParseSkillSet_badYAML(t *testing.T) {
	a := newTestAgent(t)
	writeSkillSet(t, a, "bad", "skills: [unclosed")
	path := filepath.Join(skillSetDir(a.Workspace), "bad.yaml")
	_, err := ParseSkillSet(path)
	if err == nil {
		t.Error("expected parse error for malformed YAML")
	}
}

// ─── listSkillSetNames ───────────────────────────────────────────────────────

func TestListSkillSetNames_empty(t *testing.T) {
	a := newTestAgent(t)
	names, err := listSkillSetNames(a.Workspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}
}

func TestListSkillSetNames_findsYAML(t *testing.T) {
	a := newTestAgent(t)
	writeSkillSet(t, a, "alpha", "name: alpha\nskills:\n  - x\n")
	writeSkillSet(t, a, "beta", "name: beta\nskills:\n  - y\n")

	names, err := listSkillSetNames(a.Workspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %v", names)
	}
}

// ─── validateSkillSet ────────────────────────────────────────────────────────

func TestValidateSkillSet_allPresent(t *testing.T) {
	cat := SkillCatalog{
		"skill-a": &SkillMeta{Name: "skill-a"},
		"skill-b": &SkillMeta{Name: "skill-b"},
	}
	ss := &SkillSetMeta{Name: "bundle", Skills: []string{"skill-a", "skill-b"}}
	if err := validateSkillSet(ss, cat); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSkillSet_missingSkill(t *testing.T) {
	cat := SkillCatalog{
		"skill-a": &SkillMeta{Name: "skill-a"},
	}
	ss := &SkillSetMeta{Name: "bundle", Skills: []string{"skill-a", "missing-skill"}}
	err := validateSkillSet(ss, cat)
	if err == nil {
		t.Fatal("expected error for missing skill")
	}
	if !strings.Contains(err.Error(), "missing-skill") {
		t.Errorf("error should mention missing skill name: %v", err)
	}
}

func TestValidateSkillSet_duplicateSkill(t *testing.T) {
	cat := SkillCatalog{
		"skill-a": &SkillMeta{Name: "skill-a"},
	}
	ss := &SkillSetMeta{Name: "bundle", Skills: []string{"skill-a", "skill-a"}}
	err := validateSkillSet(ss, cat)
	if err == nil {
		t.Fatal("expected error for duplicate skill")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate: %v", err)
	}
}

func TestValidateSkillSet_empty(t *testing.T) {
	ss := &SkillSetMeta{Name: "empty", Skills: nil}
	if err := validateSkillSet(ss, SkillCatalog{}); err == nil {
		t.Error("expected error for skill-set with no skills")
	}
}

// ─── cmdSkillSet (command level) ─────────────────────────────────────────────

func TestCmdSkillSet_listEmpty(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"list"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No skill-sets found") {
		t.Errorf("expected 'No skill-sets found': %s", out.String())
	}
}

func TestCmdSkillSet_listWithEntry(t *testing.T) {
	a := newTestAgent(t)
	writeSkillSet(t, a, "my-bundle", "name: my-bundle\ndescription: test\nskills:\n  - x\n")
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"list"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "my-bundle") {
		t.Errorf("expected my-bundle in list: %s", out.String())
	}
}

func TestCmdSkillSet_infoFound(t *testing.T) {
	a := newTestAgent(t)
	a.Skills = SkillCatalog{
		"skill-x": &SkillMeta{Name: "skill-x", Description: "does X things"},
	}
	writeSkillSet(t, a, "bundle", "name: bundle\nskills:\n  - skill-x\n")
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"info", "bundle"}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "skill-x") {
		t.Errorf("expected skill-x in info output: %s", s)
	}
	if !strings.Contains(s, "found") {
		t.Errorf("expected skill status in output: %s", s)
	}
}

func TestCmdSkillSet_infoNotFound(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"info", "nonexistent"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("expected 'not found': %s", out.String())
	}
}

func TestCmdSkillSet_create(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"create", "newbundle"}, &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillSetDir(a.Workspace), "newbundle.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to be created at %s", path)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "newbundle") {
		t.Errorf("file should contain the bundle name")
	}
}

func TestCmdSkillSet_createAlreadyExists(t *testing.T) {
	a := newTestAgent(t)
	writeSkillSet(t, a, "exists", "name: exists\nskills:\n  - x\n")
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"create", "exists"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already exists") {
		t.Errorf("expected 'already exists': %s", out.String())
	}
}

func TestCmdSkillSet_statusNone(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"status"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No skill-set") {
		t.Errorf("expected 'No skill-set': %s", out.String())
	}
}

func TestCmdSkillSet_unloadClearsIndicator(t *testing.T) {
	a := newTestAgent(t)
	a.ActiveSkillSet = "fountain"
	a.ActiveSkill = "fountain-analysis"
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"unload"}, &out); err != nil {
		t.Fatal(err)
	}
	if a.ActiveSkillSet != "" {
		t.Error("ActiveSkillSet should be cleared after unload")
	}
	if a.ActiveSkill != "" {
		t.Error("ActiveSkill should be cleared after unload")
	}
}

func TestCmdSkillSet_noWorkspace(t *testing.T) {
	a := &Agent{Config: DefaultConfig(), commands: make(map[string]*Command)}
	var out strings.Builder
	if err := cmdSkillSet(a, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No workspace") {
		t.Errorf("expected 'No workspace': %s", out.String())
	}
}

func TestCmdSkillSet_unknownSubcommand(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSkillSet(a, []string{"bogus"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Unknown subcommand") {
		t.Errorf("expected 'Unknown subcommand': %s", out.String())
	}
}
