package harvey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/ollama"
)

/** AnyLLMClient wraps an anyllm.Provider and implements Harvey's LLMClient
 * interface. It bridges any-llm-go's channel-based streaming to Harvey's
 * io.Writer-based streaming, and maps anyllm.Usage to ChatStats.
 *
 * Fields:
 *   provider   (anyllm.Provider) — underlying provider (ollama).
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
	CallLog    ModelCallLog // optional: told of every model call for the session log
	maxTokens  int          // 0 = no limit
	engine     string       // engine label for the capability table (e.g. "hailo"); "" = none
}

// SetEngine records which engine's server this client talks to, so the
// engine's capability row applies (a "hailo" client never sends tools).
func (a *AnyLLMClient) SetEngine(engine string) { a.engine = engine }

// Engine returns the label set by SetEngine; "" when none was set.
func (a *AnyLLMClient) Engine() string { return a.engine }

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
 *   string — e.g. "ollama".
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
	stats, _, err := a.chatInternal(ctx, messages, nil, out)
	return stats, err
}

/** ChatWithTools sends the conversation history with tool schemas to the
 * provider, streams reply tokens to out, and returns accumulated tool calls
 * (if the model requests them) alongside timing stats.
 *
 * Parameters:
 *   ctx      (context.Context)  — controls the request lifetime.
 *   messages ([]Message)        — full conversation history including tool results.
 *   tools    ([]anyllm.Tool)    — tool schemas to send in CompletionParams.
 *   out      (io.Writer)        — destination for streamed reply tokens.
 *
 * Returns:
 *   ChatStats         — timing and token counts.
 *   []anyllm.ToolCall — tool calls requested by the model (nil if none).
 *   error             — non-nil on transport or API failure.
 *
 * Example:
 *   stats, calls, err := client.ChatWithTools(ctx, history, registry.GetToolSchemas(), os.Stdout)
 */
func (a *AnyLLMClient) ChatWithTools(ctx context.Context, messages []Message, tools []anyllm.Tool, out io.Writer) (ChatStats, []anyllm.ToolCall, error) {
	return a.chatInternal(ctx, messages, tools, out)
}

/** ErrStreamTruncated marks a chat reply whose stream ended before the server
 * said it was done: the connection was closed early, or the server sent
 * nothing. The reply so far is incomplete, so Chat returns it as an error
 * rather than as an empty or partial success. It classifies as io (exit 74).
 *
 * Example:
 *   if errors.Is(err, harvey.ErrStreamTruncated) { retry() }
 */
var ErrStreamTruncated = errors.New("chat stream truncated")

// chatInternal is the shared implementation for Chat and ChatWithTools.
// When tools is nil/empty, no tools are sent and tool calls are never returned.
func (a *AnyLLMClient) chatInternal(ctx context.Context, messages []Message, tools []anyllm.Tool, out io.Writer) (ChatStats, []anyllm.ToolCall, error) {
	anyllmMsgs := harvestMessagesToAnyllm(messages)

	params := anyllm.CompletionParams{
		Model:    a.modelName,
		Messages: anyllmMsgs,
	}
	// An engine that cannot take a tools key (hailo-ollama answers HTTP 500 to
	// any, even an empty array) is never sent one, whoever asks.
	if caps, ok := capsForEngine(a.engine); ok && caps.Tools == CapNo {
		tools = nil
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	if a.maxTokens > 0 {
		params.MaxTokens = &a.maxTokens
	}

	// Every model call through this client is one request and one response in
	// the session log, whoever made it (DR-0036): the client is the one place
	// all callers meet, so a caller cannot forget to log.
	out, text, end := a.beginCall(ctx, messages, len(tools), out)
	start := time.Now()
	chunks, errs := a.provider.CompletionStream(ctx, params)

	// accumulatedCalls holds tool call deltas merged by index.
	var accumulatedCalls []anyllm.ToolCall
	var stats ChatStats
	// finished is set when a chunk carries a finish reason. A stream that ends
	// without one was cut off: the provider does not report that as an error.
	finished := false

	for chunk := range chunks {
		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				stats.PromptTokens = chunk.Usage.PromptTokens
				stats.ReplyTokens = chunk.Usage.CompletionTokens
			}
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			finished = true
		}
		if delta := choice.Delta.Content; delta != "" {
			fmt.Fprint(out, delta)
		}
		// Merge incremental tool call deltas.
		for _, tc := range choice.Delta.ToolCalls {
			mergeToolCallDelta(&accumulatedCalls, tc)
		}
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
		end(text(), ChatStats{}, err)
		return ChatStats{}, nil, err
	}

	if !finished {
		err := ClassedAs(ClassIO, fmt.Errorf("%w: the %s stream ended before the reply was complete", ErrStreamTruncated, a.provName))
		end(text(), ChatStats{}, err)
		return ChatStats{}, nil, err
	}

	end(text(), stats, nil)

	if len(accumulatedCalls) == 0 {
		return stats, nil, nil
	}
	return stats, accumulatedCalls, nil
}

// harvestMessagesToAnyllm converts Harvey []Message to []anyllm.Message.
// When a message has Parts set, those are used as the Content (multimodal);
// otherwise the plain string Content is used.
func harvestMessagesToAnyllm(messages []Message) []anyllm.Message {
	out := make([]anyllm.Message, len(messages))
	for i, m := range messages {
		var content any
		if len(m.Parts) > 0 {
			content = m.Parts
		} else {
			content = m.Content
		}
		out[i] = anyllm.Message{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}
	return out
}

// mergeToolCallDelta merges a streaming ToolCall delta into accumulatedCalls.
// Deltas for the same index are concatenated; new indices are appended.
func mergeToolCallDelta(acc *[]anyllm.ToolCall, delta anyllm.ToolCall) {
	// Find by matching ID or by appending if this is a new call.
	for i := range *acc {
		if (*acc)[i].ID == delta.ID || (delta.ID == "" && i == len(*acc)-1) {
			(*acc)[i].Function.Arguments += delta.Function.Arguments
			if delta.Function.Name != "" && (*acc)[i].Function.Name == "" {
				(*acc)[i].Function.Name = delta.Function.Name
			}
			if delta.Type != "" && (*acc)[i].Type == "" {
				(*acc)[i].Type = delta.Type
			}
			return
		}
	}
	// New tool call.
	*acc = append(*acc, delta)
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

/** SetMaxTokens configures the per-request completion token limit.
 * A value of 0 means no limit (the provider or model decides).
 *
 * Parameters:
 *   n (int) — maximum tokens to generate; 0 = unlimited.
 *
 * Example:
 *   client.SetMaxTokens(4096)
 */
func (a *AnyLLMClient) SetMaxTokens(n int) { a.maxTokens = n }

/** ProviderCapabilities returns the feature flags reported by the underlying
 * provider. When the provider does not implement CapabilityProvider, a
 * conservative set with only Completion and CompletionStreaming is returned.
 * No network call is made — all providers return static capability data.
 *
 * Returns:
 *   anyllm.Capabilities — static flags for this provider.
 *
 * Example:
 *   caps := client.ProviderCapabilities()
 *   if caps.CompletionTools { fmt.Println("tools supported") }
 */
func (a *AnyLLMClient) ProviderCapabilities() anyllm.Capabilities {
	if cp, ok := a.provider.(anyllm.CapabilityProvider); ok {
		return cp.Capabilities()
	}
	return anyllm.Capabilities{Completion: true, CompletionStreaming: true}
}

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
// timeout for local providers (Ollama). When timeout is
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

/** NewOllamaLLMClient creates an AnyLLMClient for an Ollama or hailo-ollama
 * server. It is the exported variant of newOllamaLLMClient, for cmd/assay and
 * other programs outside the harvey package.
 *
 * Parameters:
 *   baseURL (string)        — bare base URL, e.g. "http://localhost:11434"
 *   model (string)          — model name to pass in each chat request
 *   timeout (time.Duration) — HTTP timeout; pass 0 for no timeout
 *
 * Returns:
 *   *AnyLLMClient — ready-to-use client implementing LLMClient
 *
 * Example:
 *   client := harvey.NewOllamaLLMClient("http://localhost:11434", "llama3.2:3b", 120*time.Second)
 *   defer client.Close()
 */
func NewOllamaLLMClient(baseURL, model string, timeout time.Duration) *AnyLLMClient {
	return newOllamaLLMClient(baseURL, model, timeout)
}

/** ModelCallLog is told about each model call an AnyLLMClient makes, so the
 * session log can record it (DR-0036, DR-0038). Agent implements it.
 *
 * Methods:
 *   BeginModelCall(ctx, msgs, toolCount) — a call is about to be made; the
 *     returned func is called once with the reply text, the usage as the
 *     backend reported it, and the error (nil on success).
 *
 * Example:
 *   client.CallLog = agent
 */
type ModelCallLog interface {
	BeginModelCall(ctx context.Context, msgs []Message, toolCount int) func(text string, stats ChatStats, err error)
}

// beginCall brackets one model call for the session log. With no CallLog it
// returns out unchanged; otherwise a writer that also captures the streamed
// text, a func giving that text, and the func to call when the call ends.
func (a *AnyLLMClient) beginCall(ctx context.Context, msgs []Message, toolCount int, out io.Writer) (io.Writer, func() string, func(string, ChatStats, error)) {
	if a.CallLog == nil {
		return out, func() string { return "" }, func(string, ChatStats, error) {}
	}
	end := a.CallLog.BeginModelCall(ctx, msgs, toolCount)
	var captured strings.Builder
	return io.MultiWriter(out, &captured), captured.String, end
}
