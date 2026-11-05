package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A6b of assay-profile-plan.md (DR-0051 item 5): the rule-based routing hints.
// Thresholds approved by RSDOIEL 2026-11-05: reliable 0.8, weak below 0.5,
// speed margin 1.5x, tool-ready 0.8 plus every no_tool control, cold start
// over 10 s, thin under 3 trials.

// trials makes n results for one target and category, pass of them passing, each
// with the given first-token time.
func trials(svc, model, cat string, pass, n int, first time.Duration) []PromptResult {
	var out []PromptResult
	for i := 0; i < n; i++ {
		out = append(out, res(svc, model, cat, i < pass, first, 8))
	}
	return out
}

func hintFor(hs []Hint, capability, target string) *Hint {
	for i := range hs {
		if hs[i].Capability == capability && hs[i].Target == target {
			return &hs[i]
		}
	}
	return nil
}

func hints(results []PromptResult, warm ...Warmup) []Hint {
	ar := AssayResults{Results: results, Warmups: warm}
	return buildHints(ar, buildMatrix(results))
}

func TestHints_NeitherWhenNoTargetIsReliable(t *testing.T) {
	rs := append(trials("hailo", "a", "go", 1, 5, time.Second), trials("ollama", "b", "go", 2, 5, time.Second)...)
	h := hintFor(hints(rs), "go", "")
	if h == nil || h.Route != "neither" || !strings.Contains(h.Text, "ollama/b") || !strings.Contains(h.Text, "2/5") {
		t.Errorf("hint %+v, want neither, naming the best target ollama/b at 2/5", h)
	}
}

func TestHints_OnlyOneEngineReliable(t *testing.T) {
	rs := append(trials("hailo", "a", "go", 5, 5, time.Second), trials("ollama", "b", "go", 2, 5, time.Second)...)
	h := hintFor(hints(rs), "go", "")
	if h == nil || h.Route != "hailo" || !strings.Contains(h.Text, "hailo only") || !strings.Contains(h.Text, "ollama/b") {
		t.Errorf("hint %+v, want hailo only, mentioning ollama/b", h)
	}
	// An engine that did not run is "not compared", not a failure.
	solo := hintFor(hints(trials("ollama", "b", "go", 5, 5, time.Second)), "go", "")
	if solo == nil || solo.Route != "ollama" || !strings.Contains(solo.Text, "not compared") {
		t.Errorf("solo hint %+v, want ollama, not compared", solo)
	}
}

func TestHints_BothReliableNamesTheFasterOnlyBeyondTheMargin(t *testing.T) {
	fast := append(trials("hailo", "a", "go", 5, 5, 2*time.Second), trials("ollama", "b", "go", 5, 5, 6*time.Second)...)
	h := hintFor(hints(fast), "go", "")
	if h == nil || h.Route != "hailo" || !strings.Contains(h.Text, "faster") || !strings.Contains(h.Text, "3.0x") {
		t.Errorf("hint %+v, want hailo, faster, 3.0x", h)
	}
	close := append(trials("hailo", "a", "go", 5, 5, 4*time.Second), trials("ollama", "b", "go", 5, 5, 5*time.Second)...)
	if h := hintFor(hints(close), "go", ""); h == nil || h.Route != "either" {
		t.Errorf("hint %+v, want either: 1.25x is inside the 1.5x margin", h)
	}
}

func TestHints_ThinEvidenceIsSaid(t *testing.T) {
	two := append(trials("hailo", "a", "go", 2, 2, time.Second), trials("ollama", "b", "go", 2, 2, 5*time.Second)...)
	if h := hintFor(hints(two), "go", ""); h == nil || !strings.Contains(h.Text, "(thin: 2 trials)") {
		t.Errorf("hint %+v, want (thin: 2 trials)", h)
	}
	three := append(trials("hailo", "a", "go", 3, 3, time.Second), trials("ollama", "b", "go", 3, 3, 5*time.Second)...)
	if h := hintFor(hints(three), "go", ""); h == nil || strings.Contains(h.Text, "thin") {
		t.Errorf("hint %+v, three trials are not thin", h)
	}
}

func TestHints_NoDataNoHint(t *testing.T) {
	failed := []PromptResult{{Service: "hailo", Model: "a", Category: "go", Checks: []CheckResult{{Name: "call"}}}}
	if h := hintFor(hints(failed), "go", ""); h != nil {
		t.Errorf("hint %+v for a capability where every call failed, want none", h)
	}
}

func sized(svc, model string, size int, pass bool, detail string) PromptResult {
	return PromptResult{Service: svc, Model: model, Category: "long-prompt", PromptSizeEst: size, AutoPass: pass,
		Checks: []CheckResult{{Name: "uses_tail", Passed: pass, Detail: detail}}}
}

func TestHints_WindowReach(t *testing.T) {
	var rs []PromptResult
	for _, s := range []int{500, 1200} {
		rs = append(rs, sized("hailo", "a", s, true, ""))
	}
	rs = append(rs, sized("hailo", "a", 1700, false, "the server failed on a prompt of about 1716 tokens: x"), sized("hailo", "a", 2400, false, "the reply did not reflect"))
	for _, s := range []int{500, 1200, 1700, 2400} {
		rs = append(rs, sized("ollama", "b", s, true, ""))
	}
	rs = append(rs, sized("hailo", "c", 500, false, "the reply did not reflect"))
	hs := hints(rs)
	if h := hintFor(hs, "long-prompt", "hailo/a"); h == nil || !strings.Contains(h.Text, "up to ~1200") || !strings.Contains(h.Text, "server error") || !strings.Contains(h.Text, "1700") {
		t.Errorf("hailo/a %+v, want reach 1200 and a server error at 1700", h)
	}
	if h := hintFor(hs, "long-prompt", "ollama/b"); h == nil || !strings.Contains(h.Text, "all") || !strings.Contains(h.Text, "2400") {
		t.Errorf("ollama/b %+v, want it read every size up to 2400", h)
	}
	if h := hintFor(hs, "long-prompt", "hailo/c"); h == nil || !strings.Contains(h.Text, "none") || !strings.Contains(h.Text, "marker") {
		t.Errorf("hailo/c %+v, want it read none, the marker lost at 500", h)
	}
}

func toolRes(svc, model, name string, pass bool, detail string) PromptResult {
	return PromptResult{Service: svc, Model: model, Category: "tool-call", AutoPass: pass, Checks: []CheckResult{{Name: name, Passed: pass, Detail: detail}}}
}

func TestHints_ToolReady(t *testing.T) {
	var rs []PromptResult
	for i := 0; i < 4; i++ {
		rs = append(rs, toolRes("hailo", "qwen3:1.7b", "tool_call", true, ""))
	}
	rs = append(rs, toolRes("hailo", "qwen3:1.7b", "no_tool", true, ""), toolRes("hailo", "qwen3:1.7b", "no_tool", true, ""))
	for i := 0; i < 4; i++ {
		rs = append(rs, toolRes("ollama", "text", "tool_call", false, "wrote read_file as plain text, which Harvey does not run"))
	}
	rs = append(rs, toolRes("ollama", "text", "no_tool", true, ""), toolRes("ollama", "text", "no_tool", false, "called search_files although no tool was needed"))
	hs := hints(rs)
	if h := hintFor(hs, "tool-call", "hailo/qwen3:1.7b"); h == nil || !strings.Contains(h.Text, "tool-ready") || strings.Contains(h.Text, "not tool-ready") {
		t.Errorf("hailo/qwen3:1.7b %+v, want tool-ready", h)
	}
	h := hintFor(hs, "tool-call", "ollama/text")
	if h == nil || !strings.Contains(h.Text, "not tool-ready") || !strings.Contains(h.Text, "plain text") || !strings.Contains(h.Text, "invented") {
		t.Errorf("ollama/text %+v, want not tool-ready, plain text, invented a call", h)
	}
	// The capability route follows tool-readiness: only hailo/qwen3:1.7b qualifies.
	if r := hintFor(hs, "tool-call", ""); r == nil || r.Route != "hailo" {
		t.Errorf("tool-call route %+v, want hailo", r)
	}
}

func TestHints_ColdStart(t *testing.T) {
	rs := trials("hailo", "a", "go", 5, 5, time.Second)
	hs := hints(rs, Warmup{Service: "hailo", Model: "a", Cold: 21 * time.Second})
	if h := hintFor(hs, "cold-start", "hailo/a"); h == nil || !strings.Contains(h.Text, "21.0s") || !strings.Contains(h.Text, "keep") {
		t.Errorf("cold start hint %+v", h)
	}
	if h := hintFor(hints(rs, Warmup{Service: "hailo", Model: "a", Cold: 9 * time.Second}), "cold-start", "hailo/a"); h != nil {
		t.Errorf("9 s is under the 10 s line, got %+v", h)
	}
}

func TestMainRun_PrintsHintsAndRecordsThemInTheSummary(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", oneCheckCorpus)
	oll := newRecordingServer(t, "a")
	hai := newRecordingServer(t, "b")
	out := filepath.Join(dir, "out")
	code, stdout, errOut := runAssay(t, "--corpus", corpus, "--output", out, "--ollama", oll.URL, "--hailo", hai.URL, "--runs", "3")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if !strings.Contains(stdout, "Hints") || !strings.Contains(stdout, "cat-a") {
		t.Errorf("stdout lacks the hints:\n%s", stdout)
	}
	sum := readSummary(t, out)
	if hs, _ := sum["hints"].([]any); len(hs) == 0 {
		t.Errorf("summary has no hints: %v", sum["hints"])
	}
	if rules, _ := sum["rules"].([]any); len(rules) < 8 {
		t.Errorf("summary should carry the rule table (8 rules), got %v", sum["rules"])
	}
	rep := readFileOrFail(t, filepath.Join(out, "report.md"))
	if !strings.Contains(rep, "### Hints") || !strings.Contains(rep, "Rules") {
		t.Error("report.md should carry the hints and the rules that produced them")
	}
}

func readFileOrFail(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildMatrix_OverallTimingsLeaveOutSizedProbes(t *testing.T) {
	// A 2,400-token prompt takes long to start whatever the engine; counting it
	// in a target's warm first token would hide the short-prompt figure.
	short := res("ollama", "m", "go", true, 2*time.Second, 5)
	big := res("ollama", "m", "long-prompt", true, 40*time.Second, 5)
	big.PromptSizeEst = 2400
	m := buildMatrix([]PromptResult{short, big})
	if got := m.Overall["ollama/m"].MedianFirstToken; got != 2*time.Second {
		t.Errorf("overall first token %v, want 2s (the sized probe left out)", got)
	}
	if got := m.Cells["ollama/m"]["long-prompt"].MedianFirstToken; got != 40*time.Second {
		t.Errorf("the long-prompt cell keeps its own timing, got %v", got)
	}
}

func TestThin_Singular(t *testing.T) {
	if got := thin(1); got != " (thin: 1 trial)" {
		t.Errorf("thin(1) = %q", got)
	}
	if got := thin(2); got != " (thin: 2 trials)" {
		t.Errorf("thin(2) = %q", got)
	}
}

func TestHints_AHailoModelThatIsSentNoToolsSaysSo(t *testing.T) {
	var rs []PromptResult
	for i := 0; i < 4; i++ {
		rs = append(rs, toolRes("hailo", "qwen2.5-coder:1.5b", "tool_call", false, "no call; wanted read_file"))
	}
	rs = append(rs, toolRes("hailo", "qwen2.5-coder:1.5b", "no_tool", true, ""))
	h := hintFor(hints(rs), "tool-call", "hailo/qwen2.5-coder:1.5b")
	if h == nil || !strings.Contains(h.Text, "no tools are sent") || !strings.Contains(h.Text, "opt-in") || !strings.Contains(h.Text, "0/4") {
		t.Errorf("hint %+v, want it to say no tools are sent to this model, with the 0/4", h)
	}
	// qwen3 is on the list, so its failures are real failures to call.
	var q []PromptResult
	for i := 0; i < 4; i++ {
		q = append(q, toolRes("hailo", "qwen3:1.7b", "tool_call", false, "called list_files, wanted read_file"))
	}
	if h := hintFor(hints(q), "tool-call", "hailo/qwen3:1.7b"); h == nil || strings.Contains(h.Text, "no tools are sent") {
		t.Errorf("qwen3 is sent tools; hint %+v should not say otherwise", h)
	}
}

func TestHints_SpeedNeedsASecondAsWellAsTheRatio(t *testing.T) {
	ms := time.Millisecond
	// 1.5x, but a fifth of a second: noise, not a reason to route.
	noise := append(trials("hailo", "a", "go", 5, 5, 600*ms), trials("ollama", "b", "go", 5, 5, 400*ms)...)
	if h := hintFor(hints(noise), "go", ""); h == nil || h.Route != "either" {
		t.Errorf("0.6s against 0.4s: %+v, want either", h)
	}
	// 1.5x and exactly a second: a reason.
	real := append(trials("hailo", "a", "go", 5, 5, 3*time.Second), trials("ollama", "b", "go", 5, 5, 2*time.Second)...)
	if h := hintFor(hints(real), "go", ""); h == nil || h.Route != "ollama" {
		t.Errorf("3s against 2s: %+v, want ollama (1.5x and 1 s)", h)
	}
}
