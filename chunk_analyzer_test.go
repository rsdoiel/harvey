package harvey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── sequential mock LLM client ──────────────────────────────────────────────

// seqMockClient is a test double that returns replies and errors in order,
// one per Chat call. Use it instead of mockLLMClient when different calls
// should return different values (e.g., chunk 1 vs chunk 2 vs synthesis).
type seqMockClient struct {
	replies []string
	errors  []error
	calls   int
}

func (m *seqMockClient) Name() string { return "seq-mock" }
func (m *seqMockClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	i := m.calls
	m.calls++
	if i < len(m.errors) && m.errors[i] != nil {
		return ChatStats{}, m.errors[i]
	}
	if i < len(m.replies) {
		fmt.Fprint(out, m.replies[i])
	}
	return ChatStats{}, nil
}
func (m *seqMockClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (m *seqMockClient) Close() error                               { return nil }

// ─── helpers ─────────────────────────────────────────────────────────────────

func defaultParams(chunks []string) ChunkAnalysisParams {
	return ChunkAnalysisParams{
		Filename:    "doc.md",
		Chunks:      chunks,
		Instruction: "Summarize each section.",
		Model:       "llama3.2:1b",
		DocType:     DocTypeProse,
		Config:      DefaultChunkConfig(),
	}
}

// ─── RunChunkedAnalysis tests ─────────────────────────────────────────────────

func TestRunChunkedAnalysis_TwoChunks(t *testing.T) {
	client := &seqMockClient{
		replies: []string{"result1", "result2", "final"},
	}
	params := defaultParams([]string{"chunk one content", "chunk two content"})
	result, err := RunChunkedAnalysis(context.Background(), client, nil, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "final" {
		t.Errorf("expected synthesis result %q, got %q", "final", result)
	}
	if client.calls != 3 {
		t.Errorf("expected 3 LLM calls (2 chunks + 1 synthesis), got %d", client.calls)
	}
}

func TestRunChunkedAnalysis_ChunkError(t *testing.T) {
	errFail := errors.New("context window exceeded")
	client := &seqMockClient{
		replies: []string{"result1", "", "result3", "synthesis"},
		errors:  []error{nil, errFail, nil, nil}, // chunk 2 fails
	}
	params := defaultParams([]string{"chunk one", "chunk two", "chunk three"})
	var progress strings.Builder
	result, err := RunChunkedAnalysis(context.Background(), client, nil, nil, params, &progress)
	if err != nil {
		t.Fatalf("chunk error should not abort RunChunkedAnalysis, got: %v", err)
	}
	if result != "synthesis" {
		t.Errorf("expected synthesis result, got %q", result)
	}
	// Synthesis prompt must include a failure note for chunk 2.
	// We can verify this indirectly: all 4 calls were made (3 chunks + synthesis).
	if client.calls != 4 {
		t.Errorf("expected 4 calls (3 chunks + synthesis), got %d", client.calls)
	}
}

func TestRunChunkedAnalysis_SynthesisError(t *testing.T) {
	errSynth := errors.New("synthesis timeout")
	client := &seqMockClient{
		replies: []string{"r1", "r2"},
		errors:  []error{nil, nil, errSynth}, // synthesis call (index 2) fails
	}
	params := defaultParams([]string{"chunk one", "chunk two"})
	_, err := RunChunkedAnalysis(context.Background(), client, nil, nil, params, io.Discard)
	if err == nil {
		t.Fatal("expected error from synthesis failure, got nil")
	}
	if !strings.Contains(err.Error(), "synthesis") {
		t.Errorf("expected error to mention synthesis, got: %v", err)
	}
}

func TestRunChunkedAnalysis_NilRecorder(t *testing.T) {
	// mockLLMClient always returns the same reply — fine for a single chunk + synthesis.
	client := &mockLLMClient{reply: "ok"}
	params := defaultParams([]string{"only chunk"})
	result, err := RunChunkedAnalysis(context.Background(), client, nil, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error with nil recorder: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestRunChunkedAnalysis_Progress(t *testing.T) {
	client := &seqMockClient{
		replies: []string{"r1", "r2", "final"},
	}
	params := defaultParams([]string{"chunk one", "chunk two"})
	var progress strings.Builder
	_, err := RunChunkedAnalysis(context.Background(), client, nil, nil, params, &progress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := progress.String()
	if !strings.Contains(out, "Processing chunk 1/2") {
		t.Errorf("expected 'Processing chunk 1/2' in progress output, got: %q", out)
	}
	if !strings.Contains(out, "Processing chunk 2/2") {
		t.Errorf("expected 'Processing chunk 2/2' in progress output, got: %q", out)
	}
	if !strings.Contains(out, "Synthesizing") {
		t.Errorf("expected 'Synthesizing' in progress output, got: %q", out)
	}
}

func TestRunChunkedAnalysis_WithRecorder(t *testing.T) {
	dir := t.TempDir()
	sessPath := filepath.Join(dir, "session.fountain")
	rec, err := NewRecorder(sessPath, "Ollama (llama3.2:1b)", dir)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer rec.Close()

	client := &seqMockClient{
		replies: []string{"chunk 1 summary", "chunk 2 summary", "synthesized"},
	}
	params := defaultParams([]string{"first chunk", "second chunk"})
	result, err := RunChunkedAnalysis(context.Background(), client, rec, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "synthesized" {
		t.Errorf("expected %q, got %q", "synthesized", result)
	}
	rec.Close()

	data, _ := os.ReadFile(sessPath)
	content := string(data)

	checks := []string{
		"INT. CHUNK ANALYSIS",
		"[[chunk: file=doc.md",
		"[[chunk-result: 1/2 — ok]]",
		"[[chunk-result: 2/2 — ok]]",
		"[[synthesis: model=llama3.2:1b — ok]]",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in session file\n---\n%s", want, content)
		}
	}
}

func TestRunChunkedAnalysis_PromptIncludesInstruction(t *testing.T) {
	// Verify that the instruction appears in the prompt sent to the LLM.
	var capturedMsg []Message
	client := &capturingMockClient{
		captures: &capturedMsg,
		replies:  []string{"r1", "synthesis"},
	}
	params := defaultParams([]string{"the chunk"})
	params.Instruction = "Extract all headings."
	_, err := RunChunkedAnalysis(context.Background(), client, nil, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedMsg) == 0 {
		t.Fatal("no messages captured")
	}
	if !strings.Contains(capturedMsg[0].Content, "Extract all headings.") {
		t.Errorf("instruction not found in first chunk prompt: %q", capturedMsg[0].Content)
	}
}

// capturingMockClient captures the first message of each Chat call for inspection.
type capturingMockClient struct {
	captures *[]Message
	replies  []string
	calls    int
}

func (m *capturingMockClient) Name() string { return "capturing-mock" }
func (m *capturingMockClient) Chat(_ context.Context, msgs []Message, out io.Writer) (ChatStats, error) {
	if len(msgs) > 0 {
		*m.captures = append(*m.captures, msgs[0])
	}
	i := m.calls
	m.calls++
	if i < len(m.replies) {
		fmt.Fprint(out, m.replies[i])
	}
	return ChatStats{}, nil
}
func (m *capturingMockClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (m *capturingMockClient) Close() error                               { return nil }
