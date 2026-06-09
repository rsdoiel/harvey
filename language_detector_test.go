package harvey

import (
	"testing"
)

// ─── isTextContent ────────────────────────────────────────────────────────────

func TestIsTextContent_text(t *testing.T) {
	if !isTextContent([]byte("package main\n\nimport \"fmt\"\n")) {
		t.Error("isTextContent: plain Go source should be text")
	}
}

func TestIsTextContent_binary(t *testing.T) {
	data := []byte{0x7f, 0x45, 0x4c, 0x46, 0x00, 0x01} // ELF magic + null
	if isTextContent(data) {
		t.Error("isTextContent: ELF binary should not be text")
	}
}

func TestIsTextContent_empty(t *testing.T) {
	if !isTextContent([]byte{}) {
		t.Error("isTextContent: empty file should be text (no null bytes)")
	}
}

// ─── detectShebang ────────────────────────────────────────────────────────────

func TestDetectShebang_python3(t *testing.T) {
	src := []byte("#!/usr/bin/env python3\nprint('hello')\n")
	lang, ok := detectShebang(src)
	if !ok || lang != "python" {
		t.Errorf("detectShebang python3: got (%q, %v), want (\"python\", true)", lang, ok)
	}
}

func TestDetectShebang_python2(t *testing.T) {
	src := []byte("#!/usr/bin/env python2\nprint 'hello'\n")
	lang, ok := detectShebang(src)
	if !ok || lang != "python" {
		t.Errorf("detectShebang python2: got (%q, %v), want (\"python\", true)", lang, ok)
	}
}

func TestDetectShebang_pythonAbsolute(t *testing.T) {
	src := []byte("#!/usr/bin/python\nimport sys\n")
	lang, ok := detectShebang(src)
	if !ok || lang != "python" {
		t.Errorf("detectShebang /usr/bin/python: got (%q, %v), want (\"python\", true)", lang, ok)
	}
}

func TestDetectShebang_bash(t *testing.T) {
	for _, shebang := range []string{"#!/bin/bash", "#!/usr/bin/env bash", "#!/bin/sh"} {
		src := []byte(shebang + "\necho hello\n")
		lang, ok := detectShebang(src)
		if !ok || lang != "shell" {
			t.Errorf("detectShebang %q: got (%q, %v), want (\"shell\", true)", shebang, lang, ok)
		}
	}
}

func TestDetectShebang_node(t *testing.T) {
	src := []byte("#!/usr/bin/env node\nconsole.log('hi');\n")
	lang, ok := detectShebang(src)
	if !ok || lang != "javascript" {
		t.Errorf("detectShebang node: got (%q, %v), want (\"javascript\", true)", lang, ok)
	}
}

func TestDetectShebang_lisp(t *testing.T) {
	for _, shebang := range []string{"#!/usr/bin/env clisp", "#!/usr/bin/env sbcl"} {
		src := []byte(shebang + "\n(defun hello () nil)\n")
		lang, ok := detectShebang(src)
		if !ok || lang != "lisp" {
			t.Errorf("detectShebang %q: got (%q, %v), want (\"lisp\", true)", shebang, lang, ok)
		}
	}
}

func TestDetectShebang_noShebang(t *testing.T) {
	src := []byte("package main\nfunc main() {}\n")
	_, ok := detectShebang(src)
	if ok {
		t.Error("detectShebang: Go source should not match a shebang")
	}
}

func TestDetectShebang_emptyContent(t *testing.T) {
	_, ok := detectShebang([]byte{})
	if ok {
		t.Error("detectShebang: empty content should return false")
	}
}

func TestDetectShebang_noNewline(t *testing.T) {
	// Shebang on the only line — no trailing newline.
	src := []byte("#!/usr/bin/env python3")
	lang, ok := detectShebang(src)
	if !ok || lang != "python" {
		t.Errorf("detectShebang no-newline: got (%q, %v), want (\"python\", true)", lang, ok)
	}
}

// ─── detectKeywords ───────────────────────────────────────────────────────────

func TestDetectKeywords_go_package(t *testing.T) {
	src := []byte("package main\n\nimport \"fmt\"\n")
	lang, conf := detectKeywords(src)
	if lang != "go" || conf < 0.8 {
		t.Errorf("detectKeywords Go: got (%q, %.2f), want (\"go\", ≥0.8)", lang, conf)
	}
}

func TestDetectKeywords_go_funcMain(t *testing.T) {
	src := []byte("package harvey\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n")
	lang, conf := detectKeywords(src)
	if lang != "go" || conf < 0.9 {
		t.Errorf("detectKeywords Go main: got (%q, %.2f), want (\"go\", ≥0.9)", lang, conf)
	}
}

func TestDetectKeywords_oberon(t *testing.T) {
	src := []byte("MODULE Foo;\nIMPORT In;\nBEGIN\n  In.Open\nEND Foo.\n")
	lang, conf := detectKeywords(src)
	if lang != "oberon" || conf < 0.8 {
		t.Errorf("detectKeywords Oberon: got (%q, %.2f), want (\"oberon\", ≥0.8)", lang, conf)
	}
}

func TestDetectKeywords_pascal(t *testing.T) {
	src := []byte("program HelloWorld;\nuses crt;\nbegin\n  writeln('Hello');\nend.\n")
	lang, conf := detectKeywords(src)
	if lang != "pascal" || conf < 0.8 {
		t.Errorf("detectKeywords Pascal: got (%q, %.2f), want (\"pascal\", ≥0.8)", lang, conf)
	}
}

func TestDetectKeywords_lisp(t *testing.T) {
	src := []byte(";; Example\n(defun greet (name)\n  (format t \"Hello ~a\" name))\n")
	lang, conf := detectKeywords(src)
	if lang != "lisp" || conf < 0.9 {
		t.Errorf("detectKeywords Lisp: got (%q, %.2f), want (\"lisp\", ≥0.9)", lang, conf)
	}
}

func TestDetectKeywords_c_include(t *testing.T) {
	src := []byte("#include <stdio.h>\n\nint main(int argc, char *argv[]) {\n  return 0;\n}\n")
	lang, conf := detectKeywords(src)
	if lang != "c" || conf < 0.8 {
		t.Errorf("detectKeywords C: got (%q, %.2f), want (\"c\", ≥0.8)", lang, conf)
	}
}

func TestDetectKeywords_cpp_namespace(t *testing.T) {
	src := []byte("#include <iostream>\nnamespace foo {\nvoid hello() {}\n} // namespace foo\n")
	lang, conf := detectKeywords(src)
	if lang != "cpp" || conf < 0.9 {
		t.Errorf("detectKeywords C++: got (%q, %.2f), want (\"cpp\", ≥0.9)", lang, conf)
	}
}

func TestDetectKeywords_basic(t *testing.T) {
	src := []byte("DIM x AS INTEGER\nSUB Hello()\n  PRINT \"Hi\"\nEND SUB\n")
	lang, conf := detectKeywords(src)
	if lang != "basic" || conf < 0.8 {
		t.Errorf("detectKeywords Basic: got (%q, %.2f), want (\"basic\", ≥0.8)", lang, conf)
	}
}

func TestDetectKeywords_html(t *testing.T) {
	src := []byte("<!DOCTYPE html>\n<html>\n<body>Hello</body>\n</html>\n")
	lang, conf := detectKeywords(src)
	if lang != "html" || conf < 0.9 {
		t.Errorf("detectKeywords HTML: got (%q, %.2f), want (\"html\", ≥0.9)", lang, conf)
	}
}

func TestDetectKeywords_sql(t *testing.T) {
	src := []byte("CREATE TABLE users (\n  id INTEGER PRIMARY KEY\n);\n")
	lang, conf := detectKeywords(src)
	if lang != "sql" || conf < 0.9 {
		t.Errorf("detectKeywords SQL: got (%q, %.2f), want (\"sql\", ≥0.9)", lang, conf)
	}
}

func TestDetectKeywords_noMatch(t *testing.T) {
	src := []byte("This is just plain text with no code at all.\n")
	lang, conf := detectKeywords(src)
	if lang != "" || conf != 0 {
		t.Errorf("detectKeywords plain text: got (%q, %.2f), want (\"\", 0)", lang, conf)
	}
}

func TestDetectKeywords_truncatesLargeFile(t *testing.T) {
	// Keyword at byte 0; remainder is padding.  Should still detect.
	padding := make([]byte, maxKeywordScan*2)
	for i := range padding {
		padding[i] = 'x'
	}
	src := append([]byte("package main\n"), padding...)
	lang, conf := detectKeywords(src)
	if lang != "go" || conf < 0.8 {
		t.Errorf("detectKeywords large file: got (%q, %.2f), want (\"go\", ≥0.8)", lang, conf)
	}
}

// ─── ExtensionDetector ────────────────────────────────────────────────────────

func TestExtensionDetector_exactMatch(t *testing.T) {
	d := NewExtensionDetector("pascal", []string{".pas", ".p"})
	for _, ext := range []string{".pas", ".p"} {
		lang, conf := d.Detect("prog"+ext, nil)
		if lang != "pascal" || conf != 1.0 {
			t.Errorf("ExtensionDetector pascal %q: got (%q, %.1f), want (\"pascal\", 1.0)", ext, lang, conf)
		}
	}
}

func TestExtensionDetector_noMatch(t *testing.T) {
	d := NewExtensionDetector("pascal", []string{".pas", ".p"})
	lang, conf := d.Detect("main.go", nil)
	if lang != "" || conf != 0 {
		t.Errorf("ExtensionDetector pascal on .go: got (%q, %.1f), want (\"\", 0)", lang, conf)
	}
}

func TestExtensionDetector_caseInsensitive(t *testing.T) {
	// Oberon: .Mod is registered; .mod (lowercase) should also match via fold.
	d := NewExtensionDetector("oberon", []string{".Mod", ".obn"})
	lang, conf := d.Detect("Hello.mod", nil) // lowercase
	if lang != "oberon" || conf != 1.0 {
		t.Errorf("ExtensionDetector oberon .mod (lowercase): got (%q, %.1f), want (\"oberon\", 1.0)", lang, conf)
	}
}

func TestExtensionDetector_DetectFromExtension(t *testing.T) {
	d := NewExtensionDetector("lisp", []string{".lisp", ".lsp", ".cl", ".el"})
	for _, ext := range []string{".lisp", ".lsp", ".cl", ".el"} {
		lang, ok := d.DetectFromExtension(ext)
		if !ok || lang != "lisp" {
			t.Errorf("ExtensionDetector.DetectFromExtension(%q): got (%q, %v), want (\"lisp\", true)", ext, lang, ok)
		}
	}
	_, ok := d.DetectFromExtension(".go")
	if ok {
		t.Error("ExtensionDetector.DetectFromExtension(\".go\") should be false for lisp detector")
	}
}

// ─── ContentDetector ──────────────────────────────────────────────────────────

func TestContentDetector_shebang_match(t *testing.T) {
	d := NewContentDetector("python")
	lang, conf := d.Detect("script", []byte("#!/usr/bin/env python3\nprint('hi')\n"))
	if lang != "python" || conf < 0.8 {
		t.Errorf("ContentDetector python shebang: got (%q, %.2f), want (\"python\", ≥0.8)", lang, conf)
	}
}

func TestContentDetector_shebang_wrongLang(t *testing.T) {
	// Python shebang should not match a Lisp detector.
	d := NewContentDetector("lisp")
	lang, conf := d.Detect("script", []byte("#!/usr/bin/env python3\nprint('hi')\n"))
	if lang != "" || conf != 0 {
		t.Errorf("ContentDetector lisp on python shebang: got (%q, %.2f), want (\"\", 0)", lang, conf)
	}
}

func TestContentDetector_keyword_match(t *testing.T) {
	d := NewContentDetector("oberon")
	lang, conf := d.Detect("", []byte("MODULE Foo;\nBEGIN\nEND Foo.\n"))
	if lang != "oberon" || conf < 0.8 {
		t.Errorf("ContentDetector oberon keyword: got (%q, %.2f), want (\"oberon\", ≥0.8)", lang, conf)
	}
}

func TestContentDetector_binary(t *testing.T) {
	d := NewContentDetector("go")
	lang, conf := d.Detect("prog", []byte{0x00, 0x01, 0x02, 0x03})
	if lang != "" || conf != 0 {
		t.Errorf("ContentDetector binary: got (%q, %.2f), want (\"\", 0)", lang, conf)
	}
}

func TestContentDetector_DetectFromExtension_alwaysFalse(t *testing.T) {
	d := NewContentDetector("go")
	lang, ok := d.DetectFromExtension(".go")
	if ok || lang != "" {
		t.Error("ContentDetector.DetectFromExtension should always return false")
	}
}

// ─── CombinedDetector ────────────────────────────────────────────────────────

func TestCombinedDetector_extensionTakesPriority(t *testing.T) {
	// File has a .py extension but also contains a bash shebang —
	// extension should win.
	d := NewCombinedDetector("python", []string{".py"})
	lang, conf := d.Detect("script.py", []byte("#!/bin/bash\necho hi\n"))
	if lang != "python" || conf != 1.0 {
		t.Errorf("CombinedDetector extension priority: got (%q, %.1f), want (\"python\", 1.0)", lang, conf)
	}
}

func TestCombinedDetector_contentFallback(t *testing.T) {
	// No extension — falls back to shebang.
	d := NewCombinedDetector("python", []string{".py"})
	lang, conf := d.Detect("script", []byte("#!/usr/bin/env python3\nprint('hi')\n"))
	if lang != "python" || conf < 0.8 {
		t.Errorf("CombinedDetector content fallback: got (%q, %.2f), want (\"python\", ≥0.8)", lang, conf)
	}
}

func TestCombinedDetector_noMatch(t *testing.T) {
	d := NewCombinedDetector("python", []string{".py"})
	lang, conf := d.Detect("Makefile", []byte("all:\n\t@echo done\n"))
	if lang != "" || conf != 0 {
		t.Errorf("CombinedDetector no match: got (%q, %.2f), want (\"\", 0)", lang, conf)
	}
}

// ─── LanguageRegistry.DetectLanguage ─────────────────────────────────────────

func TestRegistryDetectLanguage_extensionGo(t *testing.T) {
	r := freshRegistry()
	lang, conf := r.DetectLanguage("main.go", []byte("package main\n"))
	if lang != "go" || conf != 1.0 {
		t.Errorf("DetectLanguage .go: got (%q, %.1f), want (\"go\", 1.0)", lang, conf)
	}
}

func TestRegistryDetectLanguage_oberonCapital(t *testing.T) {
	r := freshRegistry()
	lang, conf := r.DetectLanguage("Hello.Mod", []byte("MODULE Hello;\nEND Hello.\n"))
	if lang != "oberon" || conf != 1.0 {
		t.Errorf("DetectLanguage .Mod: got (%q, %.1f), want (\"oberon\", 1.0)", lang, conf)
	}
}

func TestRegistryDetectLanguage_shebangFallback(t *testing.T) {
	r := freshRegistry()
	// No extension — shebang should identify Python.
	lang, conf := r.DetectLanguage("script", []byte("#!/usr/bin/env python3\nprint('hi')\n"))
	if lang != "python" || conf < 0.8 {
		t.Errorf("DetectLanguage shebang python: got (%q, %.2f), want (\"python\", ≥0.8)", lang, conf)
	}
}

func TestRegistryDetectLanguage_keywordFallback(t *testing.T) {
	r := freshRegistry()
	// No extension, no shebang — keywords identify Oberon.
	lang, conf := r.DetectLanguage("Module", []byte("MODULE Foo;\nBEGIN\nEND Foo.\n"))
	if lang != "oberon" || conf < 0.8 {
		t.Errorf("DetectLanguage keyword oberon: got (%q, %.2f), want (\"oberon\", ≥0.8)", lang, conf)
	}
}

func TestRegistryDetectLanguage_binary(t *testing.T) {
	r := freshRegistry()
	lang, conf := r.DetectLanguage("prog.go", []byte{0x7f, 0x45, 0x4c, 0x46, 0x00})
	if lang != "" || conf != 0 {
		t.Errorf("DetectLanguage binary: got (%q, %.2f), want (\"\", 0)", lang, conf)
	}
}

func TestRegistryDetectLanguage_unknownExtension(t *testing.T) {
	r := freshRegistry()
	lang, conf := r.DetectLanguage("file.xyz", []byte("nothing special here"))
	if lang != "" || conf != 0 {
		t.Errorf("DetectLanguage unknown ext: got (%q, %.2f), want (\"\", 0)", lang, conf)
	}
}

func TestRegistryDetectLanguage_unregisteredShebang(t *testing.T) {
	// Ruby shebang is not in our registry — should return "" not "ruby".
	r := freshRegistry()
	lang, conf := r.DetectLanguage("script", []byte("#!/usr/bin/env ruby\nputs 'hi'\n"))
	if lang != "" || conf != 0 {
		t.Errorf("DetectLanguage unregistered shebang: got (%q, %.2f), want (\"\", 0)", lang, conf)
	}
}

func TestRegistryDetectLanguage_allDetectorsRegistered(t *testing.T) {
	// Phase 2: every language now has a non-nil detector.
	r := freshRegistry()
	for _, id := range r.LanguageIDs() {
		if r.GetDetector(id) == nil {
			t.Errorf("language %q has no detector registered in Phase 2", id)
		}
	}
}
