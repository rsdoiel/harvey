package harvey

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── ValidSkillName ──────────────────────────────────────────────────────────

func TestValidSkillName(t *testing.T) {
	valid := []string{"my-skill", "tool2", "a", "go-review", "abc-123"}
	for _, name := range valid {
		if !ValidSkillName(name) {
			t.Errorf("ValidSkillName(%q): want true, got false", name)
		}
	}
	invalid := []string{"", "My-Skill", "bad name", "has_underscore", "has.dot", "UPPER", "-start"}
	for _, name := range invalid {
		if ValidSkillName(name) {
			t.Errorf("ValidSkillName(%q): want false, got true", name)
		}
	}
}

// ─── CompiledBashPath / CompiledPS1Path ──────────────────────────────────────

func TestCompiledBashPath(t *testing.T) {
	got := CompiledBashPath("/proj/agents/skills/my-skill/SKILL.md")
	want := "/proj/agents/skills/my-skill/scripts/compiled.bash"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestCompiledPS1Path(t *testing.T) {
	got := CompiledPS1Path("/proj/agents/skills/my-skill/SKILL.md")
	want := "/proj/agents/skills/my-skill/scripts/compiled.ps1"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// ─── IsStale ─────────────────────────────────────────────────────────────────

func TestIsStale_missingScripts(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\ndescription: test\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := &SkillMeta{Path: skillPath}
	stale, err := IsStale(skill)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Error("want stale=true when scripts missing")
	}
}

func TestIsStale_upToDate(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\ndescription: test\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write scripts with a timestamp 1 second in the future so they appear newer.
	future := time.Now().Add(2 * time.Second)
	for _, name := range []string{"compiled.bash", "compiled.ps1"} {
		p := filepath.Join(scriptsDir, name)
		if err := os.WriteFile(p, []byte("#!/bin/bash"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}

	skill := &SkillMeta{Path: skillPath}
	stale, err := IsStale(skill)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stale {
		t.Error("want stale=false when scripts are newer than SKILL.md")
	}
}

func TestIsStale_skillNewer(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\ndescription: test\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write scripts with an old timestamp.
	past := time.Now().Add(-2 * time.Second)
	for _, name := range []string{"compiled.bash", "compiled.ps1"} {
		p := filepath.Join(scriptsDir, name)
		if err := os.WriteFile(p, []byte("#!/bin/bash"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	// Make SKILL.md newer than the scripts.
	now := time.Now()
	if err := os.Chtimes(skillPath, now, now); err != nil {
		t.Fatal(err)
	}

	skill := &SkillMeta{Path: skillPath}
	stale, err := IsStale(skill)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Error("want stale=true when SKILL.md is newer than scripts")
	}
}

// ─── RunSkillWizard ──────────────────────────────────────────────────────────

func makeWizardWorkspace(t *testing.T) *Workspace {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return ws
}

func TestRunSkillWizard_invalidName(t *testing.T) {
	ws := makeWizardWorkspace(t)
	input := "Bad Name\n"
	reader := bufio.NewReader(strings.NewReader(input))
	var out strings.Builder
	_, err := RunSkillWizard(ws, "", reader, &out)
	if err == nil {
		t.Fatal("want error for invalid skill name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("want 'invalid name' in error, got: %v", err)
	}
}

func TestRunSkillWizard_overwriteGuard(t *testing.T) {
	ws := makeWizardWorkspace(t)

	// Pre-create the skill.
	existing := "agents/skills/my-skill/SKILL.md"
	if err := ws.WriteFile(existing, []byte("---\ndescription: exists\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := "my-skill\n"
	reader := bufio.NewReader(strings.NewReader(input))
	var out strings.Builder
	_, err := RunSkillWizard(ws, "", reader, &out)
	if err == nil {
		t.Fatal("want error when skill already exists, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("want 'already exists' in error, got: %v", err)
	}
}

func TestRunSkillWizard_success(t *testing.T) {
	// Stub editor: use `true` (no-op, leaves temp file with default content).
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not found in PATH")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", truePath)

	ws := makeWizardWorkspace(t)
	// Provide all fields including optional ones.
	input := strings.Join([]string{
		"my-skill",        // name
		"A test skill",    // description
		"MIT",             // license
		"harvey",          // compatibility
		"testuser",        // author
		"2.0",             // version
		"pdf extract",     // trigger
	}, "\n") + "\n"

	reader := bufio.NewReader(strings.NewReader(input))
	var out strings.Builder
	relPath, err := RunSkillWizard(ws, "", reader, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if relPath != "agents/skills/my-skill/SKILL.md" {
		t.Errorf("want 'agents/skills/my-skill/SKILL.md', got %q", relPath)
	}

	// Read back the created file.
	abs, _ := ws.AbsPath(relPath)
	content, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	cs := string(content)

	checks := []string{
		"name: my-skill",
		"description: A test skill",
		"license: MIT",
		"compatibility: harvey",
		"trigger: pdf extract",
		"author: testuser",
		`version: "2.0"`,
	}
	for _, want := range checks {
		if !strings.Contains(cs, want) {
			t.Errorf("SKILL.md missing %q\ncontent:\n%s", want, cs)
		}
	}
}

func TestRunSkillWizard_optionalFieldsOmitted(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not found in PATH")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", truePath)

	ws := makeWizardWorkspace(t)
	// Leave compatibility and trigger blank.
	input := strings.Join([]string{
		"lean-skill",   // name
		"Minimal",      // description
		"",             // license → default Apache-2.0
		"",             // compatibility → omit
		"",             // author → default from USER env
		"",             // version → default 1.0
		"",             // trigger → omit
	}, "\n") + "\n"

	reader := bufio.NewReader(strings.NewReader(input))
	var out strings.Builder
	_, wizErr := RunSkillWizard(ws, "", reader, &out)
	if wizErr != nil {
		t.Fatalf("unexpected error: %v", wizErr)
	}

	abs, _ := ws.AbsPath("agents/skills/lean-skill/SKILL.md")
	content, _ := os.ReadFile(abs)
	cs := string(content)

	if strings.Contains(cs, "compatibility:") {
		t.Error("want compatibility omitted when empty")
	}
	if strings.Contains(cs, "trigger:") {
		t.Error("want trigger omitted when empty")
	}
	if !strings.Contains(cs, "license: Apache-2.0") {
		t.Error("want default license Apache-2.0")
	}
}
