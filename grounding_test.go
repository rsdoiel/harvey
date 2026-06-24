package harvey

import (
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// ─── extractQuotedStrings ─────────────────────────────────────────────────────

func TestExtractQuotedStrings_basic(t *testing.T) {
	got := extractQuotedStrings(`He said "hello world from test" and left.`, 5)
	if len(got) != 1 || got[0] != "hello world from test" {
		t.Errorf("expected [\"hello world from test\"], got %v", got)
	}
}

func TestExtractQuotedStrings_belowMinLen(t *testing.T) {
	// "hi" is only 2 chars — below minLen of 5.
	got := extractQuotedStrings(`She said "hi" quickly.`, 5)
	if len(got) != 0 {
		t.Errorf("expected no matches for short string, got %v", got)
	}
}

func TestExtractQuotedStrings_multipleMatches(t *testing.T) {
	got := extractQuotedStrings(`"first long string" and "second long string" done.`, 10)
	if len(got) != 2 {
		t.Errorf("expected 2 matches, got %v", got)
	}
}

func TestExtractQuotedStrings_noNewlineInside(t *testing.T) {
	// A newline inside quotes should not match.
	got := extractQuotedStrings("\"line one\nline two\"", 5)
	if len(got) != 0 {
		t.Errorf("expected no match for multi-line quote, got %v", got)
	}
}

// ─── groundingCheck ───────────────────────────────────────────────────────────

// makeHistory builds a minimal history slice: an assistant message with one
// tool call named toolName (id "tc1"), followed by a tool-result message
// containing resultContent.
func makeHistory(toolName, resultContent string) []Message {
	return []Message{
		{
			Role: "assistant",
			ToolCalls: []anyllm.ToolCall{
				{ID: "tc1", Function: anyllm.FunctionCall{Name: toolName}},
			},
		},
		{
			Role:       "tool",
			Content:    resultContent,
			ToolCallID: "tc1",
		},
	}
}

func TestGroundingCheck_noToolCalls(t *testing.T) {
	// No tool messages in history → no warning.
	warn := groundingCheck(`The answer is "forty-two characters long enough".`, []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	})
	if warn != "" {
		t.Errorf("expected no warning with no tool calls, got: %s", warn)
	}
}

func TestGroundingCheck_nonContentTool(t *testing.T) {
	// A tool call for a non-content tool (e.g. write_file) should not trigger.
	history := makeHistory("write_file", "some content")
	warn := groundingCheck(`Result: "this text is definitely not in the file".`, history)
	if warn != "" {
		t.Errorf("expected no warning for non-content tool, got: %s", warn)
	}
}

func TestGroundingCheck_noQuotedText(t *testing.T) {
	// read_file was called but the response has no quoted text → no warning.
	history := makeHistory("read_file", "the real file content goes here")
	warn := groundingCheck("The file discusses grail goals in computer science.", history)
	if warn != "" {
		t.Errorf("expected no warning when response has no quoted text, got: %s", warn)
	}
}

func TestGroundingCheck_groundedResponse(t *testing.T) {
	// Quoted text from the response appears in the tool result → no warning.
	fileContent := "A grail goal in computer and information sciences since my undergrad education"
	history := makeHistory("read_file", fileContent)
	response := `The document opens with "A grail goal in computer and information sciences since my undergrad education" as its thesis.`
	warn := groundingCheck(response, history)
	if warn != "" {
		t.Errorf("expected no warning for grounded response, got: %s", warn)
	}
}

func TestGroundingCheck_hallucination(t *testing.T) {
	// The model quoted text that doesn't appear in what read_file returned.
	fileContent := "A grail goal in computer and information sciences since my undergrad education"
	history := makeHistory("read_file", fileContent)
	// Granite's hallucinated quote — not in the real file.
	response := `Line 2: "Natural language programming (NLP) is a field of study that involves the creation of computer programs."`
	warn := groundingCheck(response, history)
	if warn == "" {
		t.Error("expected hallucination warning, got none")
	}
}

func TestGroundingCheck_partialGrounding(t *testing.T) {
	// One quoted string matches, another doesn't — still counts as grounded
	// (the model at least read part of the file correctly).
	fileContent := "A grail goal in computer and information sciences"
	history := makeHistory("read_file", fileContent)
	response := `It starts with "A grail goal in computer and information sciences" but also mentions "Virtual assistants to automated translation services".`
	warn := groundingCheck(response, history)
	if warn != "" {
		t.Errorf("expected no warning when at least one quote matches, got: %s", warn)
	}
}

func TestGroundingCheck_shortQuotesIgnored(t *testing.T) {
	// Quoted strings shorter than minQuotedLen should not trigger the check.
	fileContent := "completely different content not related to anything"
	history := makeHistory("read_file", fileContent)
	response := `The file uses "NLP" as a term throughout.` // "NLP" is only 3 chars
	warn := groundingCheck(response, history)
	if warn != "" {
		t.Errorf("expected no warning for short quotes only, got: %s", warn)
	}
}
