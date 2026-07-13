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

// strChunks builds a []DocumentChunk from plain strings for test convenience.
// StartLine and EndLine are assigned sequentially (10 lines apart) so that
// tests verifying line-number plumbing have non-trivial values to check.
func strChunks(strs ...string) []DocumentChunk {
	chunks := make([]DocumentChunk, len(strs))
	for i, s := range strs {
		start := i*10 + 1
		chunks[i] = DocumentChunk{
			Content:   s,
			StartLine: start,
			EndLine:   start + strings.Count(s, "\n"),
		}
	}
	return chunks
}

func defaultParams(chunks []DocumentChunk) ChunkAnalysisParams {
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
	params := defaultParams(strChunks("chunk one content", "chunk two content"))
	result, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
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
	params := defaultParams(strChunks("chunk one", "chunk two", "chunk three"))
	var progress strings.Builder
	result, err := RunChunkedAnalysis(context.Background(), client, nil, params, &progress)
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
	params := defaultParams(strChunks("chunk one", "chunk two"))
	_, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
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
	params := defaultParams(strChunks("only chunk"))
	result, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
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
	params := defaultParams(strChunks("chunk one", "chunk two"))
	var progress strings.Builder
	_, err := RunChunkedAnalysis(context.Background(), client, nil, params, &progress)
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
	params := defaultParams(strChunks("first chunk", "second chunk"))
	result, err := RunChunkedAnalysis(context.Background(), client, rec, params, io.Discard)
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
	// Verify that the instruction appears in the user message sent for each chunk.
	var allCaptured [][]Message
	client := &allCapturingMockClient{
		captures: &allCaptured,
		replies:  []string{"r1", "synthesis"},
	}
	params := defaultParams(strChunks("the chunk"))
	params.Instruction = "Extract all headings."
	_, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(allCaptured) == 0 || len(allCaptured[0]) < 2 {
		t.Fatal("expected at least 2 messages in first chunk call (system + user)")
	}
	userMsg := allCaptured[0][1] // msgs[1] is the user message; msgs[0] is system
	if !strings.Contains(userMsg.Content, "Extract all headings.") {
		t.Errorf("instruction not found in chunk user message: %q", userMsg.Content)
	}
}

func TestRunChunkedAnalysis_PromptIncludesLineRange(t *testing.T) {
	// Each chunk user message must include the source line range so the model can cite locations.
	var allCaptured [][]Message
	client := &allCapturingMockClient{
		captures: &allCaptured,
		replies:  []string{"r1", "synthesis"},
	}
	chunks := []DocumentChunk{{Content: "some content", StartLine: 42, EndLine: 55}}
	params := defaultParams(chunks)
	_, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(allCaptured) == 0 || len(allCaptured[0]) < 2 {
		t.Fatal("expected at least 2 messages in first chunk call (system + user)")
	}
	userMsg := allCaptured[0][1]
	if !strings.Contains(userMsg.Content, "42") {
		t.Errorf("start line 42 not found in chunk user message: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "55") {
		t.Errorf("end line 55 not found in chunk user message: %q", userMsg.Content)
	}
}

func TestRunChunkedAnalysis_ChunkMessagesAreIsolated(t *testing.T) {
	// Each chunk call must receive exactly 2 messages: one focused system message
	// and one user message containing the instruction and chunk content. No
	// conversation history must appear beyond these two.
	var allCaptured [][]Message
	client := &allCapturingMockClient{
		captures: &allCaptured,
		replies:  []string{"r1", "r2", "synthesis"},
	}
	params := defaultParams(strChunks("first chunk content", "second chunk content"))
	_, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 3 calls: chunk1, chunk2, synthesis.
	if len(allCaptured) != 3 {
		t.Fatalf("expected 3 call captures, got %d", len(allCaptured))
	}
	// Each chunk call: system message first, then exactly 1 user message.
	for i := 0; i < 2; i++ {
		msgs := allCaptured[i]
		if len(msgs) != 2 {
			t.Errorf("chunk call %d: expected exactly 2 messages (system+user, no history), got %d", i+1, len(msgs))
			continue
		}
		if msgs[0].Role != "system" {
			t.Errorf("chunk call %d: expected msgs[0].Role='system', got %q", i+1, msgs[0].Role)
		}
		if msgs[1].Role != "user" {
			t.Errorf("chunk call %d: expected msgs[1].Role='user', got %q", i+1, msgs[1].Role)
		}
	}
}

func TestRunChunkedAnalysis_ChunkSystemMessageConstrainsToChunk(t *testing.T) {
	// The system message injected into each chunk call must instruct the model
	// to analyse only the provided content, preventing cross-chunk hallucination.
	var allCaptured [][]Message
	client := &allCapturingMockClient{
		captures: &allCaptured,
		replies:  []string{"r1", "synthesis"},
	}
	params := defaultParams(strChunks("only this content"))
	_, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(allCaptured) < 1 || len(allCaptured[0]) < 1 {
		t.Fatal("no messages captured for chunk call")
	}
	sys := allCaptured[0][0]
	if sys.Role != "system" {
		t.Fatalf("expected first message to be system, got %q", sys.Role)
	}
	// System message must instruct the model to restrict analysis to the provided text.
	keywords := []string{"only", "provided"}
	for _, kw := range keywords {
		if !strings.Contains(strings.ToLower(sys.Content), kw) {
			t.Errorf("system message missing keyword %q: %q", kw, sys.Content)
		}
	}
}

func TestRunChunkedAnalysis_SynthesisAlsoHasSystemMessage(t *testing.T) {
	// The synthesis call must also include a system message constraining the model.
	var allCaptured [][]Message
	client := &allCapturingMockClient{
		captures: &allCaptured,
		replies:  []string{"r1", "synthesis"},
	}
	params := defaultParams(strChunks("chunk content"))
	_, err := RunChunkedAnalysis(context.Background(), client, nil, params, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// allCaptured[1] is the synthesis call.
	if len(allCaptured) < 2 || len(allCaptured[1]) == 0 {
		t.Fatal("synthesis call not captured")
	}
	if allCaptured[1][0].Role != "system" {
		t.Errorf("synthesis call: expected first message role='system', got %q", allCaptured[1][0].Role)
	}
}

// ─── mock clients ─────────────────────────────────────────────────────────────

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

// allCapturingMockClient captures the full message slice for every Chat call.
type allCapturingMockClient struct {
	captures *[][]Message
	replies  []string
	calls    int
}

func (m *allCapturingMockClient) Name() string { return "all-capturing-mock" }
func (m *allCapturingMockClient) Chat(_ context.Context, msgs []Message, out io.Writer) (ChatStats, error) {
	cp := make([]Message, len(msgs))
	copy(cp, msgs)
	*m.captures = append(*m.captures, cp)
	i := m.calls
	m.calls++
	if i < len(m.replies) {
		fmt.Fprint(out, m.replies[i])
	}
	return ChatStats{}, nil
}
func (m *allCapturingMockClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (m *allCapturingMockClient) Close() error                               { return nil }
