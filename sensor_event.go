// Package harvey — sensor_event.go introduces the SensorEvent type: a
// single shape for every sensor/status signal surfaced to the user during
// or after a turn (tool-call outcomes, grounding-check findings, tool-call-
// format warnings). This is pure routing plumbing per
// harness-prerequisite-refactor-plan.md Phase C — rendering is unchanged
// from before this type existed. See harness-engineering-exploration.md
// Direction C for the planned follow-on (a two-view sensor sidecar
// distinguishing human vs. agent-facing output); this type is the
// prerequisite for that design, not the design itself.
package harvey

import (
	"fmt"
	"io"
)

/** SensorClass distinguishes deterministic, CPU-run findings from LLM-judged
 * ones, per the harness-engineering framework in
 * harness-engineering-exploration.md (Böckeler's computational/inferential
 * split). Every sensor Harvey currently emits — tool-call results, the
 * grounding check, the prose-tool-call-syntax warning — is Computational:
 * each is a deterministic check over already-known content, not another LLM
 * call. Inferential exists so a future sensor (e.g. an LLM-based review) has
 * a place to mark itself as probabilistic rather than as certain as the
 * checks above.
 *
 * Example:
 *   ev := SensorEvent{Kind: "grounding", Message: "...", Class: Computational}
 */
type SensorClass int

const (
	// Computational marks a deterministic, CPU-run finding (e.g. a string
	// match, a static check) — safe to treat as certain.
	Computational SensorClass = iota
	// Inferential marks an LLM-judged finding — probabilistic, not yet
	// produced by anything in Harvey today.
	Inferential
)

/** SensorEvent is a single sensor/status signal surfaced to the user during
 * or after a turn. Every sensor signal in Harvey constructs one of these —
 * tool-call outcomes (tool_executor.go), the grounding check (grounding.go),
 * and the prose-tool-call-syntax warning (terminal.go) — even though today's
 * rendering is unchanged from before this type existed.
 *
 * Fields:
 *   Kind    (string)      — short identifier, e.g. "tool_call", "grounding", "prose_tool_syntax".
 *   Message (string)      — human-readable text to show the user.
 *   Class   (SensorClass) — Computational or Inferential.
 *
 * Example:
 *   ev := SensorEvent{Kind: "tool_call", Message: "Calling read_file…", Class: Computational}
 */
type SensorEvent struct {
	Kind    string
	Message string
	Class   SensorClass
}

// reportSensorEvent writes ev to out as a warning line, styled identically
// to Harvey's pre-existing ad-hoc sensor warnings (the grounding check's
// result, the prose-tool-call-syntax warning) — the single place this
// formatting exists now, replacing two independent copies. Class/Kind do
// not yet affect rendering — see the sensor-sidecar note above.
func reportSensorEvent(out io.Writer, ev SensorEvent) {
	fmt.Fprintln(out, yellow("  ⚠ ")+ev.Message)
}
