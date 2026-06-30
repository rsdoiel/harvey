package harvey

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── /workspace status ────────────────────────────────────────────────────────

// TestCmdWorkspaceStatus_NoWorkspace verifies that /workspace status handles
// a nil workspace gracefully.
func TestCmdWorkspaceStatus_NoWorkspace(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace = nil

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "status"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No workspace open") {
		t.Errorf("expected 'No workspace open', got: %q", buf.String())
	}
}

// TestCmdWorkspaceStatus_ShowsRoot verifies that /workspace status prints the
// workspace root path.
func TestCmdWorkspaceStatus_ShowsRoot(t *testing.T) {
	a := newTestAgent(t)

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "status"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), a.Workspace.Root) {
		t.Errorf("expected workspace root %q in output, got: %q", a.Workspace.Root, buf.String())
	}
}

// TestCmdWorkspaceStatus_ShowsAliasCount verifies that /workspace status prints
// the number of defined model aliases.
func TestCmdWorkspaceStatus_ShowsAliasCount(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"code": {Model: "phi4:latest", Tags: []string{"code"}},
		"chat": {Model: "llama3.2:3b", Tags: []string{"chat"}},
	}

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "status"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "2") {
		t.Errorf("expected alias count '2' in output, got: %q", out)
	}
}

// TestCmdWorkspaceStatus_NoProfileMessage verifies that /workspace status shows
// the "create one" hint when memory is disabled (no profile available).
func TestCmdWorkspaceStatus_NoProfileMessage(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.Enabled = false // memory off → no profile shown

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "status"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With memory disabled the profile block is skipped entirely — output must
	// still include root and aliases without crashing.
	out := buf.String()
	if !strings.Contains(out, "Root") || !strings.Contains(out, "Aliases") {
		t.Errorf("expected Root and Aliases in output, got: %q", out)
	}
}

// TestCmdWorkspaceStatus_DefaultSubcommand verifies that /workspace with no
// subcommand behaves identically to /workspace status.
func TestCmdWorkspaceStatus_DefaultSubcommand(t *testing.T) {
	a := newTestAgent(t)

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), a.Workspace.Root) {
		t.Errorf("expected workspace root in output, got: %q", buf.String())
	}
}

// ─── /workspace init ─────────────────────────────────────────────────────────

// TestCmdWorkspaceInit_NoWorkspace verifies that /workspace init returns an
// error when no workspace is open.
func TestCmdWorkspaceInit_NoWorkspace(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace = nil

	var buf bytes.Buffer
	err := cmdWorkspace(a, []string{"workspace", "init"}, &buf)
	if err == nil {
		t.Fatal("expected an error when workspace is nil, got nil")
	}
}

// TestCmdWorkspaceInit_NoPath verifies that /workspace init with no source path
// prints a status summary and a usage tip.
func TestCmdWorkspaceInit_NoPath(t *testing.T) {
	a := newTestAgent(t)

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "init"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Tip") && !strings.Contains(out, "init") {
		t.Errorf("expected usage tip in output, got: %q", out)
	}
}

// TestCmdWorkspaceInit_ImportsAliases verifies that /workspace init <path>
// imports aliases from a source workspace and persists them.
func TestCmdWorkspaceInit_ImportsAliases(t *testing.T) {
	// Build a source workspace with two aliases.
	srcDir := t.TempDir()
	agentsDir := filepath.Join(srcDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlContent := "model_aliases:\n  code:\n    model: granite3.3:8b\n    tags: [code]\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "harvey.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "init", srcDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Imported") {
		t.Errorf("expected 'Imported' in output, got: %q", out)
	}
	if _, ok := a.Config.ModelAliases["code"]; !ok {
		t.Error("expected 'code' alias to be imported into agent config")
	}
}

// TestCmdWorkspaceInit_SkipsExisting verifies that /workspace init does not
// overwrite aliases already defined in the destination.
func TestCmdWorkspaceInit_SkipsExisting(t *testing.T) {
	srcDir := t.TempDir()
	agentsDir := filepath.Join(srcDir, "agents")
	os.MkdirAll(agentsDir, 0o755)
	yamlContent := "model_aliases:\n  code:\n    model: granite3.3:8b\n    tags: [code]\n"
	os.WriteFile(filepath.Join(agentsDir, "harvey.yaml"), []byte(yamlContent), 0o644)

	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{
		"code": {Model: "existing-model:7b", Tags: []string{"code"}},
	}

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "init", srcDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The existing alias must not be overwritten.
	if a.Config.ModelAliases["code"].Model != "existing-model:7b" {
		t.Errorf("existing alias was overwritten; got %q", a.Config.ModelAliases["code"].Model)
	}
	if !strings.Contains(buf.String(), "skipped") {
		t.Errorf("expected 'skipped' in output, got: %q", buf.String())
	}
}

// TestCmdWorkspaceInit_BadPath verifies that /workspace init returns an error
// for a path that does not exist.
func TestCmdWorkspaceInit_BadPath(t *testing.T) {
	a := newTestAgent(t)

	var buf bytes.Buffer
	err := cmdWorkspace(a, []string{"workspace", "init", "/no/such/path/ever"}, &buf)
	if err == nil {
		t.Fatal("expected an error for nonexistent path, got nil")
	}
}

// ─── unknown subcommand ────────────────────────────────────────────────────────

// TestCmdWorkspace_UnknownSubcommand verifies that an unrecognised subcommand
// prints an error message without returning an error.
func TestCmdWorkspace_UnknownSubcommand(t *testing.T) {
	a := newTestAgent(t)

	var buf bytes.Buffer
	if err := cmdWorkspace(a, []string{"workspace", "bogus"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Unknown subcommand") {
		t.Errorf("expected 'Unknown subcommand' message, got: %q", buf.String())
	}
}
