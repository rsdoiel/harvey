package harvey

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

/** PDFInfo holds document-level metadata produced by pdfinfo.
 *
 * Fields:
 *   Title     (string) — document title from PDF metadata; may be empty.
 *   Author    (string) — document author from PDF metadata; may be empty.
 *   Pages     (int)    — total page count.
 *   CreatedAt (string) — creation date string as reported by pdfinfo.
 */
type PDFInfo struct {
	Title     string
	Author    string
	Pages     int
	CreatedAt string
}

/** PDFResult holds all content extracted from a PDF by pdfExtract.
 *
 * Fields:
 *   Info         (PDFInfo) — document metadata from pdfinfo.
 *   Text         (string)  — full extracted text from pdftotext -layout; pages
 *                            are separated by the form-feed character (\f).
 *   DiagramPages ([]int)   — 1-based page numbers that appear to contain only
 *                            vector graphics (sparse text, no raster images).
 *                            Content on these pages is incomplete; flag them
 *                            for follow-up with a vision-capable model.
 */
type PDFResult struct {
	Info         PDFInfo
	Text         string
	DiagramPages []int
}

// popplerTools lists the external binaries required for PDF extraction.
var popplerTools = []string{"pdfinfo", "pdftotext", "pdfimages"}

// checkPopplerTools returns a descriptive error when any poppler utility is absent,
// including platform-specific install instructions.
func checkPopplerTools() error {
	var missing []string
	for _, t := range popplerTools {
		if _, err := exec.LookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"PDF extraction requires poppler utilities (%s).\n"+
			"  macOS:         brew install poppler\n"+
			"  Debian/Ubuntu: apt install poppler-utils\n"+
			"  Fedora:        dnf install poppler-utils\n"+
			"  Run /help pdf-tools for more detail.",
		strings.Join(missing, ", "),
	)
}

// parsePDFPageRange parses an optional page range string into first and last page numbers.
// An empty string returns (0, 0), meaning the full document.
// "10" returns (10, 10). "40-55" returns (40, 55).
func parsePDFPageRange(pages string) (first, last int, err error) {
	if pages == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(pages, "-", 2)
	first, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || first < 1 {
		return 0, 0, fmt.Errorf("invalid page range %q: first page must be a positive integer", pages)
	}
	if len(parts) == 1 {
		return first, first, nil
	}
	last, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || last < first {
		return 0, 0, fmt.Errorf("invalid page range %q: last page must be >= first page", pages)
	}
	return first, last, nil
}

// parsePDFInfo parses the stdout of pdfinfo into a PDFInfo struct.
// Unrecognised lines are silently skipped.
func parsePDFInfo(output string) PDFInfo {
	var info PDFInfo
	for _, line := range strings.Split(output, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "Title":
			info.Title = val
		case "Author":
			info.Author = val
		case "Pages":
			info.Pages, _ = strconv.Atoi(val)
		case "CreationDate":
			info.CreatedAt = val
		}
	}
	return info
}

// flagDiagramPages returns 1-based page numbers that likely contain only vector
// graphics. A page is flagged when its extracted text has fewer than 50
// non-whitespace characters and no raster image is listed for it in the
// pdfimages -list output. startPage is the 1-based number of the first entry
// in pageTexts.
func flagDiagramPages(pdfimagesOut string, pageTexts []string, startPage int) []int {
	// Collect pages that have at least one raster image.
	raster := map[int]bool{}
	for _, line := range strings.Split(pdfimagesOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pg, err := strconv.Atoi(fields[0])
		if err != nil {
			continue // header row or blank line
		}
		raster[pg] = true
	}

	var diagrams []int
	for i, text := range pageTexts {
		pageNum := startPage + i
		nonws := 0
		for _, r := range text {
			if !unicode.IsSpace(r) {
				nonws++
			}
		}
		if nonws < 50 && !raster[pageNum] {
			diagrams = append(diagrams, pageNum)
		}
	}
	return diagrams
}

/** pdfExtract extracts text and metadata from a PDF file using poppler utilities.
 * Three tools are run in sequence: pdfinfo (metadata), pdftotext -layout (text),
 * and pdfimages -list (raster image detection for flagging diagram-only pages).
 *
 * Parameters:
 *   file  (string) — absolute path to the PDF file.
 *   pages (string) — optional page range, e.g. "40-55" or "10". Empty = all pages.
 *
 * Returns:
 *   *PDFResult — extracted text, document metadata, and diagram-only page numbers.
 *   error      — descriptive error if poppler tools are missing, file is unreadable,
 *                or the page range is malformed.
 *
 * Example:
 *   result, err := pdfExtract("/docs/spec.pdf", "1-10")
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   fmt.Printf("Title: %s\nPages: %d\n", result.Info.Title, result.Info.Pages)
 */
func pdfExtract(file, pages string) (*PDFResult, error) {
	if err := checkPopplerTools(); err != nil {
		return nil, err
	}

	first, last, err := parsePDFPageRange(pages)
	if err != nil {
		return nil, err
	}

	// pdfinfo — document metadata.
	infoOut, err := runTool("pdfinfo", file)
	if err != nil {
		return nil, fmt.Errorf("pdfinfo: %w", err)
	}
	info := parsePDFInfo(infoOut)

	// pdftotext -layout — text extraction preserving spatial structure.
	// "-" as the output file instructs pdftotext to write to stdout.
	txtArgs := []string{"-layout"}
	if first > 0 {
		txtArgs = append(txtArgs, "-f", strconv.Itoa(first), "-l", strconv.Itoa(last))
	}
	txtArgs = append(txtArgs, file, "-")
	textOut, err := runTool("pdftotext", txtArgs...)
	if err != nil {
		return nil, fmt.Errorf("pdftotext: %w", err)
	}

	// pdfimages -list — detect pages with raster images.
	// When using -list, no image files are written so no output root is needed.
	imgArgs := []string{"-list"}
	if first > 0 {
		imgArgs = append(imgArgs, "-f", strconv.Itoa(first), "-l", strconv.Itoa(last))
	}
	imgArgs = append(imgArgs, file)
	imagesOut, _ := runTool("pdfimages", imgArgs...) // best-effort; PDFs with no images exit non-zero on some versions

	// pdftotext inserts \f between pages; split to get per-page text.
	pageTexts := strings.Split(textOut, "\f")
	for len(pageTexts) > 0 && strings.TrimSpace(pageTexts[len(pageTexts)-1]) == "" {
		pageTexts = pageTexts[:len(pageTexts)-1]
	}

	startPage := 1
	if first > 0 {
		startPage = first
	}

	return &PDFResult{
		Info:         info,
		Text:         textOut,
		DiagramPages: flagDiagramPages(imagesOut, pageTexts, startPage),
	}, nil
}

// runTool executes a CLI program and returns its stdout. On non-zero exit,
// stderr is appended to the error message.
func runTool(name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		se := strings.TrimSpace(stderr.String())
		if se != "" {
			return "", fmt.Errorf("%w: %s", err, se)
		}
		return "", err
	}
	return stdout.String(), nil
}
