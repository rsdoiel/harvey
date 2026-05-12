package harvey

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// defaultMaxOutputBytes is the default cap on tool output injected into context.
const defaultMaxOutputBytes = 65536

// defaultMaxToolCallsPerTurn is the default loop limit per user turn.
const defaultMaxToolCallsPerTurn = 10

/** ToolHandler executes a named tool and returns its result as a string.
 * It receives the raw argument map decoded from the LLM's JSON arguments.
 *
 * Parameters:
 *   ctx  (context.Context) — controls the request lifetime.
 *   args (map[string]any)  — decoded JSON arguments from the LLM.
 *
 * Returns:
 *   string — tool output to be sent back to the LLM.
 *   error  — non-nil if the tool failed; the error message is sent back as a tool error.
 *
 * Example:
 *   handler := func(ctx context.Context, args map[string]any) (string, error) {
 *       path, ok := args["path"].(string)
 *       if !ok { return "", fmt.Errorf("path must be a string") }
 *       data, err := os.ReadFile(path)
 *       return string(data), err
 *   }
 */
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

// toolEntry pairs an anyllm.Tool schema with its local handler.
type toolEntry struct {
	schema  anyllm.Tool
	handler ToolHandler
}

/** resolveWorkspacePath resolves p relative to workspaceRoot, verifies that
 * the result stays inside the workspace (including symlink evaluation), and
 * rejects paths that fall inside the agents/ subtree or match sensitive file
 * patterns.
 *
 * Parameters:
 *   workspaceRoot (string) — absolute path of the workspace root.
 *   p             (string) — path from the LLM (relative or absolute).
 *
 * Returns:
 *   string — resolved absolute path, guaranteed to be inside workspaceRoot.
 *   error  — if the path escapes the workspace, targets agents/, or is a sensitive file.
 *
 * Example:
 *   abs, err := resolveWorkspacePath("/home/user/proj", "src/main.go")
 */
func resolveWorkspacePath(workspaceRoot, p string) (string, error) {
	// Resolve the workspace root through symlinks so comparisons are consistent
	// (e.g. macOS /var/folders → /private/var/folders).
	realRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		realRoot = workspaceRoot
	}

	abs := filepath.Join(realRoot, p)
	abs = filepath.Clean(abs)

	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File may not exist yet (write_file case); check the cleaned path.
		real = abs
	}

	rootWithSep := realRoot
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}
	if real != realRoot && !strings.HasPrefix(real, rootWithSep) {
		return "", fmt.Errorf("path %q is outside the workspace", p)
	}

	if isAgentsDir(realRoot, real) {
		return "", fmt.Errorf("path %q targets the agents/ directory which is off-limits to tools", p)
	}

	if sensitiveFileDenied(real) {
		return "", fmt.Errorf("path %q matches a sensitive file pattern and cannot be accessed by tools", p)
	}

	return real, nil
}

// isAgentsDir reports whether resolved is inside the workspace's agents/ subtree.
func isAgentsDir(workspaceRoot, resolved string) bool {
	agentsDir := filepath.Join(workspaceRoot, "agents") + string(filepath.Separator)
	return strings.HasPrefix(resolved, agentsDir)
}

// sensitiveDenyPatterns are filename suffixes/names rejected by file-access tools.
var sensitiveDenyPatterns = []string{
	".env", ".pem", ".key", ".p12", ".pfx",
	"authorized_keys",
	"harvey.yaml",
	"id_ed25519",
	"id_rsa",
}

// sensitiveFileDenied reports whether the filename of resolved matches a denylist pattern.
func sensitiveFileDenied(resolved string) bool {
	base := filepath.Base(resolved)
	for _, pat := range sensitiveDenyPatterns {
		if strings.HasPrefix(pat, ".") {
			// Suffix match: e.g. ".env", ".pem"
			if strings.HasSuffix(base, pat) || base == pat {
				return true
			}
		} else {
			// Exact name match: e.g. "harvey.yaml"
			if base == pat {
				return true
			}
		}
	}
	// Also reject names that start with ".env"
	if strings.HasPrefix(base, ".env") {
		return true
	}
	return false
}

/** capOutput truncates s to maxBytes if needed, appending a truncation notice.
 *
 * Parameters:
 *   s        (string) — the raw output string.
 *   maxBytes (int)    — maximum number of bytes; 0 or negative uses defaultMaxOutputBytes.
 *
 * Returns:
 *   string — s, possibly truncated.
 *
 * Example:
 *   out := capOutput(bigString, 65536)
 */
func capOutput(s string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultMaxOutputBytes
	}
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + fmt.Sprintf("\n[output truncated at %d bytes]", maxBytes)
}
