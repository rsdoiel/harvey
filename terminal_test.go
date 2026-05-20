package harvey

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// makeTestTree creates a temporary directory tree for completion tests:
//
//	root/
//	  harvey/
//	    main.go
//	    README.md
//	  docs/
//	    guide.pdf
//	    notes.txt
//	  .hidden/
//	  README.md
//	  report.pdf
func makeTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dirs := []string{"harvey", "docs", ".hidden"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"harvey/main.go":   "package main",
		"harvey/README.md": "# harvey",
		"docs/guide.pdf":   "%PDF",
		"docs/notes.txt":   "notes",
		"README.md":        "# root",
		"report.pdf":       "%PDF",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func sortedMatches(matches []string) []string {
	sort.Strings(matches)
	return matches
}

func TestWorkspacePathCandidates_rootListing(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "", false, nil))
	// Should see dirs and files at root, no hidden entries.
	want := []string{"README.md", "docs/", "harvey/", "report.pdf"}
	if len(got) != len(want) {
		t.Fatalf("root listing: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("root listing[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_prefixFilter(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "h", false, nil))
	want := []string{"harvey/"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("prefix 'h': got %v, want %v", got, want)
	}
}

func TestWorkspacePathCandidates_subdirListing(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "harvey/", false, nil))
	want := []string{"harvey/README.md", "harvey/main.go"}
	if len(got) != len(want) {
		t.Fatalf("harvey/ listing: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("harvey/[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_onlyDirs(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "", true, nil))
	want := []string{"docs/", "harvey/"}
	if len(got) != len(want) {
		t.Fatalf("onlyDirs: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("onlyDirs[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_extFilter(t *testing.T) {
	root := makeTestTree(t)
	pdfOnly := map[string]bool{".pdf": true}
	got := sortedMatches(workspacePathCandidates(root, "", false, pdfOnly))
	// Directories are always included so the user can navigate into them;
	// regular files are filtered to the given extensions.
	want := []string{"docs/", "harvey/", "report.pdf"}
	if len(got) != len(want) {
		t.Fatalf("extFilter .pdf at root: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("extFilter[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_extFilterSubdir(t *testing.T) {
	root := makeTestTree(t)
	pdfOnly := map[string]bool{".pdf": true}
	got := sortedMatches(workspacePathCandidates(root, "docs/", false, pdfOnly))
	want := []string{"docs/guide.pdf"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("extFilter .pdf in docs/: got %v, want %v", got, want)
	}
}

func TestWorkspacePathCandidates_hiddenExcluded(t *testing.T) {
	root := makeTestTree(t)
	got := workspacePathCandidates(root, ".", false, nil)
	for _, m := range got {
		if filepath.Base(m) == ".hidden" || m == ".hidden/" {
			t.Errorf("hidden entry leaked into completions: %q", m)
		}
	}
}

func TestWorkspacePathCandidates_escapeBlocked(t *testing.T) {
	root := makeTestTree(t)
	got := workspacePathCandidates(root, "../../etc/", false, nil)
	if len(got) != 0 {
		t.Errorf("path escape should produce no completions, got %v", got)
	}
}
