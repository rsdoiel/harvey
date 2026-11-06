package harvey

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// DR-0050: structured tool_calls is an opt-in per model on hailo, qwen3:1.7b
// only. Every other hailo model stays in prose mode and is never sent a tools key.

// toolServer answers /api/chat with one fixed line and records the request bodies.
type toolServer struct {
	*httptest.Server
	mu    sync.Mutex
	chats []map[string]any
}

func newToolServer(t *testing.T, reply string) *toolServer {
	t.Helper()
	ts := &toolServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		ts.mu.Lock()
		ts.chats = append(ts.chats, m)
		ts.mu.Unlock()
		io.WriteString(w, reply+"\n")
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *toolServer) sentTools() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, c := range ts.chats {
		if _, has := c["tools"]; has {
			return true
		}
	}
	return false
}

func echoTools() []Tool {
	return []Tool{{Type: "function", Function: Function{Name: "read_file", Description: "echo back",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}}}}}
}

const doneLine = `{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":1}`

func hailoClient(url, model string) *OllamaChatClient {
	c := newOllamaLLMClient(url, model, 0)
	c.SetEngine("hailo")
	return c
}

func TestNativeToolModels_Table(t *testing.T) {
	c, ok := capsForEngine("hailo")
	if !ok {
		t.Fatal("no hailo row")
	}
	if len(c.NativeToolModels) != 1 || c.NativeToolModels[0] != "qwen3:1.7b" {
		t.Errorf("NativeToolModels = %v, want [qwen3:1.7b] (DR-0050 item 2)", c.NativeToolModels)
	}
}

func TestNativeTools_RequestOnlyForListedHailoModel(t *testing.T) {
	for model, want := range map[string]bool{"qwen3:1.7b": true, "qwen2:1.5b": false, "llama3.2:1b": false, "deepseek_r1:1.5b": false} {
		ts := newToolServer(t, doneLine)
		c := hailoClient(ts.URL, model)
		if _, _, err := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, echoTools(), io.Discard); err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		if got := ts.sentTools(); got != want {
			t.Errorf("hailo %s: tools sent = %v, want %v", model, got, want)
		}
	}
}

// The leak seen on qwen3 (probe 2026-11-04): " echo" and " msg".
func TestNativeTools_ReplyTrimsNameAndArgumentKeys(t *testing.T) {
	ts := newToolServer(t, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":" read_file","arguments":{" path":"x"}}}]},"done":true,"done_reason":"stop"}`)
	c := hailoClient(ts.URL, "qwen3:1.7b")
	_, calls, err := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, echoTools(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Function.Name != "read_file" {
		t.Fatalf("calls = %+v, want one call named read_file", calls)
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["path"] != "x" || len(args) != 1 {
		t.Errorf("arguments = %s, want {\"path\":\"x\"}", calls[0].Function.Arguments)
	}
}

// DR-0050 item 3: a name Harvey did not offer is no call; the content is prose.
func TestNativeTools_UnofferedNameIsNoCall(t *testing.T) {
	ts := newToolServer(t, `{"message":{"role":"assistant","content":"sorry","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":true,"done_reason":"stop"}`)
	c := hailoClient(ts.URL, "qwen3:1.7b")
	var out strings.Builder
	_, calls, err := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, echoTools(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Errorf("calls = %+v, want none for a tool that was not offered", calls)
	}
	if !strings.Contains(out.String(), "sorry") {
		t.Errorf("content %q was not shown", out.String())
	}
}

// Ollama is untouched: its calls are passed through as sent.
func TestNativeTools_OllamaReplyNotRewritten(t *testing.T) {
	ts := newToolServer(t, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"other","arguments":{"k":"v"}}}]},"done":true,"done_reason":"stop"}`)
	c := newOllamaLLMClient(ts.URL, "llama3.2:3b", 0)
	_, calls, _ := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, echoTools(), io.Discard)
	if len(calls) != 1 || calls[0].Function.Name != "other" {
		t.Errorf("calls = %+v, want the unknown name passed through on Ollama", calls)
	}
}

func TestNativeTools_AgentPredicates(t *testing.T) {
	a := toolAgent(t, registryWithEcho())
	a.Client = hailoClient("http://127.0.0.1:1", "qwen3:1.7b")
	if a.activeEngineLacksTools() {
		t.Error("qwen3:1.7b on hailo reported as lacking tools")
	}
	if !a.toolsReliable() {
		t.Error("toolsReliable = false for qwen3:1.7b on hailo")
	}
	a.Client = hailoClient("http://127.0.0.1:1", "qwen2:1.5b")
	if !a.activeEngineLacksTools() || a.toolsReliable() {
		t.Error("qwen2:1.5b on hailo must stay without tools")
	}
}

// DR-0050 (amended): the card's window is 2,048 tokens, and Harvey's 15 tools
// are 7.6 KB, so hailo answers HTTP 500 (probe 2026-11-04). A listed model is
// sent only the read-only subset that fits.
func TestNativeTools_OnlyTheSubsetIsSent(t *testing.T) {
	ts := newToolServer(t, doneLine)
	c := hailoClient(ts.URL, "qwen3:1.7b")
	var offered []Tool
	for _, n := range []string{"write_file", "read_file", "run_command", "list_files", "git_command", "search_files"} {
		offered = append(offered, Tool{Type: "function", Function: Function{Name: n, Description: n}})
	}
	if _, _, err := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, offered, io.Discard); err != nil {
		t.Fatal(err)
	}
	sent, _ := ts.chats[0]["tools"].([]any)
	var got []string
	for _, s := range sent {
		got = append(got, s.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	want := "read_file,list_files,search_files"
	if strings.Join(got, ",") != want {
		t.Errorf("tools sent = %v, want %s", got, want)
	}
}

func TestNativeTools_NoSubsetToolMeansNoToolsKey(t *testing.T) {
	ts := newToolServer(t, doneLine)
	c := hailoClient(ts.URL, "qwen3:1.7b")
	only := []Tool{{Type: "function", Function: Function{Name: "run_command", Description: "x"}}}
	if _, _, err := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, only, io.Discard); err != nil {
		t.Fatal(err)
	}
	if ts.sentTools() {
		t.Error("a tools key was sent although none of the offered tools is in the subset")
	}
}

// A call to a tool that was filtered out is no call (it was never offered).
func TestNativeTools_CallToFilteredToolIsNoCall(t *testing.T) {
	ts := newToolServer(t, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"run_command","arguments":{"command":"ls"}}}]},"done":true,"done_reason":"stop"}`)
	c := hailoClient(ts.URL, "qwen3:1.7b")
	offered := append(echoTools(), Tool{Type: "function", Function: Function{Name: "run_command", Description: "x"}})
	_, calls, _ := c.ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, offered, io.Discard)
	if len(calls) != 0 {
		t.Errorf("calls = %+v, want none for run_command, which was filtered out", calls)
	}
}
