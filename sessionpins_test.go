package harvey

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rebuiltMatches(t *testing.T, a *Agent) {
	t.Helper()
	events, _, err := ReadSessionLog(a.SessionLog.Path())
	if err != nil {
		t.Fatal(err)
	}
	items := RebuildItems(events, RenderOptions{Blobs: BlobDir(a.SessionLog.Path())})
	if !sameMessages(items, a.History) {
		t.Errorf("the history is not what the log rebuilds: %d messages in the history, %d rebuilt", len(a.History), len(items))
	}
}

func TestPin_TextIsAPinEventAndAMessageAheadOfTheDialog(t *testing.T) {
	a, path := agentWithLog(t)
	var out strings.Builder
	a.AddMessage("system", "You are Harvey.")
	if err := cmdPin(a, []string{"--text", "keep", "answers", "short"}, &out); err != nil {
		t.Fatal(err)
	}
	a.logUserInput("hello")
	a.logTurnMessage("hello")
	a.AddMessage("user", "hello")

	if len(a.Pins) != 1 || a.Pins[0].Ref != "text" || a.Pins[0].Tokens == 0 {
		t.Fatalf("pins = %+v", a.Pins)
	}
	pins := eventsOf(t, a, path, KindPin)
	if len(pins) != 1 || !strings.Contains(string(pins[0].Data), `"ref":"text"`) || a.Pins[0].ID != pins[0].ID {
		t.Errorf("pin events = %v, pin = %+v", pins, a.Pins[0])
	}
	if got := a.History[len(a.History)-2].Content; got != "[pinned context]\n\nkeep answers short" {
		t.Errorf("pinned message = %q", got)
	}
	if got := a.SessionLog.Index().Pins(); len(got) != 1 || got[0] != pins[0].ID {
		t.Errorf("the index's pins = %v", got)
	}
	// The assembler keeps the pin ahead of everything but the system prompt.
	msgs, man := Assemble(a.currentItems(), AssembleBudget{})
	if msgs[0].Role != "system" || !strings.HasPrefix(msgs[1].Content, pinPrefix) || len(man.Pinned) != 1 {
		t.Errorf("assembled order = %s, manifest %+v", roles(msgs), man)
	}
	rebuiltMatches(t, a)
}

func TestPin_AnEventOfTheSessionByItsId(t *testing.T) {
	a, _ := agentWithLog(t)
	a.logUserInput("the constraint: no cgo")
	id := a.logLastID
	if err := cmdPin(a, []string{id}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := a.Pins[0].Message; got != "[pinned context]\n\n("+id+")\nthe constraint: no cgo" {
		t.Errorf("message = %q", got)
	}
	if err := cmdPin(a, []string{"nosuch#0001"}, io.Discard); ExitCodeFor(err) != ClassNegative {
		t.Errorf("an unknown event: %v", err)
	}
}

// fakeKB puts a kb on PATH that answers the two verbs /pin uses.
func fakeKB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  "record show harvey/DR-0048") echo "harvey/DR-0048 accepted: the log can rebuild the prompt" ;;
  "--json observation show 592") echo '{"ID":592,"Body":"index rebuilds in 16 ms"}' ;;
  *) echo "no such row" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kb"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPin_AKnowledgeBaseRowByItsIdentity(t *testing.T) {
	fakeKB(t)
	a, _ := agentWithLog(t)
	if err := cmdPin(a, []string{"kb:record:harvey/DR-0048"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cmdPin(a, []string{"kb:observation:592"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.Pins[0].Message, "the log can rebuild the prompt") || !strings.Contains(a.Pins[1].Message, "index rebuilds in 16 ms") {
		t.Errorf("pins = %+v", a.Pins)
	}
	if err := cmdPin(a, []string{"kb:record:harvey/DR-9999"}, io.Discard); ExitCodeFor(err) != ClassNegative {
		t.Errorf("a row kb does not have: %v", err)
	}
	if err := cmdPin(a, []string{"kb:concept:3"}, io.Discard); ExitCodeFor(err) != ClassUsage {
		t.Errorf("a kind that cannot be pinned: %v", err)
	}
}

func TestPin_BadArgumentsAreUsageErrors(t *testing.T) {
	a, _ := agentWithLog(t)
	for _, args := range [][]string{nil, {"--text"}, {"somewhere"}, {"a#1", "b#2"}} {
		if err := cmdPin(a, args, io.Discard); ExitCodeFor(err) != ClassUsage {
			t.Errorf("/pin %v: %v, want usage", args, err)
		}
	}
	for _, args := range [][]string{nil, {"9"}, {"a", "b"}} {
		if err := cmdUnpin(a, args, io.Discard); ExitCodeFor(err) != ClassUsage {
			t.Errorf("/unpin %v: %v, want usage", args, err)
		}
	}
}

func TestPin_OverTheCapIsRefusedAndListsThePins(t *testing.T) {
	a := limitedLoggedAgent(t, 0)
	a.Config.Ollama.ContextLength = 400 // a limit of 340 tokens, so the pins may take 85
	if err := cmdPin(a, []string{"--text", words(20)}, io.Discard); err != nil {
		t.Fatal(err)
	}
	err := cmdPin(a, []string{"--text", words(60)}, io.Discard)
	if ExitCodeFor(err) != ClassNegative {
		t.Fatalf("a pin over the cap: %v, want negative", err)
	}
	if !strings.Contains(err.Error(), "may take 85") || !strings.Contains(err.Error(), "1  text") || !strings.Contains(err.Error(), "/unpin") {
		t.Errorf("the refusal should name the cap and list the pins: %v", err)
	}
	if len(a.Pins) != 1 {
		t.Errorf("a refused pin must not be added: %+v", a.Pins)
	}
}

func TestUnpin_TakesTheMessageOutAndLogsIt(t *testing.T) {
	a, path := agentWithLog(t)
	a.AddMessage("system", "You are Harvey.")
	_ = cmdPin(a, []string{"--text", "first"}, io.Discard)
	_ = cmdPin(a, []string{"--text", "second"}, io.Discard)
	first := a.Pins[0].ID

	if err := cmdUnpin(a, []string{"1"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(a.Pins) != 1 || a.Pins[0].Text != "second" {
		t.Errorf("pins = %+v", a.Pins)
	}
	for _, m := range a.History {
		if strings.Contains(m.Content, "first") {
			t.Errorf("the unpinned message is still in the prompt: %q", m.Content)
		}
	}
	if un := eventsOf(t, a, path, KindUnpin); len(un) != 1 || !strings.Contains(string(un[0].Data), first) {
		t.Errorf("unpin events = %v", un)
	}
	if got := a.SessionLog.Index().Pins(); len(got) != 1 {
		t.Errorf("the index's pins = %v", got)
	}
	rebuiltMatches(t, a)
	// By id and by ref as well.
	if err := cmdUnpin(a, []string{a.Pins[0].ID}, io.Discard); err != nil {
		t.Errorf("unpin by id: %v", err)
	}
}

func TestPins_SurviveClearAndAreInThePromptAgain(t *testing.T) {
	a, _ := agentWithLog(t)
	a.Config.SystemPrompt = "You are Harvey."
	_ = cmdPin(a, []string{"--text", "target: macOS arm64"}, io.Discard)
	a.AddMessage("user", "hello")
	a.AddMessage("assistant", "hi")
	a.ClearHistory()
	if len(a.Pins) != 1 {
		t.Fatalf("pins after /clear = %+v", a.Pins)
	}
	found := false
	for _, m := range a.History {
		if strings.Contains(m.Content, "target: macOS arm64") {
			found = true
		}
	}
	if !found {
		t.Error("the pin should be in the prompt after /clear")
	}
	rebuiltMatches(t, a)
}

func TestPins_AFreshAgentHasNone(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdPins(a, nil, &out); err != nil || len(a.Pins) != 0 || !strings.Contains(out.String(), "No pins") {
		t.Errorf("err %v, pins %v, out %q", err, a.Pins, out.String())
	}
}

func TestPins_ListsCostAndShare(t *testing.T) {
	a := limitedLoggedAgent(t, 0)
	a.Config.Ollama.ContextLength = 400
	_ = cmdPin(a, []string{"--text", words(20)}, io.Discard)
	var out strings.Builder
	_ = cmdPins(a, nil, &out)
	if !strings.Contains(out.String(), "~") || !strings.Contains(out.String(), "%") || !strings.Contains(out.String(), "of the 85") {
		t.Errorf("/pins = %q", out.String())
	}
}

func TestContext_ShowsWhatTheModelSeesAndTheOldSubcommandsAreGone(t *testing.T) {
	a := limitedLoggedAgent(t, 0)
	a.Config.Ollama.ContextLength = 353
	_ = cmdPin(a, []string{"--text", "keep it short"}, io.Discard)
	for i := 1; i <= 8; i++ {
		playTurn(t, a, "question "+words(12), "answer "+words(12), io.Discard)
	}
	var out strings.Builder
	if err := cmdContext(a, nil, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"What the model sees", "limit ~300", "system prompt", "pinned", "dialog", "Total"} {
		if !strings.Contains(got, want) {
			t.Errorf("/context lacks %q:\n%s", want, got)
		}
	}
	for _, sub := range []string{"add", "clear", "show"} {
		if err := cmdContext(a, []string{sub, "x"}, io.Discard); ExitCodeFor(err) != ClassUsage || !strings.Contains(err.Error(), "/pin") {
			t.Errorf("/context %s: %v, want a usage error that points at /pin", sub, err)
		}
	}
}
