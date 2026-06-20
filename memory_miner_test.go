package harvey

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// pitfallProposalClient returns one memory with kind "pitfall" and a non-empty action.
type pitfallProposalClient struct{}

func (p *pitfallProposalClient) Name() string                               { return "mock-pitfall" }
func (p *pitfallProposalClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (p *pitfallProposalClient) Close() error                               { return nil }
func (p *pitfallProposalClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	payload := `[{
		"type": "tool_use",
		"kind": "pitfall",
		"description": "Run git init when git reports not a repository",
		"summary": "Harvey encountered fatal: not a git repository. Running git init resolved it.",
		"action": "Run git init in the project root, then retry the original command.",
		"tags": ["git", "init", "error"],
		"fountain_body": "FADE IN:\n\nINT. MEMORY 2026-06-17 00:00:00\n\nHARVEY\nTesting.\n\nTHE END.\n"
	}]`
	_, _ = io.WriteString(out, payload)
	return ChatStats{}, nil
}

func TestMineAuto_KindAndActionSaved(t *testing.T) {
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
	if err := os.WriteFile(sessPath, []byte("session content"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := &Agent{Client: &pitfallProposalClient{}}
	miner := NewMiner(store, manifest, ws)

	if err := miner.MineAuto(context.Background(), sessPath, agent, nil, io.Discard); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}

	docs, err := store.ListDocs("")
	if err != nil {
		t.Fatalf("ListDocs: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(docs))
	}

	doc := docs[0]
	if doc.Meta.Kind != "pitfall" {
		t.Errorf("Kind: got %q, want %q", doc.Meta.Kind, "pitfall")
	}
	if doc.Meta.Action == "" {
		t.Error("Action should be non-empty")
	}
	if doc.Meta.Confidence != 0.5 {
		t.Errorf("Confidence: got %v, want 0.5", doc.Meta.Confidence)
	}
}

// ─── splitAtModelSwitches ────────────────────────────────────────────────────

func TestSplitAtModelSwitches_noSwitches(t *testing.T) {
	text := "HARVEY\nHello!\n\nRSDOIEL\nThanks."
	segs := splitAtModelSwitches(text, "qwen-coding", "llamafile")
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].model != "qwen-coding" {
		t.Errorf("segment model: got %q want %q", segs[0].model, "qwen-coding")
	}
	if strings.TrimRight(segs[0].text, "\n") != strings.TrimRight(text, "\n") {
		t.Errorf("segment text mismatch\ngot:  %q\nwant: %q", segs[0].text, text)
	}
}

func TestSplitAtModelSwitches_oneSwitch(t *testing.T) {
	text := "HARVEY\nFirst reply.\n\n[[model switch: phi-mini (llamafile) at 2026-06-20 14:00:00]]\n\nHARVEY\nSecond reply."
	segs := splitAtModelSwitches(text, "qwen-coding", "llamafile")
	if len(segs) != 2 {
		t.Fatalf("expected 2 segments, got %d: %+v", len(segs), segs)
	}
	if segs[0].model != "qwen-coding" {
		t.Errorf("seg[0] model: got %q want %q", segs[0].model, "qwen-coding")
	}
	if segs[1].model != "phi-mini" {
		t.Errorf("seg[1] model: got %q want %q", segs[1].model, "phi-mini")
	}
	if segs[1].backend != "llamafile" {
		t.Errorf("seg[1] backend: got %q want %q", segs[1].backend, "llamafile")
	}
}

func TestParseModelSwitchNote_valid(t *testing.T) {
	line := "[[model switch: phi-mini (llamafile) at 2026-06-20 14:32:11]]"
	name, backend, ok := parseModelSwitchNote(line)
	if !ok {
		t.Fatal("expected ok=true for valid note")
	}
	if name != "phi-mini" {
		t.Errorf("name: got %q want %q", name, "phi-mini")
	}
	if backend != "llamafile" {
		t.Errorf("backend: got %q want %q", backend, "llamafile")
	}
}

func TestParseModelSwitchNote_invalid(t *testing.T) {
	cases := []string{
		"[[write: foo.go — ok]]",
		"some regular text",
		"[[model switch: missing-parens at 2026-06-20]]",
	}
	for _, c := range cases {
		_, _, ok := parseModelSwitchNote(c)
		if ok {
			t.Errorf("expected ok=false for %q", c)
		}
	}
}
