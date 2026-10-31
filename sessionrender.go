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
		case KindInjection:
			var inj renderInjection
			_ = json.Unmarshal(e.Data, &inj)
			if inj.Source == "skill" {
				renderSkill(out, opts, e, inj, at)
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
		Text json.RawMessage `json:"text"`
	}
	_ = json.Unmarshal(turn[0].Data, &input)

	var last *renderResponse
	var failure *renderResponse
	var skills []renderInjection
	var skillEvents []Event
	type call struct {
		id, name, args, status string
	}
	var calls []call
	results := map[string]string{}
	for _, e := range turn[1:] {
		if e.Level == LevelDebug {
			continue
		}
		switch e.Kind {
		case KindModelResponse:
			var r renderResponse
			_ = json.Unmarshal(e.Data, &r)
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
