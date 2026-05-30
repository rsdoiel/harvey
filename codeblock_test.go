package harvey

import "testing"

func TestExtractCodeBlocks_empty(t *testing.T) {
	if got := extractCodeBlocks(""); len(got) != 0 {
		t.Fatalf("expected no blocks, got %d", len(got))
	}
}

func TestExtractCodeBlocks_nofence(t *testing.T) {
	if got := extractCodeBlocks("just some text\nno fences here"); len(got) != 0 {
		t.Fatalf("expected no blocks, got %d", len(got))
	}
}

func TestExtractCodeBlocks_single_noLang(t *testing.T) {
	text := "before\n```\nhello world\n```\nafter"
	blocks := extractCodeBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Lang != "" {
		t.Errorf("lang: got %q, want empty", blocks[0].Lang)
	}
	if blocks[0].Content != "hello world" {
		t.Errorf("content: got %q, want %q", blocks[0].Content, "hello world")
	}
}

func TestExtractCodeBlocks_single_withLang(t *testing.T) {
	text := "```bash\n#!/bin/bash\necho hi\n```"
	blocks := extractCodeBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Lang != "bash" {
		t.Errorf("lang: got %q, want %q", blocks[0].Lang, "bash")
	}
	want := "#!/bin/bash\necho hi"
	if blocks[0].Content != want {
		t.Errorf("content: got %q, want %q", blocks[0].Content, want)
	}
}

func TestExtractCodeBlocks_multiline(t *testing.T) {
	text := "```sh\nline1\nline2\nline3\n```"
	blocks := extractCodeBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	want := "line1\nline2\nline3"
	if blocks[0].Content != want {
		t.Errorf("content: got %q, want %q", blocks[0].Content, want)
	}
}

func TestExtractCodeBlocks_multiple(t *testing.T) {
	text := "first:\n```go\npackage main\n```\nsecond:\n```python\nprint('hi')\n```"
	blocks := extractCodeBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Lang != "go" {
		t.Errorf("block[0].Lang: got %q, want %q", blocks[0].Lang, "go")
	}
	if blocks[1].Lang != "python" {
		t.Errorf("block[1].Lang: got %q, want %q", blocks[1].Lang, "python")
	}
}

func TestExtractCodeBlocks_unclosedFence(t *testing.T) {
	text := "```go\npackage main\n// no closing fence"
	blocks := extractCodeBlocks(text)
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks (unclosed fence discarded), got %d", len(blocks))
	}
}

func TestExtractCodeBlocks_surroundingProse(t *testing.T) {
	text := "Here is a script:\n\n```bash\necho hello\n```\n\nRun it with bash."
	blocks := extractCodeBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Content != "echo hello" {
		t.Errorf("content: got %q, want %q", blocks[0].Content, "echo hello")
	}
}

func TestExtractCodeBlocks_langWithSpace(t *testing.T) {
	// Some models emit "``` go" (space before lang) — trim it.
	text := "``` go\nfunc main() {}\n```"
	blocks := extractCodeBlocks(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Lang != "go" {
		t.Errorf("lang: got %q, want %q", blocks[0].Lang, "go")
	}
}

func TestParseProseToolCalls_qwen(t *testing.T) {
	// qwen2.5-style: "arguments" key
	text := "```json\n{\"name\": \"write_file\", \"arguments\": {\"path\": \"foo.ts\", \"content\": \"export function foo(): void {}\"}}\n```"
	blocks := extractCodeBlocks(text)
	calls := ParseProseToolCalls(blocks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "write_file" {
		t.Errorf("name: got %q, want write_file", calls[0].Function.Name)
	}
	if calls[0].ID == "" {
		t.Error("expected non-empty ID")
	}
	if calls[0].Function.Arguments == "" {
		t.Error("expected non-empty arguments JSON")
	}
}

func TestParseProseToolCalls_llama(t *testing.T) {
	// llama3.2-style: "parameters" key
	text := "```json\n{\"name\": \"read_file\", \"parameters\": {\"path\": \"main.go\"}}\n```"
	blocks := extractCodeBlocks(text)
	calls := ParseProseToolCalls(blocks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("name: got %q, want read_file", calls[0].Function.Name)
	}
}

func TestParseProseToolCalls_nonToolBlock(t *testing.T) {
	// A regular Go code block should not be parsed as a tool call.
	text := "```go\npackage main\nfunc main() {}\n```"
	blocks := extractCodeBlocks(text)
	calls := ParseProseToolCalls(blocks)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls from Go block, got %d", len(calls))
	}
}

func TestParseProseToolCalls_missingArgsKey(t *testing.T) {
	// JSON with "name" but no arguments/parameters — should be skipped.
	text := "```json\n{\"name\": \"do_something\", \"other\": \"value\"}\n```"
	blocks := extractCodeBlocks(text)
	calls := ParseProseToolCalls(blocks)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
}
