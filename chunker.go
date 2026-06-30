package harvey

import (
	"path/filepath"
	"strings"
)

// DocType classifies a file path for chunking strategy selection.
type DocType int

const (
	// DocTypeProse uses paragraph (double-newline) boundaries.
	DocTypeProse DocType = iota
	// DocTypeSource uses blank-line-then-signature boundaries.
	DocTypeSource
)

/** ChunkConfig holds tuneable parameters for chunked document analysis,
 * mirroring the harvey.yaml chunking: stanza.
 *
 * Fields:
 *   Enabled        (bool)    — when false, the overflow alert and chunking are disabled.
 *   Threshold      (float64) — fraction of remaining context that triggers the alert (0.80 = 80%).
 *   ChunkSizeBytes (int)     — target chunk size in bytes; default 6000 (~1500 tokens).
 *   MaxChunks      (int)     — chunk count above which Harvey warns before proceeding.
 *   Overlap        (string)  — "paragraph", "sentence", or "none".
 *   STMWarnPct     (float64) — fraction of total context limit below which a summary_context
 *                              nudge is appended to the user message (0.20 = warn when <20%
 *                              remains). Set to 0 to disable.
 *
 * Example:
 *   cfg := DefaultChunkConfig()
 *   cfg.ChunkSizeBytes = 4000
 */
type ChunkConfig struct {
	Enabled        bool
	Threshold      float64
	ChunkSizeBytes int
	MaxChunks      int
	Overlap        string
	STMWarnPct     float64
}

/** DefaultChunkConfig returns the ChunkConfig used when no chunking: stanza
 * is present in harvey.yaml.
 *
 * Returns:
 *   ChunkConfig — ready-to-use configuration with sensible defaults.
 *
 * Example:
 *   cfg := DefaultChunkConfig()
 *   fmt.Println(cfg.ChunkSizeBytes) // 6000
 */
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{
		Enabled:        true,
		Threshold:      0.80,
		ChunkSizeBytes: 6000,
		MaxChunks:      20,
		Overlap:        "paragraph",
		STMWarnPct:     0.20,
	}
}

// sourceLangIDs is the set of language IDs treated as source code for
// chunking boundary detection. Everything else (markdown, text, html,
// json, yaml, etc.) is treated as prose.
var sourceLangIDs = map[string]bool{
	"go":         true,
	"typescript": true,
	"javascript": true,
	"python":     true,
	"rust":       true,
	"c":          true,
	"cpp":        true,
	"pascal":     true,
	"oberon":     true,
	"lisp":       true,
	"basic":      true,
	"bash":       true,
}

/** DetectDocType returns the DocType for path based on its file extension.
 * Source code extensions return DocTypeSource; all others return DocTypeProse.
 * Unknown extensions default to DocTypeProse.
 *
 * Parameters:
 *   path (string) — file path; only the extension is examined.
 *
 * Returns:
 *   DocType — DocTypeProse or DocTypeSource.
 *
 * Example:
 *   DetectDocType("main.go")    // DocTypeSource
 *   DetectDocType("README.md")  // DocTypeProse
 *   DetectDocType("data.xyz")   // DocTypeProse
 */
func DetectDocType(path string) DocType {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return DocTypeProse
	}
	langID, ok := globalRegistry.DetectFromExtension(ext)
	if !ok {
		return DocTypeProse
	}
	if sourceLangIDs[langID] {
		return DocTypeSource
	}
	return DocTypeProse
}

/** DocumentChunk is a segment of a document produced by ChunkDocument. It
 * carries the chunk text together with its 1-indexed line span in the original
 * document so that LLM prompts and tool responses can cite precise locations.
 *
 * Fields:
 *   Content   (string) — the chunk text (trimmed units joined by "\n\n").
 *   StartLine (int)    — 1-indexed first line of this chunk in the source document.
 *   EndLine   (int)    — 1-indexed last line of this chunk in the source document.
 *                        With overlap enabled, adjacent chunks share boundary lines.
 *
 * Example:
 *   for i, ch := range chunks {
 *       fmt.Printf("chunk %d: lines %d–%d\n", i+1, ch.StartLine, ch.EndLine)
 *   }
 */
type DocumentChunk struct {
	Content   string
	StartLine int
	EndLine   int
}

// textUnit is an internal paragraph/block unit with its source line span.
type textUnit struct {
	content   string
	startLine int
	endLine   int
}

// firstNonSpace returns the first non-whitespace byte of s, or 0 if s is
// all whitespace. Used to detect whether a split unit begins with a closing
// delimiter (}, )) that signals end-of-block rather than a new declaration.
func firstNonSpace(s string) byte {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return b
		}
	}
	return 0
}

// startLineOffset counts '\n' characters before the first non-whitespace byte
// in s. Used to find where trimmed content starts relative to a paragraph's
// first line.
func startLineOffset(s string) int {
	count := 0
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == '\n' {
			count++
		} else if b != ' ' && b != '\t' && b != '\r' {
			break
		}
	}
	return count
}

// splitUnitsWithLines divides content into textUnits, tracking each unit's
// start and end line in the original content. Prose: splits on double newlines.
// Source: closing-brace/paren units are merged into the preceding unit.
func splitUnitsWithLines(content string, docType DocType) []textUnit {
	parts := strings.Split(content, "\n\n")
	var units []textUnit
	line := 1

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		rawNewlines := strings.Count(p, "\n")

		if trimmed != "" {
			startLine := line + startLineOffset(p)
			endLine := startLine + strings.Count(trimmed, "\n")

			if docType == DocTypeSource && len(units) > 0 {
				first := firstNonSpace(trimmed)
				if first == '}' || first == ')' {
					units[len(units)-1].content += "\n\n" + trimmed
					units[len(units)-1].endLine = endLine
					line += rawNewlines + 2
					continue
				}
			}
			units = append(units, textUnit{content: trimmed, startLine: startLine, endLine: endLine})
		}
		line += rawNewlines + 2
	}
	return units
}

// joinTextUnits joins unit content strings with "\n\n".
func joinTextUnits(units []textUnit) string {
	parts := make([]string, len(units))
	for i, u := range units {
		parts[i] = u.content
	}
	return strings.Join(parts, "\n\n")
}

/** ChunkDocument splits content into DocumentChunks according to cfg and docType.
 * Returns at least one chunk. When content fits within cfg.ChunkSizeBytes,
 * the returned slice has exactly one element covering the full document.
 *
 * Each chunk records its 1-indexed StartLine and EndLine in the original content.
 * With overlap enabled, the last paragraph of chunk N is repeated at the start
 * of chunk N+1, so adjacent chunks share boundary line numbers.
 *
 * Overlap behaviour (cfg.Overlap):
 *   "paragraph" — the last unit of chunk N is prepended to chunk N+1.
 *   "none"      — no content is repeated between chunks.
 *   (other)     — treated as "paragraph".
 *
 * ChunkDocument never enforces cfg.MaxChunks; that guard is the caller's
 * responsibility so it can prompt the user before proceeding.
 *
 * Parameters:
 *   content (string)      — document text to split.
 *   cfg     (ChunkConfig) — chunking parameters.
 *   docType (DocType)     — DocTypeProse or DocTypeSource.
 *
 * Returns:
 *   []DocumentChunk — non-empty slice; each element carries Content, StartLine, EndLine.
 *
 * Example:
 *   chunks := ChunkDocument(longMarkdown, DefaultChunkConfig(), DocTypeProse)
 *   fmt.Printf("%d chunks, first spans lines %d–%d\n", len(chunks), chunks[0].StartLine, chunks[0].EndLine)
 */
func ChunkDocument(content string, cfg ChunkConfig, docType DocType) []DocumentChunk {
	totalLines := strings.Count(content, "\n") + 1
	if strings.TrimSpace(content) == "" {
		return []DocumentChunk{{Content: content, StartLine: 1, EndLine: totalLines}}
	}

	units := splitUnitsWithLines(content, docType)
	if len(units) == 0 {
		return []DocumentChunk{{Content: content, StartLine: 1, EndLine: totalLines}}
	}

	threshold := int(float64(cfg.ChunkSizeBytes) * 0.75)
	noOverlap := cfg.Overlap == "none"

	var chunks []DocumentChunk
	currentUnits := []textUnit{units[0]}

	for i := 1; i < len(units); i++ {
		unit := units[i]
		currentContent := joinTextUnits(currentUnits)

		if len(currentContent) >= threshold {
			chunks = append(chunks, DocumentChunk{
				Content:   currentContent,
				StartLine: currentUnits[0].startLine,
				EndLine:   currentUnits[len(currentUnits)-1].endLine,
			})
			if noOverlap {
				currentUnits = []textUnit{unit}
			} else {
				overlap := currentUnits[len(currentUnits)-1]
				currentUnits = []textUnit{overlap, unit}
			}
		} else {
			currentUnits = append(currentUnits, unit)
		}
	}

	if len(currentUnits) > 0 {
		chunks = append(chunks, DocumentChunk{
			Content:   joinTextUnits(currentUnits),
			StartLine: currentUnits[0].startLine,
			EndLine:   currentUnits[len(currentUnits)-1].endLine,
		})
	}

	if len(chunks) == 0 {
		return []DocumentChunk{{Content: content, StartLine: 1, EndLine: totalLines}}
	}
	return chunks
}
