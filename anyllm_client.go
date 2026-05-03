package harvey

import (
	"context"
	"fmt"
	"io"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/ollama"
)

/** AnyLLMClient wraps an anyllm.Provider and implements Harvey's LLMClient
 * interface. It bridges any-llm-go's channel-based streaming to Harvey's
 * io.Writer-based streaming, and maps anyllm.Usage to ChatStats.
 *
 * Fields:
 *   provider   (anyllm.Provider) — underlying provider (e.g. ollama, anthropic).
 *   modelName  (string)          — model name sent in every completion request.
 *   provName   (string)          — provider identifier, e.g. "ollama".
 *   backendURL (string)          — base URL for local backends; empty for cloud.
 *
 * Example:
 *   p, _ := ollama.New()
 *   client := NewAnyLLMClient(p, "llama3.1:8b", "ollama", "http://localhost:11434")
 *   stats, err := client.Chat(ctx, history, os.Stdout)
 */
type AnyLLMClient struct {
	provider   anyllm.Provider
	modelName  string
	provName   string
	backendURL string
}

/** NewAnyLLMClient creates an AnyLLMClient wrapping provider.
 *
 * Parameters:
 *   provider   (anyllm.Provider) — underlying any-llm-go provider.
 *   modelName  (string)          — model to request in completions.
 *   provName   (string)          — short provider name for display (e.g. "ollama").
 *   backendURL (string)          — base URL; pass "" for cloud providers.
 *
 * Returns:
 *   *AnyLLMClient — ready to use.
 *
 * Example:
 *   client := NewAnyLLMClient(p, "llama3.1:8b", "ollama", "http://localhost:11434")
 */
func NewAnyLLMClient(provider anyllm.Provider, modelName, provName, backendURL string) *AnyLLMClient {
	return &AnyLLMClient{
		provider:   provider,
		modelName:  modelName,
		provName:   provName,
		backendURL: backendURL,
	}
}

/** ModelName returns the model name this client sends in every request.
 *
 * Returns:
 *   string — model name, e.g. "llama3.1:8b".
 *
 * Example:
 *   fmt.Println(client.ModelName()) // "llama3.1:8b"
 */
func (a *AnyLLMClient) ModelName() string { return a.modelName }

/** BackendURL returns the base URL for local backends, or "" for cloud providers.
 *
 * Returns:
 *   string — base URL, e.g. "http://localhost:11434".
 *
 * Example:
 *   fmt.Println(client.BackendURL()) // "http://localhost:11434"
 */
func (a *AnyLLMClient) BackendURL() string { return a.backendURL }

/** ProviderName returns the short provider identifier.
 *
 * Returns:
 *   string — e.g. "ollama", "anthropic", "openai".
 *
 * Example:
 *   fmt.Println(client.ProviderName()) // "ollama"
 */
func (a *AnyLLMClient) ProviderName() string { return a.provName }

// Name satisfies LLMClient. Returns "provider (model)".
func (a *AnyLLMClient) Name() string { return a.provName + " (" + a.modelName + ")" }

/** Chat sends the conversation history to the provider, streams reply tokens to
 * out, and returns timing and token stats. Harvey's []Message is converted to
 * anyllm.Message before dispatch; the provider's channel-based stream is
 * drained into out.
 *
 * Parameters:
 *   ctx      (context.Context) — controls the request lifetime.
 *   messages ([]Message)       — full conversation history.
 *   out      (io.Writer)       — destination for streamed reply tokens.
 *
 * Returns:
 *   ChatStats — timing and token counts (zero where provider does not report).
 *   error     — non-nil on transport or API failure.
 *
 * Example:
 *   stats, err := client.Chat(ctx, agent.History, os.Stdout)
 */
func (a *AnyLLMClient) Chat(ctx context.Context, messages []Message, out io.Writer) (ChatStats, error) {
	anyllmMsgs := make([]anyllm.Message, len(messages))
	for i, m := range messages {
		anyllmMsgs[i] = anyllm.Message{Role: m.Role, Content: m.Content}
	}

	start := time.Now()
	chunks, errs := a.provider.CompletionStream(ctx, anyllm.CompletionParams{
		Model:    a.modelName,
		Messages: anyllmMsgs,
	})

	var stats ChatStats
	for chunk := range chunks {
		if len(chunk.Choices) > 0 {
			if delta := chunk.Choices[0].Delta.Content; delta != "" {
				fmt.Fprint(out, delta)
			}
		}
		// Capture usage from whichever chunk carries it (typically the final one).
		if chunk.Usage != nil {
			stats.PromptTokens = chunk.Usage.PromptTokens
			stats.ReplyTokens = chunk.Usage.CompletionTokens
		}
	}
	stats.Elapsed = time.Since(start)
	if stats.ReplyTokens > 0 && stats.Elapsed > 0 {
		stats.TokensPerSec = float64(stats.ReplyTokens) / stats.Elapsed.Seconds()
	}

	if err := <-errs; err != nil {
		return ChatStats{}, err
	}
	return stats, nil
}

/** Models returns the names of models available on this backend. For providers
 * that implement ModelLister the live list is fetched; others return a
 * single-element slice containing the configured model name.
 *
 * Parameters:
 *   ctx (context.Context) — controls the request lifetime.
 *
 * Returns:
 *   []string — model names.
 *   error    — non-nil on request failure.
 *
 * Example:
 *   names, err := client.Models(ctx)
 */
func (a *AnyLLMClient) Models(ctx context.Context) ([]string, error) {
	lister, ok := a.provider.(anyllm.ModelLister)
	if !ok {
		return []string{a.modelName}, nil
	}
	resp, err := lister.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(resp.Data))
	for i, m := range resp.Data {
		names[i] = m.ID
	}
	return names, nil
}

// Close satisfies LLMClient. any-llm-go providers hold no closeable resources.
func (a *AnyLLMClient) Close() error { return nil }

// ─── AnyLLMEmbedder ──────────────────────────────────────────────────────────

/** AnyLLMEmbedder wraps an anyllm.EmbeddingProvider and implements Harvey's
 * Embedder interface.
 *
 * Example:
 *   p, _ := ollama.New()
 *   e, _ := NewAnyLLMEmbedder(p, "nomic-embed-text")
 *   vec, err := e.Embed("hello world")
 */
type AnyLLMEmbedder struct {
	provider  anyllm.EmbeddingProvider
	modelName string
}

/** NewAnyLLMEmbedder creates an AnyLLMEmbedder from provider. Returns an error
 * if provider does not implement EmbeddingProvider.
 *
 * Parameters:
 *   provider  (anyllm.Provider) — provider to wrap; must implement EmbeddingProvider.
 *   modelName (string)          — embedding model name.
 *
 * Returns:
 *   *AnyLLMEmbedder — ready to call Embed.
 *   error           — if provider does not support embeddings.
 *
 * Example:
 *   p, _ := ollama.New()
 *   e, err := NewAnyLLMEmbedder(p, "nomic-embed-text")
 */
func NewAnyLLMEmbedder(provider anyllm.Provider, modelName string) (*AnyLLMEmbedder, error) {
	ep, ok := provider.(anyllm.EmbeddingProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support embeddings", provider.Name())
	}
	return &AnyLLMEmbedder{provider: ep, modelName: modelName}, nil
}

// Name satisfies Embedder. Returns the embedding model name.
func (e *AnyLLMEmbedder) Name() string { return e.modelName }

/** Embed sends text to the provider's embedding endpoint and returns the
 * embedding vector.
 *
 * Parameters:
 *   text (string) — text to embed.
 *
 * Returns:
 *   []float64 — embedding vector.
 *   error     — on transport failure or empty response.
 *
 * Example:
 *   vec, err := e.Embed("The sky is blue")
 *   fmt.Printf("dims: %d\n", len(vec))
 */
func (e *AnyLLMEmbedder) Embed(text string) ([]float64, error) {
	resp, err := e.provider.Embedding(context.Background(), anyllm.EmbeddingParams{
		Model: e.modelName,
		Input: text,
	})
	if err != nil {
		return nil, fmt.Errorf("anyllm embed: %w", err)
	}
	if len(resp.Data) == 0 || len(resp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("anyllm embed: empty response from %q", e.modelName)
	}
	return resp.Data[0].Embedding, nil
}

// ─── convenience constructors ─────────────────────────────────────────────────

/** newOllamaLLMClient creates an AnyLLMClient backed by a local Ollama server.
 * If baseURL is empty or invalid the Ollama default (localhost:11434) is used.
 *
 * Parameters:
 *   baseURL (string) — Ollama base URL, e.g. "http://localhost:11434".
 *   model   (string) — model name to use for completions.
 *
 * Returns:
 *   *AnyLLMClient — ready to use.
 *
 * Example:
 *   client := newOllamaLLMClient("http://localhost:11434", "llama3.1:8b")
 */
func newOllamaLLMClient(baseURL, model string) *AnyLLMClient {
	var opts []anyllm.Option
	if baseURL != "" {
		opts = append(opts, anyllm.WithBaseURL(baseURL))
	}
	p, err := ollama.New(opts...)
	if err != nil {
		// baseURL failed validation; fall back to Ollama's default.
		p, _ = ollama.New()
	}
	return NewAnyLLMClient(p, model, "ollama", baseURL)
}
