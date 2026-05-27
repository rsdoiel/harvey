package harvey

import (
	"encoding/json"
	"strings"
)

/** CodeBlock holds a single fenced code block parsed from a markdown response.
 *
 * Fields:
 *   Lang    (string) — language hint from the opening fence (e.g. "go", "bash"); may be empty.
 *   Content (string) — text inside the fence with the trailing newline stripped.
 *
 * Example:
 *   blocks := extractCodeBlocks("```go\npackage main\n```")
 *   // blocks[0].Lang == "go", blocks[0].Content == "package main"
 */
type CodeBlock struct {
	Lang    string
	Content string
}

// isToolCallBlock reports whether a code block looks like a tool call invocation
// — a JSON object whose top-level keys include "name" and an arguments-like key.
// Different models use different key names:
//   - qwen2.5: {"name": "...", "arguments": {...}}
//   - llama3.2: {"name": "...", "parameters": {...}}
func isToolCallBlock(b CodeBlock) bool {
	lang := strings.ToLower(strings.TrimSpace(b.Lang))
	if lang != "json" && lang != "" {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(b.Content)), &obj); err != nil {
		return false
	}
	if _, hasName := obj["name"]; !hasName {
		return false
	}
	_, hasArgs := obj["arguments"]
	_, hasParams := obj["parameters"]
	return hasArgs || hasParams
}

// extractCodeBlocks returns all triple-backtick fenced code blocks in text,
// in document order. Unclosed fences are discarded.
func extractCodeBlocks(text string) []CodeBlock {
	var blocks []CodeBlock
	lines := strings.Split(text, "\n")
	inFence := false
	var lang string
	var sb strings.Builder

	for _, line := range lines {
		if !inFence {
			if strings.HasPrefix(line, "```") {
				inFence = true
				lang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
				sb.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			blocks = append(blocks, CodeBlock{
				Lang:    lang,
				Content: strings.TrimRight(sb.String(), "\n"),
			})
			inFence = false
			lang = ""
			sb.Reset()
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	// Unclosed fence is discarded.
	return blocks
}
