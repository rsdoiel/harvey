package harvey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// Wiring of the session log (sessionlog.go) into Agent, item L3 of
// plans/session-log-plan.md. Every helper is safe to call with no log open, so
// a call site is one line and a test agent needs no setup.

/** openSessionLog opens the session log beside the Fountain file at recPath,
 * with the same stem (DR-0038), and makes it the agent's log. A failure is
 * reported once and the session goes on without a log.
 *
 * Parameters:
 *   recPath  (string) — path of the .spmd file being recorded.
 *   replayOf (string) — path of the session this one replays; "" otherwise.
 *   out      (io.Writer) — where a failure is reported.
 *
 * Example:
 *   a.openSessionLog(recPath, "", out)
 */
func (a *Agent) openSessionLog(recPath, replayOf string, out io.Writer) {
	stem := strings.TrimSuffix(filepath.Base(recPath), filepath.Ext(recPath))
	clientName := "none"
	if a.Client != nil {
		clientName = a.Client.Name()
	}
	sysID, system := a.systemPromptVersion()
	root := ""
	if a.Workspace != nil {
		root = a.Workspace.Root
	}
	opts := SessionLogOptions{
		Dir:     filepath.Dir(recPath),
		Session: stem,
		Debug:   a.Config.Debug,
		Start: SessionStart{
			HarveyVersion: Version,
			User:          os.Getenv("USER"),
			Workspace:     root,
			Backend:       backendFromClientName(clientName),
			Model:         modelFromClientName(clientName),
			System:        SystemPrompt{ID: sysID, Text: system},
			Window:        Window{N: a.effectiveContextLimit(), Source: "unknown"},
		},
	}
	if replayOf != "" {
		opts.Start.ReplayOf = strings.TrimSuffix(filepath.Base(replayOf), filepath.Ext(replayOf))
	}
	l, err := OpenSessionLog(opts)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Session log failed: %v\n", err)
		return
	}
	a.SessionLog = l
	a.logTurn, a.logLastID, a.logFirstDialogID = 0, "", ""
	a.logSystemID = sysID
	a.logModel = ModelRef{Backend: backendFromClientName(clientName), Model: modelFromClientName(clientName)}
}

/** closeSessionLog writes session_end and closes the agent's log; it does
 * nothing when no log is open.
 *
 * Parameters:
 *   reason (string) — quit, crash_recovered or error.
 *
 * Example:
 *   defer a.closeSessionLog("quit")
 */
func (a *Agent) closeSessionLog(reason string) {
	if a.SessionLog == nil {
		return
	}
	if err := a.SessionLog.Close(reason); err != nil {
		fmt.Fprintf(os.Stderr, "harvey: session log close: %v\n", err)
	}
	a.SessionLog = nil
}

// logEvent appends an event and remembers its id. A failed write is reported
// once on stderr and the session goes on: a broken log must not stop the work,
// and the gap it leaves is detectable on read.
func (a *Agent) logEvent(kind EventKind, level EventLevel, data any) string {
	if a.SessionLog == nil {
		return ""
	}
	id, err := a.SessionLog.Append(kind, level, data)
	if err != nil {
		if !a.sessionLogWarned {
			a.sessionLogWarned = true
			fmt.Fprintf(os.Stderr, "harvey: session log: %v\n", err)
		}
		return ""
	}
	if id != "" {
		a.logLastID = id
	}
	return id
}

// logContent scrubs text for the log; with no log it returns the text as it is.
func (a *Agent) logContent(text string) any {
	if a.SessionLog == nil {
		return text
	}
	c, err := a.SessionLog.Content(text)
	if err != nil {
		if !a.sessionLogWarned {
			a.sessionLogWarned = true
			fmt.Fprintf(os.Stderr, "harvey: session log: %v\n", err)
		}
		return "[content not logged]"
	}
	return c
}

/** logUserInput records a line the author typed for the model as a user_input
 * event and starts a new logged turn. It is not called for slash commands or
 * shell lines, which are command and tool events.
 *
 * Parameters:
 *   input (string) — the line as typed.
 *
 * Example:
 *   a.logUserInput("explain this function")
 */
func (a *Agent) logUserInput(input string) { a.logUserInputVia(input, "") }

// logUserInputVia is logUserInput for a line sent to a named route; the route
// is recorded so the Fountain scene can say where the line went.
func (a *Agent) logUserInputVia(input, route string) { a.logUserInputOrigin(input, route, "") }

// logUserInputOrigin is logUserInputVia with the origin of the line; "" means
// the author typed it and "replay" means it was re-sent from an earlier session.
func (a *Agent) logUserInputOrigin(input, route, origin string) {
	if a.SessionLog == nil {
		return
	}
	a.logTurn++
	a.logInputText = input
	data := map[string]any{"text": a.logContent(input)}
	if route != "" {
		data["route"] = route
	}
	if origin != "" {
		data["origin"] = origin
	}
	id := a.logEvent(KindUserInput, LevelInfo, data)
	if a.logFirstDialogID == "" {
		a.logFirstDialogID = id
	}
}

/** addPrompt adds a message to the conversation history and records it in the
 * session log as an injection with source "prompt", so the prompt can be
 * rebuilt from the log alone (DR-0048). Every addition of a message that is
 * not the model's own reply goes through here.
 *
 * Parameters:
 *   role    (string) — "user", "system" or "assistant".
 *   via     (string) — what added it: read, read-dir, read-pdf, attach, run,
 *                      search, git, shell, summary, pinned, memory, kb-inject,
 *                      skill, retry, correction, fold, resume, plan.
 *   content (string) — the message text.
 *
 * Example:
 *   a.addPrompt("user", "read", "[context: /read a.go]\n...")
 */
func (a *Agent) addPrompt(role, via, content string) {
	a.logPromptMessage(role, via, content)
	a.AddMessage(role, content)
}

// addPromptParts is addPrompt for a multimodal message. The parts, images
// included, are kept as one blob of JSON.
func (a *Agent) addPromptParts(role, via string, parts []anyllm.ContentPart) {
	if a.SessionLog != nil {
		if raw, err := json.Marshal(parts); err == nil {
			a.logEvent(KindInjection, LevelInfo, map[string]any{
				"source": "prompt", "via": via, "role": role, "turn": a.logTurn, "parts": a.logContentBlob(string(raw)),
			})
		}
	}
	a.AddMessageParts(role, parts)
}

// logPromptMessage writes the injection event for a message added to the history.
func (a *Agent) logPromptMessage(role, via, content string) {
	if a.SessionLog == nil {
		return
	}
	a.logEvent(KindInjection, LevelInfo, map[string]any{
		"source": "prompt", "via": via, "role": role, "turn": a.logTurn, "message": a.logContent(content),
	})
}

// historySnapshot copies the history before a rewrite, so logRewrite can say
// what the rewrite did. With no log open it returns nil and costs nothing.
func (a *Agent) historySnapshot() []Message {
	if a.SessionLog == nil {
		return nil
	}
	return append([]Message(nil), a.History...)
}

/** logRewrite records that the history was rewritten, as a compaction event
 * that says how to get the new list from the old one: ranges of the old list
 * that were kept, and the messages that are new. A prompt rebuilt from the log
 * applies it and gets the history exactly (DR-0048). Nothing is written if the
 * history did not change.
 *
 * Parameters:
 *   reason (string)    — trim, retry, summary, tool_result, clear, failed, cancelled, reset...
 *   before ([]Message) — historySnapshot taken before the rewrite.
 *
 * Example:
 *   before := a.historySnapshot()
 *   a.History = trimmed
 *   a.logRewrite("trim", before)
 */
func (a *Agent) logRewrite(reason string, before []Message) { a.logRewriteWith(reason, before, nil) }

// logRewriteWith is logRewrite with more fields on the event: the summary text
// and the events it stands for, for the rewrite that makes a summary.
func (a *Agent) logRewriteWith(reason string, before []Message, extra map[string]any) {
	if a.SessionLog == nil {
		return
	}
	ops, changed := rewriteOps(before, a.History)
	if !changed {
		return
	}
	for i, op := range ops {
		if m, ok := op.(Message); ok { // a new message; its text is scrubbed like any logged text
			ops[i] = map[string]any{"role": m.Role, "message": a.logContent(m.Content), "tool_call_id": m.ToolCallID, "tool_calls": m.ToolCalls, "parts": m.Parts}
		}
	}
	data := map[string]any{
		"turn": a.logTurn, "reason": reason, "sources": []string{}, "through": a.logLastID, "ops": ops,
	}
	for k, v := range extra {
		data[k] = v
	}
	a.logEvent(KindCompaction, LevelInfo, data)
}

// dropLastMessage removes the last message of the history, the user message of
// a turn that failed or was cancelled, and logs it.
func (a *Agent) dropLastMessage(reason string) {
	if len(a.History) == 0 {
		return
	}
	before := a.historySnapshot()
	a.History = a.History[:len(a.History)-1]
	a.logRewrite(reason, before)
}

// rewriteOps describes next as a sequence of kept ranges of prev ([from, to),
// as two-element arrays) and new messages, in order. It reports false when the
// two lists are the same.
func rewriteOps(prev, next []Message) (ops []any, changed bool) {
	ops = []any{} // an empty list is a history emptied, not a missing field
	key := func(m Message) string { b, _ := json.Marshal(m); return string(b) }
	pk := make([]string, len(prev))
	for i, m := range prev {
		pk[i] = key(m)
	}
	cur := 0
	run := [2]int{-1, -1}
	flush := func() {
		if run[0] >= 0 {
			ops = append(ops, []int{run[0], run[1]})
			run = [2]int{-1, -1}
		}
	}
	for _, m := range next {
		k := key(m)
		found := -1
		for i := cur; i < len(pk); i++ {
			if pk[i] == k {
				found = i
				break
			}
		}
		if found < 0 {
			flush()
			ops = append(ops, m)
			continue
		}
		if run[0] >= 0 && found == run[1] {
			run[1] = found + 1
		} else {
			flush()
			run = [2]int{found, found + 1}
		}
		cur = found + 1
	}
	flush()
	changed = len(prev) != len(next)
	if !changed {
		for i := range prev {
			if pk[i] != key(next[i]) {
				changed = true
				break
			}
		}
	}
	return ops, changed
}

// logTurnMessage records the user message of a chat turn as sent. It is the
// line typed with whatever was prepended to it (file contents, recalled
// context); when nothing was, the event says so instead of repeating the text.
func (a *Agent) logTurnMessage(sent string) {
	if a.SessionLog == nil {
		return
	}
	data := map[string]any{"source": "prompt", "via": "turn", "role": "user", "turn": a.logTurn}
	if sent == a.logInputText {
		data["same"] = true
	} else {
		data["message"] = a.logContent(sent)
	}
	a.logEvent(KindInjection, LevelInfo, data)
}

// promptHash is the SHA-256 of the messages sent to the model. Under policy
// replay the rendered prompt is the whole history as the client receives it,
// and JSON of []Message is deterministic (struct field order, sorted map keys).
func promptHash(msgs []Message) string {
	raw, err := json.Marshal(msgs)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// callPurposeKey is the context key under which a caller says what a model
// call is for, so the client's hook can log it as the right kind of request.
type callPurposeKey struct{}

type callPurpose struct {
	name   string
	source any
}

// purposeTurn marks the calls of the turn pipeline: the user's turn, the tool
// loop under it, and the retries. They are logged as assembled requests.
const purposeTurn = "turn"

// purposeRoute marks the call of a turn sent to a named route (@name). It is
// logged like a turn request but names the route, and the prompt rebuilt from
// the log does not include it: the history it sees is a window of the real one.
const purposeRoute = "route"

/** withCallPurpose says what a model call is for. The purpose "turn" is the
 * conversation itself; any other (chunk, synthesis, summary, ...) makes the
 * call an ad-hoc request that carries the purpose and, where there is one, a
 * source such as a file and line range (DR-0040 item 2).
 *
 * Parameters:
 *   ctx    (context.Context) — the call's context.
 *   name   (string)          — the purpose.
 *   source (any)             — what the prompt was built from; nil if nothing.
 *
 * Returns:
 *   context.Context — ctx carrying the purpose.
 *
 * Example:
 *   ctx = withCallPurpose(ctx, "chunk", map[string]any{"path": "a.go", "from_line": 1, "to_line": 80})
 */
func withCallPurpose(ctx context.Context, name string, source any) context.Context {
	return context.WithValue(ctx, callPurposeKey{}, callPurpose{name, source})
}

func callPurposeOf(ctx context.Context) callPurpose {
	if p, ok := ctx.Value(callPurposeKey{}).(callPurpose); ok {
		return p
	}
	return callPurpose{}
}

/** BeginModelCall implements ModelCallLog: it writes the model_request for a
 * call about to be made and returns the func that writes the model_response
 * when it ends. With no log open it does nothing. A call whose context says
 * it belongs to the turn is logged as an assembled request (policy replay
 * until the assembler exists); every other call is an ad-hoc request that
 * names its purpose.
 *
 * Parameters:
 *   ctx       (context.Context) — carries the call's purpose, if any.
 *   msgs      ([]Message)       — the messages sent.
 *   toolCount (int)             — how many tool schemas are sent with them.
 *
 * Returns:
 *   func(text string, stats ChatStats, err error) — call once when the call ends.
 *
 * Example:
 *   end := a.BeginModelCall(ctx, msgs, 0)
 *   end(reply, stats, err)
 */
func (a *Agent) BeginModelCall(ctx context.Context, msgs []Message, toolCount int) func(string, ChatStats, error) {
	if a.SessionLog == nil {
		return func(string, ChatStats, error) {}
	}
	reqID := a.logModelRequest(ctx, msgs, toolCount)
	return func(text string, stats ChatStats, err error) { a.logModelResponse(reqID, text, stats, err) }
}

// wireSessionLog makes client tell this agent about each model call. It is
// installed whether or not a log is open yet; the agent looks the log up at
// call time, so a client built before the log opened still logs.
func (a *Agent) wireSessionLog(client LLMClient) LLMClient {
	if ac, ok := client.(*AnyLLMClient); ok {
		ac.CallLog = a
	}
	return client
}

// logModelRequest writes the model_request for one call; see BeginModelCall.
func (a *Agent) logModelRequest(ctx context.Context, msgs []Message, toolCount int) string {
	if a.SessionLog == nil {
		return ""
	}
	clientName := "none"
	if a.Client != nil {
		clientName = a.Client.Name()
	}
	purpose := callPurposeOf(ctx)
	sysID, sysText := systemPromptOf(msgs)
	data := map[string]any{
		"turn":          a.logTurn,
		"system":        sysID,
		"backend":       backendFromClientName(clientName),
		"model":         modelFromClientName(clientName),
		"prompt_sha256": promptHash(msgs),
	}
	if purpose.name == purposeTurn || purpose.name == purposeRoute {
		data["form"] = "assembled"
		if purpose.name == purposeRoute {
			data["route"] = purpose.source.(map[string]any)["route"]
		}
		data["policy"] = "replay"
		data["pinned"] = []string{}
		data["summaries"] = []string{}
		data["kb"] = []string{}
		data["tail"] = map[string]any{"from": a.logFirstDialogID, "to": a.logLastID, "history_messages": len(msgs)}
		data["budget"] = map[string]any{"window": a.effectiveContextLimit(), "window_source": "unknown", "reserve": 0, "used_est": estimateTokens(HistoryText(msgs))}
		data["template"] = "replay"
		if m := a.lastManifest; m != nil {
			// The assembler chose this prompt at the start of the turn (L7b); the
			// request names what it is made of, by event, and what was left out.
			data["policy"] = "assembled"
			data["template"] = "assembled"
			data["pinned"], data["summaries"], data["kb"] = nonNil(m.Pinned), nonNil(m.Summaries), nonNil(m.KB)
			data["tail"] = map[string]any{"from": m.Tail.From, "to": a.logLastID, "turns": m.Tail.Turns, "history_messages": len(msgs)}
			data["dropped"] = len(m.Dropped)
			data["budget"] = map[string]any{"window": a.effectiveContextLimit(), "window_source": "unknown", "limit": m.Limit, "limit_source": m.LimitSource, "reserve": 0, "used_est": estimateTokens(HistoryText(msgs))}
		}
	} else {
		name := purpose.name
		if name == "" {
			name = "unspecified"
		}
		data["form"] = "adhoc"
		data["purpose"] = name
		data["messages"] = len(msgs)
		if purpose.source != nil {
			data["source"] = purpose.source
		}
		// The prompt of an ad-hoc call is built from content the log does not
		// otherwise hold (a chunk of a file, a summary request), so it is kept,
		// once and by hash.
		if raw, err := json.Marshal(msgs); err == nil {
			data["body"] = a.logContentBlob(string(raw))
		}
	}
	// The system prompt text is stored once per version (DR-0039). It can change
	// without a model switch (a trimmed prompt, a skill catalog, /clear), so the
	// request carries the text the first time it sees a new version.
	// An ad-hoc call has a system prompt of its own (a chunk analyser, a summary
	// request) that is not the conversation's; its body holds it, and it must not
	// change which version the log believes the conversation has.
	if data["form"] == "assembled" && sysID != "" && sysID != a.logSystemID {
		data["system_text"] = a.logContent(sysText)
		a.logSystemID = sysID
	}
	id := a.logEvent(KindModelRequest, LevelInfo, data)
	// With --debug the exact messages sent are kept too, as a level-debug event
	// that rendering and kb ingest skip (DR-0038 item 5).
	if id != "" && a.Config.Debug && purpose.name == purposeTurn {
		if raw, err := json.Marshal(msgs); err == nil {
			a.logEvent(KindModelRequest, LevelDebug, map[string]any{
				"form": "body", "request": id, "tools": toolCount, "body": a.logContentBlob(string(raw)),
			})
		}
	}
	return id
}

// logContentBlob is logContent for a payload that always goes to a blob.
func (a *Agent) logContentBlob(text string) any {
	if a.SessionLog == nil {
		return text
	}
	c, err := a.SessionLog.ContentBlob(text)
	if err != nil {
		if !a.sessionLogWarned {
			a.sessionLogWarned = true
			fmt.Fprintf(os.Stderr, "harvey: session log: %v\n", err)
		}
		return "[content not logged]"
	}
	return c
}

// agentToolEvents adapts an Agent to ToolEvents.
type agentToolEvents struct{ a *Agent }

/** toolEvents returns the ToolEvents that write this agent's tool-loop events
 * to its session log, or a nil interface when no log is open, so a
 * ToolExecutor given the result records nothing.
 *
 * Returns:
 *   ToolEvents — the adapter, or nil.
 *
 * Example:
 *   ex.Events = a.toolEvents()
 */
func (a *Agent) toolEvents() ToolEvents {
	if a.SessionLog == nil {
		return nil
	}
	return agentToolEvents{a}
}

func (t agentToolEvents) ToolCall(tc anyllm.ToolCall) string {
	id := t.a.logEvent(KindToolCall, LevelInfo, map[string]any{
		"turn":    t.a.logTurn,
		"tool":    tc.Function.Name,
		"origin":  "model",
		"call_id": tc.ID,
		"args":    t.a.logContent(tc.Function.Arguments),
	})
	t.a.permCall = permissionCall{id: id, model: true}
	if t.a.toolCallEvents == nil {
		t.a.toolCallEvents = map[string]string{}
	}
	t.a.toolCallEvents[tc.ID] = id
	return id
}

// Compacted logs the shortening of a finished tool round as a compaction event
// naming the tool_call events it covers, so a prompt rebuilt from the log can
// apply the same rewrite (DR-0048).
func (t agentToolEvents) Compacted(callIDs []string) {
	var sources []string
	for _, c := range callIDs {
		if id := t.a.toolCallEvents[c]; id != "" {
			sources = append(sources, id)
		}
	}
	t.a.logEvent(KindCompaction, LevelInfo, map[string]any{
		"turn": t.a.logTurn, "reason": "tool_result", "sources": sources, "call_ids": callIDs,
	})
}

func (t agentToolEvents) ToolResult(call string, tc anyllm.ToolCall, output string, failed bool, elapsed time.Duration) {
	t.a.permCall = permissionCall{}
	status := "ok"
	if failed {
		status = "error"
	}
	t.a.logEvent(KindToolResult, LevelInfo, map[string]any{
		"turn":    t.a.logTurn,
		"call":    call,
		"call_id": tc.ID,
		"status":  status,
		"output":  t.a.logContent(output),
		"ms":      elapsed.Milliseconds(),
	})
}

/** logModelResponse records how a model call ended: its text, or why it
 * failed. It is written for every request, on success, error and
 * cancellation alike, so a turn that fails is in the log.
 *
 * Parameters:
 *   request (string)    — id of the model_request event it answers.
 *   reply   (string)    — the model's text; empty on failure.
 *   stats   (ChatStats) — usage as the backend reported it; zero counts mean
 *                         "not reported" and are left out, never written as 0.
 *   err     (error)     — nil on success.
 *
 * Example:
 *   defer func() { a.logModelResponse(reqID, reply, stats, err) }()
 */
func (a *Agent) logModelResponse(request, reply string, stats ChatStats, err error) {
	if a.SessionLog == nil || request == "" {
		return
	}
	data := map[string]any{"turn": a.logTurn, "request": request}
	switch {
	case err == nil:
		data["status"] = "ok"
		data["text"] = a.logContent(reply)
	case isCancel(err):
		data["status"] = "cancelled"
	default:
		data["status"] = "error"
		data["error"] = a.logContent(err.Error())
	}
	if stats.PromptTokens > 0 || stats.ReplyTokens > 0 {
		data["usage"] = map[string]int{"prompt": stats.PromptTokens, "completion": stats.ReplyTokens}
	}
	if stats.Elapsed > 0 {
		data["ms"] = stats.Elapsed.Milliseconds()
	}
	a.logEvent(KindModelResponse, LevelInfo, data)
}

// modelFromClientName returns the model part of a client name such as
// "ollama (llama3.2:3b)" -> "llama3.2:3b", or the name itself when it has no
// parentheses.
func modelFromClientName(name string) string {
	if i := strings.Index(name, "("); i >= 0 {
		return strings.TrimSuffix(strings.TrimSpace(name[i+1:]), ")")
	}
	return name
}

// isCancel reports whether err is a cancelled context: the author pressed
// Ctrl-C, which the log records as status cancelled and not as a failure.
func isCancel(err error) bool { return errors.Is(err, context.Canceled) }

// ModelRef names a backend and a model, as the log writes them in model_switch.
type ModelRef struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
}

// systemPromptVersion returns the id and text of the system prompt in the
// history ("" and "" when there is none). The id is "sys-" and the first four
// bytes of the text's SHA-256, so equal text always has the same id.
func (a *Agent) systemPromptVersion() (id, text string) { return systemPromptOf(a.History) }

// systemPromptOf is systemPromptVersion for any list of messages.
func systemPromptOf(msgs []Message) (id, text string) {
	for _, m := range msgs {
		if m.Role == "system" {
			sum := sha256.Sum256([]byte(m.Content))
			return "sys-" + hex.EncodeToString(sum[:4]), m.Content
		}
	}
	return "", ""
}

/** logCommand records a slash command when it has finished, with its status:
 * "ok", or the exit class name of the error (usage, negative, data, ...).
 * The line is scrubbed, since a command can carry a secret.
 *
 * Parameters:
 *   line (string) — the command as typed, with its leading slash.
 *   err  (error)  — what the command returned; nil on success.
 *
 * Example:
 *   defer func() { a.logCommand(input, err) }()
 */
func (a *Agent) logCommand(line string, err error) {
	if a.SessionLog == nil {
		return
	}
	a.logEvent(KindCommand, LevelInfo, map[string]any{
		"line":   a.logContent(line),
		"status": ExitCodeFor(err).Name,
	})
}

/** logModelSwitch records a change of the active model, naming the one it
 * leaves and the one it enters. Every path that changes the model calls it
 * beside the recorder's note, so no path can leave the log naming the
 * previous model (the bug behind DR-0036).
 *
 * Parameters:
 *   model   (string) — the model switched to.
 *   backend (string) — the backend or engine label, such as "ollama".
 *
 * Example:
 *   a.logModelSwitch("qwen2.5-coder:1.5b", "ollama")
 */
func (a *Agent) logModelSwitch(model, backend string) {
	if a.SessionLog == nil {
		return
	}
	to := ModelRef{Backend: backend, Model: model}
	a.logEvent(KindModelSwitch, LevelInfo, map[string]any{
		"from":   a.logModel,
		"to":     to,
		"window": Window{N: a.effectiveContextLimit(), Source: "unknown"},
	})
	a.logModel = to
}

/** logShellCall records a !cmd the author ran, just before the process
 * starts. It is the author's act, so origin is "user" and there is no turn
 * (DR-0040 item 4). The returned id doubles as the call_id that ties the
 * result to it.
 *
 * Parameters:
 *   cmdLine (string) — the command as typed, without the leading "!".
 *
 * Returns:
 *   string — the tool_call event id; "" when no log is open.
 *
 * Example:
 *   callID := a.logShellCall("go test ./...")
 */
func (a *Agent) logShellCall(cmdLine string) string {
	if a.SessionLog == nil {
		return ""
	}
	// A user-origin call has no provider call id: the result cites this
	// event's own id. It is the call in flight until its result is logged, so
	// the permission decision that follows can name it.
	id := a.logEvent(KindToolCall, LevelInfo, map[string]any{
		"tool":   "shell",
		"origin": "user",
		"args":   map[string]any{"cmd": a.logContent(cmdLine), "cwd": "<workspace>"},
	})
	a.permCall = permissionCall{id: id}
	return id
}

/** logShellResult records how a !cmd ended. Every command that started has a
 * result: it ran to the end, exited non-zero, could not start, or was
 * cancelled. The output is what the author saw and the model is given, capped
 * at maxRunOutput, so the log is as bounded as the context is.
 *
 * Parameters:
 *   callID    (string) — the tool_call id from logShellCall.
 *   status    (string) — ok, error or cancelled.
 *   exit      (*int)   — the process exit code; nil when it never ran to one.
 *   output    ([]byte) — combined stdout and stderr, already capped.
 *   truncated (bool)   — the output was cut at the cap.
 *   runErr    (error)  — the error from starting or waiting; nil if none.
 *
 * Returns:
 *   string — the tool_result event id; "" when no log is open.
 *
 * Example:
 *   resID := a.logShellResult(callID, "ok", &zero, out, false, nil)
 */
func (a *Agent) logShellResult(callID, status string, exit *int, output []byte, truncated bool, runErr error) string {
	a.permCall = permissionCall{}
	if a.SessionLog == nil || callID == "" {
		return ""
	}
	data := map[string]any{
		"call":    callID,
		"call_id": callID,
		"status":  status,
		"output":  a.logContent(string(output)),
	}
	if exit != nil {
		data["exit"] = *exit
	}
	if truncated {
		data["truncated"] = true
	}
	if runErr != nil && exit == nil {
		data["error"] = a.logContent(runErr.Error())
	}
	return a.logEvent(KindToolResult, LevelInfo, data)
}

/** logShellInjection records that the output of a !cmd entered the prompt. It
 * holds no text: the output is already in the tool_result it cites, and the
 * wrapper the model sees is a fixed template.
 *
 * Parameters:
 *   resultID (string) — the tool_result id from logShellResult.
 *
 * Example:
 *   a.logShellInjection(resID)
 */
func (a *Agent) logShellInjection(resultID string) {
	if a.SessionLog == nil || resultID == "" {
		return
	}
	a.logEvent(KindInjection, LevelInfo, map[string]any{
		"source": "shell",
		"origin": "user",
		"from":   resultID,
	})
}

// cappedOutput returns b cut to maxRunOutput bytes, the amount a !cmd shows
// the model.
func cappedOutput(b []byte) []byte {
	if len(b) > maxRunOutput {
		return b[:maxRunOutput]
	}
	return b
}

/** logSkillLoad records that a skill's body entered the conversation, as an
 * injection with source "skill". The body is kept once, by hash, however
 * often the skill is loaded. A load the author asked for has no turn; one
 * harvey made because the input matched the skill's trigger belongs to the
 * turn that is running.
 *
 * Parameters:
 *   skill  (*SkillMeta) — the skill that was loaded.
 *   origin (string)     — "user" or "auto".
 *
 * Example:
 *   a.logSkillLoad(skill, "user")
 */
func (a *Agent) logSkillLoad(skill *SkillMeta, origin string) {
	if a.SessionLog == nil {
		return
	}
	data := map[string]any{
		"source":      "skill",
		"origin":      origin,
		"ref":         skill.Name,
		"description": a.logContent(skill.Description),
		"path":        a.logContent(skill.Path),
		"text":        a.logContentBlob(skill.Body),
	}
	if origin == "auto" {
		data["turn"] = a.logTurn
	}
	a.logEvent(KindInjection, LevelInfo, data)
}

/** logPipelineCall records the start of one /pipeline step as a tool_call by
 * the author, with what the Fountain scene of the step needs: the file, the step
 * and how many, the threshold, every file of the run, the model that will answer,
 * and the step's prompt (a blob).
 *
 * Returns:
 *   string — the tool_call event id; "" when no log is open.
 *
 * Example:
 *   id := a.logPipelineCall("s1.md", 1, 2, 0.7, []string{"s1.md", "s2.md"}, "ollama (m)", body)
 */
func (a *Agent) logPipelineCall(file string, step, of int, threshold float64, files []string, model, body string) string {
	if a.SessionLog == nil {
		return ""
	}
	return a.logEvent(KindToolCall, LevelInfo, map[string]any{
		"tool":   "pipeline",
		"origin": "user",
		"args": map[string]any{
			"file": file, "step": step, "of": of, "threshold": threshold,
			"files": files, "model": model, "body": a.logContentBlob(body),
		},
	})
}

/** logPipelineResult records how a pipeline step ended: the response with its
 * confidence block removed, the confidence the pipeline settled on, and whether
 * the step passed. A chat error and a score below the threshold are both
 * "failed"; the error says which.
 *
 * Parameters:
 *   callID   (string) — the tool_call id from logPipelineCall.
 *   response (string) — the step's response, confidence block removed.
 *   score    (float64) — the confidence; 0 when the step could not be scored.
 *   err      (error)  — nil when the step passed.
 *
 * Example:
 *   a.logPipelineResult(id, stripped, 0.9, nil)
 */
func (a *Agent) logPipelineResult(callID, response string, score float64, err error) {
	if a.SessionLog == nil || callID == "" {
		return
	}
	data := map[string]any{
		"call": callID, "call_id": callID, "status": "ok",
		"confidence": score, "output": a.logContent(response),
	}
	if err != nil {
		data["status"] = "failed"
		data["error"] = a.logContent(err.Error())
	}
	a.logEvent(KindToolResult, LevelInfo, data)
}

// nonNil returns s, or an empty list for nil, so the log writes [] and not null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
