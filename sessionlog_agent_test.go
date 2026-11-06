package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// agentWithLog is a test agent with a session log open in a temp directory.
func agentWithLog(t *testing.T) (*Agent, string) {
	t.Helper()
	a := newTestAgent(t)
	a.registerCommands()
	opts := testLogOpts(t)
	opts.Start.Workspace = a.Workspace.Root
	l, err := OpenSessionLog(opts)
	if err != nil {
		t.Fatal(err)
	}
	a.SessionLog = l
	t.Cleanup(func() { a.closeSessionLog("test") })
	return a, logPath(opts)
}

func eventsOf(t *testing.T, a *Agent, path string, kind EventKind) []Event {
	t.Helper()
	events, _, err := ReadSessionLog(path)
	if err != nil {
		t.Fatalf("ReadSessionLog: %v", err)
	}
	var out []Event
	for _, e := range events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestDispatch_LogsACommandWithItsStatus(t *testing.T) {
	a, path := agentWithLog(t)
	if _, err := a.dispatch("/nosuchcommand", io.Discard); err == nil {
		t.Fatal("an unknown command should fail")
	}
	if _, err := a.dispatch("/help topics", io.Discard); err != nil {
		t.Fatal(err)
	}
	cmds := eventsOf(t, a, path, KindCommand)
	if len(cmds) != 2 {
		t.Fatalf("%d command events, want 2", len(cmds))
	}
	var d0, d1 struct{ Line, Status string }
	_ = json.Unmarshal(cmds[0].Data, &d0)
	_ = json.Unmarshal(cmds[1].Data, &d1)
	if d0.Line != "/nosuchcommand" || d0.Status != "usage" {
		t.Errorf("failed command = %+v, want status usage (the exit class name)", d0)
	}
	if d1.Line != "/help topics" || d1.Status != "ok" {
		t.Errorf("good command = %+v", d1)
	}
}

func TestDispatch_ScrubsTheCommandLine(t *testing.T) {
	a, path := agentWithLog(t)
	_, _ = a.dispatch("/help API_KEY=sk-live-123", io.Discard)
	cmds := eventsOf(t, a, path, KindCommand)
	if len(cmds) != 1 || bytes.Contains(cmds[0].Data, []byte("sk-live-123")) {
		t.Errorf("command event leaks a secret: %v", cmds)
	}
}

func TestDispatch_ExitIsLoggedToo(t *testing.T) {
	a, path := agentWithLog(t)
	quit, err := a.dispatch("/exit", io.Discard)
	if !quit || err != nil {
		t.Fatalf("dispatch /exit = %v, %v", quit, err)
	}
	if n := len(eventsOf(t, a, path, KindCommand)); n != 1 {
		t.Errorf("%d command events for /exit, want 1", n)
	}
}

func TestDispatch_NoLogIsNoProblem(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	if _, err := a.dispatch("/help topics", io.Discard); err != nil {
		t.Fatalf("dispatch without a log: %v", err)
	}
}

func TestModelSwitch_IsLoggedFromAnyOllamaShapedSwitch(t *testing.T) {
	a, path := agentWithLog(t)
	a.Client = newOllamaLLMClient("http://127.0.0.1:1", "llama3.2:3b", 0)
	a.logModel = ModelRef{Backend: "ollama", Model: "llama3.2:3b"}
	a.useModelAt("ollama", "http://127.0.0.1:1", "qwen2.5-coder:1.5b")
	sw := eventsOf(t, a, path, KindModelSwitch)
	if len(sw) != 1 {
		t.Fatalf("%d model_switch events, want 1", len(sw))
	}
	var d struct {
		From, To ModelRef
	}
	_ = json.Unmarshal(sw[0].Data, &d)
	if d.From.Model != "llama3.2:3b" || d.To.Model != "qwen2.5-coder:1.5b" || d.To.Backend != "ollama" {
		t.Errorf("model_switch = %s", sw[0].Data)
	}
	// The next switch starts from the model the last one entered.
	a.useModelAt("ollama", "http://127.0.0.1:1", "phi3:mini")
	sw = eventsOf(t, a, path, KindModelSwitch)
	_ = json.Unmarshal(sw[1].Data, &d)
	if d.From.Model != "qwen2.5-coder:1.5b" || d.To.Model != "phi3:mini" {
		t.Errorf("second switch = %s", sw[1].Data)
	}
}

func TestModelRequest_CarriesTheSystemPromptWhenItsVersionChanges(t *testing.T) {
	a, path := agentWithLog(t)
	a.Client = newOllamaLLMClient("http://127.0.0.1:1", "llama3.2:3b", 0)
	a.AddMessage("system", "You are Harvey.")
	a.logUserInput("one")
	a.logModelRequest(withCallPurpose(context.Background(), purposeTurn, nil), a.History, 0)
	a.logModelRequest(withCallPurpose(context.Background(), purposeTurn, nil), a.History, 0) // unchanged: no text again
	a.History[0].Content = "You are Harvey, trimmed to fit."
	a.logModelRequest(withCallPurpose(context.Background(), purposeTurn, nil), a.History, 0)
	reqs := eventsOf(t, a, path, KindModelRequest)
	if len(reqs) != 3 {
		t.Fatalf("%d requests", len(reqs))
	}
	has := func(e Event) bool { return strings.Contains(string(e.Data), `"system_text"`) }
	if !has(reqs[0]) {
		t.Errorf("the first request after the log opened with a different system prompt should carry it: %s", reqs[0].Data)
	}
	if has(reqs[1]) {
		t.Errorf("an unchanged system prompt must not be repeated: %s", reqs[1].Data)
	}
	if !has(reqs[2]) || !strings.Contains(string(reqs[2].Data), "trimmed to fit") {
		t.Errorf("a changed system prompt must be logged once: %s", reqs[2].Data)
	}
}

// ─── the tool loop and every direct model call ───────────────────────────────

type toolStep struct {
	text  string
	calls []ToolCall
	err   error
}

// scriptedToolClient is a ToolCapable LLMClient that plays a fixed script, one
// step per model call.
type scriptedToolClient struct {
	steps []toolStep
	i     int
}

func (c *scriptedToolClient) Name() string { return "mock (m1)" }
func (c *scriptedToolClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	st := c.steps[c.i]
	c.i++
	fmt.Fprint(out, st.text)
	return ChatStats{}, st.err
}
func (c *scriptedToolClient) ChatWithTools(_ context.Context, _ []Message, _ []Tool, out io.Writer) (ChatStats, []ToolCall, error) {
	st := c.steps[c.i]
	c.i++
	fmt.Fprint(out, st.text)
	return ChatStats{PromptTokens: 5, ReplyTokens: 2}, st.calls, st.err
}
func (c *scriptedToolClient) Models(context.Context) ([]string, error) { return nil, nil }
func (c *scriptedToolClient) Close() error                             { return nil }

func toolCall(id, name, args string) ToolCall {
	return ToolCall{ID: id, Type: "function", Function: FunctionCall{Name: name, Arguments: args}}
}

func infoKinds(events []Event) string {
	var k []string
	for _, e := range events {
		if e.Level == LevelInfo && e.Kind != KindSessionStart {
			k = append(k, string(e.Kind))
		}
	}
	return strings.Join(k, " ")
}

func TestToolLoop_LogsEachToolCallAndItsResult(t *testing.T) {
	a, path := agentWithLog(t)
	reg := NewToolRegistry()
	reg.RegisterTool("echo", "echoes", map[string]any{"type": "object"}, func(_ context.Context, args map[string]any) (string, error) {
		return "echoed", nil
	})
	reg.RegisterTool("boom", "fails", map[string]any{"type": "object"}, func(context.Context, map[string]any) (string, error) {
		return "", errors.New("it broke")
	})
	client := &scriptedToolClient{steps: []toolStep{
		{calls: []ToolCall{toolCall("call_1", "echo", `{"x":"API_KEY=sk-tool-1"}`), toolCall("call_2", "boom", `{}`)}},
		{text: "all done"},
	}}
	a.Client = client
	a.logUserInput("use the tools")
	ex := NewToolExecutor(reg, client, a.Config)
	ex.Events = a.toolEvents()
	if _, _, err := ex.RunToolLoop(context.Background(), []Message{{Role: "user", Content: "use the tools"}}, io.Discard); err != nil {
		t.Fatal(err)
	}

	events, _, err := ReadSessionLog(path)
	if err != nil {
		t.Fatal(err)
	}
	// The model calls are logged by the client, which this scripted double is
	// not; the loop logs the tools and the shortening of a finished round. Each call is logged when it is made, so its
	// result follows before the next call starts.
	if got, want := infoKinds(events), "user_input tool_call tool_result tool_call tool_result compaction"; got != want {
		t.Fatalf("kinds\n got: %s\nwant: %s", got, want)
	}
	calls := eventsOf(t, a, path, KindToolCall)
	results := eventsOf(t, a, path, KindToolResult)
	var c0 struct {
		Tool   string `json:"tool"`
		Origin string `json:"origin"`
		CallID string `json:"call_id"`
		Turn   int    `json:"turn"`
	}
	_ = json.Unmarshal(calls[0].Data, &c0)
	if c0.Tool != "echo" || c0.Origin != "model" || c0.CallID != "call_1" || c0.Turn != 1 {
		t.Errorf("first tool_call = %s", calls[0].Data)
	}
	if bytes.Contains(calls[0].Data, []byte("sk-tool-1")) {
		t.Errorf("tool arguments were not scrubbed: %s", calls[0].Data)
	}
	var r0, r1 struct {
		Call   string `json:"call"`
		CallID string `json:"call_id"`
		Status string `json:"status"`
		Output string `json:"output"`
		MS     *int64 `json:"ms"`
	}
	_ = json.Unmarshal(results[0].Data, &r0)
	_ = json.Unmarshal(results[1].Data, &r1)
	if r0.Call != calls[0].ID || r0.CallID != "call_1" || r0.Status != "ok" || r0.Output != "echoed" || r0.MS == nil {
		t.Errorf("first tool_result = %s", results[0].Data)
	}
	if r1.Call != calls[1].ID || r1.Status != "error" || !strings.Contains(r1.Output, "it broke") {
		t.Errorf("a failing tool must be a result with status error: %s", results[1].Data)
	}
}

func TestToolEvents_NilWhenNoLogIsOpen(t *testing.T) {
	a := newTestAgent(t)
	if a.toolEvents() != nil {
		t.Error("toolEvents must be a nil interface with no log, so the executor skips it")
	}
}

// ─── the client hook: every model call, whoever makes it ─────────────────────

// wiredClient is a real *OllamaChatClient against the fake Ollama, reporting to a.
func wiredClient(t *testing.T, a *Agent, url string) *OllamaChatClient {
	t.Helper()
	c := newOllamaLLMClient(url, "llama3.2:3b", 0)
	a.Client = a.wireSessionLog(c)
	return c
}

func TestClientHook_TurnCallsAreAssembledRequestsUnderPolicyReplay(t *testing.T) {
	a, path := agentWithLog(t)
	c := wiredClient(t, a, fakeOllamaChat(t).URL)
	a.logUserInput("hi")
	ctx := withCallPurpose(context.Background(), purposeTurn, nil)
	var out strings.Builder
	if _, err := c.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, &out); err != nil || out.String() != "hi" {
		t.Fatalf("Chat: %v %q (the reply must still reach the caller)", err, out.String())
	}
	reqs := eventsOf(t, a, path, KindModelRequest)
	resps := eventsOf(t, a, path, KindModelResponse)
	if len(reqs) != 1 || len(resps) != 1 {
		t.Fatalf("%d requests and %d responses, want 1 and 1", len(reqs), len(resps))
	}
	var q struct {
		Form, Policy, Model, Backend string
		Turn                         int
	}
	_ = json.Unmarshal(reqs[0].Data, &q)
	if q.Form != "assembled" || q.Policy != "replay" || q.Model != "llama3.2:3b" || q.Backend != "ollama" || q.Turn != 1 {
		t.Errorf("request = %s", reqs[0].Data)
	}
	var r struct {
		Request, Status, Text string
		Usage                 struct{ Prompt, Completion int }
	}
	_ = json.Unmarshal(resps[0].Data, &r)
	if r.Request != reqs[0].ID || r.Status != "ok" || r.Text != "hi" {
		t.Errorf("response = %s", resps[0].Data)
	}
}

func TestClientHook_ACallWithoutAPurposeIsAnAdHocRequestWithItsPromptKept(t *testing.T) {
	a, path := agentWithLog(t)
	c := wiredClient(t, a, fakeOllamaChat(t).URL)
	src := map[string]any{"path": "main.go", "from_line": 1, "to_line": 80}
	ctx := withCallPurpose(context.Background(), "chunk", src)
	if _, err := c.Chat(ctx, []Message{{Role: "user", Content: "summarise lines 1-80 of main.go"}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "something else"}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	reqs := eventsOf(t, a, path, KindModelRequest)
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(reqs))
	}
	var q0, q1 struct {
		Form, Purpose string
		Source        map[string]any
		Body          Content
	}
	_ = json.Unmarshal(reqs[0].Data, &q0)
	_ = json.Unmarshal(reqs[1].Data, &q1)
	if q0.Form != "adhoc" || q0.Purpose != "chunk" || q0.Source["path"] != "main.go" || q0.Source["to_line"] != float64(80) {
		t.Errorf("chunk request = %s", reqs[0].Data)
	}
	if q1.Form != "adhoc" || q1.Purpose != "unspecified" {
		t.Errorf("a call with no purpose must still be logged, as unspecified: %s", reqs[1].Data)
	}
	if q0.Body.Blob == "" {
		t.Errorf("the prompt of an ad-hoc call is always kept as a blob: %s", reqs[0].Data)
	}
}

func TestClientHook_AFailedCallIsAResponseWithStatusError(t *testing.T) {
	a, path := agentWithLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := wiredClient(t, a, srv.URL)
	if _, err := c.Chat(withCallPurpose(context.Background(), purposeTurn, nil), []Message{{Role: "user", Content: "x"}}, io.Discard); err == nil {
		t.Fatal("want the server error")
	}
	resps := eventsOf(t, a, path, KindModelResponse)
	if len(resps) != 1 || !strings.Contains(string(resps[0].Data), `"status":"error"`) {
		t.Errorf("responses = %v", resps)
	}
}

func TestClientHook_ACancelledCallIsCancelled(t *testing.T) {
	a, path := agentWithLog(t)
	c := wiredClient(t, a, fakeOllamaChat(t).URL)
	ctx, cancel := context.WithCancel(withCallPurpose(context.Background(), purposeTurn, nil))
	cancel()
	_, _ = c.Chat(ctx, []Message{{Role: "user", Content: "x"}}, io.Discard)
	resps := eventsOf(t, a, path, KindModelResponse)
	if len(resps) != 1 || !strings.Contains(string(resps[0].Data), `"status":"cancelled"`) {
		t.Errorf("responses = %v", resps)
	}
}

func TestClientHook_DebugKeepsTheExactMessagesAsALevelDebugEvent(t *testing.T) {
	a, path := agentWithLog(t)
	a.Config.Debug = true
	// agentWithLog opens the log without Debug; reopen with it.
	a.closeSessionLog("test")
	opts := testLogOpts(t)
	opts.Debug = true
	opts.Session = "dbg"
	l, err := OpenSessionLog(opts)
	if err != nil {
		t.Fatal(err)
	}
	a.SessionLog, path = l, logPath(opts)
	defer a.closeSessionLog("test")
	c := wiredClient(t, a, fakeOllamaChat(t).URL)
	if _, err := c.Chat(withCallPurpose(context.Background(), purposeTurn, nil), []Message{{Role: "user", Content: "secret sauce"}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	events, _, _ := ReadSessionLog(path)
	var body *Event
	for i := range events {
		if events[i].Kind == KindModelRequest && events[i].Level == LevelDebug {
			body = &events[i]
		}
	}
	if body == nil {
		t.Fatal("no level-debug model_request with the messages")
	}
	if !strings.Contains(string(body.Data), `"form":"body"`) {
		t.Errorf("body event = %s", body.Data)
	}
}

func TestClientHook_NoLogMeansNoEventsAndNoTrouble(t *testing.T) {
	a := newTestAgent(t)
	c := wiredClient(t, a, fakeOllamaChat(t).URL)
	var out strings.Builder
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, &out); err != nil || out.String() != "hi" {
		t.Fatalf("Chat with a wired client and no log: %v %q", err, out.String())
	}
}
