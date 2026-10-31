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
	// KindOllama reaches an Ollama or hailo-ollama server on this machine or
	// the local network. It is the only kind (workspace DR-0033, DR-0042).
	KindOllama RouteKind = "ollama"
)

/** RouteEndpoint is a named remote LLM endpoint registered for @mention dispatch.
 *
 * Fields:
 *   Name  (string)    — identifier used in @mention syntax (e.g. "pi2").
 *   URL   (string)    — endpoint URL; e.g. "ollama://host:port".
 *   Model (string)    — default model on this endpoint; empty falls back to Config defaults.
 *   Kind  (RouteKind) — KindOllama
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
	return DispatchToEndpointLogged(ctx, ep, history, prompt, cfg, registry, out, nil)
}

/** DispatchToEndpointLogged is DispatchToEndpoint with a session log: the
 * endpoint's client reports its call to log (an *Agent), as part of the turn,
 * so the routed turn is in the record. A nil log records nothing.
 *
 * Parameters:
 *   ctx, ep, history, prompt, cfg, registry, out — as for DispatchToEndpoint.
 *   log (ModelCallLog) — told of the model call; nil is accepted.
 *
 * Returns:
 *   string — the endpoint's reply.
 *   error  — if the client cannot be built or the call fails.
 *
 * Example:
 *   reply, err := DispatchToEndpointLogged(ctx, ep, a.History, prompt, a.Config, a.Tools, io.Discard, a)
 */
func DispatchToEndpointLogged(ctx context.Context, ep *RouteEndpoint, history []Message, prompt string, cfg *Config, registry *ToolRegistry, out io.Writer, log ModelCallLog) (string, error) {
	msgs := recentHistory(history, RecentContextN)
	msgs = append(msgs, Message{Role: "user", Content: prompt})

	client, err := clientForEndpoint(ep, cfg)
	if err != nil {
		return "", err
	}
	if ac, ok := client.(*AnyLLMClient); ok && log != nil {
		ac.CallLog = log
		// The routed prompt is a window of the history plus the line, sent to
		// another server: part of the turn, but not the conversation's own
		// prompt, so the request says which route it went to.
		ctx = withCallPurpose(ctx, purposeRoute, map[string]any{"route": ep.Name})
	}

	var buf strings.Builder
	w := io.MultiWriter(&buf, out)

	if _, _, err := runBoundedTurn(ctx, client, registry, cfg, ep.Tools, msgs, nil, w); err != nil {
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
	case "llamafile", "llamacpp":
		return nil, fmt.Errorf("route %s: the %s route kind was removed (workspace DR-0033); use an ollama:// route", ep.Name, ep.Kind)
	case "anthropic", "deepseek", "gemini", "mistral", "openai":
		return nil, fmt.Errorf("route %s: the %s route kind was removed (workspace DR-0042); use an ollama:// route", ep.Name, ep.Kind)
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

/** listModelsForEndpoint returns the available model IDs for the given provider.
 * It uses the standard ModelLister interface via a temporary client.
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
 *   models, err := listModelsForEndpoint(ctx, KindOllama, "ollama://host:11434", cfg)
 */
func listModelsForEndpoint(ctx context.Context, kind RouteKind, rawURL string, cfg *Config) ([]string, error) {
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
	case KindOllama:
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

/** skippedRoutes reports the persisted routes that were left out because their
 * kind was removed (workspace DR-0033, DR-0042). At a terminal each is a
 * warning and the session goes on without it; without a terminal nobody can
 * fix the file, so the run stops with a configuration error (exit 78).
 *
 * Parameters:
 *   out         (io.Writer) — destination for the warnings.
 *   interactive (bool)      — true when a person is at a terminal.
 *
 * Returns:
 *   error — a *ClassedError of ClassConfig when routes were skipped and nobody is at a terminal; nil otherwise.
 *
 * Example:
 *   if err := a.skippedRoutes(out, interactive); err != nil { return err }
 */
func (a *Agent) skippedRoutes(out io.Writer, interactive bool) error {
	if len(a.Config.RemovedRoutes) == 0 {
		return nil
	}
	var names []string
	for _, ep := range a.Config.RemovedRoutes {
		names = append(names, "@"+ep.Name)
		fmt.Fprintf(out, "  ⚠  route @%s (%s) skipped: the %s route kind was removed (workspace DR-0033, DR-0042). Re-add it with /route add NAME ollama://host:port; it is dropped from routes.json the next time routes are saved.\n", ep.Name, ep.URL, ep.Kind)
	}
	if !interactive {
		return Configf("agents/routes.json has routes of a removed kind (%s); edit the file or re-add them as ollama:// routes", strings.Join(names, ", "))
	}
	return nil
}
