package harvey

import (
	"bytes"
	"strings"
	"testing"
)

func resetNotices(t *testing.T) {
	t.Helper()
	resetDeprecationNotices()
	t.Cleanup(resetDeprecationNotices)
}

func TestDeprecationNotice_oncePerKey(t *testing.T) {
	resetNotices(t)
	var buf bytes.Buffer
	deprecationNotice(&buf, "k", "first")
	deprecationNotice(&buf, "k", "second")
	deprecationNotice(&buf, "other", "third")
	got := buf.String()
	if strings.Count(got, "first") != 1 || strings.Contains(got, "second") || !strings.Contains(got, "third") {
		t.Fatalf("notice output = %q", got)
	}
}

func TestRouteAdd_cloudKindNotice(t *testing.T) {
	resetNotices(t)
	for _, url := range []string{"anthropic://", "openai://", "llamafile://h:1", "llamacpp://h:1"} {
		a := newTestAgent(t)
		a.Routes = NewRouteRegistry()
		var buf bytes.Buffer
		if err := routeAdd(a, []string{"r", url}, &buf); err != nil {
			t.Fatalf("routeAdd %s: %v", url, err)
		}
		if !strings.Contains(buf.String(), "deprecated") {
			t.Errorf("routeAdd %s: no notice in %q", url, buf.String())
		}
		resetDeprecationNotices()
	}
}

func TestRouteAdd_ollamaNoNotice(t *testing.T) {
	resetNotices(t)
	a := newTestAgent(t)
	a.Routes = NewRouteRegistry()
	var buf bytes.Buffer
	if err := routeAdd(a, []string{"r", "ollama://10.0.0.2:11434"}, &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("unexpected notice: %q", buf.String())
	}
}

func TestClientForEndpoint_noticeOnDispatch(t *testing.T) {
	resetNotices(t)
	var buf bytes.Buffer
	old := deprecationWriter
	deprecationWriter = &buf
	t.Cleanup(func() { deprecationWriter = old })
	cfg := DefaultConfig()
	_, _ = clientForEndpoint(&RouteEndpoint{Name: "x", URL: "llamafile://h:1", Kind: KindLlamafile}, cfg)
	if !strings.Contains(buf.String(), "deprecated") {
		t.Fatalf("no notice on dispatch: %q", buf.String())
	}
}

func TestStartupDeprecations(t *testing.T) {
	resetNotices(t)
	a := newTestAgent(t)
	var buf bytes.Buffer
	a.startupDeprecations(&buf)
	if buf.Len() != 0 {
		t.Fatalf("default config printed %q", buf.String())
	}

	a.Config.Llamafile.Models = []LlamafileEntry{{Name: "m", Path: "/x"}}
	a.Routes = NewRouteRegistry()
	a.Routes.Add(&RouteEndpoint{Name: "g", URL: "gemini://", Kind: KindGemini})
	a.startupDeprecations(&buf)
	got := buf.String()
	for _, want := range []string{"llamafile", "gemini"} {
		if !strings.Contains(got, want) {
			t.Errorf("startup notice missing %q in %q", want, got)
		}
	}
}

func TestNewRemoteReader_s3Notice(t *testing.T) {
	resetNotices(t)
	var buf bytes.Buffer
	old := deprecationWriter
	deprecationWriter = &buf
	t.Cleanup(func() { deprecationWriter = old })
	_, _ = NewRemoteReader("s3://bucket/key")
	if !strings.Contains(buf.String(), "s3") || !strings.Contains(buf.String(), "deprecated") {
		t.Fatalf("no s3 notice: %q", buf.String())
	}
}
