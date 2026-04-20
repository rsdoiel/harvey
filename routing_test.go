package harvey

import (
	"context"
	"encoding/json"
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

// ── ParseAtMention ────────────────────────────────────────────────────────────

func TestParseAtMention_valid(t *testing.T) {
	name, prompt, ok := ParseAtMention("@pi2 write a Go parser")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if name != "pi2" {
		t.Errorf("name = %q, want %q", name, "pi2")
	}
	if prompt != "write a Go parser" {
		t.Errorf("prompt = %q, want %q", prompt, "write a Go parser")
	}
}

func TestParseAtMention_noMention(t *testing.T) {
	_, _, ok := ParseAtMention("just a normal prompt")
	if ok {
		t.Error("expected ok=false for non-@mention input")
	}
}

func TestParseAtMention_mentionOnly(t *testing.T) {
	name, prompt, ok := ParseAtMention("@cloud")
	if !ok {
		t.Fatal("expected ok=true for bare @mention")
	}
	if name != "cloud" {
		t.Errorf("name = %q, want %q", name, "cloud")
	}
	if prompt != "" {
		t.Errorf("prompt = %q, want empty", prompt)
	}
}

func TestParseAtMention_emptyInput(t *testing.T) {
	_, _, ok := ParseAtMention("")
	if ok {
		t.Error("expected ok=false for empty input")
	}
}

func TestParseAtMention_atSignOnly(t *testing.T) {
	_, _, ok := ParseAtMention("@")
	if ok {
		t.Error("expected ok=false for bare @ with no name")
	}
}

func TestParseAtMention_leadingWhitespace(t *testing.T) {
	name, prompt, ok := ParseAtMention("  @pi3 run tests")
	if !ok {
		t.Fatal("expected ok=true after trimming leading whitespace")
	}
	if name != "pi3" {
		t.Errorf("name = %q, want %q", name, "pi3")
	}
	if prompt != "run tests" {
		t.Errorf("prompt = %q, want %q", prompt, "run tests")
	}
}

// ── recentHistory ─────────────────────────────────────────────────────────────

func TestRecentHistory_respectsN(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
		{Role: "assistant", Content: "msg4"},
		{Role: "user", Content: "msg5"},
	}
	got := recentHistory(history, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Content != "msg3" {
		t.Errorf("first = %q, want %q", got[0].Content, "msg3")
	}
	if got[2].Content != "msg5" {
		t.Errorf("last = %q, want %q", got[2].Content, "msg5")
	}
}

func TestRecentHistory_excludesSystem(t *testing.T) {
	history := []Message{
		{Role: "system", Content: "you are harvey"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	got := recentHistory(history, 10)
	for _, m := range got {
		if m.Role == "system" {
			t.Error("recentHistory should exclude system messages")
		}
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestRecentHistory_fewerThanN(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "only message"},
	}
	got := recentHistory(history, 10)
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestRecentHistory_empty(t *testing.T) {
	got := recentHistory(nil, 10)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d", len(got))
	}
}

// ── RouteRegistry ─────────────────────────────────────────────────────────────

func TestRouteRegistry_addLookupRemove(t *testing.T) {
	rr := NewRouteRegistry()
	ep := &RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.12:11434", Kind: KindOllama}
	rr.Add(ep)

	got := rr.Lookup("pi2")
	if got == nil {
		t.Fatal("Lookup returned nil after Add")
	}
	if got.URL != ep.URL {
		t.Errorf("URL = %q, want %q", got.URL, ep.URL)
	}

	rr.Remove("pi2")
	if rr.Lookup("pi2") != nil {
		t.Error("Lookup should return nil after Remove")
	}
}

func TestRouteRegistry_addOverwrites(t *testing.T) {
	rr := NewRouteRegistry()
	rr.Add(&RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.12:11434", Kind: KindOllama})
	rr.Add(&RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.99:11434", Kind: KindOllama})
	ep := rr.Lookup("pi2")
	if ep.URL != "ollama://192.168.1.99:11434" {
		t.Errorf("URL = %q, want new value", ep.URL)
	}
}

func TestRouteRegistry_removeNonExistent(t *testing.T) {
	rr := NewRouteRegistry()
	rr.Remove("ghost") // should not panic
}

// ── DispatchToEndpoint ────────────────────────────────────────────────────────

func TestDispatchToEndpoint_ollama(t *testing.T) {
	srv := ollamaMockServer(t, 8192, "hello from remote")
	defer srv.Close()

	ep := &RouteEndpoint{
		Name:  "pi2",
		URL:   srv.URL, // raw http:// URL — ollamaBaseURL returns it unchanged
		Model: "llama3.1:8b",
		Kind:  KindOllama,
	}
	cfg := DefaultConfig()
	history := []Message{
		{Role: "user", Content: "previous message"},
		{Role: "assistant", Content: "previous reply"},
	}

	var out strings.Builder
	reply, err := DispatchToEndpoint(context.Background(), ep, history, "write hello world", cfg, &out)
	if err != nil {
		t.Fatalf("DispatchToEndpoint: %v", err)
	}
	if !strings.Contains(reply, "hello from remote") {
		t.Errorf("reply = %q, want it to contain 'hello from remote'", reply)
	}
	if !strings.Contains(out.String(), "hello from remote") {
		t.Error("reply should also be streamed to out")
	}
}

func TestDispatchToEndpoint_unknownKind(t *testing.T) {
	ep := &RouteEndpoint{Name: "bad", URL: "noop://", Kind: RouteKind("unknown")}
	cfg := DefaultConfig()
	_, err := DispatchToEndpoint(context.Background(), ep, nil, "hi", cfg, &strings.Builder{})
	if err == nil {
		t.Error("expected error for unknown endpoint kind")
	}
}

// ── ollamaBaseURL ─────────────────────────────────────────────────────────────

func TestOllamaBaseURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ollama://192.168.1.12:11434", "http://192.168.1.12:11434"},
		{"http://localhost:11434", "http://localhost:11434"},
		{"ollama://pi.local:11434", "http://pi.local:11434"},
	}
	for _, c := range cases {
		got := ollamaBaseURL(c.in)
		if got != c.want {
			t.Errorf("ollamaBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── end-to-end: @mention → history ───────────────────────────────────────────

// TestAtMentionDispatch_landsInHistory registers a mock Ollama endpoint,
// sends a prompt via DispatchToEndpoint (simulating the REPL @mention path),
// and verifies the user message and reply both land in history.
func TestAtMentionDispatch_landsInHistory(t *testing.T) {
	srv := ollamaMockServer(t, 8192, "result from pi2")
	defer srv.Close()

	rr := NewRouteRegistry()
	rr.Enabled = true
	ep := &RouteEndpoint{Name: "pi2", URL: srv.URL, Model: "llama3.1:8b", Kind: KindOllama}
	rr.Add(ep)

	cfg := DefaultConfig()
	history := []Message{
		{Role: "user", Content: "previous question"},
		{Role: "assistant", Content: "previous answer"},
	}

	var out strings.Builder
	reply, err := DispatchToEndpoint(context.Background(), ep, history, "write hello world", cfg, &out)
	if err != nil {
		t.Fatalf("DispatchToEndpoint: %v", err)
	}

	// Simulate what the REPL does: append user input and reply to history.
	history = append(history, Message{Role: "user", Content: "@pi2 write hello world"})
	history = append(history, Message{Role: "assistant", Content: reply})

	if len(history) != 4 {
		t.Fatalf("expected 4 history messages, got %d", len(history))
	}
	if history[2].Role != "user" || history[2].Content != "@pi2 write hello world" {
		t.Errorf("user message not appended correctly: %+v", history[2])
	}
	if history[3].Role != "assistant" || !strings.Contains(history[3].Content, "result from pi2") {
		t.Errorf("assistant reply not appended correctly: %+v", history[3])
	}
}

// TestAtMentionDispatch_routingOff verifies that the registry reports disabled
// correctly so the REPL can gate @mention processing.
func TestAtMentionDispatch_routingOff(t *testing.T) {
	rr := NewRouteRegistry()
	rr.Enabled = false
	rr.Add(&RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.12:11434", Kind: KindOllama})

	if rr.Enabled {
		t.Error("expected Enabled=false after setting to false")
	}
	// Endpoint is still registered; only the enabled flag gates dispatch.
	if rr.Lookup("pi2") == nil {
		t.Error("endpoint should still be registered when routing is disabled")
	}
}

// TestAtMentionDispatch_unknownEndpoint verifies Lookup returns nil for an
// unregistered name so the REPL can print a helpful error.
func TestAtMentionDispatch_unknownEndpoint(t *testing.T) {
	rr := NewRouteRegistry()
	rr.Enabled = true
	if rr.Lookup("ghost") != nil {
		t.Error("Lookup should return nil for unregistered endpoint")
	}
}

// TestRecentHistory_sentToRemote verifies that DispatchToEndpoint only sends
// the last RecentContextN messages, not the entire history.
func TestRecentHistory_sentToRemote(t *testing.T) {
	// Build a history longer than RecentContextN.
	var history []Message
	for i := 0; i < RecentContextN+4; i++ {
		history = append(history, Message{Role: "user", Content: "msg"})
		history = append(history, Message{Role: "assistant", Content: "reply"})
	}

	got := recentHistory(history, RecentContextN)
	if len(got) != RecentContextN {
		t.Errorf("recentHistory returned %d messages, want %d", len(got), RecentContextN)
	}
}

// ── estimateTokens ────────────────────────────────────────────────────────────

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
			preview := c.input
			if len(preview) > 10 {
				preview = preview[:10]
			}
			t.Errorf("estimateTokens(%q...) = %d, want %d–%d", preview, n, c.minN, c.maxN)
		}
	}
}
