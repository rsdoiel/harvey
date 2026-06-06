package harvey

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newProfileTestAgent creates an Agent backed by a temp workspace with a
// pre-existing workspace_profile memory, suitable for /profile use tests.
func newProfileTestAgent(t *testing.T) (*Agent, *MemoryStore) {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Memory.Enabled = true
	a := &Agent{
		Config:    cfg,
		Workspace: ws,
		In:        strings.NewReader(""),
		commands:  make(map[string]*Command),
	}

	// Write a seed workspace_profile so there is something to archive.
	ts := "2026-06-05 10:00:00"
	id := "workspace_profile_seed01"
	doc := NewMemoryDoc(id, MemoryTypeWorkspaceProfile, "Back End Developer — testws", "back end developer", []string{"workspace_profile"})
	doc.FountainBody = BuildFountainBody(ts, [][2]string{{"HARVEY", "seed profile"}, {"USER", "Go developer"}})
	if err := store.Save(doc, nil); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	return a, store
}

// TestResolveHandoffDir checks the directory is created.
func TestResolveHandoffDir(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir, err := ResolveHandoffDir(ws)
	if err != nil {
		t.Fatalf("ResolveHandoffDir: %v", err)
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Errorf("hand-off directory not created: %v", statErr)
	}
	want := filepath.Join(ws.HarveyDir(), "hand-off")
	if dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
}

// TestWriteHandoff_Empty verifies that WriteHandoff succeeds on an agent with
// no history and produces a valid Fountain file.
func TestWriteHandoff_Empty(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handoffDir, err := ResolveHandoffDir(ws)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	a := &Agent{Config: cfg, Workspace: ws, commands: make(map[string]*Command)}

	path, err := a.WriteHandoff(nil, handoffDir)
	if err != nil {
		t.Fatalf("WriteHandoff: %v", err)
	}
	if !strings.HasSuffix(path, ".spmd") {
		t.Errorf("handoff file should have .spmd extension: %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "INT. HAND-OFF") {
		t.Error("handoff file missing scene heading")
	}
	if !strings.Contains(content, "THE END.") {
		t.Error("handoff file missing THE END.")
	}
}

// TestWriteHandoff_WithHistory checks that topics, file paths, and open
// questions are extracted from conversation history.
func TestWriteHandoff_WithHistory(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handoffDir, _ := ResolveHandoffDir(ws)
	cfg := DefaultConfig()
	a := &Agent{Config: cfg, Workspace: ws, commands: make(map[string]*Command)}

	a.History = []Message{
		{Role: "user", Content: "Can you look at harvey/commands.go?"},
		{Role: "assistant", Content: "I reviewed harvey/commands.go and found the issue."},
		{Role: "user", Content: "Why does the test fail?"},
		{Role: "assistant", Content: "The test fails because of a nil pointer in memory_store.go."},
	}

	path, err := a.WriteHandoff(nil, handoffDir)
	if err != nil {
		t.Fatalf("WriteHandoff: %v", err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "harvey/commands.go") {
		t.Error("handoff should mention file path from history")
	}
	if !strings.Contains(content, "Why does the test fail?") {
		t.Error("handoff should include open question from user turn")
	}
}

// TestCmdMemoryProfileUse_ByName verifies that /profile use <name> archives
// the old profile, saves a new one, and resets history.
func TestCmdMemoryProfileUse_ByName(t *testing.T) {
	a, store := newProfileTestAgent(t)
	defer store.Close()

	// Seed some history to verify ClearHistory fires.
	a.AddMessage("user", "hello")
	a.AddMessage("assistant", "world")

	var out bytes.Buffer
	if err := cmdMemoryProfileUse(a, []string{"backend-developer"}, &out, store); err != nil {
		t.Fatalf("cmdMemoryProfileUse: %v", err)
	}

	// Old profile should be archived.
	activeMetas, err := store.List(string(MemoryTypeWorkspaceProfile))
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	for _, m := range activeMetas {
		if m.ID == "workspace_profile_seed01" {
			t.Error("old profile should have been archived, not in active list")
		}
	}

	// A new profile should exist.
	if len(activeMetas) == 0 {
		t.Error("no active workspace_profile after /profile use")
	}
	doc, err := store.ByID(activeMetas[0].ID)
	if err != nil || doc == nil {
		t.Fatalf("could not load new profile: %v", err)
	}
	if !strings.Contains(doc.Meta.Description, "Back End Developer") {
		t.Errorf("new profile description %q should mention template name", doc.Meta.Description)
	}

	// History should be reset.
	userTurns := 0
	for _, m := range a.History {
		if m.Role == "user" && m.Content == "hello" {
			userTurns++
		}
	}
	if userTurns > 0 {
		t.Error("ClearHistory should have removed the seeded conversation")
	}

	// Handoff file should exist in agents/hand-off/.
	handoffDir := filepath.Join(a.Workspace.HarveyDir(), "hand-off")
	files, _ := os.ReadDir(handoffDir)
	if len(files) == 0 {
		t.Error("expected at least one handoff file in agents/hand-off/")
	}
}

// TestCmdMemoryProfileUse_Picker verifies that /profile use with no name
// falls back to the interactive picker (reading from a.In).
func TestCmdMemoryProfileUse_Picker(t *testing.T) {
	a, store := newProfileTestAgent(t)
	defer store.Close()

	a.In = strings.NewReader("2\n") // select second template
	var out bytes.Buffer
	if err := cmdMemoryProfileUse(a, nil, &out, store); err != nil {
		t.Fatalf("cmdMemoryProfileUse picker: %v", err)
	}

	metas, _ := store.List(string(MemoryTypeWorkspaceProfile))
	if len(metas) == 0 {
		t.Error("no active workspace_profile after picker selection")
	}
}

// TestCmdMemoryProfileUse_UnknownNameFallsBackToPicker verifies that an
// unknown template name falls back to the interactive picker.
func TestCmdMemoryProfileUse_UnknownNameFallsBackToPicker(t *testing.T) {
	a, store := newProfileTestAgent(t)
	defer store.Close()

	a.In = strings.NewReader("1\n") // picker will select first template
	var out bytes.Buffer
	if err := cmdMemoryProfileUse(a, []string{"totally-nonexistent-template"}, &out, store); err != nil {
		t.Fatalf("cmdMemoryProfileUse unknown name: %v", err)
	}

	metas, _ := store.List(string(MemoryTypeWorkspaceProfile))
	if len(metas) == 0 {
		t.Error("no active workspace_profile after fallback to picker")
	}
	outStr := out.String()
	if !strings.Contains(outStr, "not found") {
		t.Error("output should mention that template was not found")
	}
}

// TestProfileAlias verifies that /profile delegates to /memory profile
// via the command table.
func TestProfileAlias(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()

	cmd, ok := a.commands["profile"]
	if !ok {
		t.Fatal("'profile' not registered in command table")
	}
	if cmd.Description == "" {
		t.Error("profile command should have a description")
	}
}
