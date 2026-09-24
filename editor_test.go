package harvey

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// H3 (knowledge-learning-mode-plan.md): editTextInEditor is the $EDITOR round
// trip that editInEditor did for a *MemoryDoc only, extracted so learning
// mode can edit a plain-text summary the same way. These tests fake $EDITOR
// with a small shell script that rewrites the file, records what it was
// given, or fails.

// fakeEditor writes script as an executable shell script, points $EDITOR at
// it, and returns its path. The script receives the file to edit as $1.
func fakeEditor(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake editor is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "fake-editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("EDITOR", path)
	return path
}

func TestEditTextInEditor_ReturnsWhatTheEditorWrote(t *testing.T) {
	fakeEditor(t, `printf 'rewritten by the editor\n' > "$1"`)
	got, err := editTextInEditor("the original text\n", ".md")
	if err != nil {
		t.Fatalf("editTextInEditor: %v", err)
	}
	if got != "rewritten by the editor\n" {
		t.Errorf("got %q, want the editor's output", got)
	}
}

func TestEditTextInEditor_HandsTheEditorTheOriginalText(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "seen.txt")
	fakeEditor(t, `cp "$1" "`+seen+`"`)
	const in = "line one\n\nline three, after a blank line\n"
	if _, err := editTextInEditor(in, ".md"); err != nil {
		t.Fatalf("editTextInEditor: %v", err)
	}
	got, err := os.ReadFile(seen)
	if err != nil || string(got) != in {
		t.Errorf("the editor was given %q (%v), want %q", got, err, in)
	}
}

func TestEditTextInEditor_UnchangedWhenTheEditorChangesNothing(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true(1) on this system")
	}
	t.Setenv("EDITOR", truePath)
	// Multi-paragraph, trailing spaces and non-ASCII must survive byte for byte.
	const in = "Para one, with trailing spaces   \n\nPara two: café 世界\n\n\n- a list\n- of items\n"
	got, err := editTextInEditor(in, ".md")
	if err != nil || got != in {
		t.Errorf("got %q, %v; want the text back unchanged", got, err)
	}
}

func TestEditTextInEditor_UsesTheRequestedFileExtension(t *testing.T) {
	for _, ext := range []string{".md", ".fountain"} {
		named := filepath.Join(t.TempDir(), "name.txt")
		fakeEditor(t, `printf '%s' "$1" > "`+named+`"`)
		if _, err := editTextInEditor("x\n", ext); err != nil {
			t.Fatalf("editTextInEditor(%q): %v", ext, err)
		}
		got, _ := os.ReadFile(named)
		if !strings.HasSuffix(string(got), ext) {
			t.Errorf("the editor was given %q, want a file ending in %q", got, ext)
		}
	}
}

func TestEditTextInEditor_RemovesItsTempFileAfterwards(t *testing.T) {
	named := filepath.Join(t.TempDir(), "name.txt")
	fakeEditor(t, `printf '%s' "$1" > "`+named+`"`)
	if _, err := editTextInEditor("x\n", ".md"); err != nil {
		t.Fatalf("editTextInEditor: %v", err)
	}
	path, _ := os.ReadFile(named)
	if len(path) == 0 {
		t.Fatal("the editor never ran, so this test would prove nothing")
	}
	if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
		t.Errorf("temp file %q still exists after the edit (%v)", path, err)
	}
}

func TestEditTextInEditor_AnEditorFailureIsAnErrorNamingTheEditorAndLeavesNoTempFile(t *testing.T) {
	named := filepath.Join(t.TempDir(), "name.txt")
	editor := fakeEditor(t, `printf '%s' "$1" > "`+named+`"; exit 3`)
	got, err := editTextInEditor("keep me\n", ".md")
	if err == nil {
		t.Fatal("editTextInEditor with a failing editor = nil error, want one")
	}
	if !strings.Contains(err.Error(), editor) {
		t.Errorf("error = %q, want it to name the editor %q", err, editor)
	}
	if got != "" {
		t.Errorf("got %q on failure, want no text: a failed edit must not look like an empty one", got)
	}
	path, _ := os.ReadFile(named)
	if len(path) == 0 {
		t.Fatal("the editor never ran, so this test would prove nothing")
	}
	if _, statErr := os.Stat(string(path)); !os.IsNotExist(statErr) {
		t.Errorf("temp file %q left behind after a failed edit", path)
	}
}

// editInEditor (the memory-document editor) now sits on the shared primitive;
// it must behave exactly as before.
func TestEditInEditor_StillRoundTripsAMemoryDoc(t *testing.T) {
	fakeEditor(t, `sed 's/ORIGINAL/CHANGED/' "$1" > "$1.new" && mv "$1.new" "$1"`)
	doc := &MemoryDoc{
		Meta:         MemoryMeta{ID: "edit_roundtrip_001", Type: MemoryTypeToolUse, Confidence: 0.7, Description: "a memory"},
		FountainBody: "FADE IN:\n\nINT. MEMORY - TEST\n\nORIGINAL text here.\n\nTHE END.\n",
	}
	edited, err := editInEditor(doc, os.Stderr)
	if err != nil {
		t.Fatalf("editInEditor: %v", err)
	}
	if !strings.Contains(edited.FountainBody, "CHANGED text here.") || strings.Contains(edited.FountainBody, "ORIGINAL") {
		t.Errorf("body = %q, want the editor's change applied", edited.FountainBody)
	}
	if edited.Meta.ID != "edit_roundtrip_001" || edited.Meta.Type != MemoryTypeToolUse || edited.Meta.Description != "a memory" {
		t.Errorf("meta = %+v, want the front matter carried through", edited.Meta)
	}
}

func TestEditInEditor_AnEditorFailureIsStillAnError(t *testing.T) {
	fakeEditor(t, `exit 1`)
	doc := &MemoryDoc{Meta: MemoryMeta{ID: "x", Type: MemoryTypeToolUse}, FountainBody: "FADE IN:\n"}
	if _, err := editInEditor(doc, os.Stderr); err == nil {
		t.Error("editInEditor with a failing editor = nil error, want one")
	}
}

// ─── $EDITOR with arguments (found 2026-09-24 while extracting H3) ───────────
//
// findEditor returned the whole $EDITOR string and exec.Command took it as one
// program name, so a common setting like `code --wait`, `subl -w` or
// `emacsclient -t` failed with "executable file not found". The string is now
// split on whitespace: the first field is the program, the rest come before
// the file.

func TestEditorCommand_SplitsArgumentsAheadOfTheFile(t *testing.T) {
	for _, tc := range []struct {
		editor string
		want   []string
	}{
		{"code --wait", []string{"code", "--wait", "/tmp/x.md"}},
		{"emacsclient -t -a vi", []string{"emacsclient", "-t", "-a", "vi", "/tmp/x.md"}},
		{"  code    --wait  ", []string{"code", "--wait", "/tmp/x.md"}},
		{"vi", []string{"vi", "/tmp/x.md"}},
	} {
		got := editorCommand(tc.editor, "/tmp/x.md").Args
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("editorCommand(%q).Args = %q, want %q", tc.editor, got, tc.want)
		}
	}
}

func TestFindEditor_ABlankEditorFallsBackRatherThanReturningNothing(t *testing.T) {
	for _, blank := range []string{"   ", "\t", " \n "} {
		t.Setenv("EDITOR", blank)
		if got := findEditor(); strings.TrimSpace(got) == "" {
			t.Errorf("findEditor with EDITOR=%q = %q, want a real fallback editor", blank, got)
		}
	}
}

func TestEditTextInEditor_PassesEditorArgumentsThroughAheadOfTheFile(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "args.txt")
	script := fakeEditor(t, `printf '%s\n' "$@" > "`+seen+`"; printf 'edited\n' > "$3"`)
	t.Setenv("EDITOR", script+" --flag value")

	got, err := editTextInEditor("original\n", ".md")
	if err != nil {
		t.Fatalf("editTextInEditor with an editor that takes arguments: %v", err)
	}
	if got != "edited\n" {
		t.Errorf("got %q, want the edit to have happened", got)
	}
	args, _ := os.ReadFile(seen)
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(lines) != 3 || lines[0] != "--flag" || lines[1] != "value" || !strings.HasSuffix(lines[2], ".md") {
		t.Errorf("the editor received %q, want --flag, value, then the file", lines)
	}
}

func TestEditTextInEditor_AnErrorNamesTheWholeEditorCommandIncludingItsArguments(t *testing.T) {
	script := fakeEditor(t, `exit 2`)
	t.Setenv("EDITOR", script+" --wait")
	_, err := editTextInEditor("x\n", ".md")
	if err == nil || !strings.Contains(err.Error(), "--wait") {
		t.Errorf("err = %v, want it to show the editor command as configured, arguments included", err)
	}
}

// memory_onboarding.go had its own copy of the launch code, with the same bug.
func TestEditTemplateRaw_HonoursEditorArguments(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "args.txt")
	script := fakeEditor(t, `printf '%s\n' "$@" > "`+seen+`"; printf 'from the editor\n' > "$2"`)
	t.Setenv("EDITOR", script+" --wait")

	got, err := editTemplateRaw([]byte("template\n"), os.Stderr)
	if err != nil {
		t.Fatalf("editTemplateRaw with an editor that takes arguments: %v", err)
	}
	if string(got) != "from the editor\n" {
		t.Errorf("got %q, want the editor's output", got)
	}
}
