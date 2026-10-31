package harvey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// chatFake is a fake Ollama /api/chat. It records the last request body and
// answers with the scripted lines (or status and body).
type chatFake struct {
	srv    *httptest.Server
	last   map[string]any
	lines  []string
	status int
	body   string
	hang   chan struct{}
}

func newChatFake(t *testing.T, lines ...string) *chatFake {
	t.Helper()
	f := &chatFake{lines: lines}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			b, _ := io.ReadAll(r.Body)
			f.last = nil
			_ = json.Unmarshal(b, &f.last)
			if f.hang != nil {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				select {
				case <-f.hang:
				case <-r.Context().Done():
				}
				return
			}
			if f.status != 0 {
				w.WriteHeader(f.status)
				fmt.Fprint(w, f.body)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			for _, l := range f.lines {
				fmt.Fprintln(w, l)
			}
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"model-a"},{"name":"model-b"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

const (
	chatHello = `{"message":{"role":"assistant","content":"Hello "},"done":false}`
	chatWorld = `{"message":{"role":"assistant","content":"World"},"done":false}`
	chatDone  = `{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":5}`
)

func userMsg(s string) []Message { return []Message{{Role: "user", Content: s}} }

func TestOllamaChat_writesContentAndMapsUsage(t *testing.T) {
	f := newChatFake(t, chatHello, chatWorld, chatDone)
	c := newOllamaLLMClient(f.srv.URL, "m", 0)
	var buf strings.Builder
	stats, err := c.Chat(context.Background(), userMsg("hi"), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "Hello World" {
		t.Errorf("output = %q", buf.String())
	}
	if stats.PromptTokens != 10 || stats.ReplyTokens != 5 {
		t.Errorf("usage = %d/%d, want 10/5", stats.PromptTokens, stats.ReplyTokens)
	}
	if stats.Elapsed <= 0 || stats.TokensPerSec <= 0 {
		t.Errorf("Elapsed %v TokensPerSec %v should be positive", stats.Elapsed, stats.TokensPerSec)
	}
	if f.last["stream"] != true || f.last["model"] != "m" {
		t.Errorf("request = %v", f.last)
	}
}

// hailo-ollama reports eval_count but no prompt_eval_count: the missing count
// stays zero, which the log reads as "not reported" (DR-0040).
func TestOllamaChat_missingPromptCountStaysZero(t *testing.T) {
	f := newChatFake(t, chatHello, `{"message":{"content":""},"done":true,"done_reason":"stop","eval_count":7}`)
	stats, err := newOllamaLLMClient(f.srv.URL, "m", 0).Chat(context.Background(), userMsg("hi"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PromptTokens != 0 || stats.ReplyTokens != 7 {
		t.Errorf("usage = %d/%d, want 0/7", stats.PromptTokens, stats.ReplyTokens)
	}
}

func TestOllamaChat_toolCallInAChunk(t *testing.T) {
	f := newChatFake(t,
		`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"main.go"}}},{"function":{"name":"list_files","arguments":{}}}]},"done":false}`,
		chatDone)
	tools := []Tool{{Type: "function", Function: Function{Name: "read_file", Parameters: map[string]any{"type": "object"}}}}
	_, calls, err := newOllamaLLMClient(f.srv.URL, "m", 0).ChatWithTools(context.Background(), userMsg("go"), tools, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].ID != "call_0" || calls[1].ID != "call_1" || calls[0].Type != "function" {
		t.Errorf("ids/types = %+v", calls)
	}
	if calls[0].Function.Name != "read_file" || calls[0].Function.Arguments != `{"path":"main.go"}` {
		t.Errorf("call 0 = %+v", calls[0])
	}
	if calls[1].Function.Arguments != "{}" {
		t.Errorf("empty arguments = %q, want {}", calls[1].Function.Arguments)
	}
	if got, _ := f.last["tools"].([]any); len(got) != 1 {
		t.Errorf("tools in request = %v", f.last["tools"])
	}
}

func TestOllamaChat_midStreamErrorLine(t *testing.T) {
	f := newChatFake(t, chatHello, `{"error":"model runner crashed"}`)
	_, err := newOllamaLLMClient(f.srv.URL, "m", 0).Chat(context.Background(), userMsg("hi"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "model runner crashed") {
		t.Fatalf("want the server's message, got %v", err)
	}
	if ExitCodeFor(err) != ClassUnavailable {
		t.Errorf("class %v, want unavailable", ExitCodeFor(err))
	}
}

func TestOllamaChat_nonOKReplyCarriesBody(t *testing.T) {
	f := newChatFake(t)
	f.status, f.body = http.StatusNotFound, `{"error":"model 'nope' not found"}`
	_, err := newOllamaLLMClient(f.srv.URL, "nope", 0).Chat(context.Background(), userMsg("hi"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "model 'nope' not found") || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want status and server message, got %v", err)
	}
}

func TestOllamaChat_streamWithoutDoneIsTruncated(t *testing.T) {
	f := newChatFake(t, chatHello)
	_, err := newOllamaLLMClient(f.srv.URL, "m", 0).Chat(context.Background(), userMsg("hi"), io.Discard)
	if !errors.Is(err, ErrStreamTruncated) || ExitCodeFor(err) != ClassIO {
		t.Fatalf("want ErrStreamTruncated (io), got %v (%v)", err, ExitCodeFor(err))
	}
	f2 := newChatFake(t)
	_, err = newOllamaLLMClient(f2.srv.URL, "m", 0).Chat(context.Background(), userMsg("hi"), io.Discard)
	if !errors.Is(err, ErrStreamTruncated) {
		t.Fatalf("an empty stream is truncated too, got %v", err)
	}
}

// A final line with no trailing newline is still read.
func TestOllamaChat_lastLineWithoutNewline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatHello+"\n"+chatDone)
	}))
	defer srv.Close()
	var buf strings.Builder
	if _, err := newOllamaLLMClient(srv.URL, "m", 0).Chat(context.Background(), userMsg("hi"), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "Hello " {
		t.Errorf("output = %q", buf.String())
	}
}

func TestOllamaChat_cancellation(t *testing.T) {
	f := newChatFake(t)
	f.hang = make(chan struct{})
	defer close(f.hang)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := newOllamaLLMClient(f.srv.URL, "m", 0).Chat(ctx, userMsg("hi"), io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the context error, got %v", err)
	}
}

func TestOllamaChat_hailoNeverSendsTools(t *testing.T) {
	f := newChatFake(t, chatDone)
	c := newOllamaLLMClient(f.srv.URL, "m", 0)
	c.SetEngine("hailo")
	tools := []Tool{{Type: "function", Function: Function{Name: "x"}}}
	if _, _, err := c.ChatWithTools(context.Background(), userMsg("hi"), tools, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, has := f.last["tools"]; has {
		t.Errorf("a hailo client sent a tools key: %v", f.last["tools"])
	}
}

func TestOllamaChat_numCtxOnlyWhenTheWindowIsKnown(t *testing.T) {
	f := newChatFake(t, chatDone)
	c := newOllamaLLMClient(f.srv.URL, "m", 0)
	if _, err := c.Chat(context.Background(), userMsg("hi"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if opts, has := f.last["options"]; has {
		t.Errorf("no window known, but options sent: %v", opts)
	}
	c.SetWindow(func() int { return 0 })
	_, _ = c.Chat(context.Background(), userMsg("hi"), io.Discard)
	if _, has := f.last["options"]; has {
		t.Error("a window of 0 must send no num_ctx")
	}
	c.SetWindow(func() int { return 4096 })
	c.SetMaxTokens(256)
	_, _ = c.Chat(context.Background(), userMsg("hi"), io.Discard)
	opts, _ := f.last["options"].(map[string]any)
	if opts["num_ctx"] != float64(4096) || opts["num_predict"] != float64(256) {
		t.Errorf("options = %v, want num_ctx 4096 and num_predict 256", opts)
	}
}

func TestOllamaChat_imagePartsAreSentAsBase64(t *testing.T) {
	f := newChatFake(t, chatDone)
	msgs := []Message{{Role: "user", Parts: []ContentPart{
		{Type: "text", Text: "what is this?"},
		{Type: "image_url", ImageURL: &ImageURL{URL: "data:image/png;base64,AAAA"}},
		{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/not-sent.png"}},
	}}}
	if _, err := newOllamaLLMClient(f.srv.URL, "m", 0).Chat(context.Background(), msgs, io.Discard); err != nil {
		t.Fatal(err)
	}
	m := f.last["messages"].([]any)[0].(map[string]any)
	if m["content"] != "what is this?" {
		t.Errorf("content = %v", m["content"])
	}
	imgs, _ := m["images"].([]any)
	if len(imgs) != 1 || imgs[0] != "AAAA" {
		t.Errorf("images = %v, want the one data URL as bare base64", m["images"])
	}
}

// A tool result goes out with role "tool" and the name of the tool it answers,
// and an assistant's tool call goes out with its arguments as an object.
func TestOllamaChat_toolResultRoleAndName(t *testing.T) {
	f := newChatFake(t, chatDone)
	msgs := []Message{
		{Role: "user", Content: "read it"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_0", Type: "function", Function: FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}}}},
		{Role: "tool", ToolCallID: "call_0", Content: "package a"},
	}
	if _, err := newOllamaLLMClient(f.srv.URL, "m", 0).Chat(context.Background(), msgs, io.Discard); err != nil {
		t.Fatal(err)
	}
	got := f.last["messages"].([]any)
	asst := got[1].(map[string]any)
	tc := asst["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if tc["name"] != "read_file" || tc["arguments"].(map[string]any)["path"] != "a.go" {
		t.Errorf("assistant tool_calls = %v", asst["tool_calls"])
	}
	res := got[2].(map[string]any)
	if res["role"] != "tool" || res["tool_name"] != "read_file" || res["content"] != "package a" {
		t.Errorf("tool result = %v", res)
	}
}

func TestOllamaChat_modelsAndAccessors(t *testing.T) {
	f := newChatFake(t)
	c := newOllamaLLMClient(f.srv.URL+"/", "qwen3:4b", 0)
	names, err := c.Models(context.Background())
	if err != nil || len(names) != 2 || names[0] != "model-a" {
		t.Errorf("Models() = %v, %v", names, err)
	}
	if c.ModelName() != "qwen3:4b" || c.ProviderName() != "ollama" || c.BackendURL() != f.srv.URL {
		t.Errorf("accessors: %q %q %q", c.ModelName(), c.ProviderName(), c.BackendURL())
	}
	if c.Name() != "ollama (qwen3:4b)" {
		t.Errorf("Name() = %q", c.Name())
	}
	if !c.ProviderCapabilities().CompletionTools || !c.ProviderCapabilities().CompletionImage {
		t.Error("an Ollama client should report tools and image support")
	}
	if err := c.Close(); err != nil {
		t.Error(err)
	}
	if newOllamaLLMClient("", "m", 0).BackendURL() != defaultOllamaURL {
		t.Error("an empty URL should mean the Ollama default")
	}
}

// Every call is reported to the session log: one begin and one end.
type countingLog struct{ begun, ended int }

func (l *countingLog) BeginModelCall(context.Context, []Message, int) func(string, ChatStats, error) {
	l.begun++
	return func(text string, _ ChatStats, _ error) { l.ended++ }
}

func TestOllamaChat_reportsEveryCallToTheLog(t *testing.T) {
	f := newChatFake(t, chatHello, chatDone)
	c := newOllamaLLMClient(f.srv.URL, "m", 0)
	log := &countingLog{}
	c.CallLog = log
	_, _ = c.Chat(context.Background(), userMsg("hi"), io.Discard)
	f.status, f.body = 500, "boom"
	_, _ = c.Chat(context.Background(), userMsg("hi"), io.Discard)
	if log.begun != 2 || log.ended != 2 {
		t.Errorf("begun %d ended %d, want 2 and 2", log.begun, log.ended)
	}
}
