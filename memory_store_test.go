package harvey

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// approxEqual returns true when a and b differ by less than 1e-9.
func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// newTestStore creates a MemoryStore backed by a temp directory.
// The caller must defer cleanup().
func newTestStore(t *testing.T) (*MemoryStore, func()) {
	t.Helper()
	dir := t.TempDir()
	ws := &Workspace{Root: dir}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	return store, func() { store.Close() }
}

// saveTestMemory saves a simple memory document into store and returns its ID.
func saveTestMemory(t *testing.T, store *MemoryStore, id string, kind string, conf float64) {
	t.Helper()
	doc := NewMemoryDoc(id, MemoryTypeToolUse, "desc "+id, "summary "+id, []string{"tag"})
	doc.Meta.Kind = kind
	doc.Meta.Action = "Do the thing for " + id
	doc.Meta.Confidence = conf
	doc.FountainBody = BuildFountainBody("2026-06-17 00:00:00", [][2]string{
		{"HARVEY", "Testing."},
	})
	if err := store.Save(doc, nil); err != nil {
		t.Fatalf("Save %s: %v", id, err)
	}
}

// ── SetConfidence ─────────────────────────────────────────────────────────────

func TestSetConfidence_AdjustDown(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	saveTestMemory(t, store, "sc_001", "pitfall", 0.8)

	got, err := store.SetConfidence("sc_001", -0.1)
	if err != nil {
		t.Fatalf("SetConfidence: %v", err)
	}
	if !approxEqual(got, 0.7) {
		t.Errorf("confidence: got %v, want ~0.7", got)
	}
}

func TestSetConfidence_AdjustUp(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	saveTestMemory(t, store, "sc_002", "pitfall", 0.5)

	got, err := store.SetConfidence("sc_002", 0.2)
	if err != nil {
		t.Fatalf("SetConfidence: %v", err)
	}
	if !approxEqual(got, 0.7) {
		t.Errorf("confidence: got %v, want ~0.7", got)
	}
}

func TestSetConfidence_ClampAtOne(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	saveTestMemory(t, store, "sc_003", "", 0.9)

	got, err := store.SetConfidence("sc_003", 0.5)
	if err != nil {
		t.Fatalf("SetConfidence: %v", err)
	}
	if got != 1.0 {
		t.Errorf("confidence: got %.1f, want 1.0", got)
	}
}

func TestSetConfidence_ClampAtZero(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	saveTestMemory(t, store, "sc_004", "", 0.1)

	got, err := store.SetConfidence("sc_004", -0.5)
	if !errors.Is(err, ErrAutoArchived) {
		t.Fatalf("expected ErrAutoArchived, got %v", err)
	}
	if got != 0.0 {
		t.Errorf("confidence: got %.1f, want 0.0", got)
	}
}

func TestSetConfidence_AutoArchiveAtThreshold(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	saveTestMemory(t, store, "sc_005", "workaround", 0.3)

	// One flag brings it to 0.2 — exactly at threshold → archive.
	got, err := store.SetConfidence("sc_005", -0.1)
	if !errors.Is(err, ErrAutoArchived) {
		t.Fatalf("expected ErrAutoArchived at 0.2, got err=%v conf=%.1f", err, got)
	}

	// Memory should no longer be retrievable as active.
	n, _ := store.Count()
	if n != 0 {
		t.Errorf("active count: got %d, want 0 after auto-archive", n)
	}
}

func TestSetConfidence_NotFound(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	_, err := store.SetConfidence("no_such_id", -0.1)
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
}

// ── WriteDigest ───────────────────────────────────────────────────────────────

func TestWriteDigest_EmptyStore(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "DIGEST.md")
	if err := store.WriteDigest(path); err != nil {
		t.Fatalf("WriteDigest: %v", err)
	}
	// No file written for empty store.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("DIGEST.md should not be written for empty store")
	}
}

func TestWriteDigest_ContainsKindSections(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	saveTestMemory(t, store, "d_001", "pitfall", 0.9)
	saveTestMemory(t, store, "d_002", "workaround", 0.7)
	saveTestMemory(t, store, "d_003", "recommendation", 0.6)
	saveTestMemory(t, store, "d_004", "", 0.5)

	path := filepath.Join(t.TempDir(), "DIGEST.md")
	if err := store.WriteDigest(path); err != nil {
		t.Fatalf("WriteDigest: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read digest: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"# Harvey Memory Digest",
		"## Pitfalls",
		"## Workarounds",
		"## Recommendations",
		"## Unclassified",
		"`d_001`",
		"`d_002`",
		"**Action:**",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("DIGEST.md missing %q", want)
		}
	}
}

func TestWriteDigest_HighConfidenceFirst(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	saveTestMemory(t, store, "low_conf", "pitfall", 0.3)
	saveTestMemory(t, store, "high_conf", "pitfall", 0.9)

	path := filepath.Join(t.TempDir(), "DIGEST.md")
	if err := store.WriteDigest(path); err != nil {
		t.Fatalf("WriteDigest: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	lowIdx := strings.Index(content, "low_conf")
	highIdx := strings.Index(content, "high_conf")
	if highIdx < 0 || lowIdx < 0 {
		t.Fatal("DIGEST.md missing expected entries")
	}
	if highIdx > lowIdx {
		t.Error("high-confidence memory should appear before low-confidence memory")
	}
}

func TestWriteDigest_EmptySectionsOmitted(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	// Only pitfalls — other sections should not appear.
	saveTestMemory(t, store, "only_pitfall", "pitfall", 0.8)

	path := filepath.Join(t.TempDir(), "DIGEST.md")
	if err := store.WriteDigest(path); err != nil {
		t.Fatalf("WriteDigest: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "## Workarounds") {
		t.Error("empty Workarounds section should be omitted")
	}
}

// ── Confidence-weighted Query ─────────────────────────────────────────────────

func TestQuery_HigherConfidenceRanksAboveLower(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	// Save two memories with identical embeddings (nil embedder → zero vector).
	// Query also uses nil embedder → zero vector → equal cosine similarity.
	// Confidence should break the tie.
	saveTestMemory(t, store, "low_q", "pitfall", 0.3)
	saveTestMemory(t, store, "high_q", "pitfall", 0.9)

	// Use a mock embedder that always returns the same non-zero vector so
	// cosine similarity is equal across both documents.
	emb := &fixedEmbedder{vec: []float64{1, 0, 0}}

	// Re-save with the real embedder so both have identical non-zero vectors.
	for _, id := range []string{"low_q", "high_q"} {
		conf := 0.3
		if id == "high_q" {
			conf = 0.9
		}
		doc := NewMemoryDoc(id, MemoryTypeToolUse, "desc "+id, "summary "+id, []string{"tag"})
		doc.Meta.Kind = "pitfall"
		doc.Meta.Confidence = conf
		doc.FountainBody = BuildFountainBody("2026-06-17 00:00:00", [][2]string{
			{"HARVEY", "Testing."},
		})
		if err := store.Save(doc, emb); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}

	results, err := store.Query("tag", emb, 2)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Meta.ID != "high_q" {
		t.Errorf("top result: got %q, want %q", results[0].Meta.ID, "high_q")
	}
}

// fixedEmbedder always returns the same vector, satisfying the Embedder
// interface for tests that need deterministic similarity scores.
type fixedEmbedder struct{ vec []float64 }

func (f *fixedEmbedder) Embed(_ string) ([]float64, error) {
	out := make([]float64, len(f.vec))
	copy(out, f.vec)
	return out, nil
}
func (f *fixedEmbedder) Name() string { return "fixed" }
