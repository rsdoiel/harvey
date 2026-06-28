package harvey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// ─── mock LLM client ─────────────────────────────────────────────────────────

type mockLLMClient struct {
	reply   string
	callErr error
}

func (m *mockLLMClient) Name() string { return "mock" }
func (m *mockLLMClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	if m.callErr != nil {
		return ChatStats{}, m.callErr
	}
	fmt.Fprint(out, m.reply)
	return ChatStats{}, nil
}
func (m *mockLLMClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (m *mockLLMClient) Close() error                               { return nil }

// ─── /summarize ──────────────────────────────────────────────────────────────

func TestCmdSummarize_noClient(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdSummarize(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No backend") {
		t.Error("expected 'No backend' message")
	}
}

func TestCmdSummarize_tooShort(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "summary"}
	// Only one non-system message — not enough to summarise.
	a.AddMessage("user", "hello")

	var out strings.Builder
	if err := cmdSummarize(a, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Not enough") {
		t.Error("expected 'Not enough' message")
	}
	// History should be unchanged.
	if len(a.History) != 1 {
		t.Errorf("history length = %d, want 1", len(a.History))
	}
}

func TestCmdSummarize_success(t *testing.T) {
	a := newTestAgent(t)
	a.Config.SystemPrompt = "You are Harvey."
	a.Client = &mockLLMClient{reply: "We discussed spinning and timers."}
	a.AddMessage("system", "You are Harvey.")
	a.AddMessage("user", "How does the spinner work?")
	a.AddMessage("assistant", "It animates braille frames.")
	a.AddMessage("user", "Can it show elapsed time?")
	a.AddMessage("assistant", "Yes, timerLabel does that.")

	var out strings.Builder
	if err := cmdSummarize(a, nil, &out); err != nil {
		t.Fatalf("cmdSummarize: %v", err)
	}

	// History should be: system + (optional pinned) + summary user message.
	if len(a.History) < 2 {
		t.Fatalf("expected at least 2 history messages after summarize, got %d", len(a.History))
	}
	// First message is the system prompt (re-injected by ClearHistory).
	if a.History[0].Role != "system" {
		t.Errorf("first message role = %q, want system", a.History[0].Role)
	}
	// Last message should be the summary.
	last := a.History[len(a.History)-1]
	if !strings.Contains(last.Content, "We discussed spinning") {
		t.Error("expected summary text in history")
	}
	if !strings.Contains(last.Content, "[Conversation summary]") {
		t.Error("expected summary label in history message")
	}
	if !strings.Contains(out.String(), "condensed") {
		t.Error("expected confirmation in output")
	}
}

func TestCmdSummarize_clientError(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{callErr: errors.New("connection refused")}
	a.AddMessage("user", "question one")
	a.AddMessage("assistant", "answer one")

	var out strings.Builder
	err := cmdSummarize(a, nil, &out)
	if err == nil {
		t.Error("expected error when client fails")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want 'connection refused'", err)
	}
}

func TestCmdSummarize_preservesPinnedContext(t *testing.T) {
	a := newTestAgent(t)
	a.Client = &mockLLMClient{reply: "A brief summary."}
	a.PinnedContext = "Always use Go 1.26."
	a.AddMessage("user", "first question")
	a.AddMessage("assistant", "first answer")
	a.AddMessage("user", "second question")
	a.AddMessage("assistant", "second answer")

	var out strings.Builder
	if err := cmdSummarize(a, nil, &out); err != nil {
		t.Fatalf("cmdSummarize: %v", err)
	}

	// Pinned context should be re-injected.
	found := false
	for _, m := range a.History {
		if strings.Contains(m.Content, "Always use Go 1.26.") {
			found = true
			break
		}
	}
	if !found {
		t.Error("pinned context should be preserved after summarize")
	}
}

// ─── /context ────────────────────────────────────────────────────────────────

func TestCmdContext_showEmpty(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	cmdContext(a, nil, &out)
	if !strings.Contains(out.String(), "empty") {
		t.Error("expected 'empty' message for blank pinned context")
	}
}

func TestCmdContext_addAndShow(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder

	cmdContext(a, []string{"add", "Use", "Go", "1.26"}, &out)
	if a.PinnedContext != "Use Go 1.26" {
		t.Errorf("PinnedContext = %q, want %q", a.PinnedContext, "Use Go 1.26")
	}

	out.Reset()
	cmdContext(a, []string{"show"}, &out)
	if !strings.Contains(out.String(), "Use Go 1.26") {
		t.Error("expected pinned context in show output")
	}
}

func TestCmdContext_addAppends(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	cmdContext(a, []string{"add", "line one"}, &out)
	cmdContext(a, []string{"add", "line two"}, &out)

	if !strings.Contains(a.PinnedContext, "line one") || !strings.Contains(a.PinnedContext, "line two") {
		t.Errorf("expected both lines in PinnedContext, got %q", a.PinnedContext)
	}
}

func TestCmdContext_addInjectsIntoHistory(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	cmdContext(a, []string{"add", "important note"}, &out)

	found := false
	for _, m := range a.History {
		if m.Role == "user" && strings.Contains(m.Content, "important note") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected pinned context message in history after add")
	}
}

func TestCmdContext_addUpdatesExistingMessage(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	cmdContext(a, []string{"add", "first"}, &out)
	cmdContext(a, []string{"add", "second"}, &out)

	// Should be exactly one pinned context message in history.
	count := 0
	for _, m := range a.History {
		if m.Role == "user" && strings.HasPrefix(m.Content, "[pinned context]") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 pinned context message, got %d", count)
	}
}

func TestCmdContext_clear(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	cmdContext(a, []string{"add", "some text"}, &out)
	out.Reset()
	cmdContext(a, []string{"clear"}, &out)

	if a.PinnedContext != "" {
		t.Error("PinnedContext should be empty after clear")
	}
	if !strings.Contains(out.String(), "cleared") {
		t.Error("expected 'cleared' message")
	}
	for _, m := range a.History {
		if m.Role == "user" && strings.HasPrefix(m.Content, "[pinned context]") {
			t.Error("pinned context message should be removed from history after clear")
		}
	}
}

func TestCmdContext_persistsAcrossClearHistory(t *testing.T) {
	a := newTestAgent(t)
	a.Config = &Config{SystemPrompt: "You are Harvey."}
	var out strings.Builder
	cmdContext(a, []string{"add", "target: macOS arm64"}, &out)

	// Simulate several turns then /clear.
	a.AddMessage("user", "hello")
	a.AddMessage("assistant", "hi")
	a.ClearHistory()

	found := false
	for _, m := range a.History {
		if strings.Contains(m.Content, "target: macOS arm64") {
			found = true
			break
		}
	}
	if !found {
		t.Error("pinned context should be re-injected after ClearHistory")
	}
}

// ─── ExpandDynamicSections ───────────────────────────────────────────────────

func TestExpandDynamicSections_nilWorkspace(t *testing.T) {
	input := "hello <!-- @date --> world"
	got := ExpandDynamicSections(input, nil)
	if got != input {
		t.Errorf("nil workspace: expected input unchanged, got %q", got)
	}
}

func TestExpandDynamicSections_noMarkers(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	input := "No markers here."
	got := ExpandDynamicSections(input, ws)
	if got != input {
		t.Errorf("no markers: expected input unchanged, got %q", got)
	}
}

func TestExpandDynamicSections_date(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	got := ExpandDynamicSections("Today is <!-- @date -->.", ws)
	if strings.Contains(got, "<!-- @date -->") {
		t.Error("date marker should be replaced")
	}
	// Should contain a YYYY-MM-DD formatted date.
	if !strings.Contains(got, "Today is 20") {
		t.Errorf("expected date in output, got %q", got)
	}
}

func TestExpandDynamicSections_files(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	ws.WriteFile("main.go", []byte("package main"), 0o644)
	ws.WriteFile("README.md", []byte("# Readme"), 0o644)

	got := ExpandDynamicSections("Files:\n<!-- @files -->", ws)
	if strings.Contains(got, "<!-- @files -->") {
		t.Error("files marker should be replaced")
	}
	if !strings.Contains(got, "main.go") {
		t.Error("expected main.go in file listing")
	}
	if !strings.Contains(got, "README.md") {
		t.Error("expected README.md in file listing")
	}
}

func TestExpandDynamicSections_filesSkipsHidden(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	ws.WriteFile(".hidden/secret.go", []byte("secret"), 0o644)
	ws.WriteFile("visible.go", []byte("public"), 0o644)

	got := ExpandDynamicSections("<!-- @files -->", ws)
	if strings.Contains(got, ".hidden") {
		t.Error("hidden directories should not appear in file listing")
	}
	if !strings.Contains(got, "visible.go") {
		t.Error("expected visible.go in file listing")
	}
}

func TestExpandDynamicSections_gitStatusNotRepo(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	got := ExpandDynamicSections("Status: <!-- @git-status -->", ws)
	if strings.Contains(got, "<!-- @git-status -->") {
		t.Error("git-status marker should be replaced")
	}
	if !strings.Contains(got, "not a git repository") {
		t.Errorf("expected 'not a git repository', got %q", got)
	}
}

func TestExpandDynamicSections_gitStatusCleanRepo(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	initGitRepo(t, ws.Root)
	commitFile(t, ws.Root, "hello.go", "package main\n")

	got := ExpandDynamicSections("<!-- @git-status -->", ws)
	if strings.Contains(got, "<!-- @git-status -->") {
		t.Error("git-status marker should be replaced")
	}
	if strings.Contains(got, "not a git repository") {
		t.Errorf("should be a valid repo, got: %q", got)
	}
}
