package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
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
	a.logModelRequest(a.History)
	a.logModelRequest(a.History) // unchanged: no text again
	a.History[0].Content = "You are Harvey, trimmed to fit."
	a.logModelRequest(a.History)
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
	calls []anyllm.ToolCall
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
func (c *scriptedToolClient) ChatWithTools(_ context.Context, _ []Message, _ []anyllm.Tool, out io.Writer) (ChatStats, []anyllm.ToolCall, error) {
	st := c.steps[c.i]
	c.i++
	fmt.Fprint(out, st.text)
	return ChatStats{PromptTokens: 5, ReplyTokens: 2}, st.calls, st.err
}
func (c *scriptedToolClient) Models(context.Context) ([]string, error) { return nil, nil }
func (c *scriptedToolClient) Close() error                             { return nil }

func toolCall(id, name, args string) anyllm.ToolCall {
	return anyllm.ToolCall{ID: id, Type: "function", Function: anyllm.FunctionCall{Name: name, Arguments: args}}
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

func TestToolLoop_LogsEachModelCallAndEachToolCall(t *testing.T) {
	a, path := agentWithLog(t)
	reg := NewToolRegistry()
	reg.RegisterTool("echo", "echoes", map[string]any{"type": "object"}, func(_ context.Context, args map[string]any) (string, error) {
		return "echoed", nil
	})
	reg.RegisterTool("boom", "fails", map[string]any{"type": "object"}, func(context.Context, map[string]any) (string, error) {
		return "", errors.New("it broke")
	})
	client := &scriptedToolClient{steps: []toolStep{
		{calls: []anyllm.ToolCall{toolCall("call_1", "echo", `{"x":"API_KEY=sk-tool-1"}`), toolCall("call_2", "boom", `{}`)}},
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
	// Each call is logged when it is made, so its result follows before the
	// next call starts: call, result, call, result.
	want := "user_input model_request model_response tool_call tool_result tool_call tool_result model_request model_response"
	if got := infoKinds(events); got != want {
		t.Fatalf("kinds\n got: %s\nwant: %s", got, want)
	}

	var calls, results, reqs, resps []Event
	for _, e := range events {
		switch e.Kind {
		case KindToolCall:
			calls = append(calls, e)
		case KindToolResult:
			results = append(results, e)
		case KindModelRequest:
			reqs = append(reqs, e)
		case KindModelResponse:
			resps = append(resps, e)
		}
	}
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
	}
	_ = json.Unmarshal(results[0].Data, &r0)
	_ = json.Unmarshal(results[1].Data, &r1)
	if r0.Call != calls[0].ID || r0.CallID != "call_1" || r0.Status != "ok" || r0.Output != "echoed" {
		t.Errorf("first tool_result = %s", results[0].Data)
	}
	if r1.Call != calls[1].ID || r1.Status != "error" || !strings.Contains(r1.Output, "it broke") {
		t.Errorf("a failing tool must be a result with status error: %s", results[1].Data)
	}
	// Each response names its request; the usage the backend reported is kept.
	for i := range reqs {
		var d struct {
			Request string `json:"request"`
			Status  string `json:"status"`
			Usage   struct{ Prompt, Completion int }
		}
		_ = json.Unmarshal(resps[i].Data, &d)
		if d.Request != reqs[i].ID || d.Status != "ok" || d.Usage.Prompt != 5 {
			t.Errorf("response %d = %s", i, resps[i].Data)
		}
	}
	// The second request's prompt includes the tool round: a different hash.
	var q0, q1 struct {
		Hash string `json:"prompt_sha256"`
		Tail struct {
			N int `json:"history_messages"`
		} `json:"tail"`
	}
	_ = json.Unmarshal(reqs[0].Data, &q0)
	_ = json.Unmarshal(reqs[1].Data, &q1)
	if q0.Hash == "" || q0.Hash == q1.Hash || q1.Tail.N <= q0.Tail.N {
		t.Errorf("requests should see a growing prompt: %s / %s", reqs[0].Data, reqs[1].Data)
	}
}

func TestToolLoop_AFailedModelCallIsLogged(t *testing.T) {
	a, path := agentWithLog(t)
	client := &scriptedToolClient{steps: []toolStep{{err: errors.New("backend down")}}}
	a.Client = client
	reg := NewToolRegistry()
	reg.RegisterTool("echo", "e", map[string]any{"type": "object"}, func(context.Context, map[string]any) (string, error) { return "", nil })
	ex := NewToolExecutor(reg, client, a.Config)
	ex.Events = a.toolEvents()
	if _, _, err := ex.RunToolLoop(context.Background(), []Message{{Role: "user", Content: "x"}}, io.Discard); err == nil {
		t.Fatal("want the backend error")
	}
	resps := eventsOf(t, a, path, KindModelResponse)
	if len(resps) != 1 || !strings.Contains(string(resps[0].Data), `"status":"error"`) {
		t.Errorf("responses = %v", resps)
	}
}

func TestToolEvents_NilWhenNoLogIsOpen(t *testing.T) {
	a := newTestAgent(t)
	if a.toolEvents() != nil {
		t.Error("toolEvents must be a nil interface with no log, so the executor skips it")
	}
}

func TestLoggedChat_OnePairPerCall(t *testing.T) {
	a, path := agentWithLog(t)
	a.Client = &scriptedToolClient{steps: []toolStep{{text: "one"}, {err: errors.New("nope")}, {err: context.Canceled}}}
	a.logUserInput("hi")
	var out strings.Builder
	if _, err := a.loggedChat(context.Background(), []Message{{Role: "user", Content: "a"}}, &out); err != nil || out.String() != "one" {
		t.Fatalf("first call: %v %q (output must still reach the caller's writer)", err, out.String())
	}
	_, _ = a.loggedChat(context.Background(), []Message{{Role: "user", Content: "a"}, {Role: "user", Content: "b"}}, io.Discard)
	_, _ = a.loggedChat(context.Background(), []Message{{Role: "user", Content: "c"}}, io.Discard)
	resps := eventsOf(t, a, path, KindModelResponse)
	if n := len(eventsOf(t, a, path, KindModelRequest)); n != 3 || len(resps) != 3 {
		t.Fatalf("%d requests and %d responses, want 3 and 3 (a retry is a call of its own)", n, len(resps))
	}
	for i, want := range []string{"ok", "error", "cancelled"} {
		if !strings.Contains(string(resps[i].Data), `"status":"`+want+`"`) {
			t.Errorf("response %d = %s, want status %s", i, resps[i].Data, want)
		}
	}
}
