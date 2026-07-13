package harvey

import (
	"context"
	"strings"
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// registryWithEcho returns a ToolRegistry with a single "echo" tool that
// returns its "msg" argument, for use by executeAndReportToolCalls tests.
func registryWithEcho() *ToolRegistry {
	r := NewToolRegistry()
	r.RegisterTool("echo", "echo back", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		msg, _ := args["msg"].(string)
		return msg, nil
	})
	return r
}

func toolAgent(t *testing.T, reg *ToolRegistry) *Agent {
	t.Helper()
	a := newTestAgent(t)
	a.Tools = reg
	a.Config.ToolsEnabled = true
	return a
}

// ─── executeAndReportToolCalls ───────────────────────────────────────────────

func TestExecuteAndReportToolCalls_dispatchesKnownTool(t *testing.T) {
	a := toolAgent(t, registryWithEcho())
	calls := []anyllm.ToolCall{
		{ID: "c1", Function: anyllm.FunctionCall{Name: "echo", Arguments: `{"msg":"hi"}`}},
	}
	var out strings.Builder
	dispatched, unknown := executeAndReportToolCalls(a, calls, &out)

	if !dispatched {
		t.Error("expected dispatched=true for a known, successful tool call")
	}
	if len(unknown) != 0 {
		t.Errorf("expected no unknown tools, got %v", unknown)
	}
	if !strings.Contains(out.String(), "[echo]") || !strings.Contains(out.String(), "hi") {
		t.Errorf("expected output to show tool name and result, got %q", out.String())
	}
	if strings.Contains(out.String(), "Unknown tool") {
		t.Error("did not expect an Unknown tool warning for a known tool")
	}
}

func TestExecuteAndReportToolCalls_unknownToolWarnsWithAvailableList(t *testing.T) {
	a := toolAgent(t, registryWithEcho())
	calls := []anyllm.ToolCall{
		{ID: "c1", Function: anyllm.FunctionCall{Name: "does_not_exist", Arguments: `{}`}},
	}
	var out strings.Builder
	dispatched, unknown := executeAndReportToolCalls(a, calls, &out)

	if dispatched {
		t.Error("expected dispatched=false when the only call is to an unknown tool")
	}
	if len(unknown) != 1 || unknown[0] != "does_not_exist" {
		t.Errorf("expected unknown=[does_not_exist], got %v", unknown)
	}
	if !strings.Contains(out.String(), "Unknown tool(s): does_not_exist") {
		t.Errorf("expected Unknown tool warning, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Available tools: echo") {
		t.Errorf("expected available-tools list to include echo, got %q", out.String())
	}
}

// ─── tryExecuteProseToolCalls ────────────────────────────────────────────────

func TestTryExecuteProseToolCalls_toolsDisabled(t *testing.T) {
	a := toolAgent(t, registryWithEcho())
	a.Config.ToolsEnabled = false
	blocks := extractCodeBlocks("```json\n{\"name\": \"echo\", \"arguments\": {\"msg\": \"hi\"}}\n```")

	var out strings.Builder
	dispatched, unknown := tryExecuteProseToolCalls(a, blocks, &out)

	if dispatched || unknown != nil || out.String() != "" {
		t.Errorf("expected a no-op when tools are disabled, got dispatched=%v unknown=%v out=%q", dispatched, unknown, out.String())
	}
}

func TestTryExecuteProseToolCalls_unknownToolWarns(t *testing.T) {
	a := toolAgent(t, registryWithEcho())
	blocks := extractCodeBlocks("```json\n{\"name\": \"does_not_exist\", \"arguments\": {}}\n```")

	var out strings.Builder
	dispatched, unknown := tryExecuteProseToolCalls(a, blocks, &out)

	if dispatched {
		t.Error("expected dispatched=false for an unknown tool")
	}
	if len(unknown) != 1 || unknown[0] != "does_not_exist" {
		t.Errorf("expected unknown=[does_not_exist], got %v", unknown)
	}
	if !strings.Contains(out.String(), "Unknown tool(s): does_not_exist") {
		t.Errorf("expected Unknown tool warning, got %q", out.String())
	}
}

// ─── tryExecuteApertusToolCalls ──────────────────────────────────────────────

func TestTryExecuteApertusToolCalls_dispatchesKnownTool(t *testing.T) {
	a := toolAgent(t, registryWithEcho())
	text := `<SPECIAL_71>[{"echo": {"msg": "hi"}}]<SPECIAL_72>`

	var out strings.Builder
	dispatched, unknown := tryExecuteApertusToolCalls(a, text, &out)

	if !dispatched {
		t.Error("expected dispatched=true for a known, successful tool call")
	}
	if len(unknown) != 0 {
		t.Errorf("expected no unknown tools, got %v", unknown)
	}
}

// TestTryExecuteApertusToolCalls_unknownToolWarns locks in the 2026-07-12
// decision to make the Apertus tool-call path consistent with the prose
// path: both must print an immediate "Unknown tool(s)" terminal warning,
// not just feed the correction into history silently.
func TestTryExecuteApertusToolCalls_unknownToolWarns(t *testing.T) {
	a := toolAgent(t, registryWithEcho())
	text := `<SPECIAL_71>[{"does_not_exist": {}}]<SPECIAL_72>`

	var out strings.Builder
	dispatched, unknown := tryExecuteApertusToolCalls(a, text, &out)

	if dispatched {
		t.Error("expected dispatched=false for an unknown tool")
	}
	if len(unknown) != 1 || unknown[0] != "does_not_exist" {
		t.Errorf("expected unknown=[does_not_exist], got %v", unknown)
	}
	if !strings.Contains(out.String(), "Unknown tool(s): does_not_exist") {
		t.Errorf("expected Unknown tool warning (now consistent with the prose path), got %q", out.String())
	}
}

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

// TestCompactToolRound_preservesReadFileResult is the regression test for the
// loop where ToolResultCompaction erased read_file content, causing the model
// to call read_file again on the next iteration — producing a loop.
func TestCompactToolRound_preservesReadFileResult(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "review this file"},
		{Role: "assistant", ToolCalls: []anyllm.ToolCall{
			{ID: "c1", Function: anyllm.FunctionCall{Name: "read_file"}},
		}},
		{Role: "tool", Content: "the actual 12 KB file content goes here", ToolCallID: "c1"},
	}

	compactToolRound(history, 1)

	// Assistant message is still compacted (ToolCalls cleared, summary set).
	if history[1].Content != "[called: read_file]" {
		t.Errorf("assistant content wrong: %q", history[1].Content)
	}
	// Tool result must NOT be erased — the model needs this content.
	if history[2].Content == "[done]" {
		t.Error("read_file result must not be compacted to [done]")
	}
	if history[2].Content != "the actual 12 KB file content goes here" {
		t.Errorf("read_file content changed unexpectedly: %q", history[2].Content)
	}
	if history[2].ToolCallID != "c1" {
		t.Error("ToolCallID must be preserved after compaction")
	}
}

// TestCompactToolRound_mixedContentAndActionTools verifies that in a round
// containing both content tools (read_file) and action tools (write_file),
// only the action tool result is compacted.
func TestCompactToolRound_mixedContentAndActionTools(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "read then write"},
		{Role: "assistant", ToolCalls: []anyllm.ToolCall{
			{ID: "c1", Function: anyllm.FunctionCall{Name: "read_file"}},
			{ID: "c2", Function: anyllm.FunctionCall{Name: "write_file"}},
		}},
		{Role: "tool", Content: "file contents that must survive", ToolCallID: "c1"},
		{Role: "tool", Content: "wrote 100 bytes to output.txt", ToolCallID: "c2"},
	}

	compactToolRound(history, 1)

	if history[2].Content != "file contents that must survive" {
		t.Errorf("read_file content should be preserved, got: %q", history[2].Content)
	}
	if history[3].Content != "[done]" {
		t.Errorf("write_file content should be compacted, got: %q", history[3].Content)
	}
}
