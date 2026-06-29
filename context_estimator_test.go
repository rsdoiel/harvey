package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// estimateTokens is defined and tested via routing.go; only
// remainingContext and fileExceedsBudget are tested here.

func TestFileExceedsBudget_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	// Write 400 bytes → ~100 tokens estimated.
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 400)), 0644); err != nil {
		t.Fatal(err)
	}
	// Budget of 200 tokens — file should not exceed it.
	exceeded, size, err := fileExceedsBudget(path, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exceeded {
		t.Errorf("expected file (%d bytes) not to exceed budget of 200 tokens", size)
	}
}

func TestFileExceedsBudget_LargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	// Write 4000 bytes → ~1000 tokens estimated.
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 4000)), 0644); err != nil {
		t.Fatal(err)
	}
	// Budget of 500 tokens — file should exceed it.
	exceeded, size, err := fileExceedsBudget(path, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exceeded {
		t.Errorf("expected file (%d bytes) to exceed budget of 500 tokens", size)
	}
}

func TestFileExceedsBudget_Missing(t *testing.T) {
	_, _, err := fileExceedsBudget("/nonexistent/path/file.txt", 100)
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestFileExceedsBudget_DoesNotReadFile(t *testing.T) {
	// Verify fileExceedsBudget works on a file with no read permission.
	// This confirms it uses os.Stat only, not file I/O.
	dir := t.TempDir()
	path := filepath.Join(dir, "noperm.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("b", 800)), 0000); err != nil {
		t.Fatal(err)
	}
	// os.Stat succeeds even on mode 000 files; reading would fail.
	exceeded, _, err := fileExceedsBudget(path, 50)
	if err != nil {
		t.Fatalf("fileExceedsBudget should not need read permission, got: %v", err)
	}
	// 800 bytes / 4 = 200 tokens > 50 budget.
	if !exceeded {
		t.Error("expected 800-byte file to exceed budget of 50 tokens")
	}
}

func TestRemainingContext_UnknownLimit(t *testing.T) {
	// Agent with no context limit configured returns 0.
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 0
	a := &Agent{Config: cfg}
	if got := remainingContext(a); got != 0 {
		t.Errorf("remainingContext with no limit = %d, want 0", got)
	}
}

func TestRemainingContext_SubtractsHistory(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 4000

	// Agent with empty history should have more remaining than one with content.
	empty := &Agent{Config: cfg}
	remEmpty := remainingContext(empty)

	withHistory := &Agent{
		Config: cfg,
		History: []Message{
			{Role: "system", Content: strings.Repeat("x", 4000)}, // ~1000 tokens
			{Role: "user", Content: strings.Repeat("y", 2000)},   // ~500 tokens
		},
	}
	remFull := remainingContext(withHistory)

	if remFull >= remEmpty {
		t.Errorf("agent with history (%d) should have less remaining than empty (%d)",
			remFull, remEmpty)
	}
}

func TestRemainingContext_SafetyMargin(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 1000

	// Empty history: remaining = 1000 - 0 - 100 (10% margin) = 900.
	a := &Agent{Config: cfg}
	got := remainingContext(a)
	if got != 900 {
		t.Errorf("remainingContext with empty history = %d, want 900", got)
	}
}

// ─── stmWarnNudge ─────────────────────────────────────────────────────────────

func TestSTMWarnNudge_ContextAmple(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 10000
	cfg.Chunking = DefaultChunkConfig() // STMWarnPct = 0.20
	a := &Agent{Config: cfg}            // empty history → ~9000 tokens remaining
	got := stmWarnNudge(a)
	if got != "" {
		t.Errorf("expected no nudge when context is ample; got: %q", got)
	}
}

func TestSTMWarnNudge_ContextNearlyFull(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 1000
	cfg.Chunking = DefaultChunkConfig() // STMWarnPct = 0.20 → threshold = 200 tokens
	// Fill history to ~820 tokens → remainingContext ≈ 1000-820-100 = 80 < 200.
	a := &Agent{
		Config: cfg,
		History: []Message{
			{Role: "user", Content: strings.Repeat("x", 3280)}, // 3280/4 = 820 tokens
		},
	}
	got := stmWarnNudge(a)
	if got == "" {
		t.Error("expected nudge when context is nearly full; got empty string")
	}
	if !strings.Contains(got, "summary_context") {
		t.Errorf("nudge should mention summary_context; got: %q", got)
	}
}

func TestSTMWarnNudge_DisabledWhenZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 1000
	cfg.Chunking = DefaultChunkConfig()
	cfg.Chunking.STMWarnPct = 0 // disabled
	a := &Agent{
		Config: cfg,
		History: []Message{
			{Role: "user", Content: strings.Repeat("x", 3280)}, // nearly full
		},
	}
	got := stmWarnNudge(a)
	if got != "" {
		t.Errorf("expected no nudge when STMWarnPct=0; got: %q", got)
	}
}

func TestSTMWarnNudge_NoLimitConfigured(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 0 // unknown limit
	cfg.Chunking = DefaultChunkConfig()
	a := &Agent{Config: cfg}
	got := stmWarnNudge(a)
	if got != "" {
		t.Errorf("expected no nudge when context limit is unknown; got: %q", got)
	}
}

func TestRemainingContext_ExhaustedReturnsZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OllamaContextLength = 1000

	// History that uses all tokens should return 0, not negative.
	a := &Agent{
		Config: cfg,
		History: []Message{
			{Role: "user", Content: strings.Repeat("z", 8000)}, // ~2000 tokens > limit
		},
	}
	got := remainingContext(a)
	if got != 0 {
		t.Errorf("remainingContext when exhausted = %d, want 0", got)
	}
}
