package harvey

import (
	"bytes"
	"encoding/json"
	"io"
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
	// The injection events that record a message added to the prompt (DR-0048)
	// are the prompt's business, not the knowledge base's: skip them here.
	var inj []Event
	for _, e := range eventsOf(t, a, path, KindInjection) {
		var d struct {
			Source string `json:"source"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Source != "prompt" {
			inj = append(inj, e)
		}
	}
	return eventsOf(t, a, path, KindKBCall), eventsOf(t, a, path, KindKBResult), inj
}

func TestKBCommand_ArgumentsAreScrubbed(t *testing.T) {
	a, _, path := kbAgentWithLog(t)
	recordingKB(t)
	_, _ = a.dispatch("/kb observation add API_KEY=sk-live-555", io.Discard)
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
	recordingKB(t)
	a, _ := newTestAgentWithKB(t)
	a.registerCommands()
	var out bytes.Buffer
	if _, err := a.dispatch("/kb stats", &out); err != nil {
		t.Fatalf("/kb stats with no log: %v", err)
	}
}
