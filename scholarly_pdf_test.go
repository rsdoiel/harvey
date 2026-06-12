package harvey

import (
	"reflect"
	"strings"
	"testing"

	"github.com/caltechlibrary/metadatatools"
)

// ─── isPaperLike ──────────────────────────────────────────────────────────────

func TestIsPaperLike(t *testing.T) {
	cases := []struct {
		name      string
		pageTexts []string
		want      bool
	}{
		{
			name: "two section headers",
			pageTexts: []string{
				"My Paper\n\nAbstract\nSome summary text.",
				"1. Introduction\nSome intro text.\n\nReferences\n[1] Someone, 2020.",
			},
			want: true,
		},
		{
			name: "page-1 DOI only",
			pageTexts: []string{
				"My Report\nDOI: 10.1234/abcd.5678\n\nSome prose with no headers.",
				"More prose, still no headers here.",
			},
			want: true,
		},
		{
			name: "plain prose, no headers or DOI",
			pageTexts: []string{
				"Quarterly Status Report\n\nThis quarter we shipped several features.",
				"Next quarter we plan to ship more features.",
			},
			want: false,
		},
		{
			name: "single section header is not enough",
			pageTexts: []string{
				"Some Slide Deck\n\nIntroduction\nWelcome to the presentation.",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPaperLike(tc.pageTexts); got != tc.want {
				t.Errorf("isPaperLike(...) = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─── classifySectionHeader ───────────────────────────────────────────────────

func TestClassifySectionHeader(t *testing.T) {
	cases := []struct {
		name          string
		line          string
		wantChunkType string
		wantOK        bool
	}{
		{"abstract", "Abstract", "abstract", true},
		{"introduction with numeric prefix", "1. Introduction", "introduction", true},
		{"background maps to body", "Background", "body", true},
		{"related work maps to body", "Related Work", "body", true},
		{"methods", "Methods", "methods", true},
		{"methodology", "Methodology", "methods", true},
		{"materials and methods", "Materials and Methods", "methods", true},
		{"results", "Results", "results", true},
		{"results and discussion", "Results and Discussion", "results", true},
		{"discussion with roman prefix", "II. METHODS", "methods", true},
		{"conclusion", "Conclusion", "conclusion", true},
		{"conclusions plural", "Conclusions", "conclusion", true},
		{"concluding remarks", "Concluding Remarks", "conclusion", true},
		{"acknowledgements", "Acknowledgements", "body", true},
		{"acknowledgments american spelling", "Acknowledgments", "body", true},
		{"funding maps to body", "Funding", "body", true},
		{"references", "References", "references", true},
		{"bibliography", "Bibliography", "references", true},
		{"works cited", "Works Cited", "references", true},
		{"trailing colon", "References:", "references", true},
		{"roman numeral with parenthesis prefix", "IV) Discussion", "discussion", true},
		{"not a header", "This is just a normal sentence.", "", false},
		{"empty line", "", "", false},
		{"numeric line with no header text", "1.", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotChunkType, gotOK := classifySectionHeader(tc.line)
			if gotOK != tc.wantOK || gotChunkType != tc.wantChunkType {
				t.Errorf("classifySectionHeader(%q) = (%q, %v), want (%q, %v)",
					tc.line, gotChunkType, gotOK, tc.wantChunkType, tc.wantOK)
			}
		})
	}
}

// ─── scholarlyChunk ───────────────────────────────────────────────────────────

// paperFixture is a small synthetic three-page paper: page 1 carries the
// title/authors/DOI/ORCID, abstract, and introduction; page 2 has methods
// and results; page 3 has discussion, conclusion, and a references section
// citing a different DOI.
var paperFixture = []string{
	"My Great Paper\n" +
		"By Jane Doe (ORCID: 0000-0003-0900-6903)\n" +
		"DOI: 10.1234/abcd.5678\n" +
		"\n" +
		"Abstract\n" +
		"This paper studies something interesting and presents results.\n" +
		"\n" +
		"1. Introduction\n" +
		"This is the introduction text describing background and motivation.",
	"2. Methods\n" +
		"We used a standard methodology described here.\n" +
		"\n" +
		"3. Results\n" +
		"The results show something.",
	"4. Discussion\n" +
		"We discuss the results here.\n" +
		"\n" +
		"5. Conclusion\n" +
		"In conclusion, this was great.\n" +
		"\n" +
		"References\n" +
		"[1] Smith, J. (2020). DOI: 10.5555/9999.0001",
}

func TestScholarlyChunk(t *testing.T) {
	chunks := scholarlyChunk(paperFixture, "My Great Paper", nil)

	wantTypes := []string{
		"body", "abstract", "introduction", "methods",
		"results", "discussion", "conclusion", "references",
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("scholarlyChunk(...) returned %d chunks, want %d: %#v", len(chunks), len(wantTypes), chunks)
	}
	for i, want := range wantTypes {
		if chunks[i].ChunkType != want {
			t.Errorf("chunks[%d].ChunkType = %q, want %q", i, chunks[i].ChunkType, want)
		}
	}

	// Every chunk's provenance header carries the title and section type.
	wantPrefix := `[PDF: "My Great Paper", section: ` + wantTypes[0]
	if !strings.HasPrefix(chunks[0].Content, wantPrefix) {
		t.Errorf("chunks[0].Content = %q, want prefix %q", chunks[0].Content, wantPrefix)
	}
	if !strings.Contains(chunks[0].Content, "page 1-1 of 3") {
		t.Errorf("chunks[0].Content = %q, want it to mention %q", chunks[0].Content, "page 1-1 of 3")
	}

	// Identifiers are the document's own DOI + ORCID, found outside the
	// references section, and are shared identically across every chunk.
	wantDOI := metadatatools.NormalizeDOI("10.1234/abcd.5678")
	wantORCID := metadatatools.NormalizeORCID("0000-0003-0900-6903")
	for i, c := range chunks {
		if !reflect.DeepEqual(c.Identifiers[string(IdentifierDOI)], []string{wantDOI}) {
			t.Errorf("chunks[%d].Identifiers[doi] = %v, want %v", i, c.Identifiers[string(IdentifierDOI)], []string{wantDOI})
		}
		if !reflect.DeepEqual(c.Identifiers[string(IdentifierORCID)], []string{wantORCID}) {
			t.Errorf("chunks[%d].Identifiers[orcid] = %v, want %v", i, c.Identifiers[string(IdentifierORCID)], []string{wantORCID})
		}
	}

	// Most chunks (including the body chunk, which contains the document's
	// own DOI/ORCID) have no citations.
	for i, c := range chunks {
		if wantTypes[i] == "references" {
			continue
		}
		if len(c.Citations) != 0 {
			t.Errorf("chunks[%d] (%s).Citations = %v, want empty", i, wantTypes[i], c.Citations)
		}
	}

	// The references chunk surfaces the cited work's DOI, which is not part
	// of the document's own Identifiers.
	refChunk := chunks[len(chunks)-1]
	wantCitedDOI := metadatatools.NormalizeDOI("10.5555/9999.0001")
	if !reflect.DeepEqual(refChunk.Citations, []string{wantCitedDOI}) {
		t.Errorf("references chunk Citations = %v, want %v", refChunk.Citations, []string{wantCitedDOI})
	}
	if cited := refChunk.Identifiers[string(IdentifierDOI)]; reflect.DeepEqual(cited, []string{wantCitedDOI}) {
		t.Errorf("cited DOI %v should not be part of the document's own Identifiers", cited)
	}
}

func TestScholarlyChunk_diagramPage(t *testing.T) {
	diagramSet := map[int]bool{2: true}
	chunks := scholarlyChunk(paperFixture, "My Great Paper", diagramSet)

	for _, c := range chunks {
		isMethodsOrResults := c.ChunkType == "methods" || c.ChunkType == "results"
		hasWarning := strings.Contains(c.Content, "[DIAGRAM PAGE:")
		if isMethodsOrResults && !hasWarning {
			t.Errorf("chunk %q (page 2) should carry the diagram-page warning, got: %s", c.ChunkType, c.Content)
		}
		if !isMethodsOrResults && hasWarning {
			t.Errorf("chunk %q (not on page 2) should not carry the diagram-page warning, got: %s", c.ChunkType, c.Content)
		}
	}
}
