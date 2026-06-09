# Programming Language Support Implementation Plan

## Overview

This document outlines the phased implementation plan for comprehensive programming language support in Harvey, based on the design specified in [programming_language_support_design.md](programming_language_support_design.md).

**Status:** Active  
**Created:** 2026-06-09  
**Related Documents:**
- [programming_language_support_design.md](programming_language_support_design.md)
- [DECISIONS.md](DECISIONS.md)
- [Using_RAGs_with_Harvey.md](Using_RAGs_with_Harvey.md)

---

## Quick Fix Applied

**Date:** 2026-06-09  
**File:** `commands.go`  
**Function:** `looksLikePath` (lines 3463-3472)

**Change:** Added missing programming language extensions to the `knownExts` slice:
- `.c`, `.cpp`, `.h`, `.hpp` (C/C++)
- `.pas` (Pascal)
- `.Mod`, `.obn` (Oberon)
- `.lisp` (Lisp)
- `.bas` (Basic)

**Impact:** Tagged code blocks like ````c:program.c` or ````pascal:module.pas` are now correctly recognized as file paths, enabling auto-write functionality for these languages.

---

## Implementation Phases

The implementation is divided into **6 phases** with clear milestones, deliverables, and success criteria. Each phase builds on the previous one and includes testing and documentation.

---

## Phase 1: Language Registry & Core Infrastructure

**Duration:** 1-2 weeks  
**Priority:** High  
**Dependencies:** None (foundational)

### Objectives
- Create the central language registry
- Implement core interfaces (LanguageDetector, CodeChunker, DocExtractor, CodeFormatter, SyntaxHighlighter)
- Register all supported languages with basic metadata
- Update `looksLikePath` to use the registry

### Tasks

#### Task 1.1: Create Core Types and Interfaces
- [x] Create `language_registry.go` with `LanguageRegistry` struct
- [x] Define `LanguageInfo` struct with metadata
- [x] Define all core interfaces:
  - [x] `LanguageDetector`
  - [x] `CodeChunker`
  - [x] `DocExtractor`
  - [x] `CodeFormatter`
  - [x] `SyntaxHighlighter`
- [x] Define supporting types:
  - [x] `EnrichedChunk` (with StartCol/EndCol for source re-location)
  - [x] `DocumentationBlock`
  - [x] `FormatIssue`
  - [x] `HighlightStyle`
  - [x] `FormatterMode` (PipeFormatter / FileFormatter)

**File:** `language_registry.go` (new)

#### Task 1.2: Implement Language Registry
- [x] Implement `NewLanguageRegistry()` constructor
- [x] Implement `RegisterLanguage()` method
- [x] Implement `GetChunker()`, `GetExtractor()`, `GetFormatter()`, `GetHighlighter()`, `GetDetector()` methods
- [x] Implement `DetectFromExtension()` (exact then case-insensitive fallback)
- [x] Implement `HasExtension()` (case-insensitive membership test)
- [x] Implement `GetLanguageInfo()` and `LanguageIDs()`
- [x] Registry is read-only after init(); no mutex required for reads

**File:** `language_registry.go`

#### Task 1.3: Define Language Metadata
- [x] Define `LanguageInfo` for all 21 supported languages:
  - [x] Go, TypeScript, JavaScript, Python, Rust
  - [x] C, C++
  - [x] Pascal
  - [x] Oberon — `.Mod` (exact), `.obn`; `.mod` lowercase maps to Go (first-wins);
        `HasExtension` is case-insensitive so both `.mod` and `.Mod` return true
  - [x] Lisp
  - [x] Basic
  - [x] JSON, Markdown, Text, CSS, YAML, TOML, SQL, HTML, Shell, Environment
- [x] Set capabilities flags (HasChunking, HasExtraction, etc.)

**File:** `language_registry.go`

#### Task 1.4: Initialize Registry at Startup
- [x] Create `initLanguages(r *LanguageRegistry)` function
- [x] Register all 21 languages with nil handlers (Phase 1)
- [x] Initialize `globalRegistry` via package `init()` — runs before any test
      or main function, no change to `NewAgent()` required

**File:** `language_registry.go`

#### Task 1.5: Update looksLikePath Function
- [x] ✅ DONE: Added missing extensions (2026-06-09)
- [x] Refactored to use `registryHasExt()` (delegates to `globalRegistry.HasExtension`)
- [x] All existing tests pass; new extension tests added

**File:** `commands.go` (modified)

#### Task 1.6: Add Unit Tests for Registry
- [x] Test language detection by extension (all 21 languages)
- [x] Test case-insensitive HasExtension
- [x] Test exact vs lowercase precedence (.Mod → oberon, .mod → go)
- [x] Test retrieval of handlers (all nil in Phase 1)
- [x] Test GetLanguageInfo known/unknown
- [x] Test RegisterLanguage overwrite
- [x] Test looksLikePath for all new extensions (C, Pascal, Oberon, Lisp, Basic)
- [x] Race-detector run passes

**File:** `language_registry_test.go` (new)

### Deliverables
1. ✅ `language_registry.go` — core registry, all types and interfaces
2. ✅ `language_registry_test.go` — 35 tests, 0 failures
3. ✅ Updated `commands.go` — `looksLikePath` uses registry
4. `harvey.go` — no change needed (registry uses `init()`)

### Success Criteria
- [x] All 21 language metadata entries defined
- [x] Registry correctly detects all languages by extension
- [x] Registry correctly returns nil for unregistered languages
- [x] `looksLikePath` uses registry and passes all existing tests
- [x] Unit test coverage > 90% for registry
- [x] `go test -race` passes

---

## Phase 2: Basic Language Detection

**Duration:** 1 week  
**Priority:** High  
**Dependencies:** Phase 1 complete

### Objectives
- Implement basic language detectors for all supported languages
- Ensure accurate detection by extension
- Add content-based detection as fallback

### Tasks

#### Task 2.1: Implement LanguageDetector Interface
- [x] Create `language_detector.go`
- [x] Implement `LanguageDetector` for each language group:
  - [x] `ExtensionDetector` - Base detector using file extension
  - [x] `ContentDetector` - Fallback detector using file content
  - [x] `CombinedDetector` - Combines both approaches

**File:** `language_detector.go` (new)

#### Task 2.2: Create Language-Specific Detectors
- [x] Implement detectors for:
  - [x] C/C++ (`.c`, `.h`, `.cpp`, `.hpp`, `.cc`, `.cxx`, `.hh`)
  - [x] Pascal (`.pas`, `.p`)
  - [x] Oberon (`.Mod`, `.obn`)
  - [x] Lisp (`.lisp`, `.lsp`, `.cl`, `.el`)
  - [x] Basic (`.bas`, `.bi`)
  - [x] Existing languages (Go, TypeScript, JavaScript, Python, Rust, etc.)
  - [x] All 21 languages register `CombinedDetector` in `initLanguages`

**File:** `language_detector.go`

#### Task 2.3: Add Shebang Detection
- [x] Detect language from shebang lines (e.g., `#!/usr/bin/env python3`)
- [x] Handle common shebang patterns: python3, python2, /python, node, bash, /sh, clisp, sbcl, /tcc
- [x] Added to `ContentDetector.Detect` and `LanguageRegistry.DetectLanguage` (0.9 confidence)

**File:** `language_detector.go`

#### Task 2.4: Add Magic Number Detection
- [x] Detect binary files and reject them (`isBinary` null-byte check in first 512 bytes)
- [x] `isTextContent` wraps `!isBinary` for API clarity
- [x] `DetectLanguage` returns ("", 0.0) immediately for binary content
- [x] Keyword detection limited to first `maxKeywordScan` (4096) bytes

**File:** `language_detector.go`

#### Task 2.5: Add Unit Tests
- [x] Test `isTextContent` (text, binary, empty)
- [x] Test `detectShebang` (python3/2/abs, bash/sh, node, clisp/sbcl, no-shebang, empty, no-newline)
- [x] Test `detectKeywords` (go, oberon, pascal, lisp, c, cpp, basic, html, sql, no-match, large-file truncation)
- [x] Test `ExtensionDetector` (exact, no-match, case-insensitive, DetectFromExtension)
- [x] Test `ContentDetector` (shebang match, wrong-lang, keyword match, binary, DetectFromExtension always false)
- [x] Test `CombinedDetector` (extension priority, content fallback, no-match)
- [x] Test `LanguageRegistry.DetectLanguage` (extension, .Mod, shebang fallback, keyword fallback, binary, unknown ext, unregistered shebang, all detectors registered)

**File:** `language_detector_test.go` (new)

### Deliverables
1. ✅ `language_detector.go` — `ExtensionDetector`, `ContentDetector`, `CombinedDetector`, `detectShebang`, `detectKeywords`, `isTextContent`
2. ✅ `language_detector_test.go` — 45 tests, 0 failures
3. ✅ Updated `language_registry.go` — `DetectLanguage` method; all 21 languages wired with `CombinedDetector`

### Success Criteria
- [x] All 21 languages detected correctly by extension
- [x] Content-based detection works for shebang lines
- [x] Binary files correctly rejected
- [x] Detection accuracy > 99% for known file types
- [x] Unit test coverage > 95%
- [x] `go test -race` passes

---

## Phase 3: Code-Aware Chunking for RAG

**Duration:** 2-3 weeks  
**Priority:** High  
**Dependencies:** Phases 1-2 complete

### Objectives
- Implement language-specific chunkers for C, C++, Pascal, Oberon, Lisp, Basic
- Integrate chunkers with RAG ingestion pipeline
- Preserve code structure in chunks (functions, procedures, classes)

### Tasks

#### Task 3.1: Implement CodeChunker Interface
- [x] Create `code_chunkers.go`
- [x] `CodeChunker` interface already defined in `language_registry.go`
- [x] `EnrichedChunk` already defined with `StartLine`, `StartCol`, `EndLine`, `EndCol`
- [x] `findLineCol` helper: returns 1-indexed line + 0-indexed byte-offset from line start
- [x] `makeChunk` helper: byte-range → `EnrichedChunk` with trimmed content and precise location

**File:** `code_chunkers.go` (new)

#### Task 3.2: Implement C/C++ Chunker
- [x] `CChunker` struct; shared for "c" and "cpp" via `NewCChunker(langID)`
- [x] `findCCutPoints`: byte-by-byte state machine tracking `//', `/* */`, `"..."`, `'...'`, `{}`
- [x] Splits at `}` returning depth to 0 and at blank lines at depth 0
- [x] Includes/declarations in their own chunk; function precedes its block in same chunk
- [x] `classifyC`: extracts first `identifier(` pattern that is not a keyword → "function";
      checks for `STRUCT`/`ENUM`/`TYPEDEF`/`CLASS` → "type"; else "code"
- [x] `StartLine`/`StartCol`/`EndLine`/`EndCol` via `makeChunk` + `findLineCol`
- [x] Strings and comments with braces correctly ignored

**File:** `code_chunkers.go`

#### Task 3.3: Implement Pascal Chunker
- [x] `PascalChunker` wraps `chunkBeginEnd` with `isPascalProcStart` + `extractPascalSymbol`
- [x] PROCEDURE/FUNCTION blocks: line-by-line BEGIN/END depth tracking
- [x] Nested BEGIN/END handled: depth only returns to 0 at outermost END;
- [x] CONST/VAR/USES/PROGRAM sections accumulate in `pending` → "code" chunks
- [x] `extractPascalSymbol` strips PROCEDURE/FUNCTION keyword, extracts first identifier
- [x] `stripBlockLineComment` removes `//`, `{ }`, `(* *)` from lines before keyword scan

**File:** `code_chunkers.go`

#### Task 3.4: Implement Oberon Chunker
- [x] `OberonChunker` wraps `chunkBeginEnd` with `isOberonProcStart` + `extractOberonSymbol`
- [x] MODULE header and IMPORT/TYPE/VAR sections → "code" chunks via `pending`
- [x] PROCEDURE blocks tracked via BEGIN/END depth
- [x] `extractOberonSymbol` strips `PROCEDURE` prefix and export `*` marker
- [x] Known approximation: nested `(* (* *) *)` comments treated as non-nested

**File:** `code_chunkers.go`

#### Task 3.5: Implement Lisp Chunker
- [x] `LispChunker` byte-by-byte paren-depth tracker
- [x] State: normal, `;` line comment, `"..."` string
- [x] Each depth-0 `(...)` top-level form is one chunk
- [x] `findLineCol` used for StartLine/StartCol via `makeChunk`
- [x] `classifyLisp`: matches `defun`/`defmacro` → "function"; `defvar` → "variable";
      `defclass`/`defstruct` → "type"
- [x] Extracts symbol name as second token of the top-level form

**File:** `code_chunkers.go`

#### Task 3.6: Implement Basic Chunker
- [x] `BasicChunker` line-by-line SUB/FUNCTION scanner
- [x] `SUB name(...)` / `FUNCTION name(...)` → start of function chunk
- [x] `END SUB` / `END FUNCTION` → closes chunk; emits "function" ChunkType
- [x] Lines between subs accumulate in `pending` → "code" chunks (split at blank lines)
- [x] `extractBasicSymbol` extracts name token after SUB/FUNCTION keyword

**File:** `code_chunkers.go`

#### Task 3.7: Integrate with RAG Ingestion
- [x] `ragIngestFile()` now: read → binary check → extension detect → chunker lookup
- [x] `isTextContent` check: binary files return (0, nil) silently
- [x] `filepath.Ext(path)` used directly (case-preserving) for `.Mod`/`.mod`
- [x] Language-aware chunks via `globalRegistry.GetChunker(langID)`
- [x] Fallback to `ragChunk()` wrapped as `[]EnrichedChunk` with zero location fields
- [x] `store.Ingest()` replaced with `store.IngestEnriched()`

**File:** `commands.go` (modified)

#### Task 3.8: Update RagStore for Enriched Chunks (Required)
- [x] `IngestEnriched(source string, chunks []EnrichedChunk, embedder Embedder) error`
- [x] `enrichedAlterStmts`: 7 lazy `ALTER TABLE … ADD COLUMN` statements run at call time
- [x] Existing stores without new columns continue to work (defaults to 0/"")
- [x] `symbols` stored as comma-joined string via `strings.Join(chunk.Symbols, ",")`

**File:** `rag_support.go` (modified)

#### Task 3.9: Add Unit Tests
- [x] `findLineCol`: first line, second line, at newline, offset 0, clamp
- [x] `CChunker`: empty, single function, two functions, includes section,
      string-with-brace, line-comment-with-brace, block-comment-not-split, location metadata
- [x] `PascalChunker`: procedure, function symbol, header+procedure, nested BEGIN/END
- [x] `OberonChunker`: procedure, module header as code, language()
- [x] `LispChunker`: defun, multiple forms, line comment, nested parens, defmacro type
- [x] `BasicChunker`: SUB, FUNCTION, header+sub, language()
- [x] `IngestEnriched`: basic, model mismatch, lazy migration, symbols as CSV
- [x] `ragIngestFile`: binary skip, C file uses chunker, markdown fallback
- [x] Registry: all 6 chunkers registered in freshRegistry + globalRegistry

**File:** `code_chunkers_test.go` (new)

### Deliverables
1. ✅ `code_chunkers.go` — `CChunker`, `PascalChunker`, `OberonChunker`, `LispChunker`, `BasicChunker`; helpers `findLineCol`, `makeChunk`; `SetChunker`/`initChunkers`
2. ✅ `code_chunkers_test.go` — 42 tests, 0 failures
3. ✅ Updated `commands.go` — `ragIngestFile` uses code-aware chunking + binary skip
4. ✅ Updated `rag_support.go` — `IngestEnriched` + lazy schema migration
5. ✅ Updated `language_registry.go` — `init()` calls `initChunkers`

### Success Criteria
- [x] All 6 language chunkers implemented (C, C++, Pascal, Oberon, Lisp, Basic)
- [x] Functions/procedures not split across chunks
- [x] Every chunk records `StartLine`/`StartCol`/`EndLine`/`EndCol`
- [x] `IngestEnriched` stores location metadata in SQLite
- [x] Existing RAG stores without the new columns continue to work
- [x] Binary files skipped silently (no error returned)
- [x] Symbol extraction accuracy > 90%
- [x] RAG ingestion works with code-aware chunks
- [x] Generic chunking still works as fallback
- [x] `go test -race` passes

---

## Phase 4: Documentation Extraction

**Duration:** 1-2 weeks  
**Priority:** Medium  
**Dependencies:** Phases 1-3 complete

### Objectives
- Extract comments and docstrings from source code
- Associate documentation with code chunks
- Improve embedding quality for code files

### Tasks

#### Task 4.1: Implement DocExtractor Interface
- [x] Create `doc_extractors.go`
- [x] Implement `DocExtractor` interface (already in `language_registry.go` from Phase 1)
- [x] Implement `DocumentationBlock` struct (already in `language_registry.go` from Phase 1)

**File:** `doc_extractors.go` (new)

#### Task 4.2: Implement C/C++ DocExtractor
- [x] Create `CDocExtractor` struct
- [x] Extract line comments (`//`)
- [x] Extract block comments (`/* */`)
- [x] Extract Doxygen-style comments (`/** */` and `/*! */`)
- [x] Associate comments with following code
- [x] Extract function documentation (symbol via `classifyC`)
- [x] Fixed `classifyC` to strip leading `*`/`&` from function name (pointer return types)

**File:** `doc_extractors.go`

#### Task 4.3: Implement Pascal DocExtractor
- [x] Create `PascalDocExtractor` struct
- [x] Extract line comments (`//`)
- [x] Extract block comments (`(* *)`, `{ }`)
- [x] Associate comments with procedures/functions
- [x] Skip `{$...}` compiler directives

**File:** `doc_extractors.go`

#### Task 4.4: Implement Oberon DocExtractor
- [x] Create `OberonDocExtractor` struct
- [x] Extract block comments (`(* *)`)
- [x] Associate comments with modules/procedures
- [x] Note: nested `(* (* *) *)` treated as non-nested (same as OberonChunker)

**File:** `doc_extractors.go`

#### Task 4.5: Implement Lisp DocExtractor
- [x] Create `LispDocExtractor` struct
- [x] Extract line comments (`;`, `;;`, `;;;`)
- [x] Extract block comments (`#| |#`)
- [x] Extract docstrings from DEFUN/DEFMACRO/DEFMETHOD (heuristic: first `"` on line after def header)
- [x] Associate documentation with symbols via `classifyLisp`

**File:** `doc_extractors.go`

#### Task 4.6: Implement Basic DocExtractor
- [x] Create `BasicDocExtractor` struct
- [x] Extract REM comments (case-insensitive)
- [x] Extract single-quote (`'`) comments
- [x] Associate with SUB/FUNCTION via `extractBasicSymbol`

**File:** `doc_extractors.go`

#### Task 4.7: Integrate with Chunkers
- [x] `ragIngestFile` runs `extractor.ExtractSymbols` after chunking
- [x] Chunks whose `Symbols[0]` matches an extracted symbol get their `Docs` populated
- [x] Integration is at the ingest level (not inside chunkers), keeping them decoupled

**File:** `commands.go` (modified)

#### Task 4.8: Add Unit Tests
- [x] Test doc extraction for each language (all 5 extractors)
- [x] Test comment detection for each comment style
- [x] Test symbol association (preceding comment → function/symbol name)
- [x] Test edge cases: compiler directives skipped, free-standing comments, multiline blocks
- [x] Test `lispStringContent` helper
- [x] Test registry wiring (6 extractors in globalRegistry)
- [x] Integration test: `ragIngestFile` populates Docs field in SQLite store

**File:** `doc_extractors_test.go` (new)

### Deliverables
1. ✅ `doc_extractors.go` — `CDocExtractor`, `PascalDocExtractor`, `OberonDocExtractor`, `LispDocExtractor`, `BasicDocExtractor`; `docsToSymbolMap`; `lispStringContent`; `SetExtractor`/`initExtractors`
2. ✅ `doc_extractors_test.go` — 34 tests, 0 failures
3. ✅ Updated `commands.go` — `ragIngestFile` uses doc extractors to populate `Docs` field
4. ✅ Updated `language_registry.go` — `init()` calls `initExtractors`
5. ✅ Updated `code_chunkers.go` — fixed `flushCurrent` nil-access bug; fixed `extractPascalSymbol`/`extractOberonSymbol` leading-whitespace; fixed `classifyC` pointer return type

### Success Criteria
- [x] All 5 language extractors implemented (C/C++ shared, Pascal, Oberon, Lisp, Basic)
- [x] > 90% of comments extracted correctly
- [x] Documentation correctly associated with code
- [x] Unit test coverage > 90%
- [x] `go test -race` passes

---

## Phase 5: Syntax Highlighting

**Duration:** 1 week  
**Priority:** Medium  
**Dependencies:** Phases 1-2 complete

### Objectives
- Add color syntax highlighting for code blocks in terminal output
- Support all programming languages
- Use ANSI color codes

### Tasks

#### Task 5.1: Implement SyntaxHighlighter Interface
- [x] Create `syntax_highlighters.go`
- [x] Implement `SyntaxHighlighter` interface
- [x] Implement `HighlightStyle` struct
- [x] Define ANSI color constants

**File:** `syntax_highlighters.go` (new)

#### Task 5.2: Create Base Highlighter
- [x] Implement `TerminalHighlighter` with single byte-level state-machine scanner
- [x] Handle ANSI color escaping (reset after each colored token)
- [x] `langHighlighter` struct parameterises all tokenisation per language

**File:** `syntax_highlighters.go`

#### Task 5.3: Implement Language-Specific Highlighters
- [x] Create highlighters for:
  - [x] C/C++
  - [x] Pascal (incl. `{$...}` compiler-directive guard)
  - [x] Oberon
  - [x] Lisp (`#|...|#` block, `;` line)
  - [x] Basic (`REM` + `'` comments)
  - [x] Go, Python, JavaScript, TypeScript, Rust, Shell, SQL
- [x] Language keyword sets; byte-level tokeniser handles strings, comments, numbers

**File:** `syntax_highlighters.go`

#### Task 5.4: Integrate with Terminal Output
- [x] `highlightCodeBlocks(text)` post-processes full response text
- [x] `terminal.go` applies highlighting before `fmt.Fprint(out, displayText)` (line ~875)
- [x] Gated by `a.Config.SyntaxHighlight`; `buf.String()` unchanged for history/recording

**Files:** `terminal.go` (modified), `syntax_highlighters.go` (new)

#### Task 5.5: Add Configuration
- [x] `Config.SyntaxHighlight bool` field (default `true`)
- [x] `syntax_highlight` YAML field (`*bool`, `omitempty`) in `harveyYAML`
- [x] `LoadHarveyYAML` and `SaveMemoryConfig` load/save the field

**File:** `config.go` (modified)

#### Task 5.6: Add Unit Tests
- [x] 30 tests covering each language, comment styles, string literals, numbers
- [x] `highlightCodeBlocks` round-trip, fence preservation, multi-block, aliases
- [x] Registry wiring: all 13 languages registered in `globalRegistry`

**File:** `syntax_highlighters_test.go` (new)

### Deliverables
1. ✅ `syntax_highlighters.go` — `TerminalHighlighter`, 13 language specs, `highlightCodeBlocks`, `initHighlighters`
2. ✅ `syntax_highlighters_test.go` — 30 tests; all pass
3. ✅ `terminal.go` — `highlightCodeBlocks` applied before display
4. ✅ `config.go` — `SyntaxHighlight bool` with YAML load/save
5. ✅ `language_registry.go` — `init()` calls `initHighlighters`

### Success Criteria
- [x] 13 languages have syntax highlighting (C, C++, Pascal, Oberon, Lisp, Basic, Go, Python, JS, TS, Rust, Shell, SQL)
- [x] Correct ANSI color codes generated (keywords bold-blue, strings green, comments dim, numbers yellow)
- [x] Unknown languages pass through unchanged (graceful degradation)
- [x] Performance: byte-level scanner with no regex; sub-millisecond for typical blocks
- [x] All tests pass (`go test ./...`)

---

## Phase 6: Code Formatting & Final Integration

**Duration:** 1-2 weeks  
**Priority:** Medium  
**Dependencies:** Phases 1-2 complete

### Objectives
- Add automatic code formatting on file write
- Support both pipe-mode (stdin/stdout) and file-mode (path-in-place) formatters
- Implement built-in formatters for languages without external tools

### Tasks

#### Task 6.1: Implement CodeFormatter Interface
- [x] `CodeFormatter`, `FormatterMode`, `FormatIssue` defined in Phase 1 (`language_registry.go`)
- [x] `code_formatters.go` created with all formatter structs

**File:** `code_formatters.go` (new)

#### Task 6.2a: Implement Pipe-Mode External Formatters
- [x] `PipeExternalFormatter`: `exec.CommandContext` (30 s default timeout), stdin→stdout, `filterCommandEnvironment()`, `{{filepath}}` placeholder interpolation, graceful degradation when tool absent
- [x] Wired: Go (`gofmt`), C/C++ (`clang-format -`), Python (`black -`), Rust (`rustfmt`), JS/TS (`prettier --stdin-filepath {{filepath}}`)

**File:** `code_formatters.go`

#### Task 6.2b: Implement File-Mode External Formatters
- [x] `FileExternalFormatter`: writes file first (done by caller), runs tool on path, re-reads result, `filterCommandEnvironment()`, graceful degradation; safe-mode block enforced at call site
- [x] `{{filepath}}` placeholder interpolated in args

**File:** `code_formatters.go`

#### Task 6.3: Implement Built-in Formatters
- [x] `BuiltinFormatter` wraps a pure-Go `func(string) string`
- [x] `normaliseText`: strips trailing whitespace, normalises CRLF→LF, ensures single trailing newline
- [x] Pascal, Oberon, Basic all use `normaliseText` (safe: no indentation mutation)

**File:** `code_formatters.go`

#### Task 6.4: Integrate with write_file Tool
- [x] `applyAutoFormat(a, relPath, original)` helper: detects language, looks up formatter, skips file-mode in safe mode, rewrites file on change, returns status note
- [x] `write_file` calls `applyAutoFormat` when `Config.AutoFormat` is true
- [x] Errors silently suppressed — original write always succeeds

**File:** `builtin_tools.go` (modified)

#### Task 6.5: Add Configuration
- [x] `Config.AutoFormat bool` (default `true`)
- [x] `auto_format *bool` YAML field in `harveyYAML` with load/save in `LoadHarveyYAML` / `SaveMemoryConfig`
- [x] `/format FILE [FILE...]` command added to `commands.go`

**Files:** `config.go` (modified), `commands.go` (modified)

#### Task 6.6: Add Unit Tests
- [x] `normaliseText`: trailing whitespace, CRLF, trailing newlines, idempotency
- [x] `BuiltinFormatter`: format, check, language, mode
- [x] `PipeExternalFormatter`: missing-exe passthrough, `cat` identity, check
- [x] `FileExternalFormatter`: missing-exe passthrough, `{{filepath}}` script interpolation
- [x] Registry: `initFormatters` wires all 10 languages; `SetFormatter` round-trip
- [x] `applyAutoFormat`: Pascal built-in, already-formatted, unknown ext, safe-mode block
- [x] `cmdFormat`: no-workspace, no-args, already-formatted, formats-file, unknown-ext

**File:** `code_formatters_test.go` (new)

### Deliverables
1. ✅ `code_formatters.go` — `PipeExternalFormatter`, `FileExternalFormatter`, `BuiltinFormatter`, `normaliseText`, `SetFormatter`, `initFormatters`
2. ✅ `code_formatters_test.go` — 33 tests; all pass
3. ✅ `builtin_tools.go` — `applyAutoFormat` wired into `write_file`
4. ✅ `config.go` — `AutoFormat bool` with YAML load/save
5. ✅ `commands.go` — `/format FILE [FILE...]` command
6. ✅ `language_registry.go` — `init()` calls `initFormatters`

### Success Criteria
- [x] 10 languages have formatter support (Go, C, C++, Python, Rust, JS, TS via external pipe; Pascal, Oberon, Basic via built-in)
- [x] Pipe-mode formatters work with safe mode on (no safe-mode check in formatter)
- [x] File-mode formatters correctly refused when safe mode is on (blocked in `applyAutoFormat` and `cmdFormat`)
- [x] All external formatters invoked via `exec.CommandContext` — no shell
- [x] `filterCommandEnvironment()` applied to all formatter subprocesses
- [x] Auto-formatting wired through `write_file` when `AutoFormat=true`
- [x] Formatter errors handled gracefully — original write is never rolled back
- [x] Original content preserved on failure (error path returns early without rewrite)
- [x] All tests pass (`go test ./...`)

---

## Cross-Cutting Tasks

### Testing Infrastructure

#### Task T.1: Create Test Data
- [ ] Create sample files for each language:
  - [ ] C: functions.c, structures.c, preprocessor.c
  - [ ] C++: classes.cpp, templates.cpp
  - [ ] Pascal: procedures.pas, types.pas, units.pas
  - [ ] Oberon: module.Mod, procedures.Mod (and module.mod for POSIX case test)
  - [ ] Lisp: functions.lisp, macros.lisp, classes.lisp
  - [ ] Basic: subroutines.bas, functions.bas
- [ ] Include edge cases (nested structures, large files, special characters)
- [ ] Include files with multi-line comments to verify block extraction
- [ ] Include a binary file with a `.c` extension to test binary rejection

**Directory:** `testdata/language_support/`

#### Task T.2: Integration Tests
- [ ] Test RAG ingestion for each language
- [ ] Test retrieval quality with code queries
- [ ] Test code block path detection
- [ ] Test syntax highlighting
- [ ] Test auto-formatting

**File:** `language_integration_test.go` (new)

#### Task T.3: Benchmark Tests
- [ ] Benchmark chunking performance
- [ ] Benchmark retrieval quality (vs. generic chunking)
- [ ] Benchmark formatting performance
- [ ] Establish baseline metrics

**File:** `language_benchmark_test.go` (new)

### Documentation

#### Task D.1: Update User Documentation
- [ ] Update [Using_RAGs_with_Harvey.md](Using_RAGs_with_Harvey.md)
  - [ ] Add section on language-specific features
  - [ ] Document code-aware chunking
  - [ ] Document syntax highlighting
  - [ ] Document auto-formatting
- [ ] Create examples for each language

**File:** `Using_RAGs_with_Harvey.md` (modified)

#### Task D.2: Create Language-Specific Guides
- [ ] Create `RAG_Language_Support.md` with:
  - [ ] Configuration guide
  - [ ] Best practices for each language
  - [ ] Troubleshooting
  - [ ] Known limitations

**File:** `RAG_Language_Support.md` (new)

#### Task D.3: Update Help Text
- [ ] Update `/help` output with new language features
- [ ] Add language-specific help commands
- [ ] Document new commands (`/format`, etc.)

**File:** `helptext.go` (modified)

#### Task D.4: Update Architecture Documentation
- [ ] Update [ARCHITECTURE.md](ARCHITECTURE.md) with language support components
- [ ] Add component diagrams
- [ ] Document integration points

**File:** `ARCHITECTURE.md` (modified)

---

## Milestone Summary

### Milestone 1: Foundation Complete (Week 2-3)
**Includes:** Phases 1-2  
**Deliverables:**
- Language registry with all 17 languages
- Language detection by extension and content
- Updated `looksLikePath` using registry
- Comprehensive unit tests

**Success Criteria:**
- All languages detected correctly
- Registry functional and tested
- No regressions in existing functionality

---

### Milestone 2: Code-Aware RAG (Week 4-6)
**Includes:** Phase 3  
**Deliverables:**
- Code-aware chunkers for all programming languages
- Integration with RAG ingestion
- Improved retrieval quality for code

**Success Criteria:**
- Code structures preserved in chunks
- Retrieval quality improved (measurable)
- All existing RAG functionality preserved

---

### Milestone 3: Enhanced Experience (Week 7-9)
**Includes:** Phases 4-6  
**Deliverables:**
- Documentation extraction
- Syntax highlighting
- Auto-formatting
- Full integration

**Success Criteria:**
- Documentation extracted and associated with code
- Code blocks colorized in terminal
- Auto-formatting works when enabled
- All features configurable

---

### Milestone 4: Testing & Polish (Week 10)
**Includes:** Cross-cutting tasks  
**Deliverables:**
- Comprehensive test suite
- Updated documentation
- Performance benchmarks
- Bug fixes and polish

**Success Criteria:**
- All tests passing
- Documentation complete
- Performance acceptable
- Ready for release

---

## Resource Requirements

### Human Resources
- **Lead Developer:** 1 FTE (oversight, architecture, critical code)
- **Developers:** 1-2 FTE (implementation)
- **Test Engineer:** 0.5 FTE (test development, QA)
- **Technical Writer:** 0.25 FTE (documentation)

### Time Estimates
| Phase | Duration | Person-Days |
|-------|----------|-------------|
| Phase 1 | 1-2 weeks | 10-20 |
| Phase 2 | 1 week | 5-10 |
| Phase 3 | 2-3 weeks | 15-30 |
| Phase 4 | 1-2 weeks | 10-20 |
| Phase 5 | 1 week | 5-10 |
| Phase 6 | 1-2 weeks | 10-20 |
| Cross-cutting | 2 weeks | 20-30 |
| **Total** | **10-14 weeks** | **75-150** |

### External Dependencies
| Dependency | Purpose | License | Notes |
|------------|---------|---------|-------|
| clang-format | C/C++ formatting | Apache 2.0 | Optional |
| black | Python formatting | MIT | Optional |
| prettier | JS/TS formatting | MIT | Optional |
| rustfmt | Rust formatting | Apache 2.0/MIT | Optional |
| sly | Lisp formatting | MIT | Optional |

---

## Risk Management

### Technical Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Chunker bugs break code across chunks | Medium | High | Extensive testing, fallback to generic chunking |
| Performance regression | Low | Medium | Benchmark before/after, optimize if needed |
| Embedding model limitations | Medium | Medium | Test with multiple models, document limitations |
| Memory usage increase | Medium | Medium | Profile memory, optimize data structures |
| Backward compatibility issues | Low | High | Maintain generic chunking as fallback, migration guide |
| External formatter dependencies | Low | Medium | Use built-in fallbacks, document requirements |

### Schedule Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Scope creep | Medium | Medium | Strict phase definitions, defer nice-to-haves |
| Resource availability | Medium | High | Prioritize critical features, defer optional |
| Testing complexity | Medium | Medium | Automate testing, create good test data |
| Integration issues | Medium | Medium | Early integration testing, continuous integration |

### Quality Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Bugs in production | Medium | High | Comprehensive testing, code reviews |
| Poor user experience | Medium | Medium | User testing, iterate on feedback |
| Incomplete documentation | Medium | Medium | Documentation as part of each task |
| Performance issues | Low | Medium | Performance testing, profiling |

---

## Monitoring & Metrics

### Implementation Metrics
1. **Code Coverage:** Target > 80% for new code
2. **Test Pass Rate:** Target 100% for all tests
3. **Build Success:** All builds must pass
4. **Performance:** No >10% regression in critical paths

### Quality Metrics
1. **RAG Retrieval Quality:**
   - Measure precision/recall for code queries
   - Compare code-aware vs. generic chunking
   - Target: 20%+ improvement over generic

2. **Chunk Quality:**
   - % of functions not split across chunks: Target > 95%
   - % of documentation extracted: Target > 90%

3. **User Satisfaction:**
   - Feedback on syntax highlighting
   - Feedback on auto-formatting
   - Bug reports and feature requests

### Performance Metrics
1. **Startup Time:** < 100ms increase
2. **Chunking Time:** < 100ms per file for typical sizes
3. **Formatting Time:** < 500ms per file for typical sizes
4. **Memory Usage:** < 10MB additional memory

---

## Communication Plan

### Stakeholders
- **Harvey Team:** Core development team
- **Users:** Harvey users interested in programming language support
- **Maintainers:** Repository maintainers

### Communication Channels
1. **GitHub Issues:** Feature tracking, bug reports
2. **Discussions:** Design discussions, feedback
3. **Changelog:** Release notes, new features
4. **Documentation:** Updated docs with new features

### Key Messages
1. **Phase 1 Complete:** "Harvey now has a language registry supporting all file types"
2. **Phase 3 Complete:** "Code-aware RAG chunking improves code search quality"
3. **Phase 5 Complete:** "Syntax highlighting and auto-formatting now available"
4. **Final Release:** "Comprehensive programming language support in Harvey"

---

## Contingency Plans

### If Behind Schedule
1. **Prioritize:** Focus on Phases 1-3 (registry, detection, chunking)
2. **Defer:** Syntax highlighting and formatting can be deferred
3. **Simplify:** Reduce scope (fewer languages, simpler chunkers)
4. **Parallelize:** More developers on independent tasks

### If Quality Issues
1. **Stop:** Halt development, fix critical issues
2. **Isolate:** Identify problematic components
3. **Rollback:** Revert to last known good state if necessary
4. **Test:** Add more tests to prevent regression

### If Resource Constraints
1. **Reduce Scope:** Implement for fewer languages initially
2. **Simplify:** Use simpler implementations (regex-based instead of parser)
3. **Externalize:** Defer some formatters to external tools
4. **Community:** Seek community contributions

---

## Approval & Sign-off

### Reviewers
- [ ] **Architecture Review:** @rsdoiel (Harvey maintainer)
- [ ] **Code Review:** Harvey team members
- [ ] **Testing Review:** QA team
- [ ] **Documentation Review:** Technical writers

### Approval Checklist
- [ ] Design document approved
- [ ] This plan document approved
- [ ] Resource allocation confirmed
- [ ] Timeline agreed
- [ ] Success criteria accepted

---

## Appendix A: File Changes Summary

### New Files
| File | Phase | Size (est.) | Purpose |
|------|-------|-------------|---------|
| `language_registry.go` | 1 | ~500 lines | Language registry and metadata |
| `language_registry_test.go` | 1 | ~300 lines | Registry tests |
| `language_detector.go` | 2 | ~400 lines | Language detection |
| `language_detector_test.go` | 2 | ~250 lines | Detection tests |
| `code_chunkers.go` | 3 | ~800 lines | Code-aware chunkers |
| `code_chunkers_test.go` | 3 | ~500 lines | Chunker tests |
| `doc_extractors.go` | 4 | ~600 lines | Documentation extractors |
| `doc_extractors_test.go` | 4 | ~400 lines | Extractor tests |
| `syntax_highlighters.go` | 5 | ~600 lines | Syntax highlighting |
| `syntax_highlighters_test.go` | 5 | ~400 lines | Highlighter tests |
| `code_formatters.go` | 6 | ~500 lines | Code formatters |
| `code_formatters_test.go` | 6 | ~300 lines | Formatter tests |
| `language_integration_test.go` | T | ~400 lines | Integration tests |
| `language_benchmark_test.go` | T | ~200 lines | Benchmark tests |
| `RAG_Language_Support.md` | D | ~500 lines | User documentation |
| **Total** | | **~7,000 lines** | |

### Modified Files
| File | Phase | Changes | Impact |
|------|-------|---------|--------|
| `commands.go` | 1, 3, 5 | Add registry usage, update chunking, add formatting | Core |
| `config.go` | 5, 6 | Add language settings, formatter config | Core |
| `builtin_tools.go` | 6 | Add auto-formatting to write_file | Core |
| `terminal.go` | 5 | Add syntax highlighting | UI |
| `codeblock.go` | 3 | Extend for language metadata | Core |
| `harvey.go` | 1 | Initialize registry | Core |
| `Using_RAGs_with_Harvey.md` | D | Update with new features | Docs |
| `ARCHITECTURE.md` | D | Update with new components | Docs |
| `helptext.go` | D | Update help text | UI |

---

## Appendix B: Test Data Requirements

### Sample Files Needed
```
testdata/language_support/
├── c/
│   ├── functions.c
│   ├── structures.c
│   ├── preprocessor.c
│   └── complex.c
├── cpp/
│   ├── classes.cpp
│   ├── templates.cpp
│   └── inheritance.cpp
├── pascal/
│   ├── procedures.pas
│   ├── types.pas
│   └── units.pas
├── oberon/
│   ├── module.Mod
│   └── procedures.Mod
├── lisp/
│   ├── functions.lisp
│   ├── macros.lisp
│   └── classes.lisp
├── basic/
│   ├── subroutines.bas
│   └── functions.bas
└── expected/
    ├── c_chunks.json
    ├── pascal_chunks.json
    └── ...
```

### Test File Sizes
- Small: < 100 lines (unit tests)
- Medium: 100-500 lines (integration tests)
- Large: 500-2000 lines (performance tests)

---

## Appendix C: Configuration Examples

### agents/harvey.yaml
```yaml
# Language support configuration
language:
  # Enable auto-formatting on file write
  auto_format: true
  
  # Enable syntax highlighting in terminal
  syntax_highlight: true
  
  # Per-language settings
  languages:
    c:
      enabled: true
      formatter: "clang-format"
      formatter_args: ["-"]             # stdin mode; clang-format - reads from stdin
      formatter_mode: pipe              # pipe (default) or file
      chunking: "function"
      
    cpp:
      enabled: true
      formatter: "clang-format"
      formatter_args: ["-style=google", "-"]
      formatter_mode: pipe
      
    pascal:
      enabled: true
      formatter: "builtin"             # built-in Go formatter, no subprocess
      formatter_mode: pipe             # built-in always uses pipe mode
      
    oberon:
      enabled: true
      formatter: "builtin"
      formatter_mode: pipe
      # Example of a hypothetical file-mode-only formatter:
      # formatter: "oberon-format"
      # formatter_mode: file           # requires safe_mode: false in harvey.yaml
      
    lisp:
      enabled: true
      formatter: "builtin"  # or "sly" if installed
      
    basic:
      enabled: true
      formatter: "builtin"
      
    # Existing languages
    go:
      enabled: true
      formatter: "gofmt"
      
    python:
      enabled: true
      formatter: "black"
      
    javascript:
      enabled: true
      formatter: "prettier"
      formatter_args: ["--tab-width=2", "--single-quote"]
```

---

## Appendix D: Command Examples

### New Commands
```bash
# Enable/disable auto-formatting
harvey> /config set language.auto_format true
harvey> /config set language.auto_format false

# Set formatter for a language
harvey> /config set language.c.formatter clang-format
harvey> /config set language.c.formatter_args "-style=llvm"

# Enable/disable syntax highlighting
harvey> /config set language.syntax_highlight true

# Manually format a file
harvey> /format path/to/file.c

# Show supported languages
harvey> /languages list

# Show language info
harvey> /languages info c

# Test highlighting
harvey> /highlight c path/to/file.c
```

---

*This plan is a living document. It will be updated as implementation progresses and as new requirements or constraints emerge.*
