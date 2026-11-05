package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	harvey "github.com/rsdoiel/harvey"
)

// The profile checks of DR-0051 item 3: tool_call (and no_tool), uses_tail and
// exact. Each yields a CheckResult; a failed check is a result of the
// evaluation, never an error.

/** ToolCallCheck says which call a model should make for a prompt.
 *
 * Fields:
 *   Name (string)     — the tool that should be called, e.g. "read_file"
 *   Arg (string)      — the argument to look at, e.g. "path"; "" = any
 *   Contains (string) — text the argument must contain; "" = any
 *
 * Example (corpus YAML):
 *   tool_call: {name: read_file, arg: path, contains: main.go}
 */
type ToolCallCheck struct {
	Name     string `yaml:"name"`
	Arg      string `yaml:"arg,omitempty"`
	Contains string `yaml:"contains,omitempty"`
}

// wantsTools reports whether the prompt is a tool-calling probe, which is sent
// with Harvey's read-only tool subset.
func (c Checks) wantsTools() bool { return c.ToolCall != nil || c.NoTool }

/** validateProbeChecks reports a prompt whose profile checks cannot work: a
 * tool_call with no name, tool_call together with no_tool, a uses_tail marker
 * the prompt does not contain, or an exact that is not a regular expression.
 *
 * Parameters:
 *   p (Prompt) — the corpus entry
 *
 * Returns:
 *   error — nil when the checks are usable; otherwise names the prompt
 *
 * Example:
 *   if err := validateProbeChecks(p); err != nil { ... }
 */
func validateProbeChecks(p Prompt) error {
	c := p.Checks
	switch {
	case c.ToolCall != nil && c.ToolCall.Name == "":
		return fmt.Errorf("prompt %q: tool_call needs a name", p.ID)
	case c.ToolCall != nil && c.NoTool:
		return fmt.Errorf("prompt %q: tool_call and no_tool cannot both be set", p.ID)
	case c.UsesTail != "" && !strings.Contains(p.PromptText, c.UsesTail):
		return fmt.Errorf("prompt %q: uses_tail %q does not appear in the prompt, so the reply could never carry it", p.ID, c.UsesTail)
	}
	if c.Exact != "" {
		if _, err := regexp.Compile(c.Exact); err != nil {
			return fmt.Errorf("prompt %q: exact is not a regular expression: %v", p.ID, err)
		}
	}
	return nil
}

/** buildProbePrompt returns the text to send for a prompt. A uses_tail prompt
 * with a size hint is padded with neutral, deterministic filler to about
 * Tokens (by Harvey's estimate) so that its instruction is the very end of the
 * prompt: a model that silently drops what is past its window never sees it.
 * Any other prompt is sent as written.
 *
 * Parameters:
 *   p (Prompt) — the corpus entry
 *
 * Returns:
 *   string — filler (if any) then the prompt's own text
 *
 * Example:
 *   text := buildProbePrompt(p)
 */
func buildProbePrompt(p Prompt) string {
	if p.Checks.UsesTail == "" || p.Tokens <= 0 {
		return p.PromptText
	}
	target := p.Tokens*4 - len(p.PromptText) - 2
	var sb strings.Builder
	for i := 1; sb.Len() < target; i++ {
		fmt.Fprintf(&sb, "Inventory note %d: aisle %d holds %d crates of standard bolts, last counted on day %d.\n",
			i, i%12+1, (i*37)%90+10, i%28+1)
	}
	return sb.String() + "\n" + p.PromptText
}

// textualCall reports a reply that is nothing but a tool call written as JSON
// text ({"name": ..., "arguments"|"parameters": ...}). Harvey runs only
// structured calls and fenced blocks, so such a reply is not a call, but the
// model did mean one.
func textualCall(reply string) (name string, ok bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(strings.TrimSpace(reply)), &obj) != nil {
		return "", false
	}
	_, hasArgs := obj["arguments"]
	_, hasParams := obj["parameters"]
	if json.Unmarshal(obj["name"], &name) != nil || name == "" || !(hasArgs || hasParams) {
		return "", false
	}
	return strings.TrimSpace(name), true
}

/** runToolCheck judges the tool calls a model made. Structured calls are read
 * first; if there are none, a JSON code block in the reply (the prose form
 * Harvey parses) is accepted. Stray white space in names and argument keys is
 * ignored, as Harvey does for hailo.
 *
 * Parameters:
 *   c (Checks)             — the prompt's checks
 *   calls ([]harvey.ToolCall) — structured calls the client returned
 *   reply (string)         — the reply text
 *
 * Returns:
 *   []CheckResult — one result named tool_call or no_tool; nil when the prompt has neither
 *
 * Example:
 *   res := runToolCheck(p.Checks, calls, reply)
 */
func runToolCheck(c Checks, calls []harvey.ToolCall, reply string) []CheckResult {
	if !c.wantsTools() {
		return nil
	}
	if len(calls) == 0 {
		calls = harvey.ProseToolCallsIn(reply)
	}
	plain, wrotePlain := "", false
	if len(calls) == 0 {
		plain, wrotePlain = textualCall(reply)
	}
	if c.NoTool {
		if wrotePlain {
			return []CheckResult{{Name: "no_tool", Detail: fmt.Sprintf("wrote a call to %s as plain text although no tool was needed", plain)}}
		}
		if len(calls) == 0 {
			return []CheckResult{{Name: "no_tool", Passed: true}}
		}
		return []CheckResult{{Name: "no_tool", Detail: fmt.Sprintf("called %s although no tool was needed", strings.TrimSpace(calls[0].Function.Name))}}
	}
	want := c.ToolCall
	if wrotePlain {
		return []CheckResult{{Name: "tool_call", Detail: fmt.Sprintf("wrote %s as plain text, which Harvey does not run; wanted %s as a call", plain, want.Name)}}
	}
	if len(calls) == 0 {
		return []CheckResult{{Name: "tool_call", Detail: "no call; wanted " + want.Name}}
	}
	offered := map[string]bool{}
	for _, t := range harvey.ProfileToolSet() {
		offered[t.Function.Name] = true
	}
	var why string
	for _, tc := range calls {
		name := strings.TrimSpace(tc.Function.Name)
		if !offered[name] {
			why = fmt.Sprintf("called %s, a tool that was not offered", name)
			continue
		}
		if name != want.Name {
			why = fmt.Sprintf("called %s, wanted %s", name, want.Name)
			continue
		}
		if want.Arg == "" {
			return []CheckResult{{Name: "tool_call", Passed: true}}
		}
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		val, found := "", false
		for k, v := range args {
			if strings.TrimSpace(k) == want.Arg {
				val, found = fmt.Sprint(v), true
			}
		}
		if found && strings.Contains(val, want.Contains) {
			return []CheckResult{{Name: "tool_call", Passed: true}}
		}
		if !found {
			why = fmt.Sprintf("called %s without %s", name, want.Arg)
		} else {
			why = fmt.Sprintf("called %s with %s=%q, which does not contain %q", name, want.Arg, val, want.Contains)
		}
	}
	return []CheckResult{{Name: "tool_call", Detail: why}}
}

/** runUsesTailCheck passes when the reply carries the marker that only the
 * last line of the prompt asked for. A failure means the reply did not reflect
 * the marker, which is what a prompt past the window looks like; it does not
 * prove the model never read it.
 *
 * Parameters:
 *   marker (string) — the code word
 *   reply (string)  — the reply text
 *
 * Returns:
 *   CheckResult — named uses_tail
 *
 * Example:
 *   r := runUsesTailCheck("BLUE-HERON-42", reply)
 */
func runUsesTailCheck(marker, reply string) CheckResult {
	if strings.Contains(reply, marker) {
		return CheckResult{Name: "uses_tail", Passed: true}
	}
	return CheckResult{Name: "uses_tail", Detail: fmt.Sprintf("the reply did not reflect %q", marker)}
}

/** runExactCheck passes when the whole reply, trimmed, matches the regular
 * expression (anchored at both ends, "." matches a newline).
 *
 * Parameters:
 *   re (string)    — the expression, already validated at load
 *   reply (string) — the reply text
 *
 * Returns:
 *   CheckResult — named exact
 *
 * Example:
 *   r := runExactCheck(`yes|no`, "no")
 */
func runExactCheck(re, reply string) CheckResult {
	rx, err := regexp.Compile(`(?s)^(?:` + re + `)$`)
	if err != nil {
		return CheckResult{Name: "exact", Detail: err.Error()}
	}
	got := strings.TrimSpace(reply)
	if rx.MatchString(got) {
		return CheckResult{Name: "exact", Passed: true}
	}
	if len(got) > 60 {
		got = got[:60] + "…"
	}
	return CheckResult{Name: "exact", Detail: fmt.Sprintf("the reply %q does not match %s", got, re)}
}

// toolsUnsupported reports an Ollama answer that the model cannot take tools
// at all (HTTP 400). For a tool probe that is a result about the model.
func toolsUnsupported(err error) bool {
	var se *harvey.ChatStatusError
	return errors.As(err, &se) && se.Status == 400 && strings.Contains(se.Message, "does not support tools")
}

// windowFailure reports a server answer that is what an oversized prompt does:
// an HTTP error status, or a stream that ends early (hailo's qwen3 answers 500
// or drops the stream past its window). A refused connection is neither.
func windowFailure(err error) bool {
	var se *harvey.ChatStatusError
	return errors.As(err, &se) || errors.Is(err, harvey.ErrStreamTruncated)
}

// runProbeChecks runs the profile checks that apply to a prompt. unsupported
// is true when the model refused tools, which fails a tool probe with the
// reason. windowErr is the server's failure on a uses_tail prompt, which fails
// that probe naming the prompt's size instead of failing the run.
func runProbeChecks(p Prompt, reply string, calls []harvey.ToolCall, unsupported bool, windowErr error, promptTokens int) []CheckResult {
	var out []CheckResult
	if p.Checks.wantsTools() {
		if unsupported {
			name := "tool_call"
			if p.Checks.NoTool {
				name = "no_tool"
			}
			out = append(out, CheckResult{Name: name, Detail: "the model does not support tools"})
		} else {
			out = append(out, runToolCheck(p.Checks, calls, reply)...)
		}
	}
	if p.Checks.UsesTail != "" {
		if windowErr != nil {
			out = append(out, CheckResult{Name: "uses_tail",
				Detail: fmt.Sprintf("the server failed on a prompt of about %d tokens: %v", promptTokens, windowErr)})
		} else {
			out = append(out, runUsesTailCheck(p.Checks.UsesTail, reply))
		}
	}
	if p.Checks.Exact != "" {
		out = append(out, runExactCheck(p.Checks.Exact, reply))
	}
	return out
}
