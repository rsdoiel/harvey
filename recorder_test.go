package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestRecordModelSwitch_updatesModelName verifies that after RecordModelSwitch,
// subsequent RecordTurnWithStats calls use the new model name in the scene
// header, the "Forwarding to" line, and the speaker name — not the stale name
// set at NewRecorder time.
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

// ─── RecordChunkAnalysisStart / RecordChunkResult / RecordChunkSynthesis ────

func TestFormatToolCallNote_NoArgs(t *testing.T) {
	tc := ToolCallRecord{Name: "list_files", Args: "", Result: "ok"}
	got := formatToolCallNote(tc)
	want := "tool: list_files() — ok"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCallNote_WithArgs(t *testing.T) {
	tc := ToolCallRecord{Name: "run_shell", Args: `{"cmd":"go build ./..."}`, Result: "error: exit 1"}
	got := formatToolCallNote(tc)
	want := `tool: run_shell({"cmd":"go build ./..."}) — error: exit 1`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCallNote_WithCharacter(t *testing.T) {
	tc := ToolCallRecord{Name: "read_file", Args: `{"path":"harvey.go"}`, Result: "ok", Character: "MISTRAL"}
	got := formatToolCallNote(tc)
	want := `MISTRAL.tool: read_file({"path":"harvey.go"}) — ok`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCallNote_EmptyResultDefaultsToOk(t *testing.T) {
	tc := ToolCallRecord{Name: "list_files", Args: ""}
	got := formatToolCallNote(tc)
	want := "tool: list_files() — ok"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ─── parseFountainSession strips backend suffix from Model field ──────────────

func TestParseFountainSession_stripsBackendSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.fountain")
	writeLegacyFountain(t, path, "llamafile (qwen-coding)", dir, "hello", "world")

	_, model, _, err := parseFountainSession(path)
	if err != nil {
		t.Fatalf("parseFountainSession: %v", err)
	}
	// Should return just the model name (no backend suffix) so auto-selection works.
	if model != "QWEN-CODING" {
		t.Errorf("expected 'QWEN-CODING', got %q", model)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return b
}
