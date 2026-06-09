package harvey

import (
	"bytes"
	"path/filepath"
	"strings"
)

// maxKeywordScan is the maximum number of bytes scanned for keyword-based
// language detection.  Source files almost always declare their language at the
// top, so 4 KiB is more than enough.
const maxKeywordScan = 4096

// ─── Text / binary helpers ────────────────────────────────────────────────────

/** isTextContent returns true when data appears to be text (not binary).
 * It is the logical complement of isBinary: any file that does not contain a
 * null byte in its first 512 bytes is considered text.
 *
 * Parameters:
 *   data ([]byte) — file contents to inspect.
 *
 * Returns:
 *   bool — true for text, false for binary.
 *
 * Example:
 *   if !isTextContent(data) { return 0, nil } // skip binary files
 */
func isTextContent(data []byte) bool {
	return !isBinary(data)
}

// ─── Shebang detection ────────────────────────────────────────────────────────

// shebangEntry maps a substring of a shebang line to a language ID.
type shebangEntry struct {
	pattern string // substring to look for in the first line after "#!"
	langID  string
}

// shebangTable lists known shebang patterns in matching priority order.
// Entries are checked via strings.Contains against the first line of content,
// so patterns should be specific enough to avoid false positives.
var shebangTable = []shebangEntry{
	{pattern: "python3",  langID: "python"},
	{pattern: "python2",  langID: "python"},
	{pattern: "/python",  langID: "python"},  // #!/usr/bin/python
	{pattern: "node",     langID: "javascript"},
	{pattern: "bash",     langID: "shell"},   // #!/bin/bash or #!/usr/bin/env bash
	{pattern: "/sh",      langID: "shell"},   // #!/bin/sh
	{pattern: "clisp",    langID: "lisp"},
	{pattern: "sbcl",     langID: "lisp"},
	{pattern: "/tcc",     langID: "c"},       // #!/usr/bin/env tcc (scripting C)
}

/** detectShebang reads the first line of content and returns the language ID
 * if it is a recognised shebang line.  Returns ("", false) when the first two
 * bytes are not "#!" or no known pattern is matched.
 *
 * Parameters:
 *   content ([]byte) — file contents (need not be the whole file).
 *
 * Returns:
 *   langID (string) — language ID, or "".
 *   ok     (bool)   — true when a shebang pattern was matched.
 *
 * Example:
 *   lang, ok := detectShebang([]byte("#!/usr/bin/env python3\n# …"))
 *   // "python", true
 */
func detectShebang(content []byte) (string, bool) {
	if len(content) < 2 || content[0] != '#' || content[1] != '!' {
		return "", false
	}
	end := bytes.IndexByte(content, '\n')
	if end < 0 {
		end = len(content)
	}
	firstLine := string(content[:end])
	for _, e := range shebangTable {
		if strings.Contains(firstLine, e.pattern) {
			return e.langID, true
		}
	}
	return "", false
}

// ─── Keyword detection ────────────────────────────────────────────────────────

// keywordPattern associates a language-specific keyword or construct with a
// confidence score.
type keywordPattern struct {
	langID      string
	pattern     string  // case-sensitive substring to match
	confidence  float64 // detection confidence in (0, 1]
	atLineStart bool    // when true, pattern must follow a newline (or be at offset 0)
}

// keywordPatterns lists high-signal language keywords in no particular order.
// Only patterns that strongly indicate a specific language are included;
// generic tokens (e.g. "class", "function") are intentionally omitted.
var keywordPatterns = []keywordPattern{
	// Go
	{"go",     "package ",          0.85, true},
	{"go",     "func main()",       0.95, false},

	// Rust
	{"rust",   "fn main()",         0.90, false},
	{"rust",   "use std::",         0.80, false},

	// C — angle-bracket and quoted includes are both distinctive at line start
	{"c",      "#include <",        0.80, true},
	{"c",      "#include \"",       0.80, true},
	{"c",      "int main(",         0.85, false},

	// C++ — <iostream> and namespace/template are unambiguous
	{"cpp",    "#include <iostream>", 0.95, true},
	{"cpp",    "namespace ",          0.85, false},
	{"cpp",    "template <",          0.90, false},

	// Oberon — capital keywords are highly distinctive; MODULE is unique
	{"oberon", "MODULE ",   0.90, true},
	{"oberon", "PROCEDURE ", 0.80, true},
	{"oberon", "IMPORT ",   0.80, true},

	// Pascal — lowercase procedure/program distinguishes it from Oberon
	{"pascal", "program ",   0.85, true},
	{"pascal", "uses ",      0.85, true},
	{"pascal", "procedure ", 0.70, true},

	// Lisp — s-expressions with defun/defvar are unmistakeable
	{"lisp",   "(defun ",    0.95, false},
	{"lisp",   "(defvar ",   0.90, false},
	{"lisp",   "(defmacro ", 0.95, false},
	{"lisp",   "(define ",   0.80, false},

	// Basic
	{"basic",  "SUB ",  0.85, true},
	{"basic",  "DIM ",  0.80, true},
	{"basic",  "REM ",  0.75, true},

	// HTML
	{"html",   "<!DOCTYPE", 0.95, false},
	{"html",   "<html",     0.90, false},

	// SQL
	{"sql",    "CREATE TABLE", 0.95, true},
	{"sql",    "SELECT ",      0.80, true},
	{"sql",    "INSERT INTO",  0.90, true},
}

// matchAtLineStart returns true when pattern appears at the start of content
// or immediately after a newline byte.
func matchAtLineStart(content []byte, pattern string) bool {
	p := []byte(pattern)
	if bytes.HasPrefix(content, p) {
		return true
	}
	needle := append([]byte{'\n'}, p...)
	return bytes.Contains(content, needle)
}

/** detectKeywords scans up to maxKeywordScan bytes of content for
 * language-specific keyword patterns and returns the language with the highest
 * matching confidence.  Returns ("", 0.0) when no pattern scores above 0.
 *
 * Parameters:
 *   content ([]byte) — file contents to scan.
 *
 * Returns:
 *   langID (string)  — language ID of the best match, or "".
 *   conf   (float64) — confidence of the best match in (0, 1], or 0.
 *
 * Example:
 *   lang, conf := detectKeywords([]byte("MODULE Foo; IMPORT In;\n"))
 *   // "oberon", 0.9
 */
func detectKeywords(content []byte) (string, float64) {
	scan := content
	if len(scan) > maxKeywordScan {
		scan = scan[:maxKeywordScan]
	}

	// Accumulate the highest confidence seen for each language.
	best := make(map[string]float64, 4)
	for _, kp := range keywordPatterns {
		var matched bool
		if kp.atLineStart {
			matched = matchAtLineStart(scan, kp.pattern)
		} else {
			matched = bytes.Contains(scan, []byte(kp.pattern))
		}
		if matched && kp.confidence > best[kp.langID] {
			best[kp.langID] = kp.confidence
		}
	}

	var bestLang string
	var bestConf float64
	for lang, conf := range best {
		if conf > bestConf || (conf == bestConf && lang < bestLang) {
			bestLang = lang
			bestConf = conf
		}
	}
	return bestLang, bestConf
}

// ─── ExtensionDetector ────────────────────────────────────────────────────────

/** ExtensionDetector implements LanguageDetector using file-extension matching.
 * It is language-specific: it only returns its own langID.
 *
 * Example:
 *   d := NewExtensionDetector("go", []string{".go", ".mod", ".sum"})
 *   lang, conf := d.Detect("main.go", nil) // "go", 1.0
 */
type ExtensionDetector struct {
	langID string
	exts   []string
}

/** NewExtensionDetector returns an ExtensionDetector for the given language.
 *
 * Parameters:
 *   langID (string)   — language identifier, e.g. "go".
 *   exts   ([]string) — file extensions including the dot, e.g. [".go"].
 *
 * Returns:
 *   *ExtensionDetector — ready-to-use detector.
 *
 * Example:
 *   d := NewExtensionDetector("pascal", []string{".pas", ".p"})
 */
func NewExtensionDetector(langID string, exts []string) *ExtensionDetector {
	return &ExtensionDetector{langID: langID, exts: exts}
}

/** Detect returns (langID, 1.0) when the file extension matches, otherwise
 * ("", 0.0).  content is ignored.
 *
 * Parameters:
 *   filePath (string) — path to the file; only the extension is used.
 *   content  ([]byte) — ignored.
 *
 * Returns:
 *   language   (string)  — language ID or "".
 *   confidence (float64) — 1.0 on match, 0.0 otherwise.
 *
 * Example:
 *   lang, conf := d.Detect("hello.pas", nil) // "pascal", 1.0
 */
func (d *ExtensionDetector) Detect(filePath string, content []byte) (string, float64) {
	lang, ok := d.DetectFromExtension(filepath.Ext(filePath))
	if ok {
		return lang, 1.0
	}
	return "", 0.0
}

/** DetectFromExtension returns the language ID when ext exactly matches one of
 * the registered extensions for this language.  A case-insensitive comparison
 * is also tried as a fallback (e.g., ".Mod" and ".mod" both match Oberon).
 *
 * Parameters:
 *   ext (string) — file extension with leading dot, e.g. ".go".
 *
 * Returns:
 *   language (string) — language ID or "".
 *   ok       (bool)   — true on match.
 *
 * Example:
 *   lang, ok := d.DetectFromExtension(".Mod") // "oberon", true
 */
func (d *ExtensionDetector) DetectFromExtension(ext string) (string, bool) {
	for _, e := range d.exts {
		if e == ext {
			return d.langID, true
		}
	}
	// Case-insensitive fallback.
	for _, e := range d.exts {
		if strings.EqualFold(e, ext) {
			return d.langID, true
		}
	}
	return "", false
}

// ─── ContentDetector ──────────────────────────────────────────────────────────

/** ContentDetector implements LanguageDetector using shebang and keyword
 * pattern matching.  It is language-specific: it only returns its own langID
 * so that it can be registered per-language in the registry.
 *
 * Example:
 *   d := NewContentDetector("python")
 *   lang, conf := d.Detect("script", []byte("#!/usr/bin/env python3\n"))
 *   // "python", 0.9
 */
type ContentDetector struct {
	langID string
}

/** NewContentDetector returns a ContentDetector for the given language.
 *
 * Parameters:
 *   langID (string) — language identifier, e.g. "python".
 *
 * Returns:
 *   *ContentDetector — ready-to-use detector.
 *
 * Example:
 *   d := NewContentDetector("lisp")
 */
func NewContentDetector(langID string) *ContentDetector {
	return &ContentDetector{langID: langID}
}

/** Detect scans content for shebang lines and keyword patterns.  It returns
 * a non-zero confidence only when the detected language matches this
 * detector's langID.  Binary content always returns ("", 0.0).
 *
 * Parameters:
 *   filePath (string) — path to the file; used only for context, not parsed.
 *   content  ([]byte) — file contents to inspect.
 *
 * Returns:
 *   language   (string)  — this detector's langID or "".
 *   confidence (float64) — 0.9 for shebang, keyword confidence otherwise; 0 on no match.
 *
 * Example:
 *   lang, conf := d.Detect("", []byte("(defun hello () nil)"))
 *   // "lisp", 0.95
 */
func (d *ContentDetector) Detect(_ string, content []byte) (string, float64) {
	if isBinary(content) {
		return "", 0.0
	}
	if lang, ok := detectShebang(content); ok && lang == d.langID {
		return lang, 0.9
	}
	if lang, conf := detectKeywords(content); lang == d.langID && conf > 0.5 {
		return lang, conf
	}
	return "", 0.0
}

/** DetectFromExtension always returns ("", false) for a ContentDetector because
 * content-based detection does not use file extensions.
 *
 * Parameters:
 *   ext (string) — ignored.
 *
 * Returns:
 *   language (string) — always "".
 *   ok       (bool)   — always false.
 *
 * Example:
 *   lang, ok := d.DetectFromExtension(".py") // "", false
 */
func (d *ContentDetector) DetectFromExtension(_ string) (string, bool) {
	return "", false
}

// ─── CombinedDetector ─────────────────────────────────────────────────────────

/** CombinedDetector implements LanguageDetector by trying extension-based
 * detection first (confidence 1.0) and falling back to content-based detection
 * when the extension is absent or unrecognised.
 *
 * Example:
 *   d := NewCombinedDetector("python", []string{".py"})
 *   lang, conf := d.Detect("script", []byte("#!/usr/bin/env python3\n"))
 *   // "python", 0.9  (no extension → content fallback)
 */
type CombinedDetector struct {
	ext     *ExtensionDetector
	content *ContentDetector
}

/** NewCombinedDetector returns a CombinedDetector for the given language.
 *
 * Parameters:
 *   langID (string)   — language identifier, e.g. "go".
 *   exts   ([]string) — file extensions for this language, e.g. [".go"].
 *
 * Returns:
 *   *CombinedDetector — ready-to-use detector.
 *
 * Example:
 *   d := NewCombinedDetector("oberon", []string{".Mod", ".obn"})
 */
func NewCombinedDetector(langID string, exts []string) *CombinedDetector {
	return &CombinedDetector{
		ext:     NewExtensionDetector(langID, exts),
		content: NewContentDetector(langID),
	}
}

/** Detect returns the language and confidence, trying extension first.
 * If the extension yields no match, content inspection is attempted.
 *
 * Parameters:
 *   filePath (string) — path to the file.
 *   content  ([]byte) — file contents.
 *
 * Returns:
 *   language   (string)  — language ID or "".
 *   confidence (float64) — 1.0 for extension match; content confidence otherwise.
 *
 * Example:
 *   lang, conf := d.Detect("main.go", data) // "go", 1.0
 */
func (d *CombinedDetector) Detect(filePath string, content []byte) (string, float64) {
	if lang, conf := d.ext.Detect(filePath, content); conf > 0 {
		return lang, conf
	}
	return d.content.Detect(filePath, content)
}

/** DetectFromExtension delegates to the embedded ExtensionDetector.
 *
 * Parameters:
 *   ext (string) — file extension with leading dot.
 *
 * Returns:
 *   language (string) — language ID or "".
 *   ok       (bool)   — true on match.
 *
 * Example:
 *   lang, ok := d.DetectFromExtension(".go") // "go", true
 */
func (d *CombinedDetector) DetectFromExtension(ext string) (string, bool) {
	return d.ext.DetectFromExtension(ext)
}
