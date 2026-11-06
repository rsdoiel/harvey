package harvey

import (
	"context"
	"io"
	"testing"
)

// A draft pass asks for at most a handful of one-line proposals. D7 on hailo
// llama3.2:3b (2026-11-02) let the model run to the server's own stopping point
// of 1,024 tokens: it repeated five lines eight times, 40 proposals, 391 s.
// The request therefore carries num_predict, through the real path: RunDraftPass
// -> llmDrafter -> OllamaChatClient -> HTTP.

func draftCapPass() DraftPass {
	return DraftPass{First: "s#0001", Last: "s#0002", Entries: []TranscriptEntry{
		{First: "s#0001", Last: "s#0001", Text: "author: why is bat not found?"},
	}}
}

func sentNumPredict(t *testing.T, f *chatFake) (float64, bool) {
	t.Helper()
	opts, _ := f.last["options"].(map[string]any)
	v, ok := opts["num_predict"].(float64)
	return v, ok
}

func TestDraftPass_requestCarriesTheTokenCap(t *testing.T) {
	f := newChatFake(t, chatDone)
	c := newOllamaLLMClient(f.srv.URL, "m", 0)
	_, _, err := RunDraftPass(context.Background(), newLLMDrafter(c, ""), draftCapPass(), "harvey", "s", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := sentNumPredict(t, f)
	if !ok || got != float64(DraftMaxTokens) {
		t.Errorf("num_predict = %v (sent: %v), want %d", got, ok, DraftMaxTokens)
	}
}

func TestDraftPass_aLowerClientCapStillWins(t *testing.T) {
	f := newChatFake(t, chatDone)
	c := newOllamaLLMClient(f.srv.URL, "m", 0)
	c.SetMaxTokens(100)
	_, _, err := RunDraftPass(context.Background(), newLLMDrafter(c, ""), draftCapPass(), "harvey", "s", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := sentNumPredict(t, f); got != 100 {
		t.Errorf("num_predict = %v, want the client's own lower cap, 100", got)
	}
}

func TestDraftPass_theCapIsNotLeftOnOrdinaryChat(t *testing.T) {
	f := newChatFake(t, chatDone)
	c := newOllamaLLMClient(f.srv.URL, "m", 0)
	if _, _, err := RunDraftPass(context.Background(), newLLMDrafter(c, ""), draftCapPass(), "harvey", "s", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chat(context.Background(), userMsg("hi"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if v, ok := sentNumPredict(t, f); ok {
		t.Errorf("an ordinary chat after a draft pass sent num_predict %v", v)
	}
}
