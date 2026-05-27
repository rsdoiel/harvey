package harvey

import (
	"context"
	"io"
	"strings"
	"testing"
)

// ── ShouldCompress ────────────────────────────────────────────────────────────

func TestShouldCompress_BelowThreshold(t *testing.T) {
	if ShouldCompress(79, 100, 0.80) {
		t.Error("79/100 at 80% should NOT trigger compression")
	}
}

func TestShouldCompress_AtThreshold(t *testing.T) {
	if !ShouldCompress(80, 100, 0.80) {
		t.Error("80/100 at 80% SHOULD trigger compression")
	}
}

func TestShouldCompress_AboveThreshold(t *testing.T) {
	if !ShouldCompress(95, 100, 0.80) {
		t.Error("95/100 at 80% SHOULD trigger compression")
	}
}

func TestShouldCompress_FullContext(t *testing.T) {
	if !ShouldCompress(100, 100, 0.80) {
		t.Error("100/100 at 80% SHOULD trigger compression")
	}
}

func TestShouldCompress_ZeroContextLen(t *testing.T) {
	if ShouldCompress(80, 0, 0.80) {
		t.Error("zero contextLen should never trigger compression")
	}
}

func TestShouldCompress_ZeroWarnAtPct(t *testing.T) {
	if ShouldCompress(80, 100, 0) {
		t.Error("zero warnAtPct should never trigger compression")
	}
}

func TestShouldCompress_NegativeContextLen(t *testing.T) {
	if ShouldCompress(80, -1, 0.80) {
		t.Error("negative contextLen should never trigger compression")
	}
}

func TestShouldCompress_ZeroTokens(t *testing.T) {
	if ShouldCompress(0, 100, 0.80) {
		t.Error("zero historyTokens at 80% should NOT trigger compression")
	}
}

// ── CompressHistory ───────────────────────────────────────────────────────────

// mockSummaryClient is a minimal LLMClient that returns a fixed summary.
type mockSummaryClient struct {
	summary string
}

func (m *mockSummaryClient) Name() string { return "mock" }
func (m *mockSummaryClient) Models(_ context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockSummaryClient) Close() error { return nil }
func (m *mockSummaryClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	_, _ = io.WriteString(out, m.summary)
	return ChatStats{}, nil
}

func TestCompressHistory_KeepsTurns(t *testing.T) {
	a := &Agent{
		Client: &mockSummaryClient{summary: "decisions: used git init"},
		History: []Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "turn 1"},
			{Role: "assistant", Content: "reply 1"},
			{Role: "user", Content: "turn 2"},
			{Role: "assistant", Content: "reply 2"},
			{Role: "user", Content: "turn 3"},       // recent
			{Role: "assistant", Content: "reply 3"}, // recent
		},
	}
	if err := CompressHistory(a, 2, io.Discard); err != nil {
		t.Fatalf("CompressHistory: %v", err)
	}
	// Should have: system + summary + 2 recent turns = 4 messages.
	if len(a.History) != 4 {
		t.Errorf("expected 4 messages after compression, got %d: %+v", len(a.History), a.History)
	}
	if a.History[0].Role != "system" {
		t.Errorf("first message should be system prompt, got %q", a.History[0].Role)
	}
	if !strings.Contains(a.History[1].Content, "Session history compressed") {
		t.Errorf("second message should be compression summary, got %q", a.History[1].Content)
	}
	if !strings.Contains(a.History[1].Content, "decisions: used git init") {
		t.Errorf("summary should include mock client output: %q", a.History[1].Content)
	}
	// Last two are the recent turns.
	if a.History[2].Content != "turn 3" || a.History[3].Content != "reply 3" {
		t.Errorf("recent turns not preserved: %v %v", a.History[2], a.History[3])
	}
}

func TestCompressHistory_NoSystemPrompt(t *testing.T) {
	a := &Agent{
		Client: &mockSummaryClient{summary: "summary text"},
		History: []Message{
			{Role: "user", Content: "old turn 1"},
			{Role: "assistant", Content: "old reply 1"},
			{Role: "user", Content: "recent"},       // recent
			{Role: "assistant", Content: "reply"},   // recent
		},
	}
	if err := CompressHistory(a, 2, io.Discard); err != nil {
		t.Fatalf("CompressHistory: %v", err)
	}
	// system + summary + 2 recent = no system, so summary + 2 recent = 3.
	if len(a.History) != 3 {
		t.Errorf("expected 3 messages, got %d", len(a.History))
	}
	if a.History[0].Role != "user" || !strings.Contains(a.History[0].Content, "Session history compressed") {
		t.Errorf("first message should be summary, got %+v", a.History[0])
	}
}

func TestCompressHistory_TooFewTurns(t *testing.T) {
	a := &Agent{
		Client: &mockSummaryClient{summary: "x"},
		History: []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "only turn"},
			{Role: "assistant", Content: "only reply"},
		},
	}
	original := make([]Message, len(a.History))
	copy(original, a.History)

	if err := CompressHistory(a, 6, io.Discard); err != nil {
		t.Fatalf("CompressHistory: %v", err)
	}
	// keepTurns=6 but only 2 turns (excluding system) → nothing to compress.
	if len(a.History) != len(original) {
		t.Errorf("history should be unchanged when turns <= keepTurns, got len=%d", len(a.History))
	}
}

func TestCompressHistory_NilClient(t *testing.T) {
	a := &Agent{
		Client: nil,
		History: []Message{
			{Role: "user", Content: "turn"},
		},
	}
	if err := CompressHistory(a, 2, io.Discard); err != nil {
		t.Errorf("nil client should return nil error, got %v", err)
	}
}
