package harvey

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/rsdoiel/knowledge"
)

// kbAgentWithLog is an agent with a live knowledge base, a current project and
// a session log open.
func kbAgentWithLog(t *testing.T) (*Agent, int64, string) {
	t.Helper()
	a, pid := newTestAgentWithKB(t)
	a.registerCommands()
	opts := testLogOpts(t)
	opts.Start.Workspace = a.Workspace.Root
	l, err := OpenSessionLog(opts)
	if err != nil {
		t.Fatal(err)
	}
	a.SessionLog = l
	t.Cleanup(func() { a.closeSessionLog("test") })
	return a, pid, logPath(opts)
}

type kbCallData struct {
	Verb    string `json:"verb"`
	Args    string `json:"args"`
	Purpose string `json:"purpose"`
	Turn    *int   `json:"turn"`
}

type kbResultData struct {
	Call   string `json:"call"`
	Status string `json:"status"`
	Items  []struct {
		Ref       string `json:"ref"`
		Kind      string `json:"kind"`
		Retracted *bool  `json:"retracted"`
	} `json:"items"`
	Output string `json:"output"`
}

func kbEvents(t *testing.T, a *Agent, path string) (calls, results, injections []Event) {
	t.Helper()
	return eventsOf(t, a, path, KindKBCall), eventsOf(t, a, path, KindKBResult), eventsOf(t, a, path, KindInjection)
}

func TestKBCommand_SearchIsACallAndAResultNamingWhatItReturned(t *testing.T) {
	a, pid, path := kbAgentWithLog(t)
	if _, err := a.KB.AddObservation(pid, "finding", "chunking is sound on a Pi"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := a.dispatch("/kb search chunking", &out); err != nil {
		t.Fatalf("/kb search: %v", err)
	}
	calls, results, _ := kbEvents(t, a, path)
	if len(calls) != 1 || len(results) != 1 {
		t.Fatalf("%d calls and %d results, want 1 and 1", len(calls), len(results))
	}
	var c kbCallData
	var r kbResultData
	_ = json.Unmarshal(calls[0].Data, &c)
	_ = json.Unmarshal(results[0].Data, &r)
	if c.Verb != "search" || c.Args != "chunking" || c.Purpose != "user_command" || c.Turn != nil {
		t.Errorf("kb_call = %s", calls[0].Data)
	}
	if r.Call != calls[0].ID || r.Status != "ok" || !strings.Contains(r.Output, "chunking is sound") {
		t.Errorf("kb_result = %s", results[0].Data)
	}
	if len(r.Items) != 1 || !strings.HasPrefix(r.Items[0].Ref, "kb:observation:") || r.Items[0].Kind != "observation" {
		t.Errorf("the result must name the identities it returned: %s", results[0].Data)
	}
	if r.Items[0].Retracted != nil {
		t.Errorf("search does not know about retractions and must not claim otherwise: %s", results[0].Data)
	}
	// What the author saw is still what was printed.
	if !strings.Contains(out.String(), "chunking is sound") {
		t.Errorf("output no longer reaches the author: %q", out.String())
	}
}

func TestKBCommand_ShowSaysWhenASourceIsRetracted(t *testing.T) {
	a, pid, path := kbAgentWithLog(t)
	obs, err := a.KB.AddObservation(pid, "finding", "a claim resting on a paper")
	if err != nil {
		t.Fatal(err)
	}
	src, err := a.KB.AddSource(knowledge.Source{Title: "Bad paper", IdentifierType: "doi", IdentifierValue: "10.1/bad"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.KB.LinkObservationSource(obs, src, "cited"); err != nil {
		t.Fatal(err)
	}
	if err := a.KB.RetractSource(src, "withdrawn by the publisher"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.dispatch("/kb show "+strconv.FormatInt(obs, 10), io.Discard); err != nil {
		t.Fatalf("/kb show: %v", err)
	}
	_, results, _ := kbEvents(t, a, path)
	var r kbResultData
	_ = json.Unmarshal(results[0].Data, &r)
	if len(r.Items) != 1 || r.Items[0].Retracted == nil || !*r.Items[0].Retracted {
		t.Errorf("the observation rests on a retracted source and the result must say so: %s", results[0].Data)
	}
}

func TestKBCommand_AFailureIsAResultWithItsClass(t *testing.T) {
	a, _, path := kbAgentWithLog(t)
	if _, err := a.dispatch("/kb show 99999", io.Discard); err == nil {
		t.Fatal("want an error for a missing observation")
	}
	calls, results, _ := kbEvents(t, a, path)
	if len(calls) != 1 || len(results) != 1 {
		t.Fatalf("a failed kb command must still be a call and a result: %d/%d", len(calls), len(results))
	}
	var r kbResultData
	_ = json.Unmarshal(results[0].Data, &r)
	if r.Status != "negative" {
		t.Errorf("status = %q, want the exit class name negative", r.Status)
	}
}

func TestKBCommand_UnknownSubcommandIsNotAKBCall(t *testing.T) {
	a, _, path := kbAgentWithLog(t)
	_, _ = a.dispatch("/kb frobnicate", io.Discard)
	calls, results, _ := kbEvents(t, a, path)
	if len(calls) != 0 || len(results) != 0 {
		t.Errorf("nothing was asked of the knowledge base, yet %d calls and %d results were logged", len(calls), len(results))
	}
}

func TestKBCommand_ArgumentsAreScrubbed(t *testing.T) {
	a, _, path := kbAgentWithLog(t)
	_, _ = a.dispatch("/kb observe note API_KEY=sk-live-555", io.Discard)
	calls, results, _ := kbEvents(t, a, path)
	all := string(calls[0].Data) + string(results[0].Data)
	if strings.Contains(all, "sk-live-555") {
		t.Errorf("a secret reached the log: %s", all)
	}
}

func TestKBCommand_InjectLogsWhatEnteredThePrompt(t *testing.T) {
	a, pid, path := kbAgentWithLog(t)
	if _, err := a.KB.AddObservation(pid, "note", "injected fact"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.dispatch("/kb inject", io.Discard); err != nil {
		t.Fatalf("/kb inject: %v", err)
	}
	_, results, injections := kbEvents(t, a, path)
	if len(injections) != 1 {
		t.Fatalf("%d injections, want 1", len(injections))
	}
	var inj struct {
		Source string  `json:"source"`
		Origin string  `json:"origin"`
		From   string  `json:"from"`
		Text   Content `json:"text"`
	}
	_ = json.Unmarshal(injections[0].Data, &inj)
	if inj.Source != "kb" || inj.Origin != "user" || inj.From != results[0].ID {
		t.Errorf("injection = %s, want source kb, origin user, from %s", injections[0].Data, results[0].ID)
	}
	if inj.Text.Blob == "" && !strings.Contains(inj.Text.Text, "injected fact") {
		t.Errorf("the injected text was not logged: %s", injections[0].Data)
	}
}

// Recall at the start of a turn: the knowledge base is asked, it answers with
// rows (one resting on a retracted source), and they enter the prompt.
func TestRecall_IsAKBCallItsResultAndOneInjectionPerRowThatEnteredThePrompt(t *testing.T) {
	a, pid, path := kbAgentWithLog(t)
	cid, _ := a.KB.AddConcept("chunking", "")
	obs, err := a.KB.AddObservation(pid, "finding", "map-reduce chunking is sound on CPU-only hardware")
	if err != nil {
		t.Fatal(err)
	}
	_ = a.KB.LinkObservationConcept(obs, cid)
	src, _ := a.KB.AddSource(knowledge.Source{Title: "Withdrawn paper", IdentifierType: "doi", IdentifierValue: "10.1/w"})
	_ = a.KB.LinkObservationSource(obs, src, "cited")
	_ = a.KB.RetractSource(src, "withdrawn")

	store, err := NewMemoryStore(a.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	a.Config.Memory.Enabled, a.Config.Memory.InjectOnStart = true, true
	a.Config.Memory.KnowledgeDB = knowledge.DefaultPath(a.Workspace.Root)
	a.Memory = &MemorySystem{Store: store, Unified: NewUnifiedMemory(store, &a.Config.Memory, a.Workspace)}
	a.logUserInput("how does chunking behave on a Pi?")
	a.injectMemoryContext("how does chunking behave on a Pi?")

	calls, results, injections := kbEvents(t, a, path)
	if len(calls) != 1 || len(results) != 1 || len(injections) != 1 {
		t.Fatalf("%d calls, %d results, %d injections; want 1 each", len(calls), len(results), len(injections))
	}
	var c kbCallData
	var r kbResultData
	_ = json.Unmarshal(calls[0].Data, &c)
	_ = json.Unmarshal(results[0].Data, &r)
	if c.Purpose != "retrieval" || c.Turn == nil || *c.Turn != 1 || !strings.Contains(c.Args, "chunking") {
		t.Errorf("recall call = %s", calls[0].Data)
	}
	if len(r.Items) != 1 || r.Items[0].Retracted == nil || !*r.Items[0].Retracted {
		t.Errorf("the recalled row rests on a retracted source and the result must say so: %s", results[0].Data)
	}
	var inj struct {
		Source    string `json:"source"`
		Origin    string `json:"origin"`
		Ref       string `json:"ref"`
		From      string `json:"from"`
		Retracted bool   `json:"retracted"`
		Turn      int    `json:"turn"`
		Text      string `json:"text"`
	}
	_ = json.Unmarshal(injections[0].Data, &inj)
	// The log must show the model was told, not only that the row was retracted.
	if !strings.Contains(inj.Text, "RETRACTED: withdrawn") || !strings.Contains(inj.Text, "Withdrawn paper") {
		t.Errorf("the injected text does not carry the retraction and its note: %q", inj.Text)
	}
	if inj.Source != "kb" || inj.Origin != "auto" || inj.From != results[0].ID || !inj.Retracted || inj.Turn != 1 || inj.Ref != r.Items[0].Ref {
		t.Errorf("injection = %s", injections[0].Data)
	}
}

func TestRecall_NothingFoundIsStillACallAndAResult(t *testing.T) {
	a, _, path := kbAgentWithLog(t)
	store, err := NewMemoryStore(a.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	a.Config.Memory.Enabled, a.Config.Memory.InjectOnStart = true, true
	a.Config.Memory.KnowledgeDB = knowledge.DefaultPath(a.Workspace.Root)
	a.Memory = &MemorySystem{Store: store, Unified: NewUnifiedMemory(store, &a.Config.Memory, a.Workspace)}
	a.injectMemoryContext("a question the knowledge base knows nothing about")
	calls, results, injections := kbEvents(t, a, path)
	if len(calls) != 1 || len(results) != 1 || len(injections) != 0 {
		t.Errorf("%d calls, %d results, %d injections; want 1, 1, 0", len(calls), len(results), len(injections))
	}
	var r kbResultData
	_ = json.Unmarshal(results[0].Data, &r)
	if r.Status != "ok" || len(r.Items) != 0 {
		t.Errorf("kb_result = %s", results[0].Data)
	}
}

func TestKB_NoLogIsNoProblem(t *testing.T) {
	a, _ := newTestAgentWithKB(t)
	a.registerCommands()
	var out bytes.Buffer
	if _, err := a.dispatch("/kb status", &out); err != nil {
		t.Fatalf("/kb status with no log: %v", err)
	}
}
