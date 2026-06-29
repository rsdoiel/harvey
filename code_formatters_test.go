package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── normaliseText tests ──────────────────────────────────────────────────────

func TestNormaliseText_TrailingWhitespace(t *testing.T) {
	in := "var x := 1;   \nvar y := 2;\t\n"
	got := normaliseText(in)
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("trailing whitespace not removed from line %q", line)
		}
	}
}

func TestNormaliseText_CRLFNormalised(t *testing.T) {
	in := "line1\r\nline2\r\n"
	got := normaliseText(in)
	if strings.Contains(got, "\r") {
		t.Errorf("CRLF not normalised to LF, got: %q", got)
	}
}

func TestNormaliseText_SingleTrailingNewline(t *testing.T) {
	// Input with no trailing newline.
	got := normaliseText("foo\nbar")
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("result should end with newline, got: %q", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("result should have only one trailing newline, got: %q", got)
	}
}

func TestNormaliseText_MultipleTrailingNewlines(t *testing.T) {
	got := normaliseText("foo\n\n\n\n")
	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("multiple trailing newlines should collapse to one, got: %q", got)
	}
}

func TestNormaliseText_EmptyContent(t *testing.T) {
	got := normaliseText("")
	if got != "\n" {
		t.Errorf("empty content should produce single newline, got: %q", got)
	}
}

func TestNormaliseText_Idempotent(t *testing.T) {
	in := "module Foo;\n\nPROCEDURE Bar;\nBEGIN\nEND Bar;\n\nEND Foo.\n"
	once := normaliseText(in)
	twice := normaliseText(once)
	if once != twice {
		t.Errorf("normaliseText is not idempotent:\nfirst:  %q\nsecond: %q", once, twice)
	}
}

// ─── BuiltinFormatter tests ───────────────────────────────────────────────────

func TestBuiltinFormatter_Pascal(t *testing.T) {
	f := newBuiltinFormatter("pascal", normaliseText)
	in := "procedure Foo;   \nbegin\nend;\n"
	out, err := f.Format(in, "module.pas")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "   ") {
		t.Errorf("trailing spaces not removed, got: %q", out)
	}
}

func TestBuiltinFormatter_Language(t *testing.T) {
	f := newBuiltinFormatter("oberon", normaliseText)
	if f.Language() != "oberon" {
		t.Errorf("Language() = %q, want %q", f.Language(), "oberon")
	}
}

func TestBuiltinFormatter_Mode(t *testing.T) {
	f := newBuiltinFormatter("basic", normaliseText)
	if f.Mode() != PipeFormatter {
		t.Errorf("Mode() = %v, want PipeFormatter", f.Mode())
	}
}

func TestBuiltinFormatter_Check_AlreadyFormatted(t *testing.T) {
	f := newBuiltinFormatter("pascal", normaliseText)
	clean := "procedure Foo;\nbegin\nend;\n"
	ok, issues := f.Check(clean, "")
	if !ok {
		t.Errorf("Check should return true for already-formatted content")
	}
	if len(issues) != 0 {
		t.Errorf("no issues expected for clean content, got %v", issues)
	}
}

func TestBuiltinFormatter_Check_NeedsFormatting(t *testing.T) {
	f := newBuiltinFormatter("pascal", normaliseText)
	dirty := "procedure Foo;   \nbegin\nend;\n"
	ok, issues := f.Check(dirty, "")
	if ok {
		t.Errorf("Check should return false for dirty content")
	}
	if len(issues) == 0 {
		t.Errorf("at least one issue expected for dirty content")
	}
}

// ─── PipeExternalFormatter tests ─────────────────────────────────────────────

func TestPipeExtFormatter_Language(t *testing.T) {
	f := newPipeExtFormatter("go", "gofmt")
	if f.Language() != "go" {
		t.Errorf("Language() = %q, want %q", f.Language(), "go")
	}
}

func TestPipeExtFormatter_Mode(t *testing.T) {
	f := newPipeExtFormatter("go", "gofmt")
	if f.Mode() != PipeFormatter {
		t.Errorf("Mode() = %v, want PipeFormatter", f.Mode())
	}
}

func TestPipeExtFormatter_MissingExe_Passthrough(t *testing.T) {
	// A nonexistent executable should return the original content unchanged.
	f := newPipeExtFormatter("c", "nonexistent-formatter-xyz-123")
	content := "void foo(){}"
	got, err := f.Format(content, "test.c")
	if err != nil {
		t.Fatalf("unexpected error for missing tool: %v", err)
	}
	if got != content {
		t.Errorf("missing tool should return original content, got: %q", got)
	}
}

func TestPipeExtFormatter_Cat_IdentityFormat(t *testing.T) {
	// cat is an identity pipe formatter (output == input).
	f := newPipeExtFormatter("text", "cat")
	content := "hello world\n"
	got, err := f.Format(content, "test.txt")
	if err != nil {
		t.Fatalf("cat formatter error: %v", err)
	}
	if got != content {
		t.Errorf("cat should return content unchanged, got: %q", got)
	}
}

func TestPipeExtFormatter_Check_NoChange(t *testing.T) {
	f := newPipeExtFormatter("text", "cat")
	content := "hello\n"
	ok, _ := f.Check(content, "test.txt")
	if !ok {
		t.Errorf("cat check should report already formatted (identical output)")
	}
}

// ─── FileExternalFormatter tests ─────────────────────────────────────────────

func TestFileExtFormatter_Language(t *testing.T) {
	f := newFileExtFormatter("pascal", "ptop", "{{filepath}}")
	if f.Language() != "pascal" {
		t.Errorf("Language() = %q, want %q", f.Language(), "pascal")
	}
}

func TestFileExtFormatter_Mode(t *testing.T) {
	f := newFileExtFormatter("pascal", "ptop", "{{filepath}}")
	if f.Mode() != FileFormatter {
		t.Errorf("Mode() = %v, want FileFormatter", f.Mode())
	}
}

func TestFileExtFormatter_MissingExe_Passthrough(t *testing.T) {
	f := newFileExtFormatter("pascal", "nonexistent-ptop-xyz-789", "{{filepath}}")
	content := "MODULE Foo; END Foo.\n"
	got, err := f.Format(content, "/tmp/nonexistent.Mod")
	if err != nil {
		t.Fatalf("unexpected error for missing tool: %v", err)
	}
	if got != content {
		t.Errorf("missing tool should return original content, got: %q", got)
	}
}

func TestFileExtFormatter_FilepathInterpolation(t *testing.T) {
	// Use a script that writes a known string to the target path, then verify
	// Format re-reads it.  Requires a writable temp dir.
	dir := t.TempDir()
	target := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use 'sh -c "echo replaced > PATH"' style invocation is unsafe; instead,
	// use a script file approach — or simply test with a formatter that reads the
	// path from the environment.  For portability, we use the built-in cat via a
	// temp script on Unix.
	// If we cannot execute scripts, skip.
	scriptPath := filepath.Join(dir, "formatter.sh")
	script := "#!/bin/sh\nprintf 'formatted\\n' > \"$1\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Skip("cannot write test script")
	}
	f := newFileExtFormatter("text", scriptPath, "{{filepath}}")
	got, err := f.Format("original\n", target)
	if err != nil {
		t.Fatalf("FileExternalFormatter error: %v", err)
	}
	if got != "formatted\n" {
		t.Errorf("expected 'formatted\\n', got: %q", got)
	}
}

// ─── initFormatters / registry wiring ────────────────────────────────────────

func TestInitFormatters_BuiltinLanguages(t *testing.T) {
	for _, id := range []string{"pascal", "oberon", "basic"} {
		f := globalRegistry.GetFormatter(id)
		if f == nil {
			t.Errorf("globalRegistry.GetFormatter(%q) = nil, want built-in formatter", id)
		}
		if f != nil && f.Mode() != PipeFormatter {
			t.Errorf("built-in formatter for %q should be PipeFormatter", id)
		}
	}
}

func TestInitFormatters_ExternalLanguages(t *testing.T) {
	for _, id := range []string{"go", "c", "cpp", "python", "rust", "javascript", "typescript"} {
		f := globalRegistry.GetFormatter(id)
		if f == nil {
			t.Errorf("globalRegistry.GetFormatter(%q) = nil, want external formatter", id)
		}
	}
}

func TestSetFormatter_Registry(t *testing.T) {
	r := NewLanguageRegistry()
	initLanguages(r)
	f := newBuiltinFormatter("pascal", normaliseText)
	r.SetFormatter("pascal", f)
	if got := r.GetFormatter("pascal"); got == nil {
		t.Error("GetFormatter('pascal') should return the registered formatter")
	}
}

// ─── applyAutoFormat integration test ────────────────────────────────────────

func TestApplyAutoFormat_BuiltinPascal(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)

	// Write a Pascal file with trailing whitespace.
	relPath := "module.pas"
	dirty := "MODULE Foo;   \nEND Foo.\n"
	if err := ws.WriteFile(relPath, []byte(dirty), 0o644); err != nil {
		t.Fatal(err)
	}

	note := applyAutoFormat(a, relPath, dirty)
	if note != "formatted" {
		t.Errorf("applyAutoFormat note = %q, want %q", note, "formatted")
	}

	// Verify the file was rewritten without trailing whitespace.
	data, err := ws.ReadFile(relPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "   ") {
		t.Errorf("formatted file still contains trailing whitespace: %q", string(data))
	}
}

func TestApplyAutoFormat_AlreadyFormatted(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)

	relPath := "module.pas"
	clean := "MODULE Foo;\nEND Foo.\n"
	if err := ws.WriteFile(relPath, []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}

	note := applyAutoFormat(a, relPath, clean)
	if note != "already formatted" {
		t.Errorf("applyAutoFormat note = %q, want %q", note, "already formatted")
	}
}

func TestApplyAutoFormat_UnknownExtension(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)
	note := applyAutoFormat(a, "notes.xyz", "some content")
	if note != "" {
		t.Errorf("unknown extension should return empty note, got: %q", note)
	}
}

func TestApplyAutoFormat_FileFormatterSafeModeBlocked(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	cfg := DefaultConfig()
	cfg.Security.SafeMode = true
	a := NewAgent(cfg, ws)

	// Manually register a file-mode formatter for a test language.
	r := globalRegistry
	orig := r.GetFormatter("pascal")
	fileFmt := newFileExtFormatter("pascal", "cat", "{{filepath}}")
	r.SetFormatter("pascal", fileFmt)
	defer r.SetFormatter("pascal", orig) // restore

	note := applyAutoFormat(a, "prog.pas", "MODULE Foo; END Foo.\n")
	// File-mode formatter should be skipped in safe mode.
	if note != "" {
		t.Errorf("file-mode formatter should not run in safe mode, got: %q", note)
	}
}

// ─── /format command tests ────────────────────────────────────────────────────

func TestCmdFormat_NoWorkspace(t *testing.T) {
	a := &Agent{Config: DefaultConfig(), commands: make(map[string]*Command)}
	var sb strings.Builder
	if err := cmdFormat(a, []string{"file.pas"}, &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "requires a workspace") {
		t.Errorf("expected workspace warning, got: %q", sb.String())
	}
}

func TestCmdFormat_NoArgs(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	a := &Agent{Config: DefaultConfig(), Workspace: ws, commands: make(map[string]*Command)}
	var sb strings.Builder
	if err := cmdFormat(a, nil, &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "Usage:") {
		t.Errorf("expected usage message, got: %q", sb.String())
	}
}

func TestCmdFormat_AlreadyFormatted(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)

	clean := "MODULE Foo;\nEND Foo.\n"
	if err := ws.WriteFile("module.Mod", []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := cmdFormat(a, []string{"module.Mod"}, &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "already formatted") {
		t.Errorf("expected 'already formatted', got: %q", sb.String())
	}
}

func TestCmdFormat_FormatsFile(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)

	dirty := "PROCEDURE Foo;   \nBEGIN\nEND Foo;\n"
	if err := ws.WriteFile("prog.pas", []byte(dirty), 0o644); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := cmdFormat(a, []string{"prog.pas"}, &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "formatted") {
		t.Errorf("expected 'formatted' in output, got: %q", sb.String())
	}
	// Verify file changed on disk.
	data, _ := ws.ReadFile("prog.pas")
	if strings.Contains(string(data), "   ") {
		t.Errorf("file should not contain trailing whitespace after format")
	}
}

func TestCmdFormat_UnknownExtension(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	cfg := DefaultConfig()
	a := NewAgent(cfg, ws)

	if err := ws.WriteFile("notes.xyz", []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := cmdFormat(a, []string{"notes.xyz"}, &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "no language registered") {
		t.Errorf("expected 'no language registered', got: %q", sb.String())
	}
}
