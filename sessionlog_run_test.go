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

// scriptedReply, when set, is what the fake model says to every turn, so a test
// can make it reply with tagged code blocks. Tests reset it with t.Cleanup.
var scriptedReply string

// scriptedTerminal makes scripted runs interactive, as at a terminal: only then
// are write offers made. Tests reset it with t.Cleanup.
var scriptedTerminal bool

// scriptedFailOn, when above zero, makes the nth chat call of a scripted run fail
// with an HTTP 500. Tests reset it with t.Cleanup.
var scriptedFailOn int

// scriptedRender makes a scripted run render its Fountain file from the log at
// close, as harvey does. Off by default so the live recording stays for comparison.
var scriptedRender bool

// fakeOllamaTwoModels serves /api/tags with two models, /api/show, and a
// streaming /api/chat that answers FIRST, SECOND, … and names the model that
// was asked, so a test can see which model answered.
func fakeOllamaTwoModels(t *testing.T) *httptest.Server { return fakeOllama(t, false) }

// fakeOllama is fakeOllamaTwoModels; with tools set the models advertise the
// tools capability and the first chat of a turn asks for read_file note.txt, the
// next (once a tool result is in the messages) answers TOOLDONE.
func fakeOllama(t *testing.T, tools bool) *httptest.Server {
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
			caps := []string{"completion"}
			if tools {
				caps = append(caps, "tools")
			}
			json.NewEncoder(w).Encode(map[string]any{
				"details":      map[string]any{"family": "llama", "parameter_size": "3B"},
				"capabilities": caps,
			})
		case "/api/chat":
			var req struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
				Tools []any `json:"tools"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if tools && len(req.Tools) > 0 {
				w.Header().Set("Content-Type", "application/x-ndjson")
				sawTool := false
				for _, m := range req.Messages {
					// With tool-result compaction on, the client sends the finished
					// round as an assistant "[called: …]" note followed by the result.
					if m.Role == "tool" || strings.HasPrefix(m.Content, "[called:") {
						sawTool = true
					}
				}
				if !sawTool {
					io.WriteString(w, `{"model":"`+req.Model+`","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"note.txt"}}}]},"done":true,"done_reason":"stop","prompt_eval_count":11,"eval_count":3}`+"\n")
				} else {
					io.WriteString(w, `{"model":"`+req.Model+`","message":{"role":"assistant","content":"TOOLDONE"},"done":false}`+"\n")
					io.WriteString(w, `{"model":"`+req.Model+`","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":20,"eval_count":1}`+"\n")
				}
				return
			}
			mu.Lock()
			word := words[turn%len(words)]
			turn++
			n := turn
			mu.Unlock()
			if scriptedFailOn > 0 && n == scriptedFailOn {
				http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			if scriptedReply != "" {
				word = scriptedReply
			}
			quoted, _ := json.Marshal(word)
			io.WriteString(w, `{"model":"`+req.Model+`","message":{"role":"assistant","content":`+string(quoted)+`},"done":false}`+"\n")
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
	return scriptedRunWith(t, debug, nil, false, lines...)
}

// scriptedRunWith is scriptedRun with a hook that sees the workspace root
// before the session starts, and the option of a model that calls tools.
func scriptedRunWith(t *testing.T, debug bool, setup func(root string), tools bool, lines ...string) string {
	t.Helper()
	return scriptedRunFull(t, debug, setup, tools, nil, lines...)
}

// scriptedRunFull is scriptedRunWith with a last look at the agent before Run.
func scriptedRunFull(t *testing.T, debug bool, setup func(root string), tools bool, tweak func(*Agent), lines ...string) string {
	t.Helper()
	// By default the live recording is kept, for the tests that compare it with the
	// rendering; scriptedRender turns the close-time rendering on.
	prev := renderAtClose
	renderAtClose = scriptedRender
	t.Cleanup(func() { renderAtClose = prev })
	a, ws := runFixture(t, scriptedTerminal)
	if setup != nil {
		setup(ws.Root)
	}
	a.Config.Ollama.URL = fakeOllama(t, tools).URL
	a.Config.Ollama.Model = "llama3.2:3b"
	a.Config.Session.AutoRecord = true
	a.Config.Security.SafeMode = false
	if debug {
		a.Config.Debug = true
	}
	if tweak != nil {
		tweak(a)
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
	err = a.Run(&out)
	t.Logf("session output:\n%s", out.String())
	if err != nil {
		t.Fatalf("Run: %v", err)
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
		"tool_call", "permission", "tool_result", "injection",
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

	// The shell output enters the prompt as an injection by the user that cites
	// the tool_result it came from (DR-0040 item 4).
	var resultID string
	for _, e := range events {
		if e.Kind == KindToolResult {
			resultID = e.ID
		}
		if e.Kind == KindInjection {
			var d struct {
				Source string `json:"source"`
				Origin string `json:"origin"`
				From   string `json:"from"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if d.Source != "shell" || d.Origin != "user" || d.From != resultID || resultID == "" {
				t.Errorf("shell injection = %s, want source shell, origin user, from %q", e.Data, resultID)
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

func shellEvents(t *testing.T, lines ...string) (calls, results []Event, dir string) {
	t.Helper()
	dir = scriptedRun(t, false, lines...)
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(logs) != 1 {
		t.Fatalf("want one session log, got %v", logs)
	}
	events, _, err := ReadSessionLog(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		switch e.Kind {
		case KindToolCall:
			calls = append(calls, e)
		case KindToolResult:
			results = append(results, e)
		}
	}
	return calls, results, dir
}

func TestRun_ShellResultsRecordTheirOutcome(t *testing.T) {
	calls, results, _ := shellEvents(t, "!false", "!nosuchprogram_xyz", "!echo fine", "/exit")
	if len(calls) != 3 || len(results) != 3 {
		t.Fatalf("%d calls and %d results, want 3 and 3 (every started command has a result)", len(calls), len(results))
	}
	type res struct {
		CallID string `json:"call_id"`
		Status string `json:"status"`
		Exit   *int   `json:"exit"`
	}
	var r [3]res
	for i := range results {
		_ = json.Unmarshal(results[i].Data, &r[i])
		if r[i].CallID != calls[i].ID {
			t.Errorf("result %d names call %q, want %q", i, r[i].CallID, calls[i].ID)
		}
	}
	if r[0].Status != "error" || r[0].Exit == nil || *r[0].Exit != 1 {
		t.Errorf("!false = %+v, want status error, exit 1", r[0])
	}
	if r[1].Status != "error" || r[1].Exit != nil {
		t.Errorf("a program that cannot start = %+v, want status error and no exit code", r[1])
	}
	if r[2].Status != "ok" || r[2].Exit == nil || *r[2].Exit != 0 {
		t.Errorf("!echo fine = %+v, want ok, exit 0", r[2])
	}
}

func TestRun_ShellCommandAndOutputAreScrubbed(t *testing.T) {
	_, _, dir := shellEvents(t, "!echo API_KEY=sk-live-777", "/exit")
	// The Fountain recorder is unchanged and keeps what was typed; the session
	// log and its blobs are the new, scrubbed record.
	var checked int
	var redacted bool
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, ".spmd") {
			return nil
		}
		checked++
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "sk-live-777") {
			t.Errorf("%s holds the secret", path)
		}
		if strings.Contains(string(raw), "[REDACTED:secret]") {
			redacted = true
		}
		return nil
	})
	if checked == 0 {
		t.Fatal("no session-log file was checked")
	}
	if !redacted {
		t.Error("the command was never logged: no [REDACTED:secret] marker in the session log (a pass by omission)")
	}
}

// The structured tool loop, end to end: the model asks for read_file, the tool
// runs, the model answers. Two model calls and one tool call for one user turn.
func TestRun_ModelToolCallsAreLogged(t *testing.T) {
	dir := scriptedRunWith(t, false, func(root string) {
		if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("the note says hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}, true, "read note.txt", "/exit")
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(logs) != 1 {
		t.Fatalf("want one session log, got %v", logs)
	}
	events, _, err := ReadSessionLog(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	want := "user_input model_request model_response tool_call tool_result model_request model_response command"
	if got := infoKinds(events[:len(events)-1]); got != want {
		t.Fatalf("kinds\n got: %s\nwant: %s", got, want)
	}
	var call, result, last Event
	for _, e := range events {
		switch e.Kind {
		case KindToolCall:
			call = e
		case KindToolResult:
			result = e
		case KindModelResponse:
			last = e
		}
	}
	var c struct {
		Tool   string `json:"tool"`
		Origin string `json:"origin"`
	}
	_ = json.Unmarshal(call.Data, &c)
	var r struct {
		Call   string `json:"call"`
		Status string `json:"status"`
		Output string `json:"output"`
	}
	_ = json.Unmarshal(result.Data, &r)
	if c.Tool != "read_file" || c.Origin != "model" || r.Call != call.ID || r.Status != "ok" || !strings.Contains(r.Output, "the note says hello") {
		t.Errorf("tool events = %s / %s", call.Data, result.Data)
	}
	if !strings.Contains(string(last.Data), "TOOLDONE") {
		t.Errorf("final response = %s", last.Data)
	}
}

// With safe mode on, a !cmd outside the allowlist is refused: the log shows the
// refusal, and no process was started, so there is no tool_call or tool_result.
func TestRun_ASafeModeDenialIsAPermissionEventAndNothingElse(t *testing.T) {
	dir := scriptedRunFull(t, false, nil, false, func(a *Agent) { a.Config.Security.SafeMode = true },
		"!nosuchprogram_xyz --flag", "/exit")
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	events, _, err := ReadSessionLog(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	if k := infoKinds(events); k != "permission command session_end" {
		t.Fatalf("kinds = %s, want permission command session_end", k)
	}
	var d permData
	_ = json.Unmarshal(events[1].Data, &d)
	if d.Action != "run" || d.Decision != "deny" || d.Scope != "rule" || d.OutcomeOf != "" || d.Target.Text != "nosuchprogram_xyz --flag" {
		t.Errorf("permission = %s, want run/deny/rule with no call to gate", events[1].Data)
	}
}

func TestRun_AShellCommandRunsUnderAPermissionDecision(t *testing.T) {
	dir := scriptedRunWith(t, false, nil, false, "!echo hi", "/exit")
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	events, _, err := ReadSessionLog(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	var call, perm Event
	for _, e := range events {
		switch e.Kind {
		case KindToolCall:
			call = e
		case KindPermission:
			perm = e
		}
	}
	var d permData
	_ = json.Unmarshal(perm.Data, &d)
	if d.Action != "run" || d.Decision != "auto" || d.Scope != "rule" || d.OutcomeOf != call.ID || d.Target.Text != "echo hi" {
		t.Errorf("permission = %s, want run/auto/rule gating %s", perm.Data, call.ID)
	}
}
