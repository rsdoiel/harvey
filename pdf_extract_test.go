package harvey

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// skipIfNoPopplerTools skips the test when any poppler utility is missing.
func skipIfNoPopplerTools(t *testing.T) {
	t.Helper()
	if err := checkPopplerTools(); err != nil {
		t.Skip(err.Error())
	}
}

// ─── checkPopplerTools ───────────────────────────────────────────────────────

func TestCheckPopplerTools_available(t *testing.T) {
	skipIfNoPopplerTools(t)
	if err := checkPopplerTools(); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestCheckPopplerTools_missingMessage(t *testing.T) {
	// Verify the error message mentions the tool name and install hints.
	// We simulate a missing tool by temporarily shadowing PATH.
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed; cannot test error message content")
	}
	// Save and restore PATH.
	old := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", old) })
	os.Setenv("PATH", "")

	err := checkPopplerTools()
	if err == nil {
		t.Fatal("expected error when PATH is empty")
	}
	msg := err.Error()
	for _, want := range []string{"pdfinfo", "pdftotext", "pdfimages", "poppler"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
	if runtime.GOOS == "darwin" && !strings.Contains(msg, "brew") {
		t.Errorf("error message should mention brew on macOS: %s", msg)
	}
}

// ─── parsePDFPageRange ───────────────────────────────────────────────────────

func TestParsePDFPageRange(t *testing.T) {
	tests := []struct {
		input      string
		wantFirst  int
		wantLast   int
		wantErrSub string
	}{
		{"", 0, 0, ""},
		{"10", 10, 10, ""},
		{"40-55", 40, 55, ""},
		{"1-1", 1, 1, ""},
		{"0", 0, 0, "positive integer"},
		{"-5", 0, 0, "positive integer"},
		{"abc", 0, 0, "positive integer"},
		{"10-5", 0, 0, ">= first"},
		{"10-abc", 0, 0, ">= first"},
	}
	for _, tc := range tests {
		first, last, err := parsePDFPageRange(tc.input)
		if tc.wantErrSub != "" {
			if err == nil {
				t.Errorf("parsePDFPageRange(%q): expected error containing %q, got nil", tc.input, tc.wantErrSub)
				continue
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Errorf("parsePDFPageRange(%q): error = %q, want substring %q", tc.input, err.Error(), tc.wantErrSub)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePDFPageRange(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if first != tc.wantFirst || last != tc.wantLast {
			t.Errorf("parsePDFPageRange(%q) = (%d, %d), want (%d, %d)", tc.input, first, last, tc.wantFirst, tc.wantLast)
		}
	}
}

// ─── parsePDFInfo ────────────────────────────────────────────────────────────

func TestParsePDFInfo(t *testing.T) {
	input := `Title:          The Oberon-2 Report
Author:         H. Mössenböck, N. Wirth
Creator:        LaTeX
Producer:       pdfTeX
CreationDate:   Tue Jan  5 12:00:00 1993
ModDate:        Mon Apr 12 09:00:00 2004
Pages:          37
Encrypted:      no
`
	info := parsePDFInfo(input)
	if info.Title != "The Oberon-2 Report" {
		t.Errorf("Title = %q, want %q", info.Title, "The Oberon-2 Report")
	}
	if info.Author != "H. Mössenböck, N. Wirth" {
		t.Errorf("Author = %q, want %q", info.Author, "H. Mössenböck, N. Wirth")
	}
	if info.Pages != 37 {
		t.Errorf("Pages = %d, want 37", info.Pages)
	}
	if info.CreatedAt != "Tue Jan  5 12:00:00 1993" {
		t.Errorf("CreatedAt = %q, want %q", info.CreatedAt, "Tue Jan  5 12:00:00 1993")
	}
}

func TestParsePDFInfo_empty(t *testing.T) {
	info := parsePDFInfo("")
	if info.Pages != 0 || info.Title != "" {
		t.Errorf("expected zero-value PDFInfo for empty input, got %+v", info)
	}
}

// ─── flagDiagramPages ────────────────────────────────────────────────────────

func TestFlagDiagramPages(t *testing.T) {
	// Page 2 has dense text — not a diagram.
	// Page 3 has sparse text but has a raster image — not a diagram.
	// Page 4 has sparse text and no raster image — diagram.
	pdfimagesOut := `page   num  type   width height color comp bpc  enc interp  object ID x-ppi y-ppi size ratio
   3     0 image    800   600  rgb     3   8  jpeg   no         5  0   150   150  45K  60%`

	pageTexts := []string{
		// page 1 — dense text
		strings.Repeat("a", 200),
		// page 2 — dense text
		strings.Repeat("b", 100),
		// page 3 — sparse text, but has raster image
		"   ",
		// page 4 — sparse text, no raster image
		"  \n  ",
	}

	got := flagDiagramPages(pdfimagesOut, pageTexts, 1)

	// Only page 4 should be flagged.
	if len(got) != 1 || got[0] != 4 {
		t.Errorf("flagDiagramPages = %v, want [4]", got)
	}
}

func TestFlagDiagramPages_noImages(t *testing.T) {
	// Empty pdfimages output — only sparse pages are flagged.
	pageTexts := []string{strings.Repeat("a", 60), "  "}
	got := flagDiagramPages("", pageTexts, 1)
	if len(got) != 1 || got[0] != 2 {
		t.Errorf("flagDiagramPages with no images = %v, want [2]", got)
	}
}

// ─── pdfExtract integration ──────────────────────────────────────────────────

// testPDFPath returns the path to the Oberon2 PDF relative to the repo root,
// and skips the test if the file is not present.
func testPDFPath(t *testing.T) string {
	t.Helper()
	// Resolve from the harvey/ package directory up to the repo root.
	abs, err := filepath.Abs(filepath.Join("..", "Reference", "Oberon", "Oberon2.pdf"))
	if err != nil {
		t.Skipf("cannot resolve test PDF path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("test PDF not present (%s): %v", abs, err)
	}
	return abs
}

func TestPdfExtract_fullDocument(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)

	result, err := pdfExtract(pdf, "")
	if err != nil {
		t.Fatalf("pdfExtract: %v", err)
	}
	if result.Info.Pages == 0 {
		t.Error("expected Pages > 0")
	}
	if len(result.Text) == 0 {
		t.Error("expected non-empty text")
	}
	t.Logf("Title: %q  Author: %q  Pages: %d  DiagramPages: %v",
		result.Info.Title, result.Info.Author, result.Info.Pages, result.DiagramPages)
}

func TestPdfExtract_pageRange(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)

	result, err := pdfExtract(pdf, "1-3")
	if err != nil {
		t.Fatalf("pdfExtract with range: %v", err)
	}
	if len(result.Text) == 0 {
		t.Error("expected non-empty text for pages 1-3")
	}
	// At most 3 page entries (some may be trimmed if trailing \f is absent).
	pageTexts := strings.Split(result.Text, "\f")
	if len(pageTexts) > 4 { // allow one extra trailing empty entry
		t.Errorf("expected <= 3 pages of text, got %d", len(pageTexts))
	}
}

func TestPdfExtract_badPageRange(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)

	_, err := pdfExtract(pdf, "bad")
	if err == nil {
		t.Error("expected error for invalid page range")
	}
}

func TestPdfExtract_missingFile(t *testing.T) {
	skipIfNoPopplerTools(t)
	_, err := pdfExtract("/nonexistent/file.pdf", "")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ─── resolvePDFPath ───────────────────────────────────────────────────────────

func TestResolvePDFPath_absolute(t *testing.T) {
	got, err := resolvePDFPath("/tmp/foo.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/tmp/foo.pdf" {
		t.Errorf("got %q, want /tmp/foo.pdf", got)
	}
}

func TestResolvePDFPath_tilde(t *testing.T) {
	got, err := resolvePDFPath("~/foo.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "/") {
		t.Errorf("expected absolute path, got %q", got)
	}
	if !strings.HasSuffix(got, "/foo.pdf") {
		t.Errorf("expected path ending in /foo.pdf, got %q", got)
	}
}

// ─── cmdReadPDF page cap ──────────────────────────────────────────────────────

func TestCmdReadPDF_noArgs(t *testing.T) {
	skipIfNoPopplerTools(t)
	a := newTestAgent(t)
	a.registerCommands()
	var out strings.Builder
	_ = cmdReadPDF(a, nil, &out)
	if !strings.Contains(out.String(), "Usage") {
		t.Errorf("expected usage message, got: %s", out.String())
	}
}

func TestCmdReadPDF_rangeExceedsLimit(t *testing.T) {
	skipIfNoPopplerTools(t)
	a := newTestAgent(t)
	var out strings.Builder
	// A 21-page range always exceeds the cap regardless of the file.
	_ = cmdReadPDF(a, []string{"/nonexistent.pdf", "1-21"}, &out)
	msg := out.String()
	if !strings.Contains(msg, "21 pages") || !strings.Contains(msg, "limit") {
		t.Errorf("expected page-cap error, got: %s", msg)
	}
}

// ─── ragIngestPDF ────────────────────────────────────────────────────────────

// pdfMockEmbedder is a length-4 embedder for ragIngestPDF tests that does not
// require a live Ollama instance.
type pdfMockEmbedder struct{}

func (pdfMockEmbedder) Name() string { return "pdf-mock" }
func (pdfMockEmbedder) Embed(text string) ([]float64, error) {
	vec := make([]float64, 4)
	for i, r := range text {
		vec[i%4] += float64(r)
	}
	return vec, nil
}

func TestRagIngestPDF_integration(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)

	dbPath := filepath.Join(t.TempDir(), "test_rag_pdf.db")
	store, err := NewRagStore(dbPath, "pdf-mock")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.Close()

	n, diagrams, err := ragIngestPDF(store, pdfMockEmbedder{}, pdf)
	if err != nil {
		t.Fatalf("ragIngestPDF: %v", err)
	}
	if n == 0 {
		t.Error("expected at least one chunk ingested")
	}
	t.Logf("ingested %d chunk(s), %d diagram page(s): %v", n, len(diagrams), diagrams)

	// Verify chunk count matches database.
	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != int64(n) {
		t.Errorf("store.Count()=%d, want %d", count, n)
	}
}

func TestRagIngestPDF_chunkMetadata(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)

	dbPath := filepath.Join(t.TempDir(), "test_rag_pdf_meta.db")
	store, err := NewRagStore(dbPath, "pdf-mock")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.Close()

	if _, _, err := ragIngestPDF(store, pdfMockEmbedder{}, pdf); err != nil {
		t.Fatalf("ragIngestPDF: %v", err)
	}

	// Query for something likely to appear in the Oberon spec.
	chunks, err := store.Query("module", pdfMockEmbedder{}, 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk from query")
	}
	// Every chunk must carry the PDF provenance header.
	for i, c := range chunks {
		if !strings.Contains(c.Content, "[PDF:") {
			t.Errorf("chunk %d missing [PDF:] header: %q", i, c.Content[:min(80, len(c.Content))])
		}
		if !strings.Contains(c.Content, "page") {
			t.Errorf("chunk %d missing page reference: %q", i, c.Content[:min(80, len(c.Content))])
		}
	}
	t.Logf("top chunk: %s", chunks[0].Content[:min(120, len(chunks[0].Content))])
}

func TestRagIngestPDF_diagramPageMarker(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)

	dbPath := filepath.Join(t.TempDir(), "test_rag_pdf_diag.db")
	store, err := NewRagStore(dbPath, "pdf-mock")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.Close()

	_, diagrams, err := ragIngestPDF(store, pdfMockEmbedder{}, pdf)
	if err != nil {
		t.Fatalf("ragIngestPDF: %v", err)
	}
	if len(diagrams) == 0 {
		t.Skip("no diagram pages detected in test PDF; cannot verify marker")
	}

	// Retrieve every stored chunk and verify at least one carries the marker.
	total, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	chunks, err := store.Query("diagram", pdfMockEmbedder{}, int(total))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "DIAGRAM PAGE") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one stored chunk to contain the DIAGRAM PAGE marker")
	}
}

func TestCmdReadPDF_integration(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdReadPDF(a, []string{pdf, "1-5"}, &out); err != nil {
		t.Fatalf("cmdReadPDF: %v", err)
	}
	msg := out.String()
	if !strings.Contains(msg, "✓") {
		t.Errorf("expected success confirmation, got: %s", msg)
	}
	// Verify content was added to the conversation.
	if len(a.History) == 0 {
		t.Error("expected a message to be added to conversation")
	}
	if !strings.Contains(a.History[len(a.History)-1].Content, "/read-pdf") {
		t.Errorf("context message missing /read-pdf header")
	}
}

// ─── cmdAttach ────────────────────────────────────────────────────────────────

func TestCmdAttach_noArgs(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	_ = cmdAttach(a, nil, &out)
	if !strings.Contains(out.String(), "Usage") {
		t.Errorf("expected usage message, got: %s", out.String())
	}
}

func TestCmdAttach_directory(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	_ = cmdAttach(a, []string{t.TempDir()}, &out)
	if !strings.Contains(out.String(), "directory") {
		t.Errorf("expected directory error, got: %s", out.String())
	}
}

func TestCmdAttach_missingFile(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	_ = cmdAttach(a, []string{"/nonexistent/file.txt"}, &out)
	if !strings.Contains(out.String(), "✗") {
		t.Errorf("expected error for missing file, got: %s", out.String())
	}
}

func TestCmdAttach_textFile(t *testing.T) {
	a := newTestAgent(t)
	tmp := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(tmp, []byte("Hello, world!\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := cmdAttach(a, []string{tmp}, &out); err != nil {
		t.Fatalf("cmdAttach: %v", err)
	}
	if !strings.Contains(out.String(), "✓") {
		t.Errorf("expected success, got: %s", out.String())
	}
	if len(a.History) == 0 || !strings.Contains(a.History[len(a.History)-1].Content, "Hello, world!") {
		t.Error("expected file content in history")
	}
}

func TestCmdAttach_binaryFile(t *testing.T) {
	a := newTestAgent(t)
	tmp := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(tmp, []byte{0x00, 0x01, 0x02, 0xFF}, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	_ = cmdAttach(a, []string{tmp}, &out)
	if !strings.Contains(out.String(), "binary") {
		t.Errorf("expected binary rejection, got: %s", out.String())
	}
	if len(a.History) > 0 {
		t.Error("expected no message added for binary file")
	}
}

func TestCmdAttach_pdf(t *testing.T) {
	skipIfNoPopplerTools(t)
	pdf := testPDFPath(t)
	a := newTestAgent(t)
	var out strings.Builder
	// 290-page doc without a page range should be rejected.
	_ = cmdAttach(a, []string{pdf}, &out)
	msg := out.String()
	if !strings.Contains(msg, "290 pages") && !strings.Contains(msg, "limit") {
		t.Errorf("expected page-cap rejection for large PDF, got: %s", msg)
	}
}

func TestCmdAttach_imageNoVision(t *testing.T) {
	// Create a minimal 1×1 PNG (the smallest valid PNG).
	// PNG header + IHDR + IDAT + IEND — hand-crafted minimal file.
	png1x1 := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR length + type
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1×1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, // bit depth, color type, etc.
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41, // IDAT length + type
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00, // IDAT data
		0x00, 0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, // IEND
		0x44, 0xAE, 0x42, 0x60, 0x82,
	}
	tmp := filepath.Join(t.TempDir(), "pixel.png")
	if err := os.WriteFile(tmp, png1x1, 0o644); err != nil {
		t.Fatal(err)
	}

	// Agent with no client → no vision capability.
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdAttach(a, []string{tmp}, &out); err != nil {
		t.Fatalf("cmdAttach: %v", err)
	}
	msg := out.String()
	if !strings.Contains(msg, "no vision capability") {
		t.Errorf("expected no-vision message, got: %s", msg)
	}
	// Should still add a text description to history.
	if len(a.History) == 0 {
		t.Error("expected a message added even in fallback mode")
	}
	if len(a.History[len(a.History)-1].Parts) > 0 {
		t.Error("expected plain text message (no Parts) for no-vision fallback")
	}
}

// ─── attachDetectMIME ────────────────────────────────────────────────────────

func TestAttachDetectMIME_webp(t *testing.T) {
	mime := attachDetectMIME("photo.webp", []byte("any"))
	if mime != "image/webp" {
		t.Errorf("got %q, want image/webp", mime)
	}
}

func TestAttachDetectMIME_png(t *testing.T) {
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	mime := attachDetectMIME("img.png", pngSig)
	if mime != "image/png" {
		t.Errorf("got %q, want image/png", mime)
	}
}

// ─── AddMessageParts ─────────────────────────────────────────────────────────

func TestAddMessageParts_roundtrip(t *testing.T) {
	a := newTestAgent(t)
	parts := []anyllm.ContentPart{
		{Type: "text", Text: "[attached: img.png]"},
		{Type: "image_url", ImageURL: &anyllm.ImageURL{URL: "data:image/png;base64,abc"}},
	}
	a.AddMessageParts("user", parts)
	if len(a.History) != 1 {
		t.Fatalf("expected 1 message, got %d", len(a.History))
	}
	msg := a.History[0]
	if msg.Role != "user" {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if len(msg.Parts) != 2 {
		t.Errorf("parts len = %d, want 2", len(msg.Parts))
	}
	// harvestMessagesToAnyllm should pass Parts as Content.
	anyllmMsgs := harvestMessagesToAnyllm(a.History)
	if anyllmMsgs[0].ContentString() != "" {
		t.Error("expected empty ContentString when Parts are set")
	}
	if anyllmMsgs[0].ContentParts() == nil {
		t.Error("expected ContentParts to be non-nil")
	}
}
