package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecorder_roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.fountain")

	r, err := NewRecorder(path, "Ollama (llama3:latest)", dir)
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
		"Title: Harvey Session",
		"FADE IN:",
		"INT. HARVEY AND",
		"TALKING",
		// model name extracted and uppercased
		"LLAMA3",
		// LLM relay dialogue — HARVEY speaks the forwarding line
		"HARVEY",
		"Forwarding to LLAMA3.",
		// user input and reply appear as dialogue
		"What is 2+2?",
		"2 + 2 = 4.",
		"And 3+3?",
		"3 + 3 = 6.",
		"THE END.",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("session file missing %q\n---\n%s", want, content)
		}
	}
}

func TestRecorder_withStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.fountain")
	r, _ := NewRecorder(path, "test", dir)

	stats := ChatStats{PromptTokens: 10, ReplyTokens: 20, Elapsed: 1000000000, TokensPerSec: 20}
	models := []string{"llama3.2:1b", "Ollama (llama3.1:8b)"}
	if err := r.RecordTurnWithStats("Hi", "Hello!", stats, models, "Routing to llama3.1:8b"); err != nil {
		t.Fatalf("RecordTurnWithStats: %v", err)
	}
	r.Close()

	data, _ := os.ReadFile(path)
	content := string(data)
	checks := []string{
		"llama3.2:1b → Ollama (llama3.1:8b)",
		"20 reply + 10 ctx",
		"20.0 tok/s",
		"Routing to llama3.1:8b",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in fountain output\n---\n%s", want, content)
		}
	}
	if strings.Contains(content, "[[stats:") {
		t.Error("stat line must be an action block, not a Fountain note")
	}
}

func TestRecorder_agentScene(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.fountain")
	r, _ := NewRecorder(path, "Ollama (gemma4:latest)", dir)

	if err := r.StartAgentScene("Harvey proposes to write 1 file."); err != nil {
		t.Fatalf("StartAgentScene: %v", err)
	}
	if err := r.RecordAgentAction("write", "testout/hello.bash", "yes", "ok"); err != nil {
		t.Fatalf("RecordAgentAction: %v", err)
	}
	r.Close()

	data, _ := os.ReadFile(path)
	content := string(data)

	checks := []string{
		"INT. AGENT MODE",
		"Harvey proposes to write 1 file.",
		"HARVEY",
		"testout/hello.bash",
		"yes",
		"[[write:",
		"ok",
		"THE END.",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("agent scene missing %q\n---\n%s", want, content)
		}
	}
}

func TestRecorder_skillLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.fountain")
	r, _ := NewRecorder(path, "Ollama (llama3:latest)", dir)

	body := "# Go Review\n\nCheck for correctness and style."
	if err := r.RecordSkillLoad("go-review", "Review Go source code for quality issues.", body); err != nil {
		t.Fatalf("RecordSkillLoad: %v", err)
	}
	r.Close()

	data, _ := os.ReadFile(path)
	content := string(data)

	checks := []string{
		"INT. SKILL GO-REVIEW",
		"Harvey executes the go-review skill.",
		"Review Go source code for quality issues.",
		"GO-REVIEW",
		"# Go Review",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("skill scene missing %q\n---\n%s", want, content)
		}
	}
}

func TestDefaultSessionPath(t *testing.T) {
	dir := "/tmp/myproject"
	path := DefaultSessionPath(dir)

	if !strings.HasPrefix(path, dir+"/harvey-session-") {
		t.Errorf("unexpected prefix in %q", path)
	}
	if !strings.HasSuffix(path, ".fountain") {
		t.Errorf("expected .fountain suffix in %q", path)
	}
	// Timestamp portion: YYYYMMDD-HHMMSS (15 chars)
	base := filepath.Base(path)
	ts := strings.TrimPrefix(strings.TrimSuffix(base, ".fountain"), "harvey-session-")
	if len(ts) != 15 {
		t.Errorf("timestamp %q: expected 15 chars (YYYYMMDD-HHMMSS)", ts)
	}
}
