package harvey

import (
	"strings"
	"testing"
)

func TestReportSensorEvent_writesMessage(t *testing.T) {
	var out strings.Builder
	reportSensorEvent(&out, SensorEvent{Kind: "grounding", Message: "hallucinated quote detected", Class: Computational})

	if !strings.Contains(out.String(), "hallucinated quote detected") {
		t.Errorf("expected message in output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "⚠") {
		t.Errorf("expected warning symbol in output, got %q", out.String())
	}
}

func TestReportSensorEvent_matchesPreRefactorGroundingFormat(t *testing.T) {
	// Characterization test: pins the exact visible text produced by the
	// pre-refactor groundingCheck call site (terminal.go, before the
	// 2026-07-12 SensorEvent unification), so the new shared formatter can't
	// silently drift from it.
	var out strings.Builder
	warn := "quoted text not found in any tool result this turn"
	reportSensorEvent(&out, SensorEvent{Kind: "grounding", Message: warn, Class: Computational})

	want := yellow("  ⚠ ") + warn + "\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}
