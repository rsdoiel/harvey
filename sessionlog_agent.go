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

/** logModelRequest records the manifest of a model call. Until the assembler
 * exists (plan item L7) every manifest has policy "replay": the tail is the
 * whole history, nothing is pinned or summarised, and prompt_sha256 is still
 * stored (DR-0040 item 5).
 *
 * Parameters:
 *   used  (int) — estimated tokens in the prompt.
 *   limit (int) — the context window in use; 0 when unknown.
 *
 * Returns:
 *   string — the request event id; "" when no log is open.
 *
 * Example:
 *   reqID := a.logModelRequest(used, limit)
 */
func (a *Agent) logModelRequest(used, limit int) string {
	if a.SessionLog == nil {
		return ""
	}
	clientName := "none"
	if a.Client != nil {
		clientName = a.Client.Name()
	}
	sysID, sysText := a.systemPromptVersion()
	data := map[string]any{
		"form":          "assembled",
		"policy":        "replay",
		"turn":          a.logTurn,
		"system":        sysID,
		"pinned":        []string{},
		"summaries":     []string{},
		"kb":            []string{},
		"tail":          map[string]any{"from": a.logFirstDialogID, "to": a.logLastID, "history_messages": len(a.History)},
		"budget":        map[string]any{"window": limit, "window_source": "unknown", "reserve": 0, "used_est": used},
		"template":      "replay",
		"backend":       backendFromClientName(clientName),
		"model":         modelFromClientName(clientName),
		"prompt_sha256": promptHash(a.History),
	}
	// The system prompt text is stored once per version (DR-0039). It can change
	// without a model switch (a trimmed prompt, a skill catalog, /clear), so the
	// request carries the text the first time it sees a new version.
	if sysID != "" && sysID != a.logSystemID {
		data["system_text"] = a.logContent(sysText)
		a.logSystemID = sysID
	}
	return a.logEvent(KindModelRequest, LevelInfo, data)
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
func (a *Agent) systemPromptVersion() (id, text string) {
	for _, m := range a.History {
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
