package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The llamafile, llamacpp and cloud route kinds were removed (DR-0033,
// DR-0042); a persisted endpoint of one fails with a message that says so.
func TestClientForEndpoint_removedKindsSayWhy(t *testing.T) {
	for kind, dr := range map[RouteKind]string{
		"llamafile": "DR-0033", "llamacpp": "DR-0033",
		"anthropic": "DR-0042", "deepseek": "DR-0042", "gemini": "DR-0042", "mistral": "DR-0042", "openai": "DR-0042",
	} {
		_, err := clientForEndpoint(&RouteEndpoint{Name: "x", URL: string(kind) + "://", Kind: kind}, DefaultConfig())
		if err == nil || !strings.Contains(err.Error(), dr) {
			t.Errorf("%s: want an error naming %s, got %v", kind, dr, err)
		}
	}
}

func TestRouteAdd_removedSchemesAreRefused(t *testing.T) {
	for _, url := range []string{"anthropic://", "openai://", "gemini://", "deepseek://", "mistral://"} {
		a := newTestAgent(t)
		a.Routes = NewRouteRegistry()
		err := routeAdd(a, []string{"r", url}, new(strings.Builder))
		if ExitCodeFor(err) != ClassUsage || !strings.Contains(err.Error(), "DR-0042") {
			t.Errorf("%s: want a usage error naming DR-0042, got %v", url, err)
		}
		if a.Routes.Lookup("r") != nil {
			t.Errorf("%s: the route was added", url)
		}
	}
}

// LoadRouteConfig keeps the ollama endpoints and sets the others aside.
func TestLoadRouteConfig_setsRemovedKindsAside(t *testing.T) {
	dir := t.TempDir()
	ws := &Workspace{Root: dir}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"enabled": true, "endpoints": [
		{"Name": "pi2", "URL": "ollama://10.0.0.2:11434", "Kind": "ollama"},
		{"Name": "claude", "URL": "anthropic://", "Kind": "anthropic"}]}`
	if err := os.WriteFile(filepath.Join(dir, "agents", "routes.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	LoadRouteConfig(ws, cfg)
	if len(cfg.Routes) != 1 || cfg.Routes[0].Name != "pi2" {
		t.Errorf("Routes = %+v, want only pi2", cfg.Routes)
	}
	if len(cfg.RemovedRoutes) != 1 || cfg.RemovedRoutes[0].Name != "claude" {
		t.Errorf("RemovedRoutes = %+v, want claude", cfg.RemovedRoutes)
	}
}

// At a terminal a skipped route is a warning; without one it is exit 78.
func TestSkippedRoutes(t *testing.T) {
	a := newTestAgent(t)
	var out strings.Builder
	if err := a.skippedRoutes(&out, false); err != nil || out.Len() != 0 {
		t.Fatalf("no removed routes: err %v, output %q", err, out.String())
	}
	a.Config.RemovedRoutes = []RouteEndpoint{{Name: "claude", URL: "anthropic://", Kind: "anthropic"}}
	if err := a.skippedRoutes(&out, true); err != nil {
		t.Errorf("interactive: want a warning only, got %v", err)
	}
	if !strings.Contains(out.String(), "@claude") || !strings.Contains(out.String(), "DR-0042") {
		t.Errorf("warning does not name the route and the record: %q", out.String())
	}
	err := a.skippedRoutes(new(strings.Builder), false)
	if ExitCodeFor(err) != ClassConfig {
		t.Errorf("non-interactive: class %v (%v), want config", ExitCodeFor(err), err)
	}
}
