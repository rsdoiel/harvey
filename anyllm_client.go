package harvey

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/anthropic"
	"github.com/mozilla-ai/any-llm-go/providers/deepseek"
	"github.com/mozilla-ai/any-llm-go/providers/gemini"
	"github.com/mozilla-ai/any-llm-go/providers/llamacpp"
	"github.com/mozilla-ai/any-llm-go/providers/llamafile"
	"github.com/mozilla-ai/any-llm-go/providers/mistral"
	"github.com/mozilla-ai/any-llm-go/providers/ollama"
	"github.com/mozilla-ai/any-llm-go/providers/openai"
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

// localProviderHTTPOpt returns an anyllm.Option that controls the HTTP client
// timeout for local providers (Ollama, Llamafile, llama.cpp). When timeout is
// zero the HTTP client has no timeout, which is correct for long local inference
// on slow hardware. When timeout is positive, WithTimeout is used.
func localProviderHTTPOpt(timeout time.Duration) anyllm.Option {
	if timeout <= 0 {
		return anyllm.WithHTTPClient(&http.Client{})
	}
	return anyllm.WithTimeout(timeout)
}

/** newOllamaLLMClient creates an AnyLLMClient backed by a local Ollama server.
 * If baseURL is empty or invalid the Ollama default (localhost:11434) is used.
 * Pass timeout=0 for no HTTP timeout (recommended for slow hardware).
 *
 * Parameters:
 *   baseURL  (string)        — Ollama base URL, e.g. "http://localhost:11434".
 *   model    (string)        — model name to use for completions.
 *   timeout  (time.Duration) — HTTP client timeout; 0 means no timeout.
 *
 * Returns:
 *   *AnyLLMClient — ready to use.
 *
 * Example:
 *   client := newOllamaLLMClient("http://localhost:11434", "llama3.1:8b", 0)
 */
func newOllamaLLMClient(baseURL, model string, timeout time.Duration) *AnyLLMClient {
	opts := []anyllm.Option{localProviderHTTPOpt(timeout)}
	if baseURL != "" {
		opts = append(opts, anyllm.WithBaseURL(baseURL))
	}
	p, err := ollama.New(opts...)
	if err != nil {
		p, _ = ollama.New(localProviderHTTPOpt(timeout))
	}
	return NewAnyLLMClient(p, model, "ollama", baseURL)
}

// newLlamafileLLMClient creates an AnyLLMClient backed by a Llamafile server.
// apiURL must be the full /v1 base URL, e.g. "http://localhost:8080/v1".
// Pass timeout=0 for no HTTP timeout.
func newLlamafileLLMClient(apiURL, model string, timeout time.Duration) *AnyLLMClient {
	opts := []anyllm.Option{localProviderHTTPOpt(timeout)}
	if apiURL != "" {
		opts = append(opts, anyllm.WithBaseURL(apiURL))
	}
	p, err := llamafile.New(opts...)
	if err != nil {
		p, _ = llamafile.New(localProviderHTTPOpt(timeout))
	}
	return NewAnyLLMClient(p, model, "llamafile", apiURL)
}

// newLlamaCppLLMClient creates an AnyLLMClient backed by a llama.cpp server.
// apiURL must be the full /v1 base URL, e.g. "http://127.0.0.1:8080/v1".
// Pass timeout=0 for no HTTP timeout.
func newLlamaCppLLMClient(apiURL, model string, timeout time.Duration) *AnyLLMClient {
	opts := []anyllm.Option{localProviderHTTPOpt(timeout)}
	if apiURL != "" {
		opts = append(opts, anyllm.WithBaseURL(apiURL))
	}
	p, err := llamacpp.New(opts...)
	if err != nil {
		p, _ = llamacpp.New(localProviderHTTPOpt(timeout))
	}
	return NewAnyLLMClient(p, model, "llamacpp", apiURL)
}

// newAnthropicLLMClient creates an AnyLLMClient backed by Anthropic's API.
// Reads ANTHROPIC_API_KEY from the environment; returns an error if not set.
func newAnthropicLLMClient(model string) (*AnyLLMClient, error) {
	p, err := anthropic.New()
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w (set ANTHROPIC_API_KEY)", err)
	}
	return NewAnyLLMClient(p, model, "anthropic", ""), nil
}

// newDeepSeekLLMClient creates an AnyLLMClient backed by DeepSeek's API.
// Reads DEEPSEEK_API_KEY from the environment; returns an error if not set.
func newDeepSeekLLMClient(model string) (*AnyLLMClient, error) {
	p, err := deepseek.New()
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w (set DEEPSEEK_API_KEY)", err)
	}
	return NewAnyLLMClient(p, model, "deepseek", ""), nil
}

// newGeminiLLMClient creates an AnyLLMClient backed by Google Gemini's API.
// Reads GEMINI_API_KEY or GOOGLE_API_KEY from the environment.
func newGeminiLLMClient(model string) (*AnyLLMClient, error) {
	p, err := gemini.New()
	if err != nil {
		return nil, fmt.Errorf("gemini: %w (set GEMINI_API_KEY or GOOGLE_API_KEY)", err)
	}
	return NewAnyLLMClient(p, model, "gemini", ""), nil
}

// newMistralLLMClient creates an AnyLLMClient backed by Mistral's API.
// Reads MISTRAL_API_KEY from the environment; returns an error if not set.
func newMistralLLMClient(model string) (*AnyLLMClient, error) {
	p, err := mistral.New()
	if err != nil {
		return nil, fmt.Errorf("mistral: %w (set MISTRAL_API_KEY)", err)
	}
	return NewAnyLLMClient(p, model, "mistral", ""), nil
}

// newOpenAILLMClient creates an AnyLLMClient backed by OpenAI's API.
// Reads OPENAI_API_KEY from the environment; returns an error if not set.
func newOpenAILLMClient(model string) (*AnyLLMClient, error) {
	p, err := openai.New()
	if err != nil {
		return nil, fmt.Errorf("openai: %w (set OPENAI_API_KEY)", err)
	}
	return NewAnyLLMClient(p, model, "openai", ""), nil
}
