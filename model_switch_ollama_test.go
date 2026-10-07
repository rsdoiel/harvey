package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// switchAgent is a mid-session agent: on llama3.2:3b, recording, with --debug on.
func switchAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	a := selectAgent(t, fakeOllamaChat(t).URL)
	a.Config.Ollama.Model = "llama3.2:3b"
	a.Client = newOllamaLLMClient(a.Config.Ollama.URL, "llama3.2:3b", 0)
	dir := t.TempDir()
	dl, err := OpenDebugLog(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatalf("OpenDebugLog: %v", err)
	}
	t.Cleanup(func() { dl.Close() })
	a.DebugLog = dl
	wireDebugLog(a.Client, dl)
	path := filepath.Join(dir, "session.spmd")
	rec, err := NewRecorder(path, "ollama (llama3.2:3b)", dir)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	t.Cleanup(func() { rec.Close() })
	a.Recorder = rec
	return a, path
}

// chatOnce sends one turn through the active client and records it, as the
// REPL does.
func chatOnce(t *testing.T, a *Agent) {
	t.Helper()
	var out bytes.Buffer
	if _, err := a.Client.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, &out); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if err := a.Recorder.RecordTurn("Hi", out.String()); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}
}

// assertSwitchedCleanly checks that the turn after the switch is logged and
// attributed to the new model.
func assertSwitchedCleanly(t *testing.T, a *Agent, transcript, wantSpeaker, wantNote string) {
	t.Helper()
	chatOnce(t, a)
	log, err := os.ReadFile(a.DebugLog.Path())
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	if !strings.Contains(string(log), `"event":"llm_request"`) {
		t.Errorf("debug log has no llm_request after the switch:\n%s", log)
	}
	a.Recorder.Close()
	rec, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
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

func TestModelUse_PickerMatchKeepsDebugLogAndRecordsSwitch(t *testing.T) {
	a, transcript := switchAgent(t)
	withLocalModels(t, []ModelSummary{{Name: "qwen2.5-coder:1.5b", Engine: "ollama"}})
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "qwen2.5-coder:1.5b"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	assertSwitchedCleanly(t, a, transcript, "QWEN2.5-CODER", "model switch: qwen2.5-coder:1.5b (ollama)")
}

func TestModelUse_OllamaAliasKeepsDebugLog(t *testing.T) {
	a, transcript := switchAgent(t)
	a.Config.ModelAliases["coder"] = ModelAlias{Model: "qwen2.5-coder:1.5b", Engine: "ollama"}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "coder"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	assertSwitchedCleanly(t, a, transcript, "QWEN2.5-CODER", "model switch: qwen2.5-coder:1.5b (ollama)")
}

func TestModelUse_LegacyAliasFallingBackToOllamaKeepsDebugLog(t *testing.T) {
	a, transcript := switchAgent(t)
	a.Config.ModelAliases["coder"] = ModelAlias{Model: "qwen2.5-coder:1.5b"}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "coder"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	assertSwitchedCleanly(t, a, transcript, "QWEN2.5-CODER", "model switch: qwen2.5-coder:1.5b (ollama)")
}

// A dispatched step (@coder, or a plan step's [model: coder]) switches to the
// step's model and Restore switches back. The restored Ollama client was
// rebuilt without the debug log, and the transcript kept the step's model.
func TestDispatchRestore_OllamaKeepsDebugLogAndRecordsSwitchBack(t *testing.T) {
	a, transcript := switchAgent(t)
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
	log, err := os.ReadFile(a.DebugLog.Path())
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	if !strings.Contains(string(log), `"event":"llm_request"`) {
		t.Errorf("debug log has no llm_request after Restore:\n%s", log)
	}
	a.Recorder.Close()
	rec, _ := os.ReadFile(transcript)
	if !strings.Contains(string(rec), "model switch: llama3.2:3b (ollama)") {
		t.Errorf("transcript does not record the switch back:\n%s", rec)
	}
	if strings.Contains(string(rec), "\nQWEN2.5-CODER\n") {
		t.Errorf("transcript attributes the restored turn to the step's model:\n%s", rec)
	}
}
