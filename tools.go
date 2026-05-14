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

/** resolveWorkspacePath resolves p relative to workspaceRoot and enforces
 * three invariants: (1) the result is inside the workspace, (2) no symbolic
 * links are traversed — Harvey operates only on physical paths within the
 * workspace tree, (3) the path does not target agents/ or match sensitive
 * file patterns.
 *
 * Parameters:
 *   workspaceRoot (string) — absolute path of the workspace root.
 *   p             (string) — path from the LLM (relative or absolute).
 *
 * Returns:
 *   string — resolved absolute path, guaranteed to be inside workspaceRoot with no symlinks.
 *   error  — if the path escapes the workspace, contains a symlink, targets agents/, or is a sensitive file.
 *
 * Example:
 *   abs, err := resolveWorkspacePath("/home/user/proj", "src/main.go")
 */
func resolveWorkspacePath(workspaceRoot, p string) (string, error) {
	// Resolve the workspace root to its real physical path for consistent
	// comparison (e.g. macOS /var/folders → /private/var/folders).
	realRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		realRoot = workspaceRoot
	}

	abs := filepath.Join(realRoot, p)
	abs = filepath.Clean(abs)

	// Check workspace bounds on the cleaned path before any symlink resolution
	// so that path traversal via ../ is caught regardless of symlink state.
	rootWithSep := realRoot
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}
	if abs != realRoot && !strings.HasPrefix(abs, rootWithSep) {
		return "", fmt.Errorf("path %q is outside the workspace", p)
	}

	// Reject symbolic links: Harvey must stay within the physical workspace tree.
	// When the path exists, EvalSymlinks must return the same path (no link was
	// followed). When the path doesn't exist yet (write_file), check the parent
	// directory instead — it must exist and must not be a symlink.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		if real != abs {
			return "", fmt.Errorf("path %q contains a symbolic link which is not permitted", p)
		}
	} else {
		// File does not exist yet; check that the parent is not a symlink.
		parent := filepath.Dir(abs)
		if realParent, perr := filepath.EvalSymlinks(parent); perr == nil && realParent != parent {
			return "", fmt.Errorf("path %q contains a symbolic link which is not permitted", p)
		}
	}

	if isAgentsDir(realRoot, abs) {
		return "", fmt.Errorf("path %q targets the agents/ directory which is off-limits to tools", p)
	}

	if sensitiveFileDenied(abs) {
		return "", fmt.Errorf("path %q matches a sensitive file pattern and cannot be accessed by tools", p)
	}

	return abs, nil
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
