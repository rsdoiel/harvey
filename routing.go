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
	KindOllama   RouteKind = "ollama"
	KindLlamafile RouteKind = "llamafile"
	KindLlamaCpp  RouteKind = "llamacpp"
)

/** RouteEndpoint is a named remote LLM endpoint registered for @mention dispatch.
 *
 * Fields:
 *   Name  (string)    — identifier used in @mention syntax (e.g. "pi2").
 *   URL   (string)    — endpoint URL; use "ollama://host:port" or "publicai.co://".
 *   Model (string)    — default model on this endpoint; empty falls back to Config defaults.
 *   Kind  (RouteKind) — KindOllama, KindLlamafile, KindLlamaCpp, etc.
 *
 * Example:
 *   ep := RouteEndpoint{Name: "pi2", URL: "ollama://192.168.1.12:11434", Model: "llama3.1:8b", Kind: KindOllama}
 */
type RouteEndpoint struct {
	Name  string
	URL   string
	Model string
	Kind  RouteKind
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
 * Parameters:
 *   ctx     (context.Context) — controls the HTTP request lifetime.
 *   ep      (*RouteEndpoint)  — registered endpoint to send to.
 *   history ([]Message)       — full local conversation history.
 *   prompt  (string)          — current user prompt (already stripped of @mention).
 *   cfg     (*Config)         — used to resolve model defaults.
 *   out     (io.Writer)       — destination for streamed reply tokens.
 *
 * Returns:
 *   reply (string) — full reply text.
 *   err   (error)  — non-nil on transport or API failure.
 *
 * Example:
 *   reply, err := DispatchToEndpoint(ctx, ep, agent.History, "write a parser", cfg, os.Stdout)
 */
func DispatchToEndpoint(ctx context.Context, ep *RouteEndpoint, history []Message, prompt string, cfg *Config, out io.Writer) (string, error) {
	msgs := recentHistory(history, RecentContextN)
	msgs = append(msgs, Message{Role: "user", Content: prompt})

	client, err := clientForEndpoint(ep, cfg)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	w := io.MultiWriter(&buf, out)
	if _, err := client.Chat(ctx, msgs, w); err != nil {
		return "", fmt.Errorf("route %s: %w", ep.Name, err)
	}
	return buf.String(), nil
}

// clientForEndpoint constructs the appropriate LLMClient for ep.
func clientForEndpoint(ep *RouteEndpoint, cfg *Config) (LLMClient, error) {
	switch ep.Kind {
	case KindOllama:
		model := ep.Model
		if model == "" {
			model = cfg.OllamaModel
		}
		return newOllamaLLMClient(ollamaBaseURL(ep.URL), model), nil
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

// estimateTokens returns a fast token count estimate using the 4-bytes-per-token
// heuristic.
func estimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 {
		n = 1
	}
	return n
}
