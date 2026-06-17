package harvey

import (
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

func TestCompactToolRound_basic(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "create a file"},
		{Role: "assistant", ToolCalls: []anyllm.ToolCall{
			{ID: "c1", Function: anyllm.FunctionCall{Name: "write_file"}},
		}},
		{Role: "tool", Content: "wrote 512 bytes to demo/app.js", ToolCallID: "c1"},
	}

	compactToolRound(history, 1)

	if history[1].ToolCalls != nil {
		t.Error("expected ToolCalls to be nil after compaction")
	}
	if history[1].Content != "[called: write_file]" {
		t.Errorf("unexpected assistant content: %q", history[1].Content)
	}
	if history[2].Content != "[done]" {
		t.Errorf("unexpected tool content: %q", history[2].Content)
	}
	if history[2].ToolCallID != "c1" {
		t.Error("ToolCallID must be preserved after compaction")
	}
	// User message before round must be untouched.
	if history[0].Content != "create a file" {
		t.Error("user message should not be modified")
	}
}

func TestCompactToolRound_multipleTools(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "create files"},
		{Role: "assistant", ToolCalls: []anyllm.ToolCall{
			{ID: "c1", Function: anyllm.FunctionCall{Name: "create_dir"}},
			{ID: "c2", Function: anyllm.FunctionCall{Name: "write_file"}},
		}},
		{Role: "tool", Content: "created directory demo", ToolCallID: "c1"},
		{Role: "tool", Content: "wrote 200 bytes to demo/index.html", ToolCallID: "c2"},
	}

	compactToolRound(history, 1)

	if history[1].Content != "[called: create_dir, write_file]" {
		t.Errorf("unexpected assistant content: %q", history[1].Content)
	}
	if history[2].Content != "[done]" || history[2].ToolCallID != "c1" {
		t.Error("first tool result not compacted correctly")
	}
	if history[3].Content != "[done]" || history[3].ToolCallID != "c2" {
		t.Error("second tool result not compacted correctly")
	}
}

func TestCompactToolRound_onlyCompactsTargetRound(t *testing.T) {
	history := []Message{
		{Role: "assistant", ToolCalls: []anyllm.ToolCall{
			{ID: "c1", Function: anyllm.FunctionCall{Name: "write_file"}},
		}},
		{Role: "tool", Content: "wrote 100 bytes to a.txt", ToolCallID: "c1"},
		// Second round — must NOT be touched.
		{Role: "assistant", ToolCalls: []anyllm.ToolCall{
			{ID: "c2", Function: anyllm.FunctionCall{Name: "write_file"}},
		}},
		{Role: "tool", Content: "wrote 200 bytes to b.txt", ToolCallID: "c2"},
	}

	compactToolRound(history, 0) // compact only round 0

	if history[0].Content != "[called: write_file]" {
		t.Errorf("round 0 assistant not compacted: %q", history[0].Content)
	}
	if history[1].Content != "[done]" {
		t.Errorf("round 0 tool not compacted: %q", history[1].Content)
	}
	// Round 1 messages must remain untouched.
	if history[2].ToolCalls == nil {
		t.Error("round 1 assistant ToolCalls should not be cleared")
	}
	if history[3].Content == "[done]" {
		t.Error("round 1 tool content should not be compacted")
	}
}

func TestCompactToolRound_noopOnBadIndex(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "hello"},
	}
	// Should not panic on out-of-range or negative index.
	compactToolRound(history, -1)
	compactToolRound(history, 99)
	if history[0].Content != "hello" {
		t.Error("history should not be modified for bad indices")
	}
}
