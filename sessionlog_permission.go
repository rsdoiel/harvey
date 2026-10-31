package harvey

// Session-log events for permission decisions (DR-0040 item 1). Two kinds of
// decision reach them:
//
//   - Rule decisions: harvey.yaml's permissions table and the safe-mode
//     allowlist. They all pass through AuditBuffer.Log, which is hooked here.
//   - The author's answers to an offer to write a file (yes, all, no, quit),
//     which pass through Agent.logAction.
//
// The event is written when the decision is made, so a tool call it gates is
// followed by its permission and then its result.

/** permissionCall is the tool call in flight when a decision is made, so the
 * permission can name it (outcome_of). A model call also gives the turn.
 *
 * Example:
 *   a.permCall = permissionCall{id: callID, model: true}
 */
type permissionCall struct {
	id    string
	model bool
}

/** wireAudit makes the agent's audit buffer report each entry to the session
 * log. The buffer keeps recording as before; the hook looks the log up at
 * call time, so it does not matter whether the log is open yet. A nil buffer is
 * left alone.
 *
 * Example:
 *   a.AuditBuffer = NewAuditBuffer(1000)
 *   a.wireAudit()
 */
func (a *Agent) wireAudit() {
	if a.AuditBuffer != nil {
		a.AuditBuffer.OnEvent = a.permissionFromAudit
	}
}

// permissionFromAudit turns a rule decision recorded in the audit buffer into
// a permission event. Denials of commands, writes and reads are decisions; so
// is an allowed command. A write that went ahead (success or error) shows that
// the rule allowed it. A read that went ahead is not an event: it adds nothing
// the tool_result does not already show.
func (a *Agent) permissionFromAudit(e AuditEvent) {
	switch e.Action {
	case ActionCommand:
		switch e.Status {
		case StatusAllowed:
			a.logPermission("run", e.Details, "auto", "rule")
		case StatusDenied:
			a.logPermission("run", e.Details, "deny", "rule")
		}
	case ActionFileWrite:
		switch e.Status {
		case StatusDenied:
			if a.offerOf > 0 {
				return // inside a write offer, logAction records this refusal with its offer
			}
			a.logPermission("write", e.Details, "deny", "rule")
		case StatusSuccess, StatusError:
			a.logPermission("write", e.Details, "auto", "rule")
		}
	case ActionFileRead:
		if e.Status == StatusDenied {
			a.logPermission("read", e.Details, "deny", "rule")
		}
	}
}

/** logPermission records one permission decision.
 *
 * Parameters:
 *   action   (string) — write, run, read or network.
 *   target   (string) — the path or command line; scrubbed.
 *   decision (string) — allow, deny, auto (by configuration, no prompt) or
 *                       none (an offer abandoned).
 *   scope    (string) — once, session or rule.
 *
 * Example:
 *   a.logPermission("write", "notes.md", "allow", "once")
 */
func (a *Agent) logPermission(action, target, decision, scope string) {
	a.logPermissionWith(action, target, decision, scope, nil)
}

// logPermissionWith is logPermission with further fields in the event.
func (a *Agent) logPermissionWith(action, target, decision, scope string, extra map[string]any) {
	if a.SessionLog == nil {
		return
	}
	data := map[string]any{
		"action": action, "target": a.logContent(target), "decision": decision, "scope": scope,
	}
	for k, v := range extra {
		data[k] = v
	}
	if a.permCall.id != "" {
		data["outcome_of"] = a.permCall.id
		if a.permCall.model {
			data["turn"] = a.logTurn
		}
	}
	a.logEvent(KindPermission, LevelInfo, data)
}

// logAuthorsChoice records the author's answer to an offer to write target.
// An answer after "all" is session-scoped.
func (a *Agent) logAuthorsChoice(kind, target string, choice actionChoice) {
	a.logAuthorsAnswer(kind, target, choice, "")
}

/** logAuthorsAnswer records the author's answer to an offer to write or run
 * something, with what came of it, as a permission event. Besides the
 * decision and scope it carries the literal choice (yes, no, all, quit), the
 * outcome (ok, skipped, aborted, denied, error: …) and, for the tagged blocks
 * of one reply, which offer of how many it was, so the Fountain scene of the
 * offer can be rebuilt from the log. A refusal by the permissions table is
 * recorded here once, as a rule decision with the choice no, not as a second
 * event.
 *
 * Parameters:
 *   kind    (string)       — write or run; other kinds are not permissions.
 *   target  (string)       — the path or command.
 *   choice  (actionChoice) — what the author answered.
 *   outcome (string)       — what came of it; "" when not known.
 *
 * Example:
 *   a.logAuthorsAnswer("write", "out/a.txt", actionYes, "ok")
 */
func (a *Agent) logAuthorsAnswer(kind, target string, choice actionChoice, outcome string) {
	action := ""
	switch kind {
	case "write":
		action = "write"
	case "run":
		action = "run"
	default:
		return // memory and context actions are not file or command permissions
	}
	decision, scope := "allow", "once"
	switch choice {
	case actionAll:
		scope = "session"
	case actionNo:
		decision = "deny"
	case actionQuit:
		decision = "none"
	}
	if decision == "allow" && a.permScope != "" {
		scope = a.permScope
	}
	if outcome == "denied" {
		decision, scope = "deny", "rule"
	}
	extra := map[string]any{"choice": choiceStr(choice)}
	if outcome != "" {
		extra["outcome"] = a.logContent(outcome)
	}
	if a.offerOf > 0 {
		extra["offer"] = map[string]int{"index": a.offerIndex, "of": a.offerOf}
	}
	a.logPermissionWith(action, target, decision, scope, extra)
}
