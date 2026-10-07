package harvey

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// hailoShow is /api/show as hailo-ollama answers it (captured on harvey.local,
// 2026-10-07): no capabilities array, "model_info" is an empty string, the
// template carries tool markers, and details.format is "hef". hailo-ollama
// then answers any /api/chat that has a "tools" key with HTTP 500.
const hailoShow = `{"license":"","modelfile":"","parameters":"stop \"<|im_end|>\"","template":"{%- if tools %}<tools></tools><tool_call>{%- endif %}","details":{"parent_model":"","format":"hef","family":"qwen2.5","families":["qwen2.5"],"parameter_size":"1.5B","quantization_level":"Q4_0"},"model_info":"","modified_at":"2026-10-07T02:53:48Z"}`

func hailoServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, hailoShow)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFastProbeModel_HailoShowDecodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cap, err := FastProbeModel(ctx, hailoServer(t), "qwen2.5-coder:1.5b")
	if err != nil {
		t.Fatalf("FastProbeModel failed on hailo-ollama's /api/show: %v", err)
	}
	if cap.Family != "qwen2.5" {
		t.Errorf("Family = %q", cap.Family)
	}
}

// hailo-ollama cannot take a tools array whatever the template says, so the
// probe must not report tool support.
func TestFastProbeModel_HailoModelsDoNotSupportTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cap, err := FastProbeModel(ctx, hailoServer(t), "qwen2.5-coder:1.5b")
	if err != nil {
		t.Fatal(err)
	}
	if cap.SupportsTools != CapNo {
		t.Errorf("SupportsTools = %v, want CapNo for a hef model", cap.SupportsTools)
	}
}
