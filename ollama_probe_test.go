package harvey

import "testing"

// These tests exercise the heuristic helpers used by FastProbeModel without
// requiring a running Ollama server.

func TestHasEmbedKeyword(t *testing.T) {
	yes := []string{
		"nomic-embed-text",
		"mxbai-embed-large",
		"all-minilm",
		"bge-m3",
		"bge-large-en-v1.5",
		"gte-base",
		"e5-mistral-7b-instruct",
		"jina-embeddings-v2-base",
		"NOMIC-EMBED-TEXT", // case-insensitive
	}
	for _, name := range yes {
		if !hasEmbedKeyword(name) {
			t.Errorf("hasEmbedKeyword(%q) = false, want true", name)
		}
	}

	no := []string{
		"llama3.2:latest",
		"granite-code:3b",
		"mistral:latest",
		"qwen2.5-coder:7b",
		"smollm2:1.7b",
		"phi4-mini:3.8b",
		"gemma4:latest",
		"aisingapore/Apertus-SEA-LION-v4-8B-IT:latest",
	}
	for _, name := range no {
		if hasEmbedKeyword(name) {
			t.Errorf("hasEmbedKeyword(%q) = true, want false", name)
		}
	}
}

func TestHasToolMarker(t *testing.T) {
	// Llama 3.x / Granite style
	llama3Template := `
{{- if .System }}{{ .System }}{{ end }}
{%- if tools %}
You have access to tools.
{%- for tool in tools %}{{ tool | tojson }}{% endfor %}
{% endif %}
`
	if !hasToolMarker(llama3Template) {
		t.Error("hasToolMarker: missed Llama3 Jinja2 tools block")
	}

	// Mistral style
	mistralTemplate := `[INST] {{ .Prompt }} [/INST]
[TOOL_CALLS] [{"name": "..."}]`
	if !hasToolMarker(mistralTemplate) {
		t.Error("hasToolMarker: missed Mistral [TOOL_CALLS] marker")
	}

	// Qwen 2.5 style
	qwenTemplate := `<|im_start|>system
You are a helpful assistant.<|im_end|>
<tool_call>{"name": "..."}</tool_call>`
	if !hasToolMarker(qwenTemplate) {
		t.Error("hasToolMarker: missed Qwen <tool_call> marker")
	}

	// Plain chat template — no tools
	plainTemplate := `<|im_start|>system
{{ .System }}<|im_end|>
<|im_start|>user
{{ .Prompt }}<|im_end|>
<|im_start|>assistant`
	if hasToolMarker(plainTemplate) {
		t.Error("hasToolMarker: false positive on plain chat template")
	}

	// Empty template (embedding model)
	if hasToolMarker("") {
		t.Error("hasToolMarker: false positive on empty template")
	}
}

func TestCapabilitiesContain(t *testing.T) {
	caps := []string{"completion", "tools", "vision"}

	if !capabilitiesContain(caps, "tools") {
		t.Error("capabilitiesContain: missed 'tools'")
	}
	if !capabilitiesContain(caps, "TOOLS") {
		t.Error("capabilitiesContain: case-insensitive match failed")
	}
	if capabilitiesContain(caps, "embed") {
		t.Error("capabilitiesContain: false positive for 'embed'")
	}
	if capabilitiesContain(nil, "tools") {
		t.Error("capabilitiesContain: false positive on nil slice")
	}
}
