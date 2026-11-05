package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The routing hints of DR-0051 item 5. Assay reports and validates; it never
// edits harvey.yaml. Thresholds approved 2026-11-05.
const (
	reliableMin  = 0.8              // rule 1: a target is reliable at this pass rate
	weakBelow    = 0.5              // rule 1: below this it is weak
	speedMargin  = 1.5              // rule 4: first-token ratio that names a faster engine
	coldStartMax = 10 * time.Second // rule 7: a longer cold start says "keep warm"
	thinBelow    = 3                // rule 8: fewer trials than this is thin evidence
)

// ruleTable is printed verbatim in the report and carried in summary.json, so a
// reader can disagree with a hint and see which rule made it.
var ruleTable = []string{
	"1. A target is reliable on a capability at a pass rate of 0.8 or more, and weak below 0.5.",
	"2. Neither: no target is reliable. The hint names the best target and its rate.",
	"3. One engine only: reliable targets exist on one engine. The hint names it and the other engine's best.",
	"4. Both engines reliable: either, unless one engine's best has a warm first token 1.5 times quicker or more, then that engine.",
	"5. Window (long-prompt): a target reads up to the largest probe size it passed with every smaller size also passed; the hint names what happened at the next size. Longer prompts go to the other engine.",
	"6. Tool-ready: a target passes 0.8 or more of the tool_call probes and every no_tool control; the hint says which part failed. For tool-call, rules 2 to 4 use tool-ready as reliable.",
	"7. Cold start: over 10 seconds, the hint says to keep the model loaded.",
	"8. Thin evidence: fewer than 3 trials behind a hint ends it with (thin: n trials).",
}

/** Hint is one line of routing advice with the rule behind it.
 *
 * Fields:
 *   Capability (string) — the corpus category, or "cold-start"
 *   Target (string)     — "service/model" for a per-target hint; "" for a capability-wide route
 *   Route (string)      — ollama, hailo, either or neither; "" for a per-target note
 *   Rule (int)          — which rule of ruleTable made it
 *   Text (string)       — the advice with its evidence
 */
type Hint struct {
	Capability string `json:"capability"`
	Target     string `json:"target,omitempty"`
	Route      string `json:"route,omitempty"`
	Rule       int    `json:"rule"`
	Text       string `json:"text"`
}

func thin(n int) string {
	if n < thinBelow {
		if n == 1 {
			return " (thin: 1 trial)"
		}
		return fmt.Sprintf(" (thin: %d trials)", n)
	}
	return ""
}

func rate(c Cell) float64 {
	if c.N == 0 {
		return -1
	}
	return float64(c.Pass) / float64(c.N)
}

func frac(c Cell) string { return fmt.Sprintf("%d/%d", c.Pass, c.N) }

// toolStatus tallies the tool_call and no_tool checks of one target.
type toolStatus struct {
	callPass, callN, noPass, noN int
	plainText, invented          int
}

func (s toolStatus) ready() bool {
	return s.callN > 0 && s.callPass*5 >= s.callN*4 && s.noN > 0 && s.noPass == s.noN
}

func toolStatusOf(results []PromptResult, label string) toolStatus {
	var s toolStatus
	for _, r := range results {
		if r.Label() != label || r.Variant == "guide" {
			continue
		}
		for _, c := range r.Checks {
			switch c.Name {
			case "tool_call":
				s.callN++
				if c.Passed {
					s.callPass++
				} else if strings.Contains(c.Detail, "plain text") {
					s.plainText++
				}
			case "no_tool":
				s.noN++
				if c.Passed {
					s.noPass++
				} else {
					s.invented++
				}
			}
		}
	}
	return s
}

/** buildHints applies the rules of ruleTable to a finished run.
 *
 * Parameters:
 *   ar (AssayResults) — the results and warm-ups
 *   m (Matrix)        — buildMatrix(ar.Results)
 *
 * Returns:
 *   []Hint — a capability-wide route per capability (rules 2 to 4), per-target
 *   notes for the window (5), tool-readiness (6) and cold start (7); a
 *   capability with no data gets none
 *
 * Example:
 *   hs := buildHints(ar, buildMatrix(ar.Results))
 */
func buildHints(ar AssayResults, m Matrix) []Hint {
	var out []Hint
	for _, c := range m.Categories {
		if c == "long-prompt" {
			out = append(out, windowHints(ar.Results, m)...)
			continue
		}
		if c == "tool-call" {
			for _, t := range m.Targets {
				if s := toolStatusOf(ar.Results, t); s.callN+s.noN > 0 {
					out = append(out, toolHint(t, s))
				}
			}
		}
		if h, ok := routeHint(ar.Results, m, c); ok {
			out = append(out, h)
		}
	}
	for _, w := range ar.Warmups {
		if w.Error == "" && w.Cold > coldStartMax {
			out = append(out, Hint{Capability: "cold-start", Target: w.Service + "/" + w.Model, Rule: 7,
				Text: fmt.Sprintf("cold start %.1fs: keep this model loaded before relying on it", w.Cold.Seconds())})
		}
	}
	return out
}

func toolHint(t string, s toolStatus) Hint {
	minN := s.callN
	if s.noN < minN {
		minN = s.noN
	}
	var sb strings.Builder
	if s.ready() {
		fmt.Fprintf(&sb, "tool-ready: tool_call %d/%d, no_tool %d/%d", s.callPass, s.callN, s.noPass, s.noN)
	} else {
		fmt.Fprintf(&sb, "not tool-ready: tool_call %d/%d", s.callPass, s.callN)
		if s.plainText > 0 {
			fmt.Fprintf(&sb, " (wrote the call as plain text %dx)", s.plainText)
		}
		fmt.Fprintf(&sb, ", no_tool %d/%d", s.noPass, s.noN)
		if s.invented > 0 {
			fmt.Fprintf(&sb, " (invented a call %dx)", s.invented)
		}
		if s.noN == 0 {
			sb.WriteString(" (no no_tool control ran)")
		}
	}
	sb.WriteString(thin(minN))
	return Hint{Capability: "tool-call", Target: t, Rule: 6, Text: sb.String()}
}

func routeHint(results []PromptResult, m Matrix, c string) (Hint, bool) {
	type cand struct {
		label string
		cell  Cell
	}
	var all []cand
	reliable := map[string][]cand{} // by service
	for _, t := range m.Targets {
		cell, ok := m.Cells[t][c]
		if !ok || cell.N == 0 {
			continue
		}
		cd := cand{t, cell}
		all = append(all, cd)
		ok = rate(cell) >= reliableMin
		if c == "tool-call" {
			ok = toolStatusOf(results, t).ready()
		}
		if ok {
			svc, _, _ := strings.Cut(t, "/")
			reliable[svc] = append(reliable[svc], cd)
		}
	}
	if len(all) == 0 {
		return Hint{}, false
	}
	better := func(a, b cand) bool { // a before b
		if rate(a.cell) != rate(b.cell) {
			return rate(a.cell) > rate(b.cell)
		}
		fa, fb := a.cell.MedianFirstToken, b.cell.MedianFirstToken
		return fa > 0 && (fb == 0 || fa < fb)
	}
	best := func(cs []cand) cand {
		sort.SliceStable(cs, func(i, j int) bool { return better(cs[i], cs[j]) })
		return cs[0]
	}
	h := Hint{Capability: c}
	switch {
	case len(reliable) == 0:
		b := best(all)
		word := "unreliable"
		if rate(b.cell) < weakBelow {
			word = "weak"
		}
		h.Route, h.Rule = "neither", 2
		h.Text = fmt.Sprintf("neither: best is %s at %s (%s)%s", b.label, frac(b.cell), word, thin(b.cell.N))
	case len(reliable) == 1:
		var svc string
		for s := range reliable {
			svc = s
		}
		b := best(reliable[svc])
		other := "ollama"
		if svc == "ollama" {
			other = "hailo"
		}
		var oc []cand
		for _, a := range all {
			if !strings.HasPrefix(a.label, svc+"/") {
				oc = append(oc, a)
			}
		}
		note := fmt.Sprintf("not compared: no %s target ran", other)
		minN := b.cell.N
		if len(oc) > 0 {
			ob := best(oc)
			note = fmt.Sprintf("%s best: %s at %s", other, ob.label, frac(ob.cell))
			if ob.cell.N < minN {
				minN = ob.cell.N
			}
		}
		h.Route, h.Rule = svc, 3
		h.Text = fmt.Sprintf("%s only: %s at %s (%s)%s", svc, b.label, frac(b.cell), note, thin(minN))
	default:
		ho, ol := best(reliable["hailo"]), best(reliable["ollama"])
		minN := ho.cell.N
		if ol.cell.N < minN {
			minN = ol.cell.N
		}
		h.Rule = 4
		fh, fo := ho.cell.MedianFirstToken, ol.cell.MedianFirstToken
		switch {
		case fh > 0 && fo > 0 && float64(fo)/float64(fh) >= speedMargin:
			h.Route = "hailo"
			h.Text = fmt.Sprintf("hailo: both reliable; %s %s to first token against %s %s (%.1fx faster)", ho.label, secs(fh), ol.label, secs(fo), float64(fo)/float64(fh))
		case fh > 0 && fo > 0 && float64(fh)/float64(fo) >= speedMargin:
			h.Route = "ollama"
			h.Text = fmt.Sprintf("ollama: both reliable; %s %s to first token against %s %s (%.1fx faster)", ol.label, secs(fo), ho.label, secs(fh), float64(fh)/float64(fo))
		case fh > 0 && fo > 0:
			h.Route = "either"
			h.Text = fmt.Sprintf("either: both reliable (%s %s, %s %s); first token within %.1fx", ho.label, frac(ho.cell), ol.label, frac(ol.cell), speedMargin)
		default:
			h.Route = "either"
			h.Text = fmt.Sprintf("either: both reliable (%s %s, %s %s); no first-token timing to compare", ho.label, frac(ho.cell), ol.label, frac(ol.cell))
		}
		h.Text += thin(minN)
	}
	return h, true
}

func windowHints(results []PromptResult, m Matrix) []Hint {
	type size struct {
		runs, fails int
		server      bool
	}
	var out []Hint
	for _, t := range m.Targets {
		bySize := map[int]*size{}
		for _, r := range results {
			if r.Label() != t || r.Category != "long-prompt" || r.PromptSizeEst <= 0 || isCallError(r) {
				continue
			}
			s := bySize[r.PromptSizeEst]
			if s == nil {
				s = &size{}
				bySize[r.PromptSizeEst] = s
			}
			s.runs++
			if !r.AutoPass {
				s.fails++
				for _, c := range r.Checks {
					if strings.Contains(c.Detail, "server failed") {
						s.server = true
					}
				}
			}
		}
		if len(bySize) == 0 {
			continue
		}
		var sizes []int
		minRuns := 1 << 30
		for sz, s := range bySize {
			sizes = append(sizes, sz)
			if s.runs < minRuns {
				minRuns = s.runs
			}
		}
		sort.Ints(sizes)
		reach, failAt := 0, 0
		for _, sz := range sizes {
			if bySize[sz].fails > 0 {
				failAt = sz
				break
			}
			reach = sz
		}
		what := func(sz int) string {
			if bySize[sz].server {
				return "server error"
			}
			return "marker lost"
		}
		var txt string
		switch {
		case failAt == 0:
			txt = fmt.Sprintf("reads all sizes tested, up to ~%d tokens (the sweep ends there)", reach)
		case reach == 0:
			txt = fmt.Sprintf("reads none of the sizes tested: %s already at ~%d tokens", what(failAt), failAt)
		default:
			txt = fmt.Sprintf("reads up to ~%d tokens; at ~%d tokens: %s; send longer prompts to the other engine", reach, failAt, what(failAt))
		}
		out = append(out, Hint{Capability: "long-prompt", Target: t, Rule: 5, Text: txt + thin(minRuns)})
	}
	return out
}

// hintsText renders hints as indented lines for the terminal and the report.
func hintsText(hs []Hint) string {
	var sb strings.Builder
	for _, h := range hs {
		who := "[" + h.Capability + "]"
		if h.Target != "" {
			who = "[" + h.Capability + "] " + h.Target
		}
		fmt.Fprintf(&sb, "  %s: %s\n", who, h.Text)
	}
	return sb.String()
}
