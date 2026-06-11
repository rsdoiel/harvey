package harvey

import (
	"regexp"

	"github.com/caltechlibrary/metadatatools"
)

/** IdentifierType names a kind of scholarly identifier that
 * scholarly_identifiers.go can find and normalize. Values are the
 * lower-case strings used as keys in FindIdentifiers' result map and,
 * later, in the `chunks.identifiers` JSON column and
 * `concepts.identifier_type` column described in
 * towards_a_scholarly_memory_design.md.
 *
 * Example:
 *   var t IdentifierType = IdentifierDOI
 *   fmt.Println(string(t)) // "doi"
 */
type IdentifierType string

const (
	// IdentifierDOI is a Digital Object Identifier (10.xxxx/yyyy).
	IdentifierDOI IdentifierType = "doi"
	// IdentifierORCID is a researcher ORCID iD (0000-0003-0900-6903).
	IdentifierORCID IdentifierType = "orcid"
	// IdentifierROR is a Research Organization Registry identifier.
	IdentifierROR IdentifierType = "ror"
	// IdentifierRAiD is a Research Activity Identifier (DataCite DOI
	// resolved via raid.org).
	IdentifierRAiD IdentifierType = "raid"
	// IdentifierArXiv is an arXiv preprint identifier.
	IdentifierArXiv IdentifierType = "arxiv"
	// IdentifierFundRef is a Crossref Funder Registry identifier
	// (a DOI under the reserved 10.13039 prefix).
	IdentifierFundRef IdentifierType = "fundref"
	// IdentifierISBN is an International Standard Book Number
	// (10 or 13 digit).
	IdentifierISBN IdentifierType = "isbn"
	// IdentifierISSN is an International Standard Serial Number.
	IdentifierISSN IdentifierType = "issn"
	// IdentifierISNI is an International Standard Name Identifier.
	IdentifierISNI IdentifierType = "isni"
	// IdentifierPMID is a PubMed identifier.
	IdentifierPMID IdentifierType = "pmid"
	// IdentifierPMCID is a PubMed Central identifier (PMCxxxxxxx).
	IdentifierPMCID IdentifierType = "pmcid"
	// IdentifierVIAF is a Virtual International Authority File identifier.
	IdentifierVIAF IdentifierType = "viaf"
	// IdentifierSNAC is a Social Networks and Archival Context identifier.
	IdentifierSNAC IdentifierType = "snac"
	// IdentifierLCNAF is a Library of Congress Name Authority File
	// identifier.
	IdentifierLCNAF IdentifierType = "lcnaf"
)

// appendUnique appends value to values if it is not already present,
// preserving first-seen order.
func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

// firstNonEmpty returns the first non-empty string in groups, or "" if
// all are empty. Used to pick whichever alternative of a multi-group
// regex matched.
func firstNonEmpty(groups []string) string {
	for _, g := range groups {
		if g != "" {
			return g
		}
	}
	return ""
}

// reTrailingPunct matches sentence punctuation that commonly trails a
// DOI-shaped identifier in running text (e.g. "...10.1234/abcd.5678.")
// but is not part of the identifier itself.
var reTrailingPunct = regexp.MustCompile(`[.,;:)\]}>"']+$`)

// trimTrailingPunct removes trailing sentence punctuation from a
// DOI-shaped candidate.
func trimTrailingPunct(s string) string {
	return reTrailingPunct.ReplaceAllString(s, "")
}

// collectMatches runs re against text, and for every match picks the
// first non-empty capture group, optionally cleans it, validates it,
// and (if valid) normalizes it into the result slice. Duplicates are
// dropped.
func collectMatches(re *regexp.Regexp, text string, clean func(string) string, validate func(string) bool, normalize func(string) string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		candidate := firstNonEmpty(m[1:])
		if candidate == "" {
			continue
		}
		if clean != nil {
			candidate = clean(candidate)
		}
		if !validate(candidate) {
			continue
		}
		out = appendUnique(out, normalize(candidate))
	}
	return out
}

// ─── DOI ────────────────────────────────────────────────────────────────────

// reDOICandidate matches a DOI in extended-URL, "doi:"/"DOI:" CURIE, or
// bare short form. Group 1 is the bare 10.xxxx/yyyy core.
var reDOICandidate = regexp.MustCompile(`(?i)(?:doi\s*:\s*|https?://(?:dx\.|www\.)?doi\.org/)?(10\.\d{4,9}/\S+)`)

/** FindDOIs scans text for DOIs in any common form — extended URLs
 * (https://doi.org/...), "doi:"/"DOI:" labeled CURIEs, or bare short
 * form (10.xxxx/yyyy), which is the most common form in running text.
 * Every match is normalized to the extended URL form via
 * metadatatools.NormalizeDOI.
 *
 * Note: a https://raid.org/10.xxxx/yyyy URL is also picked up here
 * (its local identifier is a real DOI too) — see FindRAiDs for the
 * RAiD-specific extraction rule.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated DOIs in extended URL form
 *   (https://doi.org/10.xxxx/yyyy), in first-seen order.
 *
 * Example:
 *   dois := FindDOIs("See DOI: 10.1234/abcd.5678 for details.")
 *   // dois == []string{"https://doi.org/10.1234/abcd.5678"}
 */
func FindDOIs(text string) []string {
	return collectMatches(reDOICandidate, text, trimTrailingPunct, metadatatools.ValidateDOI, metadatatools.NormalizeDOI)
}

// ─── ORCID ──────────────────────────────────────────────────────────────────

// reORCIDCandidate matches an ORCID iD in URL, "orcid:"/"ORCID iD:"
// labeled, or bare hyphenated form. Group 1 is the 16-character
// hyphenated identifier.
var reORCIDCandidate = regexp.MustCompile(`(?i)(?:orcid(?:\s*id)?\s*:?\s*|https?://orcid\.org/)?(\d{4}-\d{4}-\d{4}-\d{3}[\dXx])`)

/** FindORCIDs scans text for ORCID iDs in URL form
 * (https://orcid.org/...), "ORCID:"/"orcid:" labeled form, or bare
 * hyphenated form (0000-0003-0900-6903) — the common form next to an
 * author's name in a byline. A bare match must pass the ISO 7064
 * Mod 11-2 checksum (via metadatatools.ValidateORCID) to be accepted,
 * which keeps false positives from incidental 16-digit groups low.
 *
 * Note: this is also the default classification for any bare
 * 16-digit, hyphen-grouped, checksum-valid identifier — see FindISNIs
 * for why ISNI requires explicit context to be recognized separately.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated ORCID iDs in bare hyphenated form
 *   (e.g. "0000-0003-0900-6903"), in first-seen order.
 *
 * Example:
 *   ids := FindORCIDs("R. S. Doiel (ORCID: 0000-0003-0900-6903)")
 *   // ids == []string{"0000-0003-0900-6903"}
 */
func FindORCIDs(text string) []string {
	return collectMatches(reORCIDCandidate, text, nil, metadatatools.ValidateORCID, metadatatools.NormalizeORCID)
}

// ─── ROR ────────────────────────────────────────────────────────────────────

// reRORCandidate matches a ROR identifier as a ror.org URL or an
// explicit "ROR:" label followed by the bare 9-character identifier.
// Group 1 is the bare identifier.
var reRORCandidate = regexp.MustCompile(`(?i)(?:https?://ror\.org/|ror\s*:?\s*)(0[a-hj-km-np-tv-z0-9]{6}[0-9]{2})`)

/** FindRORs scans text for Research Organization Registry identifiers,
 * either as a ror.org URL (https://ror.org/05dxps055) or an explicit
 * "ROR:" label followed by the bare identifier. A bare 9-character
 * token with no URL or label is not matched — the shape alone is too
 * generic to scan reliably.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated ROR identifiers in extended URL form
 *   (https://ror.org/05dxps055), in first-seen order.
 *
 * Example:
 *   rors := FindRORs("Affiliation: https://ror.org/05dxps055")
 *   // rors == []string{"https://ror.org/05dxps055"}
 */
func FindRORs(text string) []string {
	return collectMatches(reRORCandidate, text, nil, metadatatools.ValidateROR, metadatatools.NormalizeROR)
}

// ─── RAiD ───────────────────────────────────────────────────────────────────

// reRAiDCandidate matches a RAiD only in its full https://raid.org/
// resolver-URL form. Group 1 is the bare 10.xxxx/yyyy core.
var reRAiDCandidate = regexp.MustCompile(`(?i)https://raid\.org/(10\.\d{4,9}/\S+)`)

/** FindRAiDs scans text for Research Activity Identifiers (RAiDs).
 *
 * RAiD and DOI are format-identical (10.xxxx/yyyy — a RAiD is a
 * DataCite DOI per ISO 23527), so format alone cannot disambiguate
 * them. Per the convention recorded in
 * towards_a_scholarly_memory_design.md, FindRAiDs matches ONLY an
 * explicit https://raid.org/10.xxxx/yyyy resolver URL. A bare or
 * "DOI:"-labeled 10.xxxx/yyyy is always classified as a DOI (see
 * FindDOIs), never as a RAiD.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated RAiDs in extended URL form
 *   (https://raid.org/10.xxxx/yyyy), in first-seen order.
 *
 * Example:
 *   raids := FindRAiDs("Project RAiD: https://raid.org/10.83962/fb5be317")
 *   // raids == []string{"https://raid.org/10.83962/fb5be317"}
 */
func FindRAiDs(text string) []string {
	return collectMatches(reRAiDCandidate, text, trimTrailingPunct, metadatatools.ValidateRAiD, metadatatools.NormalizeRAiD)
}

// ─── ArXiv ──────────────────────────────────────────────────────────────────

// reArXivCandidate matches an arXiv identifier as an "arxiv:" CURIE or
// an arxiv.org/abs/ URL. Group 1 is the bare identifier (without the
// "arxiv:" prefix), in either new (YYMM.NNNNN) or old (archive/YYMMNNN)
// form, with an optional version suffix.
var reArXivCandidate = regexp.MustCompile(`(?i)(?:arxiv\s*:\s*|https?://arxiv\.org/abs/)(\d{4}\.\d{4,5}(?:v\d+)?|[a-z\-]+/\d{7}(?:v\d+)?)`)

/** FindArXivIDs scans text for arXiv preprint identifiers, either as an
 * "arXiv:" CURIE (arXiv:2412.03631) or an arxiv.org/abs/ URL. A bare
 * numeric identifier with no "arxiv:" label or URL is not matched —
 * the YYMM.NNNNN shape alone is too easily confused with version
 * numbers or other decimals.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated arXiv identifiers as lower-case "arxiv:"
 *   CURIEs (e.g. "arxiv:2412.03631"), in first-seen order.
 *
 * Example:
 *   ids := FindArXivIDs("see arXiv:2412.03631 for the preprint")
 *   // ids == []string{"arxiv:2412.03631"}
 */
func FindArXivIDs(text string) []string {
	var out []string
	for _, m := range reArXivCandidate.FindAllStringSubmatch(text, -1) {
		bare := m[1]
		if bare == "" {
			continue
		}
		candidate := "arxiv:" + bare
		if !metadatatools.ValidateArXivID(candidate) {
			continue
		}
		out = appendUnique(out, metadatatools.NormalizeArXivID(candidate))
	}
	return out
}

// ─── FundRef ────────────────────────────────────────────────────────────────

// reFundRefCandidate matches a Crossref Funder Registry identifier — a
// DOI under the reserved 10.13039 prefix — optionally preceded by a
// "FundRef:"/"Funder:"/"doi:" label or a doi.org URL. Group 1 is the
// bare 10.13039/yyyy core.
var reFundRefCandidate = regexp.MustCompile(`(?i)(?:fundref\s*:\s*|funder\s*:\s*|doi\s*:\s*|https?://(?:dx\.|www\.)?doi\.org/)?(10\.13039/\S+)`)

/** FindFundRefIDs scans text for Crossref Funder Registry identifiers —
 * DOIs under the reserved 10.13039 prefix that identify a funding
 * organization.
 *
 * A 10.13039/xxxxx string is simultaneously a real DOI and a funder
 * identifier (unlike the RAiD/DOI case, there is no ambiguity to
 * resolve), so the same string is correctly returned by both FindDOIs
 * and FindFundRefIDs.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated FundRef identifiers in bare DOI-shaped
 *   form (e.g. "10.13039/100006961"), in first-seen order.
 *
 * Example:
 *   ids := FindFundRefIDs("Funder: 10.13039/100006961 (Caltech Library)")
 *   // ids == []string{"10.13039/100006961"}
 */
func FindFundRefIDs(text string) []string {
	return collectMatches(reFundRefCandidate, text, trimTrailingPunct, metadatatools.ValidateFundRefID, metadatatools.NormalizeFundRefID)
}

// ─── ISBN ───────────────────────────────────────────────────────────────────

// reISBNLabeled matches an "ISBN"/"ISBN-10"/"ISBN-13" label followed by
// a run of digits, hyphens, spaces, or a trailing check character.
// Group 1 is the candidate identifier (still hyphenated/spaced).
var reISBNLabeled = regexp.MustCompile(`(?i)isbn(?:-1[03])?\s*:?\s*([\dXx][\dXx\s-]{8,16}[\dXx])`)

// reISBNGrouped matches a hyphen-grouped ISBN-13 beginning with the
// reserved 978/979 EAN.UCC bookland prefix, without requiring a label.
// Group 1 is the candidate identifier.
var reISBNGrouped = regexp.MustCompile(`\b(97[89][\d-]{10,14}[\dXx])\b`)

/** FindISBNs scans text for ISBN-10 and ISBN-13 identifiers. A match
 * requires either an explicit "ISBN"/"ISBN-10"/"ISBN-13" label, or the
 * hyphen-grouped form beginning with the reserved 978/979 bookland
 * prefix — a bare run of digits alone is too ambiguous to scan
 * reliably. Each candidate is confirmed via its ISBN-10 (Mod 11) or
 * ISBN-13 (Mod 10 / EAN-13) checksum.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated ISBNs in hyphenated normalized form, in
 *   first-seen order.
 *
 * Example:
 *   isbns := FindISBNs("ISBN: 978-3-16-148410-0")
 *   // isbns == []string{"978-3-1-61-484100-0"} // see metadatatools.NormalizeISBN
 */
func FindISBNs(text string) []string {
	var out []string
	for _, m := range reISBNLabeled.FindAllStringSubmatch(text, -1) {
		if metadatatools.ValidateISBN(m[1]) {
			out = appendUnique(out, metadatatools.NormalizeISBN(m[1]))
		}
	}
	for _, m := range reISBNGrouped.FindAllStringSubmatch(text, -1) {
		if metadatatools.ValidateISBN(m[1]) {
			out = appendUnique(out, metadatatools.NormalizeISBN(m[1]))
		}
	}
	return out
}

// ─── ISSN ───────────────────────────────────────────────────────────────────

// reISSNCandidate matches the NNNN-NNNX shape of an ISSN. Group 1 is
// the candidate identifier.
var reISSNCandidate = regexp.MustCompile(`\b(\d{4}-\d{3}[\dXx])\b`)

/** FindISSNs scans text for International Standard Serial Numbers in
 * their NNNN-NNNX hyphenated form. The 8-character hyphenated shape
 * with a Mod 11 checksum digit (validated via
 * metadatatools.ValidateISSN) is distinctive enough to scan without
 * requiring a label.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated ISSNs in NNNN-NNNX form, in first-seen
 *   order.
 *
 * Example:
 *   issns := FindISSNs("Journal ISSN 1058-6180, online edition.")
 *   // issns == []string{"1058-6180"}
 */
func FindISSNs(text string) []string {
	return collectMatches(reISSNCandidate, text, nil, metadatatools.ValidateISSN, metadatatools.NormalizeISSN)
}

// ─── ISNI ───────────────────────────────────────────────────────────────────

// reISNICandidate matches an ISNI only when preceded by an explicit
// "ISNI:" label or an isni.org URL. Group 1 is the 16-digit candidate,
// with optional whitespace or hyphen separators (isni.org URLs use the
// contiguous 16-digit form; printed citations are usually grouped).
var reISNICandidate = regexp.MustCompile(`(?i)(?:isni\s*:?\s*|https?://(?:www\.)?isni\.org/isni/)(\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{3}[\dXx])`)

/** FindISNIs scans text for International Standard Name Identifiers.
 *
 * ISNI shares its exact shape and ISO 7064 Mod 11-2 checksum with
 * ORCID (16 digits in four groups, optional trailing X) — a bare match
 * is ambiguous between the two. Per the convention in
 * towards_a_scholarly_memory_design.md, a bare grouped+checksum match
 * defaults to ORCID (see FindORCIDs); FindISNIs only matches when an
 * explicit "ISNI:" label or isni.org URL provides disambiguating
 * context.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated ISNIs in space-grouped form
 *   (e.g. "0000 0003 0900 6903"), in first-seen order.
 *
 * Example:
 *   isnis := FindISNIs("ISNI: 0000 0003 0900 6903")
 *   // isnis == []string{"0000 0003 0900 6903"}
 */
func FindISNIs(text string) []string {
	return collectMatches(reISNICandidate, text, nil, metadatatools.ValidateISNI, metadatatools.NormalizeISNI)
}

// ─── PMID ───────────────────────────────────────────────────────────────────

// rePMIDCandidate matches a PMID only when preceded by a "PMID:" label
// or as part of a pubmed.ncbi.nlm.nih.gov URL. Groups 1 and 2 cover the
// two alternatives.
var rePMIDCandidate = regexp.MustCompile(`(?i)pmid\s*:?\s*(\d+)|pubmed\.ncbi\.nlm\.nih\.gov/(\d+)`)

/** FindPMIDs scans text for PubMed identifiers. metadatatools'
 * PMIDPattern is "one or more digits", which alone matches any integer,
 * so FindPMIDs only matches when an explicit "PMID:" label or a
 * pubmed.ncbi.nlm.nih.gov URL provides context.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated PMIDs (bare digit strings), in first-seen
 *   order.
 *
 * Example:
 *   pmids := FindPMIDs("PMID: 33417889")
 *   // pmids == []string{"33417889"}
 */
func FindPMIDs(text string) []string {
	return collectMatches(rePMIDCandidate, text, nil, metadatatools.ValidatePMID, metadatatools.NormalizePMID)
}

// ─── PMCID ──────────────────────────────────────────────────────────────────

// rePMCIDCandidate matches the self-distinctive PMCxxxxxxx form.
var rePMCIDCandidate = regexp.MustCompile(`\b(PMC\d+)\b`)

/** FindPMCIDs scans text for PubMed Central identifiers. The "PMC"
 * prefix is part of the identifier itself, so the bare PMCxxxxxxx form
 * is self-distinctive and needs no label or URL context.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated PMCIDs (e.g. "PMC1234567"), in first-seen
 *   order.
 *
 * Example:
 *   pmcids := FindPMCIDs("available at PMC1234567")
 *   // pmcids == []string{"PMC1234567"}
 */
func FindPMCIDs(text string) []string {
	return collectMatches(rePMCIDCandidate, text, nil, metadatatools.ValidatePMCID, metadatatools.NormalizePMCID)
}

// ─── VIAF ───────────────────────────────────────────────────────────────────

// reVIAFCandidate matches a VIAF identifier only when preceded by a
// "VIAF:" label or as part of a viaf.org/viaf/ URL. Groups 1 and 2
// cover the two alternatives.
var reVIAFCandidate = regexp.MustCompile(`(?i)viaf\s*:?\s*(\d+)|viaf\.org/viaf/(\d+)`)

/** FindVIAFs scans text for Virtual International Authority File
 * identifiers. metadatatools' VIAFPattern is "one or more digits",
 * which alone matches any integer, so FindVIAFs only matches when an
 * explicit "VIAF:" label or a viaf.org/viaf/ URL provides context.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated VIAF identifiers (bare digit strings), in
 *   first-seen order.
 *
 * Example:
 *   ids := FindVIAFs("VIAF: 12345678")
 *   // ids == []string{"12345678"}
 */
func FindVIAFs(text string) []string {
	return collectMatches(reVIAFCandidate, text, nil, metadatatools.ValidateVIAF, metadatatools.NormalizeVIAF)
}

// ─── SNAC ───────────────────────────────────────────────────────────────────

// reSNACCandidate matches a SNAC identifier only when preceded by a
// "SNAC:" label or as part of a snaccooperative.org/view/ URL. Groups 1
// and 2 cover the two alternatives.
var reSNACCandidate = regexp.MustCompile(`(?i)snac\s*:?\s*(\d+)|snaccooperative\.org/view/(\d+)`)

/** FindSNACs scans text for Social Networks and Archival Context
 * identifiers. metadatatools' SNACPattern is "one or more digits",
 * which alone matches any integer, so FindSNACs only matches when an
 * explicit "SNAC:" label or a snaccooperative.org/view/ URL provides
 * context.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated SNAC identifiers (bare digit strings), in
 *   first-seen order.
 *
 * Example:
 *   ids := FindSNACs("SNAC: 12345678")
 *   // ids == []string{"12345678"}
 */
func FindSNACs(text string) []string {
	return collectMatches(reSNACCandidate, text, nil, metadatatools.ValidateSNAC, metadatatools.NormalizeSNAC)
}

// ─── LCNAF ──────────────────────────────────────────────────────────────────

// reLCNAFCandidate matches an LCNAF identifier only when preceded by an
// "LCNAF:" label or as part of an id.loc.gov/authorities/names/ URL.
// Groups 1 and 2 cover the two alternatives, matching the conventional
// LC control number shape: 1-3 letters then 8-10 digits.
var reLCNAFCandidate = regexp.MustCompile(`(?i)lcnaf\s*:?\s*([a-z]{1,3}\d{8,10})|id\.loc\.gov/authorities/names/([a-z]{1,3}\d{8,10})`)

/** FindLCNAFs scans text for Library of Congress Name Authority File
 * identifiers. metadatatools' LCNAFPattern (any alphanumeric string)
 * is far too permissive for bare scanning, so FindLCNAFs only matches
 * when an explicit "LCNAF:" label or an id.loc.gov/authorities/names/
 * URL provides context, using the conventional LC control number shape
 * (1-3 letters followed by 8-10 digits).
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   []string — deduplicated LCNAF identifiers, in first-seen order.
 *
 * Example:
 *   ids := FindLCNAFs("LCNAF: n79021164")
 *   // ids == []string{"n79021164"}
 */
func FindLCNAFs(text string) []string {
	return collectMatches(reLCNAFCandidate, text, nil, metadatatools.ValidateLCNAF, metadatatools.NormalizeLCNAF)
}

// ─── FindIdentifiers ────────────────────────────────────────────────────────

/** FindIdentifiers scans text for every identifier type
 * scholarly_identifiers.go supports and returns them grouped by
 * IdentifierType. Each value is normalized via the corresponding
 * metadatatools Normalize* function (see the per-type canonical-form
 * table in towards_a_scholarly_memory_design.md). Types with no matches
 * are omitted from the result map, keeping it compact for storage in
 * the `chunks.identifiers` JSON column.
 *
 * Parameters:
 *   text (string) — the text to scan.
 *
 * Returns:
 *   map[IdentifierType][]string — identifiers found, keyed by type.
 *
 * Example:
 *   ids := FindIdentifiers("DOI: 10.1234/abcd.5678, ORCID: 0000-0003-0900-6903")
 *   // ids[IdentifierDOI]   == []string{"https://doi.org/10.1234/abcd.5678"}
 *   // ids[IdentifierORCID] == []string{"0000-0003-0900-6903"}
 */
func FindIdentifiers(text string) map[IdentifierType][]string {
	result := make(map[IdentifierType][]string)
	add := func(t IdentifierType, values []string) {
		if len(values) > 0 {
			result[t] = values
		}
	}
	add(IdentifierDOI, FindDOIs(text))
	add(IdentifierORCID, FindORCIDs(text))
	add(IdentifierROR, FindRORs(text))
	add(IdentifierRAiD, FindRAiDs(text))
	add(IdentifierArXiv, FindArXivIDs(text))
	add(IdentifierFundRef, FindFundRefIDs(text))
	add(IdentifierISBN, FindISBNs(text))
	add(IdentifierISSN, FindISSNs(text))
	add(IdentifierISNI, FindISNIs(text))
	add(IdentifierPMID, FindPMIDs(text))
	add(IdentifierPMCID, FindPMCIDs(text))
	add(IdentifierVIAF, FindVIAFs(text))
	add(IdentifierSNAC, FindSNACs(text))
	add(IdentifierLCNAF, FindLCNAFs(text))
	return result
}
