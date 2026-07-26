package harvey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollisionReport_DetectsNameUUIDMismatch(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)

	if _, err := a.AddProject("harvey", "on a"); err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	if _, err := b.AddProject("harvey", "on b"); err != nil {
		t.Fatalf("AddProject b: %v", err)
	}

	collisions, err := CollisionReport(a.Path(), b.Path())
	if err != nil {
		t.Fatalf("CollisionReport: %v", err)
	}
	if len(collisions) != 1 {
		t.Fatalf("expected 1 collision, got %d: %v", len(collisions), collisions)
	}
	c := collisions[0]
	if c.Table != "projects" || c.Name != "harvey" {
		t.Errorf("unexpected collision: %+v", c)
	}
	if c.UUIDA == "" || c.UUIDB == "" || c.UUIDA == c.UUIDB {
		t.Errorf("expected two distinct non-empty uuids, got a=%q b=%q", c.UUIDA, c.UUIDB)
	}
}

func TestCollisionReport_NoCollisionWhenUUIDsMatch(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)

	aid, err := a.AddProject("harvey", "on a")
	if err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	if _, err := b.AddProject("harvey", "on b"); err != nil {
		t.Fatalf("AddProject b: %v", err)
	}

	var aUUID string
	if err := a.db.QueryRow(`SELECT uuid FROM projects WHERE id = ?`, aid).Scan(&aUUID); err != nil {
		t.Fatalf("select a uuid: %v", err)
	}
	// Simulate a row already reconciled by a prior merge: b's copy shares a's uuid.
	if _, err := b.db.Exec(`UPDATE projects SET uuid = ? WHERE name = 'harvey'`, aUUID); err != nil {
		t.Fatalf("update b uuid: %v", err)
	}

	collisions, err := CollisionReport(a.Path(), b.Path())
	if err != nil {
		t.Fatalf("CollisionReport: %v", err)
	}
	if len(collisions) != 0 {
		t.Errorf("expected 0 collisions, got %d: %v", len(collisions), collisions)
	}
}

func TestCollisionReport_ConceptsAlsoChecked(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)

	if _, err := a.AddConcept("RAG", "on a"); err != nil {
		t.Fatalf("AddConcept a: %v", err)
	}
	if _, err := b.AddConcept("RAG", "on b"); err != nil {
		t.Fatalf("AddConcept b: %v", err)
	}

	collisions, err := CollisionReport(a.Path(), b.Path())
	if err != nil {
		t.Fatalf("CollisionReport: %v", err)
	}
	if len(collisions) != 1 || collisions[0].Table != "concepts" || collisions[0].Name != "RAG" {
		t.Errorf("unexpected collisions: %v", collisions)
	}
}

func TestCollisionReport_EmptyWhenNoOverlap(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)

	if _, err := a.AddProject("proj-a", ""); err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	if _, err := b.AddProject("proj-b", ""); err != nil {
		t.Fatalf("AddProject b: %v", err)
	}

	collisions, err := CollisionReport(a.Path(), b.Path())
	if err != nil {
		t.Fatalf("CollisionReport: %v", err)
	}
	if len(collisions) != 0 {
		t.Errorf("expected 0 collisions, got %d: %v", len(collisions), collisions)
	}
}

func TestMergeKnowledgeBases_RejectsExistingMergedPath(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)

	mergedPath := filepath.Join(t.TempDir(), "merged.db")
	if err := os.WriteFile(mergedPath, []byte{}, 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	if _, err := MergeKnowledgeBases(a.Path(), b.Path(), mergedPath); err == nil {
		t.Fatal("expected error when mergedPath already exists, got nil")
	}

	info, err := os.Stat(mergedPath)
	if err != nil {
		t.Fatalf("stat mergedPath: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected mergedPath to remain untouched (0 bytes), got %d", info.Size())
	}
}

func TestMergeKnowledgeBases_CreatesMigratedSchema(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)

	mergedPath := filepath.Join(t.TempDir(), "merged.db")
	if _, err := MergeKnowledgeBases(a.Path(), b.Path(), mergedPath); err != nil {
		t.Fatalf("MergeKnowledgeBases: %v", err)
	}

	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	merged, err := OpenKnowledgeBase(ws, mergedPath)
	if err != nil {
		t.Fatalf("open merged: %v", err)
	}
	defer merged.Close()

	projects, err := merged.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if projects == nil {
		t.Error("expected empty (not nil) projects slice from a fully-migrated merged db")
	}
}

func openMergedTestKB(t *testing.T, a, b *KnowledgeBase) *KnowledgeBase {
	t.Helper()
	mergedPath := filepath.Join(t.TempDir(), "merged.db")
	if _, err := MergeKnowledgeBases(a.Path(), b.Path(), mergedPath); err != nil {
		t.Fatalf("MergeKnowledgeBases: %v", err)
	}
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	merged, err := OpenKnowledgeBase(ws, mergedPath)
	if err != nil {
		t.Fatalf("open merged: %v", err)
	}
	t.Cleanup(func() { merged.Close() })
	return merged
}

func TestMergeKnowledgeBases_ProjectsUnion(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	if _, err := a.AddProject("one", ""); err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	if _, err := b.AddProject("two", ""); err != nil {
		t.Fatalf("AddProject b: %v", err)
	}

	merged := openMergedTestKB(t, a, b)
	projects, err := merged.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(projects), projects)
	}
	names := map[string]bool{}
	for _, p := range projects {
		names[p.Name] = true
		if p.ID == 0 {
			t.Errorf("expected a fresh non-zero merged id for project %q", p.Name)
		}
	}
	if !names["one"] || !names["two"] {
		t.Errorf("expected both projects present, got %v", names)
	}
}

func TestMergeKnowledgeBases_ProjectsDedupSharedUUID(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	aid, err := a.AddProject("shared", "on a")
	if err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	var aUUID string
	if err := a.db.QueryRow(`SELECT uuid FROM projects WHERE id = ?`, aid).Scan(&aUUID); err != nil {
		t.Fatalf("select a uuid: %v", err)
	}
	if _, err := b.AddProject("shared", "on b"); err != nil {
		t.Fatalf("AddProject b: %v", err)
	}
	if _, err := b.db.Exec(`UPDATE projects SET uuid = ? WHERE name = 'shared'`, aUUID); err != nil {
		t.Fatalf("update b uuid: %v", err)
	}

	merged := openMergedTestKB(t, a, b)
	projects, err := merged.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 deduped project, got %d: %v", len(projects), projects)
	}
}

func TestMergeKnowledgeBases_ConceptsAndSourcesUnion(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	if _, err := a.AddConcept("concept-a", ""); err != nil {
		t.Fatalf("AddConcept a: %v", err)
	}
	if _, err := b.AddConcept("concept-b", ""); err != nil {
		t.Fatalf("AddConcept b: %v", err)
	}
	if _, err := a.AddSource(Source{Title: "source-a"}); err != nil {
		t.Fatalf("AddSource a: %v", err)
	}
	if _, err := b.AddSource(Source{Title: "source-b"}); err != nil {
		t.Fatalf("AddSource b: %v", err)
	}

	merged := openMergedTestKB(t, a, b)

	concepts, err := merged.Concepts()
	if err != nil {
		t.Fatalf("Concepts: %v", err)
	}
	if len(concepts) != 2 {
		t.Fatalf("expected 2 concepts, got %d: %v", len(concepts), concepts)
	}

	sources, err := merged.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d: %v", len(sources), sources)
	}
}

func TestMergeKnowledgeBases_ObservationsTranslateProjectID(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	pid, err := a.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	if _, err := a.AddObservation(pid, "note", "obs on a"); err != nil {
		t.Fatalf("AddObservation a: %v", err)
	}

	merged := openMergedTestKB(t, a, b)

	var mergedPID int64
	if err := merged.db.QueryRow(`SELECT id FROM projects WHERE name = 'proj'`).Scan(&mergedPID); err != nil {
		t.Fatalf("select merged project id: %v", err)
	}

	var gotPID int64
	if err := merged.db.QueryRow(`SELECT project_id FROM observations WHERE body = 'obs on a'`).Scan(&gotPID); err != nil {
		t.Fatalf("select observation project_id: %v", err)
	}
	if gotPID != mergedPID {
		t.Errorf("observation.project_id = %d, want merged project id %d (source id was %d)", gotPID, mergedPID, pid)
	}
}

func TestMergeKnowledgeBases_ObservationsDedupSharedUUID(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	apid, err := a.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	oid, err := a.AddObservation(apid, "note", "shared obs")
	if err != nil {
		t.Fatalf("AddObservation a: %v", err)
	}
	var oUUID string
	if err := a.db.QueryRow(`SELECT uuid FROM observations WHERE id = ?`, oid).Scan(&oUUID); err != nil {
		t.Fatalf("select observation uuid: %v", err)
	}

	bpid, err := b.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject b: %v", err)
	}
	// Same project by name (dedups to one merged project), and the same
	// observation (by uuid) already present on both, simulating a row
	// already reconciled by a prior merge.
	if _, err := b.db.Exec(
		`INSERT INTO observations (project_id, kind, body, uuid, origin_host) VALUES (?, 'note', 'shared obs', ?, 'unknown')`,
		bpid, oUUID,
	); err != nil {
		t.Fatalf("insert b observation: %v", err)
	}

	merged := openMergedTestKB(t, a, b)
	var count int
	if err := merged.db.QueryRow(`SELECT COUNT(*) FROM observations WHERE body = 'shared obs'`).Scan(&count); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 deduped observation, got %d", count)
	}
}

func TestMergeKnowledgeBases_ObservationConceptsSurvive(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	pid, err := a.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	oid, err := a.AddObservation(pid, "note", "linked obs")
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	cid, err := a.AddConcept("concept", "")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	if err := a.LinkObservationConcept(oid, cid); err != nil {
		t.Fatalf("LinkObservationConcept: %v", err)
	}

	merged := openMergedTestKB(t, a, b)
	var count int
	if err := merged.db.QueryRow(`
		SELECT COUNT(*) FROM observation_concepts j
		JOIN observations o ON o.id = j.observation_id
		JOIN concepts     c ON c.id = j.concept_id
		WHERE o.body = 'linked obs' AND c.name = 'concept'`,
	).Scan(&count); err != nil {
		t.Fatalf("count link: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 observation_concepts link in merged db, got %d", count)
	}
}

func TestMergeKnowledgeBases_ProjectConceptsSurvive(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	pid, err := a.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	cid, err := a.AddConcept("concept", "")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	if err := a.LinkProjectConcept(pid, cid); err != nil {
		t.Fatalf("LinkProjectConcept: %v", err)
	}

	merged := openMergedTestKB(t, a, b)
	var count int
	if err := merged.db.QueryRow(`
		SELECT COUNT(*) FROM project_concepts j
		JOIN projects p ON p.id = j.project_id
		JOIN concepts c ON c.id = j.concept_id
		WHERE p.name = 'proj' AND c.name = 'concept'`,
	).Scan(&count); err != nil {
		t.Fatalf("count link: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 project_concepts link in merged db, got %d", count)
	}
}

func TestMergeKnowledgeBases_ObservationSourcesSurvive(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	pid, err := a.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	oid, err := a.AddObservation(pid, "note", "sourced obs")
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	sid, err := a.AddSource(Source{Title: "src"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if err := a.LinkObservationSource(oid, sid, "supports"); err != nil {
		t.Fatalf("LinkObservationSource: %v", err)
	}

	merged := openMergedTestKB(t, a, b)
	var relationship string
	if err := merged.db.QueryRow(`
		SELECT j.relationship FROM observation_sources j
		JOIN observations o ON o.id = j.observation_id
		JOIN sources      s ON s.id = j.source_id
		WHERE o.body = 'sourced obs' AND s.title = 'src'`,
	).Scan(&relationship); err != nil {
		t.Fatalf("select link: %v", err)
	}
	if relationship != "supports" {
		t.Errorf("relationship = %q, want %q", relationship, "supports")
	}
}

func TestMergeKnowledgeBases_SummaryCounts(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)

	aid, err := a.AddProject("shared", "on a")
	if err != nil {
		t.Fatalf("AddProject a shared: %v", err)
	}
	if _, err := a.AddProject("only-a", ""); err != nil {
		t.Fatalf("AddProject a only-a: %v", err)
	}
	var aUUID string
	if err := a.db.QueryRow(`SELECT uuid FROM projects WHERE id = ?`, aid).Scan(&aUUID); err != nil {
		t.Fatalf("select a uuid: %v", err)
	}
	if _, err := b.AddProject("shared", "on b"); err != nil {
		t.Fatalf("AddProject b shared: %v", err)
	}
	if _, err := b.db.Exec(`UPDATE projects SET uuid = ? WHERE name = 'shared'`, aUUID); err != nil {
		t.Fatalf("update b uuid: %v", err)
	}

	mergedPath := filepath.Join(t.TempDir(), "merged.db")
	summary, err := MergeKnowledgeBases(a.Path(), b.Path(), mergedPath)
	if err != nil {
		t.Fatalf("MergeKnowledgeBases: %v", err)
	}

	var projSummary *MergeTableSummary
	for i := range summary {
		if summary[i].Table == "projects" {
			projSummary = &summary[i]
		}
	}
	if projSummary == nil {
		t.Fatalf("no summary entry for projects: %v", summary)
	}
	if projSummary.FromA != 2 || projSummary.FromB != 1 || projSummary.Merged != 2 {
		t.Errorf("projects summary = %+v, want FromA=2 FromB=1 Merged=2", projSummary)
	}
}

func TestMergeKnowledgeBases_FTSPopulatedAfterMerge(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	pid, err := a.AddProject("proj", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if _, err := a.AddObservation(pid, "note", "findable via fts search term"); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	merged := openMergedTestKB(t, a, b)
	results, err := merged.Search("findable")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected Search to find the merged observation via FTS, got 0 results")
	}
}

func TestReconcileCollisions_RewritesUUIDToMatchA(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	if _, err := a.AddProject("harvey", "on a"); err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	if _, err := b.AddProject("harvey", "on b"); err != nil {
		t.Fatalf("AddProject b: %v", err)
	}

	collisions, err := CollisionReport(a.Path(), b.Path())
	if err != nil {
		t.Fatalf("CollisionReport: %v", err)
	}
	if len(collisions) != 1 {
		t.Fatalf("expected 1 collision, got %d", len(collisions))
	}

	if err := ReconcileCollisions(b.Path(), collisions); err != nil {
		t.Fatalf("ReconcileCollisions: %v", err)
	}

	var bUUID string
	if err := b.db.QueryRow(`SELECT uuid FROM projects WHERE name = 'harvey'`).Scan(&bUUID); err != nil {
		t.Fatalf("select b uuid: %v", err)
	}
	if bUUID != collisions[0].UUIDA {
		t.Errorf("b uuid = %q, want a's uuid %q", bUUID, collisions[0].UUIDA)
	}

	remaining, err := CollisionReport(a.Path(), b.Path())
	if err != nil {
		t.Fatalf("CollisionReport after reconcile: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 collisions after reconcile, got %d: %v", len(remaining), remaining)
	}
}

func TestReconcileCollisions_PreservesChildObservationsAfterMerge(t *testing.T) {
	a := openTestKB(t)
	b := openTestKB(t)
	apid, err := a.AddProject("harvey", "on a")
	if err != nil {
		t.Fatalf("AddProject a: %v", err)
	}
	if _, err := a.AddObservation(apid, "note", "obs on a"); err != nil {
		t.Fatalf("AddObservation a: %v", err)
	}
	bpid, err := b.AddProject("harvey", "on b")
	if err != nil {
		t.Fatalf("AddProject b: %v", err)
	}
	if _, err := b.AddObservation(bpid, "note", "obs on b"); err != nil {
		t.Fatalf("AddObservation b: %v", err)
	}

	collisions, err := CollisionReport(a.Path(), b.Path())
	if err != nil {
		t.Fatalf("CollisionReport: %v", err)
	}
	if err := ReconcileCollisions(b.Path(), collisions); err != nil {
		t.Fatalf("ReconcileCollisions: %v", err)
	}

	merged := openMergedTestKB(t, a, b)

	projects, err := merged.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 merged project (reconciled), got %d: %v", len(projects), projects)
	}

	var obsCount int
	if err := merged.db.QueryRow(`SELECT COUNT(*) FROM observations WHERE body IN ('obs on a', 'obs on b')`).Scan(&obsCount); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if obsCount != 2 {
		t.Errorf("expected both observations to survive the merge after reconciliation, got %d", obsCount)
	}
}
