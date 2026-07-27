package harvey

import (
	"database/sql"
	"io"
	"os"
	"reflect"
	"testing"
)

func openTestKB(t *testing.T) *KnowledgeBase {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	kb, err := OpenKnowledgeBase(ws, "")
	if err != nil {
		t.Fatalf("OpenKnowledgeBase: %v", err)
	}
	t.Cleanup(func() { kb.Close() })
	return kb
}

// ─── isValidKind ─────────────────────────────────────────────────────────────

func TestIsValidKind(t *testing.T) {
	for _, k := range ValidObservationKinds {
		if !isValidKind(k) {
			t.Errorf("isValidKind(%q) = false, want true", k)
		}
	}
	if isValidKind("bogus") {
		t.Error("isValidKind(\"bogus\") = true, want false")
	}
}

// ─── Projects ────────────────────────────────────────────────────────────────

func TestKnowledgeBase_projects(t *testing.T) {
	kb := openTestKB(t)

	projects, err := kb.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(projects))
	}

	id1, err := kb.AddProject("alpha", "first project")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if id1 == 0 {
		t.Error("expected non-zero ID")
	}

	id2, err := kb.AddProject("beta", "second project")
	if err != nil {
		t.Fatalf("AddProject beta: %v", err)
	}
	if id2 == id1 {
		t.Error("expected different IDs for different projects")
	}

	// Duplicate name should return the existing ID.
	idDup, err := kb.AddProject("alpha", "duplicate")
	if err != nil {
		t.Fatalf("AddProject duplicate: %v", err)
	}
	if idDup != id1 {
		t.Errorf("duplicate AddProject returned id=%d, want %d", idDup, id1)
	}

	projects, err = kb.Projects()
	if err != nil {
		t.Fatalf("Projects after adds: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}
}

// ─── Observations ────────────────────────────────────────────────────────────

func TestKnowledgeBase_observations(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")

	obs, err := kb.Observations(pid)
	if err != nil {
		t.Fatalf("Observations: %v", err)
	}
	if len(obs) != 0 {
		t.Errorf("expected 0 observations, got %d", len(obs))
	}

	id, err := kb.AddObservation(pid, "finding", "WAL mode is faster")
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero observation ID")
	}

	obs, err = kb.Observations(pid)
	if err != nil {
		t.Fatalf("Observations after add: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(obs))
	}
	if obs[0].Body != "WAL mode is faster" {
		t.Errorf("body = %q, want %q", obs[0].Body, "WAL mode is faster")
	}
	if obs[0].Kind != "finding" {
		t.Errorf("kind = %q, want %q", obs[0].Kind, "finding")
	}
}

func TestKnowledgeBase_invalidKind(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")
	if _, err := kb.AddObservation(pid, "nonsense", "body"); err == nil {
		t.Error("expected error for invalid kind")
	}
}

func TestKnowledgeBase_observationSourceDOI(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")

	// AddObservation leaves SourceDOI empty.
	id1, err := kb.AddObservation(pid, "note", "no source")
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	doi := "https://doi.org/10.1234/abcd.5678"
	id2, err := kb.AddObservationWithSource(pid, "finding", "from a paper", doi)
	if err != nil {
		t.Fatalf("AddObservationWithSource: %v", err)
	}

	obs, err := kb.Observations(pid)
	if err != nil {
		t.Fatalf("Observations: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(obs))
	}

	byID := map[int64]Observation{}
	for _, o := range obs {
		byID[o.ID] = o
	}
	if got := byID[id1].SourceDOI; got != "" {
		t.Errorf("observation %d SourceDOI = %q, want \"\"", id1, got)
	}
	if got := byID[id2].SourceDOI; got != doi {
		t.Errorf("observation %d SourceDOI = %q, want %q", id2, got, doi)
	}
}

// ─── Concepts ────────────────────────────────────────────────────────────────

func TestKnowledgeBase_concepts(t *testing.T) {
	kb := openTestKB(t)

	concepts, err := kb.Concepts()
	if err != nil {
		t.Fatalf("Concepts: %v", err)
	}
	if len(concepts) != 0 {
		t.Errorf("expected 0 concepts, got %d", len(concepts))
	}

	id, err := kb.AddConcept("WAL", "write-ahead logging")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}

	// Duplicate name should return the same ID.
	id2, err := kb.AddConcept("WAL", "updated description")
	if err != nil {
		t.Fatalf("AddConcept duplicate: %v", err)
	}
	if id2 != id {
		t.Errorf("duplicate concept returned id=%d, want %d", id2, id)
	}

	concepts, err = kb.Concepts()
	if err != nil {
		t.Fatalf("Concepts after add: %v", err)
	}
	if len(concepts) != 1 {
		t.Errorf("expected 1 concept, got %d", len(concepts))
	}
}

func TestKnowledgeBase_conceptIdentifier(t *testing.T) {
	kb := openTestKB(t)

	orcid := "0000-0003-0900-6903"
	id, err := kb.AddConceptWithIdentifier("Jane Doe", "paper author", string(IdentifierORCID), orcid)
	if err != nil {
		t.Fatalf("AddConceptWithIdentifier: %v", err)
	}

	concepts, err := kb.Concepts()
	if err != nil {
		t.Fatalf("Concepts: %v", err)
	}
	var got Concept
	for _, c := range concepts {
		if c.ID == id {
			got = c
		}
	}
	if got.IdentifierType != string(IdentifierORCID) || got.IdentifierValue != orcid {
		t.Errorf("concept identifier = (%q, %q), want (%q, %q)",
			got.IdentifierType, got.IdentifierValue, string(IdentifierORCID), orcid)
	}

	// A plain AddConcept on the same name must not clear the identifier.
	if _, err := kb.AddConcept("Jane Doe", "updated description"); err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	concepts, err = kb.Concepts()
	if err != nil {
		t.Fatalf("Concepts after update: %v", err)
	}
	for _, c := range concepts {
		if c.ID == id {
			got = c
		}
	}
	if got.Description != "updated description" {
		t.Errorf("description = %q, want %q", got.Description, "updated description")
	}
	if got.IdentifierType != string(IdentifierORCID) || got.IdentifierValue != orcid {
		t.Errorf("identifier was cleared by AddConcept: got (%q, %q), want (%q, %q)",
			got.IdentifierType, got.IdentifierValue, string(IdentifierORCID), orcid)
	}
}

// ─── Links ───────────────────────────────────────────────────────────────────

func TestKnowledgeBase_links(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")
	cid, _ := kb.AddConcept("streaming", "SSE streaming")
	oid, _ := kb.AddObservation(pid, "note", "uses SSE")

	if err := kb.LinkProjectConcept(pid, cid); err != nil {
		t.Fatalf("LinkProjectConcept: %v", err)
	}
	// Duplicate link must be silent.
	if err := kb.LinkProjectConcept(pid, cid); err != nil {
		t.Fatalf("duplicate LinkProjectConcept: %v", err)
	}

	if err := kb.LinkObservationConcept(oid, cid); err != nil {
		t.Fatalf("LinkObservationConcept: %v", err)
	}
}

// ─── Summary ─────────────────────────────────────────────────────────────────

func TestKnowledgeBase_summary_empty(t *testing.T) {
	kb := openTestKB(t)
	s, err := kb.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if s == "" {
		t.Error("expected non-empty summary (at minimum a 'no projects' message)")
	}
}

func TestKnowledgeBase_summary_populated(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("harvey", "terminal agent")
	kb.AddObservation(pid, "finding", "spinner helps UX")

	s, err := kb.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	for _, want := range []string{"harvey", "terminal agent", "spinner helps UX"} {
		if !containsStr(s, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ─── S2 source registry ───────────────────────────────────────────────────────

func TestAddSource_Dedup(t *testing.T) {
	kb := openTestKB(t)
	id1, err := kb.AddSource(Source{Title: "SPARQL 1.1", IdentifierType: "doi", IdentifierValue: "10.1234/sparql"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	// Second call with the same identifier must return the same ID.
	id2, err := kb.AddSource(Source{Title: "Different Title", IdentifierType: "doi", IdentifierValue: "10.1234/sparql"})
	if err != nil {
		t.Fatalf("AddSource (dup): %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same ID on duplicate doi, got id1=%d id2=%d", id1, id2)
	}
}

func TestAddSource_NoIdentifier(t *testing.T) {
	kb := openTestKB(t)
	id1, err := kb.AddSource(Source{Title: "Untitled source"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	// Without an identifier, a second call creates a new row.
	id2, err := kb.AddSource(Source{Title: "Untitled source"})
	if err != nil {
		t.Fatalf("AddSource (second): %v", err)
	}
	if id1 == id2 {
		t.Errorf("expected different IDs for sources without identifiers, got %d", id1)
	}
}

func TestListSources(t *testing.T) {
	kb := openTestKB(t)
	if _, err := kb.AddSource(Source{Title: "Alpha", IdentifierType: "doi", IdentifierValue: "10.1/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := kb.AddSource(Source{Title: "Beta", IdentifierType: "doi", IdentifierValue: "10.1/b"}); err != nil {
		t.Fatal(err)
	}
	sources, err := kb.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
}

func TestShowSource(t *testing.T) {
	kb := openTestKB(t)
	id, err := kb.AddSource(Source{
		Title: "Example Paper", IdentifierType: "doi", IdentifierValue: "10.99/ex",
		Authors: "Jane Doe", Publisher: "ACM",
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := kb.ShowSource(id)
	if err != nil {
		t.Fatalf("ShowSource: %v", err)
	}
	if s.Title != "Example Paper" {
		t.Errorf("Title = %q, want %q", s.Title, "Example Paper")
	}
	if s.Authors != "Jane Doe" {
		t.Errorf("Authors = %q, want %q", s.Authors, "Jane Doe")
	}
}

func TestRemoveSource_Unlinked(t *testing.T) {
	kb := openTestKB(t)
	id, err := kb.AddSource(Source{Title: "Removable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := kb.RemoveSource(id); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	if _, err := kb.ShowSource(id); err == nil {
		t.Error("expected error after removal, got nil")
	}
}

func TestRemoveSource_LinkedFails(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")
	obsID, _ := kb.AddObservation(pid, "note", "some text")
	srcID, err := kb.AddSource(Source{Title: "Linked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := kb.LinkObservationSource(obsID, srcID, "cited"); err != nil {
		t.Fatalf("LinkObservationSource: %v", err)
	}
	if err := kb.RemoveSource(srcID); err == nil {
		t.Error("expected error removing linked source, got nil")
	}
}

func TestRetractSource(t *testing.T) {
	kb := openTestKB(t)
	id, _ := kb.AddSource(Source{Title: "Retractable"})
	if err := kb.RetractSource(id, "Publisher withdrew"); err != nil {
		t.Fatalf("RetractSource: %v", err)
	}
	s, err := kb.ShowSource(id)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Retracted {
		t.Error("expected Retracted=true")
	}
	if s.RetractionNote != "Publisher withdrew" {
		t.Errorf("RetractionNote = %q, want %q", s.RetractionNote, "Publisher withdrew")
	}
}

func TestLinkObservationSource_Duplicate(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")
	obsID, _ := kb.AddObservation(pid, "note", "body text")
	srcID, _ := kb.AddSource(Source{Title: "Source A"})
	if err := kb.LinkObservationSource(obsID, srcID, "cited"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	// Duplicate link must be silently ignored.
	if err := kb.LinkObservationSource(obsID, srcID, "cited"); err != nil {
		t.Fatalf("duplicate link should not fail: %v", err)
	}
}

func TestSourceMigration_ExistingDOI(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")
	doi := "10.1234/migration-test"
	// Insert observation with source_doi directly (simulates pre-S2 data).
	if _, err := kb.db.Exec(
		`INSERT INTO observations (project_id, kind, body, source_doi) VALUES (?, 'note', 'migrated obs', ?)`,
		pid, doi,
	); err != nil {
		t.Fatalf("insert legacy observation: %v", err)
	}
	// Re-open the KB — migration runs on Open.
	kb.Close()
	ws, _ := NewWorkspace(t.TempDir())
	// Reuse same path via a second OpenKnowledgeBase pointing at the same DB file.
	kb2, err := OpenKnowledgeBase(ws, kb.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer kb2.Close()
	sources, err := kb2.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	found := false
	for _, s := range sources {
		if s.IdentifierValue == doi {
			found = true
		}
	}
	if !found {
		t.Errorf("expected migrated source with doi=%q; sources=%v", doi, sources)
	}
}

// ─── S4 observation sources query ────────────────────────────────────────────

func TestObservationSources(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")
	obsID, _ := kb.AddObservation(pid, "finding", "interesting result")
	srcA, _ := kb.AddSource(Source{Title: "Paper A", IdentifierType: "doi", IdentifierValue: "10.1/a"})
	srcB, _ := kb.AddSource(Source{Title: "Paper B", Retracted: false})

	if err := kb.LinkObservationSource(obsID, srcA, "cited"); err != nil {
		t.Fatalf("LinkObservationSource A: %v", err)
	}
	if err := kb.LinkObservationSource(obsID, srcB, "retrieved"); err != nil {
		t.Fatalf("LinkObservationSource B: %v", err)
	}
	// Retract source B so we can verify the warning path.
	if err := kb.RetractSource(srcB, "Test retraction"); err != nil {
		t.Fatalf("RetractSource: %v", err)
	}

	sources, err := kb.ObservationSources(obsID)
	if err != nil {
		t.Fatalf("ObservationSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	foundA, foundBRetracted := false, false
	for _, s := range sources {
		if s.Title == "Paper A" && !s.Retracted {
			foundA = true
		}
		if s.Title == "Paper B" && s.Retracted && s.RetractionNote == "Test retraction" {
			foundBRetracted = true
		}
	}
	if !foundA {
		t.Error("expected non-retracted Paper A in sources")
	}
	if !foundBRetracted {
		t.Error("expected retracted Paper B in sources")
	}
}

func TestCheckRetractions_NoDOISources(t *testing.T) {
	kb := openTestKB(t)
	defer kb.Close()

	// Add a non-DOI source — should not be checked.
	_, err := kb.AddSource(Source{Title: "Web Page", IdentifierType: "url", IdentifierValue: "https://example.com"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	checker := func(doi string) (bool, string, error) {
		t.Errorf("checker called unexpectedly for doi %q", doi)
		return false, "", nil
	}

	checked, updated, err := kb.CheckRetractions(checker, io.Discard)
	if err != nil {
		t.Fatalf("CheckRetractions: %v", err)
	}
	if checked != 0 || updated != 0 {
		t.Errorf("expected 0/0, got checked=%d updated=%d", checked, updated)
	}
}

func TestCheckRetractions_NotRetracted(t *testing.T) {
	kb := openTestKB(t)
	defer kb.Close()

	_, err := kb.AddSource(Source{Title: "Clean Paper", IdentifierType: "doi", IdentifierValue: "10.1234/clean"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	checker := func(doi string) (bool, string, error) { return false, "", nil }

	checked, updated, err := kb.CheckRetractions(checker, io.Discard)
	if err != nil {
		t.Fatalf("CheckRetractions: %v", err)
	}
	if checked != 1 {
		t.Errorf("expected checked=1, got %d", checked)
	}
	if updated != 0 {
		t.Errorf("expected updated=0, got %d", updated)
	}
}

func TestCheckRetractions_Retracted(t *testing.T) {
	kb := openTestKB(t)
	defer kb.Close()

	id, err := kb.AddSource(Source{Title: "Bad Paper", IdentifierType: "doi", IdentifierValue: "10.1234/bad"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	checker := func(doi string) (bool, string, error) {
		return true, "Data fabrication (2024/03/15)", nil
	}

	checked, updated, err := kb.CheckRetractions(checker, io.Discard)
	if err != nil {
		t.Fatalf("CheckRetractions: %v", err)
	}
	if checked != 1 || updated != 1 {
		t.Errorf("expected 1/1, got checked=%d updated=%d", checked, updated)
	}

	s, err := kb.ShowSource(id)
	if err != nil {
		t.Fatalf("ShowSource: %v", err)
	}
	if !s.Retracted {
		t.Error("source should be marked retracted")
	}
	if s.RetractionNote != "Data fabrication (2024/03/15)" {
		t.Errorf("unexpected note: %q", s.RetractionNote)
	}
}

func TestCheckRetractions_SkipsAlreadyRetracted(t *testing.T) {
	kb := openTestKB(t)
	defer kb.Close()

	id, err := kb.AddSource(Source{Title: "Already Retracted", IdentifierType: "doi", IdentifierValue: "10.1234/old"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if err := kb.RetractSource(id, "Prior retraction"); err != nil {
		t.Fatalf("RetractSource: %v", err)
	}

	calls := 0
	checker := func(doi string) (bool, string, error) { calls++; return false, "", nil }

	checked, updated, err := kb.CheckRetractions(checker, io.Discard)
	if err != nil {
		t.Fatalf("CheckRetractions: %v", err)
	}
	if checked != 0 || updated != 0 || calls != 0 {
		t.Errorf("already-retracted source should be skipped: checked=%d updated=%d calls=%d", checked, updated, calls)
	}
}

// ─── UUID migration ──────────────────────────────────────────────────────────

// reopenTestKB closes kb and reopens the same underlying database file,
// so the lazy-migration and backfill logic in OpenKnowledgeBase runs again.
// Mirrors the reopen pattern in TestSourceMigration_ExistingDOI.
func reopenTestKB(t *testing.T, kb *KnowledgeBase) *KnowledgeBase {
	t.Helper()
	path := kb.path
	kb.Close()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	kb2, err := OpenKnowledgeBase(ws, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { kb2.Close() })
	return kb2
}

// projectUUIDs returns id -> uuid for every row in projects.
func projectUUIDs(t *testing.T, kb *KnowledgeBase) map[int64]string {
	t.Helper()
	rows, err := kb.db.Query(`SELECT id, uuid FROM projects`)
	if err != nil {
		t.Fatalf("query projects: %v", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var u string
		if err := rows.Scan(&id, &u); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[id] = u
	}
	return out
}

func TestUUIDBackfill_AssignsDistinctUUIDs(t *testing.T) {
	kb := openTestKB(t)

	pres, err := kb.db.Exec(`INSERT INTO projects (name) VALUES ('legacy-proj')`)
	if err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}
	pid, _ := pres.LastInsertId()

	if _, err := kb.db.Exec(
		`INSERT INTO observations (project_id, kind, body) VALUES (?, 'note', 'legacy obs')`, pid,
	); err != nil {
		t.Fatalf("insert legacy observation: %v", err)
	}
	if _, err := kb.db.Exec(`INSERT INTO concepts (name) VALUES ('legacy-concept')`); err != nil {
		t.Fatalf("insert legacy concept: %v", err)
	}
	if _, err := kb.db.Exec(`INSERT INTO sources (title) VALUES ('legacy-source')`); err != nil {
		t.Fatalf("insert legacy source: %v", err)
	}

	kb2 := reopenTestKB(t, kb)

	seen := map[string]bool{}
	for _, table := range []string{"projects", "observations", "concepts", "sources"} {
		rows, err := kb2.db.Query(`SELECT uuid FROM ` + table)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		count := 0
		for rows.Next() {
			var u string
			if err := rows.Scan(&u); err != nil {
				rows.Close()
				t.Fatalf("scan %s: %v", table, err)
			}
			count++
			if u == "" {
				t.Errorf("%s: found row with empty uuid", table)
			}
			if seen[u] {
				t.Errorf("%s: duplicate uuid %q", table, u)
			}
			seen[u] = true
		}
		rows.Close()
		if count == 0 {
			t.Errorf("%s: expected at least one seeded row", table)
		}
	}
}

func TestUUIDBackfill_Idempotent(t *testing.T) {
	kb := openTestKB(t)
	if _, err := kb.db.Exec(`INSERT INTO projects (name) VALUES ('legacy-proj')`); err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}

	kb2 := reopenTestKB(t, kb)
	first := projectUUIDs(t, kb2)
	if len(first) != 1 || first[1] == "" {
		t.Fatalf("expected one project with a non-empty uuid after first backfill, got %v", first)
	}

	kb3 := reopenTestKB(t, kb2)
	second := projectUUIDs(t, kb3)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("uuid changed across reopen: first=%v second=%v", first, second)
	}
}

func TestUUIDBackfill_PreservesExistingIDsAndFKs(t *testing.T) {
	kb := openTestKB(t)

	pres, err := kb.db.Exec(`INSERT INTO projects (name) VALUES ('legacy-proj')`)
	if err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}
	pid, _ := pres.LastInsertId()

	ores, err := kb.db.Exec(
		`INSERT INTO observations (project_id, kind, body) VALUES (?, 'note', 'legacy obs')`, pid,
	)
	if err != nil {
		t.Fatalf("insert legacy observation: %v", err)
	}
	oid, _ := ores.LastInsertId()

	cres, err := kb.db.Exec(`INSERT INTO concepts (name) VALUES ('legacy-concept')`)
	if err != nil {
		t.Fatalf("insert legacy concept: %v", err)
	}
	cid, _ := cres.LastInsertId()

	if _, err := kb.db.Exec(
		`INSERT INTO observation_concepts (observation_id, concept_id) VALUES (?, ?)`, oid, cid,
	); err != nil {
		t.Fatalf("insert legacy link: %v", err)
	}

	kb2 := reopenTestKB(t, kb)

	var gotPID int64
	if err := kb2.db.QueryRow(`SELECT id FROM projects WHERE name = 'legacy-proj'`).Scan(&gotPID); err != nil {
		t.Fatalf("select project: %v", err)
	}
	if gotPID != pid {
		t.Errorf("project id changed across backfill: got %d, want %d", gotPID, pid)
	}

	var gotOID int64
	if err := kb2.db.QueryRow(`SELECT id FROM observations WHERE body = 'legacy obs'`).Scan(&gotOID); err != nil {
		t.Fatalf("select observation: %v", err)
	}
	if gotOID != oid {
		t.Errorf("observation id changed across backfill: got %d, want %d", gotOID, oid)
	}

	var gotCID int64
	if err := kb2.db.QueryRow(`SELECT id FROM concepts WHERE name = 'legacy-concept'`).Scan(&gotCID); err != nil {
		t.Fatalf("select concept: %v", err)
	}
	if gotCID != cid {
		t.Errorf("concept id changed across backfill: got %d, want %d", gotCID, cid)
	}

	var linkCount int
	if err := kb2.db.QueryRow(
		`SELECT COUNT(*) FROM observation_concepts WHERE observation_id = ? AND concept_id = ?`, oid, cid,
	).Scan(&linkCount); err != nil {
		t.Fatalf("select link: %v", err)
	}
	if linkCount != 1 {
		t.Errorf("expected join row (observation_id=%d, concept_id=%d) to survive backfill untouched, got count=%d", oid, cid, linkCount)
	}
}

func TestUUIDBackfill_OriginHostSentinel(t *testing.T) {
	kb := openTestKB(t)
	if _, err := kb.db.Exec(`INSERT INTO projects (name) VALUES ('legacy-proj')`); err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}

	kb2 := reopenTestKB(t, kb)

	var originHost string
	if err := kb2.db.QueryRow(
		`SELECT origin_host FROM projects WHERE name = 'legacy-proj'`,
	).Scan(&originHost); err != nil {
		t.Fatalf("select origin_host: %v", err)
	}
	if originHost != "unknown" {
		t.Errorf("origin_host = %q, want %q", originHost, "unknown")
	}
}

func TestAddProject_SetsUUIDAndOriginHost(t *testing.T) {
	kb := openTestKB(t)
	id, err := kb.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	var u, host string
	if err := kb.db.QueryRow(`SELECT uuid, origin_host FROM projects WHERE id = ?`, id).Scan(&u, &host); err != nil {
		t.Fatalf("select: %v", err)
	}
	if u == "" {
		t.Error("expected non-empty uuid")
	}
	if host == "" || host == "unknown" {
		t.Errorf("expected real hostname, got %q", host)
	}
}

func TestAddObservationWithSource_SetsUUIDAndOriginHost(t *testing.T) {
	kb := openTestKB(t)
	pid, _ := kb.AddProject("proj", "")
	id, err := kb.AddObservationWithSource(pid, "note", "body", "")
	if err != nil {
		t.Fatalf("AddObservationWithSource: %v", err)
	}
	var u, host string
	if err := kb.db.QueryRow(`SELECT uuid, origin_host FROM observations WHERE id = ?`, id).Scan(&u, &host); err != nil {
		t.Fatalf("select: %v", err)
	}
	if u == "" {
		t.Error("expected non-empty uuid")
	}
	if host == "" || host == "unknown" {
		t.Errorf("expected real hostname, got %q", host)
	}
}

func TestAddConceptWithIdentifier_SetsUUIDAndOriginHost(t *testing.T) {
	kb := openTestKB(t)
	id, err := kb.AddConceptWithIdentifier("concept", "", "", "")
	if err != nil {
		t.Fatalf("AddConceptWithIdentifier: %v", err)
	}
	var u, host string
	if err := kb.db.QueryRow(`SELECT uuid, origin_host FROM concepts WHERE id = ?`, id).Scan(&u, &host); err != nil {
		t.Fatalf("select: %v", err)
	}
	if u == "" {
		t.Error("expected non-empty uuid")
	}
	if host == "" || host == "unknown" {
		t.Errorf("expected real hostname, got %q", host)
	}
}

func TestAddSource_SetsUUIDAndOriginHost(t *testing.T) {
	kb := openTestKB(t)
	id, err := kb.AddSource(Source{Title: "src"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	var u, host string
	if err := kb.db.QueryRow(`SELECT uuid, origin_host FROM sources WHERE id = ?`, id).Scan(&u, &host); err != nil {
		t.Fatalf("select: %v", err)
	}
	if u == "" {
		t.Error("expected non-empty uuid")
	}
	if host == "" || host == "unknown" {
		t.Errorf("expected real hostname, got %q", host)
	}
}

func TestAddProject_ConflictPreservesOriginalUUID(t *testing.T) {
	kb := openTestKB(t)
	id1, err := kb.AddProject("proj", "first")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	var firstUUID string
	if err := kb.db.QueryRow(`SELECT uuid FROM projects WHERE id = ?`, id1).Scan(&firstUUID); err != nil {
		t.Fatalf("select: %v", err)
	}

	id2, err := kb.AddProject("proj", "second")
	if err != nil {
		t.Fatalf("AddProject (conflict): %v", err)
	}
	if id2 != id1 {
		t.Fatalf("expected same project id on name conflict, got %d vs %d", id2, id1)
	}
	var secondUUID string
	if err := kb.db.QueryRow(`SELECT uuid FROM projects WHERE id = ?`, id2).Scan(&secondUUID); err != nil {
		t.Fatalf("select: %v", err)
	}
	if secondUUID != firstUUID {
		t.Errorf("uuid changed on conflict: first=%q second=%q", firstUUID, secondUUID)
	}
}

// TestOpenKnowledgeBase_BackfillsLegacyConceptsCreatedAt reproduces a real
// database found on a second machine (macmini-rd.local): a concepts table
// created before created_at was added to the base schema's CREATE TABLE
// statement. Since CREATE TABLE IF NOT EXISTS never retrofits an existing
// table, and kbAlterStmts had no ALTER for this column, such a table never
// gained created_at on later opens. This must be backfilled the same way
// identifier_type/identifier_value/uuid/origin_host already are.
func TestOpenKnowledgeBase_BackfillsLegacyConceptsCreatedAt(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/agents/knowledge.db"
	if err := os.MkdirAll(dir+"/agents", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE concepts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		name        TEXT UNIQUE NOT NULL,
		description TEXT
	)`); err != nil {
		t.Fatalf("create legacy concepts table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO concepts (name, description) VALUES ('legacy-concept', 'seeded before created_at existed')`); err != nil {
		t.Fatalf("seed legacy concept: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	kb, err := OpenKnowledgeBase(ws, dbPath)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase on legacy concepts table: %v", err)
	}
	defer kb.Close()

	var createdAt sql.NullString
	if err := kb.db.QueryRow(`SELECT created_at FROM concepts WHERE name = 'legacy-concept'`).Scan(&createdAt); err != nil {
		t.Fatalf("select created_at: %v", err)
	}
	if !createdAt.Valid || createdAt.String == "" {
		t.Errorf("expected legacy row's created_at to be backfilled to a non-empty value, got %+v", createdAt)
	}
}
