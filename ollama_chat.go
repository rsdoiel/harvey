package harvey

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

/** ErrStreamTruncated marks a chat reply whose stream ended before the server
 * said it was done: the connection was closed early, or the server sent
 * nothing. The reply so far is incomplete, so Chat returns it as an error
 * rather than as an empty or partial success. It classifies as io (exit 74).
 *
 * Example:
 *   if errors.Is(err, harvey.ErrStreamTruncated) { retry() }
 */
var ErrStreamTruncated = errors.New("chat stream truncated")

// defaultOllamaURL is where an Ollama server listens unless told otherwise.
const defaultOllamaURL = "http://localhost:11434"

/** OllamaChatClient speaks Ollama's /api/chat to an Ollama or hailo-ollama
 * server (workspace DR-0046). It implements LLMClient and ToolCapable.
 *
 * Fields:
 *   CallLog (ModelCallLog) — optional; told of every model call for the session log.
 *
 * Example:
 *   c := NewOllamaLLMClient("http://localhost:11434", "llama3.2:3b", 0)
 *   stats, err := c.Chat(ctx, history, os.Stdout)
 */
type OllamaChatClient struct {
	baseURL   string
	modelName string
	provName  string
	httpc     *http.Client
	CallLog   ModelCallLog // optional: told of every model call for the session log
	maxTokens int          // 0 = no limit
	engine    string       // engine label for the capability table (e.g. "hailo"); "" = none
	window    func() int   // the context window harvey believes; nil or 0 = unknown, send no num_ctx
}

/** ModelCallLog is told about each model call an OllamaChatClient makes, so the
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

/** newOllamaLLMClient creates an OllamaChatClient for the server at baseURL.
 * An empty baseURL means the Ollama default (localhost:11434). Pass timeout=0
 * for no HTTP timeout (recommended for slow hardware).
 *
 * Parameters:
 *   baseURL (string)        — Ollama base URL, e.g. "http://localhost:11434".
 *   model   (string)        — model name to use for completions.
 *   timeout (time.Duration) — HTTP client timeout; 0 means no timeout.
 *
 * Returns:
 *   *OllamaChatClient — ready to use.
 *
 * Example:
 *   client := newOllamaLLMClient("http://localhost:11434", "llama3.1:8b", 0)
 */
func newOllamaLLMClient(baseURL, model string, timeout time.Duration) *OllamaChatClient {
	if baseURL == "" {
		baseURL = defaultOllamaURL
	}
	return &OllamaChatClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		modelName: model,
		provName:  "ollama",
		httpc:     &http.Client{Timeout: timeout},
	}
}

/** NewOllamaLLMClient is the exported newOllamaLLMClient, for cmd/assay and
 * other programs outside the harvey package.
 *
 * Parameters:
 *   baseURL (string)        — bare base URL, e.g. "http://localhost:11434"
 *   model   (string)        — model name to pass in each chat request
 *   timeout (time.Duration) — HTTP timeout; pass 0 for no timeout
 *
 * Returns:
 *   *OllamaChatClient — ready-to-use client implementing LLMClient
 *
 * Example:
 *   client := harvey.NewOllamaLLMClient("http://localhost:11434", "llama3.2:3b", 120*time.Second)
 *   defer client.Close()
 */
func NewOllamaLLMClient(baseURL, model string, timeout time.Duration) *OllamaChatClient {
	return newOllamaLLMClient(baseURL, model, timeout)
}

// SetEngine records which engine's server this client talks to, so the
// engine's capability row applies (a "hailo" client never sends tools).
func (a *OllamaChatClient) SetEngine(engine string) { a.engine = engine }

// Engine returns the label set by SetEngine; "" when none was set.
func (a *OllamaChatClient) Engine() string { return a.engine }

/** SetWindow tells the client where to read the context window harvey believes
 * the model has. When fn returns a number above zero it is sent as
 * options.num_ctx; otherwise no num_ctx is sent and the server's default
 * applies (workspace DR-0046).
 *
 * Parameters:
 *   fn (func() int) — returns the window in tokens, or 0 when unknown; nil clears it.
 *
 * Example:
 *   client.SetWindow(agent.effectiveContextLimit)
 */
func (a *OllamaChatClient) SetWindow(fn func() int) { a.window = fn }

/** ModelName returns the model name this client sends in every request.
 *
 * Returns:
 *   string — model name, e.g. "llama3.1:8b".
 *
 * Example:
 *   fmt.Println(client.ModelName()) // "llama3.1:8b"
 */
func (a *OllamaChatClient) ModelName() string { return a.modelName }

/** BackendURL returns the server's base URL.
 *
 * Returns:
 *   string — base URL, e.g. "http://localhost:11434".
 *
 * Example:
 *   fmt.Println(client.BackendURL()) // "http://localhost:11434"
 */
func (a *OllamaChatClient) BackendURL() string { return a.baseURL }

/** ProviderName returns the short provider identifier.
 *
 * Returns:
 *   string — "ollama".
 *
 * Example:
 *   fmt.Println(client.ProviderName()) // "ollama"
 */
func (a *OllamaChatClient) ProviderName() string { return a.provName }

// Name satisfies LLMClient. Returns "provider (model)".
func (a *OllamaChatClient) Name() string { return a.provName + " (" + a.modelName + ")" }

// Close satisfies LLMClient. The client holds no closeable resources.
func (a *OllamaChatClient) Close() error { return nil }

/** SetMaxTokens configures the per-request completion token limit.
 * A value of 0 means no limit (the model decides).
 *
 * Parameters:
 *   n (int) — maximum tokens to generate; 0 = unlimited.
 *
 * Example:
 *   client.SetMaxTokens(4096)
 */
func (a *OllamaChatClient) SetMaxTokens(n int) { a.maxTokens = n }

/** ProviderCapabilities returns the feature flags for an Ollama server. No
 * network call is made.
 *
 * Returns:
 *   Capabilities — static flags.
 *
 * Example:
 *   if client.ProviderCapabilities().CompletionTools { fmt.Println("tools supported") }
 */
func (a *OllamaChatClient) ProviderCapabilities() Capabilities {
	return Capabilities{
		Completion: true, CompletionImage: true, CompletionReasoning: true,
		CompletionStreaming: true, CompletionTools: true, Embedding: true, ListModels: true,
	}
}

/** Models returns the names of the models installed on the server.
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
func (a *OllamaChatClient) Models(ctx context.Context) ([]string, error) {
	summaries, err := NewOllamaClient(a.baseURL, "").ModelSummaries(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(summaries))
	for i, s := range summaries {
		names[i] = s.Name
	}
	return names, nil
}

/** Chat sends the conversation history, streams reply tokens to out, and
 * returns timing and token stats.
 *
 * Parameters:
 *   ctx      (context.Context) — controls the request lifetime.
 *   messages ([]Message)       — full conversation history.
 *   out      (io.Writer)       — destination for streamed reply tokens.
 *
 * Returns:
 *   ChatStats — timing and token counts (zero where the server does not report).
 *   error     — non-nil on transport or API failure.
 *
 * Example:
 *   stats, err := client.Chat(ctx, agent.History, os.Stdout)
 */
func (a *OllamaChatClient) Chat(ctx context.Context, messages []Message, out io.Writer) (ChatStats, error) {
	stats, _, err := a.chatInternal(ctx, messages, nil, out)
	return stats, err
}

/** ChatWithTools sends the conversation history with tool schemas, streams
 * reply tokens to out, and returns the tool calls the model requested.
 *
 * Parameters:
 *   ctx      (context.Context) — controls the request lifetime.
 *   messages ([]Message)       — full conversation history including tool results.
 *   tools    ([]Tool)          — tool schemas to offer.
 *   out      (io.Writer)       — destination for streamed reply tokens.
 *
 * Returns:
 *   ChatStats  — timing and token counts.
 *   []ToolCall — tool calls requested by the model (nil if none).
 *   error      — non-nil on transport or API failure.
 *
 * Example:
 *   stats, calls, err := client.ChatWithTools(ctx, history, registry.GetToolSchemas(), os.Stdout)
 */
func (a *OllamaChatClient) ChatWithTools(ctx context.Context, messages []Message, tools []Tool, out io.Writer) (ChatStats, []ToolCall, error) {
	return a.chatInternal(ctx, messages, tools, out)
}

// ─── wire types ──────────────────────────────────────────────────────────────

type chatWireToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type chatWireMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	Images    []string           `json:"images,omitempty"`
	ToolCalls []chatWireToolCall `json:"tool_calls,omitempty"`
	ToolName  string             `json:"tool_name,omitempty"`
}

type chatWireRequest struct {
	Model    string            `json:"model"`
	Messages []chatWireMessage `json:"messages"`
	Tools    []Tool            `json:"tools,omitempty"`
	Stream   bool              `json:"stream"`
	Options  map[string]any    `json:"options,omitempty"`
}

type chatWireResponse struct {
	Message struct {
		Content   string             `json:"content"`
		Thinking  string             `json:"thinking"`
		ToolCalls []chatWireToolCall `json:"tool_calls"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error"`
}

// toWireMessages converts harvey's messages to Ollama's. A tool result is sent
// with role "tool" and the tool's name (workspace DR-0046), found from the
// call it answers; an image part is sent as base64 in images.
func toWireMessages(messages []Message) []chatWireMessage {
	callNames := map[string]string{}
	out := make([]chatWireMessage, 0, len(messages))
	for _, m := range messages {
		wm := chatWireMessage{Role: m.Role, Content: m.Content}
		if len(m.Parts) > 0 {
			var text []string
			for _, p := range m.Parts {
				switch {
				case p.Type == "text" && p.Text != "":
					text = append(text, p.Text)
				case p.Type == "image_url" && p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:"):
					if i := strings.IndexByte(p.ImageURL.URL, ','); i >= 0 {
						wm.Images = append(wm.Images, p.ImageURL.URL[i+1:])
					}
				}
			}
			wm.Content = strings.Join(text, "\n")
		}
		for _, tc := range m.ToolCalls {
			callNames[tc.ID] = tc.Function.Name
			var wc chatWireToolCall
			wc.Function.Name = tc.Function.Name
			wc.Function.Arguments = map[string]any{}
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &wc.Function.Arguments)
			wm.ToolCalls = append(wm.ToolCalls, wc)
		}
		if m.Role == "tool" {
			wm.ToolName = callNames[m.ToolCallID]
		}
		out = append(out, wm)
	}
	return out
}

// buildRequest assembles the /api/chat request body.
func (a *OllamaChatClient) buildRequest(messages []Message, tools []Tool) chatWireRequest {
	req := chatWireRequest{
		Model:    a.modelName,
		Messages: toWireMessages(messages),
		Stream:   true,
	}
	// An engine that cannot take a tools key (hailo-ollama answers HTTP 500 to
	// any, even an empty array) is never sent one, whoever asks.
	if caps, ok := capsForEngine(a.engine); ok && caps.Tools == CapNo {
		tools = nil
	}
	if len(tools) > 0 {
		req.Tools = tools
	}
	opts := map[string]any{}
	if a.window != nil {
		if n := a.window(); n > 0 {
			opts["num_ctx"] = n
		}
	}
	if a.maxTokens > 0 {
		opts["num_predict"] = a.maxTokens
	}
	if len(opts) > 0 {
		req.Options = opts
	}
	return req
}

// chatInternal is the shared implementation for Chat and ChatWithTools.
// When tools is nil/empty, no tools are sent and tool calls are never returned.
func (a *OllamaChatClient) chatInternal(ctx context.Context, messages []Message, tools []Tool, out io.Writer) (ChatStats, []ToolCall, error) {
	req := a.buildRequest(messages, tools)

	// Every model call through this client is one request and one response in
	// the session log, whoever made it (DR-0036): the client is the one place
	// all callers meet, so a caller cannot forget to log.
	out, text, end := a.beginCall(ctx, messages, len(req.Tools), out)
	fail := func(err error) (ChatStats, []ToolCall, error) {
		end(text(), ChatStats{}, err)
		return ChatStats{}, nil, err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fail(fmt.Errorf("ollama chat: %w", err))
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fail(fmt.Errorf("ollama chat: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := a.httpc.Do(httpReq)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(chatStatusError(resp))
	}

	var calls []ToolCall
	var stats ChatStats
	finished := false
	rd := bufio.NewReader(resp.Body)
	for {
		line, rerr := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var chunk chatWireResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				return fail(ClassedAs(ClassData, fmt.Errorf("ollama chat: bad stream line: %w", err)))
			}
			if chunk.Error != "" {
				return fail(ClassedAs(ClassUnavailable, &ChatStatusError{Message: chunk.Error}))
			}
			if chunk.Message.Content != "" {
				fmt.Fprint(out, chunk.Message.Content)
			}
			for _, wc := range chunk.Message.ToolCalls {
				args, _ := json.Marshal(wc.Function.Arguments)
				if len(wc.Function.Arguments) == 0 {
					args = []byte("{}")
				}
				calls = append(calls, ToolCall{
					ID:       fmt.Sprintf("call_%d", len(calls)),
					Type:     "function",
					Function: FunctionCall{Name: wc.Function.Name, Arguments: string(args)},
				})
			}
			if chunk.Done {
				finished = true
				stats.PromptTokens = chunk.PromptEvalCount
				stats.ReplyTokens = chunk.EvalCount
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				if ctx.Err() != nil {
					return fail(ctx.Err())
				}
				return fail(ClassedAs(ClassIO, fmt.Errorf("%w: %v", ErrStreamTruncated, rerr)))
			}
			break
		}
	}

	if !finished {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(ClassedAs(ClassIO, fmt.Errorf("%w: the %s stream ended before the reply was complete", ErrStreamTruncated, a.provName)))
	}

	stats.Elapsed = time.Since(start)
	if stats.ReplyTokens > 0 && stats.Elapsed > 0 {
		stats.TokensPerSec = float64(stats.ReplyTokens) / stats.Elapsed.Seconds()
	}
	end(text(), stats, nil)
	return stats, calls, nil
}

// chatStatusError turns a non-200 reply into an error carrying the server's
// message when it sent one.
func chatStatusError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	msg := strings.TrimSpace(string(b))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		msg = e.Error
	}
	if msg == "" {
		msg = resp.Status
	}
	return ClassedAs(ClassUnavailable, &ChatStatusError{Status: resp.StatusCode, Message: msg})
}

/** ChatStatusError is an error answer from the server: an HTTP status that was
 * not 200, or an error line in the stream (Status 0). The retry logic reads it
 * to tell an oversize prompt from other failures.
 *
 * Fields:
 *   Status  (int)    — the HTTP status; 0 for an error line inside a 200 stream.
 *   Message (string) — the server's message.
 *
 * Example:
 *   var se *ChatStatusError
 *   if errors.As(err, &se) && se.Status == 500 { retrySmaller() }
 */
type ChatStatusError struct {
	Status  int
	Message string
}

// Error satisfies error.
func (e *ChatStatusError) Error() string {
	if e.Status == 0 {
		return "ollama chat: " + e.Message
	}
	return fmt.Sprintf("ollama chat: %d %s: %s", e.Status, http.StatusText(e.Status), e.Message)
}

// beginCall brackets one model call for the session log. With no CallLog it
// returns out unchanged; otherwise a writer that also captures the streamed
// text, a func giving that text, and the func to call when the call ends.
func (a *OllamaChatClient) beginCall(ctx context.Context, msgs []Message, toolCount int, out io.Writer) (io.Writer, func() string, func(string, ChatStats, error)) {
	if a.CallLog == nil {
		return out, func() string { return "" }, func(string, ChatStats, error) {}
	}
	end := a.CallLog.BeginModelCall(ctx, msgs, toolCount)
	var captured strings.Builder
	return io.MultiWriter(out, &captured), captured.String, end
}
