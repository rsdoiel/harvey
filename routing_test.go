package harvey

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ollamaMockServer starts a test HTTP server that handles /api/show and
// /api/chat. showCtxLen is returned as the context_length in /api/show.
// chatReply is the content the mock streams back from /api/chat.
func ollamaMockServer(t *testing.T, showCtxLen int, chatReply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			resp := map[string]interface{}{
				"details": map[string]string{
					"family":             "llama",
					"parameter_size":     "1.2B",
					"quantization_level": "Q4_K_M",
				},
				"model_info": map[string]interface{}{
					"llama.context_length": float64(showCtxLen),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case "/api/chat":
			w.Header().Set("Content-Type", "application/x-ndjson")
			// Stream the reply as two chunks: content + done packet.
			chunk1 := map[string]interface{}{
				"message": map[string]string{"role": "assistant", "content": chatReply},
				"done":    false,
			}
			chunk2 := map[string]interface{}{
				"message":    map[string]string{"role": "assistant", "content": ""},
				"done":       true,
				"eval_count": 5,
			}
			enc := json.NewEncoder(w)
			enc.Encode(chunk1)
			enc.Encode(chunk2)

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestNewRouter_ContextLengthFromServer(t *testing.T) {
	srv := ollamaMockServer(t, 8192, "")
	defer srv.Close()

	r, err := NewRouter(RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"}, srv.URL)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	if r.smallCtxLen != 8192 {
		t.Errorf("smallCtxLen = %d, want 8192", r.smallCtxLen)
	}
}

func TestNewRouter_FallbackContextLength(t *testing.T) {
	// Server returns 0 for context length — should fall back to 4096.
	srv := ollamaMockServer(t, 0, "")
	defer srv.Close()

	r, err := NewRouter(RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"}, srv.URL)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	if r.smallCtxLen != 4096 {
		t.Errorf("smallCtxLen = %d, want 4096 (fallback)", r.smallCtxLen)
	}
}

func TestClassify_DirectAnswer(t *testing.T) {
	srv := ollamaMockServer(t, 8192, "The capital of France is Paris.")
	defer srv.Close()

	r, _ := NewRouter(RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"}, srv.URL)
	history := []Message{{Role: "user", Content: "What is the capital of France?"}}

	answer, routeTo, err := r.Classify(context.Background(), history)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if routeTo != "" {
		t.Errorf("routeTo = %q, want empty (direct answer)", routeTo)
	}
	if !strings.Contains(answer, "Paris") {
		t.Errorf("answer = %q, want it to contain 'Paris'", answer)
	}
}

func TestClassify_RouteToFull(t *testing.T) {
	srv := ollamaMockServer(t, 8192, "ROUTE:full")
	defer srv.Close()

	r, _ := NewRouter(RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"}, srv.URL)
	history := []Message{{Role: "user", Content: "Implement a B-tree in Go with full test coverage."}}

	answer, routeTo, err := r.Classify(context.Background(), history)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if answer != "" {
		t.Errorf("answer = %q, want empty (routing)", answer)
	}
	if routeTo != "full:latest" {
		t.Errorf("routeTo = %q, want %q", routeTo, "full:latest")
	}
}

func TestClassify_EmptyHistory(t *testing.T) {
	srv := ollamaMockServer(t, 8192, "Hello!")
	defer srv.Close()

	r, _ := NewRouter(RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"}, srv.URL)

	answer, routeTo, err := r.Classify(context.Background(), []Message{})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if routeTo != "" {
		t.Errorf("routeTo = %q, want empty", routeTo)
	}
	_ = answer
}

func TestBuildRoutingMessages_AlwaysIncludesSystemAndCurrent(t *testing.T) {
	r := &Router{
		cfg:         RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"},
		smallCtxLen: 8192,
	}
	history := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "What is 2+2?"},
	}
	msgs := r.buildRoutingMessages(history)

	if msgs[0].Role != "system" {
		t.Errorf("first message role = %q, want 'system'", msgs[0].Role)
	}
	if msgs[0].Content != routingSystemPrompt {
		t.Error("first message content is not the routing system prompt")
	}
	last := msgs[len(msgs)-1]
	if last.Content != "What is 2+2?" {
		t.Errorf("last message = %q, want current user prompt", last.Content)
	}
}

func TestBuildRoutingMessages_SkipsSystemMessages(t *testing.T) {
	r := &Router{
		cfg:         RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"},
		smallCtxLen: 8192,
	}
	history := []Message{
		{Role: "system", Content: "You are Harvey."},
		{Role: "user", Content: "Hello"},
		{Role: "user", Content: "Current question"},
	}
	msgs := r.buildRoutingMessages(history)

	for _, m := range msgs[1:] { // skip the routing system prompt itself
		if m.Role == "system" && m.Content == "You are Harvey." {
			t.Error("harvey system message should be excluded from routing messages")
		}
	}
}

func TestBuildRoutingMessages_RespectsTokenBudget(t *testing.T) {
	r := &Router{
		cfg:         RouterConfig{FastModel: "fast:latest", FullModel: "full:latest"},
		smallCtxLen: 400, // tiny window → budget ≈ 100 tokens
	}

	// Build a history with many large messages that exceed the budget.
	history := []Message{{Role: "user", Content: "final question"}}
	for i := 0; i < 20; i++ {
		history = append([]Message{
			{Role: "user", Content: fmt.Sprintf("user message %d: %s", i, strings.Repeat("word ", 30))},
			{Role: "assistant", Content: fmt.Sprintf("assistant reply %d: %s", i, strings.Repeat("word ", 30))},
		}, history...)
	}

	msgs := r.buildRoutingMessages(history)

	// Count total estimated tokens across all messages.
	total := 0
	for _, m := range msgs {
		total += estimateTokens(m.Content)
	}
	// Should not wildly exceed the context window.
	if total > r.smallCtxLen {
		t.Errorf("total estimated tokens %d exceeds smallCtxLen %d", total, r.smallCtxLen)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		input string
		minN  int
		maxN  int
	}{
		{"", 1, 1},
		{"hello", 1, 2},
		{strings.Repeat("a", 400), 99, 101},
	}
	for _, c := range cases {
		n := estimateTokens(c.input)
		if n < c.minN || n > c.maxN {
			t.Errorf("estimateTokens(%q...) = %d, want %d–%d", c.input[:min(len(c.input), 10)], n, c.minN, c.maxN)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
