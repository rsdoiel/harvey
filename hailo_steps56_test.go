package harvey

import (
	"errors"
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

func ragEntry() *RagStoreEntry {
	return &RagStoreEntry{Name: "s", EmbeddingModel: "nomic-embed-text"}
}

// ollama.url is the Hailo server (harvey.local today): there is no Ollama.
func TestEmbedder_RelabelledServerSaysHailoCannotEmbed(t *testing.T) {
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, hl.URL, hl.URL, false, true)
	_, err := a.embedderFor(ragEntry()).Embed("hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"Hailo cannot embed", "ollama.url", hl.URL, "http://localhost:11434"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if got := ExitCodeFor(err); got != ClassUnavailable {
		t.Errorf("exit class = %v, want unavailable", got)
	}
}

// Hailo up, Ollama not: name the URL where Ollama was expected.
func TestEmbedder_OnlyHailoUpNamesTheMissingOllama(t *testing.T) {
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	dead := "http://127.0.0.1:1"
	a := familyAgent(t, dead, hl.URL, true, true)
	_, err := a.embedderFor(ragEntry()).Embed("hello")
	if err == nil || !strings.Contains(err.Error(), "Hailo cannot embed; no Ollama at "+dead) {
		t.Fatalf("error = %v, want %q", err, "Hailo cannot embed; no Ollama at "+dead)
	}
	if ExitCodeFor(err) != ClassUnavailable {
		t.Errorf("exit class = %v, want unavailable", ExitCodeFor(err))
	}
	var _ = errors.Unwrap(err) // the cause is kept
}

// With Ollama up the embedder just works, Hailo or not.
func TestEmbedder_OllamaUpEmbedsWhateverHailoIsDoing(t *testing.T) {
	ol, _ := embedServer(t)
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, ol.URL, hl.URL, true, true)
	vec, err := a.embedderFor(ragEntry()).Embed("hello")
	if err != nil || len(vec) != 3 {
		t.Fatalf("Embed = %v, %v", vec, err)
	}
}

// No card and no hailo.url: a failure is Ollama's own error and Hailo is never
// looked for (no request, no mention).
func TestEmbedder_PlainOllamaFailureIsUnchangedAndProbesNothing(t *testing.T) {
	hl, hits := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, "http://127.0.0.1:1", hl.URL, false, false)
	_, err := a.embedderFor(ragEntry()).Embed("hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "Hailo") {
		t.Errorf("a non-Hailo machine mentions Hailo: %v", err)
	}
	if n := atomic.LoadInt32(hits); n != 0 {
		t.Errorf("made %d request(s) to the Hailo URL", n)
	}
}

func TestEmbedder_EncoderfileIsLeftAlone(t *testing.T) {
	a := familyAgent(t, "http://127.0.0.1:1", "http://127.0.0.1:1", false, false)
	e := a.embedderFor(&RagStoreEntry{Name: "s", EmbeddingModel: "m", EmbedderKind: "encoderfile", EmbedderURL: "http://127.0.0.1:1"})
	if _, ok := e.(*EncoderfileEmbedder); !ok {
		t.Errorf("embedder = %T, want *EncoderfileEmbedder", e)
	}
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
