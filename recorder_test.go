package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecorder_roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.md")

	r, err := NewRecorder(path, "Ollama (llama3)", dir)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	if r.Path() != path {
		t.Errorf("Path() = %q, want %q", r.Path(), path)
	}

	if err := r.RecordTurn("What is 2+2?", "2 + 2 = 4."); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}
	if err := r.RecordTurn("And 3+3?", "3 + 3 = 6."); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	checks := []string{
		"# Harvey Session",
		"Ollama (llama3)",
		"Turn 1",
		"What is 2+2?",
		"2 + 2 = 4.",
		"Turn 2",
		"And 3+3?",
		"3 + 3 = 6.",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("session file missing %q", want)
		}
	}
}

func TestDefaultSessionPath(t *testing.T) {
	dir := "/tmp/myproject"
	path := DefaultSessionPath(dir)

	if !strings.HasPrefix(path, dir+"/harvey-session-") {
		t.Errorf("unexpected prefix in %q", path)
	}
	if !strings.HasSuffix(path, ".md") {
		t.Errorf("expected .md suffix in %q", path)
	}
	// Timestamp portion: YYYYMMDD-HHMMSS (15 chars)
	base := filepath.Base(path)
	// "harvey-session-YYYYMMDD-HHMMSS.md" → prefix len = len("harvey-session-") = 15
	ts := strings.TrimPrefix(strings.TrimSuffix(base, ".md"), "harvey-session-")
	if len(ts) != 15 {
		t.Errorf("timestamp %q: expected 15 chars (YYYYMMDD-HHMMSS)", ts)
	}
}
