package harvey

import (
	"strings"
	"testing"
)

// ─── /memory profile on/off ───────────────────────────────────────────────────

func TestCmdMemoryProfileOff_DisablesInjection(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.InjectOnStart = true

	var out strings.Builder
	if err := cmdMemoryProfile(a, []string{"off"}, &out, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a.Config.Memory.InjectOnStart {
		t.Error("expected InjectOnStart=false after /memory profile off")
	}
	if !strings.Contains(out.String(), "off") {
		t.Errorf("expected confirmation message to mention 'off', got: %q", out.String())
	}
}

func TestCmdMemoryProfileOn_EnablesInjection(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.InjectOnStart = false

	var out strings.Builder
	if err := cmdMemoryProfile(a, []string{"on"}, &out, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !a.Config.Memory.InjectOnStart {
		t.Error("expected InjectOnStart=true after /memory profile on")
	}
	if !strings.Contains(out.String(), "on") {
		t.Errorf("expected confirmation message to mention 'on', got: %q", out.String())
	}
}

func TestCmdMemoryProfileOff_IdempotentWhenAlreadyOff(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.InjectOnStart = false

	var out strings.Builder
	if err := cmdMemoryProfile(a, []string{"off"}, &out, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Config.Memory.InjectOnStart {
		t.Error("InjectOnStart should remain false")
	}
}

func TestCmdMemoryProfileOn_IdempotentWhenAlreadyOn(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Memory.InjectOnStart = true

	var out strings.Builder
	if err := cmdMemoryProfile(a, []string{"on"}, &out, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.Config.Memory.InjectOnStart {
		t.Error("InjectOnStart should remain true")
	}
}
