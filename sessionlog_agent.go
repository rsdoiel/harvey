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
func (a *Agent) logUserInput(input string) {
	if a.SessionLog == nil {
		return
	}
	a.logTurn++
	id := a.logEvent(KindUserInput, LevelInfo, map[string]any{"text": a.logContent(input)})
	if a.logFirstDialogID == "" {
		a.logFirstDialogID = id
	}
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
	if purpose.name == purposeTurn {
		data["form"] = "assembled"
		data["policy"] = "replay"
		data["pinned"] = []string{}
		data["summaries"] = []string{}
		data["kb"] = []string{}
		data["tail"] = map[string]any{"from": a.logFirstDialogID, "to": a.logLastID, "history_messages": len(msgs)}
		data["budget"] = map[string]any{"window": a.effectiveContextLimit(), "window_source": "unknown", "reserve": 0, "used_est": estimateTokens(HistoryText(msgs))}
		data["template"] = "replay"
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
	if sysID != "" && sysID != a.logSystemID {
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
	return t.a.logEvent(KindToolCall, LevelInfo, map[string]any{
		"turn":    t.a.logTurn,
		"tool":    tc.Function.Name,
		"origin":  "model",
		"call_id": tc.ID,
		"args":    t.a.logContent(tc.Function.Arguments),
	})
}

func (t agentToolEvents) ToolResult(call string, tc anyllm.ToolCall, output string, failed bool, elapsed time.Duration) {
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
	// event's own id.
	return a.logEvent(KindToolCall, LevelInfo, map[string]any{
		"tool":   "shell",
		"origin": "user",
		"args":   map[string]any{"cmd": a.logContent(cmdLine), "cwd": "<workspace>"},
	})
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
