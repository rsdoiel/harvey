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
		{"llamafile://localhost:8080", harvey.KindLlamafile, false},
		{"llamacpp://127.0.0.1:9090", harvey.KindLlamaCpp, false},
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

func TestLlamafileAPIURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"llamafile://localhost:8080", "http://localhost:8080/v1"},
		{"llamafile://192.168.1.5:9090", "http://192.168.1.5:9090/v1"},
		{"http://localhost:8080/v1", "http://localhost:8080/v1"},
		{"http://localhost:8080", "http://localhost:8080/v1"},
	}
	for _, tc := range tests {
		got := harvey.LlamafileAPIURL(tc.input)
		if got != tc.want {
			t.Errorf("LlamafileAPIURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLlamacppAPIURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"llamacpp://localhost:8080", "http://localhost:8080/v1"},
		{"llamacpp://127.0.0.1:9090", "http://127.0.0.1:9090/v1"},
	}
	for _, tc := range tests {
		got := harvey.LlamacppAPIURL(tc.input)
		if got != tc.want {
			t.Errorf("LlamacppAPIURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLlamafileHealthURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"llamafile://localhost:8080", "http://localhost:8080"},
		{"http://localhost:8080/v1", "http://localhost:8080"},
	}
	for _, tc := range tests {
		got := harvey.LlamafileHealthURL(tc.input)
		if got != tc.want {
			t.Errorf("LlamafileHealthURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
