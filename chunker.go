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

// splitUnits divides content into logical units for chunk accumulation.
// Prose: splits on double newlines; empty sections discarded.
// Source: same split, but units beginning with } or ) are merged into the
// preceding unit — they are end-of-block fragments, not new declarations.
func splitUnits(content string, docType DocType) []string {
	parts := strings.Split(content, "\n\n")
	var units []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if docType == DocTypeSource && len(units) > 0 {
			first := firstNonSpace(p)
			if first == '}' || first == ')' {
				units[len(units)-1] = units[len(units)-1] + "\n\n" + p
				continue
			}
		}
		units = append(units, p)
	}
	return units
}

/** ChunkDocument splits content into chunks according to cfg and docType.
 * Returns at least one chunk. When content fits within cfg.ChunkSizeBytes,
 * the returned slice has exactly one element equal to the content.
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
 *   content (string)     — document text to split.
 *   cfg     (ChunkConfig) — chunking parameters.
 *   docType (DocType)    — DocTypeProse or DocTypeSource.
 *
 * Returns:
 *   []string — non-empty slice of chunk strings.
 *
 * Example:
 *   chunks := ChunkDocument(longMarkdown, DefaultChunkConfig(), DocTypeProse)
 *   fmt.Printf("%d chunks\n", len(chunks))
 */
func ChunkDocument(content string, cfg ChunkConfig, docType DocType) []string {
	if strings.TrimSpace(content) == "" {
		return []string{content}
	}

	units := splitUnits(content, docType)
	if len(units) == 0 {
		return []string{content}
	}

	threshold := int(float64(cfg.ChunkSizeBytes) * 0.75)
	noOverlap := cfg.Overlap == "none"

	var chunks []string
	currentUnits := []string{units[0]}

	for i := 1; i < len(units); i++ {
		unit := units[i]
		currentContent := strings.Join(currentUnits, "\n\n")

		if len(currentContent) >= threshold {
			// Save the current chunk.
			chunks = append(chunks, currentContent)
			// Begin the next chunk: with or without overlap.
			if noOverlap {
				currentUnits = []string{unit}
			} else {
				// Overlap: start with the last unit of the chunk just saved.
				overlap := currentUnits[len(currentUnits)-1]
				currentUnits = []string{overlap, unit}
			}
		} else {
			currentUnits = append(currentUnits, unit)
		}
	}

	// Flush whatever remains.
	if len(currentUnits) > 0 {
		chunks = append(chunks, strings.Join(currentUnits, "\n\n"))
	}

	if len(chunks) == 0 {
		return []string{content}
	}
	return chunks
}
