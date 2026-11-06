package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A6a of assay-profile-plan.md (DR-0051 items 5-6): the matrix, a warm-up
// request per target, --runs, --timeout and summary.json. The hint rules are A6b.

func res(svc, model, cat string, pass bool, first time.Duration, decode float64) PromptResult {
	return PromptResult{Service: svc, Model: model, Category: cat, AutoPass: pass, FirstToken: first, DecodeTokensPerSec: decode,
		Checks: []CheckResult{{Name: "x", Passed: pass}}}
}

func TestBuildMatrix(t *testing.T) {
	ms := time.Millisecond
	rs := []PromptResult{
		res("hailo", "m", "tool-call", true, 100*ms, 8),
		res("hailo", "m", "tool-call", false, 300*ms, 10),
		res("hailo", "m", "tool-call", true, 200*ms, 6),
		res("hailo", "m", "go", true, 50*ms, 8),
		res("ollama", "m", "tool-call", true, 900*ms, 3),
		{Service: "ollama", Model: "m", Category: "go", Checks: []CheckResult{{Name: "call", Passed: false}}}, // a failed call
	}
	m := buildMatrix(rs)
	if got := strings.Join(m.Targets, ","); got != "hailo/m,ollama/m" {
		t.Errorf("targets %q", got)
	}
	if got := strings.Join(m.Categories, ","); got != "tool-call,go" {
		t.Errorf("categories %q, want them in first-seen order", got)
	}
	c := m.Cells["hailo/m"]["tool-call"]
	if c.Pass != 2 || c.N != 3 || c.MedianFirstToken != 200*ms || c.MedianDecodeTPS != 8 {
		t.Errorf("hailo tool-call cell %+v, want pass 2 of 3, median first token 200ms, median decode 8", c)
	}
	e := m.Cells["ollama/m"]["go"]
	if e.N != 0 || e.Errors != 1 {
		t.Errorf("a failed call is an error, not a failed trial: %+v", e)
	}
}

func TestMedian(t *testing.T) {
	if got := medianDuration([]time.Duration{3, 1, 2}); got != 2 {
		t.Errorf("odd: %v", got)
	}
	if got := medianDuration([]time.Duration{4, 1, 2, 3}); got != 2 && got != 3 {
		t.Errorf("even: %v, want one of the middle two", got)
	}
	if medianDuration(nil) != 0 || medianFloat(nil) != 0 {
		t.Error("an empty median is 0")
	}
}

func chatCounter(t *testing.T, failFirst bool) (*httptest.Server, *int32, *[]string) {
	t.Helper()
	var n int32
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "m"}}})
		case "/api/chat":
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if len(req.Messages) > 0 {
				prompts = append(prompts, req.Messages[len(req.Messages)-1].Content)
			}
			k := atomic.AddInt32(&n, 1)
			if failFirst && k == 1 {
				http.Error(w, "cold", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			json.NewEncoder(w).Encode(map[string]any{"model": "m", "created_at": "2026-11-05T00:00:00Z",
				"message": map[string]string{"role": "assistant", "content": "say hi"}, "done": true, "done_reason": "stop"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &prompts
}

func readSummary(t *testing.T, out string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatalf("summary.json was not written: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMainRun_WarmUpIsOneUnrecordedRequestPerTarget(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	srv, n, prompts := chatCounter(t, false)
	out := filepath.Join(dir, "out")
	code, _, errOut := runAssay(t, "--warmup", "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if *n != 2 {
		t.Errorf("%d chat requests, want 2 (one warm-up, one prompt)", *n)
	}
	if (*prompts)[0] == "say hi" {
		t.Error("the first request should be the warm-up, not the corpus prompt")
	}
	if rows := readResults(t, out); len(rows) != 1 {
		t.Errorf("%d result rows, want 1: the warm-up is not a result", len(rows))
	}
	sum := readSummary(t, out)
	targets := sum["targets"].([]any)
	if len(targets) != 1 || targets[0].(map[string]any)["cold_start_ns"].(float64) <= 0 {
		t.Errorf("summary targets %v, want one with a cold_start_ns", targets)
	}
}

func TestMainRun_NoWarmUpSendsOnlyThePrompts(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	srv, n, _ := chatCounter(t, false)
	code, _, _ := runAssay(t, "--warmup=false", "--corpus", corpus, "--output", filepath.Join(dir, "out"), "--ollama", srv.URL, "--models", "ollama/m")
	if code != 0 || *n != 1 {
		t.Errorf("exit %d with %d requests, want 0 with 1", code, *n)
	}
}

func TestMainRun_AFailedWarmUpIsANoteNotAFailure(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	srv, _, _ := chatCounter(t, true)
	out := filepath.Join(dir, "out")
	code, _, errOut := runAssay(t, "--warmup", "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	if code != 0 {
		t.Fatalf("exit %d, want 0: the prompts worked\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "warm-up") {
		t.Errorf("stderr %q should mention the failed warm-up", errOut)
	}
	tg := readSummary(t, out)["targets"].([]any)[0].(map[string]any)
	if _, ok := tg["warmup_error"]; !ok {
		t.Errorf("summary target %v should carry warmup_error", tg)
	}
}

func TestMainRun_RunsRepeatsEachPrompt(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	srv, n, _ := chatCounter(t, false)
	out := filepath.Join(dir, "out")
	code, stdout, _ := runAssay(t, "--runs", "3", "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	if code != 0 || *n != 3 || len(readResults(t, out)) != 3 {
		t.Fatalf("exit %d, %d requests, %d rows; want 0, 3, 3", code, *n, len(readResults(t, out)))
	}
	cells := readSummary(t, out)["matrix"].([]any)
	if c := cells[0].(map[string]any); c["n"].(float64) != 3 || c["pass"].(float64) != 3 {
		t.Errorf("matrix cell %v, want 3 of 3", c)
	}
	if !strings.Contains(stdout, "3/3") {
		t.Errorf("the matrix printed at the end should show 3/3:\n%s", stdout)
	}
}

func TestMainRun_RunsAndTimeoutMustBePositive(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	srv, _, _ := chatCounter(t, false)
	for _, args := range [][]string{{"--runs", "0"}, {"--runs", "-1"}, {"--timeout", "0"}, {"--timeout", "-5"}} {
		code, _, errOut := runAssay(t, append([]string{"--corpus", corpus, "--ollama", srv.URL, "--models", "ollama/m"}, args...)...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2\n%s", args, code, errOut)
		}
	}
}

func TestMainRun_TimeoutBoundsACall(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
	}))
	t.Cleanup(slow.Close)
	start := time.Now()
	code, _, _ := runAssay(t, "--timeout", "0.1", "--corpus", corpus, "--output", filepath.Join(dir, "out"), "--ollama", slow.URL, "--models", "ollama/m")
	if code == 0 {
		t.Error("a call past --timeout should fail the run")
	}
	if time.Since(start) > 350*time.Millisecond {
		t.Errorf("took %v, the 0.1 s timeout was not applied", time.Since(start))
	}
}

func TestMainRun_PrintsTheMatrixAndWritesTheSummary(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	oll := newRecordingServer(t, "a")
	hai := newRecordingServer(t, "b")
	out := filepath.Join(dir, "out")
	code, stdout, errOut := runAssay(t, "--corpus", corpus, "--output", out, "--ollama", oll.URL, "--hailo", hai.URL)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	for _, want := range []string{"ollama/a", "hailo/b", "cat-a", "1/1"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	sum := readSummary(t, out)
	if sum["schema"].(float64) != 1 {
		t.Errorf("schema %v, want 1", sum["schema"])
	}
	rep, _ := os.ReadFile(filepath.Join(out, "report.md"))
	if !strings.Contains(string(rep), "## Profile") {
		t.Error("report.md should carry the profile matrix")
	}
}
