package harvey

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bug (design/memory-aware-context-window-design.md): harvey sent num_ctx
// equal to a model's trained window, 131,072 for llama3.2:3b, and Ollama's
// llama-server was OOM-killed trying to allocate a 14 GiB cache on a 16 GB Pi.

// showBody is /api/show's answer for a model, with the model_info fields the
// window limit reads. The three shapes are real (ollama 2026-11-01).
func showBody(arch string, extra map[string]any, size int64) string {
	mi := map[string]any{"general.architecture": arch}
	for k, v := range extra {
		mi[arch+"."+k] = v
	}
	b, _ := json.Marshal(map[string]any{
		"details":    map[string]any{"family": arch, "parameter_size": "3B", "quantization_level": "Q4_K_M"},
		"model_info": mi,
		"size":       size,
	})
	return string(b)
}

var (
	llamaShow = func() string {
		return showBody("llama", map[string]any{
			"context_length": 131072, "block_count": 28, "embedding_length": 3072,
			"attention.head_count": 24, "attention.head_count_kv": 8,
			"attention.key_length": 128, "attention.value_length": 128,
		}, 2_019_377_088)
	}
	qwenShow = func() string { // no key/value length: embedding / heads = 128
		return showBody("qwen2", map[string]any{
			"context_length": 32768, "block_count": 36, "embedding_length": 2048,
			"attention.head_count": 16, "attention.head_count_kv": 2,
		}, 1_929_902_000)
	}
	graniteShow = func() string { // head_count_kv is one entry per layer
		kv := make([]int, 40)
		for i := range kv {
			kv[i] = 8
		}
		return showBody("granite", map[string]any{
			"context_length": 131072, "block_count": 40, "embedding_length": 2560,
			"attention.head_count": 40, "attention.head_count_kv": kv,
		}, 2_100_000_000)
	}
)

// ollamaFake answers /api/show per model and records what /api/chat is sent.
type ollamaFake struct {
	srv   *httptest.Server
	show  map[string]string
	chats []map[string]any
}

func newOllamaFake(t *testing.T, show map[string]string) *ollamaFake {
	t.Helper()
	f := &ollamaFake{show: show}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			var req struct{ Model string }
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &req)
			if body, ok := f.show[req.Model]; ok {
				fmt.Fprint(w, body)
				return
			}
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
		case "/api/chat":
			var body map[string]any
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			f.chats = append(f.chats, body)
			w.Header().Set("Content-Type", "application/x-ndjson")
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop"}`)
		case "/api/tags":
			fmt.Fprint(w, `{"models":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// numCtxOf is the num_ctx of the n-th chat request; 0 when none was sent.
func (f *ollamaFake) numCtxOf(n int) int {
	if n >= len(f.chats) {
		return -1
	}
	opts, _ := f.chats[n]["options"].(map[string]any)
	v, _ := opts["num_ctx"].(float64)
	return int(v)
}

// windowAgent is an agent whose Ollama is the fake, with a model cache and a
// stated amount of available memory.
func windowAgent(t *testing.T, f *ollamaFake, availGiB float64) *Agent {
	t.Helper()
	a := newTestAgent(t)
	a.Config.Ollama.URL = f.srv.URL
	mc, err := OpenModelCache(a.Workspace, filepath.Join(t.TempDir(), "mc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mc.Close() })
	a.ModelCache = mc
	old := availableMemory
	availableMemory = func() (int64, bool) { return int64(availGiB * (1 << 30)), true }
	t.Cleanup(func() { availableMemory = old })
	return a
}

func chatOnceWith(t *testing.T, a *Agent) {
	t.Helper()
	if _, err := a.Client.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, io.Discard); err != nil {
		t.Fatalf("chat: %v", err)
	}
}

// The reproduction, through the real path: select the model (which probes it),
// then send a turn. With 7.2 GiB available, 131,072 tokens of llama3.2's cache
// (14 GiB) cannot fit and must not be requested.
func TestWindow_ALargeTrainedWindowIsNotRequestedWhenMemoryCannotHoldIt(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	a.setOllamaModel("llama3.2:3b")
	chatOnceWith(t, a)

	got := f.numCtxOf(0)
	if got <= 0 {
		t.Fatalf("num_ctx = %d: a limit was expected", got)
	}
	if got >= 131072 {
		t.Fatalf("num_ctx = %d: the model's trained window was requested; 7.2 GiB cannot hold its 14 GiB cache", got)
	}
	// 0.6 of 7.2 GiB less the 2.02 GB of weights, at 112 KiB a token.
	if got < 16384 || got > 24576 {
		t.Errorf("num_ctx = %d, want about 22,000 (the largest multiple of 1,024 that fits)", got)
	}
	if got%1024 != 0 {
		t.Errorf("num_ctx = %d is not a multiple of 1,024", got)
	}
}

func modelInfoOf(t *testing.T, show string) map[string]any {
	t.Helper()
	var d struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := json.Unmarshal([]byte(show), &d); err != nil {
		t.Fatal(err)
	}
	return d.ModelInfo
}

func TestKVBytesPerToken_TheThreeShapesOllamaReports(t *testing.T) {
	cases := []struct {
		name string
		show string
		want int64
	}{
		// 28 layers x 8 kv heads x (128 + 128) x 2 bytes
		{"llama3.2:3b (scalar kv heads, explicit key and value length)", llamaShow(), 114688},
		// 36 x 2 x (128 + 128) x 2, the head size being embedding 2048 / 16 heads
		{"qwen2.5:3b (head size from embedding / heads)", qwenShow(), 36864},
		// 40 entries of 8 x (64 + 64) x 2, the head size being 2560 / 40
		{"granite4 (one kv head count per layer)", graniteShow(), 81920},
	}
	for _, c := range cases {
		if got := kvBytesPerToken(modelInfoOf(t, c.show)); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestKVBytesPerToken_UnknownWhenTheServerDoesNotSayEnough(t *testing.T) {
	for name, info := range map[string]map[string]any{
		"nothing":                       nil,
		"no architecture":               {"llama.block_count": 28.0},
		"no block count":                {"general.architecture": "llama", "llama.attention.head_count_kv": 8.0, "llama.attention.key_length": 128.0, "llama.attention.value_length": 128.0},
		"no kv heads":                   {"general.architecture": "llama", "llama.block_count": 28.0, "llama.attention.key_length": 128.0, "llama.attention.value_length": 128.0},
		"no head size":                  {"general.architecture": "llama", "llama.block_count": 28.0, "llama.attention.head_count_kv": 8.0},
		"a kv list that is not numbers": {"general.architecture": "llama", "llama.block_count": 2.0, "llama.attention.head_count_kv": []any{"x", "y"}, "llama.attention.key_length": 128.0, "llama.attention.value_length": 128.0},
	} {
		if got := kvBytesPerToken(info); got != 0 {
			t.Errorf("%s: %d, want 0 (unknown)", name, got)
		}
	}
}

const gib = int64(1) << 30

// gibs is n GiB in bytes, for fractions a constant expression cannot express.
func gibs(n float64) int64 { return int64(n * float64(gib)) }

func TestSafeWindow(t *testing.T) {
	llama := WindowInputs{ModelMax: 131072, WeightsBytes: 2_019_377_088, KVBytesPerToken: 114688, MemAvailable: gibs(7.2), MemKnown: true}
	qwen := WindowInputs{ModelMax: 32768, WeightsBytes: 1_929_902_000, KVBytesPerToken: 36864, MemAvailable: gibs(7.2), MemKnown: true}

	// The cause: a 14 GiB cache against 7.2 GiB.
	w := SafeWindow(llama)
	if w.N >= 131072 || w.N%1024 != 0 || w.Reason != "memory" || !w.Fits || w.ModelMax != 131072 {
		t.Errorf("llama on 7.2 GiB: %+v", w)
	}
	// It is the largest that fits, and one more step would not.
	fits := func(n int) bool {
		return float64(llama.WeightsBytes)+float64(llama.KVBytesPerToken)*float64(n) <= 0.6*float64(llama.MemAvailable)
	}
	if !fits(w.N) || fits(w.N+1024) {
		t.Errorf("llama: %d is not the largest window that fits (fits(N)=%v, fits(N+1024)=%v)", w.N, fits(w.N), fits(w.N+1024))
	}

	// A model whose whole window fits keeps it.
	if w := SafeWindow(qwen); w.N != 32768 || w.Reason != "model" {
		t.Errorf("qwen on 7.2 GiB: %+v, want its full 32768", w)
	}

	// More memory, a larger window; the ceiling still caps it.
	big := llama
	big.MemAvailable = 64 * gib
	if w := SafeWindow(big); w.N != 32768 || w.Reason != "ceiling" {
		t.Errorf("llama on 64 GiB: %+v, want the default ceiling 32768", w)
	}
	big.Ceiling = 65536
	if w := SafeWindow(big); w.N != 65536 {
		t.Errorf("llama on 64 GiB with a 65536 ceiling: %+v", w)
	}
	small := llama
	small.Ceiling = 4096
	if w := SafeWindow(small); w.N != 4096 || w.Reason != "ceiling" {
		t.Errorf("a 4096 ceiling: %+v", w)
	}

	// Not enough memory for even the floor: the floor, and Fits says so.
	tiny := llama
	tiny.MemAvailable = 2 * gib
	if w := SafeWindow(tiny); w.N != windowFloor || w.Fits || w.Reason != "memory" {
		t.Errorf("llama on 2 GiB: %+v, want the floor %d and Fits=false", w, windowFloor)
	}
}

func TestSafeWindow_WhatIsNotKnown(t *testing.T) {
	// No memory figure, or no cache estimate: the ceiling alone applies.
	noMem := WindowInputs{ModelMax: 131072, WeightsBytes: 2_019_377_088, KVBytesPerToken: 114688}
	if w := SafeWindow(noMem); w.N != 32768 || w.Reason != "ceiling" {
		t.Errorf("memory unknown: %+v", w)
	}
	noKV := WindowInputs{ModelMax: 131072, MemAvailable: 7 * gib, MemKnown: true}
	if w := SafeWindow(noKV); w.N != 32768 || w.Reason != "ceiling" {
		t.Errorf("cache size unknown: %+v", w)
	}
	// A model that does not say its window: still unknown, so no num_ctx is sent.
	if w := SafeWindow(WindowInputs{MemAvailable: 7 * gib, MemKnown: true, KVBytesPerToken: 1000}); w.N != 0 {
		t.Errorf("window unknown: %+v, want 0", w)
	}
	// A small model with a small window is left alone whatever else is unknown.
	if w := SafeWindow(WindowInputs{ModelMax: 4096}); w.N != 4096 || w.Reason != "model" {
		t.Errorf("small model: %+v", w)
	}
}

func TestParseMemAvailable(t *testing.T) {
	real := "MemTotal:       16607552 kB\nMemFree:         1022400 kB\nMemAvailable:    7559168 kB\nBuffers:          234112 kB\n"
	if n, ok := parseMemAvailable(strings.NewReader(real)); !ok || n != 7559168*1024 {
		t.Errorf("got %d, %v", n, ok)
	}
	for name, in := range map[string]string{
		"empty":          "",
		"no field":       "MemTotal: 16607552 kB\nMemFree: 1 kB\n",
		"no number":      "MemAvailable:\n",
		"not a number":   "MemAvailable: lots kB\n",
		"zero":           "MemAvailable: 0 kB\n",
		"a similar name": "MemAvailableX: 5 kB\n",
	} {
		if n, ok := parseMemAvailable(strings.NewReader(in)); ok {
			t.Errorf("%s: read %d from %q", name, n, in)
		}
	}
}

func TestShowModel_ReportsTheCacheSizePerToken(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow(), "granite4:latest": graniteShow()})
	c := NewOllamaClient(f.srv.URL, "llama3.2:3b")
	d, err := c.ShowModel(t.Context(), "llama3.2:3b")
	if err != nil || d.KVBytesPerToken != 114688 || d.ContextLength != 131072 || d.SizeBytes != 2_019_377_088 {
		t.Errorf("llama: %+v, %v", d, err)
	}
	d, err = c.ShowModel(t.Context(), "granite4:latest")
	if err != nil || d.KVBytesPerToken != 81920 {
		t.Errorf("granite: %+v, %v", d, err)
	}
}

func TestFastProbe_CarriesTheCacheSizeIntoTheCapability(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"qwen2.5:3b": qwenShow()})
	cap, _, err := fastProbe(t.Context(), f.srv.URL, "qwen2.5:3b")
	if err != nil || cap.KVBytesPerToken != 36864 || cap.ContextLength != 32768 {
		t.Errorf("%+v, %v", cap, err)
	}
}

func TestModelCache_RoundTripsTheCacheSizeAndMigratesAnOldDatabase(t *testing.T) {
	ws := &Workspace{Root: t.TempDir()}
	path := filepath.Join(t.TempDir(), "mc.db")
	mc, err := OpenModelCache(ws, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := mc.Set(&ModelCapability{Name: "llama3.2:3b", ContextLength: 131072, SizeBytes: 5, KVBytesPerToken: 114688}); err != nil {
		t.Fatal(err)
	}
	got, err := mc.Get("llama3.2:3b")
	if err != nil || got == nil || got.KVBytesPerToken != 114688 {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	all, _ := mc.All()
	if len(all) != 1 || all[0].KVBytesPerToken != 114688 {
		t.Errorf("All = %+v", all)
	}
	mc.Close()

	// A cache written before the column existed opens, keeps its rows, and reads 0.
	old := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE model_capabilities (name TEXT PRIMARY KEY, family TEXT NOT NULL DEFAULT '', parameter_size TEXT NOT NULL DEFAULT '', quantization TEXT NOT NULL DEFAULT '', size_bytes INTEGER NOT NULL DEFAULT 0, context_length INTEGER NOT NULL DEFAULT 0, supports_tools INTEGER NOT NULL DEFAULT -1, supports_embed INTEGER NOT NULL DEFAULT -1, supports_tagged_blocks INTEGER NOT NULL DEFAULT -1, probe_level TEXT NOT NULL DEFAULT 'none', probed_at DATETIME DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO model_capabilities (name, context_length) VALUES ('old-model', 8192)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	mc2, err := OpenModelCache(ws, old)
	if err != nil {
		t.Fatalf("an old cache did not open: %v", err)
	}
	defer mc2.Close()
	g, err := mc2.Get("old-model")
	if err != nil || g == nil || g.ContextLength != 8192 || g.KVBytesPerToken != 0 {
		t.Errorf("old row = %+v, %v", g, err)
	}
}

// The window harvey budgets against is the window it asks the server for.
func TestWindow_TheBudgetAndTheRequestAreTheSameNumber(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	a.setOllamaModel("llama3.2:3b")
	chatOnceWith(t, a)
	if got, want := f.numCtxOf(0), a.effectiveContextLimit(); got != want || got == 0 {
		t.Errorf("sent num_ctx %d, effectiveContextLimit %d", got, want)
	}
}

// Switching models recomputes the limit for the model switched to.
func TestWindow_EachModelSwitchFixesItsOwnLimit(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow(), "qwen2.5:3b": qwenShow(), "granite4:latest": graniteShow()})
	a := windowAgent(t, f, 7.2)
	var sent []int
	for _, m := range []string{"llama3.2:3b", "qwen2.5:3b", "granite4:latest", "llama3.2:3b"} {
		a.setOllamaModel(m)
		chatOnceWith(t, a)
		sent = append(sent, f.numCtxOf(len(f.chats)-1))
	}
	if sent[1] != 32768 {
		t.Errorf("qwen2.5:3b fits in full: %d", sent[1])
	}
	for i, n := range []int{sent[0], sent[2], sent[3]} {
		if n <= 0 || n >= 131072 || n%1024 != 0 {
			t.Errorf("large-window model #%d got num_ctx %d", i, n)
		}
	}
	if sent[0] != sent[3] {
		t.Errorf("the same model got %d, then %d, with the same memory", sent[0], sent[3])
	}
	// granite's cache costs less per token than llama's, so it gets more.
	if sent[2] <= sent[0] {
		t.Errorf("granite4 (80 KiB a token) got %d, llama3.2 (112 KiB) got %d", sent[2], sent[0])
	}
}

// The limit is fixed when the model is selected: a request must not change
// num_ctx, because Ollama reloads the model when it does.
func TestWindow_TheLimitDoesNotMoveWithMemoryDuringASession(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	a.setOllamaModel("llama3.2:3b")
	chatOnceWith(t, a)
	availableMemory = func() (int64, bool) { return gibs(1.0), true }
	chatOnceWith(t, a)
	availableMemory = func() (int64, bool) { return gibs(40), true }
	chatOnceWith(t, a)
	if f.numCtxOf(0) != f.numCtxOf(1) || f.numCtxOf(1) != f.numCtxOf(2) {
		t.Errorf("num_ctx moved with memory: %d, %d, %d", f.numCtxOf(0), f.numCtxOf(1), f.numCtxOf(2))
	}
}

func TestWindow_AnExplicitContextLengthIsHonouredAndTheAuthorToldWhenItIsTooMuch(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	a.Config.Ollama.ContextLength = 65536
	a.setOllamaModel("llama3.2:3b")
	chatOnceWith(t, a)
	if f.numCtxOf(0) != 65536 {
		t.Errorf("the explicit setting was not honoured: %d", f.numCtxOf(0))
	}
	n := a.WindowLimit().Notice()
	if !strings.Contains(n, "65,536") || !strings.Contains(n, "more than") {
		t.Errorf("no warning that 65,536 is more than fits: %q", n)
	}
	// An explicit setting that fits needs no warning.
	a.Config.Ollama.ContextLength = 8192
	if n := a.WindowLimit().Notice(); strings.Contains(n, "more than") {
		t.Errorf("a window that fits was warned about: %q", n)
	}
}

func TestWindow_TheCeilingIsConfigurable(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 64)
	a.Config.Ollama.MaxContext = 4096
	a.setOllamaModel("llama3.2:3b")
	chatOnceWith(t, a)
	if f.numCtxOf(0) != 4096 {
		t.Errorf("num_ctx %d, want the ceiling 4096", f.numCtxOf(0))
	}
}

func TestWindow_WhenMemoryCannotBeReadTheCeilingApplies(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	availableMemory = func() (int64, bool) { return 0, false }
	a.setOllamaModel("llama3.2:3b")
	chatOnceWith(t, a)
	if f.numCtxOf(0) != 32768 {
		t.Errorf("num_ctx %d, want the default ceiling 32768", f.numCtxOf(0))
	}
}

// A model already in the cache from an earlier session, with no limit fixed in
// this process (the probe failed, or the server was away), is limited too.
func TestWindow_ACachedModelWithNoLimitFixedYetIsStillLimited(t *testing.T) {
	f := newOllamaFake(t, nil)
	a := windowAgent(t, f, 7.2)
	a.useOllamaClient("llama3.2:3b")
	if err := a.ModelCache.Set(&ModelCapability{Name: "llama3.2:3b", ContextLength: 131072, SizeBytes: 2_019_377_088, KVBytesPerToken: 114688}); err != nil {
		t.Fatal(err)
	}
	if n := a.effectiveContextLimit(); n <= 0 || n >= 131072 {
		t.Errorf("effectiveContextLimit = %d, want a limited window", n)
	}
}

func TestWindow_AModelWithNoEstimateKeepsItsOwnWindow(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"small": showBody("llama", map[string]any{"context_length": 4096}, 1_000_000)})
	a := windowAgent(t, f, 7.2)
	a.setOllamaModel("small")
	chatOnceWith(t, a)
	if f.numCtxOf(0) != 4096 {
		t.Errorf("num_ctx %d, want the model's own 4096", f.numCtxOf(0))
	}
}

func TestLoadHarveyYAML_OllamaMaxContext(t *testing.T) {
	dir := t.TempDir()
	ws := &Workspace{Root: dir}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "harvey.yaml"), []byte("ollama:\n  max_context: 16384\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if err := LoadHarveyYAML(ws, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Ollama.MaxContext != 16384 {
		t.Errorf("MaxContext = %d", cfg.Ollama.MaxContext)
	}
}

// Memory is read when the model is selected, not when the first request is
// made: harvey fixes the number before anything is sent.
func TestWindow_MemoryIsReadWhenTheModelIsSelectedNotAtTheFirstRequest(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	a.setOllamaModel("llama3.2:3b")
	availableMemory = func() (int64, bool) { return gibs(64), true } // before the first request
	chatOnceWith(t, a)
	if got := f.numCtxOf(0); got >= 32768 {
		t.Errorf("num_ctx %d follows the memory at the first request (64 GiB); want the 7.2 GiB at selection", got)
	}
}

// Selecting the same model again fixes the limit afresh, against the memory then.
func TestWindow_SelectingAModelAgainReadsTheMemoryAgain(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	a.setOllamaModel("llama3.2:3b")
	first := a.WindowLimit().N
	availableMemory = func() (int64, bool) { return gibs(64), true }
	a.setOllamaModel("llama3.2:3b")
	if second := a.WindowLimit().N; second <= first {
		t.Errorf("limit %d then %d after memory grew from 7.2 to 64 GiB and the model was selected again", first, second)
	}
}

// Switching by alias (attemptModelSwitch) reaches the model through
// useOllamaClient, with no probe. The limit is still fixed at the switch, from
// the cache, against the memory then, and the author is told.
func TestWindow_ASwitchByAliasFixesTheLimitAndSaysSo(t *testing.T) {
	f := newOllamaFake(t, nil)
	a := windowAgent(t, f, 7.2)
	if err := a.ModelCache.Set(&ModelCapability{Name: "llama3.2:3b", ContextLength: 131072, SizeBytes: 2_019_377_088, KVBytesPerToken: 114688}); err != nil {
		t.Fatal(err)
	}
	a.Config.ModelAliases = map[string]ModelAlias{"llama": {Model: "llama3.2:3b"}}
	var out strings.Builder
	if ok, err := attemptModelSwitch(a, "llama", &out); !ok || err != nil {
		t.Fatalf("switch: %v %v", ok, err)
	}
	chatOnceWith(t, a)
	if n := f.numCtxOf(0); n <= 0 || n >= 131072 {
		t.Errorf("num_ctx %d after the switch", n)
	}
	if !strings.Contains(out.String(), "Context window") || !strings.Contains(out.String(), "memory limits it") {
		t.Errorf("the author was not told:\n%s", out.String())
	}
	// Memory grew; switching again reads it again.
	first := a.WindowLimit().N
	availableMemory = func() (int64, bool) { return gibs(64), true }
	attemptModelSwitch(a, "llama", &out)
	if second := a.WindowLimit().N; second <= first {
		t.Errorf("limit %d then %d after memory grew and the model was switched to again", first, second)
	}
}

func TestWindow_NoNoticeForAModelWithinItsOwnLimits(t *testing.T) {
	f := newOllamaFake(t, nil)
	a := windowAgent(t, f, 7.2)
	a.ModelCache.Set(&ModelCapability{Name: "qwen2.5:3b", ContextLength: 32768, SizeBytes: 1_929_902_000, KVBytesPerToken: 36864})
	a.Config.ModelAliases = map[string]ModelAlias{"q": {Model: "qwen2.5:3b"}}
	var out strings.Builder
	attemptModelSwitch(a, "q", &out)
	if strings.Contains(out.String(), "Context window") {
		t.Errorf("a model that fits was reported as limited:\n%s", out.String())
	}
}

func TestWindow_TheSessionLogSaysWhatSetTheWindow(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	l, _ := openTestLog(t, nil)
	a.SessionLog = l
	a.ModelCache.Set(&ModelCapability{Name: "llama3.2:3b", ContextLength: 131072, SizeBytes: 2_019_377_088, KVBytesPerToken: 114688})
	a.useOllamaClient("llama3.2:3b")
	events, _, err := ReadSessionLog(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	var sw *Event
	for i := range events {
		if events[i].Kind == KindModelSwitch {
			sw = &events[i]
		}
	}
	if sw == nil {
		t.Fatal("no model_switch event")
	}
	var d struct {
		Window struct {
			N      int    `json:"n"`
			Source string `json:"source"`
		} `json:"window"`
	}
	if err := json.Unmarshal(sw.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.Window.Source != "memory" || d.Window.N <= 0 || d.Window.N >= 131072 {
		t.Errorf("model_switch window = %+v, want the limited window with source memory", d.Window)
	}
}

func TestStatus_ShowsTheWindowAndWhyWhenItIsLimited(t *testing.T) {
	f := newOllamaFake(t, map[string]string{"llama3.2:3b": llamaShow()})
	a := windowAgent(t, f, 7.2)
	a.setOllamaModel("llama3.2:3b")
	var out strings.Builder
	if err := cmdStatus(a, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Window:") || !strings.Contains(out.String(), "memory limits it") {
		t.Errorf("/status does not say the window is limited:\n%s", out.String())
	}
}
