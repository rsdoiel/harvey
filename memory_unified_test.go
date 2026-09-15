package harvey

import (
	"fmt"
	"strings"
	"testing"

	knowledge "github.com/rsdoiel/knowledge"
)

func TestFormatContext_Empty(t *testing.T) {
	if got := FormatContext(nil); got != "" {
		t.Errorf("FormatContext(nil) = %q, want \"\"", got)
	}
	if got := FormatContext([]UnifiedResult{}); got != "" {
		t.Errorf("FormatContext([]) = %q, want \"\"", got)
	}
}

func TestFormatContext_SingleSource(t *testing.T) {
	results := []UnifiedResult{
		{Source: string(MemoryTypeWorkspaceProfile), ID: "wp1", Content: "Harvey workspace", Score: 1.0},
	}
	got := FormatContext(results)
	if !strings.Contains(got, "[workspace profile]") {
		t.Errorf("missing [workspace profile] section:\n%s", got)
	}
	if !strings.Contains(got, "Harvey workspace") {
		t.Errorf("missing content:\n%s", got)
	}
	if !strings.Contains(got, "[memory context]") {
		t.Errorf("missing [memory context] header:\n%s", got)
	}
}

func TestFormatContext_MultiSource(t *testing.T) {
	results := []UnifiedResult{
		{Source: string(MemoryTypeWorkspaceProfile), ID: "wp1", Content: "workspace info", Score: 1.0},
		{Source: "experiential", ID: "exp1", Content: "tool_use: git tip", Score: 0.9},
		{Source: "rag", ID: "rag:1", Content: "some chunk text", Score: 0.8},
	}
	got := FormatContext(results)
	for _, want := range []string{"[workspace profile]", "[relevant experience]", "[knowledge]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestFormatContext_GroupsBySource(t *testing.T) {
	results := []UnifiedResult{
		{Source: "experiential", ID: "a", Content: "first"},
		{Source: "experiential", ID: "b", Content: "second"},
		{Source: "rag", ID: "rag:1", Content: "chunk"},
	}
	got := FormatContext(results)
	// [relevant experience] should appear exactly once.
	if count := strings.Count(got, "[relevant experience]"); count != 1 {
		t.Errorf("[relevant experience] appears %d times, want 1:\n%s", count, got)
	}
}

func TestRecall_EmptyStore(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := DefaultConfig()
	um := NewUnifiedMemory(store, &cfg.Memory, ws)
	results, err := um.Recall("git error", nil, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}
}

func TestRecall_FactualOnly(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	doc := NewMemoryDoc("wp_001", MemoryTypeWorkspaceProfile,
		"Harvey is a Go coding assistant",
		"Used for editing Go files and running tests.", []string{"go", "coding"})
	doc.FountainBody = BuildFountainBody("2026-05-26 10:00:00", nil)
	if err := store.Save(doc, nil); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	um := NewUnifiedMemory(store, &cfg.Memory, ws)
	results, err := um.Recall("", nil, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 factual result, got %d", len(results))
	}
	if results[0].Source != string(MemoryTypeWorkspaceProfile) {
		t.Errorf("source: got %q, want %q", results[0].Source, MemoryTypeWorkspaceProfile)
	}
	if results[0].Score != 1.0 {
		t.Errorf("score: got %f, want 1.0", results[0].Score)
	}
}

func TestRecall_FactualBeforeExperiential(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Save one of each type.
	wpDoc := NewMemoryDoc("wp_001", MemoryTypeWorkspaceProfile,
		"This is the workspace profile", "", nil)
	wpDoc.FountainBody = BuildFountainBody("2026-05-26 10:00:00", nil)
	if err := store.Save(wpDoc, nil); err != nil {
		t.Fatal(err)
	}
	tuDoc := NewMemoryDoc("tu_001", MemoryTypeToolUse,
		"git init fixes not a repository error", "", nil)
	tuDoc.FountainBody = BuildFountainBody("2026-05-26 10:00:00", nil)
	if err := store.Save(tuDoc, nil); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	um := NewUnifiedMemory(store, &cfg.Memory, ws)
	results, err := um.Recall("git error", nil, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least one result")
	}
	// Factual type must come first.
	if results[0].Source != string(MemoryTypeWorkspaceProfile) {
		t.Errorf("first result source: got %q, want %q",
			results[0].Source, MemoryTypeWorkspaceProfile)
	}
}

func TestRecall_BudgetTruncation(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Each description is 45 chars → 11 tokens (45/4=11). Save two docs.
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("wp_%03d", i+1)
		desc := fmt.Sprintf("workspace profile entry number %d with content", i+1)
		doc := NewMemoryDoc(id, MemoryTypeWorkspaceProfile, desc, "", nil)
		doc.FountainBody = BuildFountainBody("2026-05-26 10:00:00", nil)
		if err := store.Save(doc, nil); err != nil {
			t.Fatal(err)
		}
	}

	cfg := DefaultConfig()
	um := NewUnifiedMemory(store, &cfg.Memory, ws)

	// Budget=11 fits the first result (11 tokens) but not both (22 > 11).
	results, err := um.Recall("", nil, 11)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("budget=11: expected 1 result, got %d", len(results))
	}
}

func TestRecall_NoBudget(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("wp_%03d", i+1)
		doc := NewMemoryDoc(id, MemoryTypeWorkspaceProfile,
			fmt.Sprintf("profile entry %d", i+1), "", nil)
		doc.FountainBody = BuildFountainBody("2026-05-26 10:00:00", nil)
		if err := store.Save(doc, nil); err != nil {
			t.Fatal(err)
		}
	}

	cfg := DefaultConfig()
	um := NewUnifiedMemory(store, &cfg.Memory, ws)
	results, err := um.Recall("", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("budget=0 (no limit): expected 3 results, got %d", len(results))
	}
}

func TestSourceHeader(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{string(MemoryTypeWorkspaceProfile), "workspace profile"},
		{string(MemoryTypeProjectFact), "project facts"},
		{"experiential", "relevant experience"},
		{"rag", "knowledge"},
		{"kb", "project knowledge"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		if got := sourceHeader(tt.source); got != tt.want {
			t.Errorf("sourceHeader(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

// ── Phase 2b: adaptive budget tuning ─────────────────────────────────────────

func TestRecordSessionStats_Basic(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.RecordSessionStats("session-001.spmd", 512, 128, false, 14.5); err != nil {
		t.Fatalf("RecordSessionStats: %v", err)
	}
	n, err := store.StatsCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("StatsCount after 1 record: got %d, want 1", n)
	}
}

func TestBudgetStats_BelowMinimum(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Fewer than 10 sessions — BudgetStats should return zeros.
	for i := 0; i < 5; i++ {
		if err := store.RecordSessionStats(fmt.Sprintf("s%d.spmd", i), 512, 100, false, 10.0); err != nil {
			t.Fatal(err)
		}
	}
	n, _ := store.StatsCount()
	if n != 5 {
		t.Fatalf("StatsCount: got %d, want 5", n)
	}
	avgSat, compRate, avgTps, err := store.BudgetStats(10)
	if err != nil {
		t.Fatal(err)
	}
	// Only 5 rows, but BudgetStats(10) returns what's available.
	// avgSat = (100/512)*5/5 ≈ 0.195
	if avgSat <= 0 {
		t.Errorf("avgSaturation: got %f, want > 0", avgSat)
	}
	if compRate != 0 {
		t.Errorf("compressionRate: got %f, want 0 (no compressed sessions)", compRate)
	}
	if avgTps != 10.0 {
		t.Errorf("avgToksPerSec: got %f, want 10.0", avgTps)
	}
}

func TestBudgetStats_HighSaturation(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 10 sessions at 95% saturation, good throughput.
	for i := 0; i < 10; i++ {
		if err := store.RecordSessionStats(fmt.Sprintf("s%d.spmd", i), 512, 486, false, 15.0); err != nil {
			t.Fatal(err)
		}
	}
	avgSat, _, _, err := store.BudgetStats(10)
	if err != nil {
		t.Fatal(err)
	}
	// 486/512 ≈ 0.949
	if avgSat < 0.90 {
		t.Errorf("avgSaturation: got %f, want >= 0.90 (high saturation scenario)", avgSat)
	}
}

func TestBudgetStats_CompressionTracked(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 10 sessions, 7 with compression.
	for i := 0; i < 10; i++ {
		compressed := i < 7
		if err := store.RecordSessionStats(fmt.Sprintf("s%d.spmd", i), 512, 256, compressed, 12.0); err != nil {
			t.Fatal(err)
		}
	}
	_, compRate, _, err := store.BudgetStats(10)
	if err != nil {
		t.Fatal(err)
	}
	if compRate < 0.69 || compRate > 0.71 {
		t.Errorf("compressionRate: got %f, want ~0.70", compRate)
	}
}

func TestStatsCount_Empty(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	n, err := store.StatsCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("StatsCount on empty store: got %d, want 0", n)
	}
}

// ─── recallKB: concept-tag primary path, substring fallback ──────────────────

// newTestUnifiedMemoryWithKB returns a UnifiedMemory backed by a live
// KnowledgeBase at the workspace's default path, with a current project set.
func newTestUnifiedMemoryWithKB(t *testing.T) (*UnifiedMemory, *knowledge.KnowledgeBase, int64) {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	kb, err := knowledge.Open(knowledge.DefaultPath(ws.Root))
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	t.Cleanup(func() { kb.Close() })
	pid, err := kb.AddProject("test-project", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	cfg := &MemoryConfig{CurrentProjectID: pid}
	um := NewUnifiedMemory(nil, cfg, ws)
	return um, kb, pid
}

func TestRecallKB_ConceptMatch_PrimaryPath(t *testing.T) {
	um, kb, pid := newTestUnifiedMemoryWithKB(t)

	conceptID, err := kb.AddConcept("chunking", "")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	obsID, err := kb.AddObservation(pid, "finding", "map-reduce chunking is sound on CPU-only hardware")
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if err := kb.LinkObservationConcept(obsID, conceptID); err != nil {
		t.Fatalf("LinkObservationConcept: %v", err)
	}

	results, err := um.recallKB("how does chunking behave on a Pi?")
	if err != nil {
		t.Fatalf("recallKB: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("recallKB: got %d results, want 1: %+v", len(results), results)
	}
	if !strings.Contains(results[0].Content, "map-reduce chunking is sound") {
		t.Errorf("Content = %q, want observation body", results[0].Content)
	}
	if results[0].Score != 0.6 {
		t.Errorf("Score = %v, want 0.6 for concept-tag match", results[0].Score)
	}
}

func TestRecallKB_ConceptMatch_NotProjectScoped(t *testing.T) {
	um, kb, _ := newTestUnifiedMemoryWithKB(t)

	otherPID, err := kb.AddProject("other-project", "")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	conceptID, err := kb.AddConcept("RAG", "")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	obsID, err := kb.AddObservation(otherPID, "note", "RAG chunk retrieval note from a different project")
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if err := kb.LinkObservationConcept(obsID, conceptID); err != nil {
		t.Fatalf("LinkObservationConcept: %v", err)
	}

	// The current project (from newTestUnifiedMemoryWithKB) is neither
	// otherPID nor linked to the observation; RecallByConceptNames is
	// deliberately not project-scoped, so the match must still surface.
	results, err := um.recallKB("what do we know about RAG?")
	if err != nil {
		t.Fatalf("recallKB: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("recallKB: got %d results, want 1 (cross-project concept match): %+v", len(results), results)
	}
}

func TestRecallKB_RecordMatch(t *testing.T) {
	um, kb, pid := newTestUnifiedMemoryWithKB(t)

	conceptID, err := kb.AddConcept("routing", "")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	recID, err := kb.AddRecord(knowledge.Record{
		RecordID: "0001", ProjectID: pid, Title: "Adopt multi-model routing",
		Date: "2026-06-01", Status: "accepted", Kind: "decision",
		Body: "Route prompts by declared model capability.",
	})
	if err != nil {
		t.Fatalf("AddRecord: %v", err)
	}
	if err := kb.LinkRecordConcept(recID, conceptID); err != nil {
		t.Fatalf("LinkRecordConcept: %v", err)
	}

	results, err := um.recallKB("why did we choose this routing approach?")
	if err != nil {
		t.Fatalf("recallKB: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("recallKB: got %d results, want 1: %+v", len(results), results)
	}
	if !strings.Contains(results[0].Content, "Adopt multi-model routing") {
		t.Errorf("Content = %q, want record title", results[0].Content)
	}
	if !strings.Contains(results[0].Content, "Route prompts by declared model capability") {
		t.Errorf("Content = %q, want record body", results[0].Content)
	}
}

func TestRecallKB_SkipsUnreviewedDocumentMatches(t *testing.T) {
	um, kb, pid := newTestUnifiedMemoryWithKB(t)

	conceptID, err := kb.AddConcept("harness", "")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	docID, err := kb.AddDocument(knowledge.Document{
		ProjectID: pid, Title: "harness-engineering-notes", Format: "markdown", Path: "notes.md",
	})
	if err != nil {
		t.Fatalf("AddDocument: %v", err)
	}
	sectionID, err := kb.AddDocumentSection(knowledge.DocumentSection{
		DocumentID: docID, Level: "section", Seq: 1, Heading: "Harness gaps",
		Body: "raw ingested text", SummaryStatus: "unsummarized",
	})
	if err != nil {
		t.Fatalf("AddDocumentSection: %v", err)
	}
	if err := kb.LinkDocumentSectionConcept(sectionID, conceptID); err != nil {
		t.Fatalf("LinkDocumentSectionConcept: %v", err)
	}

	results, err := um.recallKB("what's the state of the harness work?")
	if err != nil {
		t.Fatalf("recallKB: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("recallKB: got %d results, want 0 (unreviewed document summary must not surface as content): %+v", len(results), results)
	}
}

func TestRecallKB_ReviewedDocumentMatch_Surfaces(t *testing.T) {
	um, kb, pid := newTestUnifiedMemoryWithKB(t)

	conceptID, err := kb.AddConcept("harness", "")
	if err != nil {
		t.Fatalf("AddConcept: %v", err)
	}
	docID, err := kb.AddDocument(knowledge.Document{
		ProjectID: pid, Title: "harness-engineering-notes", Format: "markdown", Path: "notes.md",
	})
	if err != nil {
		t.Fatalf("AddDocument: %v", err)
	}
	sectionID, err := kb.AddDocumentSection(knowledge.DocumentSection{
		DocumentID: docID, Level: "section", Seq: 1, Heading: "Harness gaps",
		Body: "raw ingested text", SummaryBody: "A reviewed, human-approved summary.",
		SummaryStatus: "reviewed",
	})
	if err != nil {
		t.Fatalf("AddDocumentSection: %v", err)
	}
	if err := kb.LinkDocumentSectionConcept(sectionID, conceptID); err != nil {
		t.Fatalf("LinkDocumentSectionConcept: %v", err)
	}

	results, err := um.recallKB("what's the state of the harness work?")
	if err != nil {
		t.Fatalf("recallKB: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("recallKB: got %d results, want 1: %+v", len(results), results)
	}
	if !strings.Contains(results[0].Content, "A reviewed, human-approved summary.") {
		t.Errorf("Content = %q, want reviewed summary body", results[0].Content)
	}
}

func TestRecallKB_FallsBackToSubstring_WhenNoConceptMatch(t *testing.T) {
	um, kb, pid := newTestUnifiedMemoryWithKB(t)

	if _, err := kb.AddObservation(pid, "note", "the release process needs a version bump first"); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, err := um.recallKB("version bump")
	if err != nil {
		t.Fatalf("recallKB: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("recallKB: got %d results, want 1 (substring fallback): %+v", len(results), results)
	}
	if results[0].Score != 0.5 {
		t.Errorf("Score = %v, want 0.5 for substring fallback", results[0].Score)
	}
}
