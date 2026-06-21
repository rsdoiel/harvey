package harvey

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
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

	doc := NewMemoryDoc("profile_abc123", MemoryTypeWorkspaceProfile, "test", "test", nil)
	if err := store.Save(doc, nil); err != nil {
		t.Fatal(err)
	}
	if NeedsOnboarding(store) {
		t.Error("workspace with active profile in DB should not need onboarding")
	}
}

func TestNeedsOnboarding_ArchivedProfile(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	doc := NewMemoryDoc("profile_abc123", MemoryTypeWorkspaceProfile, "test", "test", nil)
	if err := store.Save(doc, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Archive("profile_abc123"); err != nil {
		t.Fatal(err)
	}
	if !NeedsOnboarding(store) {
		t.Error("workspace with only archived profiles should need onboarding")
	}
}

// newOnboardingAgent returns a minimal Agent backed by a temp workspace, suitable
// for onboarding tests. No LLM client is configured.
func newOnboardingAgent(t *testing.T) (*Agent, *MemoryStore) {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Memory.Enabled = true
	a := &Agent{Config: cfg, Workspace: ws, commands: make(map[string]*Command)}
	return a, store
}

// TestRunOnboarding_SelectTemplate simulates selecting template [1] (Back End
// Developer). Because the test runner is not a terminal, the editor is skipped
// and the template content is saved as-is.
func TestRunOnboarding_SelectTemplate(t *testing.T) {
	a, store := newOnboardingAgent(t)
	defer store.Close()

	input := strings.NewReader("1\n") // select first template
	var out bytes.Buffer

	if err := RunOnboarding(a, store, nil, &out, input); err != nil {
		t.Fatalf("RunOnboarding: %v", err)
	}

	// A workspace_profile memory should now exist.
	if NeedsOnboarding(store) {
		t.Error("NeedsOnboarding should be false after RunOnboarding")
	}

	metas, err := store.List(string(MemoryTypeWorkspaceProfile))
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(metas) == 0 {
		t.Fatal("no workspace_profile memories found after onboarding")
	}

	doc, err := store.ByID(metas[0].ID)
	if err != nil || doc == nil {
		t.Fatalf("could not load saved profile: %v", err)
	}

	// The first template is "Back End Developer".
	if !strings.Contains(doc.Meta.Description, "Back End Developer") {
		t.Errorf("description %q should mention template name", doc.Meta.Description)
	}
	if !strings.Contains(doc.FountainBody, "Back End Developer") {
		t.Errorf("FountainBody should contain template name")
	}
	// NOTE: block should be stripped from the body.
	if strings.Contains(doc.FountainBody, "NOTE:") {
		t.Error("FountainBody should not contain the NOTE: metadata block")
	}
}

// TestRunOnboarding_BlankEnter simulates pressing Enter (empty input) which
// selects the Blank template.
func TestRunOnboarding_BlankEnter(t *testing.T) {
	a, store := newOnboardingAgent(t)
	defer store.Close()

	input := strings.NewReader("\n") // Enter → Blank
	var out bytes.Buffer

	if err := RunOnboarding(a, store, nil, &out, input); err != nil {
		t.Fatalf("RunOnboarding: %v", err)
	}

	metas, err := store.List(string(MemoryTypeWorkspaceProfile))
	if err != nil || len(metas) == 0 {
		t.Fatal("no workspace_profile memories after blank selection")
	}
	doc, _ := store.ByID(metas[0].ID)
	if doc == nil {
		t.Fatal("could not load saved blank profile")
	}
	if !strings.Contains(doc.Meta.Description, "Blank") {
		t.Errorf("description %q should mention Blank template", doc.Meta.Description)
	}
}

// TestRunOnboarding_InvalidSelectionFallsBack simulates an out-of-range number,
// which should silently fall back to the Blank template.
func TestRunOnboarding_InvalidSelectionFallsBack(t *testing.T) {
	a, store := newOnboardingAgent(t)
	defer store.Close()

	input := strings.NewReader("999\n")
	var out bytes.Buffer

	if err := RunOnboarding(a, store, nil, &out, input); err != nil {
		t.Fatalf("RunOnboarding: %v", err)
	}

	metas, _ := store.List(string(MemoryTypeWorkspaceProfile))
	if len(metas) == 0 {
		t.Fatal("no workspace_profile memories after invalid selection")
	}
}

// TestRunOnboarding_NonInteractiveEOF simulates a non-interactive context where
// in returns EOF immediately. Onboarding should complete using the Blank template.
func TestRunOnboarding_NonInteractiveEOF(t *testing.T) {
	a, store := newOnboardingAgent(t)
	defer store.Close()

	input := strings.NewReader("") // immediate EOF
	var out bytes.Buffer

	if err := RunOnboarding(a, store, nil, &out, input); err != nil {
		t.Fatalf("RunOnboarding: %v", err)
	}

	if NeedsOnboarding(store) {
		t.Error("NeedsOnboarding should be false even after EOF onboarding")
	}
}

// TestRunOnboarding_ProjectFactFromCodemeta verifies that a codemeta.json in
// the workspace is detected and saved as a project_fact memory.
func TestRunOnboarding_ProjectFactFromCodemeta(t *testing.T) {
	a, store := newOnboardingAgent(t)
	defer store.Close()

	codemeta := `{"name":"testapp","description":"A test application","programmingLanguage":[{"name":"Go"}],"developmentStatus":"active"}`
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "codemeta.json"), []byte(codemeta), 0o644); err != nil {
		t.Fatal(err)
	}

	input := strings.NewReader("1\n")
	var out bytes.Buffer

	if err := RunOnboarding(a, store, nil, &out, input); err != nil {
		t.Fatalf("RunOnboarding: %v", err)
	}

	pfMetas, err := store.List(string(MemoryTypeProjectFact))
	if err != nil {
		t.Fatalf("store.List project_fact: %v", err)
	}
	if len(pfMetas) == 0 {
		t.Fatal("no project_fact memory saved despite codemeta.json being present")
	}
	pfDoc, _ := store.ByID(pfMetas[0].ID)
	if pfDoc == nil {
		t.Fatal("could not load project_fact doc")
	}
	if !strings.Contains(pfDoc.FountainBody, "testapp") {
		t.Errorf("project_fact body should contain project name: %q", pfDoc.FountainBody)
	}
}

// TestRunOnboarding_ProjectFactIdentifiers verifies that an author ORCID iD
// in codemeta.json is extracted and stored in the project_fact's
// Meta.Metadata["identifiers"].
func TestRunOnboarding_ProjectFactIdentifiers(t *testing.T) {
	a, store := newOnboardingAgent(t)
	defer store.Close()

	codemeta := `{
		"name": "testapp",
		"description": "A test application",
		"author": [{"id": "https://orcid.org/0000-0003-0900-6903"}]
	}`
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "codemeta.json"), []byte(codemeta), 0o644); err != nil {
		t.Fatal(err)
	}

	input := strings.NewReader("1\n")
	var out bytes.Buffer

	if err := RunOnboarding(a, store, nil, &out, input); err != nil {
		t.Fatalf("RunOnboarding: %v", err)
	}

	pfMetas, err := store.List(string(MemoryTypeProjectFact))
	if err != nil || len(pfMetas) == 0 {
		t.Fatal("no project_fact memory saved despite codemeta.json being present")
	}
	pfDoc, _ := store.ByID(pfMetas[0].ID)
	if pfDoc == nil {
		t.Fatal("could not load project_fact doc")
	}

	idsRaw, ok := pfDoc.Meta.Metadata["identifiers"]
	if !ok {
		t.Fatalf("project_fact Metadata missing \"identifiers\": %#v", pfDoc.Meta.Metadata)
	}

	// YAML round-trips map[string][]string as map[string]interface{}.
	ids, ok := idsRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("identifiers has unexpected type %T: %#v", idsRaw, idsRaw)
	}
	orcids, ok := ids["orcid"].([]interface{})
	if !ok || len(orcids) != 1 || orcids[0] != "0000-0003-0900-6903" {
		t.Errorf("identifiers[\"orcid\"] = %#v, want [\"0000-0003-0900-6903\"]", ids["orcid"])
	}
}

// TestExtractWorkspaceIdentifiers covers extractWorkspaceIdentifiers directly.
func TestExtractWorkspaceIdentifiers(t *testing.T) {
	// No codemeta.json or CITATION.cff present.
	if ids := extractWorkspaceIdentifiers(t.TempDir()); ids != nil {
		t.Errorf("expected nil identifiers for empty workspace, got %#v", ids)
	}

	// codemeta.json with an author ORCID and a DOI in CITATION.cff.
	dir := t.TempDir()
	codemeta := `{"name":"testapp","author":[{"id":"https://orcid.org/0000-0003-0900-6903"}]}`
	if err := os.WriteFile(filepath.Join(dir, "codemeta.json"), []byte(codemeta), 0o644); err != nil {
		t.Fatal(err)
	}
	citation := "identifiers:\n  - type: doi\n    value: 10.5281/zenodo.1234567\n"
	if err := os.WriteFile(filepath.Join(dir, "CITATION.cff"), []byte(citation), 0o644); err != nil {
		t.Fatal(err)
	}

	ids := extractWorkspaceIdentifiers(dir)
	if got := ids["orcid"]; len(got) != 1 || got[0] != "0000-0003-0900-6903" {
		t.Errorf("identifiers[\"orcid\"] = %v, want [\"0000-0003-0900-6903\"]", got)
	}
	if got := ids["doi"]; len(got) != 1 || got[0] != "https://doi.org/10.5281/zenodo.1234567" {
		t.Errorf("identifiers[\"doi\"] = %v, want [\"https://doi.org/10.5281/zenodo.1234567\"]", got)
	}
}

// TestRunOnboarding_WorkspaceLocalTemplate verifies that a template in
// agents/templates/profiles/ is included in the picker and selectable.
func TestRunOnboarding_WorkspaceLocalTemplate(t *testing.T) {
	a, store := newOnboardingAgent(t)
	defer store.Close()

	// Add a workspace-local template as the first extra entry after builtins.
	localDir := filepath.Join(a.Workspace.Root, "agents", "templates", "profiles")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmplContent := "TITLE: Custom Role\n\nROLE:\n  Custom workspace role.\n"
	if err := os.WriteFile(filepath.Join(localDir, "custom-role.fountain"), []byte(tmplContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// The custom template will appear after the 6 built-ins, so it's entry 7.
	builtins := ListTemplates("")
	customIdx := len(builtins) + 1
	input := strings.NewReader(strconv.Itoa(customIdx) + "\n")
	var out bytes.Buffer

	if err := RunOnboarding(a, store, nil, &out, input); err != nil {
		t.Fatalf("RunOnboarding: %v", err)
	}

	metas, _ := store.List(string(MemoryTypeWorkspaceProfile))
	if len(metas) == 0 {
		t.Fatal("no workspace_profile after selecting workspace-local template")
	}
	doc, _ := store.ByID(metas[0].ID)
	if doc == nil {
		t.Fatal("could not load profile doc")
	}
	if !strings.Contains(doc.Meta.Description, "Custom Role") {
		t.Errorf("description %q should contain workspace-local template name", doc.Meta.Description)
	}
}

// TestResolveTemplateChoice covers the resolveTemplateChoice helper directly.
func TestResolveTemplateChoice(t *testing.T) {
	templates := ListTemplates("")

	// Valid first entry.
	data, name := resolveTemplateChoice("1", templates, "")
	if len(data) == 0 {
		t.Error("expected non-empty template data for choice 1")
	}
	if name != templates[0].Name {
		t.Errorf("name = %q, want %q", name, templates[0].Name)
	}

	// Empty input → Blank.
	data, name = resolveTemplateChoice("", templates, "")
	if name != "Blank" {
		t.Errorf("empty input: name = %q, want Blank", name)
	}
	if len(data) == 0 {
		t.Error("blank template should have non-empty content")
	}

	// Out of range → Blank.
	_, name = resolveTemplateChoice("9999", templates, "")
	if name != "Blank" {
		t.Errorf("out-of-range: name = %q, want Blank", name)
	}
}

// TestBuildProfileFountainBody checks that NOTE: is stripped and the template
// name appears in the output.
func TestBuildProfileFountainBody(t *testing.T) {
	content := []byte("TITLE: My Role\n\nNOTE:\n  Should be removed.\n\nROLE:\n  A developer.\n")
	body := buildProfileFountainBody("2026-06-05 10:00:00", "My Role", content)

	if strings.Contains(body, "NOTE:") {
		t.Error("body should not contain NOTE: block")
	}
	if strings.Contains(body, "Should be removed") {
		t.Error("body should not contain NOTE: content")
	}
	if !strings.Contains(body, "My Role") {
		t.Error("body should contain template name")
	}
	if !strings.Contains(body, "A developer") {
		t.Error("body should contain ROLE content")
	}
}

// TestTemplateBodySummary extracts the first non-empty ROLE: line.
func TestTemplateBodySummary(t *testing.T) {
	tests := []struct {
		content string
		want    string
	}{
		{"ROLE:\n  A developer.\n", "A developer."},
		{"ROLE: Inline role.\n", "Inline role."},
		{"TITLE: No role here.\n", ""},
		{"ROLE:\n\n  First non-empty line.\n", "First non-empty line."},
	}
	for _, tt := range tests {
		got := templateBodySummary([]byte(tt.content))
		if got != tt.want {
			t.Errorf("templateBodySummary(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

// TestStripTemplateNoteField ensures NOTE: blocks are removed while other
// fields are preserved.
func TestStripTemplateNoteField(t *testing.T) {
	content := "TITLE: Test\n\nNOTE:\n  remove this\n  and this\n\nROLE:\n  keep this\n"
	got := stripTemplateNoteField([]byte(content))
	if strings.Contains(got, "remove this") {
		t.Error("NOTE content should be stripped")
	}
	if !strings.Contains(got, "keep this") {
		t.Error("ROLE content should be preserved")
	}
	if !strings.Contains(got, "TITLE:") {
		t.Error("TITLE field should be preserved")
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
