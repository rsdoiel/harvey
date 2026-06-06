package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListTemplatesBuiltin(t *testing.T) {
	entries := ListTemplates("")
	if len(entries) == 0 {
		t.Fatal("ListTemplates returned no built-in templates")
	}
	wantFiles := []string{
		"backend-developer.fountain",
		"frontend-developer.fountain",
		"dataset-developer.fountain",
		"data-scientist.fountain",
		"technical-writer.fountain",
		"blank.fountain",
	}
	byFile := map[string]TemplateEntry{}
	for _, e := range entries {
		byFile[e.File] = e
	}
	for _, f := range wantFiles {
		e, ok := byFile[f]
		if !ok {
			t.Errorf("expected built-in template %q not found", f)
			continue
		}
		if e.Source != "builtin" {
			t.Errorf("template %q: Source = %q, want %q", f, e.Source, "builtin")
		}
		if e.Name == "" {
			t.Errorf("template %q: Name is empty", f)
		}
	}
}

func TestListTemplatesWorkspaceLocal(t *testing.T) {
	dir := t.TempDir()
	localDir := filepath.Join(dir, "agents", "templates", "profiles")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}

	// A workspace-local template that does not shadow a built-in.
	content := "TITLE: Library Systems\n\nNOTE: For FOLIO and ArchiveSpace work\n\nROLE:\n  Library systems developer.\n"
	if err := os.WriteFile(filepath.Join(localDir, "librarian-systems.fountain"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := ListTemplates(dir)
	found := false
	for _, e := range entries {
		if e.File == "librarian-systems.fountain" {
			found = true
			if e.Source != "workspace" {
				t.Errorf("workspace template Source = %q, want %q", e.Source, "workspace")
			}
			if e.Name != "Library Systems" {
				t.Errorf("Name = %q, want %q", e.Name, "Library Systems")
			}
			if e.Recommended != "For FOLIO and ArchiveSpace work" {
				t.Errorf("Recommended = %q", e.Recommended)
			}
		}
	}
	if !found {
		t.Error("workspace-local template not found in ListTemplates result")
	}
}

func TestListTemplatesWorkspaceShadow(t *testing.T) {
	dir := t.TempDir()
	localDir := filepath.Join(dir, "agents", "templates", "profiles")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Shadow the built-in backend-developer.
	content := "TITLE: Back End Developer (Custom)\n\nROLE:\n  Custom override.\n"
	if err := os.WriteFile(filepath.Join(localDir, "backend-developer.fountain"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := ListTemplates(dir)
	count := 0
	for _, e := range entries {
		if e.File == "backend-developer.fountain" {
			count++
			if e.Source != "workspace" {
				t.Errorf("shadowed template Source = %q, want workspace", e.Source)
			}
			if e.Name != "Back End Developer (Custom)" {
				t.Errorf("Name = %q, want custom override name", e.Name)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for backend-developer.fountain, got %d", count)
	}
}

func TestLoadTemplate(t *testing.T) {
	data, err := LoadTemplate("backend-developer")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	if len(data) == 0 {
		t.Error("LoadTemplate returned empty content")
	}
	if !strings.Contains(string(data), "TITLE:") {
		t.Error("template missing TITLE: field")
	}
}

func TestLoadTemplateWithExtension(t *testing.T) {
	data, err := LoadTemplate("backend-developer.fountain")
	if err != nil {
		t.Fatalf("LoadTemplate with extension: %v", err)
	}
	if len(data) == 0 {
		t.Error("LoadTemplate returned empty content")
	}
}

func TestLoadTemplateNotFound(t *testing.T) {
	_, err := LoadTemplate("nonexistent-template")
	if err == nil {
		t.Error("expected error for nonexistent template, got nil")
	}
}

func TestLoadHelpGuide(t *testing.T) {
	for _, name := range []string{"ollama", "pdf-tools", "getting-started"} {
		data, err := LoadHelpGuide(name)
		if err != nil {
			t.Errorf("LoadHelpGuide(%q): %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("LoadHelpGuide(%q): returned empty content", name)
		}
	}
}

func TestTemplateNoteField(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "inline note",
			content: "TITLE: Test\n\nNOTE: Recommended model: foo\n\nROLE:\n  Developer.\n",
			want:    "Recommended model: foo",
		},
		{
			name:    "multiline note",
			content: "TITLE: Test\n\nNOTE:\n  Recommended model: foo\n  RAG: ingest source\n\nROLE:\n  Developer.\n",
			want:    "Recommended model: foo RAG: ingest source",
		},
		{
			name:    "no note field",
			content: "TITLE: Test\n\nROLE:\n  Developer.\n",
			want:    "",
		},
		{
			name:    "note stops at next field",
			content: "NOTE:\n  first line\n  second line\nROLE:\n  should not appear\n",
			want:    "first line second line",
		},
	}
	for _, tt := range tests {
		got := TemplateNoteField([]byte(tt.content))
		if got != tt.want {
			t.Errorf("TemplateNoteField %q: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestPackageLevelHelpTextVars(t *testing.T) {
	if PDFToolsHelpText == "" {
		t.Error("PDFToolsHelpText is empty; embedded pdf-tools.md failed to load")
	}
	if GettingStartedHelpText == "" {
		t.Error("GettingStartedHelpText is empty; embedded getting-started.md failed to load")
	}
}

func TestAllBuiltinTemplatesHaveTitleAndNote(t *testing.T) {
	entries := ListTemplates("")
	for _, e := range entries {
		if e.Name == "" {
			t.Errorf("template %q: Name is empty", e.File)
		}
		data, err := LoadTemplate(e.File)
		if err != nil {
			t.Errorf("template %q: load error: %v", e.File, err)
			continue
		}
		if !strings.Contains(string(data), "TITLE:") {
			t.Errorf("template %q: missing TITLE: field", e.File)
		}
		if !strings.Contains(string(data), "ROLE:") {
			t.Errorf("template %q: missing ROLE: field", e.File)
		}
	}
}
