package harvey

import (
	"fmt"
	"regexp"
	"strings"
)

// diagramPageWarning is appended to chunks whose page range includes a
// diagram-only page, matching the wording used by the flat per-page PDF
// ingest path in ragIngestPDF.
const diagramPageWarning = "\n[DIAGRAM PAGE: vector graphics detected — text extraction is incomplete. Use a vision-capable model for this page.]"

// identifierTypeOrder lists IdentifierType values in the same order
// FindIdentifiers checks them, giving Citations a deterministic ordering.
var identifierTypeOrder = []IdentifierType{
	IdentifierDOI,
	IdentifierORCID,
	IdentifierROR,
	IdentifierRAiD,
	IdentifierArXiv,
	IdentifierFundRef,
	IdentifierISBN,
	IdentifierISSN,
	IdentifierISNI,
	IdentifierPMID,
	IdentifierPMCID,
	IdentifierVIAF,
	IdentifierSNAC,
	IdentifierLCNAF,
}

// reSectionPrefix matches a leading numeric or roman-numeral outline marker
// such as "1.", "2)", "II.", or "IV)" before a section heading.
var reSectionPrefix = regexp.MustCompile(`^(?:[0-9]+|[IVXLCDM]+)[.)]\s*`)

// sectionHeaders maps a recognized section-heading phrase (lower-case, with
// any leading outline marker and trailing punctuation already stripped) to
// the EnrichedChunk.ChunkType it identifies.
var sectionHeaders = map[string]string{
	"abstract":               "abstract",
	"introduction":           "introduction",
	"background":             "body",
	"related work":           "body",
	"methods":                "methods",
	"methodology":            "methods",
	"materials and methods":  "methods",
	"results":                "results",
	"results and discussion": "results",
	"discussion":             "discussion",
	"conclusion":             "conclusion",
	"conclusions":            "conclusion",
	"concluding remarks":     "conclusion",
	"acknowledgements":       "body",
	"acknowledgments":        "body",
	"funding":                "body",
	"references":             "references",
	"bibliography":           "references",
	"works cited":            "references",
}

/** isPaperLike reports whether the extracted pages of a PDF look like a
 * scholarly paper, as opposed to a report, slide deck, or scanned document.
 * It returns true if the first page contains a DOI, or if at least two
 * distinct scholarly section types (e.g. "introduction" and "references")
 * are recognized via classifySectionHeader across all pages.
 *
 * Parameters:
 *   pageTexts ([]string) — extracted text for each page, in page order.
 *
 * Returns:
 *   bool — true if the document appears to be a scholarly paper.
 *
 * Example:
 *   pages := []string{"Title\nAbstract\n...", "1. Introduction\n...", "References\n..."}
 *   isPaperLike(pages) // true
 */
func isPaperLike(pageTexts []string) bool {
	if len(pageTexts) > 0 && len(FindDOIs(pageTexts[0])) > 0 {
		return true
	}

	seen := make(map[string]bool)
	for _, pageText := range pageTexts {
		for _, line := range strings.Split(pageText, "\n") {
			if chunkType, ok := classifySectionHeader(line); ok {
				seen[chunkType] = true
			}
		}
	}
	return len(seen) >= 2
}

/** classifySectionHeader checks whether line is a recognized scholarly
 * section heading (optionally prefixed with a numeric or roman-numeral
 * outline marker, e.g. "1." or "II.") and, if so, returns the ChunkType it
 * maps to.
 *
 * Parameters:
 *   line (string) — a single line of extracted PDF text.
 *
 * Returns:
 *   chunkType (string) — one of "abstract", "introduction", "methods",
 *                         "results", "discussion", "conclusion",
 *                         "references", or "body"; "" if ok is false.
 *   ok        (bool)   — true if line is a recognized section heading.
 *
 * Example:
 *   chunkType, ok := classifySectionHeader("1. Introduction")
 *   // chunkType == "introduction", ok == true
 */
func classifySectionHeader(line string) (chunkType string, ok bool) {
	s := strings.TrimSpace(line)
	s = reSectionPrefix.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ".:")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	chunkType, ok = sectionHeaders[strings.ToLower(s)]
	return chunkType, ok
}

// scholarlySection accumulates the lines belonging to one section of a
// scholarly document, along with the page range it spans.
type scholarlySection struct {
	chunkType string
	lines     []string
	startPage int
	endPage   int
}

// hasContent reports whether s contains any non-whitespace text.
func (s scholarlySection) hasContent() bool {
	return strings.TrimSpace(strings.Join(s.lines, "\n")) != ""
}

// hasDiagramPage reports whether any page in s's page range is in diagramSet.
func (s scholarlySection) hasDiagramPage(diagramSet map[int]bool) bool {
	for p := s.startPage; p <= s.endPage; p++ {
		if diagramSet[p] {
			return true
		}
	}
	return false
}

// convertIdentifierMap converts FindIdentifiers' map[IdentifierType][]string
// result to map[string][]string for storage in EnrichedChunk.Identifiers.
func convertIdentifierMap(m map[IdentifierType][]string) map[string][]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string][]string, len(m))
	for t, values := range m {
		out[string(t)] = values
	}
	return out
}

// findCitations returns the scholarly identifiers found in text, in
// identifierTypeOrder, excluding any value already present in docIdentifiers
// (the source document's own identifiers). This surfaces identifiers that
// point to other works (e.g. citations in a references section) while
// excluding the document's own repeated DOI/ORCID/etc.
func findCitations(text string, docIdentifiers map[string][]string) []string {
	own := make(map[string]bool)
	for _, values := range docIdentifiers {
		for _, v := range values {
			own[v] = true
		}
	}

	found := FindIdentifiers(text)
	var citations []string
	for _, t := range identifierTypeOrder {
		for _, v := range found[t] {
			if own[v] {
				continue
			}
			citations = appendUnique(citations, v)
		}
	}
	return citations
}

/** scholarlyChunk splits the extracted pages of a scholarly paper into
 * section-aware EnrichedChunks. Each page's lines are scanned for recognized
 * section headings (via classifySectionHeader); the text between headings is
 * split into ~500-character pieces with ragChunk, and each piece becomes one
 * EnrichedChunk tagged with its section's ChunkType. Text before the first
 * recognized heading (title, authors, affiliations, abstract lead-in) is
 * classified as "body".
 *
 * Every returned chunk shares the same Identifiers map — the document's own
 * scholarly identifiers (DOI, author ORCIDs, affiliation RORs, funder
 * FundRef IDs, ...), scanned from every section except "references" so that
 * identifiers belonging only to cited works are not mistaken for the
 * document's own. Each chunk's Citations holds any identifiers found in that
 * chunk's own text that are not part of the document's own Identifiers — i.e.
 * identifiers pointing to other works, typically surfaced in the references
 * section.
 *
 * Chunks whose page range includes a page listed in diagramSet get the same
 * "[DIAGRAM PAGE: ...]" warning appended that the flat per-page ingest path
 * uses.
 *
 * Parameters:
 *   pageTexts  ([]string)     — extracted text for each page, in page order.
 *   title      (string)       — document title, used in each chunk's provenance header.
 *   diagramSet (map[int]bool) — 1-indexed page numbers flagged as diagram-only.
 *
 * Returns:
 *   []EnrichedChunk — section-tagged chunks ready for RagStore.IngestEnriched.
 *
 * Example:
 *   pages := []string{"Title\n\nAbstract\nThis paper...", "1. Introduction\n..."}
 *   chunks := scholarlyChunk(pages, "My Paper", nil)
 *   chunks[0].ChunkType // "body" (title) or "abstract", depending on heading placement
 */
func scholarlyChunk(pageTexts []string, title string, diagramSet map[int]bool) []EnrichedChunk {
	var sections []scholarlySection
	current := scholarlySection{chunkType: "body", startPage: 1, endPage: 1}

	for i, pageText := range pageTexts {
		pageNum := i + 1
		for _, line := range strings.Split(pageText, "\n") {
			if chunkType, ok := classifySectionHeader(line); ok {
				if current.hasContent() {
					sections = append(sections, current)
				}
				current = scholarlySection{chunkType: chunkType, startPage: pageNum, endPage: pageNum}
				continue
			}
			current.lines = append(current.lines, line)
			current.endPage = pageNum
		}
	}
	if current.hasContent() {
		sections = append(sections, current)
	}

	// The document's own identifiers (its DOI, author ORCIDs, affiliation
	// RORs, funder FundRef IDs, ...) are scanned from everything except the
	// references section, so that identifiers belonging only to cited works
	// can surface as Citations instead of being excluded as self-references.
	var ownText []string
	for _, section := range sections {
		if section.chunkType == "references" {
			continue
		}
		ownText = append(ownText, strings.Join(section.lines, "\n"))
	}
	docIdentifiers := convertIdentifierMap(FindIdentifiers(strings.Join(ownText, "\n")))
	totalPages := len(pageTexts)

	var chunks []EnrichedChunk
	for _, section := range sections {
		sectionText := strings.Join(section.lines, "\n")
		for _, chunkText := range ragChunk(sectionText) {
			content := fmt.Sprintf("[PDF: %q, section: %s, page %d-%d of %d]\n\n%s",
				title, section.chunkType, section.startPage, section.endPage, totalPages, chunkText)
			if section.hasDiagramPage(diagramSet) {
				content += diagramPageWarning
			}
			chunks = append(chunks, EnrichedChunk{
				Content:     content,
				ChunkType:   section.chunkType,
				Identifiers: docIdentifiers,
				Citations:   findCitations(chunkText, docIdentifiers),
			})
		}
	}
	return chunks
}
