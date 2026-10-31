// Package harvey — route_persist.go handles persistence of remote endpoint
// routing configuration. The route registry is stored in agents/routes.json
// inside the workspace and contains all registered remote endpoints and the
// routing enabled/disabled state. This file provides:
//
//   - LoadRouteConfig: Load routes from disk into Config
//   - SaveRouteConfig: Write routes from RouteRegistry to disk
//   - InferRouteKind: Determine RouteKind from URL scheme
//
// Route endpoints are persisted in alphabetical order for stable diffs.
// The configuration file is created automatically when the first endpoint
// is registered via /route add.

package harvey

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// routeConfigFile is the JSON structure written to agents/routes.json.
type routeConfigFile struct {
	Enabled   bool            `json:"enabled"`
	Endpoints []RouteEndpoint `json:"endpoints"`
}

/** LoadRouteConfig reads agents/routes.json from the workspace and populates
 * cfg.Routes and cfg.RoutingEnabled. An endpoint whose kind was removed
 * (llamafile, llamacpp, the cloud kinds) is left out of cfg.Routes and listed in
 * cfg.RemovedRoutes. Silently no-ops when the file does not
 * exist or cannot be parsed, leaving cfg unchanged.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose agents/ directory is searched.
 *   cfg (*Config)    — config to populate; modified in place.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   LoadRouteConfig(ws, cfg)
 */
func LoadRouteConfig(ws *Workspace, cfg *Config) {
	path, err := ws.AbsPath(filepath.Join(harveySubdir, "routes.json"))
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var f routeConfigFile
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	cfg.Routes = nil
	for _, ep := range f.Endpoints {
		if ep.Kind == KindOllama {
			cfg.Routes = append(cfg.Routes, ep)
		} else {
			cfg.RemovedRoutes = append(cfg.RemovedRoutes, ep)
		}
	}
	cfg.RoutingEnabled = f.Enabled
}

/** SaveRouteConfig persists rr to agents/routes.json in the workspace,
 * creating the directory if necessary. Endpoints are written in alphabetical
 * name order for stable diffs.
 *
 * Parameters:
 *   ws (*Workspace)     — workspace whose agents/ directory is written.
 *   rr (*RouteRegistry) — registry to persist; nil is treated as empty+disabled.
 *
 * Returns:
 *   error — on path resolution, directory creation, or file write failure.
 *
 * Example:
 *   if err := SaveRouteConfig(ws, agent.Routes); err != nil {
 *       fmt.Println("warning:", err)
 *   }
 */
func SaveRouteConfig(ws *Workspace, rr *RouteRegistry) error {
	path, err := ws.AbsPath(filepath.Join(harveySubdir, "routes.json"))
	if err != nil {
		return fmt.Errorf("save routes: %w", err)
	}
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
 *   ollama://host:port, http://, https://  → KindOllama
 *
 * The llamafile://, llamacpp:// and cloud schemes (anthropic://, deepseek://,
 * gemini://, mistral://, openai://) were removed (workspace DR-0033, DR-0042)
 * and are refused with a message that says so.
 *
 * Parameters:
 *   rawURL (string) — URL as typed by the user.
 *
 * Returns:
 *   RouteKind — inferred kind.
 *   error     — when the URL scheme is unrecognised or was removed.
 *
 * Example:
 *   kind, err := InferRouteKind("ollama://192.168.1.12:11434")
 *   // kind = KindOllama, err = nil
 */
func InferRouteKind(rawURL string) (RouteKind, error) {
	switch {
	case strings.HasPrefix(rawURL, "ollama://"),
		strings.HasPrefix(rawURL, "http://"),
		strings.HasPrefix(rawURL, "https://"):
		return KindOllama, nil
	case strings.HasPrefix(rawURL, "llamafile://"), strings.HasPrefix(rawURL, "llamacpp://"):
		return "", fmt.Errorf("the llamafile:// and llamacpp:// route kinds were removed (workspace DR-0033); use ollama://host:port")
	case removedCloudScheme(rawURL) != "":
		return "", fmt.Errorf("the %s:// route kind was removed (workspace DR-0042): harvey reaches only Ollama-shaped servers on this machine or the local network; use ollama://host:port", removedCloudScheme(rawURL))
	default:
		return "", fmt.Errorf("unrecognised URL scheme in %q\n  Use: ollama://host:port (or http:// or https://)", rawURL)
	}
}

// removedCloudScheme returns the scheme of a cloud route URL that was removed
// (workspace DR-0042), or "" when rawURL is not one.
func removedCloudScheme(rawURL string) string {
	for _, s := range []string{"anthropic", "deepseek", "gemini", "mistral", "openai"} {
		if strings.HasPrefix(rawURL, s+"://") {
			return s
		}
	}
	return ""
}
