package harvey

import (
	"testing"
	"time"
)

// DR-0049 item 8 asked for a one-time migration of old engine/model rows to the
// default service's key. None is needed: the default services are named after
// their engines, and modelKey already gives them the keys the rows have always
// had. These tests pin that, so a change to modelKey that would orphan old rows
// fails here.

func TestLegacyCacheRows_FoundUnderDefaultServices(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "")
	for _, name := range []string{"llama3.2:3b", "hailo/llama3.2:3b"} {
		if err := mc.Set(&ModelCapability{Name: name, ProbeLevel: "fast", ProbedAt: time.Now(), MaxPromptTokens: 777}); err != nil {
			t.Fatalf("Set %s: %v", name, err)
		}
	}
	for _, c := range []struct{ backend, model, want string }{
		{"ollama", "llama3.2:3b", "llama3.2:3b"},
		{"hailo", "llama3.2:3b", "hailo/llama3.2:3b"},
	} {
		a.Backend = engineStub{engine: c.backend}
		key := a.modelKey(c.model)
		if key != c.want {
			t.Fatalf("%s key = %q, want %q", c.backend, key, c.want)
		}
		got, err := mc.Get(key)
		if err != nil || got == nil || got.MaxPromptTokens != 777 {
			t.Errorf("%s: old row not found under %q (%v, %v)", c.backend, key, got, err)
		}
	}
}

func TestConfiguredServiceKey_CannotCollideWithLegacyRow(t *testing.T) {
	// A configured service's name has no slash and is not "ollama" or "hailo"
	// (parseServices), so service/model never equals an old bare or hailo/ key.
	for _, bad := range []string{"ollama", "hailo", "a/b"} {
		if _, err := parseServices(map[string]serviceYAML{bad: {URL: "http://h:1", Engine: "hailo"}}); err == nil {
			t.Errorf("parseServices accepted service name %q", bad)
		}
	}
}
