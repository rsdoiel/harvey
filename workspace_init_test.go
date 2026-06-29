package harvey

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSourceWorkspace creates a temp workspace directory with agents/harvey.yaml
// containing the given model_aliases YAML block. Returns the workspace root.
func makeSourceWorkspace(t *testing.T, aliasesYAML string) string {
	t.Helper()
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var content string
	if aliasesYAML != "" {
		content = "model_aliases:\n" + aliasesYAML
	} else {
		content = "# empty\n"
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "harvey.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// makeDestWorkspace creates a temp workspace and config for the import destination.
func makeDestWorkspace(t *testing.T) (*Workspace, *Config) {
	t.Helper()
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: dir}
	cfg := DefaultConfig()
	return ws, cfg
}

// TestImportAliasesFrom_emptySource reports "no model aliases" when the source
// has no model_aliases key and returns zero counts.
func TestImportAliasesFrom_emptySource(t *testing.T) {
	src := makeSourceWorkspace(t, "")
	destWS, destCfg := makeDestWorkspace(t)

	var buf bytes.Buffer
	copied, skipped, err := ImportAliasesFrom(src, destWS, destCfg, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if copied != 0 || skipped != 0 {
		t.Errorf("want copied=0 skipped=0, got copied=%d skipped=%d", copied, skipped)
	}
	if !strings.Contains(buf.String(), "no model aliases") {
		t.Errorf("expected 'no model aliases' message, got: %q", buf.String())
	}
}

// TestImportAliasesFrom_gapFill imports aliases that are not yet defined in dest.
func TestImportAliasesFrom_gapFill(t *testing.T) {
	srcAliases := `  code:
    model: granite3.3:8b
    tags: [code]
  chat:
    model: llama3.2:3b
    tags: [chat]
`
	src := makeSourceWorkspace(t, srcAliases)
	destWS, destCfg := makeDestWorkspace(t)

	var buf bytes.Buffer
	copied, skipped, err := ImportAliasesFrom(src, destWS, destCfg, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if copied != 2 {
		t.Errorf("want copied=2, got %d", copied)
	}
	if skipped != 0 {
		t.Errorf("want skipped=0, got %d", skipped)
	}
	if _, ok := destCfg.ModelAliases["code"]; !ok {
		t.Error("expected 'code' alias to be imported")
	}
	if _, ok := destCfg.ModelAliases["chat"]; !ok {
		t.Error("expected 'chat' alias to be imported")
	}
	if !strings.Contains(buf.String(), "Imported 2 aliases") {
		t.Errorf("expected 'Imported 2 aliases', got: %q", buf.String())
	}
}

// TestImportAliasesFrom_skipExisting does not overwrite aliases already defined
// in the destination config.
func TestImportAliasesFrom_skipExisting(t *testing.T) {
	srcAliases := `  code:
    model: granite3.3:8b
    tags: [code]
  chat:
    model: llama3.2:3b
    tags: [chat]
`
	src := makeSourceWorkspace(t, srcAliases)
	destWS, destCfg := makeDestWorkspace(t)

	// Pre-populate 'code' in dest — it must not be overwritten.
	destCfg.ModelAliases = map[string]ModelAlias{
		"code": {Model: "existing-model:7b", Tags: []string{"code"}},
	}

	var buf bytes.Buffer
	copied, skipped, err := ImportAliasesFrom(src, destWS, destCfg, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if copied != 1 {
		t.Errorf("want copied=1 (only 'chat'), got %d", copied)
	}
	if skipped != 1 {
		t.Errorf("want skipped=1 (only 'code'), got %d", skipped)
	}
	// Original model must be preserved.
	if destCfg.ModelAliases["code"].Model != "existing-model:7b" {
		t.Errorf("'code' alias was overwritten; got model %q", destCfg.ModelAliases["code"].Model)
	}
	if !strings.Contains(buf.String(), "1 skipped") {
		t.Errorf("expected skip count in output, got: %q", buf.String())
	}
}

// TestImportAliasesFrom_directYAMLFile imports aliases from a standalone .yaml
// file rather than a workspace directory.
func TestImportAliasesFrom_directYAMLFile(t *testing.T) {
	yamlContent := `model_aliases:
  fast:
    model: phi3:mini
    tags: [fast]
`
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "aliases.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	destWS, destCfg := makeDestWorkspace(t)
	var buf bytes.Buffer
	copied, skipped, err := ImportAliasesFrom(yamlPath, destWS, destCfg, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if copied != 1 || skipped != 0 {
		t.Errorf("want copied=1 skipped=0, got copied=%d skipped=%d", copied, skipped)
	}
	if destCfg.ModelAliases["fast"].Model != "phi3:mini" {
		t.Errorf("unexpected model: %q", destCfg.ModelAliases["fast"].Model)
	}
}

// TestImportAliasesFrom_nonexistentSource returns an error for a missing path.
func TestImportAliasesFrom_nonexistentSource(t *testing.T) {
	destWS, destCfg := makeDestWorkspace(t)
	var buf bytes.Buffer
	_, _, err := ImportAliasesFrom("/no/such/path/ever", destWS, destCfg, &buf)
	if err == nil {
		t.Fatal("expected an error for non-existent source, got nil")
	}
}

// TestResolveSourceYAML_directory resolves agents/harvey.yaml inside a workspace dir.
func TestResolveSourceYAML_directory(t *testing.T) {
	src := makeSourceWorkspace(t, "")
	got, err := resolveSourceYAML(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(src, "agents", "harvey.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveSourceYAML_yamlFile returns the file path directly for .yaml files.
func TestResolveSourceYAML_yamlFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "export.yaml")
	if err := os.WriteFile(p, []byte("# placeholder\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSourceYAML(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != p {
		t.Errorf("got %q, want %q", got, p)
	}
}
