package harvey

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMostRecentSession_empty(t *testing.T) {
	if p := MostRecentSession(t.TempDir()); p != "" {
		t.Errorf("expected empty string for empty dir, got %q", p)
	}
}

func TestMostRecentSession_missingDir(t *testing.T) {
	if p := MostRecentSession("/nonexistent/path/that/does/not/exist"); p != "" {
		t.Errorf("expected empty string for missing dir, got %q", p)
	}
}

func TestMostRecentSession_returnsNewest(t *testing.T) {
	dir := t.TempDir()

	older := filepath.Join(dir, "session-old.spmd")
	newer := filepath.Join(dir, "session-new.spmd")

	if err := os.WriteFile(older, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	// Sleep briefly so mod times differ, then write the newer file.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(newer, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	got := MostRecentSession(dir)
	if got != newer {
		t.Errorf("MostRecentSession = %q, want %q", got, newer)
	}
}

func TestMostRecentSession_ignoresNonSession(t *testing.T) {
	dir := t.TempDir()

	// A .spmd file and a non-session file.
	spmd := filepath.Join(dir, "session.spmd")
	if err := os.WriteFile(spmd, []byte("session"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes"), 0644); err != nil {
		t.Fatal(err)
	}

	got := MostRecentSession(dir)
	if got != spmd {
		t.Errorf("MostRecentSession = %q, want %q", got, spmd)
	}
}
