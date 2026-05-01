package harvey

import (
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
