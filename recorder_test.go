package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if err := r.RecordTurnWithStats("Hi", "Hello!", stats, models, "Routing to llama3.1:8b", nil); err != nil {
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
	if !strings.HasSuffix(path, ".spmd") {
		t.Errorf("expected .spmd suffix in %q", path)
	}
	// Timestamp portion: YYYYMMDD-HHMMSS (15 chars)
	base := filepath.Base(path)
	ts := strings.TrimPrefix(strings.TrimSuffix(base, ".spmd"), "harvey-session-")
	if len(ts) != 15 {
		t.Errorf("timestamp %q: expected 15 chars (YYYYMMDD-HHMMSS)", ts)
	}
}

// ─── RecordModelSwitch ───────────────────────────────────────────────────────

func TestRecordModelSwitch_writesNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.fountain")
	r, err := NewRecorder(path, "llamafile (qwen-coding)", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RecordModelSwitch("phi-mini", "llamafile"); err != nil {
		t.Fatalf("RecordModelSwitch: %v", err)
	}
	r.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[[model switch: phi-mini (llamafile)") {
		t.Errorf("expected model switch note in session file, got:\n%s", content)
	}
}

func TestRecordModelSwitch_timestampFormat(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(filepath.Join(dir, "s.fountain"), "ollama (llama3:8b)", dir)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Format("2006-01-02")
	if err := r.RecordModelSwitch("llama3:70b", "ollama"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	data, _ := os.ReadFile(filepath.Join(dir, "s.fountain"))
	if !strings.Contains(string(data), before) {
		t.Errorf("expected today's date %q in switch note", before)
	}
}

// ─── NewRecorder Backend field ───────────────────────────────────────────────

func TestNewRecorder_backendFieldLlamafile(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(filepath.Join(dir, "s.fountain"), "llamafile (qwen-coding)", dir)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	data, _ := os.ReadFile(filepath.Join(dir, "s.fountain"))
	if !strings.Contains(string(data), "Backend: llamafile") {
		t.Errorf("expected 'Backend: llamafile' in title page, got:\n%s", string(data))
	}
}

func TestNewRecorder_backendFieldOllama(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(filepath.Join(dir, "s.fountain"), "ollama (llama3:8b)", dir)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	data, _ := os.ReadFile(filepath.Join(dir, "s.fountain"))
	if !strings.Contains(string(data), "Backend: ollama") {
		t.Errorf("expected 'Backend: ollama' in title page, got:\n%s", string(data))
	}
}

// ─── Model field includes backend suffix ─────────────────────────────────────

func TestNewRecorder_modelFieldIncludesBackend_llamafile(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(filepath.Join(dir, "s.fountain"), "llamafile (qwen-coding)", dir)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	data := string(mustRead(t, filepath.Join(dir, "s.fountain")))
	// Model field should include the backend in parentheses.
	if !strings.Contains(data, "Model: QWEN-CODING (llamafile)") {
		t.Errorf("expected 'Model: QWEN-CODING (llamafile)' in title page, got:\n%s", data)
	}
}

func TestNewRecorder_modelFieldIncludesBackend_ollama(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(filepath.Join(dir, "s.fountain"), "ollama (gemma4:e2b)", dir)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	data := string(mustRead(t, filepath.Join(dir, "s.fountain")))
	if !strings.Contains(data, "Model: GEMMA4 (ollama)") {
		t.Errorf("expected 'Model: GEMMA4 (ollama)' in title page, got:\n%s", data)
	}
}

// mustRead reads a file and fails the test on error.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return b
}

// ─── parseFountainSession strips backend suffix from Model field ──────────────

func TestParseFountainSession_stripsBackendSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.fountain")
	r, err := NewRecorder(path, "llamafile (qwen-coding)", dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.RecordTurnWithStats("hello", "world", ChatStats{}, nil, "", nil)
	r.Close()

	_, model, _, err := parseFountainSession(path)
	if err != nil {
		t.Fatalf("parseFountainSession: %v", err)
	}
	// Should return just the model name (no backend suffix) so auto-selection works.
	if model != "QWEN-CODING" {
		t.Errorf("expected 'QWEN-CODING', got %q", model)
	}
}
