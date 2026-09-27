package harvey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ThoroughProbeModel wiring (harvey/decisions/0006-*.md): /rag setup's
// embedder auto-pick used to trust hasEmbedKeyword's name-based guess alone.
// confirmEmbedder thoroughly probes the guess with a live /api/embed call
// before committing to it — the one place a wrong keyword guess has a real
// consequence (DR-0004 left ThoroughProbeModel unwired everywhere else).

// fakeEmbedServer answers /api/show (capabilities: ["embedding"], so
// ThoroughProbeModel's tools probe short-circuits to CapNo and it never
// needs /api/generate) and /api/embed, succeeding only for names in embeds.
func fakeEmbedServer(t *testing.T, embeds ...string) *httptest.Server {
	t.Helper()
	can := map[string]bool{}
	for _, n := range embeds {
		can[n] = true
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/api/show":
			json.NewEncoder(w).Encode(map[string]any{
				"capabilities": []string{"embedding"},
			})
		case "/api/embed":
			if !can[req.Model] {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{{0.1, 0.2, 0.3}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestConfirmEmbedder_ConfirmsThePreferredCandidate(t *testing.T) {
	srv := fakeEmbedServer(t, "nomic-embed-text")
	defer srv.Close()
	var out strings.Builder
	got := confirmEmbedder(context.Background(), srv.URL, "nomic-embed-text",
		[]string{"nomic-embed-text", "mxbai-embed-large"}, &out)
	if got != "nomic-embed-text" {
		t.Errorf("confirmEmbedder = %q, want nomic-embed-text", got)
	}
}

// The keyword guess is confirmed to be wrong (a name that only looks like an
// embedder), so the next candidate that actually embeds is used instead.
func TestConfirmEmbedder_FallsBackWhenThePreferredCandidateFailsLive(t *testing.T) {
	srv := fakeEmbedServer(t, "mxbai-embed-large")
	defer srv.Close()
	var out strings.Builder
	got := confirmEmbedder(context.Background(), srv.URL, "nomic-embed-text",
		[]string{"nomic-embed-text", "mxbai-embed-large"}, &out)
	if got != "mxbai-embed-large" {
		t.Errorf("confirmEmbedder = %q, want mxbai-embed-large (the one that actually embeds)", got)
	}
	if !strings.Contains(out.String(), "mxbai-embed-large") {
		t.Errorf("no explanation printed for the fallback:\n%s", out.String())
	}
}

// Nothing confirms (server down, or no candidate actually embeds): the
// original preferred name is still returned, unconfirmed — a failed probe
// never blocks /rag setup, matching FastProbeModel's own rule (DR-0004).
func TestConfirmEmbedder_UnreachableServerKeepsThePreferredNameUnconfirmed(t *testing.T) {
	var out strings.Builder
	got := confirmEmbedder(context.Background(), "http://127.0.0.1:1", "nomic-embed-text",
		[]string{"nomic-embed-text"}, &out)
	if got != "nomic-embed-text" {
		t.Errorf("confirmEmbedder = %q, want the preferred name unchanged", got)
	}
	if !strings.Contains(out.String(), "unconfirmed") {
		t.Errorf("no warning printed:\n%s", out.String())
	}
}
