package harvey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func confident(text string, score string) string {
	return text + "\n{\"confidence\": " + score + ", \"reason\": \"scripted\"}"
}

// pipelineSession runs /pipeline over two prompt files; replies are the model's
// answers to the calls in order, failOn makes that call fail with HTTP 500.
func pipelineSession(t *testing.T, replies []string, failOn int) (events []Event, rec, logPath string) {
	t.Helper()
	scriptedReplies, scriptedFailOn, scriptedTerminal = replies, failOn, true
	t.Cleanup(func() { scriptedReplies, scriptedFailOn, scriptedTerminal = nil, 0, false })
	dir := scriptedRunWith(t, false, func(root string) {
		os.WriteFile(filepath.Join(root, "s1.md"), []byte("First step: summarise the situation.\n"), 0o644)
		os.WriteFile(filepath.Join(root, "s2.md"), []byte("Second step: propose a fix.\n"), 0o644)
	}, false, "/pipeline 70% s1.md s2.md", "/exit")
	return logAndFountain(t, dir)
}

func TestPipeline_TheLogHasEachStepAsACallAndAResult(t *testing.T) {
	events, _, _ := pipelineSession(t, []string{confident("Step one answer.", "0.9"), confident("Step two answer.", "0.95")}, 0)
	var calls, results []Event
	for _, e := range events {
		switch e.Kind {
		case KindToolCall:
			var d struct{ Tool, Origin string }
			_ = json.Unmarshal(e.Data, &d)
			if d.Tool == "pipeline" {
				calls = append(calls, e)
				if d.Origin != "user" {
					t.Errorf("a pipeline step is the author's act: %s", e.Data)
				}
			}
		case KindToolResult:
			var d struct{ Call string }
			_ = json.Unmarshal(e.Data, &d)
			for _, c := range calls {
				if c.ID == d.Call {
					results = append(results, e)
				}
			}
		}
	}
	if len(calls) != 2 || len(results) != 2 {
		t.Fatalf("%d calls and %d results, want 2 and 2", len(calls), len(results))
	}
	var c struct {
		Args struct {
			File      string   `json:"file"`
			Step      int      `json:"step"`
			Of        int      `json:"of"`
			Threshold float64  `json:"threshold"`
			Files     []string `json:"files"`
			Model     string   `json:"model"`
			Body      Content  `json:"body"`
		} `json:"args"`
	}
	_ = json.Unmarshal(calls[1].Data, &c)
	a := c.Args
	if a.File != "s2.md" || a.Step != 2 || a.Of != 2 || a.Threshold != 0.7 || strings.Join(a.Files, ",") != "s1.md,s2.md" || a.Model != "ollama (llama3.2:3b)" {
		t.Errorf("second step args = %+v", a)
	}
	var r struct {
		Status     string  `json:"status"`
		Confidence float64 `json:"confidence"`
		Output     Content `json:"output"`
	}
	_ = json.Unmarshal(results[1].Data, &r)
	if r.Status != "ok" || r.Confidence != 0.95 || r.Output.Text != "Step two answer." {
		t.Errorf("second step result = %s", results[1].Data)
	}
}

func TestRenderFountain_PipelineScenes(t *testing.T) {
	for name, tc := range map[string]struct {
		replies []string
		failOn  int
	}{
		"two steps ok":         {[]string{confident("Step one answer.", "0.9"), confident("Step two answer.", "0.95")}, 0},
		"second step fails":    {[]string{confident("Step one answer.", "0.9"), confident("Not so sure.", "0.3")}, 0},
		"the first call fails": {nil, 1},
	} {
		t.Run(name, func(t *testing.T) {
			events, _, lp := pipelineSession(t, tc.replies, tc.failOn)
			got := string(RenderFountain(events, RenderOptions{Zone: time.Local, Blobs: BlobDir(lp)}))
			if !strings.Contains(got, "INT. PIPELINE S1.MD STEP 1/2") {
				t.Errorf("no pipeline scene in the rendering:\n%s", got)
			}
			goldenCheck(t, "pipeline_"+strings.ReplaceAll(name, " ", "_"), got)
		})
	}
}
