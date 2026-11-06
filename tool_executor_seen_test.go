package harvey

import (
	"context"
	"io"
	"strings"
	"testing"
)

// Found in D7 (2026-11-02), a real granite4 session: recall_knowledge returned
// three foulpapers records, the next request carried "[done]" in their place,
// the model asked again, got "[done]" again, and answered from nothing.
// RunToolLoop compacted the round it had just executed before the model's next
// call, so no model ever read a result that was not from read_file.

// seeingToolClient is a ToolCapable client that plays a script and keeps a copy
// of the messages each call was given.
type seeingToolClient struct {
	steps []toolStep
	i     int
	seen  [][]Message
}

func (c *seeingToolClient) Name() string { return "mock (seeing)" }
func (c *seeingToolClient) Chat(_ context.Context, _ []Message, _ io.Writer) (ChatStats, error) {
	return ChatStats{}, nil
}
func (c *seeingToolClient) ChatWithTools(_ context.Context, msgs []Message, _ []Tool, out io.Writer) (ChatStats, []ToolCall, error) {
	c.seen = append(c.seen, append([]Message(nil), msgs...))
	st := c.steps[c.i]
	c.i++
	io.WriteString(out, st.text)
	return ChatStats{}, st.calls, st.err
}
func (c *seeingToolClient) Models(context.Context) ([]string, error) { return nil, nil }
func (c *seeingToolClient) Close() error                             { return nil }

func toolContents(msgs []Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role == "tool" {
			out = append(out, m.Content)
		}
	}
	return out
}

func runSeeing(t *testing.T, steps []toolStep) *seeingToolClient {
	t.Helper()
	reg := NewToolRegistry()
	reg.RegisterTool("lookup", "looks something up", map[string]any{"type": "object"},
		func(_ context.Context, args map[string]any) (string, error) {
			return "RESULT-" + strings.TrimSpace(strings.ReplaceAll(toString(args["q"]), `"`, "")), nil
		})
	c := &seeingToolClient{steps: steps}
	cfg := DefaultConfig()
	if !cfg.ToolResultCompaction {
		t.Fatal("the default config no longer compacts tool results; the test needs it on")
	}
	ex := NewToolExecutor(reg, c, cfg)
	if _, _, err := ex.RunToolLoop(context.Background(), []Message{{Role: "user", Content: "go"}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	return c
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func TestToolLoop_theModelReadsAResultBeforeItIsCompacted(t *testing.T) {
	c := runSeeing(t, []toolStep{
		{calls: []ToolCall{toolCall("c1", "lookup", `{"q":"alpha"}`)}},
		{text: "answer"},
	})
	if len(c.seen) != 2 {
		t.Fatalf("the model was called %d times, want 2", len(c.seen))
	}
	got := toolContents(c.seen[1])
	if len(got) != 1 || got[0] != "RESULT-alpha" {
		t.Errorf("the model's second call saw tool results %q, want [RESULT-alpha]", got)
	}
}

func TestToolLoop_aRoundTheModelHasReadIsCompactedAfterwards(t *testing.T) {
	c := runSeeing(t, []toolStep{
		{calls: []ToolCall{toolCall("c1", "lookup", `{"q":"alpha"}`)}},
		{calls: []ToolCall{toolCall("c2", "lookup", `{"q":"beta"}`)}},
		{text: "answer"},
	})
	if len(c.seen) != 3 {
		t.Fatalf("the model was called %d times, want 3", len(c.seen))
	}
	// Call 2 reads round 1; call 3 reads round 2, and round 1 has been read.
	if got := toolContents(c.seen[1]); len(got) != 1 || got[0] != "RESULT-alpha" {
		t.Errorf("call 2 saw %q, want [RESULT-alpha]", got)
	}
	got := toolContents(c.seen[2])
	if len(got) != 2 || got[0] != "[done]" || got[1] != "RESULT-beta" {
		t.Errorf("call 3 saw %q, want [[done] RESULT-beta]: the older round compacted, the newest intact", got)
	}
}
