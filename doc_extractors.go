package harvey

import (
	"strings"
)

// ─── Shared helper ────────────────────────────────────────────────────────────

// docsToSymbolMap converts a slice of DocumentationBlocks to a symbol → text
// map.  When multiple blocks share the same symbol, the last one wins.
func docsToSymbolMap(blocks []DocumentationBlock) map[string]string {
	m := make(map[string]string, len(blocks))
	for _, b := range blocks {
		if b.Symbol != "" {
			m[b.Symbol] = b.Content
		}
	}
	return m
}

// ─── C / C++ DocExtractor ─────────────────────────────────────────────────────

/** CDocExtractor extracts documentation blocks from C or C++ source code.
 * It recognises line comments (//), block comments (/* * /), and
 * Doxygen-style doc-comments (/** * / and /*! * /).  Comments immediately
 * preceding (no intervening blank line) a code line are associated with the
 * symbol defined on that line.
 *
 * Example:
 *   e := NewCDocExtractor("c")
 *   docs := e.ExtractDocs("// foo docs\nvoid foo(){}\n")
 */
type CDocExtractor struct{ langID string }

/** NewCDocExtractor returns a CDocExtractor for the given language ID.
 *
 * Parameters:
 *   langID (string) — "c" or "cpp".
 *
 * Returns:
 *   *CDocExtractor — ready-to-use extractor.
 *
 * Example:
 *   e := NewCDocExtractor("c")
 */
func NewCDocExtractor(langID string) *CDocExtractor { return &CDocExtractor{langID: langID} }

/** Language returns the language ID this extractor handles.
 *
 * Returns:
 *   string — "c" or "cpp".
 *
 * Example:
 *   e.Language() // "c"
 */
func (e *CDocExtractor) Language() string { return e.langID }

/** ExtractDocs returns all DocumentationBlocks found in C/C++ source.
 *
 * Parameters:
 *   content (string) — C or C++ source text.
 *
 * Returns:
 *   []DocumentationBlock — blocks with type, line range, and symbol association.
 *
 * Example:
 *   docs := e.ExtractDocs("/** Foo does X. * /\nvoid foo(){}\n")
 */
func (e *CDocExtractor) ExtractDocs(content string) []DocumentationBlock {
	return extractCDocs(content)
}

/** ExtractSymbols returns a map of symbol name → documentation text for C/C++.
 *
 * Parameters:
 *   content (string) — C or C++ source text.
 *
 * Returns:
 *   map[string]string — symbol → doc text; only symbols with comments are included.
 *
 * Example:
 *   syms := e.ExtractSymbols("// Returns x.\nint getX(){return x;}\n")
 *   // syms["getX"] == "Returns x."
 */
func (e *CDocExtractor) ExtractSymbols(content string) map[string]string {
	return docsToSymbolMap(extractCDocs(content))
}

// extractCDocs scans C/C++ source for documentation blocks.
// Only comments that begin at the start of a (trimmed) line are recognised.
// A comment block with no following blank line is associated with the symbol
// on the next non-comment code line.
func extractCDocs(content string) []DocumentationBlock {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var result []DocumentationBlock

	inBlock := false
	var pending []string
	pendingStart, pendingEnd := 0, 0
	pendingType := ""

	addDoc := func(sym string) {
		if len(pending) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		if text == "" {
			return
		}
		result = append(result, DocumentationBlock{
			Content:   text,
			StartLine: pendingStart,
			EndLine:   pendingEnd,
			Symbol:    sym,
			Type:      pendingType,
		})
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if inBlock {
			if closeIdx := strings.Index(trimmed, "*/"); closeIdx >= 0 {
				before := strings.TrimSpace(trimmed[:closeIdx])
				before = strings.TrimSpace(strings.TrimPrefix(before, "*"))
				if before != "" {
					pending = append(pending, before)
				}
				pendingEnd = lineNum
				inBlock = false
			} else {
				stripped := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "* "), "*"))
				if stripped != "" {
					pending = append(pending, stripped)
				}
			}
			continue
		}

		// Doxygen doc-comment: /** ... */ or /*! ... */
		if strings.HasPrefix(trimmed, "/**") || strings.HasPrefix(trimmed, "/*!") {
			addDoc("")
			pendingType = "docstring"
			pendingStart = lineNum
			const open = 3
			if closeIdx := strings.Index(trimmed[open:], "*/"); closeIdx >= 0 {
				inner := strings.TrimSpace(trimmed[open : open+closeIdx])
				inner = strings.TrimSpace(strings.TrimPrefix(inner, "*"))
				if inner != "" {
					pending = []string{inner}
				}
				pendingEnd = lineNum
			} else {
				rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[open:]), "*"))
				if rest != "" {
					pending = []string{rest}
				}
				inBlock = true
			}
			continue
		}

		// Block comment: /* ... */
		if strings.HasPrefix(trimmed, "/*") {
			addDoc("")
			pendingType = "block_comment"
			pendingStart = lineNum
			rest := trimmed[2:]
			if closeIdx := strings.Index(rest, "*/"); closeIdx >= 0 {
				inner := strings.TrimSpace(rest[:closeIdx])
				inner = strings.TrimSpace(strings.TrimPrefix(inner, "*"))
				if inner != "" {
					pending = []string{inner}
				}
				pendingEnd = lineNum
			} else {
				rest2 := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "*"))
				if rest2 != "" {
					pending = []string{rest2}
				}
				inBlock = true
			}
			continue
		}

		// Line comment: //
		if strings.HasPrefix(trimmed, "//") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			if pendingType == "line_comment" && len(pending) > 0 {
				pending = append(pending, text)
				pendingEnd = lineNum
			} else {
				addDoc("")
				pendingType = "line_comment"
				pendingStart = lineNum
				pendingEnd = lineNum
				pending = []string{text}
			}
			continue
		}

		// Blank line: flush as free-standing (no symbol association).
		if trimmed == "" {
			addDoc("")
			continue
		}

		// Code line: associate pending comments with the symbol on this line.
		if len(pending) > 0 {
			_, syms := classifyC(line)
			sym := ""
			if len(syms) > 0 {
				sym = syms[0]
			}
			addDoc(sym)
		}
	}

	addDoc("")
	return result
}

// ─── Pascal DocExtractor ─────────────────────────────────────────────────────

/** PascalDocExtractor extracts documentation from Pascal source code.
 * It recognises line comments (//), brace block comments ({ }),
 * and parenthesis-star block comments ((* *)).  {$...} compiler directives
 * are ignored.
 *
 * Example:
 *   e := NewPascalDocExtractor()
 *   docs := e.ExtractDocs("{ Adds a and b. }\nFUNCTION Add(a,b:Integer):Integer;\n")
 */
type PascalDocExtractor struct{}

/** NewPascalDocExtractor returns a PascalDocExtractor.
 *
 * Returns:
 *   *PascalDocExtractor — ready-to-use extractor.
 *
 * Example:
 *   e := NewPascalDocExtractor()
 */
func NewPascalDocExtractor() *PascalDocExtractor { return &PascalDocExtractor{} }

/** Language returns "pascal".
 *
 * Returns:
 *   string — "pascal".
 *
 * Example:
 *   e.Language() // "pascal"
 */
func (e *PascalDocExtractor) Language() string { return "pascal" }

/** ExtractDocs returns all DocumentationBlocks found in Pascal source.
 *
 * Parameters:
 *   content (string) — Pascal source text.
 *
 * Returns:
 *   []DocumentationBlock — blocks with type, line range, and symbol association.
 *
 * Example:
 *   docs := e.ExtractDocs("{ Sort the list. }\nPROCEDURE Sort(var L:TList);\n")
 */
func (e *PascalDocExtractor) ExtractDocs(content string) []DocumentationBlock {
	return extractPascalDocs(content)
}

/** ExtractSymbols returns a map of symbol name → documentation text for Pascal.
 *
 * Parameters:
 *   content (string) — Pascal source text.
 *
 * Returns:
 *   map[string]string — symbol → doc text.
 *
 * Example:
 *   syms := e.ExtractSymbols("(* Sort items. *)\nPROCEDURE Sort;\n")
 *   // syms["Sort"] == "Sort items."
 */
func (e *PascalDocExtractor) ExtractSymbols(content string) map[string]string {
	return docsToSymbolMap(extractPascalDocs(content))
}

func extractPascalDocs(content string) []DocumentationBlock {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var result []DocumentationBlock

	type blockKind int
	const (
		bkNone  blockKind = iota
		bkBrace           // inside { ... }
		bkParen           // inside (* ... *)
	)

	bk := bkNone
	var pending []string
	pendingStart, pendingEnd := 0, 0
	pendingType := ""

	addDoc := func(sym string) {
		if len(pending) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		if text == "" {
			return
		}
		result = append(result, DocumentationBlock{
			Content:   text,
			StartLine: pendingStart,
			EndLine:   pendingEnd,
			Symbol:    sym,
			Type:      pendingType,
		})
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		switch bk {
		case bkBrace:
			if closeIdx := strings.Index(trimmed, "}"); closeIdx >= 0 {
				before := strings.TrimSpace(trimmed[:closeIdx])
				if before != "" {
					pending = append(pending, before)
				}
				pendingEnd = lineNum
				bk = bkNone
			} else if trimmed != "" {
				pending = append(pending, trimmed)
			}
			continue
		case bkParen:
			if closeIdx := strings.Index(trimmed, "*)"); closeIdx >= 0 {
				before := strings.TrimSpace(trimmed[:closeIdx])
				if before != "" {
					pending = append(pending, before)
				}
				pendingEnd = lineNum
				bk = bkNone
			} else if trimmed != "" {
				pending = append(pending, trimmed)
			}
			continue
		}

		// (* ... *) block comment.
		if strings.HasPrefix(trimmed, "(*") {
			addDoc("")
			pendingType = "block_comment"
			pendingStart = lineNum
			if closeIdx := strings.Index(trimmed[2:], "*)"); closeIdx >= 0 {
				inner := strings.TrimSpace(trimmed[2 : 2+closeIdx])
				if inner != "" {
					pending = []string{inner}
				}
				pendingEnd = lineNum
			} else {
				rest := strings.TrimSpace(trimmed[2:])
				if rest != "" {
					pending = []string{rest}
				}
				bk = bkParen
			}
			continue
		}

		// { ... } brace comment; skip {$...} compiler directives.
		if strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "{$") {
			addDoc("")
			pendingType = "block_comment"
			pendingStart = lineNum
			if closeIdx := strings.Index(trimmed[1:], "}"); closeIdx >= 0 {
				inner := strings.TrimSpace(trimmed[1 : 1+closeIdx])
				if inner != "" {
					pending = []string{inner}
				}
				pendingEnd = lineNum
			} else {
				rest := strings.TrimSpace(trimmed[1:])
				if rest != "" {
					pending = []string{rest}
				}
				bk = bkBrace
			}
			continue
		}

		// // line comment.
		if strings.HasPrefix(trimmed, "//") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			if pendingType == "line_comment" && len(pending) > 0 {
				pending = append(pending, text)
				pendingEnd = lineNum
			} else {
				addDoc("")
				pendingType = "line_comment"
				pendingStart = lineNum
				pendingEnd = lineNum
				pending = []string{text}
			}
			continue
		}

		if trimmed == "" {
			addDoc("")
			continue
		}

		// Code line: associate pending with the symbol on this line.
		if len(pending) > 0 {
			sym := extractPascalSymbol(trimmed)
			addDoc(sym)
		}
	}

	addDoc("")
	return result
}

// ─── Oberon DocExtractor ─────────────────────────────────────────────────────

/** OberonDocExtractor extracts documentation from Oberon source code.
 * It recognises only (* *) block comments; Oberon has no line-comment syntax.
 * Note: nested (* (* *) *) comments are treated as non-nested (same
 * approximation as OberonChunker).
 *
 * Example:
 *   e := NewOberonDocExtractor()
 *   docs := e.ExtractDocs("(* Adds a to b. *)\nPROCEDURE Add*(a,b:INTEGER):INTEGER;\n")
 */
type OberonDocExtractor struct{}

/** NewOberonDocExtractor returns an OberonDocExtractor.
 *
 * Returns:
 *   *OberonDocExtractor — ready-to-use extractor.
 *
 * Example:
 *   e := NewOberonDocExtractor()
 */
func NewOberonDocExtractor() *OberonDocExtractor { return &OberonDocExtractor{} }

/** Language returns "oberon".
 *
 * Returns:
 *   string — "oberon".
 *
 * Example:
 *   e.Language() // "oberon"
 */
func (e *OberonDocExtractor) Language() string { return "oberon" }

/** ExtractDocs returns all DocumentationBlocks found in Oberon source.
 *
 * Parameters:
 *   content (string) — Oberon source text.
 *
 * Returns:
 *   []DocumentationBlock — blocks with line range and symbol association.
 *
 * Example:
 *   docs := e.ExtractDocs("(* Greet the user. *)\nPROCEDURE Greet*;\n")
 */
func (e *OberonDocExtractor) ExtractDocs(content string) []DocumentationBlock {
	return extractOberonDocs(content)
}

/** ExtractSymbols returns a map of symbol name → documentation text for Oberon.
 *
 * Parameters:
 *   content (string) — Oberon source text.
 *
 * Returns:
 *   map[string]string — symbol → doc text.
 *
 * Example:
 *   syms := e.ExtractSymbols("(* Greet user. *)\nPROCEDURE Greet*;\n")
 *   // syms["Greet"] == "Greet user."
 */
func (e *OberonDocExtractor) ExtractSymbols(content string) map[string]string {
	return docsToSymbolMap(extractOberonDocs(content))
}

func extractOberonDocs(content string) []DocumentationBlock {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var result []DocumentationBlock

	inBlock := false
	var pending []string
	pendingStart, pendingEnd := 0, 0

	addDoc := func(sym string) {
		if len(pending) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		if text == "" {
			return
		}
		result = append(result, DocumentationBlock{
			Content:   text,
			StartLine: pendingStart,
			EndLine:   pendingEnd,
			Symbol:    sym,
			Type:      "block_comment",
		})
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if inBlock {
			if closeIdx := strings.Index(trimmed, "*)"); closeIdx >= 0 {
				before := strings.TrimSpace(trimmed[:closeIdx])
				if before != "" {
					pending = append(pending, before)
				}
				pendingEnd = lineNum
				inBlock = false
			} else if trimmed != "" {
				pending = append(pending, trimmed)
			}
			continue
		}

		if strings.HasPrefix(trimmed, "(*") {
			addDoc("")
			pendingStart = lineNum
			if closeIdx := strings.Index(trimmed[2:], "*)"); closeIdx >= 0 {
				inner := strings.TrimSpace(trimmed[2 : 2+closeIdx])
				if inner != "" {
					pending = []string{inner}
				}
				pendingEnd = lineNum
			} else {
				rest := strings.TrimSpace(trimmed[2:])
				if rest != "" {
					pending = []string{rest}
				}
				inBlock = true
			}
			continue
		}

		if trimmed == "" {
			addDoc("")
			continue
		}

		if len(pending) > 0 {
			sym := extractOberonSymbol(trimmed)
			addDoc(sym)
		}
	}

	addDoc("")
	return result
}

// ─── Lisp DocExtractor ───────────────────────────────────────────────────────

/** LispDocExtractor extracts documentation from Lisp/Common Lisp/Emacs Lisp source.
 * It recognises semicolon line comments (;, ;;, ;;;), block comments (#| |#),
 * and docstrings embedded in DEFUN/DEFMACRO/DEFMETHOD forms.
 *
 * Example:
 *   e := NewLispDocExtractor()
 *   docs := e.ExtractDocs(";; Add two numbers.\n(defun add (a b) \"Add a and b.\" (+ a b))\n")
 */
type LispDocExtractor struct{}

/** NewLispDocExtractor returns a LispDocExtractor.
 *
 * Returns:
 *   *LispDocExtractor — ready-to-use extractor.
 *
 * Example:
 *   l := NewLispDocExtractor()
 */
func NewLispDocExtractor() *LispDocExtractor { return &LispDocExtractor{} }

/** Language returns "lisp".
 *
 * Returns:
 *   string — "lisp".
 *
 * Example:
 *   e.Language() // "lisp"
 */
func (e *LispDocExtractor) Language() string { return "lisp" }

/** ExtractDocs returns all DocumentationBlocks found in Lisp source.
 *
 * Parameters:
 *   content (string) — Lisp source text.
 *
 * Returns:
 *   []DocumentationBlock — blocks including semicolon comments, #| |# blocks,
 *                          and DEFUN docstrings.
 *
 * Example:
 *   docs := e.ExtractDocs(";; Returns x.\n(defun get-x () x)\n")
 */
func (e *LispDocExtractor) ExtractDocs(content string) []DocumentationBlock {
	return extractLispDocs(content)
}

/** ExtractSymbols returns a map of symbol name → documentation text for Lisp.
 *
 * Parameters:
 *   content (string) — Lisp source text.
 *
 * Returns:
 *   map[string]string — symbol → doc text.
 *
 * Example:
 *   syms := e.ExtractSymbols(";; Returns x.\n(defun get-x () x)\n")
 *   // syms["get-x"] == "Returns x."
 */
func (e *LispDocExtractor) ExtractSymbols(content string) map[string]string {
	return docsToSymbolMap(extractLispDocs(content))
}

func extractLispDocs(content string) []DocumentationBlock {
	return append(extractLispComments(content), extractLispDocstrings(content)...)
}

func extractLispComments(content string) []DocumentationBlock {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var result []DocumentationBlock

	inBlock := false
	var pending []string
	pendingStart, pendingEnd := 0, 0
	pendingType := ""

	addDoc := func(sym string) {
		if len(pending) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		if text == "" {
			return
		}
		result = append(result, DocumentationBlock{
			Content:   text,
			StartLine: pendingStart,
			EndLine:   pendingEnd,
			Symbol:    sym,
			Type:      pendingType,
		})
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if inBlock {
			if closeIdx := strings.Index(trimmed, "|#"); closeIdx >= 0 {
				before := strings.TrimSpace(trimmed[:closeIdx])
				if before != "" {
					pending = append(pending, before)
				}
				pendingEnd = lineNum
				inBlock = false
			} else if trimmed != "" {
				pending = append(pending, trimmed)
			}
			continue
		}

		// #| ... |# block comment.
		if strings.HasPrefix(trimmed, "#|") {
			addDoc("")
			pendingType = "block_comment"
			pendingStart = lineNum
			if closeIdx := strings.Index(trimmed[2:], "|#"); closeIdx >= 0 {
				inner := strings.TrimSpace(trimmed[2 : 2+closeIdx])
				if inner != "" {
					pending = []string{inner}
				}
				pendingEnd = lineNum
			} else {
				rest := strings.TrimSpace(trimmed[2:])
				if rest != "" {
					pending = []string{rest}
				}
				inBlock = true
			}
			continue
		}

		// ; line comment (one or more semicolons).
		if strings.HasPrefix(trimmed, ";") {
			text := strings.TrimSpace(strings.TrimLeft(trimmed, ";"))
			if pendingType == "line_comment" && len(pending) > 0 {
				pending = append(pending, text)
				pendingEnd = lineNum
			} else {
				addDoc("")
				pendingType = "line_comment"
				pendingStart = lineNum
				pendingEnd = lineNum
				pending = []string{text}
			}
			continue
		}

		if trimmed == "" {
			addDoc("")
			continue
		}

		// Code line: associate pending with symbol if this is a def form.
		if len(pending) > 0 {
			lower := strings.ToLower(trimmed)
			sym := ""
			if strings.HasPrefix(lower, "(def") {
				_, syms := classifyLisp(line)
				if len(syms) > 0 {
					sym = syms[0]
				}
			}
			addDoc(sym)
		}
	}

	addDoc("")
	return result
}

// extractLispDocstrings finds DEFUN/DEFMACRO/DEFMETHOD docstrings.
// A docstring is the first string literal (starting with ") on a non-blank,
// non-comment line immediately after the defining form header.  Only the
// first such line within 8 lines of the def form is examined.
func extractLispDocstrings(content string) []DocumentationBlock {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var result []DocumentationBlock

	defForms := []string{"(defun ", "(defmacro ", "(defmethod "}

	for i, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		var sym string
		for _, kw := range defForms {
			if strings.HasPrefix(lower, kw) {
				_, syms := classifyLisp(line)
				if len(syms) > 0 {
					sym = syms[0]
				}
				break
			}
		}
		if sym == "" {
			continue
		}

		// Scan subsequent non-blank, non-comment lines for a docstring.
		for j := i + 1; j < len(lines) && j <= i+8; j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, ";") {
				continue
			}
			if strings.HasPrefix(next, `"`) {
				ds := lispStringContent(next)
				if ds != "" {
					result = append(result, DocumentationBlock{
						Content:   ds,
						StartLine: j + 1,
						EndLine:   j + 1,
						Symbol:    sym,
						Type:      "docstring",
					})
				}
			}
			break
		}
	}

	return result
}

// lispStringContent extracts the text of a Lisp string literal at the start
// of line.  Returns "" when the string is malformed, empty, or not present.
func lispStringContent(line string) string {
	if !strings.HasPrefix(line, `"`) {
		return ""
	}
	var sb strings.Builder
	for i := 1; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			if i < len(line) {
				sb.WriteByte(line[i])
			}
			continue
		}
		if line[i] == '"' {
			return sb.String()
		}
		sb.WriteByte(line[i])
	}
	return "" // unclosed string
}

// ─── Basic DocExtractor ──────────────────────────────────────────────────────

/** BasicDocExtractor extracts documentation from FreeBasic / QB64 source code.
 * It recognises REM statements (case-insensitive) and single-quote (') comments.
 * Consecutive comment lines are grouped into one DocumentationBlock.
 *
 * Example:
 *   e := NewBasicDocExtractor()
 *   docs := e.ExtractDocs("' Prints a greeting.\nSUB Hello()\n  PRINT \"Hi\"\nEND SUB\n")
 */
type BasicDocExtractor struct{}

/** NewBasicDocExtractor returns a BasicDocExtractor.
 *
 * Returns:
 *   *BasicDocExtractor — ready-to-use extractor.
 *
 * Example:
 *   e := NewBasicDocExtractor()
 */
func NewBasicDocExtractor() *BasicDocExtractor { return &BasicDocExtractor{} }

/** Language returns "basic".
 *
 * Returns:
 *   string — "basic".
 *
 * Example:
 *   e.Language() // "basic"
 */
func (e *BasicDocExtractor) Language() string { return "basic" }

/** ExtractDocs returns all DocumentationBlocks found in Basic source.
 *
 * Parameters:
 *   content (string) — Basic source text.
 *
 * Returns:
 *   []DocumentationBlock — blocks with line range and symbol association.
 *
 * Example:
 *   docs := e.ExtractDocs("' Adds a and b.\nFUNCTION Add(a As Integer, b As Integer)\n")
 */
func (e *BasicDocExtractor) ExtractDocs(content string) []DocumentationBlock {
	return extractBasicDocs(content)
}

/** ExtractSymbols returns a map of symbol name → documentation text for Basic.
 *
 * Parameters:
 *   content (string) — Basic source text.
 *
 * Returns:
 *   map[string]string — symbol → doc text.
 *
 * Example:
 *   syms := e.ExtractSymbols("REM Adds two numbers.\nFUNCTION Add(a, b)\n")
 *   // syms["Add"] == "Adds two numbers."
 */
func (e *BasicDocExtractor) ExtractSymbols(content string) map[string]string {
	return docsToSymbolMap(extractBasicDocs(content))
}

func extractBasicDocs(content string) []DocumentationBlock {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var result []DocumentationBlock

	var pending []string
	pendingStart, pendingEnd := 0, 0

	addDoc := func(sym string) {
		if len(pending) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		if text == "" {
			return
		}
		result = append(result, DocumentationBlock{
			Content:   text,
			StartLine: pendingStart,
			EndLine:   pendingEnd,
			Symbol:    sym,
			Type:      "line_comment",
		})
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		var commentText string
		isComment := false

		if upper == "REM" || strings.HasPrefix(upper, "REM ") || strings.HasPrefix(upper, "REM\t") {
			isComment = true
			commentText = strings.TrimSpace(trimmed[3:])
		} else if strings.HasPrefix(trimmed, "'") {
			isComment = true
			commentText = strings.TrimSpace(trimmed[1:])
		}

		if isComment {
			if len(pending) == 0 {
				pendingStart = lineNum
			}
			pending = append(pending, commentText)
			pendingEnd = lineNum
			continue
		}

		if trimmed == "" {
			addDoc("")
			continue
		}

		// Code line: associate pending with symbol on this line.
		if len(pending) > 0 {
			sym := extractBasicSymbol(line)
			addDoc(sym)
		}
	}

	addDoc("")
	return result
}

// ─── Registry wiring ─────────────────────────────────────────────────────────

/** SetExtractor registers a DocExtractor for an already-registered language ID.
 * Calling SetExtractor for an unregistered ID is a no-op (the extractor is
 * stored but no language metadata exists to associate it with).
 *
 * Parameters:
 *   id (string)      — language identifier, e.g. "c".
 *   e  (DocExtractor)— extractor to register.
 *
 * Example:
 *   r.SetExtractor("c", NewCDocExtractor("c"))
 */
func (r *LanguageRegistry) SetExtractor(id string, e DocExtractor) {
	r.extractors[id] = e
}

// initExtractors wires Phase-4 documentation extractors into r for the six
// languages that have HasExtraction=true: C, C++, Pascal, Oberon, Lisp, Basic.
func initExtractors(r *LanguageRegistry) {
	r.SetExtractor("c", NewCDocExtractor("c"))
	r.SetExtractor("cpp", NewCDocExtractor("cpp"))
	r.SetExtractor("pascal", NewPascalDocExtractor())
	r.SetExtractor("oberon", NewOberonDocExtractor())
	r.SetExtractor("lisp", NewLispDocExtractor())
	r.SetExtractor("basic", NewBasicDocExtractor())
}
