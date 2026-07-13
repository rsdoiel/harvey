package harvey

import (
	"context"
	"strings"
	"testing"
)

func TestRunBoundedTurn_UsesToolLoopWhenEnabled(t *testing.T) {
	client := &mockLLMClient{reply: "tool-loop reply"}
	registry := NewToolRegistry()
	cfg := DefaultConfig()

	var w strings.Builder
	updatedHistory, _, err := runBoundedTurn(context.Background(), client, registry, cfg, true,
		[]Message{{Role: "user", Content: "hi"}}, nil, &w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(w.String(), "tool-loop reply") {
		t.Errorf("expected reply in output, got: %s", w.String())
	}
	// RunToolLoop's non-ToolCapable fallback returns the input messages
	// unchanged (non-nil) — this is the signal that the ToolExecutor path
	// actually ran, as opposed to a direct client.Chat call.
	if updatedHistory == nil {
		t.Error("expected non-nil updatedHistory when useTools=true and registry is set")
	}
}

func TestRunBoundedTurn_PlainChatWhenToolsDisabled(t *testing.T) {
	client := &mockLLMClient{reply: "plain reply"}
	registry := NewToolRegistry()
	cfg := DefaultConfig()

	var w strings.Builder
	updatedHistory, _, err := runBoundedTurn(context.Background(), client, registry, cfg, false,
		[]Message{{Role: "user", Content: "hi"}}, nil, &w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(w.String(), "plain reply") {
		t.Errorf("expected reply in output, got: %s", w.String())
	}
	if updatedHistory != nil {
		t.Errorf("expected nil updatedHistory on the plain-chat path, got: %v", updatedHistory)
	}
}

func TestRunBoundedTurn_PlainChatReturnsNilHistory(t *testing.T) {
	client := &mockLLMClient{reply: "no registry reply"}
	cfg := DefaultConfig()

	var w strings.Builder
	// useTools=true but registry=nil must still take the plain-chat path —
	// "tools requested" isn't enough without a registry to dispatch through.
	updatedHistory, _, err := runBoundedTurn(context.Background(), client, nil, cfg, true,
		[]Message{{Role: "user", Content: "hi"}}, nil, &w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(w.String(), "no registry reply") {
		t.Errorf("expected reply in output, got: %s", w.String())
	}
	if updatedHistory != nil {
		t.Errorf("expected nil updatedHistory when registry is nil, got: %v", updatedHistory)
	}
}
