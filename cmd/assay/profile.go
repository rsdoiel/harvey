package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	harvey "github.com/rsdoiel/harvey"
)

// The profile of a run (DR-0051 items 5-6): a target x capability matrix, a
// warm-up request per target, and summary.json. The routing hints are A6b.

// Cell is one target's results for one capability (a corpus category).
type Cell struct {
	Pass             int           // trials that passed every check
	N                int           // trials that ran (a failed call is not one)
	Errors           int           // calls that failed, so the model never answered
	MedianFirstToken time.Duration // warm time to first content; 0 when none arrived
	MedianDecodeTPS  float64       // reply tokens per second after the first token
}

// Matrix is the profile table: Cells[target label][category].
type Matrix struct {
	Targets    []string
	Categories []string
	Cells      map[string]map[string]Cell
	Overall    map[string]Cell // per target, across every category
}

// isCallError reports a result whose model call failed.
func isCallError(r PromptResult) bool {
	return len(r.Checks) > 0 && r.Checks[0].Name == "call" && !r.Checks[0].Passed
}

func medianDuration(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func medianFloat(f []float64) float64 {
	if len(f) == 0 {
		return 0
	}
	s := append([]float64(nil), f...)
	sort.Float64s(s)
	return s[len(s)/2]
}

/** buildMatrix folds results into the profile table. Targets and categories
 * keep the order they first appear in. A failed call counts as an error, not
 * as a failed trial, so a timeout does not read as a model that cannot do the
 * task. The "guide" variant of a --guide-compare run is left out.
 *
 * Parameters:
 *   results ([]PromptResult) — every result of the run
 *
 * Returns:
 *   Matrix — per target and category: pass, trials, errors, median timings
 *
 * Example:
 *   m := buildMatrix(ar.Results)
 *   c := m.Cells["hailo/qwen3:1.7b"]["tool-call"] // c.Pass of c.N
 */
func buildMatrix(results []PromptResult) Matrix {
	type acc struct {
		pass, n, errs int
		first         []time.Duration
		decode        []float64
	}
	cells := map[string]map[string]*acc{}
	overall := map[string]*acc{}
	var m Matrix
	seenT, seenC := map[string]bool{}, map[string]bool{}
	// timed says whether the result's timings count. A target's overall figures
	// leave out the sized probes: a 2,400-token prompt is slow to start on any
	// engine and would hide the short-prompt time.
	add := func(a *acc, r PromptResult, timed bool) {
		if isCallError(r) {
			a.errs++
			return
		}
		a.n++
		if r.AutoPass {
			a.pass++
		}
		if !timed {
			return
		}
		if r.FirstToken > 0 {
			a.first = append(a.first, r.FirstToken)
		}
		if r.DecodeTokensPerSec > 0 {
			a.decode = append(a.decode, r.DecodeTokensPerSec)
		}
	}
	for _, r := range results {
		if r.Variant == "guide" {
			continue
		}
		t := r.Label()
		if !seenT[t] {
			seenT[t] = true
			m.Targets = append(m.Targets, t)
			cells[t] = map[string]*acc{}
			overall[t] = &acc{}
		}
		if !seenC[r.Category] {
			seenC[r.Category] = true
			m.Categories = append(m.Categories, r.Category)
		}
		if cells[t][r.Category] == nil {
			cells[t][r.Category] = &acc{}
		}
		add(cells[t][r.Category], r, true)
		add(overall[t], r, r.PromptSizeEst == 0)
	}
	freeze := func(a *acc) Cell {
		return Cell{Pass: a.pass, N: a.n, Errors: a.errs, MedianFirstToken: medianDuration(a.first), MedianDecodeTPS: medianFloat(a.decode)}
	}
	m.Cells = map[string]map[string]Cell{}
	m.Overall = map[string]Cell{}
	for t, byCat := range cells {
		m.Cells[t] = map[string]Cell{}
		for c, a := range byCat {
			m.Cells[t][c] = freeze(a)
		}
		m.Overall[t] = freeze(overall[t])
	}
	return m
}

// Warmup is the unrecorded request sent to a target before its prompts, so the
// matrix shows warm timings and the cold start is reported once.
type Warmup struct {
	Service string        `json:"service"`
	Model   string        `json:"model"`
	Cold    time.Duration `json:"cold_start_ns,omitempty"`
	Error   string        `json:"warmup_error,omitempty"`
}

/** runWarmup sends one short request to a target and reports how long it took
 * to start answering. A failure is returned in the result, not as an error: the
 * prompts that follow will show whether the target is usable.
 *
 * Parameters:
 *   ctx (context.Context)           — cancellation
 *   client (*harvey.OllamaChatClient) — the target's client
 *   t (target)                      — which target it is
 *
 * Returns:
 *   Warmup — the cold-start time (first content, else the whole call), or the error text
 *
 * Example:
 *   w := runWarmup(ctx, client, tg)
 */
func runWarmup(ctx context.Context, client *harvey.OllamaChatClient, t target) Warmup {
	w := Warmup{Service: t.Service, Model: t.Model}
	rec := &firstWriteRecorder{}
	start := time.Now()
	_, err := client.Chat(ctx, []harvey.Message{{Role: "user", Content: "Reply with the single word: ready"}}, rec)
	if err != nil {
		w.Error = err.Error()
		return w
	}
	w.Cold = time.Since(start)
	if !rec.first.IsZero() {
		w.Cold = rec.first.Sub(start)
	}
	return w
}

func cellText(c Cell) string {
	switch {
	case c.N == 0 && c.Errors > 0:
		return "error"
	case c.N == 0:
		return "-"
	case c.Errors > 0:
		return fmt.Sprintf("%d/%d +%derr", c.Pass, c.N, c.Errors)
	}
	return fmt.Sprintf("%d/%d", c.Pass, c.N)
}

func secs(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

/** matrixText renders the profile as two plain-text tables: pass/trials per
 * target and capability, then per target the cold start and the warm medians.
 *
 * Parameters:
 *   m (Matrix)        — the profile
 *   warm ([]Warmup)   — the warm-up results, if any were run
 *
 * Returns:
 *   string — the tables, ready to print or to put in the report
 *
 * Example:
 *   fmt.Fprint(out, matrixText(buildMatrix(ar.Results), ar.Warmups))
 */
func matrixText(m Matrix, warm []Warmup) string {
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "target\t%s\n", strings.Join(m.Categories, "\t"))
	for _, t := range m.Targets {
		row := []string{t}
		for _, c := range m.Categories {
			row = append(row, cellText(m.Cells[t][c]))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
	sb.WriteString("\n")
	tw = tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "target\tcold start\twarm first token (short prompts)\tdecode tok/s")
	for _, t := range m.Targets {
		cold := "-"
		for _, w := range warm {
			if w.Service+"/"+w.Model == t {
				switch {
				case w.Error != "":
					cold = "failed"
				case w.Cold > 0:
					cold = secs(w.Cold)
				}
			}
		}
		o := m.Overall[t]
		dec := "-"
		if o.MedianDecodeTPS > 0 {
			dec = fmt.Sprintf("%.1f", o.MedianDecodeTPS)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t, cold, secs(o.MedianFirstToken), dec)
	}
	tw.Flush()
	return sb.String()
}

// Summary is summary.json: the profile in a form kb or a script can read.
type Summary struct {
	Schema         int             `json:"schema"`
	RunAt          time.Time       `json:"run_at"`
	OllamaURL      string          `json:"ollama_url"`
	HailoURL       string          `json:"hailo_url"`
	Runs           int             `json:"runs"`
	TimeoutSeconds float64         `json:"timeout_seconds"`
	Warmup         bool            `json:"warmup"`
	Targets        []SummaryTarget `json:"targets"`
	Categories     []string        `json:"categories"`
	Matrix         []SummaryCell   `json:"matrix"`
	Hints          []Hint          `json:"hints"`
	Rules          []string        `json:"rules"`
}

// SummaryTarget is one model on one server, with its cold start and warm medians.
type SummaryTarget struct {
	Service            string  `json:"service"`
	Model              string  `json:"model"`
	ColdStartNs        int64   `json:"cold_start_ns,omitempty"`
	WarmupError        string  `json:"warmup_error,omitempty"`
	MedianFirstTokenNs int64   `json:"median_first_token_ns,omitempty"`
	MedianDecodeTPS    float64 `json:"median_decode_tps,omitempty"`
}

// SummaryCell is one (target, capability) cell of the matrix.
type SummaryCell struct {
	Target             string  `json:"target"`
	Category           string  `json:"category"`
	Pass               int     `json:"pass"`
	N                  int     `json:"n"`
	Errors             int     `json:"errors,omitempty"`
	MedianFirstTokenNs int64   `json:"median_first_token_ns,omitempty"`
	MedianDecodeTPS    float64 `json:"median_decode_tps,omitempty"`
}

/** buildSummary assembles summary.json from a finished run.
 *
 * Parameters:
 *   ar (AssayResults) — the run's results and warm-ups
 *   m (Matrix)        — buildMatrix(ar.Results)
 *   runs (int)        — trials per prompt
 *   timeout (time.Duration) — the per-call timeout used
 *
 * Returns:
 *   Summary — ready to marshal
 *
 * Example:
 *   s := buildSummary(ar, buildMatrix(ar.Results), 1, 120*time.Second)
 */
func buildSummary(ar AssayResults, m Matrix, runs int, timeout time.Duration) Summary {
	s := Summary{Schema: 1, RunAt: ar.RunAt, OllamaURL: ar.OllamaURL, HailoURL: ar.HailoURL, Runs: runs,
		TimeoutSeconds: timeout.Seconds(), Warmup: len(ar.Warmups) > 0, Categories: m.Categories,
		Hints: buildHints(ar, m), Rules: ruleTable}
	for _, t := range m.Targets {
		st := SummaryTarget{MedianFirstTokenNs: int64(m.Overall[t].MedianFirstToken), MedianDecodeTPS: m.Overall[t].MedianDecodeTPS}
		st.Service, st.Model, _ = strings.Cut(t, "/")
		for _, w := range ar.Warmups {
			if w.Service+"/"+w.Model == t {
				st.ColdStartNs, st.WarmupError = int64(w.Cold), w.Error
			}
		}
		s.Targets = append(s.Targets, st)
		for _, c := range m.Categories {
			cell, ok := m.Cells[t][c]
			if !ok {
				continue
			}
			s.Matrix = append(s.Matrix, SummaryCell{Target: t, Category: c, Pass: cell.Pass, N: cell.N, Errors: cell.Errors,
				MedianFirstTokenNs: int64(cell.MedianFirstToken), MedianDecodeTPS: cell.MedianDecodeTPS})
		}
	}
	return s
}

// writeSummary writes summary.json beside results.json.
func writeSummary(outDir string, s Summary) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "summary.json"), data, 0o644)
}

// profileSection is the "## Profile" part of report.md.
func profileSection(ar AssayResults) string {
	m := buildMatrix(ar.Results)
	var sb strings.Builder
	sb.WriteString("## Profile\n\nPass/trials per capability; a failed call is shown as +Nerr and is not a trial.\n\n```\n")
	sb.WriteString(matrixText(m, ar.Warmups))
	sb.WriteString("```\n\n### Hints\n\nAdvice for routing, from the evidence above. Assay reports; it changes no configuration.\n\n```\n")
	sb.WriteString(hintsText(buildHints(ar, m)))
	sb.WriteString("```\n\n### Rules\n\nThe rules that made the hints; disagree with a threshold here, not in the hint.\n\n")
	for _, r := range ruleTable {
		sb.WriteString(r + "\n\n")
	}
	return sb.String()
}

var _ io.Writer = (*firstWriteRecorder)(nil)
