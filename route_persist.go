package harvey

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// routeConfigFile is the JSON structure written to ~/harvey/routes.json.
type routeConfigFile struct {
	Enabled   bool           `json:"enabled"`
	Endpoints []RouteEndpoint `json:"endpoints"`
}

// routeConfigPath returns the path to the persisted route config file.
func routeConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "agents", "routes.json")
}

/** LoadRouteConfig reads ~/harvey/routes.json and populates cfg.Routes and
 * cfg.RoutingEnabled. Silently no-ops when the file does not exist or cannot
 * be parsed, leaving cfg unchanged.
 *
 * Parameters:
 *   cfg (*Config) — config to populate; modified in place.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   LoadRouteConfig(cfg)
 */
func LoadRouteConfig(cfg *Config) {
	data, err := os.ReadFile(routeConfigPath())
	if err != nil {
		return
	}
	var f routeConfigFile
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	cfg.Routes = f.Endpoints
	cfg.RoutingEnabled = f.Enabled
}

/** SaveRouteConfig persists rr to ~/harvey/routes.json, creating the
 * directory if necessary. Endpoints are written in alphabetical name order
 * for stable diffs.
 *
 * Parameters:
 *   rr (*RouteRegistry) — registry to persist; nil is treated as empty+disabled.
 *
 * Returns:
 *   error — on directory creation or file write failure.
 *
 * Example:
 *   if err := SaveRouteConfig(agent.Routes); err != nil {
 *       fmt.Println("warning:", err)
 *   }
 */
func SaveRouteConfig(rr *RouteRegistry) error {
	path := routeConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("save routes: %w", err)
	}

	f := routeConfigFile{}
	if rr != nil {
		f.Enabled = rr.Enabled
		for _, ep := range rr.Endpoints {
			f.Endpoints = append(f.Endpoints, *ep)
		}
		sort.Slice(f.Endpoints, func(i, j int) bool {
			return f.Endpoints[i].Name < f.Endpoints[j].Name
		})
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("save routes: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

/** InferRouteKind returns the RouteKind implied by rawURL.
 *
 * Local providers (no API key):
 *   ollama://host:port, http://, https://  → KindOllama
 *   llamafile://host:port                  → KindLlamafile
 *   llamacpp://host:port                   → KindLlamaCpp
 *
 * Cloud providers (API key from environment):
 *   anthropic://  → KindAnthropic  (ANTHROPIC_API_KEY)
 *   deepseek://   → KindDeepSeek   (DEEPSEEK_API_KEY)
 *   gemini://     → KindGemini     (GEMINI_API_KEY or GOOGLE_API_KEY)
 *   mistral://    → KindMistral    (MISTRAL_API_KEY)
 *   openai://     → KindOpenAI     (OPENAI_API_KEY)
 *
 * Parameters:
 *   rawURL (string) — URL as typed by the user.
 *
 * Returns:
 *   RouteKind — inferred kind.
 *   error     — when the URL scheme is unrecognised.
 *
 * Example:
 *   kind, err := InferRouteKind("ollama://192.168.1.12:11434")
 *   // kind = KindOllama, err = nil
 *   kind, err = InferRouteKind("anthropic://")
 *   // kind = KindAnthropic, err = nil
 */
func InferRouteKind(rawURL string) (RouteKind, error) {
	switch {
	case strings.HasPrefix(rawURL, "ollama://"),
		strings.HasPrefix(rawURL, "http://"),
		strings.HasPrefix(rawURL, "https://"):
		return KindOllama, nil
	case strings.HasPrefix(rawURL, "llamafile://"):
		return KindLlamafile, nil
	case strings.HasPrefix(rawURL, "llamacpp://"):
		return KindLlamaCpp, nil
	case strings.HasPrefix(rawURL, "anthropic://"):
		return KindAnthropic, nil
	case strings.HasPrefix(rawURL, "deepseek://"):
		return KindDeepSeek, nil
	case strings.HasPrefix(rawURL, "gemini://"):
		return KindGemini, nil
	case strings.HasPrefix(rawURL, "mistral://"):
		return KindMistral, nil
	case strings.HasPrefix(rawURL, "openai://"):
		return KindOpenAI, nil
	default:
		return "", fmt.Errorf(
			"unrecognised URL scheme in %q\n"+
				"  Local:  ollama://host:port  llamafile://host:port  llamacpp://host:port\n"+
				"  Cloud:  anthropic://  deepseek://  gemini://  mistral://  openai://",
			rawURL,
		)
	}
}
