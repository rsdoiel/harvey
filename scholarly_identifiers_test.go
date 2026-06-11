package harvey

import (
	"reflect"
	"testing"

	"github.com/caltechlibrary/metadatatools"
)

// runFindCases runs a table of {name, text, want} cases against fn and
// reports a t.Errorf for any mismatch.
func runFindCases(t *testing.T, fn func(string) []string, cases []struct {
	name string
	text string
	want []string
}) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fn(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("text %q: got %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestFindDOIs(t *testing.T) {
	runFindCases(t, FindDOIs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "extended URL",
			text: "See https://doi.org/10.1234/abcd.5678 for details.",
			want: []string{metadatatools.NormalizeDOI("10.1234/abcd.5678")},
		},
		{
			name: "labeled short form",
			text: "DOI: 10.1234/abcd.5678",
			want: []string{metadatatools.NormalizeDOI("10.1234/abcd.5678")},
		},
		{
			name: "bare short form mid-sentence",
			text: "Available at 10.1234/abcd.5678 in the archive.",
			want: []string{metadatatools.NormalizeDOI("10.1234/abcd.5678")},
		},
		{
			name: "trailing sentence punctuation is trimmed",
			text: "See 10.1234/abcd.5678.",
			want: []string{metadatatools.NormalizeDOI("10.1234/abcd.5678")},
		},
		{
			name: "no doi present",
			text: "Nothing to see here.",
			want: nil,
		},
	})
}

func TestFindORCIDs(t *testing.T) {
	runFindCases(t, FindORCIDs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled",
			text: "ORCID: 0000-0003-0900-6903",
			want: []string{metadatatools.NormalizeORCID("0000-0003-0900-6903")},
		},
		{
			name: "URL form",
			text: "https://orcid.org/0000-0003-0900-6903",
			want: []string{metadatatools.NormalizeORCID("0000-0003-0900-6903")},
		},
		{
			name: "bare form with no label",
			text: "R. S. Doiel (0000-0003-0900-6903)",
			want: []string{metadatatools.NormalizeORCID("0000-0003-0900-6903")},
		},
		{
			name: "invalid checksum not matched",
			text: "0000-0003-0900-6904",
			want: nil,
		},
	})
}

func TestFindRORs(t *testing.T) {
	runFindCases(t, FindRORs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "URL form",
			text: "Affiliation: https://ror.org/05dxps055",
			want: []string{metadatatools.NormalizeROR("https://ror.org/05dxps055")},
		},
		{
			name: "labeled bare form",
			text: "ROR: 05dxps055",
			want: []string{metadatatools.NormalizeROR("05dxps055")},
		},
		{
			name: "bare form with no label not matched",
			text: "Identifier 05dxps055 appears here.",
			want: nil,
		},
	})
}

func TestFindRAiDs(t *testing.T) {
	runFindCases(t, FindRAiDs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "raid.org URL",
			text: "RAiD: https://raid.org/10.26259/0e59e9a5",
			want: []string{metadatatools.NormalizeRAiD("10.26259/0e59e9a5")},
		},
		{
			name: "bare DOI-shaped form is not a RAiD",
			text: "DOI: 10.26259/0e59e9a5",
			want: nil,
		},
	})
}

func TestFindArXivIDs(t *testing.T) {
	runFindCases(t, FindArXivIDs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "CURIE new form",
			text: "see arXiv:2412.03631 for details",
			want: []string{metadatatools.NormalizeArXivID("arxiv:2412.03631")},
		},
		{
			name: "abs URL form",
			text: "https://arxiv.org/abs/2412.03631",
			want: []string{metadatatools.NormalizeArXivID("arxiv:2412.03631")},
		},
		{
			name: "old archive/number form",
			text: "arXiv:hep-th/9901001",
			want: []string{metadatatools.NormalizeArXivID("arxiv:hep-th/9901001")},
		},
		{
			name: "bare numeric form not matched",
			text: "version 2412.03631 released",
			want: nil,
		},
	})
}

func TestFindFundRefIDs(t *testing.T) {
	runFindCases(t, FindFundRefIDs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled",
			text: "Funder: 10.13039/100000001 (National Science Foundation)",
			want: []string{metadatatools.NormalizeFundRefID("10.13039/100000001")},
		},
		{
			name: "bare 10.13039 form",
			text: "10.13039/100000001",
			want: []string{metadatatools.NormalizeFundRefID("10.13039/100000001")},
		},
		{
			name: "non-13039 DOI not matched",
			text: "10.1234/abcd.5678",
			want: nil,
		},
	})
}

func TestFindISBNs(t *testing.T) {
	runFindCases(t, FindISBNs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled 13-digit",
			text: "ISBN: 978-3-16-148410-0",
			want: []string{metadatatools.NormalizeISBN("978-3-16-148410-0")},
		},
		{
			name: "labeled 10-digit with X check digit",
			text: "ISBN 160606942X",
			want: []string{metadatatools.NormalizeISBN("160606942X")},
		},
		{
			name: "978-prefixed without label",
			text: "See 978-1606069424 in the catalog.",
			want: []string{metadatatools.NormalizeISBN("978-1606069424")},
		},
		{
			name: "no isbn present",
			text: "Nothing to see here.",
			want: nil,
		},
	})
}

func TestFindISSNs(t *testing.T) {
	runFindCases(t, FindISSNs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "plain",
			text: "Journal ISSN 1058-6180, online edition.",
			want: []string{metadatatools.NormalizeISSN("1058-6180")},
		},
		{
			name: "invalid checksum not matched",
			text: "Journal ISSN 1058-6181, online edition.",
			want: nil,
		},
	})
}

func TestFindISNIs(t *testing.T) {
	runFindCases(t, FindISNIs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled space-grouped form",
			text: "ISNI: 0000 0001 2096 0218",
			want: []string{metadatatools.NormalizeISNI("0000 0001 2096 0218")},
		},
		{
			name: "isni.org URL contiguous form",
			text: "https://isni.org/isni/0000000120960218",
			want: []string{metadatatools.NormalizeISNI("0000000120960218")},
		},
		{
			name: "bare form with no label not matched",
			text: "Researcher ID 0000 0001 2096 0218 is listed.",
			want: nil,
		},
	})
}

func TestFindPMIDs(t *testing.T) {
	runFindCases(t, FindPMIDs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled",
			text: "PMID: 34125777",
			want: []string{metadatatools.NormalizePMID("34125777")},
		},
		{
			name: "pubmed URL",
			text: "https://pubmed.ncbi.nlm.nih.gov/34125777",
			want: []string{metadatatools.NormalizePMID("34125777")},
		},
		{
			name: "bare form with no label not matched",
			text: "See record 34125777 for details.",
			want: nil,
		},
	})
}

func TestFindPMCIDs(t *testing.T) {
	runFindCases(t, FindPMCIDs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "bare PMC form",
			text: "available at PMC11021482 online",
			want: []string{metadatatools.NormalizePMCID("PMC11021482")},
		},
		{
			name: "without PMC prefix not matched",
			text: "record 11021482 listed",
			want: nil,
		},
	})
}

func TestFindVIAFs(t *testing.T) {
	runFindCases(t, FindVIAFs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled",
			text: "VIAF: 108127625",
			want: []string{metadatatools.NormalizeVIAF("108127625")},
		},
		{
			name: "viaf.org URL",
			text: "https://viaf.org/viaf/108127625",
			want: []string{metadatatools.NormalizeVIAF("108127625")},
		},
		{
			name: "bare form with no label not matched",
			text: "record 108127625 listed",
			want: nil,
		},
	})
}

func TestFindSNACs(t *testing.T) {
	runFindCases(t, FindSNACs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled",
			text: "SNAC: 108127625",
			want: []string{metadatatools.NormalizeSNAC("108127625")},
		},
		{
			name: "snaccooperative.org URL",
			text: "https://snaccooperative.org/view/108127625",
			want: []string{metadatatools.NormalizeSNAC("108127625")},
		},
		{
			name: "bare form with no label not matched",
			text: "record 108127625 listed",
			want: nil,
		},
	})
}

func TestFindLCNAFs(t *testing.T) {
	runFindCases(t, FindLCNAFs, []struct {
		name string
		text string
		want []string
	}{
		{
			name: "labeled",
			text: "LCNAF: n81044376",
			want: []string{metadatatools.NormalizeLCNAF("n81044376")},
		},
		{
			name: "id.loc.gov URL",
			text: "https://id.loc.gov/authorities/names/no2023032145",
			want: []string{metadatatools.NormalizeLCNAF("no2023032145")},
		},
		{
			name: "bare form with no label not matched",
			text: "record n81044376 listed",
			want: nil,
		},
	})
}

// ─── disambiguation rules ───────────────────────────────────────────────────

func TestRAiDVsDOIDisambiguation(t *testing.T) {
	text := "Project DOI: 10.1234/abcd.0001 and RAiD https://raid.org/10.26259/0e59e9a5"

	gotDOIs := FindDOIs(text)
	wantDOIs := []string{
		metadatatools.NormalizeDOI("10.1234/abcd.0001"),
		metadatatools.NormalizeDOI("10.26259/0e59e9a5"),
	}
	if !reflect.DeepEqual(gotDOIs, wantDOIs) {
		t.Errorf("FindDOIs(%q) = %v, want %v", text, gotDOIs, wantDOIs)
	}

	gotRAiDs := FindRAiDs(text)
	wantRAiDs := []string{metadatatools.NormalizeRAiD("10.26259/0e59e9a5")}
	if !reflect.DeepEqual(gotRAiDs, wantRAiDs) {
		t.Errorf("FindRAiDs(%q) = %v, want %v", text, gotRAiDs, wantRAiDs)
	}
}

func TestORCIDVsISNIDisambiguation(t *testing.T) {
	text := "Researcher identifier 0000-0001-2096-0218 is listed with no further context."

	gotORCIDs := FindORCIDs(text)
	wantORCIDs := []string{metadatatools.NormalizeORCID("0000-0001-2096-0218")}
	if !reflect.DeepEqual(gotORCIDs, wantORCIDs) {
		t.Errorf("FindORCIDs(%q) = %v, want %v", text, gotORCIDs, wantORCIDs)
	}

	gotISNIs := FindISNIs(text)
	if len(gotISNIs) != 0 {
		t.Errorf("FindISNIs(%q) = %v, want empty (bare grouped+checksum form defaults to ORCID)", text, gotISNIs)
	}
}

func TestFundRefDualTagging(t *testing.T) {
	text := "Funder: 10.13039/100000001 (National Science Foundation)"

	gotDOIs := FindDOIs(text)
	wantDOIs := []string{metadatatools.NormalizeDOI("10.13039/100000001")}
	if !reflect.DeepEqual(gotDOIs, wantDOIs) {
		t.Errorf("FindDOIs(%q) = %v, want %v", text, gotDOIs, wantDOIs)
	}

	gotFundRefs := FindFundRefIDs(text)
	wantFundRefs := []string{metadatatools.NormalizeFundRefID("10.13039/100000001")}
	if !reflect.DeepEqual(gotFundRefs, wantFundRefs) {
		t.Errorf("FindFundRefIDs(%q) = %v, want %v", text, gotFundRefs, wantFundRefs)
	}
}

// ─── FindIdentifiers ─────────────────────────────────────────────────────────

func TestFindIdentifiers(t *testing.T) {
	text := "DOI: 10.1234/abcd.5678. ORCID: 0000-0003-0900-6903. Affiliation https://ror.org/05dxps055."
	got := FindIdentifiers(text)

	if want := []string{metadatatools.NormalizeDOI("10.1234/abcd.5678")}; !reflect.DeepEqual(got[IdentifierDOI], want) {
		t.Errorf("FindIdentifiers(...)[IdentifierDOI] = %v, want %v", got[IdentifierDOI], want)
	}
	if want := []string{metadatatools.NormalizeORCID("0000-0003-0900-6903")}; !reflect.DeepEqual(got[IdentifierORCID], want) {
		t.Errorf("FindIdentifiers(...)[IdentifierORCID] = %v, want %v", got[IdentifierORCID], want)
	}
	if want := []string{metadatatools.NormalizeROR("https://ror.org/05dxps055")}; !reflect.DeepEqual(got[IdentifierROR], want) {
		t.Errorf("FindIdentifiers(...)[IdentifierROR] = %v, want %v", got[IdentifierROR], want)
	}
	if _, ok := got[IdentifierISBN]; ok {
		t.Errorf("FindIdentifiers(...)[IdentifierISBN] = %v, want absent", got[IdentifierISBN])
	}
	if _, ok := got[IdentifierPMID]; ok {
		t.Errorf("FindIdentifiers(...)[IdentifierPMID] = %v, want absent", got[IdentifierPMID])
	}
}

func TestFindIdentifiersEmpty(t *testing.T) {
	got := FindIdentifiers("Nothing scholarly to see here.")
	if len(got) != 0 {
		t.Errorf("FindIdentifiers(...) = %v, want empty map", got)
	}
}
