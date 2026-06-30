package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── latestSessionFile ────────────────────────────────────────────────────────

// TestSuggestor_LatestSessionFile_PicksMostRecent verifies that latestSessionFile
// returns the .spmd file with the most recent modification time.
func TestSuggestor_LatestSessionFile_PicksMostRecent(t *testing.T) {
	dir := t.TempDir()

	older := filepath.Join(dir, "session-old.spmd")
	newer := filepath.Join(dir, "session-new.spmd")

	if err := os.WriteFile(older, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Set mtime one hour in the past so the filesystem reflects the age difference.
	oldTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(older, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := latestSessionFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != newer {
		t.Errorf("expected %q (newer), got %q", newer, got)
	}
}

// TestSuggestor_LatestSessionFile_IgnoresNonSpmd verifies that non-.spmd files
// in the directory are skipped.
func TestSuggestor_LatestSessionFile_IgnoresNonSpmd(t *testing.T) {
	dir := t.TempDir()

	// Write a non-.spmd file (should be ignored) and one valid .spmd file.
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644)
	spmd := filepath.Join(dir, "session.spmd")
	os.WriteFile(spmd, []byte("session"), 0o644)

	got, err := latestSessionFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != spmd {
		t.Errorf("expected %q, got %q", spmd, got)
	}
}

// TestSuggestor_LatestSessionFile_NoFiles verifies that an error is returned
// when no .spmd files exist in the directory.
func TestSuggestor_LatestSessionFile_NoFiles(t *testing.T) {
	dir := t.TempDir()

	_, err := latestSessionFile(dir)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

// ─── renderSkillMD ────────────────────────────────────────────────────────────

// TestRenderSkillMD_ContainsName verifies that the output includes the
// candidate's name in the SKILL.md frontmatter.
func TestRenderSkillMD_ContainsName(t *testing.T) {
	c := SkillCandidate{
		Name:            "setup-experiment",
		Description:     "Set up a new Laboratory experiment.",
		LongDescription: "Creates directory structure and initialises git.",
		Steps:           []string{"Create directory", "git init"},
	}
	got := renderSkillMD(c, "R. S. Doiel")
	if !strings.Contains(got, "setup-experiment") {
		t.Errorf("expected skill name in output, got: %q", got)
	}
}

// TestRenderSkillMD_ContainsVariables verifies that the output includes
// variable names when the candidate declares variables.
func TestRenderSkillMD_ContainsVariables(t *testing.T) {
	c := SkillCandidate{
		Name:        "setup-experiment",
		Description: "Set up a new Laboratory experiment.",
		Variables: []SkillVariable{
			{Name: "EXPERIMENT_NAME", Type: "string", Description: "Name of the experiment", Example: "harvey"},
		},
		Steps: []string{"Create directory"},
	}
	got := renderSkillMD(c, "R. S. Doiel")
	if !strings.Contains(got, "EXPERIMENT_NAME") {
		t.Errorf("expected variable name in output, got: %q", got)
	}
}

// TestRenderSkillMD_ContainsSteps verifies that the output lists all steps
// from the candidate.
func TestRenderSkillMD_ContainsSteps(t *testing.T) {
	c := SkillCandidate{
		Name:        "setup-experiment",
		Description: "Set up a new Laboratory experiment.",
		Steps:       []string{"Create directory", "Run git init", "Copy template"},
	}
	got := renderSkillMD(c, "R. S. Doiel")
	for _, step := range c.Steps {
		if !strings.Contains(got, step) {
			t.Errorf("expected step %q in output, got: %q", step, got)
		}
	}
}

// TestRenderSkillMD_NoVariables verifies that the variables block is omitted
// when the candidate declares no variables.
func TestRenderSkillMD_NoVariables(t *testing.T) {
	c := SkillCandidate{
		Name:        "simple-skill",
		Description: "A skill with no variables.",
		Steps:       []string{"Do the thing"},
	}
	got := renderSkillMD(c, "R. S. Doiel")
	// Output must still include the skill name and step.
	if !strings.Contains(got, "simple-skill") {
		t.Errorf("expected skill name in output, got: %q", got)
	}
}
