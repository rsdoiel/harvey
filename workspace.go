package harvey

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

/** Workspace anchors Harvey to a single root directory and enforces that all
 * file operations remain within it. The root is resolved to an absolute,
 * symlink-free path at construction time so that later checks cannot be
 * bypassed by symlinks or relative ".." components.
 *
 * Harvey's internal state directory is always at <Root>/harvey/.
 *
 * Example:
 *   ws, err := NewWorkspace(".")
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   data, err := ws.ReadFile("README.md")
 */
type Workspace struct {
	// Root is the absolute, canonicalised path of the working directory.
	Root string
}

/** NewWorkspace creates a Workspace rooted at dir, resolving the path to an
 * absolute, symlink-free form. The agents/ sub-directory is created if it
 * does not exist.
 *
 * Parameters:
 *   dir (string) — path to use as the workspace root; "." uses the current
 *                  working directory.
 *
 * Returns:
 *   *Workspace — the initialised workspace.
 *   error      — if dir cannot be resolved or agents/ cannot be created.
 *
 * Example:
 *   ws, err := NewWorkspace(".")
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 */
func NewWorkspace(dir string) (*Workspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve %q: %w", dir, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Directory might not exist yet.
		real = abs
	}
	ws := &Workspace{Root: real}
	if err := ws.MkdirAll(harveySubdir); err != nil {
		return nil, fmt.Errorf("workspace: create harvey dir: %w", err)
	}
	return ws, nil
}

// harveySubdir is the name of Harvey's internal state directory inside Root.
const harveySubdir = "agents"

/** HarveyDir returns the absolute path of Harvey's internal state directory
 * (agents/) inside the workspace root.
 *
 * Returns:
 *   string — absolute path to <Root>/agents/
 *
 * Example:
 *   fmt.Println(ws.HarveyDir()) // "/home/user/myproject/agents"
 */
func (ws *Workspace) HarveyDir() string {
	return filepath.Join(ws.Root, harveySubdir)
}

/** AbsPath resolves rel relative to the workspace root and verifies that the
 * resulting path lies inside the root. It returns an error for any path that
 * would escape the workspace (e.g. via ".." components or absolute paths
 * outside Root).
 *
 * Security: Uses filepath.Clean to resolve ".." and "." components, then verifies
 * the cleaned path is a subtree of ws.Root. This prevents path traversal attacks.
 *
 * Parameters:
 *   rel (string) — relative path to resolve; may use "/" as separator.
 *
 * Returns:
 *   string — absolute, cleaned path inside the workspace.
 *   error  — if the resolved path escapes the workspace root.
 *
 * Example:
 *   p, err := ws.AbsPath("src/main.go")
 *   // p == "/home/user/myproject/src/main.go"
 *   _, err = ws.AbsPath("../../etc/passwd") // returns error
 */
func (ws *Workspace) AbsPath(rel string) (string, error) {
	// Clean the path to resolve ".." and "." components, then join with root
	candidate := filepath.Clean(filepath.Join(ws.Root, rel))
	
	// Ensure the candidate is absolute and starts with the workspace root
	// Use filepath.Dir to handle the case where candidate equals ws.Root exactly
	if !filepath.IsAbs(candidate) {
		return "", fmt.Errorf("workspace: path %q resolves to non-absolute path", rel)
	}
	
	// Normalize both paths for comparison (handles trailing slashes)
	rootNorm := ws.Root
	if !strings.HasSuffix(rootNorm, string(filepath.Separator)) {
		rootNorm = rootNorm + string(filepath.Separator)
	}
	
	candidateNorm := candidate
	if !strings.HasSuffix(candidateNorm, string(filepath.Separator)) {
		candidateNorm = candidateNorm + string(filepath.Separator)
	}
	
	if !strings.HasPrefix(candidateNorm, rootNorm) {
		return "", fmt.Errorf("workspace: path %q escapes workspace root", rel)
	}
	
	return candidate, nil
}

/** ReadFile reads the file at path (relative to the workspace root) and
 * returns its contents. The path must not escape the workspace.
 *
 * Parameters:
 *   path (string) — relative path to the file.
 *
 * Returns:
 *   []byte — file contents.
 *   error  — if the path escapes the workspace or the file cannot be read.
 *
 * Example:
 *   data, err := ws.ReadFile("HARVEY.md")
 */
func (ws *Workspace) ReadFile(path string) ([]byte, error) {
	abs, err := ws.AbsPath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

/** WriteFile writes data to path (relative to the workspace root), creating
 * parent directories as needed. The path must not escape the workspace.
 *
 * Parameters:
 *   path (string)      — relative path to write; parent dirs are created.
 *   data ([]byte)      — bytes to write.
 *   perm (fs.FileMode) — file permission bits (e.g. 0o644).
 *
 * Returns:
 *   error — if the path escapes the workspace or the write fails.
 *
 * Example:
 *   err := ws.WriteFile("notes/todo.md", []byte("# TODO\n"), 0o644)
 */
func (ws *Workspace) WriteFile(path string, data []byte, perm fs.FileMode) error {
	abs, err := ws.AbsPath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, data, perm)
}

/** MkdirAll creates path (relative to the workspace root) and any missing
 * parent directories. The path must not escape the workspace.
 *
 * Parameters:
 *   path (string) — relative directory path to create.
 *
 * Returns:
 *   error — if the path escapes the workspace or directory creation fails.
 *
 * Example:
 *   err := ws.MkdirAll("docs/api")
 */
func (ws *Workspace) MkdirAll(path string) error {
	abs, err := ws.AbsPath(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0o755)
}

/** ListDir returns the directory entries at path (relative to the workspace
 * root). The path must not escape the workspace.
 *
 * Parameters:
 *   path (string) — relative path of the directory to list; use "." for root.
 *
 * Returns:
 *   []fs.DirEntry — sorted directory entries.
 *   error         — if the path escapes the workspace or the read fails.
 *
 * Example:
 *   entries, err := ws.ListDir(".")
 *   for _, e := range entries {
 *       fmt.Println(e.Name())
 *   }
 */
func (ws *Workspace) ListDir(path string) ([]fs.DirEntry, error) {
	abs, err := ws.AbsPath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(abs)
}

/** LoadHarveyMD reads HARVEY.md from the workspace root and returns the
 * agent preamble followed by the file contents. The preamble is always
 * included so the LLM knows it must use slash commands for real side-effects
 * rather than narrating fake output. Returns only the preamble when
 * HARVEY.md does not exist.
 *
 * Returns:
 *   string — agentPreamble + HARVEY.md contents (or agentPreamble alone).
 *
 * Example:
 *   cfg.SystemPrompt = ws.LoadHarveyMD()
 */
func (ws *Workspace) LoadHarveyMD() string {
	data, err := ws.ReadFile("HARVEY.md")
	if err != nil {
		return agentPreamble
	}
	return agentPreamble + string(data)
}

/** RequireCWDInRoot verifies that cwd lies inside root (or equals it), after
 * resolving both to absolute, symlink-free paths. This is the enforcement
 * half of Harvey's workspace security model: launching with no -w/--workdir
 * flag always trusts cwd as the workspace, but an explicitly-passed root
 * must not be allowed to point somewhere the process isn't actually standing
 * inside — otherwise agents/, sessions, and HARVEY.md would all be anchored
 * to a tree unrelated to where the user is actually working.
 *
 * Parameters:
 *   cwd  (string) — the process's current working directory.
 *   root (string) — the workspace root requested via -w/--workdir.
 *
 * Returns:
 *   error — non-nil, naming both directories, when cwd does not lie inside
 *           root; nil when cwd equals root or is a descendant of it.
 *
 * Example:
 *   cwd, _ := os.Getwd()
 *   if err := RequireCWDInRoot(cwd, cfg.WorkDir); err != nil {
 *       log.Fatal(err)
 *   }
 */
func RequireCWDInRoot(cwd, root string) error {
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("workspace: resolve cwd %q: %w", cwd, err)
	}
	realCWD, err := filepath.EvalSymlinks(absCWD)
	if err != nil {
		realCWD = absCWD
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("workspace: resolve workdir %q: %w", root, err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		realRoot = absRoot
	}

	if realCWD == realRoot {
		return nil
	}
	rel, err := filepath.Rel(realRoot, realCWD)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("workspace: current directory %q is not inside workspace root %q (run harvey from within the workspace tree, or drop -w/--workdir)", realCWD, realRoot)
	}
	return nil
}
