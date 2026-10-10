package harvey

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Switching to a llama.cpp model mid-session installed a new client without
// the debug log and wrote no [[model switch]] note, the same fault fixed for
// Ollama on 2026-10-06 (model_switch_ollama_test.go): the --debug log went
// quiet after the switch and the transcript kept naming the previous model.
// startLlamaCppModelPath was the one path left (TODO.md, "Not covered").

// fakeLlamaServer is a running llama-server: /health for Detect, so Start
// adopts it, and a one-chunk streaming /v1/chat/completions.
func fakeLlamaServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`+"\n\n")
			io.WriteString(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
			io.WriteString(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// llamaCppSwitchAgent is switchAgent (on llama3.2:3b, recording, --debug on)
// pointed at a fake llama-server, with one .gguf in its models directory.
func llamaCppSwitchAgent(t *testing.T) (*Agent, string, string) {
	t.Helper()
	a, transcript := switchAgent(t)
	a.Config.LlamaCpp.URL = fakeLlamaServer(t).URL
	dir := t.TempDir()
	path := filepath.Join(dir, "tinyllama-Q4_K_M.gguf")
	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Config.LlamaCpp.ModelsDir = dir
	return a, transcript, path
}

func TestModelUse_LlamaCppAliasKeepsDebugLogAndRecordsSwitch(t *testing.T) {
	a, transcript, _ := llamaCppSwitchAgent(t)
	a.Config.ModelAliases["tiny"] = ModelAlias{Model: "tinyllama-Q4_K_M", Engine: "llamacpp"}
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "tiny"}, &out); err != nil {
		t.Fatalf("/model use: %v\n%s", err, out.String())
	}
	assertSwitchedCleanly(t, a, transcript, "TINYLLAMA-Q4_K_M", "model switch: tinyllama-Q4_K_M (llamacpp)")
}

func TestModelUse_LlamaCppPickerMatchKeepsDebugLogAndRecordsSwitch(t *testing.T) {
	a, transcript, path := llamaCppSwitchAgent(t)
	withLocalModels(t, []ModelSummary{{Name: "tinyllama-Q4_K_M", Engine: "llamacpp", Path: path}})
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "tinyllama-Q4_K_M"}, &out); err != nil {
		t.Fatalf("/model use: %v\n%s", err, out.String())
	}
	assertSwitchedCleanly(t, a, transcript, "TINYLLAMA-Q4_K_M", "model switch: tinyllama-Q4_K_M (llamacpp)")
}

// The startup path has no debug log or recorder open yet; the same helper
// must cope with both being nil.
func TestStartLlamaCppModelPath_NoDebugLogOrRecorderIsFine(t *testing.T) {
	a, _, path := llamaCppSwitchAgent(t)
	a.DebugLog, a.Recorder = nil, nil
	if err := startLlamaCppModelPath(a, path, io.Discard); err != nil {
		t.Fatalf("startLlamaCppModelPath: %v", err)
	}
	if a.Client == nil || a.Backend == nil || a.Backend.Name() != "llamacpp" {
		t.Errorf("client %v backend %v, want a llamacpp client and backend", a.Client, a.Backend)
	}
}
