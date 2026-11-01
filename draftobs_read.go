package harvey

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// D1 of the observation-drafting plan (design/observation-drafting-design.md,
// decision 3): what a drafting pass reads from a session log.

const (
	/** transcriptArgCap is the most of a tool call's arguments a transcript
	 * entry keeps, in bytes; a longer one is cut and marked.
	 */
	transcriptArgCap = 200

	/** transcriptOutputCap is the most of a tool result's output a transcript
	 * entry keeps, in bytes; a longer one is cut and marked.
	 */
	transcriptOutputCap = 400

	// blobReadCap bounds what LogBlobReader reads of one blob.
	blobReadCap = 1 << 20
)

/** TranscriptEntry is one thing that happened in a session, as a drafting pass
 * reads it: the message, or the tool call joined to its result.
 *
 * Fields:
 *   First (string) — id of the first event the entry comes from (<session>#<seq>).
 *   Last  (string) — id of the last event; the same as First for a single event.
 *   Text  (string) — the entry as the drafter sees it, such as "User: ..." or
 *                    "Tool run_command(go test) -> ok: ...".
 *
 * Example:
 *   // {First: "s#0004", Last: "s#0005", Text: "Tool run_command(go test) -> ok: ok pkg"}
 */
type TranscriptEntry struct {
	First string
	Last  string
	Text  string
}

/** LogBlobReader returns a function that reads a spilled payload of the
 * session log at logPath from its blobs directory (DR-0039): the log
 * agents/sessions/NAME.jsonl keeps blobs in agents/sessions/NAME.blobs/SHA256.
 * It reads at most 1 MiB of a blob, and reports false for one that is missing.
 *
 * Parameters:
 *   logPath (string) — the .jsonl file.
 *
 * Returns:
 *   func(string) (string, bool) — sha256 in, text out.
 *
 * Example:
 *   events, _, _ := ReadSessionLog(path)
 *   entries := DraftTranscript(events, LogBlobReader(path))
 */
func LogBlobReader(logPath string) func(string) (string, bool) {
	dir := strings.TrimSuffix(logPath, filepath.Ext(logPath)) + ".blobs"
	return func(sha string) (string, bool) {
		if sha == "" || strings.ContainsAny(sha, `/\`) {
			return "", false
		}
		f, err := os.Open(filepath.Join(dir, sha))
		if err != nil {
			return "", false
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, blobReadCap))
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

/** DraftTranscript turns a session's events into the entries a drafting pass
 * reads. Kept: the author's messages, the model's replies that finished ok,
 * each tool call joined to its result (or marked "no result"), and the author's
 * own shell and kb commands the same way. Left out: every injection (skills,
 * kb recall, prompts: context harvey supplied, not something the session
 * learned), kb recall calls, pins, compactions, model switches, permissions,
 * faults, requests, failed replies, and anything at level debug. A tool call's
 * arguments and output are cut (transcriptArgCap, transcriptOutputCap).
 *
 * Parameters:
 *   events []Event                      — the log's events, in order.
 *   blob   func(string) (string, bool)  — reads a spilled payload by sha256,
 *                                         or nil to use each payload's preview.
 *
 * Returns:
 *   []TranscriptEntry — in log order; empty when nothing qualifies.
 *
 * Example:
 *   events, _, _ := ReadSessionLog("agents/sessions/s1.jsonl")
 *   for _, e := range DraftTranscript(events, nil) { fmt.Println(e.Text) }
 */
func DraftTranscript(events []Event, blob func(string) (string, bool)) []TranscriptEntry {
	// Index the results by the call they answer, so a call finds its result
	// wherever it landed.
	results := map[string]*logResult{}
	for _, ev := range events {
		if ev.Level != LevelInfo || (ev.Kind != KindToolResult && ev.Kind != KindKBResult) {
			continue
		}
		r := &logResult{id: ev.ID}
		if json.Unmarshal(ev.Data, &r.data) == nil && r.data.Call != "" {
			results[r.data.Call] = r
		}
	}

	var out []TranscriptEntry
	for _, ev := range events {
		if ev.Level != LevelInfo {
			continue
		}
		switch ev.Kind {
		case KindUserInput:
			var d struct {
				Text json.RawMessage `json:"text"`
			}
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			if t := strings.TrimSpace(rawText(d.Text, blob)); t != "" {
				out = append(out, TranscriptEntry{ev.ID, ev.ID, "User: " + t})
			}
		case KindModelResponse:
			var d struct {
				Status string          `json:"status"`
				Text   json.RawMessage `json:"text"`
			}
			if json.Unmarshal(ev.Data, &d) != nil || d.Status != "ok" {
				continue
			}
			if t := strings.TrimSpace(rawText(d.Text, blob)); t != "" {
				out = append(out, TranscriptEntry{ev.ID, ev.ID, "Model: " + t})
			}
		case KindToolCall:
			var d struct {
				Tool   string          `json:"tool"`
				Origin string          `json:"origin"`
				Args   json.RawMessage `json:"args"`
			}
			if json.Unmarshal(ev.Data, &d) != nil || d.Tool == "" {
				continue
			}
			args := cutMarked(strings.TrimSpace(argsText(d.Args, blob)), transcriptArgCap)
			who := "Tool " + d.Tool + "(" + args + ")"
			if d.Origin == "user" && d.Tool == "shell" {
				who = "Author ran shell: " + args
			}
			out = append(out, TranscriptEntry{ev.ID, ev.ID, who}.withResult(results[ev.ID], blob))
		case KindKBCall:
			var d struct {
				Verb    string          `json:"verb"`
				Args    json.RawMessage `json:"args"`
				Purpose string          `json:"purpose"`
			}
			if json.Unmarshal(ev.Data, &d) != nil || d.Purpose != "user_command" {
				continue
			}
			args := cutMarked(strings.TrimSpace(argsText(d.Args, blob)), transcriptArgCap)
			line := strings.TrimSpace("Author ran kb " + d.Verb + " " + args)
			out = append(out, TranscriptEntry{ev.ID, ev.ID, line}.withResult(results[ev.ID], blob))
		}
	}
	return out
}

// logResult is the event that answers a tool call or a kb call.
type logResult struct {
	id   string
	data struct {
		Call   string          `json:"call"`
		Status string          `json:"status"`
		Output json.RawMessage `json:"output"`
		Error  json.RawMessage `json:"error"`
		Exit   *int            `json:"exit"`
	}
}

// withResult appends " -> STATUS: OUTPUT" to an entry and extends its span to
// the result's event; with no result it says so.
func (e TranscriptEntry) withResult(r *logResult, blob func(string) (string, bool)) TranscriptEntry {
	if r == nil {
		e.Text += " -> no result"
		return e
	}
	e.Last = r.id
	status := r.data.Status
	if status == "" {
		status = "ok"
	}
	out := strings.TrimSpace(rawText(r.data.Output, blob))
	if out == "" {
		out = strings.TrimSpace(rawText(r.data.Error, blob))
	}
	e.Text += " -> " + status
	if out != "" {
		e.Text += ": " + cutMarked(out, transcriptOutputCap)
	}
	return e
}

// rawText reads a Content value (a bare string or an object) out of raw JSON.
func rawText(raw json.RawMessage, blob func(string) (string, bool)) string {
	if len(raw) == 0 {
		return ""
	}
	var c Content
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	return contentText(c, blob)
}

// contentText is a Content's text: inline, else the blob, else its preview.
func contentText(c Content, blob func(string) (string, bool)) string {
	if c.Blob == "" {
		return c.Text
	}
	if blob != nil {
		if t, ok := blob(c.Blob); ok {
			return t
		}
	}
	return c.Preview
}

// argsText renders a tool call's arguments: a Content as its text, an object
// with a cmd (the author's shell) as the command, anything else as compact JSON.
func argsText(raw json.RawMessage, blob func(string) (string, bool)) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		if cmd, ok := obj["cmd"]; ok {
			return rawText(cmd, blob)
		}
		if _, ok := obj["text"]; ok {
			return rawText(raw, blob)
		}
		if _, ok := obj["blob"]; ok {
			return rawText(raw, blob)
		}
		var compact strings.Builder
		if b, err := json.Marshal(obj); err == nil {
			compact.Write(b)
		}
		return compact.String()
	}
	return rawText(raw, blob)
}

// cutMarked cuts s to at most max bytes at a rune boundary and marks the cut.
func cutMarked(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
