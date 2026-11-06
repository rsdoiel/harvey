package harvey

import (
	"bytes"
	"encoding/json"
	"testing"
)

// permAgent is an agent with a session log open and its audit buffer wired to
// it, as NewAgent does.
func permAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	a, path := agentWithLog(t)
	a.AuditBuffer = NewAuditBuffer(10)
	a.wireAudit()
	return a, path
}

type permData struct {
	Action    string  `json:"action"`
	Target    Content `json:"target"`
	Decision  string  `json:"decision"`
	Scope     string  `json:"scope"`
	OutcomeOf string  `json:"outcome_of"`
	Turn      *int    `json:"turn"`
}

func perms(t *testing.T, a *Agent, path string) []permData {
	t.Helper()
	var out []permData
	for _, e := range eventsOf(t, a, path, KindPermission) {
		var d permData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func TestPermission_RuleDecisionsComeFromTheAuditBuffer(t *testing.T) {
	a, path := permAgent(t)
	a.AuditBuffer.Log(ActionCommand, "go test ./...", StatusAllowed)
	a.AuditBuffer.Log(ActionCommand, "rm -rf /", StatusDenied)
	a.AuditBuffer.Log(ActionFileWrite, "notes.md", StatusDenied)
	a.AuditBuffer.Log(ActionFileWrite, "out.txt", StatusSuccess)
	a.AuditBuffer.Log(ActionFileWrite, "out2.txt", StatusError)
	a.AuditBuffer.Log(ActionFileRead, "secret.pem", StatusDenied)
	a.AuditBuffer.Log(ActionFileRead, "main.go", StatusSuccess) // an allowed read is not an event
	a.AuditBuffer.Log(ActionFileRead, "gone.go", StatusError)

	want := []permData{
		{Action: "run", Target: Content{Text: "go test ./..."}, Decision: "auto", Scope: "rule"},
		{Action: "run", Target: Content{Text: "rm -rf /"}, Decision: "deny", Scope: "rule"},
		{Action: "write", Target: Content{Text: "notes.md"}, Decision: "deny", Scope: "rule"},
		{Action: "write", Target: Content{Text: "out.txt"}, Decision: "auto", Scope: "rule"},
		{Action: "write", Target: Content{Text: "out2.txt"}, Decision: "auto", Scope: "rule"},
		{Action: "read", Target: Content{Text: "secret.pem"}, Decision: "deny", Scope: "rule"},
	}
	got := perms(t, a, path)
	if len(got) != len(want) {
		t.Fatalf("%d permission events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Action != w.Action || g.Target.Text != w.Target.Text || g.Decision != w.Decision || g.Scope != w.Scope {
			t.Errorf("event %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestPermission_AuditStillRecordsInTheBuffer(t *testing.T) {
	a, _ := permAgent(t)
	a.AuditBuffer.Log(ActionCommand, "ls", StatusAllowed)
	if a.AuditBuffer.Size() != 1 {
		t.Errorf("audit buffer holds %d events, want 1: the hook must not replace it", a.AuditBuffer.Size())
	}
}

func TestPermission_AModelToolCallIsTheOutcomeOfItsPermission(t *testing.T) {
	a, path := permAgent(t)
	a.logUserInput("write it")
	ev := a.toolEvents()
	tc := ToolCall{ID: "call_7", Function: FunctionCall{Name: "write_file", Arguments: `{}`}}
	callID := ev.ToolCall(tc)
	a.AuditBuffer.Log(ActionFileWrite, "out.txt", StatusSuccess)
	ev.ToolResult(callID, tc, "wrote", false, 0)

	got := perms(t, a, path)
	if len(got) != 1 || got[0].OutcomeOf != callID || got[0].Turn == nil || *got[0].Turn != 1 {
		t.Fatalf("permission = %+v, want outcome_of %s in turn 1", got, callID)
	}
	events, _, _ := ReadSessionLog(path)
	if k := infoKinds(events); k != "user_input tool_call permission tool_result" {
		t.Errorf("kinds = %s: the decision sits between the call it gates and the result", k)
	}
	// After the result the call is no longer in flight.
	a.AuditBuffer.Log(ActionFileWrite, "later.txt", StatusSuccess)
	got = perms(t, a, path)
	if got[1].OutcomeOf != "" || got[1].Turn != nil {
		t.Errorf("a decision outside any tool call must not claim one: %+v", got[1])
	}
}

func TestPermission_TheTargetIsScrubbed(t *testing.T) {
	a, path := permAgent(t)
	a.AuditBuffer.Log(ActionCommand, "curl -H 'Authorization: Bearer abc.def' https://x", StatusAllowed)
	evs := eventsOf(t, a, path, KindPermission)
	if len(evs) != 1 || bytes.Contains(evs[0].Data, []byte("abc.def")) {
		t.Errorf("permission leaks a credential: %s", evs[0].Data)
	}
}

func TestPermission_TheAuthorsAnswersAreLogged(t *testing.T) {
	a, path := permAgent(t)
	a.logAction("write", "a.txt", actionYes, "ok")
	a.logAction("write", "b.txt", actionAll, "ok")
	a.logAction("write", "c.txt", actionNo, "skipped")
	a.logAction("write", "d.txt", actionQuit, "aborted")
	// A refusal by the permissions table is a rule decision, recorded once, as
	// the author saying no to something that could not have happened.
	a.logAction("write", "e.txt", actionNo, "denied")
	// Memory and context actions are not file or command permissions.
	a.logAction("add_memory", "m", actionYes, "ok")

	want := []permData{
		{Action: "write", Target: Content{Text: "a.txt"}, Decision: "allow", Scope: "once"},
		{Action: "write", Target: Content{Text: "b.txt"}, Decision: "allow", Scope: "session"},
		{Action: "write", Target: Content{Text: "c.txt"}, Decision: "deny", Scope: "once"},
		{Action: "write", Target: Content{Text: "d.txt"}, Decision: "none", Scope: "once"},
		{Action: "write", Target: Content{Text: "e.txt"}, Decision: "deny", Scope: "rule"},
	}
	got := perms(t, a, path)
	if len(got) != len(want) {
		t.Fatalf("%d permission events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Action != w.Action || g.Target.Text != w.Target.Text || g.Decision != w.Decision || g.Scope != w.Scope {
			t.Errorf("answer %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestPermission_AfterAllLaterWritesAreSessionScoped(t *testing.T) {
	a, path := permAgent(t)
	a.logAction("write", "a.txt", actionAll, "ok")
	a.permScope = "session" // set by the offer loop once the author answers all
	a.logAction("write", "b.txt", actionYes, "ok")
	a.permScope = ""
	a.logAction("write", "c.txt", actionYes, "ok")
	got := perms(t, a, path)
	if got[1].Scope != "session" || got[2].Scope != "once" {
		t.Errorf("scopes = %s, %s, %s; want session, session, once", got[0].Scope, got[1].Scope, got[2].Scope)
	}
}

func TestPermission_NoLogIsNoProblem(t *testing.T) {
	a := newTestAgent(t)
	a.AuditBuffer = NewAuditBuffer(5)
	a.wireAudit()
	a.AuditBuffer.Log(ActionCommand, "ls", StatusAllowed)
	a.logAction("write", "x", actionYes, "ok")
}
