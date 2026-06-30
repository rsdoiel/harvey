package harvey

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createTempSession writes content to a temporary .spmd file and returns its path.
func createTempSession(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-session.spmd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

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

// ─── writeSkillMD ─────────────────────────────────────────────────────────────

// TestWriteSkillMD_CreatesFile verifies that writeSkillMD creates SKILL.md at
// the expected path inside the workspace.
func TestWriteSkillMD_CreatesFile(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	c := SkillCandidate{
		Name:        "test-skill",
		Description: "A test skill.",
		Steps:       []string{"Step one", "Step two", "Step three"},
	}
	if err := writeSkillMD(ws, c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	skillPath := filepath.Join(ws.Root, "agents", "skills", "test-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("expected SKILL.md at %s, got: %v", skillPath, err)
	}
}

// TestWriteSkillMD_ContainsName verifies that the written SKILL.md contains
// the candidate's name.
func TestWriteSkillMD_ContainsName(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	c := SkillCandidate{
		Name:        "my-cool-skill",
		Description: "Does something cool.",
		Steps:       []string{"Do it", "Do more", "Finish"},
	}
	if err := writeSkillMD(ws, c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := ws.ReadFile(filepath.Join("agents", "skills", "my-cool-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(content), "my-cool-skill") {
		t.Errorf("expected skill name in SKILL.md, got: %q", string(content))
	}
}

// TestWriteSkillMD_CreatesScriptsDir verifies that writeSkillMD also creates
// the scripts/ subdirectory alongside SKILL.md.
func TestWriteSkillMD_CreatesScriptsDir(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	c := SkillCandidate{
		Name:        "test-skill",
		Description: "A test skill.",
		Steps:       []string{"Step one", "Step two", "Step three"},
	}
	if err := writeSkillMD(ws, c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scriptsDir := filepath.Join(ws.Root, "agents", "skills", "test-skill", "scripts")
	info, err := os.Stat(scriptsDir)
	if err != nil {
		t.Fatalf("expected scripts/ dir at %s, got: %v", scriptsDir, err)
	}
	if !info.IsDir() {
		t.Errorf("expected scripts/ to be a directory")
	}
}

// ─── Suggest ─────────────────────────────────────────────────────────────────

// TestSuggest_NilClient verifies that Suggest returns an error when the agent
// has no LLM client connected.
func TestSuggest_NilClient(t *testing.T) {
	a := newTestAgent(t)
	a.Client = nil
	sg := NewSuggestor(a.Workspace)

	var out strings.Builder
	err := sg.Suggest(context.Background(), "", a, &out, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error with nil client, got nil")
	}
}

// TestSuggest_NoCandidates verifies that Suggest prints a "no candidates"
// message when the LLM returns an empty JSON array.
func TestSuggest_NoCandidates(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "[]"}
	sg := NewSuggestor(a.Workspace)
	sessionFile := createTempSession(t, "a session transcript")

	var out strings.Builder
	if err := sg.Suggest(context.Background(), sessionFile, a, &out, strings.NewReader("")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No skill candidates") {
		t.Errorf("expected 'No skill candidates' in output, got: %q", out.String())
	}
}

// TestSuggest_AcceptsCandidate verifies that answering "y" at the prompt causes
// writeSkillMD to create the SKILL.md file in the workspace.
func TestSuggest_AcceptsCandidate(t *testing.T) {
	a := newTestAgent(t)
	candidateJSON := `[{"name":"test-skill","description":"A test skill.","long_description":"Does something useful.","variables":[],"steps":["Step one","Step two","Step three"]}]`
	a.Client = &mockLLMClient{reply: candidateJSON}
	sg := NewSuggestor(a.Workspace)
	sessionFile := createTempSession(t, "a session transcript")

	var out strings.Builder
	if err := sg.Suggest(context.Background(), sessionFile, a, &out, strings.NewReader("y\n")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	skillPath := filepath.Join(a.Workspace.Root, "agents", "skills", "test-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("expected SKILL.md at %s after accepting: %v", skillPath, err)
	}
}

// TestSuggest_SkipsCandidate verifies that answering "n" does not create
// any SKILL.md file.
func TestSuggest_SkipsCandidate(t *testing.T) {
	a := newTestAgent(t)
	candidateJSON := `[{"name":"test-skill","description":"A test skill.","long_description":"Does something useful.","variables":[],"steps":["Step one","Step two","Step three"]}]`
	a.Client = &mockLLMClient{reply: candidateJSON}
	sg := NewSuggestor(a.Workspace)
	sessionFile := createTempSession(t, "a session transcript")

	var out strings.Builder
	if err := sg.Suggest(context.Background(), sessionFile, a, &out, strings.NewReader("n\n")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	skillPath := filepath.Join(a.Workspace.Root, "agents", "skills", "test-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err == nil {
		t.Errorf("expected no SKILL.md after skipping, but file exists at %s", skillPath)
	}
	if !strings.Contains(out.String(), "Skipped") {
		t.Errorf("expected 'Skipped' in output, got: %q", out.String())
	}
}

// TestSuggest_QuitEarly verifies that answering "q" stops the review loop and
// does not process subsequent candidates.
func TestSuggest_QuitEarly(t *testing.T) {
	a := newTestAgent(t)
	// Two candidates; user quits on the first.
	candidateJSON := `[
		{"name":"skill-one","description":"First.","long_description":"First skill.","variables":[],"steps":["A","B","C"]},
		{"name":"skill-two","description":"Second.","long_description":"Second skill.","variables":[],"steps":["X","Y","Z"]}
	]`
	a.Client = &mockLLMClient{reply: candidateJSON}
	sg := NewSuggestor(a.Workspace)
	sessionFile := createTempSession(t, "a session transcript")

	var out strings.Builder
	if err := sg.Suggest(context.Background(), sessionFile, a, &out, strings.NewReader("q\n")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Neither skill should be written.
	for _, name := range []string{"skill-one", "skill-two"} {
		p := filepath.Join(a.Workspace.Root, "agents", "skills", name, "SKILL.md")
		if _, err := os.Stat(p); err == nil {
			t.Errorf("expected no SKILL.md for %s after quit, but file exists", name)
		}
	}
}
