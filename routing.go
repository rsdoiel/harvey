package harvey

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// RecentContextN is the number of recent non-system history messages sent
// alongside a dispatched prompt. Excludes system messages. Tune over time.
const RecentContextN = 10

// RouteKind identifies the protocol used to reach a registered endpoint.
type RouteKind string

const (
	// Local providers — no API key required.
	KindOllama    RouteKind = "ollama"
	KindLlamafile RouteKind = "llamafile"
	KindLlamaCpp  RouteKind = "llamacpp"

	// Cloud providers — credentials read from environment variables.
	KindAnthropic RouteKind = "anthropic"
	KindDeepSeek  RouteKind = "deepseek"
	KindGemini    RouteKind = "gemini"
	KindMistral   RouteKind = "mistral"
	KindOpenAI    RouteKind = "openai"
)

/** RouteEndpoint is a named remote LLM endpoint registered for @mention dispatch.
 *
 * Fields:
 *   Name  (string)    — identifier used in @mention syntax (e.g. "pi2").
 *   URL   (string)    — endpoint URL; e.g. "ollama://host:port" or "anthropic://".
 *   Model (string)    — default model on this endpoint; empty falls back to Config defaults.
 *   Kind  (RouteKind) — KindOllama, KindLlamafile, KindLlamaCpp, etc.
 *   Tools (bool)      — when true, dispatch uses ToolExecutor for tool calling.
 *
 * Example:
 *   ep := RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.12:11434", Model: "llama3.1:8b", Kind: KindOllama}
 */
type RouteEndpoint struct {
	Name  string
	URL   string
	Model string
	Kind  RouteKind
	Tools bool `json:",omitempty"` // opt-in tool calling via ToolExecutor
}

/** RouteRegistry holds all registered endpoints and the routing-enabled flag.
 *
 * Example:
 *   rr := NewRouteRegistry()
 *   rr.Add(&RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.12:11434", Kind: KindOllama})
 */
type RouteRegistry struct {
	Endpoints map[string]*RouteEndpoint
	Enabled   bool
}

/** NewRouteRegistry returns an empty registry with routing enabled.
 *
 * Returns:
 *   *RouteRegistry — empty, enabled registry.
 *
 * Example:
 *   rr := NewRouteRegistry()
 */
func NewRouteRegistry() *RouteRegistry {
	return &RouteRegistry{
		Endpoints: make(map[string]*RouteEndpoint),
		Enabled:   true,
	}
}

/** Add registers ep in the registry, replacing any existing endpoint with the
 * same name.
 *
 * Parameters:
 *   ep (*RouteEndpoint) — endpoint to register.
 *
 * Example:
 *   rr.Add(&RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.12:11434", Kind: KindOllama})
 */
func (rr *RouteRegistry) Add(ep *RouteEndpoint) {
	rr.Endpoints[ep.Name] = ep
}

/** Remove deletes the endpoint with the given name. No-op if not found.
 *
 * Parameters:
 *   name (string) — endpoint name to remove.
 *
 * Example:
 *   rr.Remove("pi2")
 */
func (rr *RouteRegistry) Remove(name string) {
	delete(rr.Endpoints, name)
}

/** Lookup returns the endpoint registered under name, or nil if not found.
 *
 * Parameters:
 *   name (string) — endpoint name.
 *
 * Returns:
 *   *RouteEndpoint — registered endpoint; nil if not found.
 *
 * Example:
 *   ep := rr.Lookup("pi2")
 *   if ep == nil { fmt.Println("not registered") }
 */
func (rr *RouteRegistry) Lookup(name string) *RouteEndpoint {
	return rr.Endpoints[name]
}

/** ParseAtMention extracts the @name prefix and remaining prompt from input.
 * Returns ok=false when input does not start with "@" followed by a non-empty
 * word. Leading and trailing whitespace is trimmed before parsing.
 *
 * Parameters:
 *   input (string) — raw user input, potentially starting with "@name ".
 *
 * Returns:
 *   name   (string) — endpoint name without the "@" sigil.
 *   prompt (string) — remaining input after the @name token; may be empty.
 *   ok     (bool)   — false when no @mention is present.
 *
 * Example:
 *   name, prompt, ok := ParseAtMention("@pi2 write a Go parser")
 *   // name="pi2", prompt="write a Go parser", ok=true
 *
 *   _, _, ok = ParseAtMention("just a normal prompt")
 *   // ok=false
 */
func ParseAtMention(input string) (name, prompt string, ok bool) {
	s := strings.TrimSpace(input)
	if !strings.HasPrefix(s, "@") {
		return "", "", false
	}
	s = s[1:]
	idx := strings.IndexByte(s, ' ')
	if idx < 0 {
		name = strings.TrimSpace(s)
		if name == "" {
			return "", "", false
		}
		return name, "", true
	}
	name = strings.TrimSpace(s[:idx])
	prompt = strings.TrimSpace(s[idx+1:])
	if name == "" {
		return "", "", false
	}
	return name, prompt, true
}

// recentHistory returns up to n non-system messages from history, preserving
// chronological order. System messages are always excluded.
func recentHistory(history []Message, n int) []Message {
	var nonSystem []Message
	for _, m := range history {
		if m.Role != "system" {
			nonSystem = append(nonSystem, m)
		}
	}
	if len(nonSystem) <= n {
		return nonSystem
	}
	return nonSystem[len(nonSystem)-n:]
}

/** DispatchToEndpoint sends the recent conversation context plus prompt to ep,
 * streams the reply to out, and returns the full reply text. The context window
 * is capped at RecentContextN non-system messages from history.
 *
 * When ep.Tools is true and registry is non-nil, the call goes through
 * ToolExecutor so the remote model can invoke Harvey's local tools. Harvey's
 * existing permission constraints (safe_mode, allowed_commands, permissions)
 * are enforced by the registry — the remote model cannot bypass them.
 *
 * Parameters:
 *   ctx      (context.Context) — controls the HTTP request lifetime.
 *   ep       (*RouteEndpoint)  — registered endpoint to send to.
 *   history  ([]Message)       — full local conversation history.
 *   prompt   (string)          — current user prompt (already stripped of @mention).
 *   cfg      (*Config)         — used to resolve model defaults and tool limits.
 *   registry (*ToolRegistry)   — Harvey's tool registry; nil disables tool calling.
 *   out      (io.Writer)       — destination for streamed reply tokens.
 *
 * Returns:
 *   reply (string) — full reply text.
 *   err   (error)  — non-nil on transport or API failure.
 *
 * Example:
 *   reply, err := DispatchToEndpoint(ctx, ep, agent.History, "write a parser", cfg, agent.Tools, os.Stdout)
 */
func DispatchToEndpoint(ctx context.Context, ep *RouteEndpoint, history []Message, prompt string, cfg *Config, registry *ToolRegistry, out io.Writer) (string, error) {
	msgs := recentHistory(history, RecentContextN)
	msgs = append(msgs, Message{Role: "user", Content: prompt})

	client, err := clientForEndpoint(ep, cfg)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	w := io.MultiWriter(&buf, out)

	if ep.Tools && registry != nil {
		ex := NewToolExecutor(registry, client, cfg)
		if _, _, err := ex.RunToolLoop(ctx, msgs, w); err != nil {
			return "", fmt.Errorf("route %s: %w", ep.Name, err)
		}
		return buf.String(), nil
	}

	if _, err := client.Chat(ctx, msgs, w); err != nil {
		return "", fmt.Errorf("route %s: %w", ep.Name, err)
	}
	return buf.String(), nil
}

// clientForEndpoint constructs the appropriate LLMClient for ep.
func clientForEndpoint(ep *RouteEndpoint, cfg *Config) (LLMClient, error) {
	model := ep.Model
	switch ep.Kind {
	case KindOllama:
		if model == "" {
			model = cfg.Ollama.Model
		}
		return newOllamaLLMClient(ollamaBaseURL(ep.URL), model, cfg.Ollama.Timeout), nil
	case KindLlamafile:
		return newLlamafileLLMClient(LlamafileAPIURL(ep.URL), model, cfg.Ollama.Timeout), nil
	case KindLlamaCpp:
		return newLlamaCppLLMClient(LlamacppAPIURL(ep.URL), model, cfg.Ollama.Timeout), nil
	case KindAnthropic:
		return newAnthropicLLMClient(model)
	case KindDeepSeek:
		return newDeepSeekLLMClient(model)
	case KindGemini:
		return newGeminiLLMClient(model)
	case KindMistral:
		return newMistralLLMClient(model)
	case KindOpenAI:
		return newOpenAILLMClient(model)
	default:
		return nil, fmt.Errorf("route %s: unknown endpoint kind %q", ep.Name, ep.Kind)
	}
}

// ollamaBaseURL converts an "ollama://host:port" URL to "http://host:port".
// Raw http:// URLs are returned unchanged, allowing direct registration.
func ollamaBaseURL(u string) string {
	if strings.HasPrefix(u, "ollama://") {
		return "http://" + strings.TrimPrefix(u, "ollama://")
	}
	return u
}

// LlamafileAPIURL converts a "llamafile://host:port" URL to the form
// "http://host:port/v1" expected by the any-llm-go llamafile provider.
// Already well-formed http(s):// URLs are left unchanged.
func LlamafileAPIURL(u string) string {
	base := u
	if strings.HasPrefix(u, "llamafile://") {
		base = "http://" + strings.TrimPrefix(u, "llamafile://")
	}
	if !strings.HasSuffix(base, "/v1") {
		base = strings.TrimRight(base, "/") + "/v1"
	}
	return base
}

// LlamafileHealthURL returns the base URL (without /v1) suitable for health
// probing a llamafile or llama.cpp server.
func LlamafileHealthURL(u string) string {
	base := LlamafileAPIURL(u)
	return strings.TrimSuffix(base, "/v1")
}

// LlamacppAPIURL converts a "llamacpp://host:port" URL to "http://host:port/v1".
func LlamacppAPIURL(u string) string {
	base := u
	if strings.HasPrefix(u, "llamacpp://") {
		base = "http://" + strings.TrimPrefix(u, "llamacpp://")
	}
	if !strings.HasSuffix(base, "/v1") {
		base = strings.TrimRight(base, "/") + "/v1"
	}
	return base
}


/** listModelsForEndpoint returns the available model IDs for the given provider.
 * For Anthropic, the v1/models SDK endpoint is called directly because the
 * any-llm-go Anthropic provider does not implement ModelLister. All other
 * providers use the standard ModelLister interface via a temporary client.
 *
 * Parameters:
 *   ctx    (context.Context) — controls the request lifetime.
 *   kind   (RouteKind)       — provider kind inferred from the URL scheme.
 *   rawURL (string)          — raw endpoint URL; used for local providers.
 *   cfg    (*Config)         — provides timeout and model defaults.
 *
 * Returns:
 *   []string — model IDs; order depends on the provider.
 *   error    — non-nil on API or transport failure.
 *
 * Example:
 *   models, err := listModelsForEndpoint(ctx, KindMistral, "mistral://", cfg)
 */
func listModelsForEndpoint(ctx context.Context, kind RouteKind, rawURL string, cfg *Config) ([]string, error) {
	if kind == KindAnthropic {
		return listAnthropicModels(ctx)
	}
	ep := &RouteEndpoint{Kind: kind, URL: rawURL}
	client, err := clientForEndpoint(ep, cfg)
	if err != nil {
		return nil, err
	}
	return client.Models(ctx)
}

// kindSupportsTools reports whether the given provider kind supports tool
// calling. This is derived from static capability data — no network call is
// made. Used by /route list and /route set to display and validate tool status.
func kindSupportsTools(kind RouteKind) bool {
	switch kind {
	case KindAnthropic, KindDeepSeek, KindGemini, KindMistral, KindOpenAI,
		KindOllama, KindLlamafile, KindLlamaCpp:
		return true
	}
	return false
}

/** resolveTagAlias looks up tag in cfg.ModelAliases and returns the model name
 * of the best alias carrying that tag. When multiple aliases share the tag,
 * the first (alphabetically lowest alias name) is returned as a stable
 * tie-break. Returns ("", false) when no alias carries the tag.
 *
 * Parameters:
 *   cfg (Config) — agent configuration holding ModelAliases.
 *   tag (string) — tag to search for (case-insensitive).
 *
 * Returns:
 *   model (string) — resolved model name; "" when no match.
 *   found (bool)   — true when at least one alias carries the tag.
 *
 * Example:
 *   model, ok := resolveTagAlias(cfg, "code")
 *   // model = "granite3.3:8b", ok = true
 */
func resolveTagAlias(cfg *Config, tag string) (model string, found bool) {
	names := cfg.AliasesByTag(tag)
	if len(names) == 0 {
		return "", false
	}
	// First entry from AliasesByTag (stable alphabetical order) is the winner.
	return cfg.ModelAliases[names[0]].Model, true
}
