package harvey

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
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
