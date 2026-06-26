package harvey

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── CDocExtractor tests ──────────────────────────────────────────────────────

func TestCDocExtractor_lineComment_noSymbol(t *testing.T) {
	src := "// standalone comment\n\nint x = 0;\n"
	e := NewCDocExtractor("c")
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Content != "standalone comment" {
		t.Errorf("Content = %q", docs[0].Content)
	}
	if docs[0].Symbol != "" {
		t.Errorf("Symbol = %q, want empty", docs[0].Symbol)
	}
	if docs[0].Type != "line_comment" {
		t.Errorf("Type = %q, want line_comment", docs[0].Type)
	}
}

func TestCDocExtractor_lineComment_symbolAssoc(t *testing.T) {
	src := "// Frobnicates the widget.\nvoid frobnicate(Widget *w) {}\n"
	e := NewCDocExtractor("c")
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "frobnicate" {
		t.Errorf("Symbol = %q, want frobnicate", docs[0].Symbol)
	}
	if docs[0].Content != "Frobnicates the widget." {
		t.Errorf("Content = %q", docs[0].Content)
	}
}

func TestCDocExtractor_multiLineComment(t *testing.T) {
	src := "// Line one.\n// Line two.\nstatic int helper(int x) {}\n"
	e := NewCDocExtractor("c")
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "helper" {
		t.Errorf("Symbol = %q, want helper", docs[0].Symbol)
	}
	want := "Line one.\nLine two."
	if docs[0].Content != want {
		t.Errorf("Content = %q, want %q", docs[0].Content, want)
	}
}

func TestCDocExtractor_blockComment(t *testing.T) {
	src := "/* Sum two ints. */\nint sum(int a, int b) { return a + b; }\n"
	e := NewCDocExtractor("c")
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Type != "block_comment" {
		t.Errorf("Type = %q, want block_comment", docs[0].Type)
	}
	if docs[0].Symbol != "sum" {
		t.Errorf("Symbol = %q, want sum", docs[0].Symbol)
	}
}

func TestCDocExtractor_doxygen(t *testing.T) {
	src := "/** Allocates a new node. */\nNode *newNode(int val) {}\n"
	e := NewCDocExtractor("c")
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Type != "docstring" {
		t.Errorf("Type = %q, want docstring", docs[0].Type)
	}
	if docs[0].Symbol != "newNode" {
		t.Errorf("Symbol = %q, want newNode", docs[0].Symbol)
	}
	if docs[0].Content != "Allocates a new node." {
		t.Errorf("Content = %q", docs[0].Content)
	}
}

func TestCDocExtractor_multilineBlock(t *testing.T) {
	src := "/**\n * Returns the length.\n * Uses UTF-8.\n */\nint length(const char *s) {}\n"
	e := NewCDocExtractor("c")
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "length" {
		t.Errorf("Symbol = %q, want length", docs[0].Symbol)
	}
	if docs[0].Content == "" {
		t.Error("Content must not be empty for multiline block")
	}
}

func TestCDocExtractor_lineRange(t *testing.T) {
	src := "// doc\nvoid foo() {}\n"
	e := NewCDocExtractor("c")
	docs := e.ExtractDocs(src)
	if len(docs) == 0 {
		t.Fatal("no blocks")
	}
	if docs[0].StartLine != 1 || docs[0].EndLine != 1 {
		t.Errorf("StartLine=%d EndLine=%d, want 1 1", docs[0].StartLine, docs[0].EndLine)
	}
}

func TestCDocExtractor_ExtractSymbols(t *testing.T) {
	src := "// Doubles x.\nint dbl(int x) { return x*2; }\n"
	e := NewCDocExtractor("c")
	syms := e.ExtractSymbols(src)
	if syms["dbl"] != "Doubles x." {
		t.Errorf("syms[dbl] = %q, want \"Doubles x.\"", syms["dbl"])
	}
}

func TestCDocExtractor_language(t *testing.T) {
	if NewCDocExtractor("c").Language() != "c" {
		t.Error("Language() != c")
	}
	if NewCDocExtractor("cpp").Language() != "cpp" {
		t.Error("Language() != cpp")
	}
}

func TestCDocExtractor_empty(t *testing.T) {
	e := NewCDocExtractor("c")
	if docs := e.ExtractDocs(""); len(docs) != 0 {
		t.Errorf("empty input: want 0 blocks, got %d", len(docs))
	}
}

// ─── PascalDocExtractor tests ────────────────────────────────────────────────

func TestPascalDocExtractor_lineComment(t *testing.T) {
	src := "// Halts the program.\nPROCEDURE Halt;\nBEGIN END;\n"
	e := NewPascalDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Halt" {
		t.Errorf("Symbol = %q, want Halt", docs[0].Symbol)
	}
	if docs[0].Content != "Halts the program." {
		t.Errorf("Content = %q", docs[0].Content)
	}
}

func TestPascalDocExtractor_braceComment(t *testing.T) {
	src := "{ Returns the sum. }\nFUNCTION Add(a,b:Integer):Integer;\nBEGIN Add:=a+b END;\n"
	e := NewPascalDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Add" {
		t.Errorf("Symbol = %q, want Add", docs[0].Symbol)
	}
	if docs[0].Type != "block_comment" {
		t.Errorf("Type = %q, want block_comment", docs[0].Type)
	}
}

func TestPascalDocExtractor_parenComment(t *testing.T) {
	src := "(* Multiplies x by n. *)\nFUNCTION Mul(x,n:Integer):Integer;\nBEGIN Mul:=x*n END;\n"
	e := NewPascalDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Mul" {
		t.Errorf("Symbol = %q, want Mul", docs[0].Symbol)
	}
}

func TestPascalDocExtractor_multilineParenComment(t *testing.T) {
	src := "(*\n  Prints a greeting.\n  Uses WriteLn.\n*)\nPROCEDURE Greet;\nBEGIN WriteLn('Hi') END;\n"
	e := NewPascalDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Greet" {
		t.Errorf("Symbol = %q, want Greet", docs[0].Symbol)
	}
	if docs[0].Content == "" {
		t.Error("Content must not be empty")
	}
}

func TestPascalDocExtractor_compilerDirectiveSkipped(t *testing.T) {
	src := "{$IFDEF FPC}\nuses SysUtils;\n{$ENDIF}\n"
	e := NewPascalDocExtractor()
	docs := e.ExtractDocs(src)
	// {$...} directives must not produce documentation blocks.
	for _, d := range docs {
		if d.Content != "" {
			t.Errorf("unexpected block from compiler directive: %q", d.Content)
		}
	}
}

func TestPascalDocExtractor_ExtractSymbols(t *testing.T) {
	src := "{ Return max. }\nFUNCTION Max(a,b:Integer):Integer;\nBEGIN END;\n"
	e := NewPascalDocExtractor()
	syms := e.ExtractSymbols(src)
	if syms["Max"] != "Return max." {
		t.Errorf("syms[Max] = %q, want \"Return max.\"", syms["Max"])
	}
}

func TestPascalDocExtractor_language(t *testing.T) {
	if NewPascalDocExtractor().Language() != "pascal" {
		t.Error("Language() != pascal")
	}
}

// ─── OberonDocExtractor tests ────────────────────────────────────────────────

func TestOberonDocExtractor_singleLine(t *testing.T) {
	src := "(* Exports the result. *)\nPROCEDURE Export*;\nBEGIN END Export;\n"
	e := NewOberonDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Export" {
		t.Errorf("Symbol = %q, want Export", docs[0].Symbol)
	}
	if docs[0].Content != "Exports the result." {
		t.Errorf("Content = %q", docs[0].Content)
	}
}

func TestOberonDocExtractor_multiLine(t *testing.T) {
	src := "(*\n  Module initialisation.\n  Must be called first.\n*)\nPROCEDURE Init;\nBEGIN END Init;\n"
	e := NewOberonDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Init" {
		t.Errorf("Symbol = %q, want Init", docs[0].Symbol)
	}
	if docs[0].StartLine != 1 || docs[0].EndLine != 4 {
		t.Errorf("line range %d–%d, want 1–4", docs[0].StartLine, docs[0].EndLine)
	}
}

func TestOberonDocExtractor_freeStanding(t *testing.T) {
	src := "(* Module Foo. *)\n\nMODULE Foo;\n"
	e := NewOberonDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) == 0 {
		t.Fatal("expected at least one block")
	}
	if docs[0].Symbol != "" {
		t.Errorf("Symbol = %q, want empty (blank line separates comment from MODULE)", docs[0].Symbol)
	}
}

func TestOberonDocExtractor_ExtractSymbols(t *testing.T) {
	src := "(* Squared. *)\nPROCEDURE Sq*(x:INTEGER):INTEGER;\nBEGIN RETURN x*x END Sq;\n"
	e := NewOberonDocExtractor()
	syms := e.ExtractSymbols(src)
	if syms["Sq"] != "Squared." {
		t.Errorf("syms[Sq] = %q, want \"Squared.\"", syms["Sq"])
	}
}

func TestOberonDocExtractor_language(t *testing.T) {
	if NewOberonDocExtractor().Language() != "oberon" {
		t.Error("Language() != oberon")
	}
}

// ─── LispDocExtractor tests ───────────────────────────────────────────────────

func TestLispDocExtractor_semicolonComment(t *testing.T) {
	src := ";; Doubles n.\n(defun double (n) (* 2 n))\n"
	e := NewLispDocExtractor()
	docs := e.ExtractDocs(src)
	found := false
	for _, d := range docs {
		if d.Symbol == "double" && d.Type == "line_comment" {
			found = true
			if d.Content != "Doubles n." {
				t.Errorf("Content = %q, want \"Doubles n.\"", d.Content)
			}
		}
	}
	if !found {
		t.Error("no line_comment block for 'double'")
	}
}

func TestLispDocExtractor_tripleComment(t *testing.T) {
	src := ";;; A pure function.\n(defun square (x) (* x x))\n"
	e := NewLispDocExtractor()
	docs := e.ExtractDocs(src)
	found := false
	for _, d := range docs {
		if d.Symbol == "square" {
			found = true
		}
	}
	if !found {
		t.Error("no block for 'square'")
	}
}

func TestLispDocExtractor_blockComment(t *testing.T) {
	src := "#|\n  Utility package.\n|#\n(defpackage :util)\n"
	e := NewLispDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) == 0 {
		t.Fatal("no blocks")
	}
	if docs[0].Type != "block_comment" {
		t.Errorf("Type = %q, want block_comment", docs[0].Type)
	}
}

func TestLispDocExtractor_docstring(t *testing.T) {
	src := "(defun greet (name)\n  \"Greet NAME.\"\n  (format t \"Hello, ~a!\" name))\n"
	e := NewLispDocExtractor()
	docs := e.ExtractDocs(src)
	found := false
	for _, d := range docs {
		if d.Symbol == "greet" && d.Type == "docstring" {
			found = true
			if d.Content != "Greet NAME." {
				t.Errorf("Content = %q, want \"Greet NAME.\"", d.Content)
			}
		}
	}
	if !found {
		t.Error("no docstring block for 'greet'")
	}
}

func TestLispDocExtractor_ExtractSymbols(t *testing.T) {
	src := ";; Return sum.\n(defun add (a b) (+ a b))\n"
	e := NewLispDocExtractor()
	syms := e.ExtractSymbols(src)
	if syms["add"] != "Return sum." {
		t.Errorf("syms[add] = %q, want \"Return sum.\"", syms["add"])
	}
}

func TestLispDocExtractor_language(t *testing.T) {
	if NewLispDocExtractor().Language() != "lisp" {
		t.Error("Language() != lisp")
	}
}

func TestLispStringContent_basic(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"hello"`, "hello"},
		{`"a \"b\" c"`, `a "b" c`},
		{`""`, ""},
		{`"unclosed`, ""},
		{`not-a-string`, ""},
	}
	for _, c := range cases {
		got := lispStringContent(c.in)
		if got != c.want {
			t.Errorf("lispStringContent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ─── BasicDocExtractor tests ──────────────────────────────────────────────────

func TestBasicDocExtractor_remComment(t *testing.T) {
	src := "REM Adds two integers.\nFUNCTION Add(a As Integer, b As Integer) As Integer\n  Add = a + b\nEND FUNCTION\n"
	e := NewBasicDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Add" {
		t.Errorf("Symbol = %q, want Add", docs[0].Symbol)
	}
	if docs[0].Content != "Adds two integers." {
		t.Errorf("Content = %q", docs[0].Content)
	}
}

func TestBasicDocExtractor_apostropheComment(t *testing.T) {
	src := "' Prints a greeting.\nSUB Hello()\n  PRINT \"Hi\"\nEND SUB\n"
	e := NewBasicDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Hello" {
		t.Errorf("Symbol = %q, want Hello", docs[0].Symbol)
	}
	if docs[0].Content != "Prints a greeting." {
		t.Errorf("Content = %q", docs[0].Content)
	}
}

func TestBasicDocExtractor_remCaseInsensitive(t *testing.T) {
	src := "rem lower-case comment\nSUB Demo()\nEND SUB\n"
	e := NewBasicDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) != 1 {
		t.Fatalf("want 1 block, got %d", len(docs))
	}
	if docs[0].Symbol != "Demo" {
		t.Errorf("Symbol = %q, want Demo", docs[0].Symbol)
	}
}

func TestBasicDocExtractor_freeStanding(t *testing.T) {
	src := "' Module header\n\nDIM x As Integer\n"
	e := NewBasicDocExtractor()
	docs := e.ExtractDocs(src)
	if len(docs) == 0 {
		t.Fatal("expected at least one block")
	}
	if docs[0].Symbol != "" {
		t.Errorf("Symbol = %q, want empty", docs[0].Symbol)
	}
}

func TestBasicDocExtractor_ExtractSymbols(t *testing.T) {
	src := "' Computes n squared.\nFUNCTION Sq(n As Integer) As Integer\n  Sq = n * n\nEND FUNCTION\n"
	e := NewBasicDocExtractor()
	syms := e.ExtractSymbols(src)
	if syms["Sq"] != "Computes n squared." {
		t.Errorf("syms[Sq] = %q, want \"Computes n squared.\"", syms["Sq"])
	}
}

func TestBasicDocExtractor_language(t *testing.T) {
	if NewBasicDocExtractor().Language() != "basic" {
		t.Error("Language() != basic")
	}
}

// ─── Registry wiring tests ────────────────────────────────────────────────────

func TestDocExtractorRegistry_globalRegistryHasAll(t *testing.T) {
	for _, id := range []string{"c", "cpp", "pascal", "oberon", "lisp", "basic"} {
		if globalRegistry.GetExtractor(id) == nil {
			t.Errorf("globalRegistry.GetExtractor(%q) == nil, want non-nil after Phase 4", id)
		}
	}
}

func TestDocExtractorRegistry_nonExtractionLangsNil(t *testing.T) {
	for _, id := range []string{"go", "python", "json", "markdown", "yaml"} {
		if globalRegistry.GetExtractor(id) != nil {
			t.Errorf("globalRegistry.GetExtractor(%q) != nil for non-extraction language", id)
		}
	}
}

func TestDocExtractorRegistry_setExtractor(t *testing.T) {
	r := NewLanguageRegistry()
	initLanguages(r)
	r.SetExtractor("c", NewCDocExtractor("c"))
	if r.GetExtractor("c") == nil {
		t.Error("SetExtractor: GetExtractor(c) == nil after set")
	}
}

// ─── Integration: ragIngestFile populates Docs ────────────────────────────────

func TestRagIngestFile_docsPopulated(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "test.db"), "stub")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	// Write a small C file with a comment above a function.
	cPath := filepath.Join(dir, "lib.c")
	if err := os.WriteFile(cPath, []byte("// Doubles the value.\nint dbl(int x) { return x * 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := ragIngestFile(store, stubEmbedder{"stub"}, cPath, ProvenanceMeta{})
	if err != nil {
		t.Fatalf("ragIngestFile: %v", err)
	}
	if n == 0 {
		t.Fatal("ragIngestFile returned 0 chunks")
	}

	// Query the store; the chunk for dbl should have Docs populated.
	rows, err := store.db.Query("SELECT symbols, docs FROM chunks")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var syms, docs string
		if err := rows.Scan(&syms, &docs); err != nil {
			t.Fatal(err)
		}
		if syms == "dbl" && docs == "Doubles the value." {
			found = true
		}
	}
	if !found {
		t.Error("expected chunk with symbols=dbl and docs='Doubles the value.' in store")
	}
}
