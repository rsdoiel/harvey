package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A1 of assay-profile-plan.md (DR-0051 item 1): targets are service/model
// names on two servers, Ollama and hailo-ollama.

// recordingServer answers /api/tags with tags and /api/chat with "say hi",
// and records the model of every chat request it receives.
type recordingServer struct {
	*httptest.Server
	mu    sync.Mutex
	Chats []string
}

func newRecordingServer(t *testing.T, tags ...string) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			var models []map[string]string
			for _, n := range tags {
				models = append(models, map[string]string{"name": n})
			}
			json.NewEncoder(w).Encode(map[string]any{"models": models})
		case "/api/chat":
			var req struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			rs.mu.Lock()
			rs.Chats = append(rs.Chats, req.Model)
			rs.mu.Unlock()
			w.Header().Set("Content-Type", "application/x-ndjson")
			json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "created_at": "2026-11-05T00:00:00Z",
				"message": map[string]string{"role": "assistant", "content": "say hi"}, "done": true, "done_reason": "stop"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordingServer) chats() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.Chats...)
}

// resultServices returns "service/model" for every row of results.json.
func resultServices(t *testing.T, outDir string) []string {
	t.Helper()
	var got []string
	for _, r := range readResults(t, outDir) {
		svc, _ := r["service"].(string)
		mod, _ := r["model"].(string)
		got = append(got, svc+"/"+mod)
	}
	return got
}

func a1Args(corpus, outDir, ollama, hailo string, extra ...string) []string {
	return append([]string{"--corpus", corpus, "--output", outDir, "--ollama", ollama, "--hailo", hailo}, extra...)
}

func TestMainRun_QualifiedModelsGoToTheirOwnServer(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	oll := newRecordingServer(t, "qwen2.5-coder:1.5b")
	hai := newRecordingServer(t, "llama3.2:3b")
	out := filepath.Join(dir, "out")

	code, _, errOut := runAssay(t, a1Args(corpus, out, oll.URL, hai.URL,
		"--models", "ollama/qwen2.5-coder:1.5b,hailo/llama3.2:3b")...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, errOut)
	}
	if got := oll.chats(); len(got) != 1 || got[0] != "qwen2.5-coder:1.5b" {
		t.Errorf("ollama server got chats %v, want [qwen2.5-coder:1.5b] (no service prefix on the wire)", got)
	}
	if got := hai.chats(); len(got) != 1 || got[0] != "llama3.2:3b" {
		t.Errorf("hailo server got chats %v, want [llama3.2:3b]", got)
	}
	want := []string{"ollama/qwen2.5-coder:1.5b", "hailo/llama3.2:3b"}
	if got := resultServices(t, out); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("results.json rows %v, want %v", got, want)
	}
}

func TestMainRun_NoModelsProfilesEveryModelOnBothServers(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	oll := newRecordingServer(t, "llama3.2:3b", "nomic-embed-text")
	hai := newRecordingServer(t, "llama3.2:3b", "qwen3:1.7b")
	out := filepath.Join(dir, "out")

	code, _, errOut := runAssay(t, a1Args(corpus, out, oll.URL, hai.URL)...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, errOut)
	}
	want := "ollama/llama3.2:3b,hailo/llama3.2:3b,hailo/qwen3:1.7b"
	if got := strings.Join(resultServices(t, out), ","); got != want {
		t.Errorf("rows %q, want %q (embedding models skipped; the same name on two servers is two targets)", got, want)
	}
	repB, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	rep := string(repB)
	for _, label := range []string{"ollama/llama3.2:3b", "hailo/llama3.2:3b"} {
		if !strings.Contains(rep, label) {
			t.Errorf("report.md does not name %q, so the two same-named models cannot be told apart", label)
		}
	}
}

func TestMainRun_UnreachableServerIsSkippedWithANote(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	hai := newRecordingServer(t, "qwen3:1.7b")
	out := filepath.Join(dir, "out")

	code, _, errOut := runAssay(t, a1Args(corpus, out, closedURL(t), hai.URL)...)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (hailo answered)\nstderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "ollama") || !strings.Contains(errOut, "skipp") {
		t.Errorf("stderr %q should say the ollama server was skipped", errOut)
	}
	if got := strings.Join(resultServices(t, out), ","); got != "hailo/qwen3:1.7b" {
		t.Errorf("rows %q, want only hailo/qwen3:1.7b", got)
	}
}

func TestMainRun_NoServerAnswersIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	code, _, errOut := runAssay(t, a1Args(corpus, filepath.Join(dir, "out"), closedURL(t), closedURL(t))...)
	if code != 69 {
		t.Errorf("exit %d, want 69\nstderr: %s", code, errOut)
	}
}

func TestMainRun_BareNameOnBothServersIsAUsageError(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	oll := newRecordingServer(t, "llama3.2:3b")
	hai := newRecordingServer(t, "llama3.2:3b")

	code, out, errOut := runAssay(t, a1Args(corpus, filepath.Join(dir, "out"), oll.URL, hai.URL,
		"--models", "llama3.2:3b")...)
	if code != 2 {
		t.Fatalf("exit %d, want 2\nstderr: %s", code, errOut)
	}
	if out != "" {
		t.Errorf("stdout %q, want nothing (nothing was attempted)", out)
	}
	for _, want := range []string{"ollama/llama3.2:3b", "hailo/llama3.2:3b"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q does not offer %q", errOut, want)
		}
	}
	if len(oll.chats())+len(hai.chats()) != 0 {
		t.Error("a chat was sent although the name was ambiguous")
	}
}

func TestMainRun_BareNameFindsTheServerThatHasIt(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	oll := newRecordingServer(t, "phi4")
	hai := newRecordingServer(t, "qwen3:1.7b")
	out := filepath.Join(dir, "out")

	code, _, errOut := runAssay(t, a1Args(corpus, out, oll.URL, hai.URL, "--models", "qwen3:1.7b")...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, errOut)
	}
	if got := hai.chats(); len(got) != 1 {
		t.Errorf("hailo got %v, want one chat", got)
	}
	if got := oll.chats(); len(got) != 0 {
		t.Errorf("ollama got %v, want none", got)
	}
}

func TestMainRun_BareNameKnownToNeitherStaysOnOllama(t *testing.T) {
	// The old behaviour: a name nobody lists is sent to Ollama, which answers
	// for it (assay does not second-guess a model it cannot see).
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	oll := newRecordingServer(t)
	hai := newRecordingServer(t)
	code, _, errOut := runAssay(t, a1Args(corpus, filepath.Join(dir, "out"), oll.URL, hai.URL, "--models", "m")...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, errOut)
	}
	if got := oll.chats(); len(got) != 1 || len(hai.chats()) != 0 {
		t.Errorf("ollama %v hailo %v, want the chat on ollama only", got, hai.chats())
	}
}

func TestMainRun_HailoQualifiedModelWithHailoDownFailsTheCall(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	oll := newRecordingServer(t)
	code, _, errOut := runAssay(t, a1Args(corpus, filepath.Join(dir, "out"), oll.URL, closedURL(t),
		"--models", "hailo/qwen3:1.7b")...)
	if code != 69 {
		t.Errorf("exit %d, want 69 (a named service that is down)\nstderr: %s", code, errOut)
	}
}
