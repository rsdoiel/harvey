# Programming Language Support Design

## Overview

This document describes the design for comprehensive programming language support in Harvey, with a focus on enhancing interactions with source code across all languages currently supported by the RAG system.

**Status:** Draft  
**Author:** Harvey Design Team  
**Created:** 2026-06-09  
**Related Documents:**
- [Using_RAGs_with_Harvey.md](Using_RAGs_with_Harvey.md)
- [DECISIONS.md](DECISIONS.md)
- [ARCHITECTURE.md](ARCHITECTURE.md)

---

## Current State Analysis

### Supported Languages

Based on the RAG ingestion system (`commands.go:4975-4979`) and documentation, Harvey currently recognizes the following programming language file extensions:

| Language | Primary Extension | Additional Extensions | Current Support Level |
|----------|------------------|---------------------|----------------------|
| **Go** | `.go` | `.mod`, `.sum` | Full |
| **TypeScript** | `.ts` | | Full |
| **JavaScript** | `.js` | | Full |
| **Python** | `.py` | | Full |
| **Rust** | `.rs` | | Full |
| **C** | `.c` | `.h` | Basic |
| **C++** | `.cpp` | `.hpp`, `.h` | Basic |
| **Pascal** | `.pas` | | Basic |
| **Oberon** | `.Mod` | `.obn` | Basic |
| **Lisp** | `.lisp` | | Basic |
| **Basic** | `.bas` | | Basic |
| **JSON** | `.json` | | Full |
| **Markdown** | `.md` | | Full |
| **Text** | `.txt` | | Full |
| **CSS** | `.css` | | Full |
| **YAML** | `.yaml`, `.yml` | | Full |
| **TOML** | `.toml` | | Full |
| **SQL** | `.sql` | | Full |
| **HTML** | `.html` | | Full |
| **Shell** | `.sh`, `.bash` | | Full |
| **Environment** | `.env` | | Full |

**Support Levels:**
- **Full:** File extension recognized in all subsystems (RAG ingestion, code block detection)
- **Basic:** File extension recognized in RAG ingestion but missing from code block path detection

### Identified Gaps

1. **Code Block Path Detection (`looksLikePath` function)**
   - **Issue:** Extensions `.c`, `.cpp`, `.h`, `.hpp`, `.pas`, `.Mod`, `.obn`, `.lisp`, `.bas` were missing
   - **Status:** ✅ FIXED (2026-06-09) - Added to `commands.go:3463-3472`
   - **Impact:** Tagged code blocks like ````c:program.c` or ````pascal:module.pas` were not recognized as file paths

2. **Code-Aware Chunking for RAG**
   - **Issue:** Current `ragChunk()` function (line 5272) uses generic paragraph-based splitting (~500 chars)
   - **Problem:** Breaks code structures (functions, procedures, classes) across chunk boundaries
   - **Impact:** Reduced retrieval quality for code-related queries

3. **Language-Specific Metadata Extraction**
   - **Issue:** No extraction of comments, docstrings, or structured documentation from code
   - **Problem:** Code files without natural language text don't embed well
   - **Impact:** Poor semantic understanding of code structure

4. **Syntax-Aware Tooling**
   - **Issue:** No language-specific tools (symbol lookup, function listing, etc.)
   - **Problem:** Users cannot query code structure effectively
   - **Impact:** Limited code navigation capabilities

5. **Code Formatting on Write**
   - **Issue:** No automatic formatting when writing source files
   - **Problem:** Inconsistent code style in workspace
   - **Impact:** Reduced code quality

6. **Syntax Highlighting in Output**
   - **Issue:** Code blocks in terminal output are not colorized by language
   - **Problem:** Harder to read and understand code in responses
   - **Impact:** Poor user experience

---

## Architecture

### Design Principles

1. **Language Detection First:** All file operations should first detect the language from file extension
2. **Progressive Enhancement:** Basic support (file I/O) works for all languages; advanced features (parsing, formatting) are opt-in
3. **Extensibility:** New languages can be added by registering handlers, not by modifying core code
4. **Performance:** Language-specific features should not significantly impact performance for non-code files
5. **Fallback Gracefully:** If language-specific handler fails, fall back to generic behavior

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                     HARVEY LANGUAGE SUPPORT                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────┐  │
│  │   File I/O      │     │ Language        │     │   RAG       │  │
│  │   (existing)    │────▶│ Detection       │────▶│   Store     │  │
│  └─────────────────┘     └─────────────────┘     └─────────────┘  │
│         │                       │                       │           │
│         ▼                       ▼                       ▼           │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────┐  │
│  │   Generic       │     │ Language        │     │ Code-Aware  │  │
│  │   Handlers      │     │ Registry        │     │ Chunkers    │  │
│  └─────────────────┘     └─────────────────┘     └─────────────┘  │
│         │                       │                       │           │
│         ▼                       ▼                       ▼           │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    Language-Specific Services                 │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────┐   │   │
│  │  │ Chunkers  │ │ Extractors│ │ Formatters│ │ Analyzers  │   │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └────────────┘   │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Core Interfaces

#### 1. LanguageDetector

```go
// LanguageDetector identifies the programming language of a file
type LanguageDetector interface {
    // Detect returns the language identifier and confidence score (0.0-1.0)
    Detect(filePath string, content []byte) (language string, confidence float64)
    
    // DetectFromExtension returns language from file extension only
    DetectFromExtension(ext string) (language string, ok bool)
}
```

#### 2. CodeChunker

```go
// CodeChunker splits source code into meaningful chunks for RAG
type CodeChunker interface {
    // Chunk splits content into meaningful units (functions, classes, etc.)
    Chunk(content string, filePath string) []EnrichedChunk
    
    // Language returns the language identifier this chunker handles
    Language() string
}

// EnrichedChunk contains code content with metadata.
// StartLine/StartCol and EndLine/EndCol together provide a precise re-location
// address so a reader can jump directly to the chunk in the source file.
// Columns are 0-indexed byte offsets from the start of the line.
type EnrichedChunk struct {
    Content   string   // The text chunk
    StartLine int      // Starting line number (1-indexed)
    StartCol  int      // Starting column — byte offset from line start (0-indexed)
    EndLine   int      // Ending line number (1-indexed)
    EndCol    int      // Ending column — byte offset from last line start (0-indexed)
    ChunkType string   // "function", "class", "procedure", "comment", "code"
    Symbols   []string // Identifiers defined in this chunk
    Docs      string   // Associated documentation/comments extracted from comments
}
```

#### 3. DocExtractor

```go
// DocExtractor extracts documentation from source code
type DocExtractor interface {
    // ExtractDocs extracts documentation blocks from source
    ExtractDocs(content string) []DocumentationBlock
    
    // ExtractSymbols extracts symbol -> documentation mapping
    ExtractSymbols(content string) map[string]string
    
    // Language returns the language identifier
    Language() string
}

// DocumentationBlock represents extracted documentation
type DocumentationBlock struct {
    Content    string // The documentation text
    StartLine int    // Starting line number
    EndLine   int    // Ending line number
    Symbol    string // Associated symbol (if any)
    Type      string // "comment", "docstring", "annotation"
}
```

#### 4. CodeFormatter

Formatters fall into two modes that must be handled differently:

- **PipeFormatter**: reads content from stdin, writes formatted output to stdout.
  Example: `clang-format -`, `black -`, `gofmt`.
  Harvey pipes content in and reads the result back; no file is written until
  formatting succeeds. This mode works with safe mode on.

- **FileFormatter**: reads and rewrites a file at a given path in place.
  Example: some Pascal and Oberon formatters that only accept a file path.
  Harvey writes the file first, then invokes the formatter on it.
  **Requires safe mode off** — the formatter executable must be in the
  `allowed_commands` list, just like any `/run` command.

```go
// FormatterMode describes how a formatter consumes and produces code.
type FormatterMode int

const (
    PipeFormatter FormatterMode = iota // stdin→stdout; no filesystem side effects
    FileFormatter                       // reads/rewrites the file at the given path
)

// CodeFormatter formats source code according to language conventions
type CodeFormatter interface {
    // Format formats the given source code.
    // For PipeFormatter: content is the input, returned string is the output.
    // For FileFormatter: content is written to filePath first; return value
    // is the re-read file content after the formatter runs.
    Format(content string, filePath string) (string, error)

    // Check checks if content is already properly formatted
    Check(content string, filePath string) (bool, []FormatIssue)

    // Language returns the language identifier
    Language() string

    // Mode returns PipeFormatter or FileFormatter.
    Mode() FormatterMode
}

// FormatIssue describes a formatting problem
type FormatIssue struct {
    Line     int    // Line number
    Column   int    // Column number
    Message  string // Description of the issue
    Severity string // "error", "warning", "info"
}
```

#### 5. SyntaxHighlighter

```go
// SyntaxHighlighter adds color to source code for terminal display
type SyntaxHighlighter interface {
    // Highlight returns ANSI-colored content
    Highlight(content string, lang string) string
    
    // GetStyle returns the style configuration for a language
    GetStyle(lang string) *HighlightStyle
}

// HighlightStyle defines coloring for syntax elements
type HighlightStyle struct {
    Keyword     string // ANSI color for keywords
    String      string // ANSI color for strings
    Comment     string // ANSI color for comments
    Number      string // ANSI color for numbers
    Operator    string // ANSI color for operators
    Function    string // ANSI color for function names
    Type        string // ANSI color for type names
    Builtin     string // ANSI color for builtins
}
```

---

## Language Registry

### Design

A central registry that maps language identifiers to their handlers:

```go
// LanguageRegistry manages all language-specific handlers
type LanguageRegistry struct {
    detectors   map[string]LanguageDetector
    chunkers    map[string]CodeChunker
    extractors  map[string]DocExtractor
    formatters  map[string]CodeFormatter
    highlighters map[string]SyntaxHighlighter
    
    // Language metadata
    languages map[string]LanguageInfo
}

// LanguageInfo contains metadata about a language
type LanguageInfo struct {
    ID          string   // e.g., "go", "c", "pascal"
    Name        string   // e.g., "Go", "C", "Pascal"
    Extensions  []string // e.g., [".go"], [".c", ".h"], [".pas"]
    CommentMarkers []string // Line and block comment markers
    BlockStart   string   // Starting delimiter for code blocks (e.g., "{" for C)
    BlockEnd     string   // Ending delimiter for code blocks (e.g., "}" for C)
    Shebang      string   // Shebang line if applicable
    
    // Capabilities
    HasChunking    bool
    HasExtraction  bool
    HasFormatting  bool
    HasHighlighting bool
}

// Global registry instance
var languageRegistry = NewLanguageRegistry()

// RegisterLanguage registers all handlers for a language
func (r *LanguageRegistry) RegisterLanguage(info LanguageInfo, 
    detector LanguageDetector,
    chunker CodeChunker,
    extractor DocExtractor,
    formatter CodeFormatter,
    highlighter SyntaxHighlighter) {
    // Store all handlers
}

// GetChunker returns the chunker for a language, or nil if not available
func (r *LanguageRegistry) GetChunker(lang string) CodeChunker {
    return r.chunkers[lang]
}

// DetectLanguage detects the language from file path or content
func (r *LanguageRegistry) DetectLanguage(filePath string, content []byte) (string, float64) {
    // Try extension-based detection first
    ext := filepath.Ext(filePath)
    if lang, ok := r.detectFromExtension(ext); ok {
        return lang, 1.0
    }
    // Try content-based detection
    for _, detector := range r.detectors {
        if lang, conf := detector.Detect(filePath, content); conf > 0.5 {
            return lang, conf
        }
    }
    return "", 0.0
}
```

### Initialization

```go
// initLanguages initializes the language registry with all supported languages
func initLanguages() {
    // Register Go
    languageRegistry.RegisterLanguage(goLangInfo,
        &GoDetector{},
        &GoChunker{},
        &GoDocExtractor{},
        &GoFormatter{},
        &GoHighlighter{})
    
    // Register C
    languageRegistry.RegisterLanguage(cLangInfo,
        &CDetector{},
        &CChunker{},
        &CDocExtractor{},
        &CFormatter{},
        &CHighlighter{})
    
    // Register Pascal
    languageRegistry.RegisterLanguage(pascalLangInfo,
        &PascalDetector{},
        &PascalChunker{},
        &PascalDocExtractor{},
        &PascalFormatter{},
        &PascalHighlighter{})
    
    // Register Oberon
    languageRegistry.RegisterLanguage(oberonLangInfo,
        &OberonDetector{},
        &OberonChunker{},
        &OberonDocExtractor{},
        &OberonFormatter{},
        &OberonHighlighter{})
    
    // Register Lisp
    languageRegistry.RegisterLanguage(lispLangInfo,
        &LispDetector{},
        &LispChunker{},
        &LispDocExtractor{},
        &LispFormatter{},
        &LispHighlighter{})
    
    // Register Basic
    languageRegistry.RegisterLanguage(basicLangInfo,
        &BasicDetector{},
        &BasicChunker{},
        &BasicDocExtractor{},
        &BasicFormatter{},
        &BasicHighlighter{})
    
    // ... other languages
}
```

---

## Language-Specific Implementations

### 1. C / C++

#### Language Info
```go
var cLangInfo = LanguageInfo{
    ID:          "c",
    Name:        "C",
    Extensions:  []string{".c", ".h"},
    CommentMarkers: []string{"//", "/*", "*/"},
    BlockStart:   "{",
    BlockEnd:     "}",
    Shebang:      "#!/usr/bin/env tcc\n",
    HasChunking:    true,
    HasExtraction:  true,
    HasFormatting:  true,
    HasHighlighting: true,
}

var cppLangInfo = LanguageInfo{
    ID:          "cpp",
    Name:        "C++",
    Extensions:  []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".h"},
    CommentMarkers: []string{"//", "/*", "*/"},
    BlockStart:   "{",
    BlockEnd:     "}",
    Shebang:      "",
    HasChunking:    true,
    HasExtraction:  true,
    HasFormatting:  true,
    HasHighlighting: true,
}
```

#### Chunker Implementation
```go
type CChunker struct{}

func (c *CChunker) Language() string { return "c" }

func (c *CChunker) Chunk(content string, filePath string) []EnrichedChunk {
    var chunks []EnrichedChunk
    
    // Parse into preprocessor directives, functions, structs
    lines := strings.Split(content, "\n")
    
    var currentChunk *EnrichedChunk
    var braceDepth int
    var inFunction bool
    var functionStart int
    
    for i, line := range lines {
        lineNum := i + 1
        trimmed := strings.TrimSpace(line)
        
        // Handle preprocessor directives
        if strings.HasPrefix(trimmed, "#") {
            if currentChunk == nil || currentChunk.ChunkType != "preprocessor" {
                // Start new preprocessor chunk
                currentChunk = &EnrichedChunk{
                    Content:    line + "\n",
                    StartLine: lineNum,
                    ChunkType:  "preprocessor",
                }
                chunks = append(chunks, *currentChunk)
            } else {
                currentChunk.Content += line + "\n"
            }
            currentChunk.EndLine = lineNum
            continue
        }
        
        // Handle comments (extract but keep with code)
        if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
            // Extract comment and associate with current context
            continue
        }
        
        // Handle brace counting for code blocks
        if strings.Contains(trimmed, "{") {
            braceDepth += strings.Count(trimmed, "{")
        }
        if strings.Contains(trimmed, "}") {
            braceDepth -= strings.Count(trimmed, "}")
        }
        
        // Start of function/type definition
        if braceDepth == 0 && (strings.Contains(trimmed, "(") || 
            strings.Contains(trimmed, "{")) && 
            !inFunction {
            // This might be a function start
            inFunction = true
            functionStart = lineNum
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "function",
            }
            chunks = append(chunks, *currentChunk)
            continue
        }
        
        // Continue function
        if inFunction {
            currentChunk.Content += line + "\n"
            currentChunk.EndLine = lineNum
            
            // End of function
            if braceDepth == 0 && strings.Contains(trimmed, "}") {
                inFunction = false
                currentChunk = nil
            }
            continue
        }
        
        // Regular code line
        if currentChunk == nil {
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "code",
            }
            chunks = append(chunks, *currentChunk)
        } else {
            currentChunk.Content += line + "\n"
        }
        currentChunk.EndLine = lineNum
        
        // If chunk gets too large, finalize it
        if len(currentChunk.Content) > 1000 {
            currentChunk = nil
        }
    }
    
    // Post-process: Extract symbols and documentation
    for i := range chunks {
        chunks[i].Symbols = extractCSymbols(chunks[i].Content)
        chunks[i].Docs = extractCDocs(chunks[i].Content)
    }
    
    return chunks
}
```

#### DocExtractor Implementation
```go
type CDocExtractor struct{}

func (c *CDocExtractor) Language() string { return "c" }

func (c *CDocExtractor) ExtractDocs(content string) []DocumentationBlock {
    var docs []DocumentationBlock
    lines := strings.Split(content, "\n")
    
    var inBlockComment bool
    var blockComment strings.Builder
    var blockStart int
    
    for i, line := range lines {
        lineNum := i + 1
        trimmed := strings.TrimSpace(line)
        
        // Line comments
        if strings.HasPrefix(trimmed, "//") {
            comment := strings.TrimPrefix(trimmed, "//")
            comment = strings.TrimSpace(comment)
            if comment != "" {
                docs = append(docs, DocumentationBlock{
                    Content:    comment,
                    StartLine: lineNum,
                    EndLine:   lineNum,
                    Type:      "line_comment",
                })
            }
        }
        
        // Block comments
        if strings.HasPrefix(trimmed, "/*") {
            inBlockComment = true
            blockComment.Reset()
            blockStart = lineNum
            comment := strings.TrimPrefix(trimmed, "/*")
            blockComment.WriteString(comment + "\n")
        }
        
        if inBlockComment {
            if strings.HasSuffix(trimmed, "*/") {
                inBlockComment = false
                comment := strings.TrimSuffix(blockComment.String(), "*/")
                docs = append(docs, DocumentationBlock{
                    Content:    strings.TrimSpace(comment),
                    StartLine: blockStart,
                    EndLine:   lineNum,
                    Type:      "block_comment",
                })
            } else {
                blockComment.WriteString(line + "\n")
            }
        }
    }
    
    return docs
}

func (c *CDocExtractor) ExtractSymbols(content string) map[string]string {
    symbols := make(map[string]string)
    
    // Use regex to find function definitions
    // Pattern: return_type function_name(params)
    re := regexp.MustCompile(`(\w+\s+)+(\w+)\s*\([^)]*\)\s*(?:;|\{)`)
    matches := re.FindAllStringSubmatch(content, -1)
    
    for _, match := range matches {
        if len(match) >= 3 {
            returnType := strings.TrimSpace(match[1])
            funcName := strings.TrimSpace(match[2])
            symbols[funcName] = returnType + " " + funcName
        }
    }
    
    // Extract struct/type definitions
    re = regexp.MustCompile(`(?:struct|typedef\s+struct|class|enum|union)\s+(\w+)`)
    matches = re.FindAllStringSubmatch(content, -1)
    
    for _, match := range matches {
        if len(match) >= 2 {
            typeName := match[1]
            symbols[typeName] = "type " + typeName
        }
    }
    
    return symbols
}
```

### 2. Pascal

#### Language Info
```go
var pascalLangInfo = LanguageInfo{
    ID:          "pascal",
    Name:        "Pascal",
    Extensions:  []string{".pas", ".p"},
    CommentMarkers: []string{"//", "(*", "*)", "{ ", " }"},
    BlockStart:   "begin",
    BlockEnd:     "end;",
    Shebang:      "",
    HasChunking:    true,
    HasExtraction:  true,
    HasFormatting:  true,
    HasHighlighting: true,
}
```

#### Chunker Implementation
```go
type PascalChunker struct{}

func (p *PascalChunker) Language() string { return "pascal" }

func (p *PascalChunker) Chunk(content string, filePath string) []EnrichedChunk {
    var chunks []EnrichedChunk
    lines := strings.Split(content, "\n")
    
    var currentChunk *EnrichedChunk
    var nestingLevel int
    var inProcedure bool
    var inType bool
    var inConst bool
    var inVar bool
    
    for i, line := range lines {
        lineNum := i + 1
        trimmed := strings.TrimSpace(line)
        upper := strings.ToUpper(trimmed)
        
        // Skip comments
        if strings.HasPrefix(upper, "//") || 
           strings.HasPrefix(upper, "(*") || 
           strings.HasPrefix(upper, "{ ") {
            continue
        }
        
        // Handle block ends
        if strings.HasPrefix(upper, "END.") {
            if currentChunk != nil {
                currentChunk.Content += line + "\n"
                currentChunk.EndLine = lineNum
                currentChunk = nil
            }
            continue
        }
        
        // Count nesting
        if strings.Contains(upper, "BEGIN") {
            nestingLevel++
        }
        if strings.Contains(upper, "END") && !strings.HasSuffix(upper, ";") {
            nestingLevel--
        }
        
        // Start of procedure/function
        if nestingLevel == 0 && (strings.HasPrefix(upper, "PROCEDURE ") || 
            strings.HasPrefix(upper, "FUNCTION ")) {
            if currentChunk != nil {
                currentChunk.EndLine = lineNum - 1
            }
            inProcedure = true
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "procedure",
            }
            chunks = append(chunks, *currentChunk)
            continue
        }
        
        // Start of type definition
        if nestingLevel == 0 && strings.HasPrefix(upper, "TYPE ") {
            if currentChunk != nil {
                currentChunk.EndLine = lineNum - 1
            }
            inType = true
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "type",
            }
            chunks = append(chunks, *currentChunk)
            continue
        }
        
        // Continue current chunk
        if currentChunk != nil {
            currentChunk.Content += line + "\n"
            currentChunk.EndLine = lineNum
            
            // End procedure at end;
            if inProcedure && strings.Contains(upper, "END;") {
                inProcedure = false
                currentChunk = nil
            }
            continue
        }
        
        // Regular code
        if currentChunk == nil {
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "code",
            }
            chunks = append(chunks, *currentChunk)
        } else {
            currentChunk.Content += line + "\n"
        }
        currentChunk.EndLine = lineNum
        
        // Size limit
        if len(currentChunk.Content) > 1000 {
            currentChunk = nil
        }
    }
    
    // Extract symbols and docs
    for i := range chunks {
        chunks[i].Symbols = extractPascalSymbols(chunks[i].Content)
        chunks[i].Docs = extractPascalDocs(chunks[i].Content)
    }
    
    return chunks
}
```

### 3. Oberon

#### Language Info
```go
var oberonLangInfo = LanguageInfo{
    ID:          "oberon",
    Name:        "Oberon",
    // .Mod is the canonical Wirth/ETH convention; .mod is common on POSIX systems.
    // The registry DetectFromExtension must match case-insensitively for these.
    Extensions:  []string{".Mod", ".mod", ".obn"},
    CommentMarkers: []string{"(*", "*)"},
    BlockStart:   "BEGIN",
    BlockEnd:     "END",
    Shebang:      "",
    HasChunking:    true,
    HasExtraction:  true,
    HasFormatting:  true,
    HasHighlighting: true,
}
```

#### Chunker Implementation
```go
type OberonChunker struct{}

func (o *OberonChunker) Language() string { return "oberon" }

func (o *OberonChunker) Chunk(content string, filePath string) []EnrichedChunk {
    var chunks []EnrichedChunk
    lines := strings.Split(content, "\n")
    
    var currentChunk *EnrichedChunk
    var inModule bool
    var inProcedure bool
    var nestingLevel int
    
    for i, line := range lines {
        lineNum := i + 1
        trimmed := strings.TrimSpace(line)
        
        // Skip comments
        if strings.HasPrefix(trimmed, "(*") {
            continue
        }
        
        // MODULE definition
        if strings.HasPrefix(trimmed, "MODULE ") {
            if currentChunk != nil {
                currentChunk.EndLine = lineNum - 1
            }
            inModule = true
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "module",
            }
            chunks = append(chunks, *currentChunk)
            continue
        }
        
        // PROCEDURE definition
        if strings.HasPrefix(trimmed, "PROCEDURE ") {
            if currentChunk != nil {
                currentChunk.EndLine = lineNum - 1
            }
            inProcedure = true
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "procedure",
            }
            chunks = append(chunks, *currentChunk)
            continue
        }
        
        // BEGIN/END counting
        if strings.HasPrefix(trimmed, "BEGIN") {
            nestingLevel++
        }
        if strings.HasPrefix(trimmed, "END") {
            if inModule && nestingLevel == 0 {
                inModule = false
            }
            if inProcedure && nestingLevel == 0 {
                inProcedure = false
            }
            nestingLevel--
        }
        
        // Continue current chunk
        if currentChunk != nil {
            currentChunk.Content += line + "\n"
            currentChunk.EndLine = lineNum
            continue
        }
        
        // Regular code
        if currentChunk == nil {
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "code",
            }
            chunks = append(chunks, *currentChunk)
        } else {
            currentChunk.Content += line + "\n"
        }
        currentChunk.EndLine = lineNum
        
        // Size limit
        if len(currentChunk.Content) > 1000 {
            currentChunk = nil
        }
    }
    
    // Extract symbols and docs
    for i := range chunks {
        chunks[i].Symbols = extractOberonSymbols(chunks[i].Content)
        chunks[i].Docs = extractOberonDocs(chunks[i].Content)
    }
    
    return chunks
}
```

### 4. Lisp

#### Language Info
```go
var lispLangInfo = LanguageInfo{
    ID:          "lisp",
    Name:        "Lisp",
    Extensions:  []string{".lisp", ".lsp", ".cl", ".el"},
    CommentMarkers: []string{";;", ";", "#|", "|#"},
    BlockStart:   "(",
    BlockEnd:     ")",
    Shebang:      "#!/usr/bin/env clisp\n",
    HasChunking:    true,
    HasExtraction:  true,
    HasFormatting:  true,
    HasHighlighting: true,
}
```

#### Chunker Implementation
```go
type LispChunker struct{}

func (l *LispChunker) Language() string { return "lisp" }

func (l *LispChunker) Chunk(content string, filePath string) []EnrichedChunk {
    var chunks []EnrichedChunk
    
    // Use a parenthesis-balanced parser
    var currentChunk *EnrichedChunk
    var parenDepth int
    var inTopLevel bool
    var formStart int
    
    // Split into tokens while tracking parenthesis depth
    tokens := tokenizeLisp(content)
    
    for i, token := range tokens {
        token = strings.TrimSpace(token)
        if token == "" {
            continue
        }
        
        // Track depth
        if token == "(" {
            parenDepth++
        }
        if token == ")" {
            parenDepth--
        }
        
        // At top level (depth 0)
        if parenDepth == 0 {
            // End of current form
            if currentChunk != nil {
                currentChunk.Content = strings.TrimRight(currentChunk.Content, " ")
                currentChunk.EndLine = findLineNumber(content, formStart, i)
                chunks = append(chunks, *currentChunk)
                currentChunk = nil
            }
            continue
        }
        
        // Start of new top-level form
        if parenDepth == 1 && currentChunk == nil {
            formStart = i
            currentChunk = &EnrichedChunk{
                Content:    token + " ",
                StartLine: findLineNumber(content, i, i),
                ChunkType:  classifyLispForm(token),
            }
            continue
        }
        
        // Continue current form
        if currentChunk != nil {
            currentChunk.Content += token + " "
        }
    }
    
    // Handle last form
    if currentChunk != nil {
        currentChunk.Content = strings.TrimRight(currentChunk.Content, " ")
        currentChunk.EndLine = findLineNumber(content, formStart, len(tokens))
        chunks = append(chunks, *currentChunk)
    }
    
    // Extract symbols and docs
    for i := range chunks {
        chunks[i].Symbols = extractLispSymbols(chunks[i].Content)
        chunks[i].Docs = extractLispDocs(chunks[i].Content)
    }
    
    return chunks
}

func classifyLispForm(firstToken string) string {
    switch strings.ToUpper(firstToken) {
    case "DEFUN":
        return "function"
    case "DEFVAR", "DEFPARAMETER", "DEFCONSTANT":
        return "variable"
    case "DEFMACRO":
        return "macro"
    case "DEFTYPE", "DEFSTRUCT", "DEFCLASS":
        return "type"
    case ";":
        return "comment"
    default:
        return "form"
    }
}

func tokenizeLisp(content string) []string {
    // Split by whitespace but respect quoted strings and parenthesis
    var tokens []string
    var current strings.Builder
    inString := false
    escapeNext := false
    
    for _, ch := range content {
        if escapeNext {
            current.WriteRune(ch)
            escapeNext = false
            continue
        }
        
        if ch == '\\' {
            current.WriteRune(ch)
            escapeNext = true
            continue
        }
        
        if ch == '"' {
            inString = !inString
            current.WriteRune(ch)
            continue
        }
        
        if inString {
            current.WriteRune(ch)
            continue
        }
        
        // Handle parenthesis
        if ch == '(' || ch == ')' || ch == '\'' {
            if current.Len() > 0 {
                tokens = append(tokens, current.String())
                current.Reset()
            }
            tokens = append(tokens, string(ch))
            continue
        }
        
        // Handle whitespace
        if unicode.IsSpace(ch) {
            if current.Len() > 0 {
                tokens = append(tokens, current.String())
                current.Reset()
            }
            tokens = append(tokens, " ")
            continue
        }
        
        current.WriteRune(ch)
    }
    
    if current.Len() > 0 {
        tokens = append(tokens, current.String())
    }
    
    return tokens
}
```

### 5. Basic

#### Language Info
```go
var basicLangInfo = LanguageInfo{
    ID:          "basic",
    Name:        "Basic",
    Extensions:  []string{".bas", ".bi"},
    CommentMarkers: []string{"REM", "'"},
    BlockStart:   "",
    BlockEnd:     "",
    Shebang:      "",
    HasChunking:    true,
    HasExtraction:  true,
    HasFormatting:  true,
    HasHighlighting: true,
}
```

#### Chunker Implementation
```go
type BasicChunker struct{}

func (b *BasicChunker) Language() string { return "basic" }

func (b *BasicChunker) Chunk(content string, filePath string) []EnrichedChunk {
    var chunks []EnrichedChunk
    lines := strings.Split(content, "\n")
    
    var currentChunk *EnrichedChunk
    var inSub bool
    var inFunction bool
    var lineNum int
    
    for i, line := range lines {
        lineNum = i + 1
        upper := strings.ToUpper(strings.TrimSpace(line))
        
        // Skip comments
        if strings.HasPrefix(upper, "REM ") || 
           strings.HasPrefix(upper, "'") {
            continue
        }
        
        // Line numbers
        if isBasicLineNumber(line) {
            continue
        }
        
        // Start of SUB
        if strings.HasPrefix(upper, "SUB ") || strings.HasPrefix(upper, "FUNCTION ") {
            if currentChunk != nil {
                currentChunk.EndLine = lineNum - 1
            }
            inSub = true
            inFunction = strings.HasPrefix(upper, "FUNCTION ")
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "sub",
            }
            chunks = append(chunks, *currentChunk)
            continue
        }
        
        // End of SUB/FUNCTION
        if inSub && (strings.HasPrefix(upper, "END SUB") || 
            strings.HasPrefix(upper, "END FUNCTION")) {
            if currentChunk != nil {
                currentChunk.Content += line + "\n"
                currentChunk.EndLine = lineNum
                currentChunk = nil
            }
            inSub = false
            inFunction = false
            continue
        }
        
        // Continue current chunk
        if currentChunk != nil {
            currentChunk.Content += line + "\n"
            currentChunk.EndLine = lineNum
            continue
        }
        
        // Regular code
        if currentChunk == nil {
            currentChunk = &EnrichedChunk{
                Content:    line + "\n",
                StartLine: lineNum,
                ChunkType:  "code",
            }
            chunks = append(chunks, *currentChunk)
        } else {
            currentChunk.Content += line + "\n"
        }
        currentChunk.EndLine = lineNum
        
        // Size limit
        if len(currentChunk.Content) > 1000 {
            currentChunk = nil
        }
    }
    
    // Extract symbols and docs
    for i := range chunks {
        chunks[i].Symbols = extractBasicSymbols(chunks[i].Content)
        chunks[i].Docs = extractBasicDocs(chunks[i].Content)
    }
    
    return chunks
}
```

---

## Integration Points

### 1. RAG Ingestion Pipeline

**Current Flow:**
```
ragIngestFile() 
  → Read file
  → ragChunk() (generic, ~500 chars)
  → Store chunks
```

**New Flow:**
```
ragIngestFile() 
  → Read file
  → Detect language from extension (case-insensitive for Oberon .Mod/.mod)
  → Reject binary files (log warning, return 0 chunks)
  → If code language:
    → Get language-specific chunker from registry
    → Chunk with CodeChunker (structure-aware)
      → Multi-line comments: extract block as unit, then split into lines
      → Each chunk records StartLine/StartCol/EndLine/EndCol for re-location
    → Enrich with DocExtractor (Phase 4)
  → Else:
    → Use generic ragChunk()
  → Store EnrichedChunk metadata in SQLite alongside text (required)
```

**Multi-line comment extraction pattern:**

Chunkers use a simple single-line approximation but handle multi-line comments
by first extracting the full comment block as a unit, then splitting it into
individual lines before ingesting. This avoids the complexity of a full parser
while still preventing comment text from contaminating adjacent code chunks.

```go
// extractMultiLineComment returns the full comment text and the line after it ends.
// For `(* ... *)` and `/* ... */` styles spanning multiple lines.
func extractMultiLineComment(lines []string, startIdx int, open, close string) (text string, nextIdx int) {
    var buf strings.Builder
    for i := startIdx; i < len(lines); i++ {
        buf.WriteString(lines[i] + "\n")
        if strings.Contains(lines[i], close) {
            return buf.String(), i + 1
        }
    }
    return buf.String(), len(lines) // unterminated comment — consume to EOF
}
```

The returned text is then split on `"\n"` and each non-empty line becomes a
`"comment"` chunk so it can be embedded and retrieved independently.

**Implementation in `commands.go`:**
```go
func ragIngestFile(store *RagStore, embedder Embedder, path string) (int, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return 0, err
    }

    // Reject binary files before any processing.
    if !isTextContent(data) {
        debugLog("ragIngestFile: skipping binary file %s", path)
        return 0, nil
    }

    ext := filepath.Ext(path) // preserve case for Oberon .Mod/.mod
    lang := languageRegistry.DetectFromExtension(ext)

    var enrichedChunks []EnrichedChunk

    if lang != "" {
        if chunker := languageRegistry.GetChunker(lang); chunker != nil {
            enrichedChunks = chunker.Chunk(string(data), path)
        }
    }

    if len(enrichedChunks) == 0 {
        // Fallback: wrap generic chunks as EnrichedChunk with no metadata.
        for _, c := range ragChunk(string(data)) {
            enrichedChunks = append(enrichedChunks, EnrichedChunk{Content: c})
        }
    }

    if len(enrichedChunks) == 0 {
        return 0, nil
    }
    if err := store.IngestEnriched(path, enrichedChunks, embedder); err != nil {
        return 0, err
    }
    return len(enrichedChunks), nil
}
```

### 2. Code Block Path Detection

**Current:** `looksLikePath` uses hardcoded extension list  
**New:** Use language registry

**Implementation:**
```go
// Update looksLikePath to use language registry
func looksLikePath(s string) bool {
    if strings.Contains(s, "/") {
        return true
    }
    
    // Check against all known language extensions
    for _, info := range languageRegistry.languages {
        for _, ext := range info.Extensions {
            if strings.HasSuffix(s, ext) {
                return true
            }
        }
    }
    
    return false
}
```

### 3. Syntax Highlighting in Output

**Integration Point:** When displaying code blocks in terminal

**Implementation:**
```go
// In terminal.go or helptext.go
func formatCodeBlockForDisplay(codeBlock CodeBlock, lang string) string {
    if highlighter := languageRegistry.GetHighlighter(lang); highlighter != nil {
        return highlighter.Highlight(codeBlock.Content, lang)
    }
    return codeBlock.Content
}
```

### 4. Auto-Formatting on File Write

**Integration Point:** `write_file` tool in `builtin_tools.go`

The write path differs depending on formatter mode:

- **PipeFormatter**: format before writing. Content flows through the formatter
  in memory; the file is only written if formatting succeeds (or is skipped on
  error). Works with safe mode on.
- **FileFormatter**: write first, then invoke the formatter on the resulting
  file path. The formatter rewrites the file in place. Harvey re-reads the file
  to confirm the result. **Requires safe mode off**; the formatter executable is
  validated against `allowed_commands` before invocation, the same as `/run`.

In both modes, the original content is preserved on formatter error — formatting
failure is logged but does not cause `write_file` to fail.

```go
// In builtin_tools.go, write_file handler
func(ctx context.Context, args map[string]any) (string, error) {
    // ... existing validation code ...

    p := args["path"].(string)
    content := args["content"].(string)

    if a.Config.AutoFormat {
        ext := filepath.Ext(p)
        lang := languageRegistry.DetectFromExtension(ext)
        if lang != "" {
            if formatter := languageRegistry.GetFormatter(lang); formatter != nil {
                switch formatter.Mode() {
                case PipeFormatter:
                    // Format in memory before writing.
                    if formatted, err := formatter.Format(content, p); err == nil {
                        content = formatted
                    } else {
                        debugLog("auto-format skipped for %s: %v", p, err)
                    }
                    // Fall through to normal write with (possibly) formatted content.

                case FileFormatter:
                    // Write the file first, then run the formatter on it.
                    if err := writeWorkspaceFile(p, content); err != nil {
                        return "", err
                    }
                    if !a.Config.SafeMode {
                        if _, err := formatter.Format(content, p); err != nil {
                            debugLog("file-mode auto-format failed for %s: %v", p, err)
                        }
                    } else {
                        debugLog("auto-format skipped for %s: requires safe mode off", p)
                    }
                    return formatWriteResult(p), nil
                }
            }
        }
    }

    // ... normal write with content ...
}
```

---

## Configuration

### YAML Configuration Extensions

```yaml
# agents/harvey.yaml

# Existing configuration...

language:
  # Enable auto-formatting on file write
  auto_format: true
  
  # Per-language settings
  languages:
    c:
      enabled: true
      formatter: "clang-format"  # or "uncrustify", "astyle"
      formatter_args: ["-style=llvm"]
      chunking: "function"      # or "file", "balanced"
      
    pascal:
      enabled: true
      formatter: "pascal-formatter"
      
    oberon:
      enabled: true
      # No external formatter available, use built-in
      
    lisp:
      enabled: true
      formatter: "sly"  # or "cl-formatting"
      
    basic:
      enabled: true
      # No external formatter, use built-in
```

### Command-Line Options

```bash
# Enable/disable auto-formatting
--auto-format        Enable auto-formatting on file write
--no-auto-format     Disable auto-formatting

# Configure formatter for a language
harvey> /config set language.c.formatter clang-format
harvey> /config set language.c.formatter_args "-style=llvm"
```

---

## File Modifications Summary

### Modified Files

| File | Changes |
|------|---------|
| `commands.go` | Add language registry, update `looksLikePath`, update `ragIngestFile` |
| `config.go` | Add `LanguageConfig` struct, add language settings to YAML parsing |
| `builtin_tools.go` | Add auto-formatting in `write_file` handler |
| `terminal.go` | Add syntax highlighting for code block display |
| `codeblock.go` | Extend to support language metadata |

### New Files

| File | Purpose |
|------|---------|
| `language_registry.go` | Language registry and core interfaces |
| `language_detector.go` | Language detection implementations |
| `code_chunkers.go` | All language-specific chunker implementations |
| `doc_extractors.go` | All language-specific documentation extractors |
| `code_formatters.go` | All language-specific formatter implementations |
| `syntax_highlighters.go` | All language-specific syntax highlighters |
| `programming_language_support_design.md` | This document |
| `programming_language_support_plan.md` | Implementation plan |

---

## Testing Strategy

### Unit Tests

1. **Language Detection Tests**
   - Test detection by extension
   - Test detection by content
   - Test edge cases (files without extensions, ambiguous cases)

2. **Chunker Tests**
   - Test each language's chunker with sample files
   - Verify chunk boundaries respect language syntax
   - Verify symbol extraction
   - Verify documentation extraction

3. **Formatter Tests**
   - Test formatting preserves semantics
   - Test error handling for malformed code

4. **Highlighter Tests**
   - Test ANSI color output
   - Test with various terminal types

### Integration Tests

1. **RAG Ingestion Tests**
   - Verify code-aware chunks improve retrieval quality
   - Benchmark against generic chunking

2. **File Write Tests**
   - Test auto-formatting on write
   - Test permission handling

3. **End-to-End Tests**
   - Ingest sample codebase in each language
   - Query for specific functions/procedures
   - Verify correct results

---

## Performance Considerations

### Memory
- Language handlers are registered once at startup (not per-file)
- Each file processing uses minimal additional memory
- Enriched chunks use ~20% more memory than plain chunks

### CPU
- Chunking is O(n) where n = file size
- Language-specific chunkers add ~10-20% overhead vs. generic chunking
- Formatters are only invoked when auto-format is enabled

### Startup Time
- Language registry initialization: < 10ms
- No significant impact on Harvey startup

---

## Backward Compatibility

### Existing Behavior Preserved
1. Generic chunking remains the fallback
2. All existing file extensions continue to work
3. Existing RAG stores continue to work; `IngestEnriched` falls back gracefully
   when the schema columns are absent (migration adds them lazily on first use)
4. No changes to session file format

### Migration Path
1. New features are opt-in via configuration
2. The `RagStore` schema gains new columns (`start_line`, `start_col`,
   `end_line`, `end_col`, `chunk_type`, `symbols`, `docs`) added via `ALTER TABLE`
   on first use — no manual migration required for existing stores
3. Previously ingested files retain plain chunks; re-ingest to gain metadata

---

## Security Considerations

### Code Execution — Formatter Modes

**PipeFormatter** (stdin/stdout):
- Invoked via `exec.Command(executable, args...)` — no shell, no injection path
- Content is passed via stdin pipe; no files written by the formatter
- Works with safe mode on
- All child-process environment filtering applies (API keys stripped)
- Formatter executable must appear in `allowed_commands` when safe mode is on

**FileFormatter** (file path):
- Harvey writes the target file first, then invokes the formatter on it
- The formatter rewrites the file in place; Harvey re-reads to confirm
- **Requires safe mode off** — the user has explicitly disabled command restrictions
- Formatter executable is validated against `allowed_commands` before invocation,
  identical to how `/run` validates commands
- Formatters cannot receive paths outside the workspace root (enforced by the
  same `resolveWorkspacePath` check used by all file tools)

### Data Handling
- Source code content is not sent to external services
- All chunking and embedding happens locally
- Binary files are rejected before chunking (silent skip + debug log)

### Syntax Highlighting
- Before applying ANSI color codes, the highlighter must strip any existing ANSI
  escape sequences from input content (e.g., sequences embedded in string
  literals). Failure to do so can corrupt the terminal display.

### Input Size Limits
- Chunkers must enforce a maximum file size consistent with `maxInputContent`
  (10 MiB). Files exceeding the limit are skipped with a warning, not errored,
  so a large file does not halt a batch ingest.

### Registry Thread Safety
- The registry is populated once during `initLanguages()`, called from
  `NewAgent()` before any goroutines that invoke RAG ingestion are started
- After initialization the registry is read-only; no mutex is needed for reads
- If future code modifies the registry after startup, a `sync.RWMutex` must
  be added to `LanguageRegistry`

---

## Future Extensions

1. **Additional Languages**
   - Java, C#, Ruby, PHP, Swift, Kotlin
   - Configuration languages: JSON Schema, OpenAPI
   - Markup: XML, SGML

2. **Advanced Features**
   - Cross-reference indexing (find all uses of symbol)
   - Semantic code search (find functions with specific signatures)
   - AST-based analysis for deeper understanding
   - Refactoring support (rename symbol across files)

3. **Integration with External Tools**
   - Language Server Protocol (LSP) integration
   - Static analysis tools
   - Test framework integration

4. **Code Intelligence**
   - Code completion using RAG
   - Automatic documentation generation
   - Bug detection and suggestions

---

## Glossary

| Term | Definition |
|------|-----------|
| **Code-aware chunking** | Splitting source code at language-specific boundaries (functions, classes) rather than arbitrary character limits |
| **Enriched chunk** | A code chunk with additional metadata (line numbers, symbols, documentation) |
| **Language registry** | Central catalog of all supported languages and their handlers |
| **RAG** | Retrieval-Augmented Generation - Harvey's system for grounding responses in ingested documents |
| **LSP** | Language Server Protocol - standard protocol for IDE-like features |

---

## References

1. [Using_RAGs_with_Harvey.md](Using_RAGs_with_Harvey.md) - Current RAG documentation
2. [DECISIONS.md](DECISIONS.md) - Harvey's decision log
3. [ARCHITECTURE.md](ARCHITECTURE.md) - Harvey's architecture overview
4. [Tree-sitter](https://tree-sitter.github.io/tree-sitter/) - Parser generator tool for syntax-aware chunking
5. [LSP Specification](https://microsoft.com/en-us/language-server-protocol/) - Language Server Protocol

---

*This document is a living design specification. Updates should be made as the implementation progresses and as new requirements emerge.*
