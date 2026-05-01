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
 * absolute, symlink-free form. The .harvey/ sub-directory is created if it
 * does not exist.
 *
 * Parameters:
 *   dir (string) — path to use as the workspace root; "." uses the current
 *                  working directory.
 *
 * Returns:
 *   *Workspace — the initialised workspace.
 *   error      — if dir cannot be resolved or .harvey/ cannot be created.
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
const harveySubdir = "harvey"

/** HarveyDir returns the absolute path of Harvey's internal state directory
 * (harvey/) inside the workspace root.
 *
 * Returns:
 *   string — absolute path to <Root>/harvey/
 *
 * Example:
 *   fmt.Println(ws.HarveyDir()) // "/home/user/myproject/harvey"
 */
func (ws *Workspace) HarveyDir() string {
	return filepath.Join(ws.Root, harveySubdir)
}

/** AbsPath resolves rel relative to the workspace root and verifies that the
 * resulting path lies inside the root. It returns an error for any path that
 * would escape the workspace (e.g. via ".." components or absolute paths
 * outside Root).
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
	// Join cleans ".." components before we do the prefix check.
	candidate := filepath.Join(ws.Root, rel)
	if !strings.HasPrefix(candidate+string(filepath.Separator), ws.Root+string(filepath.Separator)) {
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
