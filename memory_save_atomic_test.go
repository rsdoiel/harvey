package harvey

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// A failed `/memory mine` accept used to leave an orphan: Save wrote the memory's
// file first and then asked Ollama for an embedding, so when the embedding model was
// not installed (HTTP 404 "try pulling it first") the file stayed on disk with no
// database row, and the error never said what to do. Save now embeds first, writes
// nothing when that fails, names the embedding model and says how to pull it, and
// removes what it wrote if the index write fails after it.

type failingEmbedder struct {
	name string
	err  error
}

func (f *failingEmbedder) Embed(string) ([]float64, error) { return nil, f.err }
func (f *failingEmbedder) Name() string                    { return f.name }

var errNotInstalled = errors.New(`ollama embed: HTTP 404: {"error":"model \"nomic-embed-text:latest\" not found, try pulling it first"}`)

func testDoc(id, description string) *MemoryDoc {
	doc := NewMemoryDoc(id, MemoryTypeToolUse, description, "summary "+id, []string{"tag"})
	doc.Meta.Kind = "pitfall"
	doc.Meta.Action = "Do the thing for " + id
	doc.FountainBody = BuildFountainBody("2026-09-25 00:00:00", [][2]string{{"HARVEY", "Testing."}})
	return doc
}

func memoryRows(t *testing.T, s *MemoryStore, id string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSave_EmbedFailureWritesNothing(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	doc := testDoc("atomic_001", "never written")
	err := store.Save(doc, &failingEmbedder{name: "nomic-embed-text:latest", err: errNotInstalled})
	if err == nil {
		t.Fatal("Save succeeded with a failing embedder")
	}
	if _, statErr := os.Stat(doc.FilePath(store.dir)); !os.IsNotExist(statErr) {
		t.Errorf("a failed save left the memory file behind (stat: %v)", statErr)
	}
	if n := memoryRows(t, store, "atomic_001"); n != 0 {
		t.Errorf("a failed save left %d database row(s)", n)
	}
	for _, want := range []string{"nothing was written", "nomic-embed-text:latest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if !errors.Is(err, errNotInstalled) {
		t.Error("the embedder's own error must stay reachable with errors.Is")
	}
}

func TestSave_MissingModelErrorSaysHowToPullIt(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	err := store.Save(testDoc("atomic_002", "x"), &failingEmbedder{name: "nomic-embed-text:latest", err: errNotInstalled})
	if err == nil || !strings.Contains(err.Error(), "ollama pull nomic-embed-text:latest") {
		t.Errorf("error %v should say how to pull the model", err)
	}
	// Any other embedding failure gets no pull hint: it would be wrong advice.
	err = store.Save(testDoc("atomic_003", "x"), &failingEmbedder{name: "m", err: errors.New("connection refused")})
	if err == nil || strings.Contains(err.Error(), "ollama pull") {
		t.Errorf("error %v must not suggest a pull for a connection failure", err)
	}
}

// Re-saving an existing memory while the embedder is down must leave the existing
// file and row exactly as they were: before, the file was overwritten and the row
// was not, so the two disagreed.
func TestSave_EmbedFailureLeavesAnExistingMemoryUntouched(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	first := testDoc("atomic_004", "the original description")
	if err := store.Save(first, &fixedEmbedder{vec: []float64{1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(first.FilePath(store.dir))

	changed := testDoc("atomic_004", "a different description")
	if err := store.Save(changed, &failingEmbedder{name: "m", err: errors.New("down")}); err == nil {
		t.Fatal("want an error")
	}
	after, _ := os.ReadFile(first.FilePath(store.dir))
	if string(before) != string(after) {
		t.Error("the existing memory file was rewritten by a failed save")
	}
	var desc string
	store.db.QueryRow(`SELECT description FROM memories WHERE id = ?`, "atomic_004").Scan(&desc)
	if desc != "the original description" {
		t.Errorf("row description = %q, want it unchanged", desc)
	}
}

func TestSave_IndexFailureRemovesTheNewFile(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	store.db.Close() // the embedding succeeds; the index write cannot
	doc := testDoc("atomic_005", "x")
	if err := store.Save(doc, &fixedEmbedder{vec: []float64{1, 0, 0}}); err == nil {
		t.Fatal("want an error from the closed database")
	}
	if _, statErr := os.Stat(doc.FilePath(store.dir)); !os.IsNotExist(statErr) {
		t.Errorf("a save whose index write failed left an orphan file (stat: %v)", statErr)
	}
}

// A nil embedder still stores a zero vector, as it always has.
func TestSave_NilEmbedderStillSaves(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	if err := store.Save(testDoc("atomic_006", "x"), nil); err != nil {
		t.Fatalf("Save with a nil embedder: %v", err)
	}
	if memoryRows(t, store, "atomic_006") != 1 {
		t.Error("the memory was not indexed")
	}
}
