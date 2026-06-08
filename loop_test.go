package harvey

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// ─── parseLoopArgs ────────────────────────────────────────────────────────────

func TestParseLoopArgs_valid(t *testing.T) {
	cases := []struct {
		args          []string
		wantInterval  time.Duration
		wantCount     int
		wantRest      string
	}{
		{
			args:         []string{"30s", "hello", "world"},
			wantInterval: 30 * time.Second,
			wantCount:    10,
			wantRest:     "hello world",
		},
		{
			args:         []string{"5m", "--count", "3", "check", "the", "build"},
			wantInterval: 5 * time.Minute,
			wantCount:    3,
			wantRest:     "check the build",
		},
		{
			args:         []string{"300", "run", "this"},
			wantInterval: 300 * time.Second,
			wantCount:    10,
			wantRest:     "run this",
		},
		{
			args:         []string{"1h", "--count", "100", "/git", "status"},
			wantInterval: time.Hour,
			wantCount:    100,
			wantRest:     "/git status",
		},
		{
			args:         []string{"1m30s", "--count", "1", "prompt"},
			wantInterval: 90 * time.Second,
			wantCount:    1,
			wantRest:     "prompt",
		},
	}

	for _, tc := range cases {
		interval, count, rest, err := parseLoopArgs(tc.args)
		if err != nil {
			t.Errorf("parseLoopArgs(%v) unexpected error: %v", tc.args, err)
			continue
		}
		if interval != tc.wantInterval {
			t.Errorf("interval: got %v, want %v (args=%v)", interval, tc.wantInterval, tc.args)
		}
		if count != tc.wantCount {
			t.Errorf("count: got %d, want %d (args=%v)", count, tc.wantCount, tc.args)
		}
		if rest != tc.wantRest {
			t.Errorf("rest: got %q, want %q (args=%v)", rest, tc.wantRest, tc.args)
		}
	}
}

func TestParseLoopArgs_invalid(t *testing.T) {
	cases := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{}, wantErr: "usage"},
		{args: []string{"notaduration", "prompt"}, wantErr: "invalid interval"},
		{args: []string{"0", "prompt"}, wantErr: "interval must be positive"},
		{args: []string{"5m", "--count", "0", "prompt"}, wantErr: "--count must be"},
		{args: []string{"5m", "--count", "101", "prompt"}, wantErr: "--count must be"},
		{args: []string{"5m", "--count", "abc", "prompt"}, wantErr: "--count must be"},
		{args: []string{"5m"}, wantErr: "usage"},
		{args: []string{"5m", "--count", "3"}, wantErr: "usage"},
	}

	for _, tc := range cases {
		_, _, _, err := parseLoopArgs(tc.args)
		if err == nil {
			t.Errorf("parseLoopArgs(%v) expected error containing %q, got nil", tc.args, tc.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("parseLoopArgs(%v) error %q does not contain %q", tc.args, err.Error(), tc.wantErr)
		}
	}
}

// ─── sleepInterruptible ───────────────────────────────────────────────────────

func TestSleepInterruptible_completes(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	cancelled := sleepInterruptible(ctx, 10*time.Millisecond)
	if cancelled {
		t.Error("expected cancelled=false, got true")
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Errorf("returned too early: elapsed %v", elapsed)
	}
}

func TestSleepInterruptible_cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	cancelled := sleepInterruptible(ctx, 10*time.Second)
	if !cancelled {
		t.Error("expected cancelled=true, got false")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took too long to cancel: elapsed %v", elapsed)
	}
}

// ─── cmdLoop ─────────────────────────────────────────────────────────────────

func newLoopTestAgent(reply string) *Agent {
	a := &Agent{}
	a.Client = &mockLLMClient{reply: reply}
	a.Config = DefaultConfig()
	a.registerCommands()
	return a
}

func TestCmdLoop_chatMode(t *testing.T) {
	a := newLoopTestAgent("the answer")
	var out bytes.Buffer
	err := cmdLoop(a, []string{"1ms", "--count", "3", "hello"}, &out)
	if err != nil {
		t.Fatalf("cmdLoop returned error: %v", err)
	}
	// Each chat turn adds 2 messages (user + assistant).
	if len(a.History) != 6 {
		t.Errorf("expected 6 history messages after 3 turns, got %d", len(a.History))
	}
	if !strings.Contains(out.String(), "Loop finished after 3/3") {
		t.Errorf("expected finish message, got: %s", out.String())
	}
}

func TestCmdLoop_commandMode(t *testing.T) {
	a := newLoopTestAgent("")
	histBefore := len(a.History)
	var out bytes.Buffer
	// /status is a no-op command that reads agent state but never calls Chat.
	err := cmdLoop(a, []string{"1ms", "--count", "2", "/status"}, &out)
	if err != nil {
		t.Fatalf("cmdLoop returned error: %v", err)
	}
	if len(a.History) != histBefore {
		t.Errorf("command-mode loop should not touch History; before=%d after=%d", histBefore, len(a.History))
	}
	if !strings.Contains(out.String(), "Loop finished after 2/2") {
		t.Errorf("expected finish message, got: %s", out.String())
	}
}

func TestCmdLoop_exitSentinel(t *testing.T) {
	for _, sentinel := range []string{"/exit", "/quit", "/bye"} {
		a := newLoopTestAgent("")
		histBefore := len(a.History)
		var out bytes.Buffer
		err := cmdLoop(a, []string{"1ms", "--count", "5", sentinel}, &out)
		if err != nil {
			t.Fatalf("%s: cmdLoop returned error: %v", sentinel, err)
		}
		if len(a.History) != histBefore {
			t.Errorf("%s: History should be unchanged, before=%d after=%d", sentinel, histBefore, len(a.History))
		}
		if !strings.Contains(out.String(), "Loop stopped") {
			t.Errorf("%s: expected stopped message, got: %s", sentinel, out.String())
		}
	}
}

func TestCmdLoop_invalidArgs(t *testing.T) {
	cases := []struct {
		args    []string
		wantOut string
	}{
		{args: []string{}, wantOut: "usage"},
		{args: []string{"5m", "--count", "0", "prompt"}, wantOut: "--count"},
		{args: []string{"5m", "--count", "101", "prompt"}, wantOut: "--count"},
	}
	for _, tc := range cases {
		a := newLoopTestAgent("")
		var out bytes.Buffer
		err := cmdLoop(a, tc.args, &out)
		if err != nil {
			t.Errorf("args=%v: expected nil error, got %v", tc.args, err)
			continue
		}
		if !strings.Contains(strings.ToLower(out.String()), tc.wantOut) {
			t.Errorf("args=%v: output %q does not contain %q", tc.args, out.String(), tc.wantOut)
		}
	}
}

// ─── runLoopIteration ─────────────────────────────────────────────────────────

func TestRunLoopIteration_exitSentinels(t *testing.T) {
	a := newLoopTestAgent("")
	ctx := context.Background()
	var out bytes.Buffer
	for _, cmd := range []string{"/exit", "/quit", "/bye"} {
		out.Reset()
		exit, err := runLoopIteration(ctx, a, cmd, &out)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", cmd, err)
		}
		if !exit {
			t.Errorf("%s: expected exitRequested=true, got false", cmd)
		}
	}
}

func TestRunLoopIteration_chatTurn(t *testing.T) {
	a := newLoopTestAgent("great answer")
	ctx := context.Background()
	var out bytes.Buffer
	exit, err := runLoopIteration(ctx, a, "what is 2+2?", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exit {
		t.Error("expected exitRequested=false")
	}
	if len(a.History) != 2 {
		t.Errorf("expected 2 history messages, got %d", len(a.History))
	}
}
