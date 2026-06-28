package harvey

import (
	"os"
	"strings"
	"testing"
)

// TestCmdStatus_ProfileShown verifies that /status prints the active workspace
// profile name when one exists in the memory store.
func TestCmdStatus_ProfileShown(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Memory.Enabled = true

	ms, err := OpenMemory(ws, &cfg.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()

	ts := "2026-06-05 11:00:00"
	id := "workspace_profile_status01"
	doc := NewMemoryDoc(id, MemoryTypeWorkspaceProfile, "Data Scientist — testws", "data scientist", []string{"workspace_profile"})
	doc.FountainBody = BuildFountainBody(ts, [][2]string{{"HARVEY", "data scientist profile"}})
	if err := ms.Store.Save(doc, nil); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	a := &Agent{Config: cfg, Workspace: ws, Memory: ms, commands: make(map[string]*Command)}
	a.registerCommands()

	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatalf("cmdStatus: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Data Scientist") {
		t.Errorf("status output should include profile description; got:\n%s", got)
	}
	if !strings.Contains(got, "workspace_profile_status01") {
		t.Errorf("status output should include profile ID; got:\n%s", got)
	}
}

// TestCmdStatus_NoProfile verifies that /status prints a prompt to create a
// profile when none exists.
func TestCmdStatus_NoProfile(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Memory.Enabled = true

	ms, err := OpenMemory(ws, &cfg.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()

	a := &Agent{Config: cfg, Workspace: ws, Memory: ms, commands: make(map[string]*Command)}
	a.registerCommands()

	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatalf("cmdStatus: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "/profile use") {
		t.Errorf("status output should suggest /profile use when no profile; got:\n%s", got)
	}
}

// TestCmdStatus_MemoryDisabled verifies that when memory is disabled the
// Profile line is not printed at all.
func TestCmdStatus_MemoryDisabled(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Memory.Enabled = false
	a := &Agent{Config: cfg, Workspace: ws, commands: make(map[string]*Command)}
	a.registerCommands()

	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatalf("cmdStatus: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Profile:") {
		t.Errorf("Profile line should not appear when memory is disabled; got:\n%s", got)
	}
}

// ─── /status llamafile parity ─────────────────────────────────────────────────

func TestCmdStatus_llamafileShowsTokenEstimate(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{{Name: "qwen-coding", Path: "/tmp/q.llamafile", ContextLength: 32768}}
	cfg.LlamafileActive = "qwen-coding"
	a := &Agent{
		Config:   cfg,
		Workspace: ws,
		commands: make(map[string]*Command),
	}
	a.registerCommands()
	a.Client = newLlamafileLLMClient("http://localhost:8080/v1", "qwen-coding", 0)
	a.AddMessage("user", strings.Repeat("x", 400)) // ~100 tokens

	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatalf("cmdStatus: %v", err)
	}
	got := out.String()
	// Should show a token estimate (with ~ prefix) for llamafile.
	if !strings.Contains(got, "Tokens:") {
		t.Errorf("expected Tokens line for llamafile backend, got:\n%s", got)
	}
	if !strings.Contains(got, "~") {
		t.Errorf("expected ~ estimate qualifier for llamafile, got:\n%s", got)
	}
}

func TestCmdStatus_llamafileShowsHarveyTag(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamafileModels = []LlamafileEntry{{Name: "qwen-coding", Path: "/tmp/q.llamafile"}}
	cfg.LlamafileActive = "qwen-coding"
	a := &Agent{
		Config:   cfg,
		Workspace: ws,
		commands: make(map[string]*Command),
	}
	a.registerCommands()
	a.Client = newLlamafileLLMClient("http://localhost:8080/v1", "qwen-coding", 0)
	// Simulate Harvey having started the llamafile.
	a.llamafileProc = &os.Process{Pid: 99999}

	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatalf("cmdStatus: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[Harvey]") {
		t.Errorf("expected [Harvey] tag when Harvey started the llamafile, got:\n%s", got)
	}
}
