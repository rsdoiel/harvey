package harvey

import (
	"strings"
	"testing"
)

// ─── langHighlighter unit tests ───────────────────────────────────────────────

func TestHighlight_CKeyword(t *testing.T) {
	h := &langHighlighter{
		style:    defaultHighlightStyle(),
		keywords: cHighlightKeywords(),
		lineComment1: "//", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, sqStrings: true,
	}
	result := h.highlight("void foo(int x) { return x; }")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Keyword+"void") {
		t.Errorf("expected keyword color before 'void', got: %q", result)
	}
	if !strings.Contains(result, style.Keyword+"int") {
		t.Errorf("expected keyword color before 'int', got: %q", result)
	}
	if !strings.Contains(result, style.Keyword+"return") {
		t.Errorf("expected keyword color before 'return', got: %q", result)
	}
}

func TestHighlight_CLineComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: cHighlightKeywords(),
		lineComment1: "//", dqStrings: true,
	}
	result := h.highlight("int x; // comment here")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"// comment here") {
		t.Errorf("expected comment color on '// comment here', got: %q", result)
	}
}

func TestHighlight_CBlockComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: cHighlightKeywords(),
		blockOpen1: "/*", blockClose1: "*/",
	}
	result := h.highlight("/* hello world */")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"/* hello world */") {
		t.Errorf("expected block comment colored, got: %q", result)
	}
}

func TestHighlight_CString(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: cHighlightKeywords(),
		dqStrings: true,
	}
	result := h.highlight(`printf("hello");`)
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.String+`"hello"`) {
		t.Errorf("expected string color around 'hello', got: %q", result)
	}
}

func TestHighlight_CNumber(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: cHighlightKeywords(),
	}
	result := h.highlight("int x = 42;")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Number+"42") {
		t.Errorf("expected number color before '42', got: %q", result)
	}
}

func TestHighlight_CMultiLineBlockComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: cHighlightKeywords(),
		blockOpen1: "/*", blockClose1: "*/",
	}
	result := h.highlight("/* line1\nline2 */")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"/* line1\nline2 */") {
		t.Errorf("expected multi-line block comment colored, got: %q", result)
	}
}

func TestHighlight_PascalKeywordAndBraceComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: pascalKeywords(),
		lineComment1: "//",
		blockOpen1: "(*", blockClose1: "*)",
		blockOpen2: "{", blockClose2: "}",
		sqStrings: true,
	}
	result := h.highlight("procedure Foo; { a comment } begin end;")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Keyword+"procedure") {
		t.Errorf("expected keyword color before 'procedure', got: %q", result)
	}
	if !strings.Contains(result, style.Comment+"{ a comment }") {
		t.Errorf("expected comment color around '{ a comment }', got: %q", result)
	}
}

func TestHighlight_PascalCompilerDirectiveNotColored(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: pascalKeywords(),
		blockOpen2: "{", blockClose2: "}",
		sqStrings: true,
	}
	result := h.highlight("{$MODE DELPHI}")
	style := defaultHighlightStyle()
	// Compiler directive {$...} must NOT be highlighted as a comment.
	if strings.Contains(result, style.Comment+"{$") {
		t.Errorf("compiler directive should not be highlighted as comment, got: %q", result)
	}
}

func TestHighlight_PascalParenStarComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: pascalKeywords(),
		blockOpen1: "(*", blockClose1: "*)",
	}
	result := h.highlight("(* pascal block *)")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"(* pascal block *)") {
		t.Errorf("expected (* *) comment colored, got: %q", result)
	}
}

func TestHighlight_OberonKeyword(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: oberonKeywords(),
		blockOpen1: "(*", blockClose1: "*)",
		dqStrings: true,
	}
	result := h.highlight("MODULE Foo; PROCEDURE Bar; BEGIN END Bar; END Foo.")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Keyword+"MODULE") {
		t.Errorf("expected keyword color before 'MODULE', got: %q", result)
	}
	if !strings.Contains(result, style.Keyword+"PROCEDURE") {
		t.Errorf("expected keyword color before 'PROCEDURE', got: %q", result)
	}
}

func TestHighlight_LispLineComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: lispKeywords(),
		lineComment1: ";",
		blockOpen1: "#|", blockClose1: "|#",
		dqStrings: true,
	}
	result := h.highlight("(defun foo () ; a comment\n  42)")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"; a comment") {
		t.Errorf("expected comment color on '; a comment', got: %q", result)
	}
}

func TestHighlight_LispBlockComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: lispKeywords(),
		blockOpen1: "#|", blockClose1: "|#",
		dqStrings: true,
	}
	result := h.highlight("#| block comment |#")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"#| block comment |#") {
		t.Errorf("expected block comment colored, got: %q", result)
	}
}

func TestHighlight_LispKeyword(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: lispKeywords(),
		lineComment1: ";", dqStrings: true,
	}
	result := h.highlight("(defun square (x) (* x x))")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Keyword+"defun") {
		t.Errorf("expected keyword color before 'defun', got: %q", result)
	}
}

func TestHighlight_BasicREMComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: basicKeywords(),
		lineComment3: "REM", lineComment2: "'",
		dqStrings: true,
	}
	result := h.highlight("REM this is a remark\n")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"REM this is a remark") {
		t.Errorf("expected REM comment colored, got: %q", result)
	}
}

func TestHighlight_BasicSingleQuoteComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: basicKeywords(),
		lineComment2: "'", lineComment3: "REM",
		dqStrings: true,
	}
	result := h.highlight("DIM x AS INTEGER ' declare x\n")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"' declare x") {
		t.Errorf("expected single-quote comment colored, got: %q", result)
	}
}

func TestHighlight_GoKeywords(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: goKeywords(),
		lineComment1: "//", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, btStrings: true,
	}
	result := h.highlight("func main() { var x int = 42 }")
	style := defaultHighlightStyle()
	for _, kw := range []string{"func", "var", "int"} {
		if !strings.Contains(result, style.Keyword+kw) {
			t.Errorf("expected keyword color before %q, got: %q", kw, result)
		}
	}
}

func TestHighlight_GoBacktickString(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: goKeywords(),
		lineComment1: "//", btStrings: true,
	}
	result := h.highlight("s := `raw string`")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.String+"`") {
		t.Errorf("expected string color for backtick string, got: %q", result)
	}
}

func TestHighlight_PythonLineComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: pythonKeywords(),
		lineComment1: "#", dqStrings: true, sqStrings: true,
	}
	result := h.highlight("x = 1 # python comment")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"# python comment") {
		t.Errorf("expected comment color on Python comment, got: %q", result)
	}
}

func TestHighlight_ShellHashComment(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: shellKeywords(),
		lineComment1: "#", dqStrings: true, sqStrings: true,
	}
	result := h.highlight("#!/bin/bash\necho hello # greet")
	style := defaultHighlightStyle()
	if !strings.Contains(result, style.Comment+"#!/bin/bash") {
		t.Errorf("expected shebang colored as comment, got: %q", result)
	}
}

func TestHighlight_SQLKeywords(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: sqlKeywords(),
		lineComment1: "--", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, sqStrings: true,
	}
	result := h.highlight("SELECT id FROM users WHERE active = true;")
	style := defaultHighlightStyle()
	for _, kw := range []string{"SELECT", "FROM", "WHERE"} {
		if !strings.Contains(result, style.Keyword+kw) {
			t.Errorf("expected keyword color before %q, got: %q", kw, result)
		}
	}
}

func TestHighlight_RustKeywords(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: rustKeywords(),
		lineComment1: "//", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, sqStrings: true,
	}
	result := h.highlight("fn main() { let x: i32 = 0; }")
	style := defaultHighlightStyle()
	for _, kw := range []string{"fn", "let"} {
		if !strings.Contains(result, style.Keyword+kw) {
			t.Errorf("expected keyword color before %q, got: %q", kw, result)
		}
	}
}

func TestHighlight_NoChange_EmptyContent(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: goKeywords(),
		lineComment1: "//",
	}
	if got := h.highlight(""); got != "" {
		t.Errorf("empty input should return empty, got: %q", got)
	}
}

func TestHighlight_UnclosedString_ResetAtEnd(t *testing.T) {
	h := &langHighlighter{
		style: defaultHighlightStyle(), keywords: goKeywords(),
		dqStrings: true,
	}
	result := h.highlight(`"unclosed`)
	style := defaultHighlightStyle()
	// Must end with reset even for unclosed string.
	if !strings.HasSuffix(result, style.Reset) {
		t.Errorf("result should end with reset for unclosed string, got: %q", result)
	}
}

// ─── highlightCodeBlocks tests ────────────────────────────────────────────────

func TestHighlightCodeBlocks_NoFence(t *testing.T) {
	text := "Just some plain text with no code blocks."
	if got := highlightCodeBlocks(text); got != text {
		t.Errorf("no-fence text should be returned unchanged, got: %q", got)
	}
}

func TestHighlightCodeBlocks_UnknownLang(t *testing.T) {
	text := "```unknownlang\nsome code\n```"
	got := highlightCodeBlocks(text)
	// Body should be unchanged since the language is not registered.
	if !strings.Contains(got, "some code") {
		t.Errorf("expected body preserved for unknown lang, got: %q", got)
	}
}

func TestHighlightCodeBlocks_GoBlock(t *testing.T) {
	text := "Here is code:\n```go\nfunc foo() {}\n```\nDone."
	got := highlightCodeBlocks(text)
	style := defaultHighlightStyle()
	if !strings.Contains(got, style.Keyword+"func") {
		t.Errorf("expected 'func' highlighted in Go block, got: %q", got)
	}
	// Surrounding text must be preserved.
	if !strings.Contains(got, "Here is code:") {
		t.Errorf("text before fence should be preserved, got: %q", got)
	}
	if !strings.Contains(got, "Done.") {
		t.Errorf("text after fence should be preserved, got: %q", got)
	}
}

func TestHighlightCodeBlocks_CppAlias(t *testing.T) {
	text := "```c++\nvoid foo(){}\n```"
	got := highlightCodeBlocks(text)
	style := defaultHighlightStyle()
	if !strings.Contains(got, style.Keyword+"void") {
		t.Errorf("c++ alias should resolve to cpp highlighter, got: %q", got)
	}
}

func TestHighlightCodeBlocks_BashAlias(t *testing.T) {
	text := "```bash\n#!/bin/bash\necho hi\n```"
	got := highlightCodeBlocks(text)
	style := defaultHighlightStyle()
	// Shebang is a comment in shell.
	if !strings.Contains(got, style.Comment+"#!/bin/bash") {
		t.Errorf("bash alias should resolve to shell highlighter, got: %q", got)
	}
}

func TestHighlightCodeBlocks_FenceMarkersPreserved(t *testing.T) {
	text := "```go\nvar x int\n```"
	got := highlightCodeBlocks(text)
	if !strings.HasPrefix(got, "```go") {
		t.Errorf("opening fence marker should be preserved, got: %q", got)
	}
	if !strings.HasSuffix(got, "```") {
		t.Errorf("closing fence marker should be preserved, got: %q", got)
	}
}

func TestHighlightCodeBlocks_MultipleBlocks(t *testing.T) {
	text := "```go\nfunc a(){}\n```\ntext\n```python\ndef b(): pass\n```"
	got := highlightCodeBlocks(text)
	style := defaultHighlightStyle()
	if !strings.Contains(got, style.Keyword+"func") {
		t.Errorf("go block should be highlighted, got: %q", got)
	}
	if !strings.Contains(got, style.Keyword+"def") {
		t.Errorf("python block should be highlighted, got: %q", got)
	}
	if !strings.Contains(got, "text") {
		t.Errorf("inter-block text should be preserved, got: %q", got)
	}
}

func TestHighlightCodeBlocks_TrailingNewlinePreserved(t *testing.T) {
	text := "```go\nfunc a(){}\n```\n"
	got := highlightCodeBlocks(text)
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("trailing newline should be preserved, got: %q", got)
	}
}

func TestHighlightCodeBlocks_EmptyFence(t *testing.T) {
	text := "```go\n```"
	got := highlightCodeBlocks(text)
	if !strings.Contains(got, "```go") || !strings.Contains(got, "```") {
		t.Errorf("empty fence should still contain markers, got: %q", got)
	}
}

// ─── applyHighlighting tests ──────────────────────────────────────────────────

func TestApplyHighlighting_UnknownLang(t *testing.T) {
	content := "some code"
	if got := applyHighlighting(content, "cobol"); got != content {
		t.Errorf("unknown lang should return content unchanged, got: %q", got)
	}
}

func TestApplyHighlighting_CppAlias(t *testing.T) {
	content := "void foo(){}"
	got := applyHighlighting(content, "c++")
	style := defaultHighlightStyle()
	if !strings.Contains(got, style.Keyword+"void") {
		t.Errorf("c++ alias should resolve to cpp highlighter, got: %q", got)
	}
}

func TestApplyHighlighting_PyAlias(t *testing.T) {
	content := "def foo(): pass"
	got := applyHighlighting(content, "py")
	style := defaultHighlightStyle()
	if !strings.Contains(got, style.Keyword+"def") {
		t.Errorf("py alias should resolve to python highlighter, got: %q", got)
	}
}

func TestApplyHighlighting_RsAlias(t *testing.T) {
	content := "fn main(){}"
	got := applyHighlighting(content, "rs")
	style := defaultHighlightStyle()
	if !strings.Contains(got, style.Keyword+"fn") {
		t.Errorf("rs alias should resolve to rust highlighter, got: %q", got)
	}
}

// ─── Registry wiring tests ────────────────────────────────────────────────────

func TestInitHighlighters_GlobalRegistry(t *testing.T) {
	for _, id := range []string{
		"c", "cpp", "pascal", "oberon", "lisp", "basic",
		"go", "python", "javascript", "typescript", "rust",
		"shell", "sql",
	} {
		h := globalRegistry.GetHighlighter(id)
		if h == nil {
			t.Errorf("globalRegistry.GetHighlighter(%q) = nil, want non-nil", id)
		}
	}
}

func TestTerminalHighlighter_GetStyle(t *testing.T) {
	h := NewTerminalHighlighter()
	for _, id := range []string{"c", "go", "python"} {
		if style := h.GetStyle(id); style == nil {
			t.Errorf("GetStyle(%q) = nil, want non-nil", id)
		}
	}
	if style := h.GetStyle("notregistered"); style != nil {
		t.Errorf("GetStyle(unknown) should return nil, got %v", style)
	}
}

func TestTerminalHighlighter_Highlight_UnknownLang(t *testing.T) {
	h := NewTerminalHighlighter()
	content := "some code"
	if got := h.Highlight(content, "cobol"); got != content {
		t.Errorf("unknown lang should return content unchanged, got: %q", got)
	}
}

func TestSetHighlighter_Registry(t *testing.T) {
	r := NewLanguageRegistry()
	initLanguages(r)
	h := NewTerminalHighlighter()
	r.SetHighlighter("go", h)
	if got := r.GetHighlighter("go"); got == nil {
		t.Error("GetHighlighter('go') should return the registered highlighter")
	}
}
