package harvey

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// estimateTokens is defined and tested via routing.go; only
// remainingContext and fileExceedsBudget are tested here.

// ─── contextUsage ────────────────────────────────────────────────────────────

func TestContextUsage_estimatedPath(t *testing.T) {
	a := newTestAgent(t)
	a.Config.Ollama.ContextLength = 1000
	a.AddMessage("user", "hello there")

	used, limit, exact := a.contextUsage()

	if exact {
		t.Error("expected exact=false without a live Ollama client")
	}
	if limit != 1000 {
		t.Errorf("limit = %d, want 1000", limit)
	}
	want := estimateTokens(HistoryText(a.History))
	if used != want {
		t.Errorf("used = %d, want %d (whole-history-string estimate)", used, want)
	}
}

func TestContextUsage_ollamaExactPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": []int{1, 2, 3, 4, 5}})
	}))
	defer srv.Close()

	a := newTestAgent(t)
	a.Config.Ollama.ContextLength = 2048
	a.Client = NewAnyLLMClient(nil, "llama3", "ollama", srv.URL)
	a.AddMessage("user", "hello")

	used, limit, exact := a.contextUsage()

	if !exact {
		t.Error("expected exact=true from a live Ollama tokenize response")
	}
	if used != 5 {
		t.Errorf("used = %d, want 5", used)
	}
	if limit != 2048 {
		t.Errorf("limit = %d, want 2048", limit)
	}
}

// TestContextUsage_ollamaUsesEffectiveContextLimit locks in the 2026-07-12
// decision: the Ollama path must derive limit from effectiveContextLimit()
// (which also checks llamafile-entry/ModelCache fallbacks), not read
// a.Config.Ollama.ContextLength directly — so it agrees with /status and the
// llamafile path on what "the limit" is, even when Ollama.ContextLength is
// left unset in harvey.yaml.
func TestContextUsage_ollamaUsesEffectiveContextLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": []int{1, 2, 3}})
	}))
	defer srv.Close()

	a := newTestAgent(t)
	a.Config.Ollama.ContextLength = 0 // deliberately unset
	a.Config.Llamafile.Active = "probed-entry"
	a.Config.Llamafile.Models = []LlamafileEntry{{Name: "probed-entry", ContextLength: 9999}}
	a.Client = NewAnyLLMClient(nil, "llama3", "ollama", srv.URL)

	_, limit, _ := a.contextUsage()

	if limit != 9999 {
		t.Errorf("limit = %d, want 9999 (from effectiveContextLimit(), not Config.Ollama.ContextLength)", limit)
	}
}

// ─── formatContextUsage ──────────────────────────────────────────────────────

func TestFormatContextUsage_belowWarnThreshold(t *testing.T) {
	tier, msg := formatContextUsage(100, 1000, true)
	if tier != contextOK || msg != "" {
		t.Errorf("got tier=%v msg=%q, want contextOK/empty", tier, msg)
	}
}

func TestFormatContextUsage_warnTierExact(t *testing.T) {
	tier, msg := formatContextUsage(800, 1000, true)
	want := "Context 80% full: 800 / 1000 tokens"
	if tier != contextWarn || msg != want {
		t.Errorf("got tier=%v msg=%q, want contextWarn/%q", tier, msg, want)
	}
}

func TestFormatContextUsage_warnTierEstimated(t *testing.T) {
	tier, msg := formatContextUsage(800, 1000, false)
	want := "Context 80% full: ~800 / 1000 tokens"
	if tier != contextWarn || msg != want {
		t.Errorf("got tier=%v msg=%q, want contextWarn/%q", tier, msg, want)
	}
}

func TestFormatContextUsage_fullTier(t *testing.T) {
	tier, msg := formatContextUsage(1000, 1000, true)
	want := "Context full: 1000 / 1000 tokens (100%) — reply may be truncated; try /clear or switch to a model with larger context"
	if tier != contextFull || msg != want {
		t.Errorf("got tier=%v msg=%q, want contextFull/%q", tier, msg, want)
	}
}

func TestFormatContextUsage_unknownLimit(t *testing.T) {
	tier, msg := formatContextUsage(5000, 0, false)
	if tier != contextOK || msg != "" {
		t.Errorf("got tier=%v msg=%q, want contextOK/empty when limit unknown", tier, msg)
	}
}

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
	cfg.Ollama.ContextLength = 0
	a := &Agent{Config: cfg}
	if got := remainingContext(a); got != 0 {
		t.Errorf("remainingContext with no limit = %d, want 0", got)
	}
}

func TestRemainingContext_SubtractsHistory(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ollama.ContextLength = 4000

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
	cfg.Ollama.ContextLength = 1000

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
	cfg.Ollama.ContextLength = 10000
	cfg.Chunking = DefaultChunkConfig() // STMWarnPct = 0.20
	a := &Agent{Config: cfg}            // empty history → ~9000 tokens remaining
	got := stmWarnNudge(a)
	if got != "" {
		t.Errorf("expected no nudge when context is ample; got: %q", got)
	}
}

func TestSTMWarnNudge_ContextNearlyFull(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ollama.ContextLength = 1000
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
	cfg.Ollama.ContextLength = 1000
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
	cfg.Ollama.ContextLength = 0 // unknown limit
	cfg.Chunking = DefaultChunkConfig()
	a := &Agent{Config: cfg}
	got := stmWarnNudge(a)
	if got != "" {
		t.Errorf("expected no nudge when context limit is unknown; got: %q", got)
	}
}

// ─── systemPromptTokenEstimate ────────────────────────────────────────────────

func TestSystemPromptTokenEstimate_NoSystemMessage(t *testing.T) {
	a := &Agent{History: []Message{{Role: "user", Content: "hi"}}}
	if got := a.systemPromptTokenEstimate(); got != 0 {
		t.Errorf("got %d, want 0 when no system message is set", got)
	}
}

func TestSystemPromptTokenEstimate_PadsHeuristicBy20Percent(t *testing.T) {
	// 400 chars -> chars/4 = 100 tokens -> padded 20% = 120.
	a := &Agent{History: []Message{
		{Role: "system", Content: strings.Repeat("x", 400)},
	}}
	want := 120
	if got := a.systemPromptTokenEstimate(); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

// ─── systemPromptExceedsContext ───────────────────────────────────────────────

func TestSystemPromptExceedsContext_UnknownLimit(t *testing.T) {
	if err := systemPromptExceedsContext("some-model", 5000, 0); err != nil {
		t.Errorf("expected nil error when limit is unknown, got %v", err)
	}
}

func TestSystemPromptExceedsContext_Fits(t *testing.T) {
	if err := systemPromptExceedsContext("some-model", 1000, 2048); err != nil {
		t.Errorf("expected nil error when prompt fits, got %v", err)
	}
}

func TestSystemPromptExceedsContext_ExceedsLimit(t *testing.T) {
	err := systemPromptExceedsContext("OpenELM-3B-Instruct", 3372, 2048)
	if err == nil {
		t.Fatal("expected an error when prompt tokens meet or exceed the limit")
	}
	for _, want := range []string{"OpenELM-3B-Instruct", "3372", "2048"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}

func TestSystemPromptExceedsContext_EqualsLimit(t *testing.T) {
	// n == limit leaves zero room for any reply; treat as exceeding.
	if err := systemPromptExceedsContext("m", 2048, 2048); err == nil {
		t.Error("expected an error when prompt tokens equal the limit exactly")
	}
}

func TestRemainingContext_ExhaustedReturnsZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ollama.ContextLength = 1000

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
