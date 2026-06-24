// Package harvey — grounding.go detects when a model's response contains
// quoted text that does not appear in any file content retrieved via tool
// calls during the same turn, which is a strong signal of hallucination.
package harvey

import (
	"regexp"
	"strconv"
	"strings"
)

// minQuotedLen is the minimum byte length a quoted string must have to be
// checked. Shorter strings (e.g. single words) match too easily by coincidence.
const minQuotedLen = 20

// contentToolNames lists tools whose results contain file/resource content
// that the model is expected to attend to. Extend as new tools are added.
var contentToolNames = map[string]bool{
	"read_file": true,
}

/** groundingCheck inspects the model's response against file content returned
 * by read_file (and similar) tool calls during this turn. Returns a non-empty
 * warning string when the response contains quoted text (double-quoted strings
 * longer than minQuotedLen bytes) that does not appear in any retrieved file
 * content — a strong indicator the model hallucinated the file's contents
 * rather than attending to what the tool returned.
 *
 * Returns "" when no content tools were called, when the response has no
 * quoted text, or when at least one quoted string is found in a tool result.
 *
 * Parameters:
 *   response (string)    — the model's final text response for this turn.
 *   history  ([]Message) — conversation history slice covering this turn only
 *                          (i.e. history[histLenBeforeChat:]).
 *
 * Returns:
 *   string — warning text, or "" when no grounding issue detected.
 *
 * Example:
 *   if warn := groundingCheck(buf.String(), a.History[start:]); warn != "" {
 *       fmt.Fprintln(out, yellow("  ⚠ ")+warn)
 *   }
 */
func groundingCheck(response string, history []Message) string {
	// Map ToolCallID → tool name from assistant messages.
	callNames := make(map[string]string)
	for _, m := range history {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			callNames[tc.ID] = tc.Function.Name
		}
	}

	// Collect content returned by content-bearing tool calls (e.g. read_file).
	var fileContents []string
	for _, m := range history {
		if m.Role != "tool" {
			continue
		}
		if contentToolNames[callNames[m.ToolCallID]] {
			fileContents = append(fileContents, m.Content)
		}
	}
	if len(fileContents) == 0 {
		return "" // no content tools called this turn
	}

	// Extract quoted strings from the response.
	quoted := extractQuotedStrings(response, minQuotedLen)
	if len(quoted) == 0 {
		return "" // no quoted text to verify
	}

	// If at least one quoted string appears in any tool result, the response
	// is grounded enough — don't warn.
	for _, q := range quoted {
		for _, fc := range fileContents {
			if strings.Contains(fc, q) {
				return ""
			}
		}
	}

	return "Model quoted text not found in the file(s) read this turn — it may have hallucinated the file contents."
}

/** extractQuotedStrings returns all double-quoted substrings in s that are at
 * least minLen bytes long. Single-line only (no newlines inside quotes).
 *
 * Parameters:
 *   s      (string) — text to scan.
 *   minLen (int)    — minimum byte length of a match.
 *
 * Returns:
 *   []string — matched inner strings (without the surrounding quotes).
 *
 * Example:
 *   strs := extractQuotedStrings(`He said "hello world" and left.`, 5)
 *   // strs == []string{"hello world"}
 */
func extractQuotedStrings(s string, minLen int) []string {
	pattern := `"([^"\n]{` + strconv.Itoa(minLen) + `,})"`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(s, -1)
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		result = append(result, m[1])
	}
	return result
}
