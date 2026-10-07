package harvey

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Step 3 of the Hailo brief (DR-0030): a static capability table keyed by
// engine states what hailo-ollama cannot do, and requests that would only
// rediscover it are not made.

// recordingServer is an Ollama-shaped server that records the path of every
// request and the body of each /api/chat, and answers like hailo-ollama.
type recordingServer struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
	chats []map[string]any
}

func newRecordingServer(t *testing.T, hailo bool) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.paths = append(rs.paths, r.Method+" "+r.URL.Path)
		rs.mu.Unlock()
		switch r.URL.Path {
		case "/api/tags":
			io.WriteString(w, `{"models":[{"name":"llama3.2:3b","size":1}]}`)
		case "/api/show":
			body := `{"details":{"family":"qwen2","parameter_size":"1.5B"},"capabilities":["completion"]}`
			if hailo {
				body = strings.ReplaceAll(hailoShow, `"family":"qwen2.5"`, `"family":"llama3.2"`)
			}
			io.WriteString(w, body)
		case "/hailo/v1/list":
			if hailo {
				io.WriteString(w, hailoListBody)
				return
			}
			http.NotFound(w, r)
		case "/api/chat":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			rs.mu.Lock()
			rs.chats = append(rs.chats, m)
			rs.mu.Unlock()
			if _, has := m["tools"]; has && hailo {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"hi"},"done":false}`+"\n")
			io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":1}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordingServer) saw(path string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, p := range rs.paths {
		if strings.HasSuffix(p, " "+path) {
			return true
		}
	}
	return false
}

func TestEngineCaps_Table(t *testing.T) {
	c, ok := capsForEngine("hailo")
	if !ok {
		t.Fatal("no capability row for hailo")
	}
	if c.Tools != CapNo || c.Embed != CapNo || c.Tokenize != CapNo || c.ProcessList != CapNo {
		t.Errorf("hailo caps = %+v, want tools, embed, tokenize and ps all CapNo", c)
	}
	if _, ok := capsForEngine("ollama"); ok {
		t.Error("ollama must not have a row: its servers are asked")
	}
	if _, ok := capsForEngine(""); ok {
		t.Error("an unknown engine must not have a row")
	}
}

// ─── tools ───────────────────────────────────────────────────────────────────

// hailoToolAgent is on the hailo engine with a tool registry and a cache row
// that is deliberately wrong (it says tools work, mode auto).
func hailoToolAgent(t *testing.T, rs *recordingServer, row string) *Agent {
	t.Helper()
	a := toolAgent(t, registryWithEcho())
	a.Config.Ollama.URL = "http://127.0.0.1:1"
	a.Config.Hailo = HailoConfig{URL: rs.URL, URLSet: true}
	a.hailoDevicePath = fakeDevice(t, true)
	mc, err := OpenModelCache(a.Workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mc.Close() })
	a.ModelCache = mc
	if err := a.setHailoModel("llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	switch row {
	case "wrong":
		_ = mc.Set(&ModelCapability{Name: "hailo/llama3.2:3b", SupportsTools: CapYes, ToolMode: ToolModeAuto, ProbeLevel: "fast", ProbedAt: time.Now()})
	case "none":
		_ = mc.Delete("hailo/llama3.2:3b")
	}
	// "probed": the row setHailoModel wrote (SupportsTools CapNo, mode auto)
	return a
}

func TestRunChatTurn_HailoNeverGetsATools(t *testing.T) {
	for name, row := range map[string]string{"no cache row": "none", "a wrong cache row": "wrong", "the probed cache row": "probed"} {
		t.Run(name, func(t *testing.T) {
			rs := newRecordingServer(t, true)
			a := hailoToolAgent(t, rs, row)
			var out strings.Builder
			if _, _, err := a.runChatTurn(context.Background(), "hello", &out, nil, false, ""); err != nil {
				t.Fatalf("turn failed: %v\n%s", err, out.String())
			}
			if len(rs.chats) == 0 {
				t.Fatal("no /api/chat request was made")
			}
			for _, c := range rs.chats {
				if _, has := c["tools"]; has {
					t.Errorf("a tools key was sent to hailo-ollama: %v", c["tools"])
				}
			}
		})
	}
}

// The control: the same turn on Ollama does send the tools, so the test above
// would fail if the table did nothing.
func TestRunChatTurn_OllamaStillGetsTools(t *testing.T) {
	rs := newRecordingServer(t, false)
	a := toolAgent(t, registryWithEcho())
	a.Config.Ollama.URL = rs.URL
	a.setOllamaModel("llama3.2:3b")
	if _, _, err := a.runChatTurn(context.Background(), "hello", &strings.Builder{}, nil, false, ""); err != nil {
		t.Fatal(err)
	}
	sent := false
	for _, c := range rs.chats {
		if _, has := c["tools"]; has {
			sent = true
		}
	}
	if !sent {
		t.Error("Ollama no longer receives tools; the hailo test proves nothing")
	}
}

func TestToolsReliable_FalseOnHailoWhateverTheCacheSays(t *testing.T) {
	rs := newRecordingServer(t, true)
	a := hailoToolAgent(t, rs, "wrong")
	if a.toolsReliable() {
		t.Error("toolsReliable = true on hailo with a row saying CapYes")
	}
	// a user-set structured mode cannot make hailo take tools either
	mc := a.ModelCache
	_ = mc.Set(&ModelCapability{Name: "hailo/llama3.2:3b", SupportsTools: CapYes, ToolMode: ToolModeStructured, ProbeLevel: "fast", ProbedAt: time.Now()})
	if a.toolsReliable() {
		t.Error("toolsReliable = true on hailo with mode structured")
	}
}

// ─── probes ──────────────────────────────────────────────────────────────────

func TestFastProbeModel_HailoAppliesTheTable(t *testing.T) {
	rs := newRecordingServer(t, true)
	// a name that looks like an embedding model must still be CapNo on hailo
	cap, err := FastProbeModel(context.Background(), rs.URL, "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	if cap.SupportsTools != CapNo || cap.SupportsEmbed != CapNo {
		t.Errorf("tools %v embed %v, want CapNo for both", cap.SupportsTools, cap.SupportsEmbed)
	}
}

func TestThoroughProbeModel_HailoMakesNoEmbedRequest(t *testing.T) {
	rs := newRecordingServer(t, true)
	cap, err := ThoroughProbeModel(context.Background(), rs.URL, "llama3.2:3b")
	if err != nil {
		t.Fatal(err)
	}
	if rs.saw("/api/embed") {
		t.Error("POST /api/embed was sent to hailo-ollama, which has no such route")
	}
	if cap.SupportsEmbed != CapNo {
		t.Errorf("SupportsEmbed = %v, want CapNo", cap.SupportsEmbed)
	}
}

// ─── tokenize ────────────────────────────────────────────────────────────────

func TestCountTokensFor_HailoDoesNotAsk(t *testing.T) {
	rs := newRecordingServer(t, true)
	n, exact := CountTokensFor(context.Background(), "hailo", rs.URL, "llama3.2:3b", strings.Repeat("x", 400))
	if exact || n != 100 {
		t.Errorf("= %d, %v; want the chars/4 estimate 100, not exact", n, exact)
	}
	if rs.saw("/api/tokenize") {
		t.Error("POST /api/tokenize was sent to hailo-ollama")
	}
}

func TestContextUsage_HailoDoesNotAskToTokenize(t *testing.T) {
	rs := newRecordingServer(t, true)
	a := hailoToolAgent(t, rs, "none")
	a.History = []Message{{Role: "user", Content: strings.Repeat("x", 400)}}
	n, _, exact := a.contextUsage()
	if exact || n != 100 {
		t.Errorf("contextUsage = %d, %v; want the estimate 100", n, exact)
	}
	if rs.saw("/api/tokenize") {
		t.Error("contextUsage sent /api/tokenize to hailo-ollama")
	}
}

// Ollama keeps asking.
func TestCountTokensFor_OllamaStillAsks(t *testing.T) {
	rs := newRecordingServer(t, false)
	CountTokensFor(context.Background(), "ollama", rs.URL, "m", "hello")
	if !rs.saw("/api/tokenize") {
		t.Error("Ollama is no longer asked to tokenize")
	}
}

// ─── ps ──────────────────────────────────────────────────────────────────────

func TestListFamilyModels_HailoIsNotAskedForPS(t *testing.T) {
	rs := newRecordingServer(t, true)
	a := familyAgent(t, "http://127.0.0.1:1", rs.URL, true, true)
	models := a.listFamilyModels()
	if len(models) != 1 || models[0].Engine != "hailo" {
		t.Fatalf("models = %+v", models)
	}
	if rs.saw("/api/ps") {
		t.Error("GET /api/ps was sent to hailo-ollama, which has no such route")
	}
}

// A server Harvey was not told is Hailo (no card, no hailo.url, ollama.url
// pointed at it) is recognised by the model's hef format; from then on it is
// held to the hailo capability row.
func TestSetOllamaModel_UnlabelledHailoServerIsHeldToTheHailoRow(t *testing.T) {
	rs := newRecordingServer(t, true)
	a := toolAgent(t, registryWithEcho())
	a.Config.Ollama.URL = rs.URL
	a.Config.Hailo = HailoConfig{URL: "http://127.0.0.1:1"} // not set, and no card
	a.hailoDevicePath = fakeDevice(t, false)
	mc, _ := OpenModelCache(a.Workspace, "")
	defer mc.Close()
	a.ModelCache = mc

	a.setOllamaModel("llama3.2:3b")
	if a.Backend.Name() != "ollama" {
		t.Fatalf("backend = %s; this case is the unlabelled one", a.Backend.Name())
	}
	if a.activeEngine() != "hailo" {
		t.Errorf("activeEngine = %q, want hailo after the hef probe", a.activeEngine())
	}
	if _, _, err := a.runChatTurn(context.Background(), "hello", &strings.Builder{}, nil, false, ""); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	for _, c := range rs.chats {
		if _, has := c["tools"]; has {
			t.Error("a tools key was sent to an unlabelled hailo-ollama")
		}
	}
}
