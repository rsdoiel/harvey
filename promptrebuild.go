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
	var msgs []Message
	var out []RebuiltRequest
	turnRequests := map[string]bool{} // model_request ids of turn requests
	inTurn := false                   // the last model request was a turn request
	modelCalls := map[string]bool{}   // tool_call events the model made, by event id
	typed := ""                       // the line last logged as user_input
	var calls []anyToolCall           // tool calls of the response being applied, before their results
	flushCalls := func() {
		if len(calls) > 0 {
			msgs = append(msgs, Message{Role: "assistant", ToolCalls: toLibCalls(calls)})
			calls = nil
		}
	}
	var held *Message // a response held until the next event says whether it carried tool calls
	flushHeld := func() {
		if held != nil {
			msgs = append(msgs, *held)
			held = nil
		}
	}
	for _, e := range events {
		if e.Level == LevelDebug {
			continue
		}
		if e.Kind == KindToolCall && isModelOrigin(e) && inTurn {
			modelCalls[e.ID] = true
			held = nil // the response made tool calls; its text is not part of the history
		} else if !(e.Kind == KindToolResult && resultOfModelCall(e, modelCalls)) && e.Kind != KindToolCall {
			flushCalls()
			flushHeld()
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
				msgs = []Message{{Role: "system", Content: t}}
			}
		case KindUserInput:
			var d struct {
				Text json.RawMessage `json:"text"`
			}
			_ = json.Unmarshal(e.Data, &d)
			typed = opts.contentOf(d.Text)
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
				continue
			}
			if len(d.Parts) > 0 {
				var parts []anyllm.ContentPart
				if json.Unmarshal([]byte(opts.contentOf(d.Parts)), &parts) == nil {
					msgs = append(msgs, Message{Role: d.Role, Parts: parts})
				}
				continue
			}
			text := opts.contentOf(d.Message)
			if d.Same {
				text = typed
			}
			msgs = append(msgs, Message{Role: d.Role, Content: text})
		case KindModelRequest:
			var d struct {
				Form   string          `json:"form"`
				Hash   string          `json:"prompt_sha256"`
				System json.RawMessage `json:"system_text"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if len(d.System) > 0 {
				sys := Message{Role: "system", Content: opts.contentOf(d.System)}
				if len(msgs) > 0 && msgs[0].Role == "system" {
					msgs[0] = sys
				} else {
					msgs = append([]Message{sys}, msgs...)
				}
			}
			inTurn = d.Form == "assembled"
			if !inTurn {
				continue
			}
			turnRequests[e.ID] = true
			cp := append([]Message(nil), msgs...)
			out = append(out, RebuiltRequest{RequestID: e.ID, Stored: d.Hash, Rebuilt: promptHash(cp), Messages: cp})
		case KindModelResponse:
			var d struct {
				Status  string          `json:"status"`
				Request string          `json:"request"`
				Text    json.RawMessage `json:"text"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if d.Status == "ok" && turnRequests[d.Request] {
				held = &Message{Role: "assistant", Content: strings.TrimSpace(opts.contentOf(d.Text))}
			}
		case KindCompaction:
			var d struct {
				Reason  string            `json:"reason"`
				CallIDs []string          `json:"call_ids"`
				Ops     []json.RawMessage `json:"ops"`
			}
			_ = json.Unmarshal(e.Data, &d)
			if d.Ops != nil {
				msgs = applyRewriteOps(msgs, d.Ops, opts)
			}
			if d.Reason == "tool_result" && len(d.CallIDs) > 0 {
				for i, m := range msgs {
					if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == d.CallIDs[0] {
						compactToolRound(msgs, i) // the same rewrite the tool loop made
						break
					}
				}
			}
		case KindToolCall:
			if !isModelOrigin(e) || !inTurn {
				continue
			}
			var d struct {
				Tool string          `json:"tool"`
				ID   string          `json:"call_id"`
				Args json.RawMessage `json:"args"`
			}
			_ = json.Unmarshal(e.Data, &d)
			calls = append(calls, anyToolCall{id: d.ID, name: d.Tool, args: opts.contentOf(d.Args)})
		case KindToolResult:
			if !resultOfModelCall(e, modelCalls) {
				continue
			}
			flushCalls()
			var d struct {
				ID     string          `json:"call_id"`
				Output json.RawMessage `json:"output"`
			}
			_ = json.Unmarshal(e.Data, &d)
			msgs = append(msgs, Message{Role: "tool", Content: opts.contentOf(d.Output), ToolCallID: d.ID})
		}
	}
	flushCalls()
	flushHeld()
	return out
}

// applyRewriteOps builds the new history from the old one and the ops a
// rewrite event recorded: [from, to] keeps a range of the old list; an object
// is a new message.
func applyRewriteOps(prev []Message, ops []json.RawMessage, opts RenderOptions) []Message {
	var next []Message
	for _, raw := range ops {
		var r []int
		if json.Unmarshal(raw, &r) == nil && len(r) == 2 {
			if r[0] >= 0 && r[1] <= len(prev) && r[0] <= r[1] {
				next = append(next, prev[r[0]:r[1]]...)
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
			next = append(next, Message{Role: m.Role, Content: opts.contentOf(m.Message), ToolCallID: m.ToolCallID, ToolCalls: m.ToolCalls, Parts: m.Parts})
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
