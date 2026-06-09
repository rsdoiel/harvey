package harvey

import (
	"sort"
	"testing"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// freshRegistry returns a new registry populated by initLanguages.
// Use this when a test needs its own isolated registry rather than globalRegistry.
func freshRegistry() *LanguageRegistry {
	r := NewLanguageRegistry()
	initLanguages(r)
	return r
}

// ─── DetectFromExtension ──────────────────────────────────────────────────────

func TestDetectFromExtension_go(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".go", ".mod", ".sum"} {
		lang, ok := r.DetectFromExtension(ext)
		if !ok || lang != "go" {
			t.Errorf("DetectFromExtension(%q) = (%q, %v), want (\"go\", true)", ext, lang, ok)
		}
	}
}

func TestDetectFromExtension_typescript(t *testing.T) {
	r := freshRegistry()
	lang, ok := r.DetectFromExtension(".ts")
	if !ok || lang != "typescript" {
		t.Errorf("DetectFromExtension(\".ts\") = (%q, %v), want (\"typescript\", true)", lang, ok)
	}
}

func TestDetectFromExtension_javascript(t *testing.T) {
	r := freshRegistry()
	lang, ok := r.DetectFromExtension(".js")
	if !ok || lang != "javascript" {
		t.Errorf("DetectFromExtension(\".js\") = (%q, %v), want (\"javascript\", true)", lang, ok)
	}
}

func TestDetectFromExtension_python(t *testing.T) {
	r := freshRegistry()
	lang, ok := r.DetectFromExtension(".py")
	if !ok || lang != "python" {
		t.Errorf("DetectFromExtension(\".py\") = (%q, %v), want (\"python\", true)", lang, ok)
	}
}

func TestDetectFromExtension_rust(t *testing.T) {
	r := freshRegistry()
	lang, ok := r.DetectFromExtension(".rs")
	if !ok || lang != "rust" {
		t.Errorf("DetectFromExtension(\".rs\") = (%q, %v), want (\"rust\", true)", lang, ok)
	}
}

func TestDetectFromExtension_c(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".c", ".h"} {
		lang, ok := r.DetectFromExtension(ext)
		if !ok || lang != "c" {
			t.Errorf("DetectFromExtension(%q) = (%q, %v), want (\"c\", true)", ext, lang, ok)
		}
	}
}

func TestDetectFromExtension_cpp(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".cpp", ".cc", ".cxx", ".hpp", ".hh"} {
		lang, ok := r.DetectFromExtension(ext)
		if !ok || lang != "cpp" {
			t.Errorf("DetectFromExtension(%q) = (%q, %v), want (\"cpp\", true)", ext, lang, ok)
		}
	}
}

func TestDetectFromExtension_pascal(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".pas", ".p"} {
		lang, ok := r.DetectFromExtension(ext)
		if !ok || lang != "pascal" {
			t.Errorf("DetectFromExtension(%q) = (%q, %v), want (\"pascal\", true)", ext, lang, ok)
		}
	}
}

func TestDetectFromExtension_oberon_canonical(t *testing.T) {
	r := freshRegistry()
	// .Mod (capital M) must detect as Oberon via exact match.
	lang, ok := r.DetectFromExtension(".Mod")
	if !ok || lang != "oberon" {
		t.Errorf("DetectFromExtension(\".Mod\") = (%q, %v), want (\"oberon\", true)", lang, ok)
	}
}

func TestDetectFromExtension_oberon_obn(t *testing.T) {
	r := freshRegistry()
	lang, ok := r.DetectFromExtension(".obn")
	if !ok || lang != "oberon" {
		t.Errorf("DetectFromExtension(\".obn\") = (%q, %v), want (\"oberon\", true)", lang, ok)
	}
}

func TestDetectFromExtension_modLowercase_goWins(t *testing.T) {
	// .mod (lowercase) is Go's module file extension; Go is registered first,
	// so it wins the lowercase index over Oberon's .Mod.
	r := freshRegistry()
	lang, ok := r.DetectFromExtension(".mod")
	if !ok || lang != "go" {
		t.Errorf("DetectFromExtension(\".mod\") = (%q, %v), want (\"go\", true)", lang, ok)
	}
}

func TestDetectFromExtension_lisp(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".lisp", ".lsp", ".cl", ".el"} {
		lang, ok := r.DetectFromExtension(ext)
		if !ok || lang != "lisp" {
			t.Errorf("DetectFromExtension(%q) = (%q, %v), want (\"lisp\", true)", ext, lang, ok)
		}
	}
}

func TestDetectFromExtension_basic(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".bas", ".bi"} {
		lang, ok := r.DetectFromExtension(ext)
		if !ok || lang != "basic" {
			t.Errorf("DetectFromExtension(%q) = (%q, %v), want (\"basic\", true)", ext, lang, ok)
		}
	}
}

func TestDetectFromExtension_dataLanguages(t *testing.T) {
	r := freshRegistry()
	cases := map[string]string{
		".json": "json",
		".md":   "markdown",
		".txt":  "text",
		".css":  "css",
		".yaml": "yaml",
		".yml":  "yaml",
		".toml": "toml",
		".sql":  "sql",
		".html": "html",
		".sh":   "shell",
		".bash": "shell",
		".env":  "env",
	}
	for ext, want := range cases {
		lang, ok := r.DetectFromExtension(ext)
		if !ok || lang != want {
			t.Errorf("DetectFromExtension(%q) = (%q, %v), want (%q, true)", ext, lang, ok, want)
		}
	}
}

func TestDetectFromExtension_unknown(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".xyz", ".abc", "", ".docx"} {
		lang, ok := r.DetectFromExtension(ext)
		if ok {
			t.Errorf("DetectFromExtension(%q) = (%q, true), want (\"\", false)", ext, lang)
		}
	}
}

// ─── HasExtension ─────────────────────────────────────────────────────────────

func TestHasExtension_known(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{
		".go", ".ts", ".py", ".rs", ".c", ".h", ".cpp", ".hpp",
		".pas", ".Mod", ".obn", ".lisp", ".bas",
		".json", ".md", ".yaml", ".sql",
	} {
		if !r.HasExtension(ext) {
			t.Errorf("HasExtension(%q) = false, want true", ext)
		}
	}
}

func TestHasExtension_caseInsensitive(t *testing.T) {
	r := freshRegistry()
	// .mod (lowercase) is known via Go; .Mod (capital) is known via Oberon.
	// Both must return true regardless of case.
	cases := []string{".mod", ".Mod", ".MOD", ".GO", ".PY", ".TS"}
	for _, ext := range cases {
		if !r.HasExtension(ext) {
			t.Errorf("HasExtension(%q) = false, want true (case-insensitive)", ext)
		}
	}
}

func TestHasExtension_unknown(t *testing.T) {
	r := freshRegistry()
	for _, ext := range []string{".xyz", ".docx", ""} {
		if r.HasExtension(ext) {
			t.Errorf("HasExtension(%q) = true, want false", ext)
		}
	}
}

// ─── GetLanguageInfo ──────────────────────────────────────────────────────────

func TestGetLanguageInfo_known(t *testing.T) {
	r := freshRegistry()
	info, ok := r.GetLanguageInfo("oberon")
	if !ok {
		t.Fatal("GetLanguageInfo(\"oberon\"): ok = false, want true")
	}
	if info.Name != "Oberon" {
		t.Errorf("info.Name = %q, want \"Oberon\"", info.Name)
	}
	if !info.HasChunking {
		t.Error("info.HasChunking = false, want true")
	}
}

func TestGetLanguageInfo_unknown(t *testing.T) {
	r := freshRegistry()
	_, ok := r.GetLanguageInfo("cobol")
	if ok {
		t.Error("GetLanguageInfo(\"cobol\"): ok = true, want false")
	}
}

// ─── LanguageIDs ──────────────────────────────────────────────────────────────

func TestLanguageIDs_count(t *testing.T) {
	r := freshRegistry()
	ids := r.LanguageIDs()
	if len(ids) != 21 {
		t.Errorf("len(LanguageIDs()) = %d, want 21", len(ids))
	}
}

func TestLanguageIDs_containsAll(t *testing.T) {
	r := freshRegistry()
	ids := r.LanguageIDs()
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	want := []string{
		"go", "typescript", "javascript", "python", "rust",
		"c", "cpp", "pascal", "oberon", "lisp", "basic",
		"json", "markdown", "text", "css", "yaml", "toml", "sql", "html", "shell", "env",
	}
	sort.Strings(want)
	for _, id := range want {
		if !idSet[id] {
			t.Errorf("LanguageIDs() missing %q", id)
		}
	}
}

// ─── Phase-1/2 handlers ───────────────────────────────────────────────────────

// Chunker, Extractor, Formatter, and Highlighter remain nil through Phase 2.
// Detector is non-nil after Phase 2 (all 21 languages register a CombinedDetector).
func TestGetHandlers_phase2(t *testing.T) {
	r := freshRegistry()
	for _, id := range r.LanguageIDs() {
		if r.GetChunker(id) != nil {
			t.Errorf("GetChunker(%q) != nil (expected nil through Phase 2)", id)
		}
		if r.GetExtractor(id) != nil {
			t.Errorf("GetExtractor(%q) != nil (expected nil through Phase 2)", id)
		}
		if r.GetFormatter(id) != nil {
			t.Errorf("GetFormatter(%q) != nil (expected nil through Phase 2)", id)
		}
		if r.GetHighlighter(id) != nil {
			t.Errorf("GetHighlighter(%q) != nil (expected nil through Phase 2)", id)
		}
		// Phase 2: every language must have a detector.
		if r.GetDetector(id) == nil {
			t.Errorf("GetDetector(%q) == nil after Phase 2", id)
		}
	}
}

// ─── RegisterLanguage overwrite ───────────────────────────────────────────────

func TestRegisterLanguage_overwrite(t *testing.T) {
	r := NewLanguageRegistry()
	r.RegisterLanguage(LanguageInfo{ID: "x", Name: "X", Extensions: []string{".x"}},
		nil, nil, nil, nil, nil)
	r.RegisterLanguage(LanguageInfo{ID: "x", Name: "Y", Extensions: []string{".x"}},
		nil, nil, nil, nil, nil)

	info, ok := r.GetLanguageInfo("x")
	if !ok {
		t.Fatal("GetLanguageInfo(\"x\") not found after overwrite")
	}
	if info.Name != "Y" {
		t.Errorf("info.Name = %q, want \"Y\" after overwrite", info.Name)
	}
}

// ─── looksLikePath via registry ───────────────────────────────────────────────

func TestLooksLikePath_cExtensions(t *testing.T) {
	cases := []string{"main.c", "util.h", "hello.cpp", "lib.hpp", "module.cc"}
	for _, c := range cases {
		if !looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = false, want true", c)
		}
	}
}

func TestLooksLikePath_pascalExtension(t *testing.T) {
	cases := []string{"program.pas", "unit.p"}
	for _, c := range cases {
		if !looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = false, want true", c)
		}
	}
}

func TestLooksLikePath_oberonExtensions(t *testing.T) {
	// Both canonical .Mod and lowercase .mod (via Go) must be recognised.
	cases := []string{"HelloWorld.Mod", "module.obn", "go.mod"}
	for _, c := range cases {
		if !looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = false, want true", c)
		}
	}
}

func TestLooksLikePath_lispExtension(t *testing.T) {
	cases := []string{"functions.lisp", "macros.lsp", "package.cl", "init.el"}
	for _, c := range cases {
		if !looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = false, want true", c)
		}
	}
}

func TestLooksLikePath_basicExtension(t *testing.T) {
	cases := []string{"program.bas", "include.bi"}
	for _, c := range cases {
		if !looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = false, want true", c)
		}
	}
}

func TestLooksLikePath_noExtension(t *testing.T) {
	// Bare names without an extension or slash should not look like paths.
	cases := []string{"Makefile", "LICENSE", "README"}
	for _, c := range cases {
		if looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = true, want false (no recognised extension)", c)
		}
	}
}
