package harvey

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsdoiel/fountain"
)

// sessionrender.go renders a session log as a Fountain screenplay (plan item
// L4, DR-0036). The Fountain file is a presentation of the log, and the log is
// the record: the same events always give the same file, given the same time
// zone. Unlike the prompt renderer of plan item L7, which must not read the
// timestamps (DR-0039), this one needs them for scene headings and notes.

/** RenderOptions holds what a rendering needs that is not in the log.
 *
 * Fields:
 *   Zone  (*time.Location)               — the zone event times are shown in; nil is the local zone.
 *   Blobs (func(string) (string, error)) — reads a spilled payload by its hash; nil shows the preview instead.
 *
 * Example:
 *   RenderOptions{Zone: time.UTC, Blobs: BlobDir("agents/sessions/s1.jsonl")}
 */
type RenderOptions struct {
	Zone  *time.Location
	Blobs func(hash string) (string, error)
}

/** BlobDir returns a reader for the payloads that go with a session log: the
 * files in <log stem>.blobs next to it.
 *
 * Parameters:
 *   logPath (string) — path of the .jsonl file.
 *
 * Returns:
 *   func(string) (string, error) — reads the blob with the given hash.
 *
 * Example:
 *   opts := RenderOptions{Blobs: BlobDir(path)}
 */
func BlobDir(logPath string) func(string) (string, error) {
	dir := strings.TrimSuffix(logPath, filepath.Ext(logPath)) + ".blobs"
	return func(hash string) (string, error) {
		if hash == "" || strings.ContainsAny(hash, `/\`) {
			return "", fmt.Errorf("bad blob name %q", hash)
		}
		b, err := os.ReadFile(filepath.Join(dir, hash))
		return string(b), err
	}
}

// contentOf decodes a Content field and returns its text: inline, read from
// its blob, or the preview when the blob cannot be read.
func (o RenderOptions) contentOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var c Content
	if err := json.Unmarshal(raw, &c); err != nil {
		return ""
	}
	if c.Blob == "" {
		return c.Text
	}
	if o.Blobs != nil {
		if b, err := o.Blobs(c.Blob); err == nil {
			return b
		}
	}
	return c.Preview
}

type renderResponse struct {
	Status string          `json:"status"`
	Text   json.RawMessage `json:"text"`
	Error  json.RawMessage `json:"error"`
	Usage  *struct {
		Prompt     int `json:"prompt"`
		Completion int `json:"completion"`
	} `json:"usage"`
	MS int64 `json:"ms"`
}

type renderRequest struct {
	Form    string `json:"form"`
	Purpose string `json:"purpose"`
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Source  struct {
		Path      string `json:"path"`
		Chunk     int    `json:"chunk"`
		Of        int    `json:"of"`
		Chunks    int    `json:"chunks"`
		DocType   string `json:"doc_type"`
		ChunkSize int    `json:"chunk_size"`
	} `json:"source"`
}

type renderInjection struct {
	Source      string          `json:"source"`
	Origin      string          `json:"origin"`
	Ref         string          `json:"ref"`
	Description json.RawMessage `json:"description"`
	Text        json.RawMessage `json:"text"`
}

type renderTool struct {
	Tool   string `json:"tool"`
	Origin string `json:"origin"`
	Args   json.RawMessage
	CallID string `json:"call_id"`
}

/** RenderFountain renders the events of a session log as Fountain source. The
 * scene vocabulary is the live recorder's: a title page, FADE IN, one chat
 * scene per turn (the author's line, the forwarding line, the tool notes, the
 * reply, the stats line), a note for a model switch, a shell scene for each
 * !cmd, and THE END. Debug events are left out. A log with no session_end is
 * marked as ended without a clean close. A turn that failed shows the
 * failure; the recorder showed nothing for it. Tool notes come from the log,
 * so a turn whose tool round was compacted away in memory still has them.
 *
 * Parameters:
 *   events []Event        — the events of one session, in order.
 *   opts   RenderOptions  — the time zone and the blob reader.
 *
 * Returns:
 *   []byte — the Fountain text; nil for an empty log.
 *
 * Example:
 *   events, _, _ := ReadSessionLog(path)
 *   spmd := RenderFountain(events, RenderOptions{Blobs: BlobDir(path)})
 */
func RenderFountain(events []Event, opts RenderOptions) []byte {
	if len(events) == 0 || events[0].Kind != KindSessionStart {
		return nil
	}
	zone := opts.Zone
	if zone == nil {
		zone = time.Local
	}
	at := func(e Event) string {
		t, err := time.Parse(time.RFC3339, e.T)
		if err != nil {
			return e.T
		}
		return t.In(zone).Format("2006-01-02 15:04:05")
	}

	var start struct {
		User      string `json:"user"`
		Workspace string `json:"workspace"`
		Backend   string `json:"backend"`
		Model     string `json:"model"`
	}
	_ = json.Unmarshal(events[0].Data, &start)
	user := start.User
	if user == "" {
		user = "OPERATOR"
	}
	label := start.Backend + " (" + start.Model + ")"
	modelName := extractModelName(label)

	var buf bytes.Buffer
	for _, elem := range []*fountain.Element{
		{Type: fountain.TitlePageType, Name: "Title", Content: "Harvey Session"},
		{Type: fountain.TitlePageType, Name: "Credit", Content: "Recorded by Harvey"},
		{Type: fountain.TitlePageType, Name: "Author", Content: user},
		{Type: fountain.TitlePageType, Name: "Date", Content: at(events[0])},
		{Type: fountain.TitlePageType, Name: "Draft date", Content: strings.SplitN(at(events[0]), " ", 2)[0]},
		{Type: fountain.TitlePageType, Name: "Model", Content: modelName + " (" + start.Backend + ")"},
		{Type: fountain.TitlePageType, Name: "Backend", Content: start.Backend},
	} {
		fmt.Fprintln(&buf, fountainSrc(elem))
	}
	fmt.Fprintln(&buf)
	out := fountainOut{&buf}
	out.transition("FADE IN:")

	ended := false
	for i := 1; i < len(events); i++ {
		e := events[i]
		if e.Level == LevelDebug {
			continue
		}
		switch e.Kind {
		case KindModelSwitch:
			var d struct {
				To ModelRef `json:"to"`
			}
			_ = json.Unmarshal(e.Data, &d)
			fmt.Fprintln(&buf, "\n[[model switch: "+d.To.Model+" ("+d.To.Backend+") at "+at(e)+"]]")
			label = d.To.Backend + " (" + d.To.Model + ")"
			modelName = extractModelName(d.To.Model)
		case KindSessionEnd:
			ended = true
			out.transition("THE END.")
		case KindToolCall:
			var tc renderTool
			_ = json.Unmarshal(e.Data, &tc)
			if tc.Origin == "user" && tc.Tool == "shell" {
				renderShell(out, opts, events, i, user, at)
			}
			if tc.Origin == "user" && tc.Tool == "pipeline" {
				renderPipelineStep(out, opts, events, i, user, start.Workspace, label, at)
			}
		case KindInjection:
			var inj renderInjection
			_ = json.Unmarshal(e.Data, &inj)
			if inj.Source == "skill" {
				renderSkill(out, opts, e, inj, at)
			}
		case KindModelRequest:
			if isChunkStart(e) {
				i = renderChunkAnalysis(out, opts, events, i, at) - 1
			}
		case KindUserInput:
			end := turnEnd(events, i)
			renderTurn(out, opts, events[i:end], user, start.Workspace, modelName, label, at)
			i = end - 1
		}
	}
	if !ended {
		out.note("session ended without a clean close")
		out.transition("THE END.")
	}
	return buf.Bytes()
}

// turnEnd returns the index of the first event after the turn that starts at
// events[i]: the next user_input, the author's shell command, a model switch
// or the end of the session.
func turnEnd(events []Event, i int) int {
	for j := i + 1; j < len(events); j++ {
		switch events[j].Kind {
		case KindUserInput, KindModelSwitch, KindSessionEnd:
			return j
		case KindInjection:
			// An injection the author asked for sits between turns; one made
			// for the turn (a trigger match, a recall) stays inside it.
			var inj renderInjection
			_ = json.Unmarshal(events[j].Data, &inj)
			if inj.Origin == "user" {
				return j
			}
		case KindToolCall:
			var tc renderTool
			_ = json.Unmarshal(events[j].Data, &tc)
			if tc.Origin == "user" {
				return j
			}
		}
	}
	return len(events)
}

func renderTurn(out fountainOut, opts RenderOptions, turn []Event, user, workspace, modelName, label string, at func(Event) string) {
	var input struct {
		Text  json.RawMessage `json:"text"`
		Route string          `json:"route"`
	}
	_ = json.Unmarshal(turn[0].Data, &input)

	// A chunk analysis inside the turn (the model asked for one, or an oversized
	// file was chunked) was written by the recorder when it ran, before the
	// turn's own scene; render those first and set their events aside.
	consumed := map[int]bool{}
	adhoc := map[string]bool{}
	for k := 1; k < len(turn); k++ {
		if turn[k].Level == LevelDebug || turn[k].Kind != KindModelRequest {
			continue
		}
		var rq renderRequest
		_ = json.Unmarshal(turn[k].Data, &rq)
		if rq.Form == "adhoc" {
			adhoc[turn[k].ID] = true
		}
		if isChunkStart(turn[k]) {
			next := renderChunkAnalysis(out, opts, turn, k, at)
			for m := k; m < next; m++ {
				consumed[m] = true
			}
			k = next - 1
		}
	}

	var last *renderResponse
	var failure *renderResponse
	var skills []renderInjection
	var skillEvents []Event
	var offers []renderOffer
	type call struct {
		id, name, args, status string
	}
	var calls []call
	results := map[string]string{}
	for k, e := range turn {
		if k == 0 || consumed[k] || e.Level == LevelDebug {
			continue
		}
		switch e.Kind {
		case KindModelResponse:
			var r renderResponse
			_ = json.Unmarshal(e.Data, &r)
			var link struct {
				Request string `json:"request"`
			}
			_ = json.Unmarshal(e.Data, &link)
			if adhoc[link.Request] {
				continue // an answer to a side request is not the turn's reply
			}
			if r.Status == "ok" {
				last = &r
			} else {
				failure = &r
			}
		case KindInjection:
			var inj renderInjection
			_ = json.Unmarshal(e.Data, &inj)
			if inj.Source == "skill" {
				skills = append(skills, inj)
				skillEvents = append(skillEvents, e)
			}
		case KindPermission:
			var o renderOffer
			_ = json.Unmarshal(e.Data, &o)
			if o.Choice != "" { // an answer to an offer; rule decisions carry no choice
				o.event = e
				offers = append(offers, o)
			}
		case KindToolCall:
			var tc renderTool
			_ = json.Unmarshal(e.Data, &tc)
			calls = append(calls, call{id: e.ID, name: tc.Tool, args: opts.contentOf(tc.Args)})
		case KindToolResult:
			var tr struct {
				Call   string          `json:"call"`
				Status string          `json:"status"`
				Output json.RawMessage `json:"output"`
			}
			_ = json.Unmarshal(e.Data, &tr)
			status := "ok"
			if tr.Status != "ok" {
				first := strings.SplitN(strings.TrimSpace(opts.contentOf(tr.Output)), "\n", 2)[0]
				if strings.HasPrefix(first, "error:") {
					status = first
				} else {
					status = "error: " + strings.TrimSpace(first)
				}
			}
			results[tr.Call] = status
		}
	}

	// The recorder wrote a skill's scene when it was loaded, which is before the
	// turn's own scene.
	for k, inj := range skills {
		renderSkill(out, opts, skillEvents[k], inj, at)
	}
	if input.Route != "" {
		endpoint := strings.ToUpper(input.Route)
		out.sceneHeading(fmt.Sprintf("EXT. %s AND %s %s", endpoint, user, at(turn[0])))
		out.action(fmt.Sprintf("Harvey routing to %s. Workspace: %s.", endpoint, workspace))
		out.dialogue(user, "", opts.contentOf(input.Text))
		out.dialogue("HARVEY", "", fmt.Sprintf("Forwarding to %s.", endpoint))
		switch {
		case last != nil:
			out.dialogue(endpoint, "", opts.contentOf(last.Text))
		case failure != nil && failure.Status == "cancelled":
			out.note("model response cancelled")
		case failure != nil:
			out.note("model error: " + strings.TrimSpace(opts.contentOf(failure.Error)))
		}
		for _, o := range offers {
			renderOffer1(out, opts, user, o, at)
		}
		return
	}
	out.sceneHeading(fmt.Sprintf("INT. HARVEY AND %s TALKING %s", user, at(turn[0])))
	out.action(fmt.Sprintf("Harvey and %s are in chat mode. Model: %s. Workspace: %s.", user, modelName, workspace))
	out.dialogue(user, "", opts.contentOf(input.Text))
	out.dialogue("HARVEY", "", fmt.Sprintf("Forwarding to %s.", modelName))
	for _, c := range calls {
		note := toolNote(ToolCallRecord{Name: c.name, Args: c.args, Result: results[c.id]})
		out.note(note)
	}
	switch {
	case last != nil:
		out.dialogue(modelName, "", opts.contentOf(last.Text))
		stats := ChatStats{Elapsed: time.Duration(last.MS) * time.Millisecond}
		if last.Usage != nil {
			stats.PromptTokens, stats.ReplyTokens = last.Usage.Prompt, last.Usage.Completion
		}
		if stats.ReplyTokens > 0 && stats.Elapsed > 0 {
			stats.TokensPerSec = float64(stats.ReplyTokens) / stats.Elapsed.Seconds()
		}
		if text := stats.FormatWithModels([]string{label}); text != "" {
			out.action(text)
		}
	case failure != nil && failure.Status == "cancelled":
		out.note("model response cancelled")
	case failure != nil:
		out.note("model error: " + strings.TrimSpace(opts.contentOf(failure.Error)))
	}
	// The offers to write what the reply proposed come after the turn's scene.
	for _, o := range offers {
		renderOffer1(out, opts, user, o, at)
	}
}

// renderOffer is the permission event of the author's answer to an offer.
type renderOffer struct {
	Action  string          `json:"action"`
	Target  json.RawMessage `json:"target"`
	Choice  string          `json:"choice"`
	Outcome json.RawMessage `json:"outcome"`
	Offer   *struct {
		Index int `json:"index"`
		Of    int `json:"of"`
	} `json:"offer"`
	event Event
}

// renderOffer1 writes one offer: the first of a reply's tagged blocks opens the
// agent scene; each offer is the proposal, the author's answer and a note of
// what came of it.
func renderOffer1(out fountainOut, opts RenderOptions, user string, o renderOffer, at func(Event) string) {
	target := opts.contentOf(o.Target)
	if o.Offer != nil && o.Offer.Index == 1 {
		out.sceneHeading("INT. AGENT MODE " + at(o.event))
		out.dialogue("HARVEY", "", fmt.Sprintf("Harvey proposes to write %d file(s).", o.Offer.Of))
	}
	proposal := strings.ToUpper(o.Action[:1]) + o.Action[1:] + " " + target
	switch o.Action {
	case "write":
		proposal = fmt.Sprintf("Write %s?", target)
	case "run":
		proposal = fmt.Sprintf("Run: %s?", target)
	}
	out.dialogue("HARVEY", "", proposal)
	out.dialogue(user, "", o.Choice)
	out.note(fmt.Sprintf("%s: %s — %s", o.Action, target, opts.contentOf(o.Outcome)))
}

// toolNote formats a tool call as the recorder does, without the character.
func toolNote(tc ToolCallRecord) string { return formatToolCallNote(tc) }

func renderShell(out fountainOut, opts RenderOptions, events []Event, i int, user string, at func(Event) string) {
	call := events[i]
	var d struct {
		Args struct {
			Cmd json.RawMessage `json:"cmd"`
		} `json:"args"`
	}
	_ = json.Unmarshal(call.Data, &d)
	cmd := opts.contentOf(d.Args.Cmd)
	var res struct {
		Status string          `json:"status"`
		Exit   *int            `json:"exit"`
		Output json.RawMessage `json:"output"`
	}
	found := false
	for j := i + 1; j < len(events); j++ {
		if events[j].Kind != KindToolResult {
			continue
		}
		var r struct {
			Call string `json:"call"`
		}
		_ = json.Unmarshal(events[j].Data, &r)
		if r.Call == call.ID {
			_ = json.Unmarshal(events[j].Data, &res)
			found = true
			break
		}
	}
	out.sceneHeading("INT. SHELL " + at(call))
	out.dialogue(user, "", "! "+cmd)
	if !found {
		out.note("shell: " + cmd + " — no result recorded")
		return
	}
	if text := opts.contentOf(res.Output); text != "" {
		out.dialogue("SHELL", "", text)
	}
	switch {
	case res.Status == "cancelled":
		out.note("shell: " + cmd + " — cancelled")
	case res.Exit != nil:
		out.note(fmt.Sprintf("shell: %s — exit %d", cmd, *res.Exit))
	default:
		out.note("shell: " + cmd + " — did not run")
	}
}

// renderSkill writes the scene for a skill load: its heading, the line saying
// harvey executes it, its description, and its body spoken by the skill.
func renderSkill(out fountainOut, opts RenderOptions, e Event, inj renderInjection, at func(Event) string) {
	char := strings.ToUpper(inj.Ref)
	out.sceneHeading(fmt.Sprintf("INT. SKILL %s %s", char, at(e)))
	out.action(fmt.Sprintf("Harvey executes the %s skill.", inj.Ref))
	if desc := opts.contentOf(inj.Description); desc != "" {
		out.action(desc)
	}
	if body := opts.contentOf(inj.Text); body != "" {
		out.dialogue(char, "", body)
	}
}

// isChunkStart reports whether e is the request for the first chunk of an analysis.
func isChunkStart(e Event) bool {
	if e.Kind != KindModelRequest || e.Level == LevelDebug {
		return false
	}
	var rq renderRequest
	_ = json.Unmarshal(e.Data, &rq)
	return rq.Form == "adhoc" && rq.Purpose == "chunk" && rq.Source.Chunk == 1
}

// responseTo finds the model_response that answers the request with this id.
func responseTo(events []Event, from int, id string) *renderResponse {
	for j := from; j < len(events); j++ {
		if events[j].Kind != KindModelResponse {
			continue
		}
		var link struct {
			Request string `json:"request"`
		}
		_ = json.Unmarshal(events[j].Data, &link)
		if link.Request == id {
			var r renderResponse
			_ = json.Unmarshal(events[j].Data, &r)
			return &r
		}
	}
	return nil
}

// renderChunkAnalysis writes the scene for a chunk analysis that starts at
// events[i]: the file, the model, each chunk's answer with how it ended, and the
// synthesis. A chunk that failed shows the failure as the recorder did. It
// returns the index of the first event after the analysis.
func renderChunkAnalysis(out fountainOut, opts RenderOptions, events []Event, i int, at func(Event) string) int {
	var first renderRequest
	_ = json.Unmarshal(events[i].Data, &first)
	path, total := first.Source.Path, first.Source.Of
	model := first.Model
	label := "prose"
	boundary := "paragraph"
	if first.Source.DocType == "source" {
		label, boundary = "source", "block"
	}
	out.sceneHeading("INT. CHUNK ANALYSIS — " + path + " " + at(events[i]))
	out.action(fmt.Sprintf("Harvey processes %s as %d chunks of type %s.", path, total, label))
	out.note(fmt.Sprintf("chunk: file=%s, chunks=%d, model=%s, boundary=%s, chunk-size=%d",
		path, total, model, boundary, first.Source.ChunkSize))

	k := i
	for ; k < len(events); k++ {
		e := events[k]
		if e.Level == LevelDebug {
			continue
		}
		if e.Kind == KindModelResponse || e.Kind == KindPermission || e.Kind == KindFault {
			continue // answers are looked up with their requests
		}
		if e.Kind != KindModelRequest {
			break
		}
		var rq renderRequest
		_ = json.Unmarshal(e.Data, &rq)
		if rq.Form != "adhoc" || rq.Source.Path != path {
			break
		}
		resp := responseTo(events, k+1, e.ID)
		switch rq.Purpose {
		case "chunk":
			result, outcome := "", "ok"
			if resp != nil && resp.Status == "ok" {
				result = opts.contentOf(resp.Text)
			} else if resp != nil {
				msg := strings.TrimSpace(opts.contentOf(resp.Error))
				result = fmt.Sprintf("[chunk %d failed: %s]", rq.Source.Chunk, msg)
				outcome = "error: " + firstLine(msg)
			}
			out.dialogue(extractModelName(rq.Model), fmt.Sprintf("chunk %d/%d", rq.Source.Chunk, total), result)
			out.note(fmt.Sprintf("chunk-result: %d/%d — %s", rq.Source.Chunk, total, outcome))
		case "synthesis":
			text, outcome := "", "ok"
			if resp != nil && resp.Status == "ok" {
				text = opts.contentOf(resp.Text)
			} else if resp != nil {
				outcome = "error: " + firstLine(strings.TrimSpace(opts.contentOf(resp.Error)))
			}
			out.dialogue("HARVEY", "", "Synthesizing results.")
			out.dialogue(extractModelName(rq.Model), "synthesis", text)
			out.note(fmt.Sprintf("synthesis: model=%s — %s", rq.Model, outcome))
			return k + 1
		default:
			return k
		}
	}
	return k
}

// renderPipelineStep writes the scene for one step of a /pipeline run: the step's
// heading, the model line, the hidden-instruction note, the author's prompt, the
// response and its confidence; then CUT TO the next step, or CUT BACK to the chat
// scene, with a note if the step failed or the response if the last one passed.
func renderPipelineStep(out fountainOut, opts RenderOptions, events []Event, i int, user, workspace, chatLabel string, at func(Event) string) {
	call := events[i]
	var d struct {
		Args struct {
			File      string          `json:"file"`
			Step      int             `json:"step"`
			Of        int             `json:"of"`
			Threshold float64         `json:"threshold"`
			Files     []string        `json:"files"`
			Model     string          `json:"model"`
			Body      json.RawMessage `json:"body"`
		} `json:"args"`
	}
	_ = json.Unmarshal(call.Data, &d)
	a := d.Args
	var res struct {
		Status     string          `json:"status"`
		Confidence float64         `json:"confidence"`
		Output     json.RawMessage `json:"output"`
	}
	for j := i + 1; j < len(events); j++ {
		if events[j].Kind != KindToolResult {
			continue
		}
		var r struct {
			Call string `json:"call"`
		}
		_ = json.Unmarshal(events[j].Data, &r)
		if r.Call == call.ID {
			_ = json.Unmarshal(events[j].Data, &res)
			break
		}
	}
	out.sceneHeading(fmt.Sprintf("INT. PIPELINE %s STEP %d/%d %s", strings.ToUpper(a.File), a.Step, a.Of, at(call)))
	out.action(fmt.Sprintf("%s executes /pipeline %.0f%% %s.\nModel: %s. Workspace: %s.",
		user, a.Threshold*100, strings.Join(a.Files, " "), a.Model, workspace))
	out.note("hidden confidence instruction appended")
	out.dialogue(user, "", opts.contentOf(a.Body))
	response := opts.contentOf(res.Output)
	out.dialogue("HARVEY", "", response)
	failed := res.Status != "ok"
	if failed {
		out.note(fmt.Sprintf("confidence: %.2f — threshold not met (%.2f)", res.Confidence, a.Threshold))
		out.transition("CUT BACK TO:")
		out.sceneHeading(fmt.Sprintf("INT. HARVEY AND %s TALKING %s", user, at(call)))
		out.action(fmt.Sprintf("Harvey and %s are in chat mode. Model: %s. Workspace: %s.", user, chatLabel, workspace))
		out.note(fmt.Sprintf("pipeline stopped at step %d/%d: confidence %.2f below threshold %.2f",
			a.Step, a.Of, res.Confidence, a.Threshold))
		return
	}
	out.note(fmt.Sprintf("confidence: %.2f — threshold met", res.Confidence))
	if a.Step < a.Of {
		out.transition("CUT TO:")
		return
	}
	out.transition("CUT BACK TO:")
	out.sceneHeading(fmt.Sprintf("INT. HARVEY AND %s TALKING %s", user, at(call)))
	out.action(fmt.Sprintf("Harvey and %s are in chat mode. Model: %s. Workspace: %s.", user, chatLabel, workspace))
	out.dialogue("HARVEY", "", response)
}
