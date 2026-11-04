package harvey

import (
	"net/url"
	"sort"
	"strings"
)

/** parseServices checks the services: map of harvey.yaml (DR-0049 items 1 and
 * 10) and returns it as configuration. A name is free-form but not "ollama" or
 * "hailo" (those are the default services), has no slash (it would read as part
 * of a model name), and is unique ignoring case. Each entry needs an http(s)
 * URL and an engine kind of ollama or hailo.
 *
 * Parameters:
 *   in (map[string]serviceYAML) — the section as read; nil when absent.
 *
 * Returns:
 *   map[string]ServiceConfig — keyed by name as written; nil when none.
 *   error                    — class config (78) naming the first bad entry,
 *                              in name order.
 *
 * Example:
 *   svc, err := parseServices(y.Services)
 */
func parseServices(in map[string]serviceYAML) (map[string]ServiceConfig, error) {
	if len(in) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(in))
	for n := range in {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make(map[string]ServiceConfig, len(in))
	seen := map[string]string{}
	for _, n := range names {
		e := in[n]
		key := strings.ToLower(strings.TrimSpace(n))
		switch {
		case key == "":
			return nil, Configf("harvey.yaml: services: a service needs a name")
		case strings.Contains(key, "/"):
			return nil, Configf("harvey.yaml: services: name %q has a slash; a slash would read as part of a model name", n)
		case key == "ollama" || key == "hailo":
			return nil, Configf("harvey.yaml: services: %q is a default service; set its URL with %s.url instead", n, key)
		}
		if other, dup := seen[key]; dup {
			return nil, Configf("harvey.yaml: services: %q and %q are the same name (names ignore case)", other, n)
		}
		seen[key] = n
		kind := strings.ToLower(strings.TrimSpace(e.Engine))
		if kind != "ollama" && kind != "hailo" {
			return nil, Configf("harvey.yaml: services.%s.engine must be %q or %q, got %q", n, "ollama", "hailo", e.Engine)
		}
		raw := strings.TrimSpace(e.URL)
		u, err := url.Parse(raw)
		if raw == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, Configf("harvey.yaml: services.%s.url must be an http or https URL such as http://host:11434, got %q", n, e.URL)
		}
		out[strings.TrimSpace(n)] = ServiceConfig{URL: raw, Engine: kind}
	}
	return out, nil
}
