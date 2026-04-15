package harvey

import (
	"testing"
	"time"
)

// ─── ChatStats.Format ────────────────────────────────────────────────────────

func TestChatStatsFormat_noTokens(t *testing.T) {
	s := ChatStats{Elapsed: 5 * time.Second}
	got := s.Format()
	// No token counts — only elapsed time.
	if got != "5s" {
		t.Errorf("got %q want %q", got, "5s")
	}
}

func TestChatStatsFormat_withTokens(t *testing.T) {
	s := ChatStats{
		PromptTokens: 20,
		ReplyTokens:  40,
		Elapsed:      8 * time.Second,
		TokensPerSec: 5.0,
	}
	got := s.Format()
	want := "20 prompt + 40 reply tokens · 8s · 5.0 tok/s"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// ─── Agent.recordStats / estimateDuration ────────────────────────────────────

func TestAgentEstimateDuration_empty(t *testing.T) {
	a := &Agent{}
	if got := a.estimateDuration(); got != 0 {
		t.Errorf("expected 0 with no history, got %v", got)
	}
}

func TestAgentEstimateDuration_noTokenData(t *testing.T) {
	// publicai turns have zero token counts — should not contribute to estimate.
	a := &Agent{}
	a.recordStats(ChatStats{Elapsed: 10 * time.Second})
	a.recordStats(ChatStats{Elapsed: 12 * time.Second})
	if got := a.estimateDuration(); got != 0 {
		t.Errorf("expected 0 when no token data present, got %v", got)
	}
}

func TestAgentEstimateDuration_withData(t *testing.T) {
	a := &Agent{}
	// Two turns: both take 10s to generate 100 tokens → 10 tok/s.
	a.recordStats(ChatStats{ReplyTokens: 100, TokensPerSec: 10, Elapsed: 10 * time.Second})
	a.recordStats(ChatStats{ReplyTokens: 100, TokensPerSec: 10, Elapsed: 10 * time.Second})
	got := a.estimateDuration()
	if got != 10*time.Second {
		t.Errorf("got %v want 10s", got)
	}
}

func TestAgentEstimateDuration_average(t *testing.T) {
	a := &Agent{}
	// Turn 1: 50 tokens at 10 tok/s = 5s
	// Turn 2: 100 tokens at 10 tok/s = 10s
	// Average: 7.5s → rounds to 8s
	a.recordStats(ChatStats{ReplyTokens: 50, TokensPerSec: 10, Elapsed: 5 * time.Second})
	a.recordStats(ChatStats{ReplyTokens: 100, TokensPerSec: 10, Elapsed: 10 * time.Second})
	got := a.estimateDuration()
	want := 8 * time.Second // (5+10)/2 = 7.5 → rounds to 8
	if got != want {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestAgentRecordStats_cappedAtMax(t *testing.T) {
	a := &Agent{}
	for i := range maxStatHistory + 3 {
		a.recordStats(ChatStats{ReplyTokens: i + 1, TokensPerSec: 1})
	}
	if len(a.statHistory) != maxStatHistory {
		t.Errorf("history length %d, want %d", len(a.statHistory), maxStatHistory)
	}
	// The oldest entries should have been dropped; the last entry should be the
	// most recently recorded one.
	last := a.statHistory[len(a.statHistory)-1]
	if last.ReplyTokens != maxStatHistory+3 {
		t.Errorf("last ReplyTokens = %d, want %d", last.ReplyTokens, maxStatHistory+3)
	}
}

// ─── Agent.AddMessage / ClearHistory ─────────────────────────────────────────

func TestAgentAddMessage(t *testing.T) {
	a := &Agent{}
	a.AddMessage("user", "hello")
	a.AddMessage("assistant", "hi")
	if len(a.History) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(a.History))
	}
	if a.History[0].Role != "user" || a.History[0].Content != "hello" {
		t.Errorf("unexpected first message: %+v", a.History[0])
	}
}

func TestAgentClearHistory_withSystemPrompt(t *testing.T) {
	a := &Agent{Config: &Config{SystemPrompt: "You are Harvey."}}
	a.AddMessage("user", "hello")
	a.AddMessage("assistant", "hi")
	a.ClearHistory()
	if len(a.History) != 1 {
		t.Fatalf("expected 1 message (system prompt), got %d", len(a.History))
	}
	if a.History[0].Role != "system" {
		t.Errorf("expected system message, got %q", a.History[0].Role)
	}
}

func TestAgentClearHistory_noSystemPrompt(t *testing.T) {
	a := &Agent{Config: &Config{}}
	a.AddMessage("user", "hello")
	a.ClearHistory()
	if len(a.History) != 0 {
		t.Errorf("expected empty history, got %d messages", len(a.History))
	}
}
