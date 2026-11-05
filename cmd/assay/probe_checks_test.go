package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	harvey "github.com/rsdoiel/harvey"
)

// A4 of assay-profile-plan.md (DR-0051 item 3): the tool_call, uses_tail and
// exact checks. A failed check is a result, never an error.

func call(name, args string) harvey.ToolCall {
	return harvey.ToolCall{Type: "function", Function: harvey.FunctionCall{Name: name, Arguments: args}}
}

func TestToolCallCheck(t *testing.T) {
	want := &ToolCallCheck{Name: "read_file", Arg: "path", Contains: "main.go"}
	for _, tc := range []struct {
		name  string
		check Checks
		calls []harvey.ToolCall
		reply string
		pass  bool
	}{
		{"right tool, right argument", Checks{ToolCall: want}, []harvey.ToolCall{call("read_file", `{"path":"cmd/main.go"}`)}, "", true},
		{"name with stray white space", Checks{ToolCall: want}, []harvey.ToolCall{call(" read_file", `{" path":"main.go"}`)}, "", true},
		{"wrong tool", Checks{ToolCall: want}, []harvey.ToolCall{call("list_files", `{"path":"."}`)}, "", false},
		{"right tool, invented path", Checks{ToolCall: want}, []harvey.ToolCall{call("read_file", `{"path":"/etc/passwd"}`)}, "", false},
		{"right tool, argument missing", Checks{ToolCall: want}, []harvey.ToolCall{call("read_file", `{}`)}, "", false},
		{"no call at all", Checks{ToolCall: want}, nil, "I would read main.go.", false},
		{"first call wrong, second right", Checks{ToolCall: want}, []harvey.ToolCall{call("list_files", `{}`), call("read_file", `{"path":"main.go"}`)}, "", true},
		{"prose JSON block is accepted", Checks{ToolCall: want}, nil, "```json\n{\"name\":\"read_file\",\"arguments\":{\"path\":\"main.go\"}}\n```", true},
		{"a tool that was not offered", Checks{ToolCall: want}, []harvey.ToolCall{call("run_command", `{"command":"cat main.go"}`)}, "", false},
		{"no_tool and no call", Checks{NoTool: true}, nil, "It returns an int.", true},
		{"no_tool but a call was invented", Checks{NoTool: true}, []harvey.ToolCall{call("read_file", `{"path":"x"}`)}, "", false},
	} {
		got := runToolCheck(tc.check, tc.calls, tc.reply)
		if len(got) != 1 {
			t.Errorf("%s: %d results, want 1", tc.name, len(got))
			continue
		}
		if got[0].Passed != tc.pass {
			t.Errorf("%s: passed=%v, want %v (%s)", tc.name, got[0].Passed, tc.pass, got[0].Detail)
		}
	}
}

func TestUsesTailCheck(t *testing.T) {
	if r := runUsesTailCheck("BLUE-HERON-42", "The word is BLUE-HERON-42."); !r.Passed {
		t.Errorf("marker in the reply should pass: %+v", r)
	}
	r := runUsesTailCheck("BLUE-HERON-42", "I do not see a code word.")
	if r.Passed || !strings.Contains(r.Detail, "did not reflect") {
		t.Errorf("a reply without the marker should fail with 'did not reflect', got %+v", r)
	}
}

func TestExactCheck(t *testing.T) {
	for _, tc := range []struct {
		re, reply string
		pass      bool
	}{
		{`[a-z]+`, "  hello\n", true},
		{`[a-z]+`, "hello there", false},
		{`[a-z]+`, "Sure! hello", false},
		{`\{"ok": ?(true|false)\}`, `{"ok": true}`, true},
		{`\{"ok": ?(true|false)\}`, "```json\n{\"ok\": true}\n```", false},
		{`yes|no`, "no", true},
	} {
		if got := runExactCheck(tc.re, tc.reply); got.Passed != tc.pass {
			t.Errorf("exact %q on %q: passed=%v, want %v", tc.re, tc.reply, got.Passed, tc.pass)
		}
	}
}

func TestBuildProbePrompt(t *testing.T) {
	p := Prompt{ID: "tail", Tokens: 1200, PromptText: "Reply with exactly this code word and nothing else: BLUE-HERON-42",
		Checks: Checks{UsesTail: "BLUE-HERON-42"}}
	got := buildProbePrompt(p)
	if !strings.HasSuffix(got, p.PromptText) {
		t.Error("the instruction must be the end of the prompt, that is the point of the probe")
	}
	if strings.Count(got, "BLUE-HERON-42") != 1 {
		t.Error("the marker must appear once, in the instruction, never in the filler")
	}
	// tokens: counts Qwen-family tokens, about 3.3 characters each for this
	// filler (measured: lp-1200 at 4 chars/token was 1,468 real tokens).
	if n := len(got); n < 3560 || n > 4360 {
		t.Errorf("prompt is %d characters, want about 3960 (1200 tokens at 3.3 characters each, within 10%%)", n)
	}
	if buildProbePrompt(p) != got {
		t.Error("the filler must be deterministic so runs compare")
	}
	// No size hint, or a check that is not uses_tail: the prompt is sent as written.
	p.Tokens = 0
	if buildProbePrompt(p) != p.PromptText {
		t.Error("with no tokens: hint the prompt is sent as written")
	}
	plain := Prompt{Tokens: 1200, PromptText: "hi"}
	if buildProbePrompt(plain) != "hi" {
		t.Error("only a uses_tail prompt is padded")
	}
}

func TestLoadCorpus_ProbeChecksAreValidated(t *testing.T) {
	base := func(checks string) string {
		return corpusWith("  - id: probe\n    category: c\n    description: d\n    language: text\n    prompt: say BLUE-HERON-42\n    checks:\n" + checks + "    human: []\n    notes: \"\"\n")
	}
	for _, tc := range []struct{ name, checks, mention string }{
		{"tool_call without a name", "      tool_call: {arg: path}\n", "probe"},
		{"tool_call and no_tool together", "      tool_call: {name: read_file}\n      no_tool: true\n", "probe"},
		{"uses_tail marker not in the prompt", "      uses_tail: NOT-THERE\n", "probe"},
		{"exact that is not a regexp", "      exact: \"[\"\n", "probe"},
	} {
		_, err := loadCorpus(writeFile(t, t.TempDir(), "c.yaml", base(tc.checks)))
		if err == nil || harvey.ExitCodeFor(err).Code != 65 || !strings.Contains(err.Error(), tc.mention) {
			t.Errorf("%s: %v, want a data error naming the prompt", tc.name, err)
		}
	}
	good := base("      uses_tail: BLUE-HERON-42\n      exact: \"[A-Z0-9-]+\"\n")
	if _, err := loadCorpus(writeFile(t, t.TempDir(), "g.yaml", good)); err != nil {
		t.Errorf("valid probe checks rejected: %v", err)
	}
}

// toolServer answers /api/chat with a structured read_file call, or with
// HTTP 400 "does not support tools" for the model named in noTools. It records
// the tool names of every request.
func toolServer(t *testing.T, noTools string) (*httptest.Server, *[][]string) {
	t.Helper()
	var seen [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "m"}}})
			return
		}
		var req struct {
			Model string `json:"model"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var names []string
		for _, tl := range req.Tools {
			names = append(names, tl.Function.Name)
		}
		seen = append(seen, names)
		if req.Model == noTools && len(names) > 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"registry.ollama.ai/library/m does not support tools"}`))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "created_at": "2026-11-05T00:00:00Z",
			"message": map[string]any{"role": "assistant", "content": "",
				"tool_calls": []map[string]any{{"function": map[string]any{"name": "read_file", "arguments": map[string]any{"path": "main.go"}}}}},
			"done": true, "done_reason": "stop"})
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

const toolCorpus = `  - id: read-main
    category: tool-call
    description: d
    language: text
    prompt: The workspace has main.go. Show me what it contains.
    checks:
      tool_call: {name: read_file, arg: path, contains: main.go}
    human: []
    notes: ""
`

func TestMainRun_ToolCallCheckOffersHarveysSubsetAndReadsTheCall(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", corpusWith(toolCorpus))
	srv, seen := toolServer(t, "")
	out := filepath.Join(dir, "out")
	code, _, errOut := runAssay(t, "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if len(*seen) != 1 || strings.Join((*seen)[0], ",") != "list_files,read_file,search_files" {
		t.Errorf("tools offered %v, want Harvey's read-only subset", *seen)
	}
	rows := readResults(t, out)
	if rows[0]["auto_pass"] != true {
		t.Errorf("the structured call should pass the check: %v", rows[0]["checks"])
	}
}

func TestMainRun_AModelWithoutToolSupportFailsTheCheckNotTheRun(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", corpusWith(toolCorpus))
	srv, _ := toolServer(t, "m")
	out := filepath.Join(dir, "out")
	code, _, errOut := runAssay(t, "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	if code != 0 {
		t.Fatalf("exit %d, want 0: a model that cannot take tools is a result\n%s", code, errOut)
	}
	rows := readResults(t, out)
	if rows[0]["auto_pass"] == true {
		t.Error("the check should fail")
	}
	b, _ := json.Marshal(rows[0]["checks"])
	if !strings.Contains(string(b), "does not support tools") {
		t.Errorf("the check detail should say why: %s", b)
	}
}

// A long-prompt probe is written to go past a model's window, and hailo then
// answers HTTP 500 or drops the stream (qwen3, DR-0050). That is the result
// the probe exists to find, not a service failure.
const tailCorpus = `  - id: tail-big
    category: long-prompt
    description: d
    language: text
    tokens: 3000
    prompt: "Reply with exactly this code word: BLUE-HERON-42"
    checks:
      uses_tail: BLUE-HERON-42
    human: []
    notes: ""
`

func TestMainRun_ALongPromptTheServerCannotTakeIsAResult(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"HTTP 500": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
		"stream cut off": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"BLUE"},"done":false}` + "\n"))
		},
	} {
		srv := httptest.NewServer(handler)
		dir := t.TempDir()
		corpus := writeFile(t, dir, "c.yaml", corpusWith(tailCorpus))
		out := filepath.Join(dir, "out")
		code, _, errOut := runAssay(t, "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
		srv.Close()
		if code != 0 {
			t.Errorf("%s: exit %d, want 0 (a window result)\n%s", name, code, errOut)
			continue
		}
		b, _ := json.Marshal(readResults(t, out)[0]["checks"])
		if !strings.Contains(string(b), `"uses_tail"`) || !strings.Contains(string(b), "failed on a prompt of about 30") {
			t.Errorf("%s: checks %s should be a failed uses_tail naming the size", name, b)
		}
	}
}

func TestMainRun_ALongPromptToADeadServerIsStillUnavailable(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", corpusWith(tailCorpus))
	code, _, _ := runAssay(t, "--corpus", corpus, "--output", filepath.Join(dir, "out"), "--ollama", closedURL(t), "--models", "ollama/m")
	if code != 69 {
		t.Errorf("exit %d, want 69: a refused connection is not a window result", code)
	}
}

// Some models (qwen2.5-coder:3b and llama3.2:3b on Ollama) write the call as
// bare JSON in the reply instead of a structured call. Harvey only runs
// fenced blocks, so it is not a pass, but it is not the same as "no call".
func TestToolCheck_ACallWrittenAsPlainText(t *testing.T) {
	text := `{"name": "read_file", "arguments": {"path": "config.yaml"}}`
	want := Checks{ToolCall: &ToolCallCheck{Name: "read_file", Arg: "path", Contains: "config.yaml"}}
	got := runToolCheck(want, nil, text)
	if got[0].Passed || !strings.Contains(got[0].Detail, "plain text") || !strings.Contains(got[0].Detail, "read_file") {
		t.Errorf("tool_call on a plain-text call: %+v, want a failure that says plain text and names the tool", got[0])
	}
	if got := runToolCheck(Checks{NoTool: true}, nil, text); got[0].Passed {
		t.Errorf("no_tool passed although the reply is a tool call written as text: %+v", got[0])
	}
	// llama3.2 names the arguments "parameters"; an unknown tool is still a call attempt.
	if got := runToolCheck(Checks{NoTool: true}, nil, `{"name":"math","parameters":{"a":12}}`); got[0].Passed {
		t.Errorf("no_tool passed for a made-up tool written as text: %+v", got[0])
	}
	// JSON that is not a call is just an answer.
	if got := runToolCheck(Checks{NoTool: true}, nil, `{"ok": true}`); !got[0].Passed {
		t.Errorf("no_tool failed for JSON that is not a call: %+v", got[0])
	}
}
