package harvey

import (
	"fmt"
	"io"
	"strings"
)

// Session-log events for the knowledge base: kb_call and kb_result, and the
// injection that follows when what it returned enters the prompt (DR-0036,
// DR-0039). kb is where provenance of stored knowledge lives; the log is where
// the harness records which of it reached a prompt.

// kbItem is an identity a kb call returned. Retracted is nil when the call
// cannot tell, which is not the same as false.
type kbItem struct {
	Ref       string
	Kind      string
	Retracted *bool
}

type kbInjection struct{ Ref, Text string }

// kbRun collects what the running /kb subcommand reports; the wrapper turns it
// into the kb_result and injection events.
type kbRun struct {
	items   []kbItem
	injects []kbInjection
}

// noteKBItem records an identity the running /kb subcommand returned. It does
// nothing outside a logged /kb call.
func (a *Agent) noteKBItem(ref, kind string, retracted *bool) {
	if a.kbRun != nil {
		a.kbRun.items = append(a.kbRun.items, kbItem{ref, kind, retracted})
	}
}

// noteKBInjection records text a /kb subcommand put into the prompt.
func (a *Agent) noteKBInjection(ref, text string) {
	if a.kbRun != nil {
		a.kbRun.injects = append(a.kbRun.injects, kbInjection{ref, text})
	}
}

/** kbRefOf is the identity the log uses for a kb row: "kb:<type>:<id>", or
 * "kb:<type>:<ref>" for a record, whose qualified reference ("harvey/DR-0034")
 * is its identity.
 *
 * Parameters:
 *   sourceType (string) — observation, record, concept, project, ...
 *   id         (int64)  — the row's id in its own table.
 *   ref        (string) — a record's qualified reference; "" otherwise.
 *
 * Returns:
 *   string — the identity.
 *
 * Example:
 *   kbRefOf("observation", 12, "") // "kb:observation:12"
 *   kbRefOf("record", 0, "harvey/DR-0034") // "kb:record:harvey/DR-0034"
 */
func kbRefOf(sourceType string, id int64, ref string) string {
	if ref != "" {
		return "kb:" + sourceType + ":" + ref
	}
	return fmt.Sprintf("kb:%s:%d", sourceType, id)
}

// cappedBuffer keeps the first max bytes written to it and notes whether more
// came, so a kb command that prints a great deal cannot make the log great.
type cappedBuffer struct {
	max       int
	buf       []byte
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - len(c.buf); room > 0 {
		if len(p) > room {
			c.buf = append(c.buf, p[:room]...)
			c.truncated = true
		} else {
			c.buf = append(c.buf, p...)
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func kbItemsData(items []kbItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		m := map[string]any{"ref": it.Ref, "kind": it.Kind}
		if it.Retracted != nil {
			m["retracted"] = *it.Retracted
		}
		out = append(out, m)
	}
	return out
}

/** loggedKB runs a /kb subcommand between a kb_call and a kb_result. The call
 * is the author's, so it has no turn; the result carries the exit class name
 * as its status, what the command printed (capped), and the identities the
 * subcommand reported. Text a subcommand put into the prompt follows as
 * injection events that cite the result. With no log open it just runs.
 *
 * Parameters:
 *   verb (string)                     — the subcommand, lower-case.
 *   args ([]string)                   — the subcommand and its arguments.
 *   out  (io.Writer)                  — where the command prints.
 *   run  (func(io.Writer) error)      — runs the subcommand against a writer.
 *
 * Returns:
 *   error — whatever the subcommand returned.
 *
 * Example:
 *   return a.loggedKB("search", args, out, func(w io.Writer) error { return kbDispatch(a, args, w) })
 */
func (a *Agent) loggedKB(verb string, args []string, out io.Writer, run func(io.Writer) error) error {
	if a.SessionLog == nil {
		return run(out)
	}
	rest := ""
	if len(args) > 1 {
		rest = strings.Join(args[1:], " ")
	}
	callID := a.logEvent(KindKBCall, LevelInfo, map[string]any{
		"verb": verb, "args": a.logContent(rest), "purpose": "user_command",
	})
	capture := &cappedBuffer{max: maxRunOutput}
	a.kbRun = &kbRun{}
	lastObs := a.LastObservationID
	err := run(io.MultiWriter(out, capture))
	collected := a.kbRun
	a.kbRun = nil
	if verb == "observe" && err == nil && a.LastObservationID != lastObs {
		collected.items = append(collected.items, kbItem{Ref: kbRefOf("observation", a.LastObservationID, ""), Kind: "observation"})
	}
	result := map[string]any{
		"call":   callID,
		"status": ExitCodeFor(err).Name,
		"items":  kbItemsData(collected.items),
		"output": a.logContent(string(capture.buf)),
	}
	if capture.truncated {
		result["truncated"] = true
	}
	resultID := a.logEvent(KindKBResult, LevelInfo, result)
	for _, inj := range collected.injects {
		a.logEvent(KindInjection, LevelInfo, map[string]any{
			"source": "kb", "origin": "user", "ref": inj.Ref, "from": resultID,
			"text": a.logContentBlob(inj.Text),
		})
	}
	return err
}

// logKBRetrievalCall records the knowledge base being asked, on the model's
// behalf, for what bears on query. Returns the kb_call id.
func (a *Agent) logKBRetrievalCall(query string) string {
	if a.SessionLog == nil {
		return ""
	}
	return a.logEvent(KindKBCall, LevelInfo, map[string]any{
		"turn": a.logTurn, "verb": "concept recall", "args": a.logContent(query), "purpose": "retrieval",
	})
}

// kbRowsOf picks the knowledge-base rows out of a recall and names them.
func kbRowsOf(results []UnifiedResult) []UnifiedResult {
	var rows []UnifiedResult
	for _, r := range results {
		if r.Source == "kb" {
			rows = append(rows, r)
		}
	}
	return rows
}

// kbIdentity splits a recall row's id, "kb:<type>:<id>" (or the older
// "kb:<id>" of an observation), into the log's ref and kind.
func kbIdentity(id string) (ref, kind string) {
	parts := strings.SplitN(id, ":", 3)
	if len(parts) == 3 {
		return id, parts[1]
	}
	if len(parts) == 2 {
		return "kb:observation:" + parts[1], "observation"
	}
	return id, "unknown"
}

// logKBRetrievalResult records what the knowledge base answered, including an
// answer of nothing or a failure. Returns the kb_result id.
func (a *Agent) logKBRetrievalResult(call string, results []UnifiedResult, err error) string {
	if a.SessionLog == nil {
		return ""
	}
	var items []kbItem
	for _, r := range kbRowsOf(results) {
		ref, kind := kbIdentity(r.ID)
		items = append(items, kbItem{Ref: ref, Kind: kind, Retracted: r.Retracted})
	}
	return a.logEvent(KindKBResult, LevelInfo, map[string]any{
		"turn": a.logTurn, "call": call, "status": ExitCodeFor(err).Name, "items": kbItemsData(items),
	})
}

// logKBInjections records each recalled kb row that entered the prompt. They
// are automatic: nobody asked for these rows by name. Rows of the other silos
// (memory, rag) are retired by DR-0037 and DR-0035 and are not logged.
func (a *Agent) logKBInjections(result string, results []UnifiedResult) {
	if a.SessionLog == nil {
		return
	}
	for _, r := range kbRowsOf(results) {
		ref, _ := kbIdentity(r.ID)
		retracted := r.Retracted != nil && *r.Retracted
		a.logEvent(KindInjection, LevelInfo, map[string]any{
			"turn": a.logTurn, "source": "kb", "origin": "auto", "ref": ref,
			"retracted": retracted, "from": result, "text": a.logContent(r.Content),
		})
	}
}
