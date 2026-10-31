package harvey

import (
	"encoding/json"
	"strings"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// promptrebuild.go — rebuilds the messages sent to the model from the session
// log alone (DR-0048). It is the first form of the assembler, the "replay"
// policy: apply the events in order and the message list that results at each
// model_request is the prompt. Its test is the hash: every turn request stores
// the SHA-256 of the messages it sent, and the rebuilt list must match it.

/** RebuiltRequest is one turn request and the prompt rebuilt for it.
 *
 * Fields:
 *   RequestID (string)    — the model_request event id.
 *   Stored    (string)    — the prompt_sha256 the request recorded.
 *   Rebuilt   (string)    — the hash of the rebuilt messages.
 *   Messages  ([]Message) — the rebuilt prompt.
 *
 * Example:
 *   for _, r := range reqs { if r.Stored != r.Rebuilt { fmt.Println(r.RequestID) } }
 */
type RebuiltRequest struct {
	RequestID string
	Stored    string
	Rebuilt   string
	Messages  []Message
}

/** RebuildPrompts replays a session log's events and returns, for every turn
 * request, the prompt as the log says it was. Events are applied in order:
 * session_start sets the system prompt; user_input adds the user message (the
 * text as sent when it differs from the line typed); injection adds its text;
 * tool_call and tool_result add the tool messages; model_response adds the
 * assistant message; compaction applies its rewrite. Level-debug events are
 * ignored. Ad-hoc requests are not turn prompts and are skipped.
 *
 * Parameters:
 *   events ([]Event)    — the log's events, from ReadSessionLog.
 *   opts   (RenderOptions) — how to read a blob reference.
 *
 * Returns:
 *   []RebuiltRequest — one per turn request, in log order.
 *
 * Example:
 *   events, _, _ := ReadSessionLog(path)
 *   reqs := RebuildPrompts(events, RenderOptions{Blobs: BlobDir(path)})
 */
func RebuildPrompts(events []Event, opts RenderOptions) []RebuiltRequest {
	r := newRebuilder(opts)
	for _, e := range events {
		r.apply(e)
	}
	r.finish()
	return r.requests
}

/** RebuildItems replays a session log and returns the history as it stands at
 * the end of it, each message with what the assembler needs to know about it:
 * its kind, the group (turn) it belongs to and the event that added it. The
 * log may still be open; the events up to the last complete one are used.
 *
 * Parameters:
 *   events ([]Event)       — the log's events.
 *   opts   (RenderOptions) — how to read a blob reference.
 *
 * Returns:
 *   []PromptItem — the history, oldest first.
 *
 * Example:
 *   items := RebuildItems(events, RenderOptions{Blobs: BlobDir(path)})
 */
func RebuildItems(events []Event, opts RenderOptions) []PromptItem {
	r := newRebuilder(opts)
	for _, e := range events {
		r.apply(e)
	}
	r.finish()
	return r.items
}

// rebuilder is the state of a replay of a session log.
type rebuilder struct {
	opts         RenderOptions
	items        []PromptItem
	requests     []RebuiltRequest
	turnRequests map[string]bool // model_request ids of turn requests
	inTurn       bool            // the last model request was a turn request
	modelCalls   map[string]bool // tool_call events the model made, by event id
	typed        string          // the line last logged as user_input
	calls        []anyToolCall   // tool calls of the response being applied, before their results
	callEvent    string          // the first tool_call event of calls
	held         *Message        // a response held until the next event says whether it carried tool calls
	heldEvent    string
	group        int  // the group (turn) new messages join
	closed       bool // a final reply has been given, so the next message starts a new group
}

func newRebuilder(opts RenderOptions) *rebuilder {
	return &rebuilder{opts: opts, turnRequests: map[string]bool{}, modelCalls: map[string]bool{}}
}

// msgs returns the messages of the items.
func (r *rebuilder) msgs() []Message {
	out := make([]Message, len(r.items))
	for i, it := range r.items {
		out[i] = it.Msg
	}
	return out
}

// add appends a message to the history. A message that follows a final reply
// starts the next group, so what the author added before typing a line (a
// /read, a loaded skill) travels with that line's turn.
func (r *rebuilder) add(m Message, via, event string, final bool) {
	if m.Role != "system" && r.closed && !final {
		r.group++
		r.closed = false
	}
	r.items = append(r.items, PromptItem{Msg: m, Kind: classifyItem(m, via), Group: r.group, Event: event})
	if final {
		r.closed = true
	}
}

func (r *rebuilder) flushCalls() {
	if len(r.calls) > 0 {
		r.add(Message{Role: "assistant", ToolCalls: toLibCalls(r.calls)}, "", r.callEvent, false)
		r.calls = nil
	}
}

func (r *rebuilder) flushHeld() {
	if r.held != nil {
		r.add(*r.held, "", r.heldEvent, true)
		r.held = nil
	}
}

// setSystem puts the conversation's system prompt first.
func (r *rebuilder) setSystem(text, event string) {
	it := PromptItem{Msg: Message{Role: "system", Content: text}, Kind: ItemSystem, Event: event}
	if len(r.items) > 0 && r.items[0].Msg.Role == "system" {
		r.items[0] = it
	} else {
		r.items = append([]PromptItem{it}, r.items...)
	}
}

func (r *rebuilder) finish() {
	r.flushCalls()
	r.flushHeld()
}

func (r *rebuilder) apply(e Event) {
	if e.Level == LevelDebug {
		return
	}
	opts := r.opts
	if e.Kind == KindToolCall && isModelOrigin(e) && r.inTurn {
		r.modelCalls[e.ID] = true
		r.held = nil // the response made tool calls; its text is not part of the history
	} else if !(e.Kind == KindToolResult && resultOfModelCall(e, r.modelCalls)) && e.Kind != KindToolCall {
		r.flushCalls()
		r.flushHeld()
	}
	switch e.Kind {
	case KindSessionStart:
		var d struct {
			System struct {
				Text json.RawMessage `json:"text"`
			} `json:"system"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if t := opts.contentOf(d.System.Text); t != "" {
			r.items = []PromptItem{{Msg: Message{Role: "system", Content: t}, Kind: ItemSystem, Event: e.ID}}
		}
	case KindUserInput:
		var d struct {
			Text json.RawMessage `json:"text"`
		}
		_ = json.Unmarshal(e.Data, &d)
		r.typed = opts.contentOf(d.Text)
	case KindInjection:
		var d struct {
			Source  string          `json:"source"`
			Via     string          `json:"via"`
			Role    string          `json:"role"`
			Same    bool            `json:"same"`
			Message json.RawMessage `json:"message"`
			Parts   json.RawMessage `json:"parts"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Source != "prompt" {
			return
		}
		if len(d.Parts) > 0 {
			var parts []anyllm.ContentPart
			if json.Unmarshal([]byte(opts.contentOf(d.Parts)), &parts) == nil {
				r.add(Message{Role: d.Role, Parts: parts}, d.Via, e.ID, false)
			}
			return
		}
		text := opts.contentOf(d.Message)
		if d.Same {
			text = r.typed
		}
		r.add(Message{Role: d.Role, Content: text}, d.Via, e.ID, false)
	case KindModelRequest:
		var d struct {
			Form   string          `json:"form"`
			Route  string          `json:"route"`
			Hash   string          `json:"prompt_sha256"`
			System json.RawMessage `json:"system_text"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if len(d.System) > 0 {
			r.setSystem(opts.contentOf(d.System), e.ID)
		}
		r.inTurn = d.Form == "assembled" && d.Route == ""
		if !r.inTurn {
			return
		}
		r.turnRequests[e.ID] = true
		cp := r.msgs()
		r.requests = append(r.requests, RebuiltRequest{RequestID: e.ID, Stored: d.Hash, Rebuilt: promptHash(cp), Messages: cp})
	case KindModelResponse:
		var d struct {
			Status  string          `json:"status"`
			Request string          `json:"request"`
			Text    json.RawMessage `json:"text"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Status == "ok" && r.turnRequests[d.Request] {
			r.held = &Message{Role: "assistant", Content: strings.TrimSpace(opts.contentOf(d.Text))}
			r.heldEvent = e.ID
		}
	case KindCompaction:
		var d struct {
			Reason  string            `json:"reason"`
			CallIDs []string          `json:"call_ids"`
			Ops     []json.RawMessage `json:"ops"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Ops != nil {
			r.items = applyRewriteOps(r.items, d.Ops, opts, e.ID)
		}
		if d.Reason == "tool_result" && len(d.CallIDs) > 0 {
			msgs := r.msgs()
			for i, m := range msgs {
				if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == d.CallIDs[0] {
					compactToolRound(msgs, i) // the same rewrite the tool loop made
					for j := range r.items {
						r.items[j].Msg = msgs[j]
					}
					break
				}
			}
		}
	case KindToolCall:
		if !isModelOrigin(e) || !r.inTurn {
			return
		}
		var d struct {
			Tool string          `json:"tool"`
			ID   string          `json:"call_id"`
			Args json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if len(r.calls) == 0 {
			r.callEvent = e.ID
		}
		r.calls = append(r.calls, anyToolCall{id: d.ID, name: d.Tool, args: opts.contentOf(d.Args)})
	case KindToolResult:
		if !resultOfModelCall(e, r.modelCalls) {
			return
		}
		r.flushCalls()
		var d struct {
			ID     string          `json:"call_id"`
			Output json.RawMessage `json:"output"`
		}
		_ = json.Unmarshal(e.Data, &d)
		r.add(Message{Role: "tool", Content: opts.contentOf(d.Output), ToolCallID: d.ID}, "", e.ID, false)
	}
}

// applyRewriteOps builds the new history from the old one and the ops a
// rewrite event recorded: [from, to] keeps a range of the old list; an object
// is a new message, which belongs to the group of the message before it.
func applyRewriteOps(prev []PromptItem, ops []json.RawMessage, opts RenderOptions, event string) []PromptItem {
	var next []PromptItem
	for _, raw := range ops {
		var rg []int
		if json.Unmarshal(raw, &rg) == nil && len(rg) == 2 {
			if rg[0] >= 0 && rg[1] <= len(prev) && rg[0] <= rg[1] {
				next = append(next, prev[rg[0]:rg[1]]...)
			}
			continue
		}
		var m struct {
			Role       string               `json:"role"`
			Message    json.RawMessage      `json:"message"`
			ToolCallID string               `json:"tool_call_id"`
			ToolCalls  []anyllm.ToolCall    `json:"tool_calls"`
			Parts      []anyllm.ContentPart `json:"parts"`
		}
		if json.Unmarshal(raw, &m) == nil {
			msg := Message{Role: m.Role, Content: opts.contentOf(m.Message), ToolCallID: m.ToolCallID, ToolCalls: m.ToolCalls, Parts: m.Parts}
			g := 0
			if len(next) > 0 {
				g = next[len(next)-1].Group
			}
			next = append(next, PromptItem{Msg: msg, Kind: classifyItem(msg, ""), Group: g, Event: event})
		}
	}
	return next
}

// anyToolCall is a tool call as the log holds it.
type anyToolCall struct{ id, name, args string }

// toLibCalls converts logged tool calls to the message form.
func toLibCalls(cs []anyToolCall) []anyllm.ToolCall {
	out := make([]anyllm.ToolCall, len(cs))
	for i, c := range cs {
		out[i] = anyllm.ToolCall{ID: c.id, Type: "function", Function: anyllm.FunctionCall{Name: c.name, Arguments: c.args}}
	}
	return out
}

// isModelOrigin reports whether a tool_call event is the model's: a call the
// author made (!cmd, a pipeline step, /kb) is not a message in the history.
func isModelOrigin(e Event) bool {
	var d struct {
		Origin string `json:"origin"`
	}
	_ = json.Unmarshal(e.Data, &d)
	return d.Origin == "model"
}

// resultOfModelCall reports whether a tool_result answers a call the model made.
func resultOfModelCall(e Event, modelCalls map[string]bool) bool {
	var d struct {
		Call string `json:"call"`
	}
	_ = json.Unmarshal(e.Data, &d)
	return modelCalls[d.Call]
}
