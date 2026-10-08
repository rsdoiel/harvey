package harvey

import (
	"context"
	"strings"
	"testing"
)

// Bug (kb 381): the write offers take the next line of input as their answer,
// whatever it is. A piped "/exit" became the path "exit" and an empty file was
// created; at the box prompt any unrecognised text counted as yes. An answer
// that is a command, or the end of input, must never write a file.

const offerUntaggedReply = "Here you go:\n\n```bash\necho hi\n```\n"
const offerTaggedReply = "Here you go:\n\n```bash:out/run.sh\necho hi\n```\n"

func fileExists(a *Agent, rel string) bool {
	_, err := a.Workspace.ReadFile(rel)
	return err == nil
}

// chatAgent is a test agent whose backend answers every turn with reply.
func chatAgent(t *testing.T, reply string) *Agent {
	t.Helper()
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: reply}
	return a
}

func TestWriteOffer_CommandAtPathPrompt_WritesNothingAndIsQueued(t *testing.T) {
	a := chatAgent(t, offerUntaggedReply)
	var out strings.Builder

	if _, _, err := a.runChatTurn(context.Background(), "hi", &out, newReader("/exit\n"), true, ""); err != nil {
		t.Fatalf("runChatTurn: %v", err)
	}
	if fileExists(a, "exit") {
		t.Error("the command /exit was taken as a path and a file named exit was written")
	}
	if got := a.pendingInput; len(got) != 1 || got[0] != "/exit" {
		t.Errorf("the command should be handed back to the REPL, pendingInput = %q", got)
	}
	if !strings.Contains(out.String(), "command") {
		t.Errorf("the user should be told the line was taken as a command: %q", out.String())
	}
}

func TestWriteOffer_EndOfInputAtPathPrompt_WritesNothingAndAsksNoMore(t *testing.T) {
	a := chatAgent(t, "One:\n\n```bash\necho 1\n```\n\nTwo:\n\n```bash\necho 2\n```\n")
	var out strings.Builder

	if _, _, err := a.runChatTurn(context.Background(), "hi", &out, newReader(""), true, ""); err != nil {
		t.Fatalf("runChatTurn: %v", err)
	}
	if n := strings.Count(out.String(), "Write bash block"); n != 1 {
		t.Errorf("after the input ended the second offer was still made (%d offers)", n)
	}
	if len(a.pendingInput) != 0 {
		t.Errorf("end of input queues nothing, got %q", a.pendingInput)
	}
}

func TestWriteOffer_PathStillWorks_AndSecondAnswerCanBeACommand(t *testing.T) {
	a := chatAgent(t, "One:\n\n```bash\necho 1\n```\n\nTwo:\n\n```bash\necho 2\n```\n")
	var out strings.Builder

	if _, _, err := a.runChatTurn(context.Background(), "hi", &out, newReader("one.sh\n/exit\n"), true, ""); err != nil {
		t.Fatalf("runChatTurn: %v", err)
	}
	if !fileExists(a, "one.sh") {
		t.Error("a real path must still be written")
	}
	if fileExists(a, "exit") {
		t.Error("the command at the second offer was written as a file")
	}
	if len(a.pendingInput) != 1 || a.pendingInput[0] != "/exit" {
		t.Errorf("pendingInput = %q", a.pendingInput)
	}
}

func TestWriteOffer_AbsolutePathIsStillAPath(t *testing.T) {
	a := chatAgent(t, offerUntaggedReply)
	if _, _, err := a.runChatTurn(context.Background(), "hi", &strings.Builder{}, newReader("/out/run.sh\n"), true, ""); err != nil {
		t.Fatal(err)
	}
	if !fileExists(a, "out/run.sh") || len(a.pendingInput) != 0 {
		t.Errorf("a path with a directory is not a command (written=%v, queued=%q)", fileExists(a, "out/run.sh"), a.pendingInput)
	}
}

func TestAutoExecuteReply_CommandAtBoxPrompt_WritesNothingAndIsQueued(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder

	a.autoExecuteReply(offerTaggedReply, &out, newReader("/exit\n"), context.Background())

	if fileExists(a, "out/run.sh") {
		t.Error("an unrecognised answer counted as yes: the file was written")
	}
	if len(a.pendingInput) != 1 || a.pendingInput[0] != "/exit" {
		t.Errorf("pendingInput = %q", a.pendingInput)
	}
}

func TestAutoExecuteReply_UnrecognisedAnswerIsNotYes(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder

	a.autoExecuteReply(offerTaggedReply, &out, newReader("maybe\n"), context.Background())

	if fileExists(a, "out/run.sh") {
		t.Error("\"maybe\" was taken as yes")
	}
	if !strings.Contains(out.String(), "maybe") {
		t.Errorf("the user should be told the answer was not understood: %q", out.String())
	}
	if len(a.pendingInput) != 0 {
		t.Errorf("text that is not a command is not queued, got %q", a.pendingInput)
	}
}

func TestAutoExecuteReply_UntaggedPathPrompt_CommandIsNotAPath(t *testing.T) {
	a := newTestAgent(t)
	a.autoExecuteReply(offerUntaggedReply, &strings.Builder{}, newReader("/exit\n"), context.Background())
	if fileExists(a, "exit") {
		t.Error("the command was written as a file")
	}
	if len(a.pendingInput) != 1 {
		t.Errorf("pendingInput = %q", a.pendingInput)
	}
}

func TestLooksLikeCommand(t *testing.T) {
	a := newTestAgent(t)
	a.commands["model"] = &Command{}
	cases := map[string]bool{
		"/exit":          true,
		"/quit":          true,
		"/model list":    true,
		"/MODEL":         true,
		"!ls":            true,
		"/out/run.sh":    false,
		"/nosuchcommand": false,
		"out.sh":         false,
		"":               false,
		"/":              false,
		"!":              false,
	}
	for line, want := range cases {
		if got := a.looksLikeCommand(line); got != want {
			t.Errorf("looksLikeCommand(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestNextQueuedInput_ServesQueueBeforeTheKeyboard(t *testing.T) {
	a := newTestAgent(t)
	a.pendingInput = []string{"/exit", "/help"}
	if line, ok := a.nextQueuedInput(); !ok || line != "/exit" {
		t.Errorf("first = %q, %v", line, ok)
	}
	if line, ok := a.nextQueuedInput(); !ok || line != "/help" {
		t.Errorf("second = %q, %v", line, ok)
	}
	if _, ok := a.nextQueuedInput(); ok {
		t.Error("an empty queue must say so")
	}
}

func TestWriteOffer_QueuedCommandStopsEveryLaterOffer(t *testing.T) {
	// The reply has an untagged block: runChatTurn offers it, then autoExecuteReply
	// offers it again. A command given at the first must end both.
	a := chatAgent(t, offerUntaggedReply)
	var out strings.Builder

	if _, _, err := a.runChatTurn(context.Background(), "hi", &out, newReader("/exit\n"), true, ""); err != nil {
		t.Fatalf("runChatTurn: %v", err)
	}
	if strings.Contains(out.String(), "Untagged code block") {
		t.Errorf("a second offer was made after the user gave a command:\n%s", out.String())
	}
	if len(a.pendingInput) != 1 {
		t.Errorf("pendingInput = %q", a.pendingInput)
	}
}
