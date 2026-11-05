package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A2 of assay-profile-plan.md (DR-0051 items 4 and 6): runs are one request at
// a time, and each result records when the first content arrived.

// slowStreamServer answers /api/chat as a stream: a first content chunk after
// firstDelay, a second after another gap, then the done line with eval_count.
// It records the highest number of chat requests in flight at once.
func slowStreamServer(t *testing.T, tags []string, firstDelay, gap time.Duration) (*httptest.Server, *int32) {
	t.Helper()
	var inflight, peak int32
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			var models []map[string]string
			for _, n := range tags {
				models = append(models, map[string]string{"name": n})
			}
			json.NewEncoder(w).Encode(map[string]any{"models": models})
		case "/api/chat":
			n := atomic.AddInt32(&inflight, 1)
			mu.Lock()
			if n > atomic.LoadInt32(&peak) {
				atomic.StoreInt32(&peak, n)
			}
			mu.Unlock()
			defer atomic.AddInt32(&inflight, -1)
			w.Header().Set("Content-Type", "application/x-ndjson")
			fl := w.(http.Flusher)
			enc := json.NewEncoder(w)
			msg := func(c string, done bool, extra map[string]any) {
				m := map[string]any{"model": "m", "created_at": "2026-11-05T00:00:00Z",
					"message": map[string]string{"role": "assistant", "content": c}, "done": done}
				for k, v := range extra {
					m[k] = v
				}
				enc.Encode(m)
				fl.Flush()
			}
			time.Sleep(firstDelay)
			msg("say ", false, nil)
			time.Sleep(gap)
			msg("hi", true, map[string]any{"done_reason": "stop", "eval_count": 20, "prompt_eval_count": 5})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &peak
}

func TestMainRun_ResultsRecordTimeToFirstToken(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	srv, _ := slowStreamServer(t, nil, 150*time.Millisecond, 100*time.Millisecond)
	out := filepath.Join(dir, "out")

	code, _, errOut := runAssay(t, "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	rows := readResults(t, out)
	if len(rows) != 1 {
		t.Fatalf("%d result rows, want 1", len(rows))
	}
	first := time.Duration(rows[0]["first_token_ns"].(float64))
	elapsed := time.Duration(rows[0]["elapsed_ns"].(float64))
	if first < 150*time.Millisecond || first >= elapsed {
		t.Errorf("first_token_ns %v, want at least the 150ms the server waited and less than elapsed %v", first, elapsed)
	}
	// 20 reply tokens over the ~100ms after the first token: far above the
	// whole-call rate, which includes the 150ms wait.
	decode, ok := rows[0]["decode_tokens_per_sec"].(float64)
	whole := rows[0]["tokens_per_sec"].(float64)
	if !ok || decode <= whole {
		t.Errorf("decode_tokens_per_sec %v should exceed the whole-call tokens_per_sec %v", decode, whole)
	}
}

func TestMainRun_ATargetWithNoContentHasNoFirstToken(t *testing.T) {
	// A failed call has no first token; the field is absent, not zero-as-fast.
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus)
	srv := fakeModelServer(t, nil, "m")
	out := filepath.Join(dir, "out")
	runAssay(t, "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	rows := readResults(t, out)
	if _, present := rows[0]["first_token_ns"]; present {
		t.Errorf("a failed call recorded first_token_ns %v", rows[0]["first_token_ns"])
	}
}

func TestMainRun_RequestsAreSequentialAcrossServers(t *testing.T) {
	// hailo-ollama answers one request at a time (DR-0051 item 4); two servers
	// and several prompts must still never overlap.
	dir := t.TempDir()
	corpus := writeFile(t, dir, "corpus.yaml", oneCheckCorpus+`  - id: p2
    category: cat-a
    description: d
    language: go
    prompt: say hi again
    checks:
      contains: ["hi"]
    human: []
    notes: ""
`)
	a, peakA := slowStreamServer(t, []string{"m1", "m2"}, 20*time.Millisecond, 20*time.Millisecond)
	b, peakB := slowStreamServer(t, []string{"m3"}, 20*time.Millisecond, 20*time.Millisecond)
	code, _, errOut := runAssay(t, "--corpus", corpus, "--output", filepath.Join(dir, "out"), "--ollama", a.URL, "--hailo", b.URL)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if *peakA != 1 || *peakB != 1 {
		t.Errorf("peak requests in flight: ollama %d, hailo %d; want 1 each", *peakA, *peakB)
	}
}

func TestDecodeRate(t *testing.T) {
	ms := time.Millisecond
	for _, tc := range []struct {
		name           string
		tokens         int
		elapsed, first time.Duration
		want           float64
	}{
		{"after the first token only", 20, 250 * ms, 150 * ms, 200},
		{"no tokens counted", 0, 250 * ms, 150 * ms, 0},
		{"no first token", 20, 250 * ms, 0, 0},
		{"nothing after the first token", 20, 150 * ms, 150 * ms, 0},
	} {
		if got := decodeRate(tc.tokens, tc.elapsed, tc.first); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
