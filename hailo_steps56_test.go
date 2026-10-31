package harvey

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Steps 5 and 6 of the Hailo brief (DR-0030): embeddings always use Ollama and
// say so plainly when only Hailo is up; a card with its server stopped is
// reported at start.

// ─── step 5: the embedder ────────────────────────────────────────────────────

// embedServer answers POST /api/embed like Ollama and counts requests.
func embedServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path == "/api/embed" {
			io.WriteString(w, `{"embeddings":[[0.1,0.2,0.3]]}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// ─── step 6: the stopped-service hint ────────────────────────────────────────

func unitFile(t *testing.T, present bool) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hailo-ollama.service")
	if present {
		if err := os.WriteFile(p, []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func deadHailoAgent(t *testing.T, card bool, unit bool) *Agent {
	t.Helper()
	srv, _ := ollamaFamilyServer(t, true)
	url := srv.URL
	srv.Close() // a port nothing answers on
	a := familyAgent(t, "http://127.0.0.1:1", url, false, card)
	a.hailoUnitPath = unitFile(t, unit)
	return a
}

func TestHailoHint_CardWithStoppedServerAndUnit(t *testing.T) {
	a := deadHailoAgent(t, true, true)
	var out strings.Builder
	a.hailoHint(&out)
	got := out.String()
	for _, want := range []string{"AI HAT+ 2 found", "hailo-ollama is not running", "systemctl --user start hailo-ollama"} {
		if !strings.Contains(got, want) {
			t.Errorf("hint %q lacks %q", got, want)
		}
	}
	if n := strings.Count(strings.TrimRight(got, "\n"), "\n"); n != 0 {
		t.Errorf("hint is %d lines, want one: %q", n+1, got)
	}
}

func TestHailoHint_NoUnitNoCommand(t *testing.T) {
	a := deadHailoAgent(t, true, false)
	var out strings.Builder
	a.hailoHint(&out)
	if !strings.Contains(out.String(), "AI HAT+ 2 found") || strings.Contains(out.String(), "systemctl") {
		t.Errorf("hint = %q; want the finding and no command", out.String())
	}
}

func TestHailoHint_SilentWhenNothingToSay(t *testing.T) {
	srv, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	up := familyAgent(t, "http://127.0.0.1:1", srv.URL, true, true)
	up.hailoUnitPath = unitFile(t, true)
	var out strings.Builder
	up.hailoHint(&out)
	if out.Len() != 0 {
		t.Errorf("server up but hint printed: %q", out.String())
	}

	// no card, no hailo.url: no hint, and the Hailo URL is never asked
	srv2, hits := ollamaFamilyServer(t, true, "llama3.2:3b")
	none := familyAgent(t, "http://127.0.0.1:1", srv2.URL, false, false)
	none.hailoUnitPath = unitFile(t, true)
	out.Reset()
	none.hailoHint(&out)
	if out.Len() != 0 || atomic.LoadInt32(hits) != 0 {
		t.Errorf("no card: output %q, %d request(s)", out.String(), atomic.LoadInt32(hits))
	}
}

// A remote hailo.url that is down: starting a local unit would not help.
func TestHailoHint_RemoteServerGetsNoSystemctl(t *testing.T) {
	st := HailoStatus{CardPresent: true, Probed: true, URL: "http://pi5.example:8000"}
	line := hailoHintLine(st, HailoConfig{URL: st.URL, URLSet: true}, true)
	if !strings.Contains(line, "AI HAT+ 2 found") || strings.Contains(line, "systemctl") {
		t.Errorf("line = %q", line)
	}
	local := hailoHintLine(HailoStatus{CardPresent: true, Probed: true, URL: "http://localhost:8000"}, HailoConfig{URL: "http://localhost:8000", URLSet: true}, true)
	if !strings.Contains(local, "systemctl") {
		t.Errorf("local line = %q", local)
	}
}

// Said once, by start-up.
func TestSelectBackend_PrintsTheHailoHintOnce(t *testing.T) {
	a := deadHailoAgent(t, true, true)
	var out strings.Builder
	_ = a.selectBackend(newTestBufioReader("0\n"), &out, "")
	if n := strings.Count(out.String(), "AI HAT+ 2 found"); n != 1 {
		t.Errorf("hint printed %d times:\n%s", n, out.String())
	}
}
