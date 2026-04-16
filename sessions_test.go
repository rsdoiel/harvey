package harvey

import (
	"testing"
)

// newTestSessionManager creates a SessionManager backed by a temporary
// workspace for use in a single test. The workspace (and its .harvey/
// directory) is cleaned up automatically when the test ends.
func newTestSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	sm, err := OpenSessionManager(ws)
	if err != nil {
		t.Fatalf("OpenSessionManager: %v", err)
	}
	t.Cleanup(func() { sm.Close() })
	return sm
}

func TestSessionManagerEmpty(t *testing.T) {
	sm := newTestSessionManager(t)

	// LoadLast on an empty database must return nil, nil.
	session, err := sm.LoadLast()
	if err != nil {
		t.Fatalf("LoadLast on empty db: unexpected error: %v", err)
	}
	if session != nil {
		t.Fatalf("LoadLast on empty db: want nil session, got %+v", session)
	}

	// List on an empty database must return an empty (non-nil) slice.
	sessions, err := sm.List()
	if err != nil {
		t.Fatalf("List on empty db: unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("List on empty db: want 0 sessions, got %d", len(sessions))
	}
}

func TestSessionManagerCreateAndLoad(t *testing.T) {
	sm := newTestSessionManager(t)

	history := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	id, err := sm.Create("/workspace/myproject", "llama3", history)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("Create: expected non-zero ID")
	}

	// Load by explicit ID.
	got, err := sm.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load: got nil session, want non-nil")
	}
	if got.ID != id {
		t.Errorf("Load: ID = %d, want %d", got.ID, id)
	}
	if got.Workspace != "/workspace/myproject" {
		t.Errorf("Load: Workspace = %q, want %q", got.Workspace, "/workspace/myproject")
	}
	if got.Model != "llama3" {
		t.Errorf("Load: Model = %q, want %q", got.Model, "llama3")
	}
	if len(got.History) != 2 {
		t.Fatalf("Load: len(History) = %d, want 2", len(got.History))
	}
	if got.History[0].Role != "user" || got.History[0].Content != "Hello" {
		t.Errorf("Load: History[0] = %+v, want {user Hello}", got.History[0])
	}
	if got.History[1].Role != "assistant" || got.History[1].Content != "Hi there!" {
		t.Errorf("Load: History[1] = %+v, want {assistant Hi there!}", got.History[1])
	}
}

func TestSessionManagerLoadNonExistent(t *testing.T) {
	sm := newTestSessionManager(t)

	session, err := sm.Load(9999)
	if err != nil {
		t.Fatalf("Load of missing ID: unexpected error: %v", err)
	}
	if session != nil {
		t.Fatalf("Load of missing ID: want nil, got %+v", session)
	}
}

func TestSessionManagerSaveUpdatesHistory(t *testing.T) {
	sm := newTestSessionManager(t)

	id, err := sm.Create("/workspace/myproject", "llama3", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Save an updated history and a new model.
	updated := []Message{
		{Role: "user", Content: "Rewrite this in Go"},
		{Role: "assistant", Content: "Sure, here it is..."},
		{Role: "user", Content: "Thanks"},
	}
	if err := sm.Save(id, "mistral", updated); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := sm.Load(id)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got.Model != "mistral" {
		t.Errorf("after Save: Model = %q, want %q", got.Model, "mistral")
	}
	if len(got.History) != 3 {
		t.Fatalf("after Save: len(History) = %d, want 3", len(got.History))
	}
}

func TestSessionManagerLoadLast(t *testing.T) {
	sm := newTestSessionManager(t)

	// Create two sessions; the second should be returned by LoadLast.
	_, err := sm.Create("/workspace/a", "llama3", []Message{{Role: "user", Content: "first"}})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	id2, err := sm.Create("/workspace/b", "mistral", []Message{{Role: "user", Content: "second"}})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	got, err := sm.LoadLast()
	if err != nil {
		t.Fatalf("LoadLast: %v", err)
	}
	if got == nil {
		t.Fatal("LoadLast: got nil, want non-nil")
	}
	if got.ID != id2 {
		t.Errorf("LoadLast: ID = %d, want %d (most recent)", got.ID, id2)
	}
	if got.Model != "mistral" {
		t.Errorf("LoadLast: Model = %q, want %q", got.Model, "mistral")
	}
}

func TestSessionManagerList(t *testing.T) {
	sm := newTestSessionManager(t)

	sm.Create("/workspace/a", "llama3", nil)
	sm.Create("/workspace/b", "mistral", nil)
	sm.Create("/workspace/c", "gemma3", nil)

	sessions, err := sm.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("List: got %d sessions, want 3", len(sessions))
	}
	// List returns most-recent first; History should not be populated.
	for _, s := range sessions {
		if s.History != nil {
			t.Errorf("List: session %d has History populated, want nil", s.ID)
		}
	}
}

func TestSessionManagerRename(t *testing.T) {
	sm := newTestSessionManager(t)

	id, err := sm.Create("/workspace/myproject", "llama3", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := sm.Rename(id, "fixing spinner bug"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	got, err := sm.Load(id)
	if err != nil {
		t.Fatalf("Load after Rename: %v", err)
	}
	if got.Name != "fixing spinner bug" {
		t.Errorf("after Rename: Name = %q, want %q", got.Name, "fixing spinner bug")
	}
}

func TestSessionManagerEmptyHistory(t *testing.T) {
	sm := newTestSessionManager(t)

	// nil and empty slice should both round-trip cleanly.
	id, err := sm.Create("/workspace/myproject", "llama3", nil)
	if err != nil {
		t.Fatalf("Create with nil history: %v", err)
	}
	got, err := sm.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// After JSON round-trip, nil becomes an empty slice — both are valid.
	if len(got.History) != 0 {
		t.Errorf("expected empty history, got %d messages", len(got.History))
	}
}
