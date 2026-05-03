package harvey_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mozilla-ai/any-llm-go/providers"

	"github.com/rsdoiel/harvey"
)

// TestAgent_ChatRoundTrip exercises the full Agent.Client.Chat() path using a
// mock provider — no network, no cost. Verifies stats flow back to the caller.
func TestAgent_ChatRoundTrip(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	client := harvey.NewAnyLLMClient(mock, "test-model", "mock", "")

	cfg := harvey.DefaultConfig()
	ws, err := harvey.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agent := harvey.NewAgent(cfg, ws)
	agent.Client = client

	var buf strings.Builder
	stats, err := agent.Client.Chat(context.Background(), []harvey.Message{
		{Role: "user", Content: "hello"},
	}, &buf)

	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if buf.String() != "Hello World" {
		t.Errorf("reply = %q, want %q", buf.String(), "Hello World")
	}
	if stats.ReplyTokens != 5 {
		t.Errorf("ReplyTokens = %d, want 5", stats.ReplyTokens)
	}
}

// TestAgent_ModelsReturnsNames checks that AnyLLMClient.Models() flows through
// the provider's ListModels and returns the model IDs.
func TestAgent_ModelsReturnsNames(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	client := harvey.NewAnyLLMClient(mock, "x", "mock", "")

	names, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("Models() = %v, want 2 entries", names)
	}
}

// TestAgent_ChatStreamErrorSurfaces verifies that a stream-level error from
// the provider is returned as an error from Chat(), not silently discarded.
func TestAgent_ChatStreamErrorSurfaces(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{
		completionStreamFunc: func(_ context.Context, _ providers.CompletionParams) (<-chan providers.ChatCompletionChunk, <-chan error) {
			chunks := make(chan providers.ChatCompletionChunk)
			errs := make(chan error, 1)
			close(chunks)
			errs <- io.ErrUnexpectedEOF
			close(errs)
			return chunks, errs
		},
	}
	client := harvey.NewAnyLLMClient(mock, "m", "mock", "")

	_, err := client.Chat(context.Background(), nil, io.Discard)
	if err == nil {
		t.Fatal("expected error from broken stream, got nil")
	}
}

// TestAgent_EmbedderRoundTrip verifies AnyLLMEmbedder.Embed() returns a
// non-empty vector through the mock provider.
func TestAgent_EmbedderRoundTrip(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	emb, err := harvey.NewAnyLLMEmbedder(mock, "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	vec, err := emb.Embed("test sentence")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) == 0 {
		t.Error("Embed() returned empty vector")
	}
}
