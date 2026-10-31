package harvey_test

import (
	"testing"

	"github.com/rsdoiel/harvey"
)

func TestInferRouteKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rawURL   string
		wantKind harvey.RouteKind
		wantErr  bool
	}{
		{"ollama://192.168.1.12:11434", harvey.KindOllama, false},
		{"http://localhost:11434", harvey.KindOllama, false},
		{"https://ollama.example.com", harvey.KindOllama, false},
		{"llamafile://localhost:8080", "", true}, // removed, DR-0033
		{"llamacpp://127.0.0.1:9090", "", true},  // removed, DR-0033
		{"anthropic://", harvey.KindAnthropic, false},
		{"deepseek://", harvey.KindDeepSeek, false},
		{"gemini://", harvey.KindGemini, false},
		{"mistral://", harvey.KindMistral, false},
		{"openai://", harvey.KindOpenAI, false},
		{"unknown://foo", "", true},
		{"", "", true},
	}
	for _, tc := range tests {
		kind, err := harvey.InferRouteKind(tc.rawURL)
		if tc.wantErr {
			if err == nil {
				t.Errorf("InferRouteKind(%q): expected error, got nil", tc.rawURL)
			}
			continue
		}
		if err != nil {
			t.Errorf("InferRouteKind(%q): unexpected error: %v", tc.rawURL, err)
			continue
		}
		if kind != tc.wantKind {
			t.Errorf("InferRouteKind(%q) = %q, want %q", tc.rawURL, kind, tc.wantKind)
		}
	}
}
