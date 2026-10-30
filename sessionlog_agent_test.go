package harvey

import (
	"bytes"
	"encoding/json"
	"io"
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
	a.logModelRequest(10, 4096)
	a.logModelRequest(10, 4096) // unchanged: no text again
	a.History[0].Content = "You are Harvey, trimmed to fit."
	a.logModelRequest(10, 4096)
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
