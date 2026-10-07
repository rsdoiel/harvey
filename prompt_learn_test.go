package harvey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	anyllmerrors "github.com/mozilla-ai/any-llm-go/errors"
)

// Layer 3 of the prompt budget: learn a model's limit from a failure. When a
// turn fails the way an oversize prompt does, Harvey says so, retries once on
// a smaller copy of the conversation, and records the smaller size as the
// model's limit only if the retry succeeds, which proves the cause. See
// prompt_limit_test.go (layer 1) and prompt_fit_test.go (layer 2).

func TestIsPromptTooLargeFailure(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"truncated stream", fmt.Errorf("x: %w", ErrStreamTruncated), true},
		{"context length", anyllmerrors.NewContextLengthError("ollama", errors.New("context")), true},
		{"provider 500", anyllmerrors.NewProviderError("ollama", errors.New("500 Internal Server Error")), true},
		{"server not running", anyllmerrors.NewProviderError("ollama", errors.New("ollama server not running: connection refused")), false},
		{"model not found", anyllmerrors.NewModelNotFoundError("ollama", errors.New("404")), false},
		{"cancelled", context.Canceled, false},
		{"plain", errors.New("boom"), false},
		{"nil", nil, false},
	} {
		if got := isPromptTooLargeFailure(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// sizeLimitedServer answers /api/chat with HTTP 500 when the request's
// messages hold more than maxChars characters, as hailo-ollama does, and with
// a short reply otherwise. It records each request's size.
func sizeLimitedServer(t *testing.T, maxChars int) (*httptest.Server, func() []int) {
	t.Helper()
	var mu sync.Mutex
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		n := 0
		for _, m := range req.Messages {
			n += len(m.Content)
		}
		mu.Lock()
		sizes = append(sizes, n)
		mu.Unlock()
		if n > maxChars {
			http.Error(w, "read failed!", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"model":"m","message":{"role":"assistant","content":"ok"},"done":false}`)
		fmt.Fprintln(w, `{"model":"m","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":1}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []int { mu.Lock(); defer mu.Unlock(); return append([]int(nil), sizes...) }
}

// limitLearnAgent has a layered system prompt and three earlier turns, and a model
// whose limit is unknown (0), so layer 1 and 2 do nothing until one is learned.
func limitLearnAgent(t *testing.T, srvURL string) *Agent {
	t.Helper()
	a := promptLimitAgent(t, 0)
	a.Client = newOllamaLLMClient(srvURL, "llama3.2:3b", 0)
	full, cat := fitFull(30, 20)
	a.Config.SystemPrompt = full
	a.catalogBlock = cat
	a.History = []Message{{Role: "system", Content: full}}
	for i := 0; i < 3; i++ {
		a.History = append(a.History, turn(strings.Repeat("q", 300), strings.Repeat("a", 300))...)
	}
	return a
}

func TestRunChatTurn_LearnsTheLimitFromAFailureAndRetries(t *testing.T) {
	srv, sizes := sizeLimitedServer(t, 4000)
	a := limitLearnAgent(t, srv.URL)
	var out strings.Builder
	_, _, err := a.runChatTurn(context.Background(), "next question", &out, nil, false, "")
	if err != nil {
		t.Fatalf("turn failed: %v\n%s", err, out.String())
	}
	got := sizes()
	// The first retry (70%) is still over what the server takes, the second (45%) fits.
	if len(got) != 3 || !(got[1] < got[0] && got[2] < got[1]) {
		t.Fatalf("requests = %v, want three, each smaller than the last", got)
	}
	cap, _ := a.ModelCache.Get("llama3.2:3b")
	if cap == nil || cap.MaxPromptTokens == 0 {
		t.Fatalf("no limit was recorded: %+v", cap)
	}
	if cap.MaxPromptTokens*4 > 4000 {
		t.Errorf("recorded limit %d tokens is not under the %d chars the server took", cap.MaxPromptTokens, 4000)
	}
	for _, want := range []string{"retry", "prompt limit"} {
		if !strings.Contains(strings.ToLower(out.String()), want) {
			t.Errorf("output does not mention %q:\n%s", want, out.String())
		}
	}
	if len(a.History) >= 1+6+2 {
		t.Errorf("history has %d messages; the retry's smaller copy was not kept", len(a.History))
	}
}

// If the smaller retry also fails the cause was not size: report the original
// error, record nothing, and keep the conversation as it was.
func TestRunChatTurn_FailedRetryRecordsNothingAndKeepsHistory(t *testing.T) {
	srv, sizes := sizeLimitedServer(t, 0)
	a := limitLearnAgent(t, srv.URL)
	before := len(a.History)
	_, _, err := a.runChatTurn(context.Background(), "next question", &strings.Builder{}, nil, false, "")
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if n := len(sizes()); n != 1+len(promptRetryPercents) {
		t.Errorf("%d requests, want %d (the original and each smaller retry)", n, 1+len(promptRetryPercents))
	}
	if cap, _ := a.ModelCache.Get("llama3.2:3b"); cap != nil && cap.MaxPromptTokens != 0 {
		t.Errorf("recorded a limit %d after a failed retry", cap.MaxPromptTokens)
	}
	if len(a.History) != before {
		t.Errorf("history has %d messages, want %d", len(a.History), before)
	}
}

func TestRunChatTurn_NoRetryForATinyPrompt(t *testing.T) {
	srv, sizes := sizeLimitedServer(t, 0)
	a := promptLimitAgent(t, 0)
	a.Client = newOllamaLLMClient(srv.URL, "llama3.2:3b", 0)
	_, _, err := a.runChatTurn(context.Background(), "hi", &strings.Builder{}, nil, false, "")
	if err == nil {
		t.Fatal("expected the failure")
	}
	if n := len(sizes()); n != 1 {
		t.Errorf("%d requests for a tiny prompt, want 1", n)
	}
}

func TestRunChatTurn_NoRetryWhenTheServerIsNotThere(t *testing.T) {
	srv, sizes := sizeLimitedServer(t, 100000)
	a := limitLearnAgent(t, srv.URL)
	srv.Close() // connection refused: not a size problem
	_, _, err := a.runChatTurn(context.Background(), "next", &strings.Builder{}, nil, false, "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := len(sizes()); n != 0 {
		t.Errorf("%d requests reached a closed server", n)
	}
}
