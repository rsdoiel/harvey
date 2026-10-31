package harvey

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func item(kind ItemKind, role string, group int, text string) PromptItem {
	return PromptItem{Msg: Message{Role: role, Content: text}, Kind: kind, Group: group, Event: fmt.Sprintf("e%d-%s", group, kind)}
}

func words(n int) string { return strings.Repeat("word ", n) }

// A four-turn conversation with a system prompt, a pin and recalled knowledge.
func sampleItems() []PromptItem {
	return []PromptItem{
		item(ItemSystem, "system", 0, "You are Harvey."),
		item(ItemKB, "user", 0, "[kb] "+words(40)),
		item(ItemPin, "user", 0, "[pinned context]\n\nkeep answers short"),
		item(ItemTurn, "user", 0, "q1 "+words(20)), item(ItemTurn, "assistant", 0, "a1 "+words(20)),
		item(ItemTurn, "user", 1, "q2 "+words(20)), item(ItemTurn, "assistant", 1, "a2 "+words(20)),
		item(ItemTurn, "user", 2, "q3 "+words(20)), item(ItemTurn, "assistant", 2, "a3 "+words(20)),
		item(ItemTurn, "user", 3, "q4 "+words(20)),
	}
}

func roles(msgs []Message) string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.Role[:1]+":"+strings.Fields(m.Content)[0])
	}
	return strings.Join(out, " ")
}

func TestAssemble_WithinTheLimitKeepsEverythingStableFirst(t *testing.T) {
	msgs, m := Assemble(sampleItems(), AssembleBudget{Limit: 10000})
	// The order is system, pins, summaries, knowledge, then the tail.
	want := "s:You u:[pinned u:[kb] u:q1 a:a1 u:q2 a:a2 u:q3 a:a3 u:q4"
	if got := roles(msgs); got != want {
		t.Errorf("order\n got: %s\nwant: %s", got, want)
	}
	if len(m.Dropped) != 0 || m.Tail.Turns != 4 || len(m.Pinned) != 1 || len(m.KB) != 1 || m.System != "e0-system" {
		t.Errorf("manifest = %+v", m)
	}
}

func TestAssemble_UnderPressureKnowledgeGoesBeforeDialogAndOldTurnsBeforeNew(t *testing.T) {
	all := sampleItems()
	full := 0
	for _, it := range all {
		full += itemTokens(it.Msg)
	}
	kbTokens := itemTokens(all[1].Msg)

	// Just over the limit without the knowledge: only it goes.
	msgs, m := Assemble(all, AssembleBudget{Limit: full - kbTokens + 1, KBShare: 1})
	if len(m.Dropped) != 1 || m.Dropped[0] != "e0-kb" || strings.Contains(roles(msgs), "[kb]") {
		t.Errorf("want only the knowledge dropped, got %v / %s", m.Dropped, roles(msgs))
	}

	// Tighter: whole oldest turns go, never half of one, system and pin stay, the latest turn stays.
	msgs, m = Assemble(all, AssembleBudget{Limit: 120, KBShare: 1})
	got := roles(msgs)
	if !strings.HasPrefix(got, "s:You u:[pinned") || !strings.HasSuffix(got, "u:q4") {
		t.Errorf("system, pin and latest turn must stay: %s", got)
	}
	if strings.Contains(got, "q1") || strings.Contains(got, "a1") {
		t.Errorf("the oldest turn should be gone: %s", got)
	}
	for _, turn := range []string{"q2", "q3"} {
		if strings.Contains(got, turn) != strings.Contains(got, "a"+turn[1:]) && turn != "q4" {
			t.Errorf("turn %s was split: %s", turn, got)
		}
	}
	if m.UsedEst > 120 && m.Tail.Turns > 1 {
		t.Errorf("over the limit with more than the latest turn: %+v", m)
	}
}

func TestAssemble_TheLatestTurnAndThePinsSurviveAnyLimit(t *testing.T) {
	msgs, m := Assemble(sampleItems(), AssembleBudget{Limit: 1})
	got := roles(msgs)
	if got != "s:You u:[pinned u:q4" {
		t.Errorf("got %s", got)
	}
	if m.Tail.Turns != 1 || len(m.Pinned) != 1 {
		t.Errorf("manifest = %+v", m)
	}
}

func TestAssemble_KnowledgeIsCappedToItsShare(t *testing.T) {
	items := sampleItems()
	items = append(items[:2], append([]PromptItem{item(ItemKB, "user", 0, "[kb2] "+words(40))}, items[2:]...)...)
	items[2].Event = "e0-kb2"
	_, m := Assemble(items, AssembleBudget{Limit: 400, KBShare: 0.15})
	if len(m.KB) != 1 || m.KB[0] != "e0-kb2" || len(m.Dropped) == 0 || m.Dropped[0] != "e0-kb" {
		t.Errorf("the oldest knowledge should go first once over its share: %+v", m)
	}
}

func TestAssemble_IsDeterministicAndLeavesItsInputAlone(t *testing.T) {
	in := sampleItems()
	a, ma := Assemble(in, AssembleBudget{Limit: 150})
	b, mb := Assemble(in, AssembleBudget{Limit: 150})
	if promptHash(a) != promptHash(b) || fmt.Sprint(ma) != fmt.Sprint(mb) {
		t.Error("the same input and budget gave different prompts")
	}
	if len(in) != len(sampleItems()) {
		t.Error("Assemble changed its input")
	}
}

func TestAssemble_NoLimitChangesNothingButTheOrder(t *testing.T) {
	msgs, m := Assemble(sampleItems(), AssembleBudget{})
	if len(msgs) != len(sampleItems()) || len(m.Dropped) != 0 {
		t.Errorf("%d messages, manifest %+v", len(msgs), m)
	}
}

// limitedLoggedAgent is an agent with a session log and an active model whose
// prompt limit is cached, so assembleForTurn has a budget to keep to.
func limitedLoggedAgent(t *testing.T, limit int) *Agent {
	t.Helper()
	a, mc := newTestAgentWithCache(t, "llama3.2:3b")
	if err := mc.Set(&ModelCapability{Name: "llama3.2:3b", MaxPromptTokens: limit, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	opts := testLogOpts(t)
	opts.Start.Workspace = a.Workspace.Root
	l, err := OpenSessionLog(opts)
	if err != nil {
		t.Fatal(err)
	}
	a.SessionLog = l
	a.AddMessage("system", opts.Start.System.Text) // the history starts with the system prompt the log opened with
	t.Cleanup(func() { a.closeSessionLog("test") })
	return a
}

// playTurn does what a chat turn does to the history and the log, without a
// model: the line, the assembly, a reply.
func playTurn(t *testing.T, a *Agent, line, reply string, out io.Writer) {
	t.Helper()
	line, reply = strings.TrimSpace(line), strings.TrimSpace(reply) // the turn code trims the reply
	a.logUserInput(line)
	a.logTurnMessage(line)
	a.AddMessage("user", line)
	a.assembleForTurn(out)
	end := a.BeginModelCall(withCallPurpose(context.Background(), purposeTurn, nil), a.History, 0)
	end(reply, ChatStats{}, nil)
	a.AddMessage("assistant", reply)
}

func TestAssembleForTurn_KeepsToTheLimitAndTheLogRebuildsWhatWasSent(t *testing.T) {
	a := limitedLoggedAgent(t, 120)
	var notices strings.Builder
	for i := 1; i <= 8; i++ {
		playTurn(t, a, fmt.Sprintf("question %d %s", i, words(15)), fmt.Sprintf("answer %d %s", i, words(15)), &notices)
	}
	if !strings.Contains(notices.String(), "Trimmed") {
		t.Errorf("a notice should say turns were left out: %q", notices.String())
	}
	if len(a.History) >= 16 {
		t.Errorf("history has %d messages; the limit should have dropped some", len(a.History))
	}
	if a.lastManifest == nil || len(a.lastManifest.Dropped) == 0 || a.lastManifest.Tail.Turns < 1 {
		t.Errorf("manifest = %+v", a.lastManifest)
	}

	// The last request carries the manifest the assembler chose.
	evs, _, _ := ReadSessionLog(a.SessionLog.Path())
	var last struct {
		Policy  string `json:"policy"`
		Dropped int    `json:"dropped"`
		Tail    struct {
			Turns int `json:"turns"`
		} `json:"tail"`
		Budget struct {
			Limit int `json:"limit"`
		} `json:"budget"`
	}
	for _, e := range evs {
		if e.Kind == KindModelRequest {
			_ = json.Unmarshal(e.Data, &last)
		}
	}
	if last.Policy != "assembled" || last.Dropped == 0 || last.Tail.Turns < 1 || last.Budget.Limit != 120 {
		t.Errorf("model_request manifest = %+v", last)
	}

	// The gate: every request's prompt is what the log says it was, including
	// after the turns that were left out.
	events, _, err := ReadSessionLog(a.SessionLog.Path())
	if err != nil {
		t.Fatal(err)
	}
	reqs := RebuildPrompts(events, RenderOptions{Blobs: BlobDir(a.SessionLog.Path())})
	if len(reqs) != 8 {
		t.Fatalf("%d requests, want 8", len(reqs))
	}
	for _, r := range reqs {
		if r.Stored != r.Rebuilt {
			t.Errorf("request %s: rebuilt %.12s, stored %.12s (%d messages)", r.RequestID, r.Rebuilt, r.Stored, len(r.Messages))
		}
	}
}

func TestAssembleForTurn_WithoutALimitItDoesNothingNew(t *testing.T) {
	a := limitedLoggedAgent(t, 0)
	for i := 1; i <= 4; i++ {
		playTurn(t, a, fmt.Sprintf("question %d", i), fmt.Sprintf("answer %d", i), io.Discard)
	}
	if len(a.History) != 9 || a.lastManifest != nil {
		t.Errorf("history %d messages, manifest %v: with no limit nothing is assembled", len(a.History), a.lastManifest)
	}
}

func TestAssembleForTurn_WithoutAPromptLimitItKeepsToAShareOfTheWindow(t *testing.T) {
	a := limitedLoggedAgent(t, 0)
	a.Config.Ollama.ContextLength = 140
	for i := 1; i <= 6; i++ {
		playTurn(t, a, fmt.Sprintf("question %d %s", i, words(15)), fmt.Sprintf("answer %d %s", i, words(15)), io.Discard)
	}
	m := a.lastManifest
	if m == nil || m.LimitSource != "window" || m.Limit != 119 || len(m.Dropped) == 0 {
		t.Fatalf("manifest = %+v, want a limit of 85%% of the 140-token window and turns left out", m)
	}
	events, _, _ := ReadSessionLog(a.SessionLog.Path())
	for _, r := range RebuildPrompts(events, RenderOptions{Blobs: BlobDir(a.SessionLog.Path())}) {
		if r.Stored != r.Rebuilt {
			t.Errorf("request %s does not rebuild from the log", r.RequestID)
		}
	}
}
