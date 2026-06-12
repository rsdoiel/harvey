package harvey

import (
	"path/filepath"
	"strings"
)

// ─── Content-based detection (registry-level) ─────────────────────────────────

// ─── Formatter mode ───────────────────────────────────────────────────────────

/** FormatterMode describes how a CodeFormatter exchanges content with Harvey.
 *
 * Two modes are supported:
 *   PipeFormatter — content is piped to the formatter's stdin; formatted
 *                   output is read from its stdout.  Works with safe mode on.
 *   FileFormatter — Harvey writes the file first, then invokes the formatter
 *                   on the path so it can rewrite the file in place.
 *                   Requires safe mode off; the executable is validated
 *                   against allowed_commands before invocation.
 *
 * Example:
 *   if f.Mode() == PipeFormatter { ... }
 */
type FormatterMode int

const (
	// PipeFormatter reads from stdin and writes to stdout.
	PipeFormatter FormatterMode = iota
	// FileFormatter reads and rewrites the file at the given path.
	FileFormatter
)

// ─── Language metadata ────────────────────────────────────────────────────────

/** LanguageInfo holds static metadata about a programming or markup language.
 *
 * Fields:
 *   ID              (string)   — canonical lowercase identifier, e.g. "go", "c".
 *   Name            (string)   — display name, e.g. "Go", "C".
 *   Extensions      ([]string) — file extensions including the dot, e.g. [".go"].
 *                                Extensions are stored as-is (case-preserving);
 *                                see LanguageRegistry.HasExtension for matching rules.
 *   CommentMarkers  ([]string) — line and block comment delimiters.
 *   BlockStart      (string)   — token that opens a code block, e.g. "{" or "begin".
 *   BlockEnd        (string)   — token that closes a code block, e.g. "}" or "end".
 *   Shebang         (string)   — typical shebang line when used as a script, or "".
 *   HasChunking     (bool)     — code-aware chunker planned/available.
 *   HasExtraction   (bool)     — documentation extractor planned/available.
 *   HasFormatting   (bool)     — formatter planned/available.
 *   HasHighlighting (bool)     — syntax highlighter planned/available.
 *
 * Example:
 *   info := LanguageInfo{ID: "go", Name: "Go", Extensions: []string{".go"}}
 */
type LanguageInfo struct {
	ID             string
	Name           string
	Extensions     []string
	CommentMarkers []string
	BlockStart     string
	BlockEnd       string
	Shebang        string

	HasChunking     bool
	HasExtraction   bool
	HasFormatting   bool
	HasHighlighting bool
}

// ─── Chunk and documentation types ───────────────────────────────────────────

/** EnrichedChunk is a fragment of source code or document text with location
 * and semantic metadata.  StartLine/StartCol and EndLine/EndCol give a precise
 * re-location address so a reader can jump directly to the chunk in the source
 * file. Columns are 0-indexed byte offsets from the start of the line.
 *
 * Fields:
 *   Content     (string)              — the text of this chunk.
 *   StartLine   (int)                 — first line of the chunk, 1-indexed.
 *   StartCol    (int)                 — byte offset from the start of StartLine, 0-indexed.
 *   EndLine     (int)                 — last line of the chunk, 1-indexed.
 *   EndCol      (int)                 — byte offset from the start of EndLine, 0-indexed.
 *   ChunkType   (string)              — "function", "class", "procedure", "comment", "code",
 *                                        or for scholarly documents: "abstract", "introduction",
 *                                        "methods", "results", "discussion", "conclusion",
 *                                        "references", "body".
 *   Symbols     ([]string)            — identifiers defined in this chunk.
 *   Docs        (string)              — extracted comment/docstring text associated with the chunk.
 *   Identifiers (map[string][]string) — the source document's own scholarly identifiers
 *                                        (DOI, ORCID, ROR, FundRef, etc.), keyed by
 *                                        IdentifierType string; shared across every chunk
 *                                        from the same document. Empty/nil for non-scholarly chunks.
 *   Citations   ([]string)            — scholarly identifiers found in this chunk's text that
 *                                        point to OTHER works (e.g. DOIs cited in a references
 *                                        section). Empty/nil for non-scholarly chunks.
 *
 * Example:
 *   chunk := EnrichedChunk{Content: "func Foo() {}", StartLine: 10, StartCol: 0,
 *       EndLine: 10, EndCol: 14, ChunkType: "function", Symbols: []string{"Foo"}}
 */
type EnrichedChunk struct {
	Content     string
	StartLine   int
	StartCol    int
	EndLine     int
	EndCol      int
	ChunkType   string
	Symbols     []string
	Docs        string
	Identifiers map[string][]string
	Citations   []string
}

/** DocumentationBlock represents a comment or docstring extracted from source.
 *
 * Fields:
 *   Content   (string) — the documentation text (comment markers stripped).
 *   StartLine (int)    — first line of the block, 1-indexed.
 *   EndLine   (int)    — last line of the block, 1-indexed.
 *   Symbol    (string) — name of the associated symbol, or "" if free-standing.
 *   Type      (string) — "line_comment", "block_comment", or "docstring".
 *
 * Example:
 *   doc := DocumentationBlock{Content: "Foo does X.", StartLine: 9, EndLine: 9,
 *       Symbol: "Foo", Type: "line_comment"}
 */
type DocumentationBlock struct {
	Content   string
	StartLine int
	EndLine   int
	Symbol    string
	Type      string
}

/** FormatIssue describes a formatting problem found by a CodeFormatter.
 *
 * Fields:
 *   Line     (int)    — 1-indexed line number.
 *   Column   (int)    — 0-indexed byte offset from line start.
 *   Message  (string) — human-readable description.
 *   Severity (string) — "error", "warning", or "info".
 *
 * Example:
 *   issue := FormatIssue{Line: 3, Column: 0, Message: "missing newline", Severity: "warning"}
 */
type FormatIssue struct {
	Line     int
	Column   int
	Message  string
	Severity string
}

/** HighlightStyle defines ANSI terminal colors for syntax elements.
 * Each field holds an ANSI escape prefix, e.g. "\033[34m" for blue.
 * Empty string means "no color".
 *
 * Fields:
 *   Keyword  — color for language keywords.
 *   String   — color for string literals.
 *   Comment  — color for comments.
 *   Number   — color for numeric literals.
 *   Operator — color for operators and punctuation.
 *   Function — color for function/procedure names.
 *   Type     — color for type names.
 *   Builtin  — color for built-in identifiers.
 *   Reset    — ANSI reset sequence; normally "\033[0m".
 *
 * Example:
 *   style := HighlightStyle{Keyword: "\033[34m", Reset: "\033[0m"}
 */
type HighlightStyle struct {
	Keyword  string
	String   string
	Comment  string
	Number   string
	Operator string
	Function string
	Type     string
	Builtin  string
	Reset    string
}

// ─── Interfaces ───────────────────────────────────────────────────────────────

/** LanguageDetector identifies the programming language of a file.
 *
 * Example:
 *   lang, conf := d.Detect("main.go", data)
 */
type LanguageDetector interface {
	// Detect returns the language ID and a confidence score in [0, 1].
	// A score below 0.5 should be treated as a non-match.
	Detect(filePath string, content []byte) (language string, confidence float64)

	// DetectFromExtension returns the language ID for a file extension (with dot).
	// ok is false when the extension is not recognised.
	DetectFromExtension(ext string) (language string, ok bool)
}

/** CodeChunker splits source code into semantically meaningful chunks for
 * RAG ingestion.  Each chunk preserves code boundaries (functions, procedures,
 * etc.) and carries source-location metadata in EnrichedChunk.
 *
 * Example:
 *   chunks := chunker.Chunk(src, "main.go")
 */
type CodeChunker interface {
	// Chunk splits content into enriched units.  filePath is used for context only.
	Chunk(content string, filePath string) []EnrichedChunk

	// Language returns the language ID this chunker handles.
	Language() string
}

/** DocExtractor pulls documentation blocks out of source code and associates
 * them with the symbols they document.
 *
 * Example:
 *   docs := extractor.ExtractDocs(src)
 */
type DocExtractor interface {
	// ExtractDocs returns all documentation blocks found in content.
	ExtractDocs(content string) []DocumentationBlock

	// ExtractSymbols returns a map of symbol name → documentation text.
	ExtractSymbols(content string) map[string]string

	// Language returns the language ID this extractor handles.
	Language() string
}

/** CodeFormatter formats source code according to language conventions.
 *
 * Two modes are supported; see FormatterMode for details.
 *
 * Example:
 *   formatted, err := f.Format(src, "main.go")
 */
type CodeFormatter interface {
	// Format returns formatted source code.
	// PipeFormatter: content → formatted string (file not touched).
	// FileFormatter: Harvey writes the file first; Format is called after and
	//                returns the re-read file content.
	Format(content string, filePath string) (string, error)

	// Check returns true when content is already properly formatted,
	// plus a list of issues if it is not.
	Check(content string, filePath string) (bool, []FormatIssue)

	// Language returns the language ID this formatter handles.
	Language() string

	// Mode returns PipeFormatter or FileFormatter.
	Mode() FormatterMode
}

/** SyntaxHighlighter adds ANSI colour codes to source code for terminal display.
 * Implementations must strip existing ANSI sequences from the input before
 * adding their own, to avoid corrupting the terminal.
 *
 * Example:
 *   coloured := h.Highlight(src, "go")
 */
type SyntaxHighlighter interface {
	// Highlight returns ANSI-coloured content for terminal display.
	Highlight(content string, lang string) string

	// GetStyle returns the colour configuration for a language.
	GetStyle(lang string) *HighlightStyle
}

// ─── Registry ─────────────────────────────────────────────────────────────────

/** LanguageRegistry is a central catalogue that maps language identifiers to
 * their metadata and optional handlers (chunker, extractor, formatter,
 * highlighter, detector).
 *
 * The registry is populated once at package initialisation via init() and is
 * read-only thereafter.  No mutex is required for concurrent reads.
 *
 * Example:
 *   lang, ok := globalRegistry.DetectFromExtension(".go")
 *   chunker  := globalRegistry.GetChunker("go") // nil until Phase 3
 */
type LanguageRegistry struct {
	languages    map[string]LanguageInfo
	detectors    map[string]LanguageDetector
	chunkers     map[string]CodeChunker
	extractors   map[string]DocExtractor
	formatters   map[string]CodeFormatter
	highlighters map[string]SyntaxHighlighter

	// extExact maps the exact extension string (case-sensitive) to a language ID.
	extExact map[string]string
	// extLower maps the lowercase extension to a language ID (first-registered wins).
	// Used by HasExtension for case-insensitive membership tests.
	extLower map[string]string
}

/** NewLanguageRegistry returns an empty, ready-to-use registry.
 *
 * Returns:
 *   *LanguageRegistry — empty registry.
 *
 * Example:
 *   r := NewLanguageRegistry()
 *   r.RegisterLanguage(goInfo, nil, nil, nil, nil, nil)
 */
func NewLanguageRegistry() *LanguageRegistry {
	return &LanguageRegistry{
		languages:    make(map[string]LanguageInfo),
		detectors:    make(map[string]LanguageDetector),
		chunkers:     make(map[string]CodeChunker),
		extractors:   make(map[string]DocExtractor),
		formatters:   make(map[string]CodeFormatter),
		highlighters: make(map[string]SyntaxHighlighter),
		extExact:     make(map[string]string),
		extLower:     make(map[string]string),
	}
}

/** RegisterLanguage adds a language and its optional handlers to the registry.
 * Any handler may be nil; nil handlers are silently ignored in later lookups
 * (GetChunker etc. return nil).  Calling RegisterLanguage for the same ID a
 * second time overwrites the previous entry.
 *
 * Parameters:
 *   info        (LanguageInfo)     — language metadata.
 *   detector    (LanguageDetector) — content/extension detector, or nil.
 *   chunker     (CodeChunker)      — code-aware chunker, or nil.
 *   extractor   (DocExtractor)     — documentation extractor, or nil.
 *   formatter   (CodeFormatter)    — code formatter, or nil.
 *   highlighter (SyntaxHighlighter)— syntax highlighter, or nil.
 *
 * Example:
 *   r.RegisterLanguage(goInfo, nil, nil, nil, nil, nil)
 */
func (r *LanguageRegistry) RegisterLanguage(
	info LanguageInfo,
	detector LanguageDetector,
	chunker CodeChunker,
	extractor DocExtractor,
	formatter CodeFormatter,
	highlighter SyntaxHighlighter,
) {
	r.languages[info.ID] = info

	for _, ext := range info.Extensions {
		// Exact index: first registration of that exact string wins.
		if _, exists := r.extExact[ext]; !exists {
			r.extExact[ext] = info.ID
		}
		// Lowercase index: first registration of the lowercased form wins.
		lc := strings.ToLower(ext)
		if _, exists := r.extLower[lc]; !exists {
			r.extLower[lc] = info.ID
		}
	}

	if detector != nil {
		r.detectors[info.ID] = detector
	}
	if chunker != nil {
		r.chunkers[info.ID] = chunker
	}
	if extractor != nil {
		r.extractors[info.ID] = extractor
	}
	if formatter != nil {
		r.formatters[info.ID] = formatter
	}
	if highlighter != nil {
		r.highlighters[info.ID] = highlighter
	}
}

/** DetectFromExtension returns the language ID for the given file extension.
 * ext must include the leading dot, e.g. ".go".  Matching is case-sensitive
 * (exact) first; if no exact match is found, a case-insensitive fallback is
 * tried.  Returns ("", false) when no language owns that extension.
 *
 * Parameters:
 *   ext (string) — file extension with leading dot, e.g. ".go", ".Mod".
 *
 * Returns:
 *   language (string) — language ID, or "".
 *   ok       (bool)   — true when a match was found.
 *
 * Example:
 *   lang, ok := r.DetectFromExtension(".go")   // "go", true
 *   lang, ok  = r.DetectFromExtension(".Mod")  // "oberon", true
 *   lang, ok  = r.DetectFromExtension(".xyz")  // "", false
 */
func (r *LanguageRegistry) DetectFromExtension(ext string) (string, bool) {
	if lang, ok := r.extExact[ext]; ok {
		return lang, true
	}
	lang, ok := r.extLower[strings.ToLower(ext)]
	return lang, ok
}

/** HasExtension returns true when ext (matched case-insensitively) belongs to
 * any registered language.  Use this for membership tests when you do not need
 * to know which language owns the extension.
 *
 * Parameters:
 *   ext (string) — file extension with leading dot, e.g. ".mod".
 *
 * Returns:
 *   bool — true when the extension is known.
 *
 * Example:
 *   r.HasExtension(".go")   // true
 *   r.HasExtension(".Mod")  // true (Oberon, case-insensitive)
 *   r.HasExtension(".xyz")  // false
 */
func (r *LanguageRegistry) HasExtension(ext string) bool {
	_, ok := r.extLower[strings.ToLower(ext)]
	return ok
}

/** GetLanguageInfo returns the LanguageInfo for the given language ID.
 * ok is false when the language is not registered.
 *
 * Parameters:
 *   id (string) — language ID, e.g. "go".
 *
 * Returns:
 *   LanguageInfo — metadata struct.
 *   bool         — true when the language is registered.
 *
 * Example:
 *   info, ok := r.GetLanguageInfo("pascal")
 */
func (r *LanguageRegistry) GetLanguageInfo(id string) (LanguageInfo, bool) {
	info, ok := r.languages[id]
	return info, ok
}

/** DetectLanguage identifies the language of a file using a three-stage pipeline:
 * (1) binary check — returns ("", 0) for binary content,
 * (2) extension-based detection (confidence 1.0),
 * (3) shebang detection (confidence 0.9),
 * (4) keyword pattern detection (variable confidence).
 * Returns ("", 0) when no language can be identified.
 *
 * Parameters:
 *   filePath (string) — path to the file; extension is extracted with filepath.Ext.
 *   content  ([]byte) — file contents; only the first maxKeywordScan bytes are scanned.
 *
 * Returns:
 *   language   (string)  — language ID or "".
 *   confidence (float64) — confidence in [0, 1]; 0 means "not detected".
 *
 * Example:
 *   lang, conf := globalRegistry.DetectLanguage("main.go", data)  // "go", 1.0
 *   lang, conf  = globalRegistry.DetectLanguage("script", shData) // "python", 0.9
 */
func (r *LanguageRegistry) DetectLanguage(filePath string, content []byte) (string, float64) {
	if isBinary(content) {
		return "", 0.0
	}

	// Extension-based: highest confidence.
	if ext := filepath.Ext(filePath); ext != "" {
		if lang, ok := r.DetectFromExtension(ext); ok {
			return lang, 1.0
		}
	}

	// Shebang-based: only accept languages actually registered.
	if lang, ok := detectShebang(content); ok {
		if _, registered := r.languages[lang]; registered {
			return lang, 0.9
		}
	}

	// Keyword-based: only accept registered languages.
	if lang, conf := detectKeywords(content); conf > 0.5 {
		if _, registered := r.languages[lang]; registered {
			return lang, conf
		}
	}

	return "", 0.0
}

/** LanguageIDs returns the IDs of all registered languages in an unspecified order.
 *
 * Returns:
 *   []string — slice of language IDs.
 *
 * Example:
 *   for _, id := range r.LanguageIDs() { fmt.Println(id) }
 */
func (r *LanguageRegistry) LanguageIDs() []string {
	ids := make([]string, 0, len(r.languages))
	for id := range r.languages {
		ids = append(ids, id)
	}
	return ids
}

/** GetDetector returns the LanguageDetector registered for id, or nil.
 *
 * Parameters:
 *   id (string) — language ID.
 *
 * Returns:
 *   LanguageDetector — handler, or nil.
 *
 * Example:
 *   d := r.GetDetector("go")
 */
func (r *LanguageRegistry) GetDetector(id string) LanguageDetector {
	return r.detectors[id]
}

/** GetChunker returns the CodeChunker registered for id, or nil.
 *
 * Parameters:
 *   id (string) — language ID.
 *
 * Returns:
 *   CodeChunker — handler, or nil.
 *
 * Example:
 *   c := r.GetChunker("c")
 */
func (r *LanguageRegistry) GetChunker(id string) CodeChunker {
	return r.chunkers[id]
}

/** GetExtractor returns the DocExtractor registered for id, or nil.
 *
 * Parameters:
 *   id (string) — language ID.
 *
 * Returns:
 *   DocExtractor — handler, or nil.
 *
 * Example:
 *   e := r.GetExtractor("pascal")
 */
func (r *LanguageRegistry) GetExtractor(id string) DocExtractor {
	return r.extractors[id]
}

/** GetFormatter returns the CodeFormatter registered for id, or nil.
 *
 * Parameters:
 *   id (string) — language ID.
 *
 * Returns:
 *   CodeFormatter — handler, or nil.
 *
 * Example:
 *   f := r.GetFormatter("go")
 */
func (r *LanguageRegistry) GetFormatter(id string) CodeFormatter {
	return r.formatters[id]
}

/** GetHighlighter returns the SyntaxHighlighter registered for id, or nil.
 *
 * Parameters:
 *   id (string) — language ID.
 *
 * Returns:
 *   SyntaxHighlighter — handler, or nil.
 *
 * Example:
 *   h := r.GetHighlighter("lisp")
 */
func (r *LanguageRegistry) GetHighlighter(id string) SyntaxHighlighter {
	return r.highlighters[id]
}

// ─── Language metadata vars ───────────────────────────────────────────────────

// Programming languages with planned code-aware features.

var langGo = LanguageInfo{
	ID:             "go",
	Name:           "Go",
	Extensions:     []string{".go", ".mod", ".sum"},
	CommentMarkers: []string{"//", "/*", "*/"},
	BlockStart:     "{",
	BlockEnd:       "}",
	HasFormatting:  true, // gofmt (Phase 6)
}

var langTypeScript = LanguageInfo{
	ID:             "typescript",
	Name:           "TypeScript",
	Extensions:     []string{".ts"},
	CommentMarkers: []string{"//", "/*", "*/"},
	BlockStart:     "{",
	BlockEnd:       "}",
	HasFormatting:  true, // prettier (Phase 6)
}

var langJavaScript = LanguageInfo{
	ID:             "javascript",
	Name:           "JavaScript",
	Extensions:     []string{".js"},
	CommentMarkers: []string{"//", "/*", "*/"},
	BlockStart:     "{",
	BlockEnd:       "}",
	HasFormatting:  true, // prettier (Phase 6)
}

var langPython = LanguageInfo{
	ID:             "python",
	Name:           "Python",
	Extensions:     []string{".py"},
	CommentMarkers: []string{"#", `"""`, `"""`},
	BlockStart:     ":",
	BlockEnd:       "",
	Shebang:        "#!/usr/bin/env python3",
	HasFormatting:  true, // black (Phase 6)
}

var langRust = LanguageInfo{
	ID:             "rust",
	Name:           "Rust",
	Extensions:     []string{".rs"},
	CommentMarkers: []string{"//", "/*", "*/"},
	BlockStart:     "{",
	BlockEnd:       "}",
	HasFormatting:  true, // rustfmt (Phase 6)
}

var langC = LanguageInfo{
	ID:              "c",
	Name:            "C",
	Extensions:      []string{".c", ".h"},
	CommentMarkers:  []string{"//", "/*", "*/"},
	BlockStart:      "{",
	BlockEnd:        "}",
	Shebang:         "#!/usr/bin/env tcc",
	HasChunking:     true,
	HasExtraction:   true,
	HasFormatting:   true,
	HasHighlighting: true,
}

var langCPP = LanguageInfo{
	ID:              "cpp",
	Name:            "C++",
	Extensions:      []string{".cpp", ".cc", ".cxx", ".hpp", ".hh"},
	CommentMarkers:  []string{"//", "/*", "*/"},
	BlockStart:      "{",
	BlockEnd:        "}",
	HasChunking:     true,
	HasExtraction:   true,
	HasFormatting:   true,
	HasHighlighting: true,
}

var langPascal = LanguageInfo{
	ID:              "pascal",
	Name:            "Pascal",
	Extensions:      []string{".pas", ".p"},
	CommentMarkers:  []string{"//", "(*", "*)", "{", "}"},
	BlockStart:      "begin",
	BlockEnd:        "end;",
	HasChunking:     true,
	HasExtraction:   true,
	HasFormatting:   true,
	HasHighlighting: true,
}

var langOberon = LanguageInfo{
	ID:   "oberon",
	Name: "Oberon",
	// .Mod is the canonical Wirth/ETH convention (capital M).
	// .obn is also common.  On POSIX systems users may store Oberon files as
	// .mod (lowercase); since .mod is also used for Go module files, detection
	// falls back to "go" when the extension is lowercase — use content-based
	// detection (Phase 2) to disambiguate in that case.
	Extensions:      []string{".Mod", ".obn"},
	CommentMarkers:  []string{"(*", "*)"},
	BlockStart:      "BEGIN",
	BlockEnd:        "END",
	HasChunking:     true,
	HasExtraction:   true,
	HasFormatting:   true,
	HasHighlighting: true,
}

var langLisp = LanguageInfo{
	ID:              "lisp",
	Name:            "Lisp",
	Extensions:      []string{".lisp", ".lsp", ".cl", ".el"},
	CommentMarkers:  []string{";;", ";", "#|", "|#"},
	BlockStart:      "(",
	BlockEnd:        ")",
	Shebang:         "#!/usr/bin/env clisp",
	HasChunking:     true,
	HasExtraction:   true,
	HasFormatting:   true,
	HasHighlighting: true,
}

var langBasic = LanguageInfo{
	ID:              "basic",
	Name:            "Basic",
	Extensions:      []string{".bas", ".bi"},
	CommentMarkers:  []string{"REM", "'"},
	HasChunking:     true,
	HasExtraction:   true,
	HasFormatting:   true,
	HasHighlighting: true,
}

// Data and markup languages — file I/O only; no code-aware features planned.

var langJSON = LanguageInfo{
	ID:         "json",
	Name:       "JSON",
	Extensions: []string{".json"},
}

var langMarkdown = LanguageInfo{
	ID:         "markdown",
	Name:       "Markdown",
	Extensions: []string{".md"},
}

var langText = LanguageInfo{
	ID:         "text",
	Name:       "Text",
	Extensions: []string{".txt"},
}

var langCSS = LanguageInfo{
	ID:         "css",
	Name:       "CSS",
	Extensions: []string{".css"},
}

var langYAML = LanguageInfo{
	ID:         "yaml",
	Name:       "YAML",
	Extensions: []string{".yaml", ".yml"},
}

var langTOML = LanguageInfo{
	ID:         "toml",
	Name:       "TOML",
	Extensions: []string{".toml"},
}

var langSQL = LanguageInfo{
	ID:         "sql",
	Name:       "SQL",
	Extensions: []string{".sql"},
}

var langHTML = LanguageInfo{
	ID:         "html",
	Name:       "HTML",
	Extensions: []string{".html"},
}

var langShell = LanguageInfo{
	ID:             "shell",
	Name:           "Shell",
	Extensions:     []string{".sh", ".bash"},
	CommentMarkers: []string{"#"},
	Shebang:        "#!/usr/bin/env bash",
}

var langEnv = LanguageInfo{
	ID:         "env",
	Name:       "Environment",
	Extensions: []string{".env"},
}

// ─── Registry initialisation ──────────────────────────────────────────────────

// globalRegistry is the package-level language registry, populated once during
// package initialisation.  It is read-only after init() returns and requires no
// mutex for concurrent reads.
var globalRegistry *LanguageRegistry

func init() {
	globalRegistry = NewLanguageRegistry()
	initLanguages(globalRegistry)
	initChunkers(globalRegistry)
	initExtractors(globalRegistry)
	initHighlighters(globalRegistry)
	initFormatters(globalRegistry)
}

/** initLanguages populates r with metadata for all supported languages.
 * All handler slots (detector, chunker, extractor, formatter, highlighter)
 * are nil in Phase 1; they are filled by later phases.
 *
 * Parameters:
 *   r (*LanguageRegistry) — registry to populate; must not be nil.
 *
 * Example:
 *   r := NewLanguageRegistry()
 *   initLanguages(r)
 */
func initLanguages(r *LanguageRegistry) {
	// Register Go first so that ".mod" (Go module) wins the lowercase index
	// over Oberon's ".Mod" when the input extension is lowercase.
	r.RegisterLanguage(langGo,
		NewCombinedDetector(langGo.ID, langGo.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langTypeScript,
		NewCombinedDetector(langTypeScript.ID, langTypeScript.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langJavaScript,
		NewCombinedDetector(langJavaScript.ID, langJavaScript.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langPython,
		NewCombinedDetector(langPython.ID, langPython.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langRust,
		NewCombinedDetector(langRust.ID, langRust.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langC,
		NewCombinedDetector(langC.ID, langC.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langCPP,
		NewCombinedDetector(langCPP.ID, langCPP.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langPascal,
		NewCombinedDetector(langPascal.ID, langPascal.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langOberon,
		NewCombinedDetector(langOberon.ID, langOberon.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langLisp,
		NewCombinedDetector(langLisp.ID, langLisp.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langBasic,
		NewCombinedDetector(langBasic.ID, langBasic.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langJSON,
		NewCombinedDetector(langJSON.ID, langJSON.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langMarkdown,
		NewCombinedDetector(langMarkdown.ID, langMarkdown.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langText,
		NewCombinedDetector(langText.ID, langText.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langCSS,
		NewCombinedDetector(langCSS.ID, langCSS.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langYAML,
		NewCombinedDetector(langYAML.ID, langYAML.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langTOML,
		NewCombinedDetector(langTOML.ID, langTOML.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langSQL,
		NewCombinedDetector(langSQL.ID, langSQL.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langHTML,
		NewCombinedDetector(langHTML.ID, langHTML.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langShell,
		NewCombinedDetector(langShell.ID, langShell.Extensions),
		nil, nil, nil, nil)
	r.RegisterLanguage(langEnv,
		NewCombinedDetector(langEnv.ID, langEnv.Extensions),
		nil, nil, nil, nil)
}

// ─── looksLikePath helper (used by commands.go) ───────────────────────────────

// registryHasExt is the registry-backed test used by looksLikePath.
// It extracts the extension from s via filepath.Ext and delegates to
// globalRegistry.HasExtension.  Returns false when s has no extension.
func registryHasExt(s string) bool {
	ext := filepath.Ext(s)
	if ext == "" {
		return false
	}
	return globalRegistry.HasExtension(ext)
}
