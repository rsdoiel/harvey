package harvey

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Bug (TODO.md): when the server closes /api/chat early, harvey showed an
// empty reply and recorded a 0-token success. These tests drive the real
// Ollama chat client against a server that misbehaves.

func chatAgainst(t *testing.T, h http.HandlerFunc) (string, ChatStats, error) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newOllamaLLMClient(srv.URL, "m", 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out strings.Builder
	stats, err := c.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, &out)
	return out.String(), stats, err
}

// closeEarly makes the server drop the connection mid-response, the way
// hailo-ollama does past its prompt limit.
func closeEarly(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)
	if body != "" {
		fmt.Fprint(w, body)
	}
	w.(http.Flusher).Flush()
	conn, _, _ := w.(http.Hijacker).Hijack()
	conn.Close()
}

func TestChat_StreamClosedWithNoDataIsAnError(t *testing.T) {
	out, _, err := chatAgainst(t, func(w http.ResponseWriter, r *http.Request) { closeEarly(w, "") })
	if err == nil {
		t.Fatalf("no error for a stream that closed with no data (output %q)", out)
	}
}

func TestChat_StreamCutOffMidReplyIsAnError(t *testing.T) {
	part := `{"model":"m","message":{"role":"assistant","content":"par"},"done":false}` + "\n"
	out, _, err := chatAgainst(t, func(w http.ResponseWriter, r *http.Request) { closeEarly(w, part) })
	if err == nil {
		t.Fatalf("no error for a truncated stream (output %q)", out)
	}
}

func TestChat_CompleteStreamStillSucceeds(t *testing.T) {
	body := `{"model":"m","message":{"role":"assistant","content":"ok"},"done":false}` + "\n" +
		`{"model":"m","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}` + "\n"
	out, _, err := chatAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, body)
	})
	if err != nil || out != "ok" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
