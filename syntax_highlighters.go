package harvey

import (
	"strings"
)

// ─── Byte-level identifier helpers ───────────────────────────────────────────

// hlIdentStart reports whether b is a valid identifier start byte (ASCII only).
func hlIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

// hlIdentChar reports whether b may appear inside an identifier.
func hlIdentChar(b byte) bool { return hlIdentStart(b) || (b >= '0' && b <= '9') }

// ─── langHighlighter ─────────────────────────────────────────────────────────

// langHighlighter holds language-specific tokenisation parameters and applies
// ANSI colour codes to source text.
type langHighlighter struct {
	style        HighlightStyle
	keywords     map[string]bool
	lineComment1 string // primary line comment prefix, e.g. "//"
	lineComment2 string // secondary line comment prefix, e.g. "'" (Basic)
	lineComment3 string // keyword-based line comment at line start, e.g. "REM" (Basic)
	blockOpen1   string // primary block comment open, e.g. "/*"
	blockClose1  string // primary block comment close, e.g. "*/"
	blockOpen2   string // secondary block comment open, e.g. "(*" or "{"
	blockClose2  string // secondary block comment close, e.g. "*)" or "}"
	dqStrings    bool   // double-quoted string literals
	sqStrings    bool   // single-quoted string / character literals
	btStrings    bool   // backtick raw-string literals (Go)
}

// highlight applies ANSI syntax colours to content and returns the result.
// Multi-line block comments and strings are tracked across line boundaries.
func (h *langHighlighter) highlight(content string) string {
	if content == "" {
		return content
	}

	var out strings.Builder
	out.Grow(len(content) + len(content)/4)

	const (
		stNormal = iota
		stBlock1
		stBlock2
		stDQ
		stSQ
		stBT
	)
	state := stNormal
	i, n := 0, len(content)

	for i < n {
		switch state {

		case stBlock1:
			if h.blockClose1 != "" && strings.HasPrefix(content[i:], h.blockClose1) {
				out.WriteString(h.blockClose1 + h.style.Reset)
				i += len(h.blockClose1)
				state = stNormal
			} else {
				out.WriteByte(content[i])
				i++
			}

		case stBlock2:
			if h.blockClose2 != "" && strings.HasPrefix(content[i:], h.blockClose2) {
				out.WriteString(h.blockClose2 + h.style.Reset)
				i += len(h.blockClose2)
				state = stNormal
			} else {
				out.WriteByte(content[i])
				i++
			}

		case stDQ:
			if content[i] == '\\' && i+1 < n {
				out.WriteByte(content[i])
				out.WriteByte(content[i+1])
				i += 2
			} else if content[i] == '"' {
				out.WriteByte('"')
				out.WriteString(h.style.Reset)
				i++
				state = stNormal
			} else {
				out.WriteByte(content[i])
				i++
			}

		case stSQ:
			if content[i] == '\\' && i+1 < n {
				out.WriteByte(content[i])
				out.WriteByte(content[i+1])
				i += 2
			} else if content[i] == '\'' {
				out.WriteByte('\'')
				out.WriteString(h.style.Reset)
				i++
				state = stNormal
			} else {
				out.WriteByte(content[i])
				i++
			}

		case stBT:
			if content[i] == '`' {
				out.WriteByte('`')
				out.WriteString(h.style.Reset)
				i++
				state = stNormal
			} else {
				out.WriteByte(content[i])
				i++
			}

		case stNormal:
			// Keyword-based line comment (only at the start of a line, e.g. "REM").
			if h.lineComment3 != "" && (i == 0 || content[i-1] == '\n') {
				upper := strings.ToUpper(content[i:])
				rem := h.lineComment3
				if strings.HasPrefix(upper, rem) &&
					(len(content[i:]) == len(rem) || !hlIdentChar(content[i+len(rem)])) {
					j := i
					for j < n && content[j] != '\n' {
						j++
					}
					out.WriteString(h.style.Comment + content[i:j] + h.style.Reset)
					i = j
					continue
				}
			}

			// Primary block comment.
			if h.blockOpen1 != "" && strings.HasPrefix(content[i:], h.blockOpen1) {
				out.WriteString(h.style.Comment + h.blockOpen1)
				i += len(h.blockOpen1)
				state = stBlock1
				continue
			}

			// Secondary block comment; Pascal: skip {$...} compiler directives.
			if h.blockOpen2 != "" && strings.HasPrefix(content[i:], h.blockOpen2) {
				if content[i] == '{' && i+1 < n && content[i+1] == '$' {
					out.WriteByte(content[i])
					i++
					continue
				}
				out.WriteString(h.style.Comment + h.blockOpen2)
				i += len(h.blockOpen2)
				state = stBlock2
				continue
			}

			// Primary line comment.
			if h.lineComment1 != "" && strings.HasPrefix(content[i:], h.lineComment1) {
				j := i
				for j < n && content[j] != '\n' {
					j++
				}
				out.WriteString(h.style.Comment + content[i:j] + h.style.Reset)
				i = j
				continue
			}

			// Secondary line comment (e.g. "'" for Basic).
			if h.lineComment2 != "" && strings.HasPrefix(content[i:], h.lineComment2) {
				j := i
				for j < n && content[j] != '\n' {
					j++
				}
				out.WriteString(h.style.Comment + content[i:j] + h.style.Reset)
				i = j
				continue
			}

			// Double-quoted string.
			if h.dqStrings && content[i] == '"' {
				out.WriteString(h.style.String + `"`)
				i++
				state = stDQ
				continue
			}

			// Single-quoted string / char literal.
			if h.sqStrings && content[i] == '\'' {
				out.WriteString(h.style.String + `'`)
				i++
				state = stSQ
				continue
			}

			// Backtick raw-string literal.
			if h.btStrings && content[i] == '`' {
				out.WriteString(h.style.String + "`")
				i++
				state = stBT
				continue
			}

			// Identifier or keyword.
			if hlIdentStart(content[i]) {
				j := i + 1
				for j < n && hlIdentChar(content[j]) {
					j++
				}
				word := content[i:j]
				if h.keywords[strings.ToLower(word)] {
					out.WriteString(h.style.Keyword + word + h.style.Reset)
				} else {
					out.WriteString(word)
				}
				i = j
				continue
			}

			// Numeric literal (not preceded by an identifier char).
			if content[i] >= '0' && content[i] <= '9' && (i == 0 || !hlIdentChar(content[i-1])) {
				j := i + 1
				for j < n {
					b := content[j]
					if b >= '0' && b <= '9' || b == '.' || b == '_' ||
						b == 'x' || b == 'X' || b == 'o' || b == 'O' || b == 'b' || b == 'B' ||
						(b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') ||
						b == 'e' || b == 'E' {
						j++
					} else {
						break
					}
				}
				out.WriteString(h.style.Number + content[i:j] + h.style.Reset)
				i = j
				continue
			}

			out.WriteByte(content[i])
			i++
		}
	}

	if state != stNormal {
		out.WriteString(h.style.Reset)
	}
	return out.String()
}

// ─── Keyword sets ─────────────────────────────────────────────────────────────

// makeKWSet creates a lowercase keyword map from the given words.
func makeKWSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[strings.ToLower(w)] = true
	}
	return m
}

func cHighlightKeywords() map[string]bool {
	m := make(map[string]bool, len(cKeywords)+8)
	for k := range cKeywords {
		m[k] = true
	}
	for _, k := range []string{"size_t", "ssize_t", "uint8_t", "uint16_t", "uint32_t", "uint64_t",
		"int8_t", "int16_t", "int32_t", "int64_t", "ptrdiff_t"} {
		m[k] = true
	}
	return m
}

func pascalKeywords() map[string]bool {
	return makeKWSet(
		"program", "unit", "uses", "interface", "implementation",
		"initialization", "finalization", "begin", "end",
		"procedure", "function", "const", "var", "type",
		"record", "array", "set", "object", "class", "inherited",
		"of", "to", "downto", "if", "then", "else",
		"for", "while", "do", "repeat", "until", "with",
		"case", "and", "or", "not", "xor", "shl", "shr",
		"div", "mod", "in", "nil", "true", "false",
		"string", "integer", "boolean", "real", "char", "byte",
		"word", "longint", "shortint", "smallint",
		"cardinal", "pointer", "file", "text", "new", "dispose",
		"writeln", "write", "readln", "read",
	)
}

func oberonKeywords() map[string]bool {
	return makeKWSet(
		"module", "import", "const", "type", "var",
		"procedure", "begin", "end",
		"if", "then", "else", "elsif",
		"while", "do", "repeat", "until",
		"for", "to", "by", "case", "of",
		"exit", "return", "loop", "with",
		"record", "array", "pointer", "nil",
		"is", "in", "true", "false",
		"div", "mod", "or", "and", "not",
		"integer", "real", "boolean", "char", "byte",
		"set", "new", "copy",
	)
}

func lispKeywords() map[string]bool {
	return makeKWSet(
		"defun", "defmacro", "defmethod", "defgeneric",
		"defvar", "defparameter", "defconstant",
		"defclass", "defstruct", "deftype", "defpackage",
		"let", "let*", "flet", "labels", "lambda",
		"progn", "if", "when", "unless", "cond", "case",
		"loop", "dotimes", "dolist", "do",
		"mapcar", "mapc", "maplist", "apply", "funcall",
		"format", "print", "prin1", "princ",
		"car", "cdr", "cons", "list", "append", "reverse",
		"and", "or", "not", "null", "t", "nil",
		"setf", "setq", "push", "pop",
		"return", "return-from", "block",
		"declare", "quote", "function",
		"in-package", "use-package", "export",
	)
}

func basicKeywords() map[string]bool {
	return makeKWSet(
		"dim", "as", "integer", "string", "float", "double", "boolean", "long",
		"let", "for", "to", "next", "step",
		"do", "loop", "while", "wend", "until",
		"if", "then", "else", "elseif", "end",
		"select", "case",
		"sub", "function", "return",
		"goto", "gosub", "on", "error", "resume",
		"print", "input", "write", "read",
		"and", "or", "not", "xor", "mod",
		"true", "false", "null", "nothing",
		"new", "delete", "redim", "preserve",
		"byref", "byval", "optional",
		"public", "private", "static", "shared",
		"type", "with", "each", "in",
	)
}

func goKeywords() map[string]bool {
	return makeKWSet(
		"break", "case", "chan", "const", "continue", "default",
		"defer", "else", "fallthrough", "for", "func", "go",
		"goto", "if", "import", "interface", "map", "package",
		"range", "return", "select", "struct", "switch", "type", "var",
		"nil", "true", "false", "iota",
		"append", "cap", "close", "complex", "copy", "delete",
		"imag", "len", "make", "new", "panic", "print", "println",
		"real", "recover",
		"bool", "byte", "complex64", "complex128", "error",
		"float32", "float64", "int", "int8", "int16", "int32", "int64",
		"rune", "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"any",
	)
}

func pythonKeywords() map[string]bool {
	return makeKWSet(
		"false", "none", "true",
		"and", "as", "assert", "async", "await",
		"break", "class", "continue", "def", "del",
		"elif", "else", "except", "finally", "for",
		"from", "global", "if", "import", "in",
		"is", "lambda", "nonlocal", "not", "or",
		"pass", "raise", "return", "try", "while",
		"with", "yield",
		"print", "input", "len", "range", "type",
		"list", "dict", "set", "tuple",
		"int", "float", "str", "bool", "bytes", "object",
		"open", "super", "self", "cls",
		"enumerate", "zip", "map", "filter", "sorted",
		"min", "max", "sum", "abs", "round",
	)
}

func jsKeywords() map[string]bool {
	return makeKWSet(
		"abstract", "async", "await",
		"break", "case", "catch", "class", "const", "continue",
		"debugger", "default", "delete", "do",
		"else", "enum", "export", "extends",
		"false", "finally", "for", "from", "function",
		"if", "implements", "import", "in", "instanceof", "interface",
		"let", "new", "null", "of", "package",
		"private", "protected", "public",
		"return", "static", "super", "switch", "this",
		"throw", "true", "try", "type", "typeof",
		"undefined", "var", "void", "while", "with", "yield",
		"any", "boolean", "declare", "keyof", "namespace",
		"never", "number", "readonly", "string", "symbol", "unknown",
		"console", "document", "window", "process",
	)
}

func rustKeywords() map[string]bool {
	return makeKWSet(
		"as", "break", "const", "continue", "crate",
		"else", "enum", "extern", "false", "fn",
		"for", "if", "impl", "in", "let", "loop",
		"match", "mod", "move", "mut", "pub",
		"ref", "return", "self", "super", "struct",
		"trait", "true", "type", "unsafe", "use",
		"where", "while", "async", "await", "dyn",
		"bool", "char", "f32", "f64",
		"i8", "i16", "i32", "i64", "i128", "isize",
		"str", "u8", "u16", "u32", "u64", "u128", "usize",
		"Option", "Some", "None", "Result", "Ok", "Err",
		"Vec", "String", "Box",
		"println", "print", "eprintln", "format",
		"panic", "todo", "unimplemented", "unreachable",
	)
}

func shellKeywords() map[string]bool {
	return makeKWSet(
		"if", "then", "else", "elif", "fi",
		"for", "while", "until", "do", "done",
		"case", "in", "esac", "function", "return", "exit",
		"local", "export", "declare", "readonly",
		"true", "false",
		"echo", "printf", "read",
		"cd", "ls", "mkdir", "rm", "cp", "mv",
		"grep", "sed", "awk", "find", "sort",
		"cat", "head", "tail", "wc", "test", "source",
	)
}

func sqlKeywords() map[string]bool {
	return makeKWSet(
		"select", "from", "where", "insert", "into", "values",
		"update", "set", "delete", "create", "drop", "alter",
		"table", "index", "view", "database", "schema",
		"join", "inner", "outer", "left", "right", "full", "cross",
		"on", "using", "as", "distinct", "all", "any",
		"and", "or", "not", "in", "like", "between", "exists",
		"is", "null", "true", "false",
		"order", "by", "group", "having", "limit", "offset",
		"union", "intersect", "except",
		"begin", "commit", "rollback", "transaction",
		"primary", "key", "foreign", "references", "unique",
		"constraint", "default", "check",
		"integer", "text", "real", "blob", "numeric", "boolean",
		"varchar", "char", "date", "time", "datetime", "timestamp",
		"count", "sum", "avg", "min", "max", "coalesce",
		"case", "when", "then", "end",
	)
}

// ─── Default highlight style ─────────────────────────────────────────────────

func defaultHighlightStyle() HighlightStyle {
	return HighlightStyle{
		Keyword: "\033[1;34m", // bold blue
		String:  ansiGreen,
		Comment: ansiDim,
		Number:  ansiYellow,
		Reset:   ansiReset,
	}
}

// ─── TerminalHighlighter ──────────────────────────────────────────────────────

/** TerminalHighlighter applies ANSI syntax highlighting to source code for
 * terminal display.  A single instance handles all registered languages by
 * dispatching to per-language tokenisers based on the lang parameter.
 *
 * Implementations must strip existing ANSI sequences from the input before
 * adding their own — this is satisfied here because code blocks extracted from
 * LLM responses are not expected to contain prior ANSI codes.
 *
 * Example:
 *   h := NewTerminalHighlighter()
 *   coloured := h.Highlight("int main(){return 0;}", "c")
 */
type TerminalHighlighter struct {
	langs map[string]*langHighlighter
}

/** NewTerminalHighlighter returns a TerminalHighlighter pre-loaded with
 * highlighters for all supported languages.
 *
 * Returns:
 *   *TerminalHighlighter — ready-to-use multi-language highlighter.
 *
 * Example:
 *   h := NewTerminalHighlighter()
 */
func NewTerminalHighlighter() *TerminalHighlighter {
	h := &TerminalHighlighter{langs: make(map[string]*langHighlighter)}
	h.registerAll()
	return h
}

/** Highlight returns content with ANSI colour codes for the given language.
 * Returns content unchanged when lang is not recognised.
 *
 * Parameters:
 *   content (string) — source text to highlight.
 *   lang    (string) — language ID (e.g. "go", "c", "python").
 *
 * Returns:
 *   string — highlighted text, or content unchanged when lang is unknown.
 *
 * Example:
 *   coloured := h.Highlight("void foo(){}", "c")
 */
func (h *TerminalHighlighter) Highlight(content string, lang string) string {
	lh := h.langs[strings.ToLower(lang)]
	if lh == nil {
		return content
	}
	return lh.highlight(content)
}

/** GetStyle returns the HighlightStyle for the given language, or nil when the
 * language is not registered.
 *
 * Parameters:
 *   lang (string) — language ID.
 *
 * Returns:
 *   *HighlightStyle — colour configuration, or nil.
 *
 * Example:
 *   style := h.GetStyle("go")
 */
func (h *TerminalHighlighter) GetStyle(lang string) *HighlightStyle {
	lh := h.langs[strings.ToLower(lang)]
	if lh == nil {
		return nil
	}
	style := lh.style
	return &style
}

// registerAll populates h.langs with per-language highlighters.
func (h *TerminalHighlighter) registerAll() {
	style := defaultHighlightStyle()

	cSpec := &langHighlighter{
		style: style, keywords: cHighlightKeywords(),
		lineComment1: "//", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, sqStrings: true,
	}
	h.langs["c"] = cSpec
	h.langs["cpp"] = cSpec

	h.langs["pascal"] = &langHighlighter{
		style: style, keywords: pascalKeywords(),
		lineComment1: "//",
		blockOpen1: "(*", blockClose1: "*)",
		blockOpen2: "{", blockClose2: "}",
		sqStrings: true,
	}

	h.langs["oberon"] = &langHighlighter{
		style: style, keywords: oberonKeywords(),
		blockOpen1: "(*", blockClose1: "*)",
		dqStrings: true,
	}

	h.langs["lisp"] = &langHighlighter{
		style: style, keywords: lispKeywords(),
		lineComment1: ";",
		blockOpen1: "#|", blockClose1: "|#",
		dqStrings: true,
	}

	h.langs["basic"] = &langHighlighter{
		style: style, keywords: basicKeywords(),
		lineComment2: "'", lineComment3: "REM",
		dqStrings: true,
	}

	h.langs["go"] = &langHighlighter{
		style: style, keywords: goKeywords(),
		lineComment1: "//", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, btStrings: true,
	}

	h.langs["python"] = &langHighlighter{
		style: style, keywords: pythonKeywords(),
		lineComment1: "#",
		dqStrings: true, sqStrings: true,
	}

	jsSpec := &langHighlighter{
		style: style, keywords: jsKeywords(),
		lineComment1: "//", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, sqStrings: true, btStrings: true,
	}
	h.langs["javascript"] = jsSpec
	h.langs["typescript"] = jsSpec

	h.langs["rust"] = &langHighlighter{
		style: style, keywords: rustKeywords(),
		lineComment1: "//", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, sqStrings: true,
	}

	h.langs["shell"] = &langHighlighter{
		style: style, keywords: shellKeywords(),
		lineComment1: "#",
		dqStrings: true, sqStrings: true,
	}

	h.langs["sql"] = &langHighlighter{
		style: style, keywords: sqlKeywords(),
		lineComment1: "--", blockOpen1: "/*", blockClose1: "*/",
		dqStrings: true, sqStrings: true,
	}
}

// ─── Response text processing ─────────────────────────────────────────────────

// highlightCodeBlocks finds fenced code blocks in LLM response text and
// replaces their content with ANSI-coloured versions.  Blocks for unregistered
// languages are returned unchanged.  The surrounding non-code text and fence
// markers are preserved verbatim.
func highlightCodeBlocks(text string) string {
	if !strings.Contains(text, "```") {
		return text
	}

	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))

	inFence := false
	var fenceLang string
	var fenceLines []string

	for _, line := range lines {
		if !inFence {
			if strings.HasPrefix(line, "```") {
				inFence = true
				fenceLang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
				fenceLines = nil
				result = append(result, line)
				continue
			}
			result = append(result, line)
			continue
		}

		if strings.HasPrefix(line, "```") {
			body := strings.Join(fenceLines, "\n")
			result = append(result, applyHighlighting(body, fenceLang))
			result = append(result, line)
			inFence = false
			fenceLines = nil
			continue
		}

		fenceLines = append(fenceLines, line)
	}

	// Unclosed fence: emit raw.
	if inFence {
		result = append(result, fenceLines...)
	}

	return strings.Join(result, "\n")
}

// applyHighlighting looks up lang in the global registry and applies syntax
// highlighting. Returns content unchanged when no highlighter is registered.
// Common language-hint aliases (e.g. "c++" → "cpp", "py" → "python") are
// normalised before the lookup.
func applyHighlighting(content, lang string) string {
	langID := strings.ToLower(strings.TrimSpace(lang))
	switch langID {
	case "c++", "c++11", "c++14", "c++17", "c++20", "c++23":
		langID = "cpp"
	case "js":
		langID = "javascript"
	case "ts":
		langID = "typescript"
	case "py":
		langID = "python"
	case "sh", "bash", "zsh":
		langID = "shell"
	case "rs":
		langID = "rust"
	}
	h := globalRegistry.GetHighlighter(langID)
	if h == nil {
		return content
	}
	return h.Highlight(content, langID)
}

// ─── Registry wiring ─────────────────────────────────────────────────────────

/** SetHighlighter registers a SyntaxHighlighter for an already-registered
 * language ID.
 *
 * Parameters:
 *   id (string)           — language identifier, e.g. "c".
 *   h  (SyntaxHighlighter)— highlighter to register.
 *
 * Example:
 *   r.SetHighlighter("c", NewTerminalHighlighter())
 */
func (r *LanguageRegistry) SetHighlighter(id string, h SyntaxHighlighter) {
	r.highlighters[id] = h
}

// initHighlighters wires Phase-5 terminal syntax highlighters into r for all
// languages with HasHighlighting=true plus common script and data languages.
func initHighlighters(r *LanguageRegistry) {
	h := NewTerminalHighlighter()
	for _, id := range []string{
		"c", "cpp", "pascal", "oberon", "lisp", "basic",
		"go", "python", "javascript", "typescript", "rust",
		"shell", "sql",
	} {
		r.SetHighlighter(id, h)
	}
}
