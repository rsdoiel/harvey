package harvey

import (
	"context"
	"io"
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
