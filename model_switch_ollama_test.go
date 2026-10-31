package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Switching to an Ollama model mid-session built a new client without the
// debug log and, on the picker path, without a recorder note. Seen on
// harvey.local 2026-10-06 (harvey-session-20261006-204135.spmd): a session
// started on llama3.2:3b and switched to qwen2.5-coder:1.5b recorded the
// reply as LLAMA3.2, and its --debug log held only session_start.

// fakeOllamaChat serves /api/show and a one-chunk streaming /api/chat.
func fakeOllamaChat(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			json.NewEncoder(w).Encode(map[string]any{
				"details":      map[string]any{"family": "qwen2", "parameter_size": "1.5B"},
				"capabilities": []string{"completion"},
			})
		case "/api/chat":
			w.Header().Set("Content-Type", "application/x-ndjson")
			io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"hi"},"done":false}`+"\n")
			io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":1}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// switchAgent is a mid-session agent: on llama3.2:3b, recording, with --debug on
// and a session log open.
func switchAgent(t *testing.T) *Agent {
	t.Helper()
	a := selectAgent(t, fakeOllamaChat(t).URL)
	a.Config.Ollama.Model = "llama3.2:3b"
	a.Config.Debug = true
	a.Client = a.wireSessionLog(newOllamaLLMClient(a.Config.Ollama.URL, "llama3.2:3b", 0))
	dir := t.TempDir()
	l, err := OpenSessionLog(SessionLogOptions{
		Dir: dir, Session: "switch", Debug: true,
		Start: SessionStart{Backend: "ollama", Model: "llama3.2:3b", Workspace: a.Workspace.Root},
	})
	if err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	t.Cleanup(func() { l.Close("test") })
	a.SessionLog = l
	a.logModel = ModelRef{Backend: "ollama", Model: "llama3.2:3b"}
	return a
}

// switchTranscript closes the agent's session log and returns its Fountain
// rendering, the transcript a reader of the session would see.
func switchTranscript(t *testing.T, a *Agent) string {
	t.Helper()
	if err := a.SessionLog.Close("test"); err != nil {
		t.Fatalf("close the session log: %v", err)
	}
	events, _, err := ReadSessionLog(a.SessionLog.Path())
	if err != nil {
		t.Fatalf("read the session log: %v", err)
	}
	return string(RenderFountain(events, RenderOptions{Zone: time.UTC, Blobs: BlobDir(a.SessionLog.Path())}))
}

// chatOnce sends one turn through the active client and logs it, as the REPL
// does: the author's input, then the call.
func chatOnce(t *testing.T, a *Agent) {
	t.Helper()
	a.logUserInput("Hi")
	var out bytes.Buffer
	ctx := withCallPurpose(context.Background(), purposeTurn, nil)
	if _, err := a.Client.Chat(ctx, []Message{{Role: "user", Content: "Hi"}}, &out); err != nil {
		t.Fatalf("chat: %v", err)
	}
}

// assertSwitchedCleanly checks that the turn after the switch is logged and
// attributed to the new model, in the session log and in the transcript.
func assertSwitchedCleanly(t *testing.T, a *Agent, wantSpeaker, wantNote, wantModel string) {
	t.Helper()
	chatOnce(t, a)
	events, _, err := ReadSessionLog(a.SessionLog.Path())
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	var reqs, resps, switches []Event
	for _, e := range events {
		switch e.Kind {
		case KindModelRequest:
			if e.Level == LevelInfo {
				reqs = append(reqs, e)
			}
		case KindModelResponse:
			resps = append(resps, e)
		case KindModelSwitch:
			switches = append(switches, e)
		}
	}
	if len(reqs) == 0 || len(resps) != len(reqs) {
		t.Fatalf("the session log has %d requests and %d responses after the switch", len(reqs), len(resps))
	}
	var req struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(reqs[len(reqs)-1].Data, &req)
	if req.Model != wantModel {
		t.Errorf("the turn after the switch was logged for model %q, want %q", req.Model, wantModel)
	}
	if !strings.Contains(string(resps[len(resps)-1].Data), `"status":"ok"`) {
		t.Errorf("the turn after the switch has no ok response: %s", resps[len(resps)-1].Data)
	}
	var sw struct {
		To ModelRef `json:"to"`
	}
	if len(switches) == 0 {
		t.Fatalf("the session log has no model_switch")
	}
	_ = json.Unmarshal(switches[len(switches)-1].Data, &sw)
	if sw.To.Model != wantModel {
		t.Errorf("model_switch names %q, want %q", sw.To.Model, wantModel)
	}
	rec := []byte(switchTranscript(t, a))
	if !strings.Contains(string(rec), "\n"+wantSpeaker+"\n") {
		t.Errorf("transcript does not attribute the reply to %s:\n%s", wantSpeaker, rec)
	}
	if strings.Contains(string(rec), "\nLLAMA3.2\n") {
		t.Errorf("transcript still attributes a reply to the old model:\n%s", rec)
	}
	if wantNote != "" && !strings.Contains(string(rec), wantNote) {
		t.Errorf("transcript has no %q note:\n%s", wantNote, rec)
	}
}

func TestModelUse_PickerMatchKeepsSessionLogAndRecordsSwitch(t *testing.T) {
	a := switchAgent(t)
	withLocalModels(t, []ModelSummary{{Name: "qwen2.5-coder:1.5b", Engine: "ollama"}})
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "qwen2.5-coder:1.5b"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	assertSwitchedCleanly(t, a, "QWEN2.5-CODER", "model switch: qwen2.5-coder:1.5b (ollama)", "qwen2.5-coder:1.5b")
}

func TestModelUse_OllamaAliasKeepsSessionLog(t *testing.T) {
	a := switchAgent(t)
	a.Config.ModelAliases["coder"] = ModelAlias{Model: "qwen2.5-coder:1.5b", Engine: "ollama"}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "coder"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	assertSwitchedCleanly(t, a, "QWEN2.5-CODER", "model switch: qwen2.5-coder:1.5b (ollama)", "qwen2.5-coder:1.5b")
}

func TestModelUse_LegacyAliasFallingBackToOllamaKeepsSessionLog(t *testing.T) {
	a := switchAgent(t)
	a.Config.ModelAliases["coder"] = ModelAlias{Model: "qwen2.5-coder:1.5b"}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "coder"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	assertSwitchedCleanly(t, a, "QWEN2.5-CODER", "model switch: qwen2.5-coder:1.5b (ollama)", "qwen2.5-coder:1.5b")
}

// A dispatched step (@coder, or a plan step's [model: coder]) switches to the
// step's model and Restore switches back. The restored Ollama client was
// rebuilt without the debug log, and the transcript kept the step's model.
func TestDispatchRestore_OllamaKeepsSessionLogAndRecordsSwitchBack(t *testing.T) {
	a := switchAgent(t)
	a.Config.ModelAliases["coder"] = ModelAlias{Model: "qwen2.5-coder:1.5b", Engine: "ollama"}
	var out strings.Builder
	target, ok, err := resolveDispatchTarget(a, "coder", &out)
	if err != nil || !ok {
		t.Fatalf("resolveDispatchTarget: ok=%v err=%v", ok, err)
	}
	target.Restore()
	if a.Config.Ollama.Model != "llama3.2:3b" {
		t.Fatalf("Restore left the model at %q", a.Config.Ollama.Model)
	}
	chatOnce(t, a)
	events, _, err := ReadSessionLog(a.SessionLog.Path())
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	var lastReq Event
	var switchBack bool
	for _, e := range events {
		if e.Kind == KindModelRequest && e.Level == LevelInfo {
			lastReq = e
		}
		if e.Kind == KindModelSwitch && strings.Contains(string(e.Data), `"to":{"backend":"ollama","model":"llama3.2:3b"}`) {
			switchBack = true
		}
	}
	if !strings.Contains(string(lastReq.Data), `"model":"llama3.2:3b"`) {
		t.Errorf("the turn after Restore was not logged for the restored model: %s", lastReq.Data)
	}
	if !switchBack {
		t.Error("the session log has no model_switch back to the restored model")
	}
	rec := []byte(switchTranscript(t, a))
	if !strings.Contains(string(rec), "model switch: llama3.2:3b (ollama)") {
		t.Errorf("transcript does not record the switch back:\n%s", rec)
	}
	if strings.Contains(string(rec), "\nQWEN2.5-CODER\n") {
		t.Errorf("transcript attributes the restored turn to the step's model:\n%s", rec)
	}
}
