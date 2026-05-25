package harvey

import (
	"fmt"
	"strings"
)

// defaultSensitivePatterns is the list of case-insensitive substrings that
// trigger a privacy warning during memory review. Matches are flagged for
// human review; nothing is automatically redacted.
var defaultSensitivePatterns = []string{
	"password",
	"passwd",
	"token",
	"secret",
	"api_key",
	"apikey",
	"api-key",
	"-----BEGIN",
	"Authorization:",
	"Bearer ",
	"private_key",
	"privatekey",
	"aws_secret",
	"aws_access",
	"client_secret",
	"client_id",
}

/** ScrubResult holds the privacy-scrubbed content and any warning flags.
 *
 * Fields:
 *   Content (string)   — content with workspace paths normalised to
 *                        <workspace>. No other automatic redaction is
 *                        performed; sensitive patterns are only flagged.
 *   Flags   ([]string) — human-readable descriptions of detected patterns,
 *                        e.g. ["contains \"password\"", "contains \"token\""].
 *                        Empty when nothing suspicious was found.
 *
 * Example:
 *   result := Scrub(rawText, "/home/alice/myproject")
 *   if len(result.Flags) > 0 {
 *       fmt.Println("⚠  Sensitive content detected:")
 *       for _, f := range result.Flags { fmt.Println(" -", f) }
 *   }
 */
type ScrubResult struct {
	Content string
	Flags   []string
}

/** Scrub normalises workspace paths in content and checks for sensitive
 * patterns. Path normalisation is silent and automatic; pattern detection
 * produces flags that are surfaced to the human reviewer before a memory
 * is saved.
 *
 * Parameters:
 *   content       (string) — raw text to scrub (YAML front matter or
 *                            Fountain body, or the combined file content).
 *   workspacePath (string) — absolute workspace root path that will be
 *                            replaced with the placeholder <workspace>.
 *                            Pass "" to skip path normalisation.
 *
 * Returns:
 *   ScrubResult — normalised content and any warning flags.
 *
 * Example:
 *   r := Scrub("path: /home/alice/project/src", "/home/alice/project")
 *   // r.Content == "path: <workspace>/src"
 *   // r.Flags  == []
 */
func Scrub(content, workspacePath string) ScrubResult {
	normalized := content
	if workspacePath != "" {
		normalized = strings.ReplaceAll(normalized, workspacePath, "<workspace>")
	}

	lower := strings.ToLower(normalized)
	var flags []string
	seen := make(map[string]bool)
	for _, pattern := range defaultSensitivePatterns {
		key := strings.ToLower(pattern)
		if seen[key] {
			continue
		}
		if strings.Contains(lower, key) {
			flags = append(flags, fmt.Sprintf("contains %q", pattern))
			seen[key] = true
		}
	}

	return ScrubResult{Content: normalized, Flags: flags}
}

/** ScrubDoc applies Scrub to both the YAML front matter text and the
 * FountainBody of a MemoryDoc, returning a new doc with normalised content
 * and combined flags from both sections.
 *
 * Parameters:
 *   doc           (*MemoryDoc) — document to scrub.
 *   workspacePath (string)     — workspace root to normalise away.
 *
 * Returns:
 *   *MemoryDoc  — new document with scrubbed content (original is unchanged).
 *   ScrubResult — flags from the combined scrub of both sections.
 *   error       — if the doc cannot be re-serialised after scrubbing.
 *
 * Example:
 *   scrubbed, result, err := ScrubDoc(proposed, ws.Root())
 *   if len(result.Flags) > 0 {
 *       showWarnings(result.Flags)
 *   }
 */
func ScrubDoc(doc *MemoryDoc, workspacePath string) (*MemoryDoc, ScrubResult, error) {
	raw, err := doc.Bytes()
	if err != nil {
		return nil, ScrubResult{}, fmt.Errorf("scrub doc: serialise: %w", err)
	}

	result := Scrub(string(raw), workspacePath)

	scrubbed, err := ParseMemoryDoc([]byte(result.Content))
	if err != nil {
		return nil, result, fmt.Errorf("scrub doc: re-parse after scrub: %w", err)
	}

	return scrubbed, result, nil
}
