package harvey

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	anyllm "github.com/mozilla-ai/any-llm-go"
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

// ParseProseToolCalls converts tool-call code blocks into anyllm.ToolCall values.
// It handles both qwen2.5 ("arguments") and llama3.2 ("parameters") key names.
// Blocks that do not parse as tool calls are silently skipped.
func ParseProseToolCalls(blocks []CodeBlock) []anyllm.ToolCall {
	var calls []anyllm.ToolCall
	for i, b := range blocks {
		if !isToolCallBlock(b) {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(strings.TrimSpace(b.Content)), &obj); err != nil {
			continue
		}
		var name string
		if err := json.Unmarshal(obj["name"], &name); err != nil || name == "" {
			continue
		}
		argsRaw, ok := obj["arguments"]
		if !ok {
			argsRaw, ok = obj["parameters"]
		}
		if !ok {
			continue
		}
		calls = append(calls, anyllm.ToolCall{
			ID:   fmt.Sprintf("prose_%d", i),
			Type: "function",
			Function: anyllm.FunctionCall{
				Name:      name,
				Arguments: string(argsRaw),
			},
		})
	}
	return calls
}

// apertusToolCallRE matches Apertus-native tool call blocks: <SPECIAL_71>...<SPECIAL_72>.
var apertusToolCallRE = regexp.MustCompile(`<SPECIAL_71>([\s\S]*?)<SPECIAL_72>`)

/** ParseApertusToolCalls scans raw response text for Apertus-native tool call
 * blocks and returns them as anyllm.ToolCall values.
 *
 * Apertus emits tool calls as <SPECIAL_71>[{"name": args}, ...]<SPECIAL_72>
 * where each array element is a single-key object: the key is the function name
 * and the value is the arguments (any JSON value).
 *
 * Parameters:
 *   text (string) — raw LLM response text to scan.
 *
 * Returns:
 *   []anyllm.ToolCall — parsed tool calls; nil when none found.
 *
 * Example:
 *   calls := ParseApertusToolCalls(`<SPECIAL_71>[{"read_file": {"path": "x.go"}}]<SPECIAL_72>`)
 *   // calls[0].Function.Name == "read_file"
 */
func ParseApertusToolCalls(text string) []anyllm.ToolCall {
	matches := apertusToolCallRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	var calls []anyllm.ToolCall
	for i, m := range matches {
		if len(m) < 2 {
			continue
		}
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &entries); err != nil {
			continue
		}
		for j, entry := range entries {
			for toolName, argsRaw := range entry {
				calls = append(calls, anyllm.ToolCall{
					ID:   fmt.Sprintf("apertus_%d_%d", i, j),
					Type: "function",
					Function: anyllm.FunctionCall{
						Name:      toolName,
						Arguments: string(argsRaw),
					},
				})
			}
		}
	}
	return calls
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
