package harvey

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func newTestSpinner(estimate time.Duration) *Spinner {
	return &Spinner{estimate: estimate}
}

func TestUpdateStatus_appearsInOutput(t *testing.T) {
	var buf bytes.Buffer
	sp := newSpinner(&buf, 0, "")
	sp.UpdateStatus("hello from status")
	time.Sleep(200 * time.Millisecond) // allow at least one frame tick (100ms)
	sp.stop()
	if !strings.Contains(buf.String(), "hello from status") {
		t.Errorf("UpdateStatus message not found in spinner output\ngot: %q", buf.String())
	}
}

func TestUpdateStatus_stoppedSpinnerNoPanic(t *testing.T) {
	var buf bytes.Buffer
	sp := newSpinner(&buf, 0, "")
	sp.stop()
	// Should not panic or block.
	sp.UpdateStatus("after stop")
}

func TestTimerLabel_noEstimate(t *testing.T) {
	s := newTestSpinner(0)
	got := s.timerLabel(7 * time.Second)
	want := "[7s]"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestTimerLabel_withEstimate_elapsedUnder(t *testing.T) {
	s := newTestSpinner(12 * time.Second)
	got := s.timerLabel(4 * time.Second)
	want := "[4s / ~12s]"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestTimerLabel_withEstimate_elapsedOver(t *testing.T) {
	// Once elapsed exceeds the estimate the estimate is dropped from the label.
	s := newTestSpinner(5 * time.Second)
	got := s.timerLabel(9 * time.Second)
	want := "[9s]"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestTimerLabel_withEstimate_elapsedEqual(t *testing.T) {
	// Exactly at the estimate boundary — estimate should also be dropped.
	s := newTestSpinner(10 * time.Second)
	got := s.timerLabel(10 * time.Second)
	want := "[10s]"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestTimerLabel_zeroElapsed(t *testing.T) {
	s := newTestSpinner(8 * time.Second)
	got := s.timerLabel(0)
	want := "[0s / ~8s]"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
