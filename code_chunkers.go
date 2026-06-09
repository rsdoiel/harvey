package harvey

import (
	"strings"
	"unicode"
)

// ─── Shared helpers ───────────────────────────────────────────────────────────

/** findLineCol returns the 1-indexed line number and 0-indexed byte offset from
 * the start of that line for the given byte offset within content.
 *
 * Parameters:
 *   content ([]byte) — file contents.
 *   offset  (int)    — byte position; clamped to [0, len(content)].
 *
 * Returns:
 *   line (int) — 1-indexed line number.
 *   col  (int) — 0-indexed byte offset from the start of the line.
 *
 * Example:
 *   line, col := findLineCol([]byte("ab\ncd"), 4) // 2, 1
 */
func findLineCol(content []byte, offset int) (line, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	line = 1
	lineStart := 0
	for i := 0; i < offset; i++ {
		if content[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, offset - lineStart
}

// isIdentRune reports whether r may appear in an identifier.
func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isASCIISpace reports whether b is an ASCII whitespace byte.
func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// makeChunk builds an EnrichedChunk from a byte-range, stripping leading and
// trailing whitespace and computing precise source-location metadata.
// Returns the zero value when the range is entirely whitespace.
func makeChunk(data []byte, startOff, endOff int, chunkType string, syms []string, docs string) EnrichedChunk {
	for startOff < endOff && isASCIISpace(data[startOff]) {
		startOff++
	}
	for endOff > startOff && isASCIISpace(data[endOff-1]) {
		endOff--
	}
	if startOff >= endOff {
		return EnrichedChunk{}
	}
	startLine, startCol := findLineCol(data, startOff)
	endLine, endCol := findLineCol(data, endOff-1)
	return EnrichedChunk{
		Content:   string(data[startOff:endOff]),
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
		ChunkType: chunkType,
		Symbols:   syms,
		Docs:      docs,
	}
}

// appendNonEmpty appends c to chunks only when its Content is non-empty.
func appendNonEmpty(chunks []EnrichedChunk, c EnrichedChunk) []EnrichedChunk {
	if c.Content != "" {
		return append(chunks, c)
	}
	return chunks
}

// isValidIdent reports whether s is a non-empty sequence of identifier characters.
func isValidIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if !isIdentRune(r) {
			return false
		}
	}
	return true
}

// ─── CChunker ────────────────────────────────────────────────────────────────

/** CChunker splits C or C++ source code into enriched chunks at brace-balanced
 * top-level boundaries.  Function definitions, struct/class definitions, and
 * preprocessor/declaration sections each become their own chunk.
 *
 * Example:
 *   c := NewCChunker("c")
 *   chunks := c.Chunk("#include <stdio.h>\nint main(){return 0;}\n", "main.c")
 */
type CChunker struct{ langID string }

/** NewCChunker returns a CChunker for the given language ID ("c" or "cpp").
 *
 * Parameters:
 *   langID (string) — "c" or "cpp".
 *
 * Returns:
 *   *CChunker — ready-to-use chunker.
 *
 * Example:
 *   c := NewCChunker("cpp")
 */
func NewCChunker(langID string) *CChunker { return &CChunker{langID: langID} }

/** Language returns the language ID this chunker handles.
 *
 * Returns:
 *   string — "c" or "cpp".
 *
 * Example:
 *   c.Language() // "c"
 */
func (c *CChunker) Language() string { return c.langID }

/** Chunk splits C/C++ source into enriched chunks.  Brace depth is tracked
 * through strings and comments so function bodies are never split.
 *
 * Parameters:
 *   content  (string) — C/C++ source text.
 *   filePath (string) — source path; used for context only.
 *
 * Returns:
 *   []EnrichedChunk — source chunks with location and symbol metadata.
 *
 * Example:
 *   chunks := c.Chunk("void foo(){}\nvoid bar(){}\n", "lib.c")
 */
func (c *CChunker) Chunk(content string, _ string) []EnrichedChunk {
	data := []byte(content)
	cuts := findCCutPoints(data)
	var chunks []EnrichedChunk
	prev := 0
	for _, cut := range cuts {
		ch := makeChunk(data, prev, cut, "", nil, "")
		if ch.Content != "" {
			ch.ChunkType, ch.Symbols = classifyC(ch.Content)
			chunks = appendNonEmpty(chunks, ch)
		}
		prev = cut
	}
	if prev < len(data) {
		ch := makeChunk(data, prev, len(data), "", nil, "")
		if ch.Content != "" {
			ch.ChunkType, ch.Symbols = classifyC(ch.Content)
			chunks = appendNonEmpty(chunks, ch)
		}
	}
	return chunks
}

// findCCutPoints scans C/C++ source and returns byte offsets where it is safe
// to split into chunks: after `}` returning to depth 0, and at blank lines
// when depth is already 0.
func findCCutPoints(data []byte) []int {
	type cst int
	const (
		csNormal cst = iota
		csLineComment
		csBlockComment
		csStrDouble
		csStrSingle
	)
	var cuts []int
	st := csNormal
	depth := 0
	for i := 0; i < len(data); i++ {
		b := data[i]
		switch st {
		case csNormal:
			switch b {
			case '/':
				if i+1 < len(data) {
					switch data[i+1] {
					case '/':
						st = csLineComment
						i++
					case '*':
						st = csBlockComment
						i++
					}
				}
			case '"':
				st = csStrDouble
			case '\'':
				st = csStrSingle
			case '{':
				depth++
			case '}':
				if depth > 0 {
					depth--
				}
				if depth == 0 {
					cuts = append(cuts, i+1)
				}
			case '\n':
				if depth == 0 && i+1 < len(data) && data[i+1] == '\n' {
					cuts = append(cuts, i+1)
					for i+1 < len(data) && data[i+1] == '\n' {
						i++
					}
				}
			}
		case csLineComment:
			if b == '\n' {
				st = csNormal
				if depth == 0 && i+1 < len(data) && data[i+1] == '\n' {
					cuts = append(cuts, i+1)
					for i+1 < len(data) && data[i+1] == '\n' {
						i++
					}
				}
			}
		case csBlockComment:
			if b == '*' && i+1 < len(data) && data[i+1] == '/' {
				st = csNormal
				i++
			}
		case csStrDouble:
			if b == '\\' {
				i++
			} else if b == '"' {
				st = csNormal
			}
		case csStrSingle:
			if b == '\\' {
				i++
			} else if b == '\'' {
				st = csNormal
			}
		}
	}
	return cuts
}

// cKeywords is the set of reserved C and C++ keywords used to filter false
// positives in function-name extraction.
var cKeywords = map[string]bool{
	"if": true, "else": true, "for": true, "while": true, "do": true,
	"switch": true, "case": true, "default": true, "return": true,
	"break": true, "continue": true, "goto": true, "sizeof": true,
	"typedef": true, "struct": true, "union": true, "enum": true,
	"void": true, "int": true, "char": true, "long": true, "short": true,
	"unsigned": true, "signed": true, "float": true, "double": true,
	"const": true, "static": true, "extern": true, "register": true,
	"volatile": true, "inline": true, "restrict": true, "auto": true,
	"class": true, "namespace": true, "template": true,
	"public": true, "private": true, "protected": true,
	"virtual": true, "override": true, "new": true, "delete": true,
	"this": true, "nullptr": true, "true": true, "false": true,
	"try": true, "catch": true, "throw": true,
}

// classifyC returns the chunk type and primary symbol(s) for a C/C++ chunk.
// It scans lines for the first identifier-before-`(` pattern that is not a
// keyword, treating that as a function name.
func classifyC(text string) (chunkType string, symbols []string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		idx := strings.IndexByte(line, '(')
		if idx <= 0 {
			continue
		}
		before := strings.TrimRight(line[:idx], " \t*&")
		fields := strings.Fields(before)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimLeft(fields[len(fields)-1], "*&")
		if isValidIdent(name) && !cKeywords[strings.ToLower(name)] {
			return "function", []string{name}
		}
	}
	upper := strings.ToUpper(text)
	for _, kw := range []string{"STRUCT ", "ENUM ", "TYPEDEF ", "CLASS "} {
		if strings.Contains(upper, kw) {
			return "type", nil
		}
	}
	return "code", nil
}

// ─── BEGIN/END shared chunker (Pascal + Oberon) ───────────────────────────────

// stripBlockLineComment removes same-line `{ }`, `(* *)`, and `//` comments
// from line for keyword-detection purposes.
func stripBlockLineComment(line string) string {
	// Strip // line comments.
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = line[:idx]
	}
	// Strip { } inline comments (same line only).
	for {
		s := strings.Index(line, "{")
		if s < 0 {
			break
		}
		e := strings.Index(line[s+1:], "}")
		if e < 0 {
			line = line[:s]
			break
		}
		line = line[:s] + line[s+1+e+1:]
	}
	// Strip (* *) inline comments (same line only).
	for {
		s := strings.Index(line, "(*")
		if s < 0 {
			break
		}
		e := strings.Index(line[s+2:], "*)")
		if e < 0 {
			line = line[:s]
			break
		}
		line = line[:s] + line[s+2+e+2:]
	}
	return line
}

// chunkBeginEnd is the shared implementation for Pascal and Oberon chunkers.
// procStart returns true when the first word of a line starts a procedure/function.
// extractSym extracts the symbol name from the first line of a procedure header.
func chunkBeginEnd(
	content string,
	procStart func(firstWord string) bool,
	extractSym func(header string) string,
) []EnrichedChunk {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var chunks []EnrichedChunk

	var pending []string
	pendingStart := 1
	var current []string
	currentStart := 1
	inProc := false
	depth := 0
	bodyStarted := false

	flushPending := func(beforeLine int) {
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		if text == "" {
			return
		}
		end := beforeLine - 1
		if end < pendingStart {
			end = pendingStart
		}
		chunks = append(chunks, EnrichedChunk{
			Content:   text,
			StartLine: pendingStart,
			EndLine:   end,
			ChunkType: "code",
		})
	}

	flushCurrent := func(endLine int) {
		if len(current) == 0 {
			return
		}
		firstLine := current[0]
		text := strings.TrimSpace(strings.Join(current, "\n"))
		current = nil
		if text == "" {
			return
		}
		sym := extractSym(firstLine)
		var syms []string
		if sym != "" {
			syms = []string{sym}
		}
		chunks = append(chunks, EnrichedChunk{
			Content:   text,
			StartLine: currentStart,
			EndLine:   endLine,
			ChunkType: "procedure",
			Symbols:   syms,
		})
	}

	for i, line := range lines {
		lineNum := i + 1
		stripped := stripBlockLineComment(line)
		upper := strings.ToUpper(strings.TrimSpace(stripped))
		words := strings.Fields(upper)
		firstWord := ""
		if len(words) > 0 {
			firstWord = words[0]
		}

		if !inProc {
			if firstWord != "" && procStart(firstWord) {
				flushPending(lineNum)
				inProc = true
				currentStart = lineNum
				current = []string{line}
				depth = 0
				bodyStarted = false
			} else {
				if len(pending) == 0 {
					pendingStart = lineNum
				}
				pending = append(pending, line)
				if strings.TrimSpace(line) == "" && len(pending) > 1 {
					flushPending(lineNum)
				}
			}
		} else {
			current = append(current, line)
			for _, w := range words {
				switch strings.TrimRight(w, ".;,") {
				case "BEGIN":
					depth++
					bodyStarted = true
				case "END":
					if depth > 0 {
						depth--
					}
				}
			}
			if bodyStarted && depth == 0 {
				// Capture current before flushCurrent resets it.
				firstLine := ""
				if len(current) > 0 {
					firstLine = current[0]
				}
				text := strings.TrimSpace(strings.Join(current, "\n"))
				current = nil
				if text != "" {
					sym := extractSym(firstLine)
					var syms []string
					if sym != "" {
						syms = []string{sym}
					}
					chunks = append(chunks, EnrichedChunk{
						Content:   text,
						StartLine: currentStart,
						EndLine:   lineNum,
						ChunkType: "procedure",
						Symbols:   syms,
					})
				}
				inProc = false
				bodyStarted = false
			}
		}
	}

	// Flush anything remaining.
	if inProc && len(current) > 0 {
		flushCurrent(len(lines))
	}
	if len(pending) > 0 {
		flushPending(len(lines) + 1)
	}
	return chunks
}

// ─── PascalChunker ───────────────────────────────────────────────────────────

/** PascalChunker splits Pascal source into enriched chunks at PROCEDURE and
 * FUNCTION boundaries, tracking nested BEGIN/END blocks.
 *
 * Example:
 *   p := NewPascalChunker()
 *   chunks := p.Chunk("PROCEDURE Foo;\nBEGIN\nWRITELN('hi');\nEND;", "foo.pas")
 */
type PascalChunker struct{}

/** NewPascalChunker returns a PascalChunker.
 *
 * Returns:
 *   *PascalChunker — ready-to-use chunker.
 *
 * Example:
 *   p := NewPascalChunker()
 */
func NewPascalChunker() *PascalChunker { return &PascalChunker{} }

/** Language returns "pascal".
 *
 * Returns:
 *   string — "pascal".
 *
 * Example:
 *   p.Language() // "pascal"
 */
func (p *PascalChunker) Language() string { return "pascal" }

/** Chunk splits Pascal source into PROCEDURE/FUNCTION chunks and code sections.
 *
 * Parameters:
 *   content  (string) — Pascal source text.
 *   filePath (string) — source path; used for context only.
 *
 * Returns:
 *   []EnrichedChunk — chunks with location and symbol metadata.
 *
 * Example:
 *   chunks := p.Chunk(src, "prog.pas")
 */
func (p *PascalChunker) Chunk(content string, _ string) []EnrichedChunk {
	return chunkBeginEnd(content, isPascalProcStart, extractPascalSymbol)
}

func isPascalProcStart(firstWord string) bool {
	return firstWord == "PROCEDURE" || firstWord == "FUNCTION"
}

func extractPascalSymbol(header string) string {
	header = strings.TrimSpace(header)
	upper := strings.ToUpper(header)
	for _, kw := range []string{"PROCEDURE ", "FUNCTION "} {
		if strings.HasPrefix(upper, kw) {
			rest := strings.TrimSpace(header[len(kw):])
			end := 0
			for end < len(rest) && isIdentRune(rune(rest[end])) {
				end++
			}
			return rest[:end]
		}
	}
	return ""
}

// ─── OberonChunker ───────────────────────────────────────────────────────────

/** OberonChunker splits Oberon source into enriched chunks at PROCEDURE
 * boundaries.  MODULE headers and IMPORT/CONST/TYPE/VAR sections are emitted
 * as "code" chunks; each PROCEDURE is its own chunk.
 *
 * Note: Oberon supports nested comment delimiters (* (* *) *); this chunker
 * treats them as non-nested (known approximation).
 *
 * Example:
 *   o := NewOberonChunker()
 *   chunks := o.Chunk("MODULE Foo;\nPROCEDURE Bar;\nBEGIN END Bar;\nEND Foo.", "Foo.Mod")
 */
type OberonChunker struct{}

/** NewOberonChunker returns an OberonChunker.
 *
 * Returns:
 *   *OberonChunker — ready-to-use chunker.
 *
 * Example:
 *   o := NewOberonChunker()
 */
func NewOberonChunker() *OberonChunker { return &OberonChunker{} }

/** Language returns "oberon".
 *
 * Returns:
 *   string — "oberon".
 *
 * Example:
 *   o.Language() // "oberon"
 */
func (o *OberonChunker) Language() string { return "oberon" }

/** Chunk splits Oberon source into PROCEDURE chunks and code sections.
 *
 * Parameters:
 *   content  (string) — Oberon source text.
 *   filePath (string) — source path; used for context only.
 *
 * Returns:
 *   []EnrichedChunk — chunks with location and symbol metadata.
 *
 * Example:
 *   chunks := o.Chunk(src, "Foo.Mod")
 */
func (o *OberonChunker) Chunk(content string, _ string) []EnrichedChunk {
	return chunkBeginEnd(content, isOberonProcStart, extractOberonSymbol)
}

func isOberonProcStart(firstWord string) bool {
	return firstWord == "PROCEDURE"
}

func extractOberonSymbol(header string) string {
	header = strings.TrimSpace(header)
	upper := strings.ToUpper(header)
	if !strings.HasPrefix(upper, "PROCEDURE") {
		return ""
	}
	rest := strings.TrimSpace(header[len("PROCEDURE"):])
	end := 0
	for end < len(rest) && isIdentRune(rune(rest[end])) {
		end++
	}
	return rest[:end]
}

// ─── LispChunker ─────────────────────────────────────────────────────────────

/** LispChunker splits Lisp/Common Lisp/Emacs Lisp source into enriched chunks
 * at parenthesis-balanced top-level form boundaries.  Each defun, defmacro,
 * defvar, defclass, etc. becomes its own chunk.
 *
 * Example:
 *   l := NewLispChunker()
 *   chunks := l.Chunk("(defun hello () (print \"hi\"))", "greet.lisp")
 */
type LispChunker struct{}

/** NewLispChunker returns a LispChunker.
 *
 * Returns:
 *   *LispChunker — ready-to-use chunker.
 *
 * Example:
 *   l := NewLispChunker()
 */
func NewLispChunker() *LispChunker { return &LispChunker{} }

/** Language returns "lisp".
 *
 * Returns:
 *   string — "lisp".
 *
 * Example:
 *   l.Language() // "lisp"
 */
func (l *LispChunker) Language() string { return "lisp" }

/** Chunk splits Lisp source into enriched chunks at top-level form boundaries.
 *
 * Parameters:
 *   content  (string) — Lisp source text.
 *   filePath (string) — source path; used for context only.
 *
 * Returns:
 *   []EnrichedChunk — chunks with location and symbol metadata.
 *
 * Example:
 *   chunks := l.Chunk(src, "funcs.lisp")
 */
func (l *LispChunker) Chunk(content string, _ string) []EnrichedChunk {
	data := []byte(content)
	var chunks []EnrichedChunk

	depth := 0
	inStr := false
	inLineComment := false
	segStart := -1

	for i := 0; i < len(data); i++ {
		b := data[i]

		if inLineComment {
			if b == '\n' {
				inLineComment = false
			}
			continue
		}

		if inStr {
			if b == '\\' {
				i++
			} else if b == '"' {
				inStr = false
			}
			continue
		}

		switch b {
		case ';':
			inLineComment = true
		case '"':
			inStr = true
		case '(':
			if depth == 0 {
				segStart = i
			}
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
			if depth == 0 && segStart >= 0 {
				ch := makeChunk(data, segStart, i+1, "", nil, "")
				if ch.Content != "" {
					ch.ChunkType, ch.Symbols = classifyLisp(ch.Content)
					chunks = appendNonEmpty(chunks, ch)
				}
				segStart = -1
			}
		}
	}
	return chunks
}

// lispDefForms maps the first word of a top-level form to its chunk type.
var lispDefForms = map[string]string{
	"defun":      "function",
	"defmacro":   "function",
	"defmethod":  "function",
	"defgeneric": "function",
	"defvar":     "variable",
	"defparameter": "variable",
	"defconstant": "variable",
	"defclass":   "type",
	"defstruct":  "type",
	"deftype":    "type",
	"defpackage": "code",
}

// classifyLisp returns the chunk type and primary symbol for a Lisp form.
func classifyLisp(text string) (chunkType string, symbols []string) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if !strings.HasPrefix(lower, "(") {
		return "code", nil
	}
	inner := strings.TrimSpace(lower[1:])
	// Extract the first token (the form keyword).
	spaceIdx := strings.IndexAny(inner, " \t\n\r()")
	var formKw string
	if spaceIdx < 0 {
		formKw = inner
	} else {
		formKw = inner[:spaceIdx]
	}
	ct, ok := lispDefForms[formKw]
	if !ok {
		return "code", nil
	}
	// Extract the symbol name (second token).
	rest := strings.TrimSpace(inner[len(formKw):])
	nameEnd := strings.IndexAny(rest, " \t\n\r()")
	var name string
	if nameEnd < 0 {
		name = rest
	} else {
		name = rest[:nameEnd]
	}
	if name != "" {
		return ct, []string{name}
	}
	return ct, nil
}

// ─── BasicChunker ────────────────────────────────────────────────────────────

/** BasicChunker splits Basic (FreeBasic / QB64) source into enriched chunks
 * at SUB and FUNCTION boundaries.
 *
 * Example:
 *   b := NewBasicChunker()
 *   chunks := b.Chunk("SUB Hello()\n  PRINT \"Hi\"\nEND SUB", "prog.bas")
 */
type BasicChunker struct{}

/** NewBasicChunker returns a BasicChunker.
 *
 * Returns:
 *   *BasicChunker — ready-to-use chunker.
 *
 * Example:
 *   b := NewBasicChunker()
 */
func NewBasicChunker() *BasicChunker { return &BasicChunker{} }

/** Language returns "basic".
 *
 * Returns:
 *   string — "basic".
 *
 * Example:
 *   b.Language() // "basic"
 */
func (b *BasicChunker) Language() string { return "basic" }

/** Chunk splits Basic source into SUB/FUNCTION chunks and code sections.
 *
 * Parameters:
 *   content  (string) — Basic source text.
 *   filePath (string) — source path; used for context only.
 *
 * Returns:
 *   []EnrichedChunk — chunks with location and symbol metadata.
 *
 * Example:
 *   chunks := b.Chunk(src, "prog.bas")
 */
func (b *BasicChunker) Chunk(content string, _ string) []EnrichedChunk {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var chunks []EnrichedChunk

	var pending []string
	pendingStart := 1
	var current []string
	currentStart := 1
	inSub := false

	flushPending := func(beforeLine int) {
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		if text == "" {
			return
		}
		end := beforeLine - 1
		if end < pendingStart {
			end = pendingStart
		}
		chunks = append(chunks, EnrichedChunk{
			Content:   text,
			StartLine: pendingStart,
			EndLine:   end,
			ChunkType: "code",
		})
	}

	for i, line := range lines {
		lineNum := i + 1
		upper := strings.ToUpper(strings.TrimSpace(line))

		if !inSub {
			if strings.HasPrefix(upper, "SUB ") || strings.HasPrefix(upper, "FUNCTION ") {
				flushPending(lineNum)
				inSub = true
				currentStart = lineNum
				current = []string{line}
			} else {
				if len(pending) == 0 {
					pendingStart = lineNum
				}
				pending = append(pending, line)
				if strings.TrimSpace(line) == "" && len(pending) > 1 {
					flushPending(lineNum)
				}
			}
		} else {
			current = append(current, line)
			if upper == "END SUB" || upper == "END FUNCTION" ||
				strings.HasPrefix(upper, "END SUB ") ||
				strings.HasPrefix(upper, "END FUNCTION ") {
				text := strings.TrimSpace(strings.Join(current, "\n"))
				if text != "" {
					sym := extractBasicSymbol(current[0])
					var syms []string
					if sym != "" {
						syms = []string{sym}
					}
					chunks = append(chunks, EnrichedChunk{
						Content:   text,
						StartLine: currentStart,
						EndLine:   lineNum,
						ChunkType: "function",
						Symbols:   syms,
					})
				}
				current = nil
				inSub = false
			}
		}
	}

	// Flush remaining.
	if inSub && len(current) > 0 {
		text := strings.TrimSpace(strings.Join(current, "\n"))
		if text != "" {
			sym := extractBasicSymbol(current[0])
			var syms []string
			if sym != "" {
				syms = []string{sym}
			}
			chunks = append(chunks, EnrichedChunk{
				Content:   text,
				StartLine: currentStart,
				EndLine:   len(lines),
				ChunkType: "function",
				Symbols:   syms,
			})
		}
	}
	if len(pending) > 0 {
		flushPending(len(lines) + 1)
	}
	return chunks
}

func extractBasicSymbol(header string) string {
	upper := strings.ToUpper(strings.TrimSpace(header))
	for _, kw := range []string{"SUB ", "FUNCTION "} {
		if strings.HasPrefix(upper, kw) {
			rest := strings.TrimSpace(header[len(kw):])
			end := strings.IndexAny(rest, "( \t\r\n")
			if end < 0 {
				return rest
			}
			return rest[:end]
		}
	}
	return ""
}

// ─── Registry wiring ─────────────────────────────────────────────────────────

/** SetChunker registers a CodeChunker for an already-registered language ID.
 * Calling SetChunker for an unregistered ID is a no-op (the chunker is stored
 * but no language metadata exists to associate it with).
 *
 * Parameters:
 *   id (string)      — language identifier, e.g. "c".
 *   c  (CodeChunker) — chunker to register.
 *
 * Example:
 *   r.SetChunker("c", NewCChunker("c"))
 */
func (r *LanguageRegistry) SetChunker(id string, c CodeChunker) {
	r.chunkers[id] = c
}

// initChunkers wires Phase-3 code-aware chunkers into r for the six languages
// that have HasChunking=true: C, C++, Pascal, Oberon, Lisp, Basic.
func initChunkers(r *LanguageRegistry) {
	r.SetChunker("c", NewCChunker("c"))
	r.SetChunker("cpp", NewCChunker("cpp"))
	r.SetChunker("pascal", NewPascalChunker())
	r.SetChunker("oberon", NewOberonChunker())
	r.SetChunker("lisp", NewLispChunker())
	r.SetChunker("basic", NewBasicChunker())
}
