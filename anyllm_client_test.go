package harvey_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers"
	"github.com/mozilla-ai/any-llm-go/providers/openai"

	"github.com/rsdoiel/harvey"
)

// ─── mock provider ────────────────────────────────────────────────────────────

// mockProvider implements anyllm.Provider for unit tests. All behaviour is
// controlled via function fields so individual tests can override only what
// they need.
type mockProvider struct {
	completionStreamFunc func(ctx context.Context, params providers.CompletionParams) (<-chan providers.ChatCompletionChunk, <-chan error)
	listModelsFunc       func(ctx context.Context) (*providers.ModelsResponse, error)
	embeddingFunc        func(ctx context.Context, params providers.EmbeddingParams) (*providers.EmbeddingResponse, error)
}

var _ anyllm.Provider = (*mockProvider)(nil)

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Completion(_ context.Context, params providers.CompletionParams) (*providers.ChatCompletion, error) {
	return &providers.ChatCompletion{
		Choices: []providers.Choice{{
			Message:      providers.Message{Role: providers.RoleAssistant, Content: "ok"},
			FinishReason: providers.FinishReasonStop,
		}},
		Usage: &providers.Usage{PromptTokens: 5, CompletionTokens: 1},
	}, nil
}

func (m *mockProvider) CompletionStream(ctx context.Context, params providers.CompletionParams) (<-chan providers.ChatCompletionChunk, <-chan error) {
	if m.completionStreamFunc != nil {
		return m.completionStreamFunc(ctx, params)
	}
	return defaultStream("Hello World", 10, 5)
}

func (m *mockProvider) ListModels(ctx context.Context) (*providers.ModelsResponse, error) {
	if m.listModelsFunc != nil {
		return m.listModelsFunc(ctx)
	}
	return &providers.ModelsResponse{
		Data: []providers.Model{
			{ID: "model-a"}, {ID: "model-b"},
		},
	}, nil
}

func (m *mockProvider) Embedding(ctx context.Context, params providers.EmbeddingParams) (*providers.EmbeddingResponse, error) {
	if m.embeddingFunc != nil {
		return m.embeddingFunc(ctx, params)
	}
	return &providers.EmbeddingResponse{
		Data: []providers.EmbeddingData{
			{Embedding: []float64{0.1, 0.2, 0.3}},
		},
	}, nil
}

// defaultStream returns a two-chunk stream: one content chunk and one final
// chunk carrying usage stats.
func defaultStream(content string, promptTokens, replyTokens int) (<-chan providers.ChatCompletionChunk, <-chan error) {
	chunks := make(chan providers.ChatCompletionChunk, 3)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		chunks <- providers.ChatCompletionChunk{
			Choices: []providers.ChunkChoice{{Delta: providers.ChunkDelta{Content: content}}},
		}
		chunks <- providers.ChatCompletionChunk{
			Choices: []providers.ChunkChoice{{FinishReason: providers.FinishReasonStop}},
			Usage:   &providers.Usage{PromptTokens: promptTokens, CompletionTokens: replyTokens},
		}
	}()
	return chunks, errs
}

// ─── AnyLLMClient unit tests ──────────────────────────────────────────────────

func TestAnyLLMClient_Chat_writesContent(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	client := harvey.NewAnyLLMClient(mock, "test-model", "mock", "")

	var buf strings.Builder
	stats, err := client.Chat(context.Background(), []harvey.Message{
		{Role: "user", Content: "hi"},
	}, &buf)

	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if buf.String() != "Hello World" {
		t.Errorf("output = %q, want %q", buf.String(), "Hello World")
	}
	if stats.ReplyTokens != 5 {
		t.Errorf("ReplyTokens = %d, want 5", stats.ReplyTokens)
	}
	if stats.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", stats.PromptTokens)
	}
	if stats.Elapsed <= 0 {
		t.Error("Elapsed should be positive")
	}
}

func TestAnyLLMClient_Chat_propagatesError(t *testing.T) {
	t.Parallel()
	wantErr := fmt.Errorf("stream broken")
	mock := &mockProvider{
		completionStreamFunc: func(_ context.Context, _ providers.CompletionParams) (<-chan providers.ChatCompletionChunk, <-chan error) {
			chunks := make(chan providers.ChatCompletionChunk)
			errs := make(chan error, 1)
			close(chunks)
			errs <- wantErr
			close(errs)
			return chunks, errs
		},
	}
	client := harvey.NewAnyLLMClient(mock, "m", "mock", "")
	_, err := client.Chat(context.Background(), nil, io.Discard)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAnyLLMClient_Chat_tokenRateComputed(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{
		completionStreamFunc: func(_ context.Context, _ providers.CompletionParams) (<-chan providers.ChatCompletionChunk, <-chan error) {
			chunks := make(chan providers.ChatCompletionChunk, 2)
			errs := make(chan error, 1)
			chunks <- providers.ChatCompletionChunk{
				Choices: []providers.ChunkChoice{{Delta: providers.ChunkDelta{Content: "x"}}},
			}
			chunks <- providers.ChatCompletionChunk{
				Choices: []providers.ChunkChoice{{FinishReason: providers.FinishReasonStop}},
				Usage:   &providers.Usage{PromptTokens: 4, CompletionTokens: 100},
			}
			close(chunks)
			close(errs)
			return chunks, errs
		},
	}
	client := harvey.NewAnyLLMClient(mock, "m", "mock", "")
	stats, err := client.Chat(context.Background(), nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TokensPerSec <= 0 {
		t.Errorf("TokensPerSec should be positive when ReplyTokens > 0, got %f", stats.TokensPerSec)
	}
}

func TestAnyLLMClient_Models_usesLister(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	client := harvey.NewAnyLLMClient(mock, "x", "mock", "")

	names, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "model-a" || names[1] != "model-b" {
		t.Errorf("Models() = %v, want [model-a model-b]", names)
	}
}

func TestAnyLLMClient_Models_fallbackWhenNoLister(t *testing.T) {
	t.Parallel()
	// A bare provider that does NOT implement ModelLister.
	type minimalProvider struct{ mockProvider }
	// Wrap so it satisfies Provider but not ModelLister.
	p := &struct {
		anyllm.Provider
	}{Provider: &mockProvider{}}

	client := harvey.NewAnyLLMClient(p, "only-model", "mock", "")
	names, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "only-model" {
		t.Errorf("Models() fallback = %v, want [only-model]", names)
	}
}

func TestAnyLLMClient_accessors(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	client := harvey.NewAnyLLMClient(mock, "qwen3:4b", "ollama", "http://localhost:11434")

	if client.ModelName() != "qwen3:4b" {
		t.Errorf("ModelName() = %q", client.ModelName())
	}
	if client.ProviderName() != "ollama" {
		t.Errorf("ProviderName() = %q", client.ProviderName())
	}
	if client.BackendURL() != "http://localhost:11434" {
		t.Errorf("BackendURL() = %q", client.BackendURL())
	}
	want := "ollama (qwen3:4b)"
	if client.Name() != want {
		t.Errorf("Name() = %q, want %q", client.Name(), want)
	}
}

func TestAnyLLMClient_Close(t *testing.T) {
	t.Parallel()
	client := harvey.NewAnyLLMClient(&mockProvider{}, "m", "mock", "")
	if err := client.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

// ─── AnyLLMEmbedder unit tests ────────────────────────────────────────────────

func TestAnyLLMEmbedder_Embed(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	emb, err := harvey.NewAnyLLMEmbedder(mock, "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	vec, err := emb.Embed("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 {
		t.Errorf("Embed() len = %d, want 3", len(vec))
	}
	if emb.Name() != "nomic-embed-text" {
		t.Errorf("Name() = %q", emb.Name())
	}
}

func TestNewAnyLLMEmbedder_errWhenNoEmbeddingSupport(t *testing.T) {
	t.Parallel()
	// Provider that does NOT implement EmbeddingProvider.
	p := &struct{ anyllm.Provider }{Provider: &mockProvider{}}
	_, err := harvey.NewAnyLLMEmbedder(p, "model")
	if err == nil {
		t.Error("expected error for provider without embedding support")
	}
}

// ─── FakeStreamingServer integration test ────────────────────────────────────
// Tests that the OpenAI-compatible base provider (used for future cloud routes)
// parses SSE correctly through AnyLLMClient.Chat. No real API calls, no cost.

func fakeStreamingServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		chunk := `{"id":"test","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":"pong"},"finish_reason":null}]}`
		done := `{"id":"test","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
		_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", done)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func fakeCompletionServer(t *testing.T) (string, func() map[string]any) {
	t.Helper()
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1700000000,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() map[string]any { return captured }
}

func TestAnyLLMClient_FakeSSEServer(t *testing.T) {
	// Verifies the channel→Writer bridge works end-to-end with a real HTTP server.
	serverURL := fakeStreamingServer(t)

	p, err := openai.NewCompatible(openai.CompatibleConfig{
		APIKeyEnvVar:   "",
		BaseURLEnvVar:  "",
		DefaultAPIKey:  "no-key",
		DefaultBaseURL: serverURL + "/v1",
		Name:           "fake",
		RequireAPIKey:  false,
		Capabilities: providers.Capabilities{
			Completion:         true,
			CompletionStreaming: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := harvey.NewAnyLLMClient(p, "test-model", "fake", serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var buf strings.Builder
	stats, err := client.Chat(ctx, []harvey.Message{{Role: "user", Content: "ping"}}, &buf)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if buf.String() != "pong" {
		t.Errorf("output = %q, want %q", buf.String(), "pong")
	}
	if stats.PromptTokens != 3 {
		t.Errorf("PromptTokens = %d, want 3", stats.PromptTokens)
	}
}
