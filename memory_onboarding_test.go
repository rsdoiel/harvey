package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsOnboarding_Empty(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !NeedsOnboarding(store) {
		t.Error("empty workspace_profile dir should need onboarding")
	}
}

func TestNeedsOnboarding_HasProfile(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dir := filepath.Join(store.Dir(), string(MemoryTypeWorkspaceProfile))
	stub := "---\nid: profile_abc123\ntype: workspace_profile\ncreated_at: \"2026-01-01T00:00:00Z\"\nupdated_at: \"2026-01-01T00:00:00Z\"\nsupersedes: []\ntags: []\ndescription: test\nsummary: test\n---\n\nFADE IN:\n\nINT. MEMORY 2026-01-01 00:00:00\n\nTHE END.\n"
	if err := os.WriteFile(filepath.Join(dir, "profile_abc123.fountain"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	if NeedsOnboarding(store) {
		t.Error("workspace with profile file should not need onboarding")
	}
}

func TestExtractProjectFact_Codemeta(t *testing.T) {
	dir := t.TempDir()
	codemeta := `{"name":"myapp","description":"A test app","programmingLanguage":[{"name":"Go"}],"developmentStatus":"active"}`
	if err := os.WriteFile(filepath.Join(dir, "codemeta.json"), []byte(codemeta), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractProjectFact(dir)
	if got == "" {
		t.Fatal("expected non-empty project fact from codemeta.json")
	}
	if !strings.Contains(got, "myapp") {
		t.Errorf("expected name in result: %q", got)
	}
	if !strings.Contains(got, "Go") {
		t.Errorf("expected language in result: %q", got)
	}
	if !strings.Contains(got, "active") {
		t.Errorf("expected developmentStatus in result: %q", got)
	}
}

func TestExtractProjectFact_CodemetaStringLang(t *testing.T) {
	dir := t.TempDir()
	codemeta := `{"name":"proj","programmingLanguage":"TypeScript"}`
	if err := os.WriteFile(filepath.Join(dir, "codemeta.json"), []byte(codemeta), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractProjectFact(dir)
	if !strings.Contains(got, "TypeScript") {
		t.Errorf("expected TypeScript in result: %q", got)
	}
}

func TestExtractProjectFact_GoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := "module github.com/example/myproject\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractProjectFact(dir)
	if !strings.Contains(got, "github.com/example/myproject") {
		t.Errorf("expected module name in result: %q", got)
	}
}

func TestExtractProjectFact_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"name":"my-pkg","description":"A JavaScript package"}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractProjectFact(dir)
	if !strings.Contains(got, "my-pkg") {
		t.Errorf("expected package name in result: %q", got)
	}
	if !strings.Contains(got, "A JavaScript package") {
		t.Errorf("expected description in result: %q", got)
	}
}

func TestExtractProjectFact_GitOrigin(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = https://github.com/example/repo.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractProjectFact(dir)
	if !strings.Contains(got, "https://github.com/example/repo.git") {
		t.Errorf("expected git remote URL in result: %q", got)
	}
}

func TestExtractProjectFact_Bare(t *testing.T) {
	dir := t.TempDir()
	if got := extractProjectFact(dir); got != "" {
		t.Errorf("empty workspace should return \"\", got %q", got)
	}
}

func TestExtractProjectFact_EmptyRoot(t *testing.T) {
	if got := extractProjectFact(""); got != "" {
		t.Errorf("empty wsRoot should return \"\", got %q", got)
	}
}

func TestExtractProjectFact_PreferCodemeta(t *testing.T) {
	dir := t.TempDir()
	// Both codemeta.json and go.mod present — codemeta wins.
	codemeta := `{"name":"preferred","description":"codemeta wins"}`
	if err := os.WriteFile(filepath.Join(dir, "codemeta.json"), []byte(codemeta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/other/mod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractProjectFact(dir)
	if !strings.Contains(got, "preferred") {
		t.Errorf("codemeta.json should take priority: %q", got)
	}
}
