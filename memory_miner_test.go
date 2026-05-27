package harvey

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// emptyProposalClient returns a JSON empty array so extract() produces no docs.
type emptyProposalClient struct{}

func (e *emptyProposalClient) Name() string                               { return "mock" }
func (e *emptyProposalClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (e *emptyProposalClient) Close() error                               { return nil }
func (e *emptyProposalClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	_, _ = io.WriteString(out, "[]")
	return ChatStats{}, nil
}

func TestMineAuto_AlreadyMined(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	manifest, err := LoadManifest(store.Dir())
	if err != nil {
		t.Fatal(err)
	}

	// Create a fake session file and record it as already mined.
	sessPath := filepath.Join(t.TempDir(), "session.spmd")
	if err := os.WriteFile(sessPath, []byte("session content"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest.Record(sessPath, []string{"mem1"}, 0)

	agent := &Agent{Client: &emptyProposalClient{}}
	miner := NewMiner(store, manifest, ws)

	if err := miner.MineAuto(context.Background(), sessPath, agent, nil, io.Discard); err != nil {
		t.Errorf("MineAuto on already-mined session should return nil, got %v", err)
	}
}

func TestMineAuto_NoProposals(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	manifest, err := LoadManifest(store.Dir())
	if err != nil {
		t.Fatal(err)
	}

	sessPath := filepath.Join(t.TempDir(), "session.spmd")
	if err := os.WriteFile(sessPath, []byte("a short session"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := &Agent{Client: &emptyProposalClient{}}
	miner := NewMiner(store, manifest, ws)

	if err := miner.MineAuto(context.Background(), sessPath, agent, nil, io.Discard); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}

	// Session should be in the manifest even when no memories were proposed.
	if !manifest.IsMined(sessPath) {
		t.Error("session should be recorded in manifest after MineAuto")
	}

	// No memories should have been created.
	metas, err := store.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 0 {
		t.Errorf("expected 0 memories, got %d", len(metas))
	}
}

func TestMineAuto_NilClient(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	manifest, err := LoadManifest(store.Dir())
	if err != nil {
		t.Fatal(err)
	}

	sessPath := filepath.Join(t.TempDir(), "session.spmd")
	if err := os.WriteFile(sessPath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := &Agent{Client: nil}
	miner := NewMiner(store, manifest, ws)

	if err := miner.MineAuto(context.Background(), sessPath, agent, nil, io.Discard); err != nil {
		t.Errorf("nil client should return nil, got %v", err)
	}
	// No manifest entry written (nil client exits early).
	if manifest.IsMined(sessPath) {
		t.Error("session should not be recorded when client is nil")
	}
}

func TestMineAuto_MissingSessionFile(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	manifest, err := LoadManifest(store.Dir())
	if err != nil {
		t.Fatal(err)
	}

	agent := &Agent{Client: &emptyProposalClient{}}
	miner := NewMiner(store, manifest, ws)

	err = miner.MineAuto(context.Background(), "/nonexistent/session.spmd", agent, nil, io.Discard)
	if err == nil {
		t.Error("expected error for missing session file")
	}
}
