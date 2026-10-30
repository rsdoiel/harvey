package harvey

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// L3 of plans/session-log-plan.md: the session log is written beside the
// recorder, by the real Run path. This is the bug that started DR-0036 as a
// test: a session on one model, a switch to another mid-session, and a turn
// after it, which the --debug log lost (harvey-session-20261006-204135).

// fakeOllamaTwoModels serves /api/tags with two models, /api/show, and a
// streaming /api/chat that answers FIRST, SECOND, … and names the model that
// was asked, so a test can see which model answered.
func fakeOllamaTwoModels(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	turn := 0
	words := []string{"FIRST", "SECOND", "THIRD", "FOURTH"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
				{"name": "llama3.2:3b", "model": "llama3.2:3b", "size": 2000000000},
				{"name": "qwen2.5-coder:1.5b", "model": "qwen2.5-coder:1.5b", "size": 1000000000},
			}})
		case "/api/show":
			json.NewEncoder(w).Encode(map[string]any{
				"details":      map[string]any{"family": "llama", "parameter_size": "3B"},
				"capabilities": []string{"completion"},
			})
		case "/api/chat":
			var req struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			word := words[turn%len(words)]
			turn++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/x-ndjson")
			io.WriteString(w, `{"model":"`+req.Model+`","message":{"role":"assistant","content":"`+word+`"},"done":false}`+"\n")
			io.WriteString(w, `{"model":"`+req.Model+`","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":7,"eval_count":1}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// scriptedRun runs a whole session through Agent.Run with the given input
// lines on stdin, against the fake Ollama, and returns the sessions directory.
func scriptedRun(t *testing.T, debug bool, lines ...string) string {
	t.Helper()
	a, ws := runFixture(t, false)
	a.Config.Ollama.URL = fakeOllamaTwoModels(t).URL
	a.Config.Ollama.Model = "llama3.2:3b"
	a.Config.Session.AutoRecord = true
	a.Config.Security.SafeMode = false
	if debug {
		a.Config.Debug = true
	}
	// The first line answers the workspace-profile onboarding prompt (Enter
	// picks Blank), which a fresh workspace shows before the first turn.
	lines = append([]string{""}, lines...)
	in := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(in, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	a.stdin = f
	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	return filepath.Join(ws.Root, "agents", "sessions")
}

func TestRun_WritesASessionLogBesideTheFountainFile(t *testing.T) {
	dir := scriptedRun(t, false,
		"hello",
		"/model use qwen2.5-coder:1.5b",
		"again",
		"!echo hi",
		"/exit",
	)

	spmd, _ := filepath.Glob(filepath.Join(dir, "*.spmd"))
	if len(spmd) != 1 {
		t.Fatalf("want one Fountain file in %s, got %v", dir, spmd)
	}
	fountain, _ := os.ReadFile(spmd[0])
	for _, want := range []string{"FIRST", "SECOND"} {
		if !strings.Contains(string(fountain), want) {
			t.Errorf("the Fountain file lost the %s reply: it must be unchanged by the log", want)
		}
	}

	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(logs) != 1 {
		t.Fatalf("want one session log in %s, got %v", dir, logs)
	}
	if stem := strings.TrimSuffix(filepath.Base(spmd[0]), ".spmd"); strings.TrimSuffix(filepath.Base(logs[0]), ".jsonl") != stem {
		t.Errorf("log %s is not beside %s with the same stem (DR-0038)", logs[0], spmd[0])
	}

	events, torn, err := ReadSessionLog(logs[0])
	if err != nil || torn {
		t.Fatalf("ReadSessionLog: torn=%v err=%v", torn, err)
	}

	var kinds []string
	for _, e := range events {
		if e.Level == LevelInfo {
			kinds = append(kinds, string(e.Kind))
		}
	}
	want := []string{
		"session_start",
		"user_input", "model_request", "model_response",
		"model_switch", "command",
		"user_input", "model_request", "model_response",
		"tool_call", "tool_result",
		"command",
		"session_end",
	}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Fatalf("event kinds\n got: %v\nwant: %v", kinds, want)
	}

	// A command is written when it finishes, so its effects (here the switch)
	// come first, and it carries its status. The line is what the author typed.
	var cmds []struct{ Line, Status string }
	for _, e := range events {
		if e.Kind == KindCommand {
			var d struct {
				Line   string `json:"line"`
				Status string `json:"status"`
			}
			_ = json.Unmarshal(e.Data, &d)
			cmds = append(cmds, struct{ Line, Status string }{d.Line, d.Status})
		}
	}
	if len(cmds) != 2 || cmds[0].Line != "/model use qwen2.5-coder:1.5b" || cmds[0].Status != "ok" ||
		cmds[1].Line != "/exit" || cmds[1].Status != "ok" {
		t.Errorf("command events = %+v", cmds)
	}

	// The switch names the model it leaves and the one it enters.
	for _, e := range events {
		if e.Kind == KindModelSwitch {
			var d struct {
				From struct{ Backend, Model string } `json:"from"`
				To   struct{ Backend, Model string } `json:"to"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if d.From.Model != "llama3.2:3b" || d.To.Model != "qwen2.5-coder:1.5b" || d.To.Backend != "ollama" {
				t.Errorf("model_switch = %s", e.Data)
			}
		}
	}

	// One response per request, naming the request it answers; the turn after
	// the switch is asked of the new model and answered.
	var requests []Event
	responseTo := map[string]string{}
	for _, e := range events {
		switch e.Kind {
		case KindModelRequest:
			requests = append(requests, e)
		case KindModelResponse:
			var d struct {
				Request string `json:"request"`
				Status  string `json:"status"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if d.Status != "ok" {
				t.Errorf("%s: status %q, want ok", e.ID, d.Status)
			}
			responseTo[d.Request] = e.ID
		}
	}
	if len(requests) != 2 {
		t.Fatalf("%d model_request events, want 2", len(requests))
	}
	for _, r := range requests {
		if responseTo[r.ID] == "" {
			t.Errorf("request %s has no model_response naming it", r.ID)
		}
	}
	var second struct {
		Model  string `json:"model"`
		Policy string `json:"policy"`
		Form   string `json:"form"`
	}
	_ = json.Unmarshal(requests[1].Data, &second)
	if second.Model != "qwen2.5-coder:1.5b" || second.Policy != "replay" || second.Form != "assembled" {
		t.Errorf("the turn after the switch = %+v, want the new model under policy replay (DR-0040)", second)
	}

	// The user's shell command is the user's act: origin user, exit recorded.
	for _, e := range events {
		if e.Kind == KindToolCall {
			var d struct {
				Tool   string `json:"tool"`
				Origin string `json:"origin"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if d.Tool != "shell" || d.Origin != "user" {
				t.Errorf("shell tool_call = %+v, want tool shell, origin user", d)
			}
		}
		if e.Kind == KindToolResult {
			var d struct {
				Exit *int `json:"exit"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if d.Exit == nil || *d.Exit != 0 {
				t.Errorf("shell tool_result = %s, want exit 0", e.Data)
			}
		}
	}

	last := events[len(events)-1]
	var end struct {
		Reason string `json:"reason"`
		Events int    `json:"events"`
	}
	_ = json.Unmarshal(last.Data, &end)
	if last.Kind != KindSessionEnd || end.Reason != "quit" || end.Events != len(events) {
		t.Errorf("last event = %s %s, want session_end quit with events=%d", last.Kind, last.Data, len(events))
	}
}

func TestRun_DebugRaisesTheSessionLogsVerbosity(t *testing.T) {
	dir := scriptedRun(t, true, "hello", "/exit")
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(logs) != 1 {
		t.Fatalf("want one session log, got %v", logs)
	}
	events, _, err := ReadSessionLog(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	debug := 0
	for _, e := range events {
		if e.Level == LevelDebug {
			debug++
		}
	}
	if debug == 0 {
		t.Error("--debug wrote no level-debug events to the session log (DR-0038 item 5)")
	}
	var start struct {
		Debug bool `json:"debug"`
	}
	_ = json.Unmarshal(events[0].Data, &start)
	if !start.Debug {
		t.Error("session_start.debug should be true")
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "logs", "*.jsonl")); len(matches) > 0 {
		t.Errorf("--debug still wrote a separate log: %v", matches)
	}
}
