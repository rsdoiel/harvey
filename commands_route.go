// Package harvey — commands_route.go implements the /route slash command
// family for managing remote LLM endpoints.
package harvey

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

/** cmdRoute handles remote endpoint routing configuration for multi-model
 * workflows. Routes allow dispatching prompts to remote LLM endpoints via
 * @mention syntax (e.g., @claude, @mistral) or explicitly via /route.
 *
 * Subcommands:
 *   add NAME URL [MODEL]       — Register a new remote endpoint
 *   rm NAME                    — Remove a registered endpoint
 *   models URL                 — List models available at a provider URL
 *   probe NAME                 — Show capability detail for a registered endpoint
 *   set NAME tools on|off      — Enable or disable tool calling for an endpoint
 *   list                       — List all registered endpoints with capabilities
 *   on                         — Enable routing globally
 *   off                        — Disable routing globally
 *   status                     — Show routing status and endpoints
 *
 * Supported endpoint types:
 *   Local:  ollama://host:port, llamafile://host:port, llamacpp://host:port
 *   Cloud:  anthropic://, deepseek://, gemini://, mistral://, openai://
 *
 * Cloud providers read API keys from environment variables:
 *   ANTHROPIC_API_KEY, DEEPSEEK_API_KEY, GEMINI_API_KEY, MISTRAL_API_KEY, OPENAI_API_KEY
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with route registry.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdRoute(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return routeStatus(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "add":
		return routeAdd(a, args[1:], out)
	case "rm", "remove":
		if len(args) < 2 {
			names := routeNameCandidates(a)
			if len(names) == 0 {
				fmt.Fprintln(out, "  No routes registered. Use /route add NAME URL to add one.")
				return nil
			}
			chosen, err := SelectFromStrings(names, fmt.Sprintf("Remove which route [1-%d] or Enter to cancel: ", len(names)), a.In, out)
			if err != nil || chosen == "" {
				return err
			}
			args = append(args, chosen)
		}
		return routeRemove(a, args[1], out)
	case "models":
		return routeModels(a, args[1:], out)
	case "probe":
		if len(args) < 2 {
			names := routeNameCandidates(a)
			if len(names) == 0 {
				fmt.Fprintln(out, "  No routes registered.")
				return nil
			}
			chosen, err := SelectFromStrings(names, fmt.Sprintf("Probe which route [1-%d] or Enter to cancel: ", len(names)), a.In, out)
			if err != nil || chosen == "" {
				return err
			}
			args = append(args, chosen)
		}
		return routeProbe(a, args[1], out)
	case "set":
		return routeSet(a, args[1:], out)
	case "list":
		return routeList(a, out)
	case "on":
		return routeOn(a, out)
	case "off":
		return routeOff(a, out)
	case "status":
		return routeStatus(a, out)
	case "use":
		if len(args) < 2 {
			// No name given — clear the sticky route.
			if a.ActiveRoute != "" {
				fmt.Fprintf(out, "  Cleared active route (was %q). Prompts go to default model.\n", a.ActiveRoute)
				a.ActiveRoute = ""
			} else {
				fmt.Fprintln(out, "  No active route set. Use /route use NAME to set one.")
			}
			return nil
		}
		name := args[1]
		if a.Routes == nil || a.Routes.Lookup(name) == nil {
			fmt.Fprintf(out, "  Route %q not found. Use /route list to see registered routes.\n", name)
			return nil
		}
		a.ActiveRoute = name
		fmt.Fprintf(out, "  Active route set to %q. All prompts will be dispatched via @%s.\n", name, name)
		fmt.Fprintln(out, dim("  Use /route use (no name) to clear."))
		return nil
	default:
		fmt.Fprintf(out, "  Unknown route subcommand: %q\n", args[0])
		fmt.Fprintln(out, "  Usage: /route <add NAME URL [MODEL] | rm NAME | models URL | probe NAME | set NAME tools on|off | list | use [NAME] | on | off | status>")
	}
	return nil
}

func routeAdd(a *Agent, args []string, out io.Writer) error {
	if len(args) < 2 {
		fmt.Fprintln(out, "  Usage: /route add NAME URL [MODEL]")
		fmt.Fprintln(out, "  Local:  ollama://host:port  llamafile://host:port  llamacpp://host:port")
		fmt.Fprintln(out, "  Cloud:  anthropic://  deepseek://  gemini://  mistral://  openai://")
		fmt.Fprintln(out, "  Cloud providers read API keys from environment variables.")
		return nil
	}
	name, rawURL := args[0], args[1]
	model := ""
	if len(args) >= 3 {
		model = args[2]
	}
	kind, err := InferRouteKind(rawURL)
	if err != nil {
		fmt.Fprintf(out, "  %v\n", err)
		return nil
	}
	ep := &RouteEndpoint{Name: name, URL: rawURL, Model: model, Kind: kind}
	a.Routes.Add(ep)
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintf(out, "  Added: @%s → %s", name, rawURL)
	if model != "" {
		fmt.Fprintf(out, " (%s)", model)
	}
	fmt.Fprintln(out)
	if strings.HasPrefix(rawURL, "http://") {
		host := strings.TrimPrefix(rawURL, "http://")
		if i := strings.IndexAny(host, "/:"); i >= 0 {
			host = host[:i]
		}
		if !isPrivateHost(host) {
			fmt.Fprintln(out, "  Warning: plain HTTP — prompts and responses travel unencrypted.")
			fmt.Fprintln(out, "           Use https:// if the server supports TLS.")
		}
	}
	return nil
}

// isPrivateHost reports whether host is a loopback, link-local, or RFC-1918
// private address. Accepts bare hostnames too — returns false for those since
// we cannot classify them without a DNS lookup, so the warning is still shown.
func isPrivateHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		// Not a numeric IP. Keep the loopback name check for "localhost".
		return host == "localhost"
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func routeRemove(a *Agent, name string, out io.Writer) error {
	if a.Routes.Lookup(name) == nil {
		fmt.Fprintf(out, "  Endpoint %q not found. Use /route list to see registered endpoints.\n", name)
		return nil
	}
	a.Routes.Remove(name)
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintf(out, "  Removed: @%s\n", name)
	return nil
}

func routeList(a *Agent, out io.Writer) error {
	if len(a.Routes.Endpoints) == 0 {
		fmt.Fprintln(out, "  No endpoints registered. Use /route add NAME URL [MODEL].")
		return nil
	}
	names := make([]string, 0, len(a.Routes.Endpoints))
	for n := range a.Routes.Endpoints {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(out)
	for _, n := range names {
		ep := a.Routes.Endpoints[n]
		reach := probeRouteEndpoint(ep, a.Config)
		reachStr := green("✓")
		if !reach {
			reachStr = yellow("✗")
		}
		model := ep.Model
		if model == "" {
			model = "(default)"
		}
		toolsSuffix := ""
		if kindSupportsTools(ep.Kind) {
			if ep.Tools {
				toolsSuffix = "  " + green("tools:on")
			} else {
				toolsSuffix = "  tools:off"
			}
		}
		fmt.Fprintf(out, "  %s  @%-16s  %-10s  %s  [%s]%s\n", reachStr, n, ep.Kind, ep.URL, model, toolsSuffix)
	}
	fmt.Fprintln(out)
	return nil
}

func routeOn(a *Agent, out io.Writer) error {
	a.Routes.Enabled = true
	a.Config.RoutingEnabled = true
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintln(out, "  Routing on. Prefix your prompt with @name to dispatch to a registered endpoint.")
	return nil
}

func routeOff(a *Agent, out io.Writer) error {
	a.Routes.Enabled = false
	a.Config.RoutingEnabled = false
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintln(out, "  Routing off. @mentions will be rejected until you run /route on.")
	return nil
}

func routeStatus(a *Agent, out io.Writer) error {
	enabled := a.Routes != nil && a.Routes.Enabled
	if enabled {
		fmt.Fprintln(out, "  Routing: on")
	} else {
		fmt.Fprintln(out, "  Routing: off")
	}
	count := 0
	if a.Routes != nil {
		count = len(a.Routes.Endpoints)
	}
	if count == 0 {
		fmt.Fprintln(out, "  Endpoints: none registered (use /route add NAME URL [MODEL])")
	} else {
		fmt.Fprintf(out, "  Endpoints: %d registered (use /route list for details)\n", count)
	}
	return nil
}

// formatCapabilities returns a compact one-line capability summary.
func formatCapabilities(caps anyllm.Capabilities) string {
	f := func(b bool) string {
		if b {
			return "✓"
		}
		return "—"
	}
	return fmt.Sprintf("chat %s  stream %s  tools %s  vision %s  pdf %s  reasoning %s  embed %s",
		f(caps.Completion), f(caps.CompletionStreaming), f(caps.CompletionTools),
		f(caps.CompletionImage), f(caps.CompletionPDF), f(caps.CompletionReasoning),
		f(caps.Embedding))
}

/** routeModels lists available models for a provider URL before a route is
 * registered. Takes a raw URL (e.g. "anthropic://") to infer the provider kind.
 * For Anthropic the v1/models API is called directly; all other providers use
 * the any-llm-go ModelLister interface.
 *
 * Usage: /route models URL
 */
func routeModels(a *Agent, args []string, out io.Writer) error {
	if len(args) < 1 {
		fmt.Fprintln(out, "  Usage: /route models URL")
		fmt.Fprintln(out, "  Example: /route models anthropic://")
		fmt.Fprintln(out, "           /route models ollama://192.168.1.12:11434")
		return nil
	}
	rawURL := args[0]
	kind, err := InferRouteKind(rawURL)
	if err != nil {
		fmt.Fprintf(out, "  %v\n", err)
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Provider: %s\n", kind)

	// Show static capabilities (no network call needed).
	ep := &RouteEndpoint{Kind: kind, URL: rawURL}
	if client, cerr := clientForEndpoint(ep, a.Config); cerr == nil {
		if ac, ok := client.(*AnyLLMClient); ok {
			fmt.Fprintf(out, "  Capabilities: %s\n", formatCapabilities(ac.ProviderCapabilities()))
		}
	}

	fmt.Fprintf(out, "\n  Fetching models from %s ...\n", rawURL)
	ctx := context.Background()
	models, err := listModelsForEndpoint(ctx, kind, rawURL, a.Config)
	if err != nil {
		fmt.Fprintf(out, "  Error: %v\n", err)
		return nil
	}
	if len(models) == 0 {
		fmt.Fprintln(out, "  No models returned.")
		return nil
	}
	sort.Strings(models)
	fmt.Fprintf(out, "  Models (%d):\n", len(models))
	for _, m := range models {
		fmt.Fprintf(out, "    %s\n", m)
	}
	fmt.Fprintln(out)
	return nil
}

/** routeProbe shows detailed capability and status information for a registered
 * endpoint. Unlike /route models, this operates on a named route rather than
 * a raw URL.
 *
 * Usage: /route probe NAME
 */
func routeProbe(a *Agent, name string, out io.Writer) error {
	ep := a.Routes.Lookup(name)
	if ep == nil {
		fmt.Fprintf(out, "  @%s not found. Use /route list to see registered endpoints.\n", name)
		return nil
	}

	reach := probeRouteEndpoint(ep, a.Config)
	reachStr := green("✓ reachable")
	if !reach {
		reachStr = yellow("✗ unreachable")
	}
	model := ep.Model
	if model == "" {
		model = "(default)"
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Route:    @%s\n", name)
	fmt.Fprintf(out, "  Provider: %s\n", ep.Kind)
	fmt.Fprintf(out, "  URL:      %s\n", ep.URL)
	fmt.Fprintf(out, "  Status:   %s\n", reachStr)
	fmt.Fprintf(out, "  Model:    %s\n", model)

	toolsLine := "off"
	if !kindSupportsTools(ep.Kind) {
		toolsLine = "not supported by provider"
	} else if ep.Tools {
		toolsLine = green("on")
	}
	fmt.Fprintf(out, "  Tools:    %s\n", toolsLine)

	if reach {
		if client, err := clientForEndpoint(ep, a.Config); err == nil {
			if ac, ok := client.(*AnyLLMClient); ok {
				fmt.Fprintf(out, "  Caps:     %s\n", formatCapabilities(ac.ProviderCapabilities()))
			}
		}
	}
	fmt.Fprintf(out, "\n  Use /route models %s to list available models.\n\n", ep.URL)
	return nil
}

/** routeSet updates a per-endpoint setting for a registered route and persists
 * the change. Currently supports one setting:
 *
 *   tools on|off — enable or disable tool calling via ToolExecutor.
 *
 * Usage: /route set NAME tools on|off
 */
func routeSet(a *Agent, args []string, out io.Writer) error {
	if len(args) < 3 {
		fmt.Fprintln(out, "  Usage: /route set NAME tools on|off")
		return nil
	}
	name, key, val := args[0], strings.ToLower(args[1]), strings.ToLower(args[2])
	ep := a.Routes.Lookup(name)
	if ep == nil {
		fmt.Fprintf(out, "  @%s not found. Use /route list to see registered endpoints.\n", name)
		return nil
	}
	switch key {
	case "tools":
		if !kindSupportsTools(ep.Kind) {
			fmt.Fprintf(out, "  @%s provider (%s) does not support tool calling.\n", name, ep.Kind)
			return nil
		}
		switch val {
		case "on", "true", "yes":
			ep.Tools = true
			fmt.Fprintf(out, "  @%s: tools enabled. Dispatches will use Harvey's tool registry.\n", name)
		case "off", "false", "no":
			ep.Tools = false
			fmt.Fprintf(out, "  @%s: tools disabled.\n", name)
		default:
			fmt.Fprintf(out, "  Unknown value %q — use: on | off\n", val)
			return nil
		}
	default:
		fmt.Fprintf(out, "  Unknown setting %q — available settings: tools\n", key)
		return nil
	}
	if err := SaveRouteConfig(a.Workspace, a.Routes); err != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", err)
	}
	return nil
}

// probeRouteEndpoint returns true when ep appears reachable.
// Local providers are probed via HTTP; cloud providers check for a non-empty API key env var.
func probeRouteEndpoint(ep *RouteEndpoint, cfg *Config) bool {
	switch ep.Kind {
	case KindOllama:
		return ProbeOllama(ollamaBaseURL(ep.URL))
	case KindLlamafile:
		return ProbeEncoderfile(LlamafileHealthURL(ep.URL))
	case KindLlamaCpp:
		return ProbeEncoderfile(LlamafileHealthURL(LlamacppAPIURL(ep.URL)))
	case KindAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY") != ""
	case KindDeepSeek:
		return os.Getenv("DEEPSEEK_API_KEY") != ""
	case KindGemini:
		return os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != ""
	case KindMistral:
		return os.Getenv("MISTRAL_API_KEY") != ""
	case KindOpenAI:
		return os.Getenv("OPENAI_API_KEY") != ""
	}
	return false
}
