package harvey

import "strings"

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
