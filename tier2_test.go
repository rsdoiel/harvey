package harvey

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func TestIsBinary_text(t *testing.T) {
	if isBinary([]byte("hello world\n")) {
		t.Error("plain text should not be binary")
	}
}

func TestIsBinary_binary(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 'h', 'e', 'l', 'l', 'o'}
	if !isBinary(data) {
		t.Error("data with null byte should be binary")
	}
}

func TestIsBinary_emptyFile(t *testing.T) {
	if isBinary([]byte{}) {
		t.Error("empty file should not be binary")
	}
}

func TestLooksLikePath_withSlash(t *testing.T) {
	cases := []string{"harvey/spinner.go", "cmd/harvey/main.go", "a/b"}
	for _, c := range cases {
		if !looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = false, want true", c)
		}
	}
}

func TestLooksLikePath_knownExtensions(t *testing.T) {
	cases := []string{"main.go", "index.ts", "README.md", "config.yaml", "query.sql"}
	for _, c := range cases {
		if !looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = false, want true", c)
		}
	}
}

func TestLooksLikePath_languageIdentifiers(t *testing.T) {
	cases := []string{"go", "python", "bash", "typescript", "rust"}
	for _, c := range cases {
		if looksLikePath(c) {
			t.Errorf("looksLikePath(%q) = true, want false (language name)", c)
		}
	}
}

// ─── findTaggedBlocks ─────────────────────────────────────────────────────────

func TestFindTaggedBlocks_none(t *testing.T) {
	text := "No code blocks here."
	if got := findTaggedBlocks(text); len(got) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(got))
	}
}

func TestFindTaggedBlocks_untagged(t *testing.T) {
	text := "```go\nfmt.Println()\n```"
	if got := findTaggedBlocks(text); len(got) != 0 {
		t.Errorf("expected 0 tagged blocks for language-only fence, got %d", len(got))
	}
}

func TestFindTaggedBlocks_withSlashPath(t *testing.T) {
	text := "```go harvey/spinner.go\nfunc foo() {}\n```"
	blocks := findTaggedBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].path != "harvey/spinner.go" {
		t.Errorf("path = %q, want %q", blocks[0].path, "harvey/spinner.go")
	}
	if blocks[0].content != "func foo() {}\n" {
		t.Errorf("content = %q, want %q", blocks[0].content, "func foo() {}\n")
	}
}

func TestFindTaggedBlocks_pathOnlyFence(t *testing.T) {
	text := "```README.md\n# Title\n```"
	blocks := findTaggedBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].path != "README.md" {
		t.Errorf("path = %q, want %q", blocks[0].path, "README.md")
	}
}

func TestFindTaggedBlocks_multiple(t *testing.T) {
	text := "```go a/foo.go\nfunc A() {}\n```\n\n```go b/bar.go\nfunc B() {}\n```"
	blocks := findTaggedBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].path != "a/foo.go" || blocks[1].path != "b/bar.go" {
		t.Errorf("unexpected paths: %v", []string{blocks[0].path, blocks[1].path})
	}
}

func TestFindTaggedBlocks_unterminated(t *testing.T) {
	text := "```go harvey/x.go\nfunc X() {}"
	// No closing fence — block should not appear.
	if got := findTaggedBlocks(text); len(got) != 0 {
		t.Errorf("expected 0 blocks for unterminated fence, got %d", len(got))
	}
}

// ─── /search ─────────────────────────────────────────────────────────────────

func TestCmdSearch_noArgs(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSearch(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.History) != 0 {
		t.Error("expected no history for missing args")
	}
}

func TestCmdSearch_invalidPattern(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSearch(a, []string{"[invalid"}, &out); err == nil {
		t.Error("expected error for invalid regexp")
	}
}

func TestCmdSearch_noMatches(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace.WriteFile("hello.go", []byte("package main\n"), 0o644)

	var out strings.Builder
	if err := cmdSearch(a, []string{"zzznomatch"}, &out); err != nil {
		t.Fatalf("cmdSearch: %v", err)
	}
	if len(a.History) != 0 {
		t.Error("expected no history for zero matches")
	}
	if !strings.Contains(out.String(), "No matches") {
		t.Error("expected 'No matches' message")
	}
}

func TestCmdSearch_withMatches(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace.WriteFile("main.go", []byte("package main\n\nfunc main() {}\n"), 0o644)
	a.Workspace.WriteFile("util.go", []byte("package main\n\nfunc helper() {}\n"), 0o644)

	var out strings.Builder
	if err := cmdSearch(a, []string{"func"}, &out); err != nil {
		t.Fatalf("cmdSearch: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	content := a.History[0].Content
	if !strings.Contains(content, "func main") {
		t.Error("expected match content in history")
	}
	if !strings.Contains(content, "main.go") {
		t.Error("expected filename in history")
	}
}

func TestCmdSearch_skipHiddenDirs(t *testing.T) {
	a := newTestAgent(t)
	// agents/ already exists; write a file with our pattern inside a hidden dir.
	a.Workspace.WriteFile(".hidden/secret.go", []byte("func secret() {}\n"), 0o644)
	a.Workspace.WriteFile("visible.go", []byte("func visible() {}\n"), 0o644)

	var out strings.Builder
	cmdSearch(a, []string{"func"}, &out)

	if len(a.History) == 0 {
		t.Fatal("expected a match in visible.go")
	}
	content := a.History[0].Content
	if strings.Contains(content, ".hidden") {
		t.Error("hidden directory should be skipped")
	}
}

func TestCmdSearch_scopedPath(t *testing.T) {
	a := newTestAgent(t)
	a.Workspace.WriteFile("sub/a.go", []byte("func inSub() {}\n"), 0o644)
	a.Workspace.WriteFile("b.go", []byte("func notInSub() {}\n"), 0o644)

	var out strings.Builder
	cmdSearch(a, []string{"func", "sub"}, &out)

	if len(a.History) == 0 {
		t.Fatal("expected a match")
	}
	content := a.History[0].Content
	if strings.Contains(content, "notInSub") {
		t.Error("search should be scoped to sub/ only")
	}
	if !strings.Contains(content, "inSub") {
		t.Error("expected inSub match in scoped search")
	}
}

// ─── /git ────────────────────────────────────────────────────────────────────

// initGitRepo runs `git init` and a minimal config in dir so git commands work.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestCmdGit_noArgs(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdGit(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCmdGit_disallowedSubcommand(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	cmdGit(a, []string{"push"}, &out)
	if !strings.Contains(out.String(), "read-only") {
		t.Error("expected rejection message for non-read-only subcommand")
	}
	if len(a.History) != 0 {
		t.Error("disallowed subcommand should not add to history")
	}
}

func TestCmdGit_status(t *testing.T) {
	a := newTestAgent(t)
	initGitRepo(t, a.Workspace.Root)

	var out strings.Builder
	if err := cmdGit(a, []string{"status"}, &out); err != nil {
		t.Fatalf("cmdGit: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	if !strings.Contains(a.History[0].Content, "/git status") {
		t.Error("expected command label in history message")
	}
}

// ─── /apply ──────────────────────────────────────────────────────────────────

func TestCmdApply_noHistory(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdApply(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No assistant reply") {
		t.Error("expected 'No assistant reply' message")
	}
}

func TestCmdApply_noTaggedBlocks(t *testing.T) {
	a := newTestAgent(t)
	a.AddMessage("assistant", "Here is some plain text with no tagged blocks.")
	var out strings.Builder
	cmdApply(a, nil, &out)
	if !strings.Contains(out.String(), "No tagged code blocks") {
		t.Error("expected 'No tagged code blocks' message")
	}
}

func TestCmdApply_success(t *testing.T) {
	a := newTestAgent(t)
	a.In = strings.NewReader("y\n")
	a.AddMessage("assistant",
		"Here is the updated file:\n\n```go sub/hello.go\nfunc Hello() {}\n```\n")

	var out strings.Builder
	if err := cmdApply(a, nil, &out); err != nil {
		t.Fatalf("cmdApply: %v", err)
	}

	data, err := a.Workspace.ReadFile("sub/hello.go")
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != "func Hello() {}\n" {
		t.Errorf("content = %q, want %q", data, "func Hello() {}\n")
	}
	if !strings.Contains(out.String(), "✓") {
		t.Error("expected success marker in output")
	}
}

func TestCmdApply_decline(t *testing.T) {
	a := newTestAgent(t)
	a.In = strings.NewReader("n\n")
	a.AddMessage("assistant", "```go sub/hello.go\nfunc Hello() {}\n```")

	var out strings.Builder
	cmdApply(a, nil, &out)

	if _, err := a.Workspace.ReadFile("sub/hello.go"); err == nil {
		t.Error("file should not have been written when user declined")
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Error("expected 'Aborted' message")
	}
}

func TestCmdApply_multipleBlocks(t *testing.T) {
	a := newTestAgent(t)
	a.In = strings.NewReader("y\n")
	a.AddMessage("assistant",
		"```go a/foo.go\nfunc Foo() {}\n```\n\n```go b/bar.go\nfunc Bar() {}\n```")

	var out strings.Builder
	if err := cmdApply(a, nil, &out); err != nil {
		t.Fatalf("cmdApply: %v", err)
	}

	for _, path := range []string{"a/foo.go", "b/bar.go"} {
		if _, err := a.Workspace.ReadFile(path); err != nil {
			t.Errorf("expected %s to be written: %v", path, err)
		}
	}
}

// ─── /git additional subcommands ─────────────────────────────────────────────

// commitFile creates a file, stages it, and commits it so git log / diff have
// something to show.
func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := dir + "/" + name
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{
		{"add", name},
		{"commit", "-m", "add " + name},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestCmdGit_log(t *testing.T) {
	a := newTestAgent(t)
	initGitRepo(t, a.Workspace.Root)
	commitFile(t, a.Workspace.Root, "hello.go", "package main\n")

	var out strings.Builder
	if err := cmdGit(a, []string{"log", "--oneline"}, &out); err != nil {
		t.Fatalf("cmdGit log: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	if !strings.Contains(a.History[0].Content, "add hello.go") {
		t.Error("expected commit message in history")
	}
}

func TestCmdGit_diff(t *testing.T) {
	a := newTestAgent(t)
	initGitRepo(t, a.Workspace.Root)
	commitFile(t, a.Workspace.Root, "hello.go", "package main\n")

	// Modify the file so there is a diff.
	a.Workspace.WriteFile("hello.go", []byte("package main\n\nfunc main() {}\n"), 0o644)

	var out strings.Builder
	if err := cmdGit(a, []string{"diff"}, &out); err != nil {
		t.Fatalf("cmdGit diff: %v", err)
	}
	if len(a.History) != 1 {
		t.Fatalf("expected 1 history message, got %d", len(a.History))
	}
	if !strings.Contains(a.History[0].Content, "func main") {
		t.Error("expected diff content in history")
	}
}

// ─── /search additional coverage ─────────────────────────────────────────────

func TestCmdSearch_skipsBinaryFiles(t *testing.T) {
	a := newTestAgent(t)
	// Write a file with a null byte — should be treated as binary and skipped.
	binary := []byte{'h', 'e', 'l', 'l', 'o', 0x00, 'w', 'o', 'r', 'l', 'd'}
	a.Workspace.WriteFile("image.bin", binary, 0o644)
	a.Workspace.WriteFile("text.go", []byte("func hello() {}\n"), 0o644)

	var out strings.Builder
	cmdSearch(a, []string{"hello"}, &out)

	content := ""
	if len(a.History) > 0 {
		content = a.History[0].Content
	}
	if strings.Contains(content, "image.bin") {
		t.Error("binary file should be skipped by /search")
	}
	if !strings.Contains(content, "text.go") {
		t.Error("expected text.go match in results")
	}
}
