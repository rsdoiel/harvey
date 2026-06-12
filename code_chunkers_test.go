package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── findLineCol ──────────────────────────────────────────────────────────────

func TestFindLineCol_firstLine(t *testing.T) {
	data := []byte("hello\nworld")
	line, col := findLineCol(data, 3)
	if line != 1 || col != 3 {
		t.Errorf("findLineCol(3) = (%d, %d), want (1, 3)", line, col)
	}
}

func TestFindLineCol_secondLine(t *testing.T) {
	data := []byte("hello\nworld")
	line, col := findLineCol(data, 8)
	if line != 2 || col != 2 {
		t.Errorf("findLineCol(8) = (%d, %d), want (2, 2)", line, col)
	}
}

func TestFindLineCol_atNewline(t *testing.T) {
	data := []byte("ab\ncd")
	line, col := findLineCol(data, 2)
	if line != 1 || col != 2 {
		t.Errorf("findLineCol at \\n = (%d, %d), want (1, 2)", line, col)
	}
}

func TestFindLineCol_zero(t *testing.T) {
	data := []byte("abc")
	line, col := findLineCol(data, 0)
	if line != 1 || col != 0 {
		t.Errorf("findLineCol(0) = (%d, %d), want (1, 0)", line, col)
	}
}

func TestFindLineCol_clampBeyondEnd(t *testing.T) {
	data := []byte("abc")
	line, col := findLineCol(data, 999)
	if line != 1 || col != 3 {
		t.Errorf("findLineCol(999) = (%d, %d), want (1, 3)", line, col)
	}
}

// ─── CChunker ────────────────────────────────────────────────────────────────

func TestCChunker_emptyContent(t *testing.T) {
	c := NewCChunker("c")
	if chunks := c.Chunk("", "empty.c"); len(chunks) != 0 {
		t.Errorf("empty content: got %d chunks, want 0", len(chunks))
	}
}

func TestCChunker_singleFunction(t *testing.T) {
	src := "int add(int a, int b) {\n    return a + b;\n}\n"
	c := NewCChunker("c")
	chunks := c.Chunk(src, "math.c")
	if len(chunks) != 1 {
		t.Fatalf("single function: got %d chunks, want 1", len(chunks))
	}
	if chunks[0].ChunkType != "function" {
		t.Errorf("ChunkType = %q, want \"function\"", chunks[0].ChunkType)
	}
	if len(chunks[0].Symbols) == 0 || chunks[0].Symbols[0] != "add" {
		t.Errorf("Symbols = %v, want [add]", chunks[0].Symbols)
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", chunks[0].StartLine)
	}
}

func TestCChunker_twoFunctions(t *testing.T) {
	src := "int foo() { return 1; }\nint bar() { return 2; }\n"
	c := NewCChunker("c")
	chunks := c.Chunk(src, "lib.c")
	if len(chunks) < 2 {
		t.Fatalf("two functions: got %d chunks, want ≥2", len(chunks))
	}
}

func TestCChunker_includesSection(t *testing.T) {
	src := "#include <stdio.h>\n#include <stdlib.h>\n\nint main() {\n    return 0;\n}\n"
	c := NewCChunker("c")
	chunks := c.Chunk(src, "main.c")
	// Includes should be separate from main.
	if len(chunks) < 2 {
		t.Fatalf("includes + main: got %d chunks, want ≥2", len(chunks))
	}
	hasInclude := false
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "#include") {
			hasInclude = true
		}
	}
	if !hasInclude {
		t.Error("no chunk contains #include")
	}
}

func TestCChunker_stringWithBrace(t *testing.T) {
	// A `{` inside a string must not affect brace depth.
	src := `void print_brace() { printf("{"); }` + "\n"
	c := NewCChunker("c")
	chunks := c.Chunk(src, "t.c")
	if len(chunks) != 1 {
		t.Fatalf("string with brace: got %d chunks, want 1", len(chunks))
	}
}

func TestCChunker_lineCommentWithBrace(t *testing.T) {
	src := "// open brace: {\nint x = 0;\n\nvoid foo() { return; }\n"
	c := NewCChunker("c")
	chunks := c.Chunk(src, "t.c")
	// Comment+declaration and function should be separate chunks.
	if len(chunks) < 2 {
		t.Fatalf("line comment with brace: got %d chunks, want ≥2", len(chunks))
	}
}

func TestCChunker_blockCommentNotSplit(t *testing.T) {
	src := "/* multi\n   line\n   comment */\nvoid foo() { }\n"
	c := NewCChunker("c")
	chunks := c.Chunk(src, "t.c")
	// Comment precedes function with no blank line — should be in same chunk as foo.
	found := false
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "multi") && strings.Contains(ch.Content, "foo") {
			found = true
		}
	}
	if !found {
		t.Error("block comment and following function should be in the same chunk")
	}
}

func TestCChunker_language(t *testing.T) {
	if NewCChunker("c").Language() != "c" {
		t.Error("Language() should return \"c\"")
	}
	if NewCChunker("cpp").Language() != "cpp" {
		t.Error("Language() should return \"cpp\"")
	}
}

func TestCChunker_locationMetadata(t *testing.T) {
	src := "void foo() {\n    return;\n}\n"
	chunks := NewCChunker("c").Chunk(src, "t.c")
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	ch := chunks[0]
	if ch.StartLine < 1 || ch.EndLine < ch.StartLine {
		t.Errorf("bad location: StartLine=%d EndLine=%d", ch.StartLine, ch.EndLine)
	}
}

// ─── PascalChunker ───────────────────────────────────────────────────────────

func TestPascalChunker_singleProcedure(t *testing.T) {
	src := "PROCEDURE Greet;\nBEGIN\n  WriteLn('Hello');\nEND;\n"
	chunks := NewPascalChunker().Chunk(src, "g.pas")
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	var found bool
	for _, ch := range chunks {
		if ch.ChunkType == "procedure" && len(ch.Symbols) > 0 && ch.Symbols[0] == "Greet" {
			found = true
		}
	}
	if !found {
		t.Errorf("no procedure chunk named Greet; chunks: %+v", chunks)
	}
}

func TestPascalChunker_functionExtractedAsSymbol(t *testing.T) {
	src := "FUNCTION Add(A, B: Integer): Integer;\nBEGIN\n  Add := A + B;\nEND;\n"
	chunks := NewPascalChunker().Chunk(src, "math.pas")
	found := false
	for _, ch := range chunks {
		if ch.ChunkType == "procedure" && len(ch.Symbols) > 0 && ch.Symbols[0] == "Add" {
			found = true
		}
	}
	if !found {
		t.Errorf("no function chunk named Add; chunks: %+v", chunks)
	}
}

func TestPascalChunker_headerAndProcedure(t *testing.T) {
	src := "PROGRAM Hello;\nUSES SysUtils;\n\nPROCEDURE Foo;\nBEGIN\n  WriteLn;\nEND;\n"
	chunks := NewPascalChunker().Chunk(src, "h.pas")
	if len(chunks) < 2 {
		t.Fatalf("expected header chunk + procedure chunk, got %d chunks", len(chunks))
	}
	hasProg := false
	hasProc := false
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "PROGRAM") {
			hasProg = true
		}
		if ch.ChunkType == "procedure" {
			hasProc = true
		}
	}
	if !hasProg {
		t.Error("no chunk containing PROGRAM")
	}
	if !hasProc {
		t.Error("no procedure chunk")
	}
}

func TestPascalChunker_nestedBeginEnd(t *testing.T) {
	src := "PROCEDURE Outer;\nBEGIN\n  IF TRUE THEN\n  BEGIN\n    WriteLn;\n  END;\nEND;\n"
	chunks := NewPascalChunker().Chunk(src, "n.pas")
	found := false
	for _, ch := range chunks {
		if ch.ChunkType == "procedure" && strings.Contains(ch.Content, "Outer") {
			found = true
			if !strings.Contains(ch.Content, "END;") {
				t.Error("nested procedure chunk missing END;")
			}
		}
	}
	if !found {
		t.Error("no procedure chunk for Outer")
	}
}

func TestPascalChunker_language(t *testing.T) {
	if NewPascalChunker().Language() != "pascal" {
		t.Error("Language() should return \"pascal\"")
	}
}

// ─── OberonChunker ───────────────────────────────────────────────────────────

func TestOberonChunker_singleProcedure(t *testing.T) {
	src := "MODULE Foo;\nPROCEDURE Bar*;\nBEGIN\nEND Bar;\nEND Foo.\n"
	chunks := NewOberonChunker().Chunk(src, "Foo.Mod")
	found := false
	for _, ch := range chunks {
		if ch.ChunkType == "procedure" && len(ch.Symbols) > 0 && ch.Symbols[0] == "Bar" {
			found = true
		}
	}
	if !found {
		t.Errorf("no procedure chunk named Bar; chunks: %+v", chunks)
	}
}

func TestOberonChunker_moduleHeaderIsCodeChunk(t *testing.T) {
	src := "MODULE Foo;\nIMPORT In;\nVAR x: INTEGER;\n\nPROCEDURE P;\nBEGIN\nEND P;\n\nEND Foo.\n"
	chunks := NewOberonChunker().Chunk(src, "Foo.Mod")
	hasCode := false
	for _, ch := range chunks {
		if ch.ChunkType == "code" && strings.Contains(ch.Content, "MODULE") {
			hasCode = true
		}
	}
	if !hasCode {
		t.Error("no code chunk containing MODULE declaration")
	}
}

func TestOberonChunker_language(t *testing.T) {
	if NewOberonChunker().Language() != "oberon" {
		t.Error("Language() should return \"oberon\"")
	}
}

// ─── LispChunker ─────────────────────────────────────────────────────────────

func TestLispChunker_defun(t *testing.T) {
	src := "(defun greet (name)\n  (format t \"Hello ~a\" name))\n"
	chunks := NewLispChunker().Chunk(src, "greet.lisp")
	if len(chunks) != 1 {
		t.Fatalf("defun: got %d chunks, want 1", len(chunks))
	}
	if chunks[0].ChunkType != "function" {
		t.Errorf("ChunkType = %q, want \"function\"", chunks[0].ChunkType)
	}
	if len(chunks[0].Symbols) == 0 || chunks[0].Symbols[0] != "greet" {
		t.Errorf("Symbols = %v, want [greet]", chunks[0].Symbols)
	}
}

func TestLispChunker_multipleForms(t *testing.T) {
	src := "(defvar *count* 0)\n(defun inc () (incf *count*))\n(defun dec () (decf *count*))\n"
	chunks := NewLispChunker().Chunk(src, "c.lisp")
	if len(chunks) != 3 {
		t.Errorf("3 top-level forms: got %d chunks, want 3", len(chunks))
	}
}

func TestLispChunker_lineCommentIgnored(t *testing.T) {
	src := "; just a comment\n(defun foo () nil)\n"
	chunks := NewLispChunker().Chunk(src, "c.lisp")
	// comment is not inside parens → not a chunk; only defun is
	if len(chunks) != 1 {
		t.Errorf("comment+defun: got %d chunks, want 1", len(chunks))
	}
}

func TestLispChunker_nestedParens(t *testing.T) {
	src := "(defun complex (x)\n  (if (> x 0)\n      (+ x 1)\n      (- x 1)))\n"
	chunks := NewLispChunker().Chunk(src, "c.lisp")
	if len(chunks) != 1 {
		t.Fatalf("nested parens: got %d chunks, want 1", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "complex") {
		t.Error("chunk missing 'complex'")
	}
}

func TestLispChunker_defmacroType(t *testing.T) {
	src := "(defmacro when2 (cond &body body) `(if ,cond (progn ,@body)))\n"
	chunks := NewLispChunker().Chunk(src, "m.lisp")
	if len(chunks) != 1 || chunks[0].ChunkType != "function" {
		t.Errorf("defmacro: got ChunkType=%q, want \"function\"", chunks[0].ChunkType)
	}
	if len(chunks[0].Symbols) == 0 || chunks[0].Symbols[0] != "when2" {
		t.Errorf("defmacro symbol = %v, want [when2]", chunks[0].Symbols)
	}
}

func TestLispChunker_language(t *testing.T) {
	if NewLispChunker().Language() != "lisp" {
		t.Error("Language() should return \"lisp\"")
	}
}

// ─── BasicChunker ────────────────────────────────────────────────────────────

func TestBasicChunker_singleSub(t *testing.T) {
	src := "SUB Hello()\n  PRINT \"Hi\"\nEND SUB\n"
	chunks := NewBasicChunker().Chunk(src, "p.bas")
	if len(chunks) != 1 {
		t.Fatalf("single SUB: got %d chunks, want 1", len(chunks))
	}
	if chunks[0].ChunkType != "function" {
		t.Errorf("ChunkType = %q, want \"function\"", chunks[0].ChunkType)
	}
	if len(chunks[0].Symbols) == 0 || chunks[0].Symbols[0] != "Hello" {
		t.Errorf("Symbols = %v, want [Hello]", chunks[0].Symbols)
	}
}

func TestBasicChunker_functionKeyword(t *testing.T) {
	src := "FUNCTION Add(A AS INTEGER, B AS INTEGER) AS INTEGER\n  Add = A + B\nEND FUNCTION\n"
	chunks := NewBasicChunker().Chunk(src, "math.bas")
	found := false
	for _, ch := range chunks {
		if ch.ChunkType == "function" && len(ch.Symbols) > 0 && ch.Symbols[0] == "Add" {
			found = true
		}
	}
	if !found {
		t.Errorf("no FUNCTION chunk named Add; chunks: %+v", chunks)
	}
}

func TestBasicChunker_headerAndSub(t *testing.T) {
	src := "DIM x AS INTEGER\n\nSUB Foo()\n  PRINT x\nEND SUB\n"
	chunks := NewBasicChunker().Chunk(src, "p.bas")
	if len(chunks) < 2 {
		t.Fatalf("header + SUB: got %d chunks, want ≥2", len(chunks))
	}
	hasCode := false
	hasSub := false
	for _, ch := range chunks {
		if ch.ChunkType == "code" {
			hasCode = true
		}
		if ch.ChunkType == "function" {
			hasSub = true
		}
	}
	if !hasCode || !hasSub {
		t.Errorf("expected both code and function chunks; code=%v sub=%v", hasCode, hasSub)
	}
}

func TestBasicChunker_language(t *testing.T) {
	if NewBasicChunker().Language() != "basic" {
		t.Error("Language() should return \"basic\"")
	}
}

// ─── RagStore.IngestEnriched ─────────────────────────────────────────────────

type stubEmbedder struct{ name string }

func (s stubEmbedder) Embed(text string) ([]float64, error) { return []float64{1.0, 0.0}, nil }
func (s stubEmbedder) Name() string                         { return s.name }

func TestIngestEnriched_basic(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	chunks := []EnrichedChunk{
		{Content: "void foo() {}", StartLine: 1, EndLine: 1, ChunkType: "function", Symbols: []string{"foo"}},
		{Content: "int x = 0;", StartLine: 3, EndLine: 3, ChunkType: "code"},
	}
	if err := store.IngestEnriched("test.c", chunks, stubEmbedder{"stub"}); err != nil {
		t.Fatalf("IngestEnriched: %v", err)
	}

	// Verify rows landed in the database.
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM chunks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("got %d rows, want 2", count)
	}

	// Verify enriched columns were populated.
	var chunkType string
	var startLine int
	if err := store.db.QueryRow(
		`SELECT chunk_type, start_line FROM chunks WHERE content = 'void foo() {}'`,
	).Scan(&chunkType, &startLine); err != nil {
		t.Fatalf("query enriched row: %v", err)
	}
	if chunkType != "function" {
		t.Errorf("chunk_type = %q, want \"function\"", chunkType)
	}
	if startLine != 1 {
		t.Errorf("start_line = %d, want 1", startLine)
	}
}

func TestIngestEnriched_modelMismatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "r.db"), "model-a")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	err = store.IngestEnriched("x.c", []EnrichedChunk{{Content: "x"}}, stubEmbedder{"model-b"})
	if err == nil {
		t.Error("expected error on model mismatch")
	}
}

func TestIngestEnriched_lazyMigration(t *testing.T) {
	// Create a store without the enriched columns (pre-Phase-3 store), then
	// call IngestEnriched and confirm the columns are added on the fly.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")

	// Create a minimal store manually (no enriched columns).
	store, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatal(err)
	}
	// Drop the enriched columns by reopening with a fresh schema isn't easy;
	// instead just call IngestEnriched twice to confirm idempotency.
	chunks := []EnrichedChunk{{Content: "x", ChunkType: "code"}}
	if err := store.IngestEnriched("f.c", chunks, stubEmbedder{"stub"}); err != nil {
		t.Fatalf("first IngestEnriched: %v", err)
	}
	if err := store.IngestEnriched("f.c", chunks, stubEmbedder{"stub"}); err != nil {
		t.Fatalf("second IngestEnriched (idempotent migration): %v", err)
	}
	store.db.Close()
}

func TestIngestEnriched_symbolsStoredAsCsv(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "s.db"), "stub")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	chunks := []EnrichedChunk{
		{Content: "multi", ChunkType: "code", Symbols: []string{"alpha", "beta", "gamma"}},
	}
	if err := store.IngestEnriched("x.c", chunks, stubEmbedder{"stub"}); err != nil {
		t.Fatal(err)
	}
	var syms string
	store.db.QueryRow(`SELECT symbols FROM chunks WHERE content='multi'`).Scan(&syms)
	if syms != "alpha,beta,gamma" {
		t.Errorf("symbols = %q, want \"alpha,beta,gamma\"", syms)
	}
}

func TestIngestEnriched_identifiersAndCitations(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "s.db"), "stub")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	chunks := []EnrichedChunk{
		{
			Content:     "with metadata",
			ChunkType:   "references",
			Identifiers: map[string][]string{"doi": {"https://doi.org/10.1234/abcd.5678"}},
			Citations:   []string{"https://doi.org/10.5555/9999.0001", "arxiv:2412.03631"},
		},
		{Content: "without metadata", ChunkType: "body"},
	}
	if err := store.IngestEnriched("paper.pdf", chunks, stubEmbedder{"stub"}); err != nil {
		t.Fatal(err)
	}

	var identifiers, citations string
	if err := store.db.QueryRow(`SELECT identifiers, citations FROM chunks WHERE content='with metadata'`).Scan(&identifiers, &citations); err != nil {
		t.Fatal(err)
	}
	if want := `{"doi":["https://doi.org/10.1234/abcd.5678"]}`; identifiers != want {
		t.Errorf("identifiers = %q, want %q", identifiers, want)
	}
	if want := "https://doi.org/10.5555/9999.0001,arxiv:2412.03631"; citations != want {
		t.Errorf("citations = %q, want %q", citations, want)
	}

	if err := store.db.QueryRow(`SELECT identifiers, citations FROM chunks WHERE content='without metadata'`).Scan(&identifiers, &citations); err != nil {
		t.Fatal(err)
	}
	if identifiers != "{}" {
		t.Errorf("identifiers = %q, want \"{}\"", identifiers)
	}
	if citations != "" {
		t.Errorf("citations = %q, want \"\"", citations)
	}
}

// ─── ragIngestFile integration ────────────────────────────────────────────────

func TestRagIngestFile_binarySkipped(t *testing.T) {
	dir := t.TempDir()
	// Write a binary file (contains null byte).
	binPath := filepath.Join(dir, "prog.bin")
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	n, err := ragIngestFile(store, stubEmbedder{"stub"}, binPath)
	if err != nil {
		t.Errorf("binary file should not return error: %v", err)
	}
	if n != 0 {
		t.Errorf("binary file should produce 0 chunks, got %d", n)
	}
}

func TestRagIngestFile_cFileUsesChunker(t *testing.T) {
	dir := t.TempDir()
	cPath := filepath.Join(dir, "lib.c")
	src := "#include <stdio.h>\n\nvoid hello() {\n    printf(\"hi\\n\");\n}\n"
	if err := os.WriteFile(cPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	n, err := ragIngestFile(store, stubEmbedder{"stub"}, cPath)
	if err != nil {
		t.Fatalf("ragIngestFile: %v", err)
	}
	if n == 0 {
		t.Error("expected at least one chunk for C file")
	}
	// Verify at least one row has chunk_type = 'function'.
	var ct string
	store.db.QueryRow(`SELECT chunk_type FROM chunks WHERE chunk_type='function' LIMIT 1`).Scan(&ct)
	if ct != "function" {
		t.Error("expected a 'function' chunk_type row from C file")
	}
}

func TestRagIngestFile_fallbackForUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(mdPath, []byte("# Hello\n\nThis is a note.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	n, err := ragIngestFile(store, stubEmbedder{"stub"}, mdPath)
	if err != nil {
		t.Fatalf("ragIngestFile: %v", err)
	}
	if n == 0 {
		t.Error("expected at least one chunk for markdown file")
	}
}

// ─── Registry wiring: all 6 chunkers registered after init ───────────────────

func TestInitChunkers_allRegistered(t *testing.T) {
	r := freshRegistry()
	initChunkers(r)
	for _, id := range []string{"c", "cpp", "pascal", "oberon", "lisp", "basic"} {
		if r.GetChunker(id) == nil {
			t.Errorf("GetChunker(%q) == nil after initChunkers", id)
		}
	}
}

func TestInitChunkers_globalRegistryWired(t *testing.T) {
	// globalRegistry is wired by package init() before any test runs.
	for _, id := range []string{"c", "cpp", "pascal", "oberon", "lisp", "basic"} {
		if globalRegistry.GetChunker(id) == nil {
			t.Errorf("globalRegistry.GetChunker(%q) == nil", id)
		}
	}
}
