package harvey

// sessions_files.go — fountain/spmd session file discovery and metadata extraction.
//
// Session files use the .spmd extension for new recordings and .fountain for
// files created by other LLM systems. Both are accepted everywhere Harvey reads
// session files; only .spmd is written for new sessions.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionFileInfo describes a single session file found on disk.
type SessionFileInfo struct {
	Path    string    // absolute path to the file
	Name    string    // basename without extension
	ModTime time.Time // file modification time
}

/** ListSessionFiles returns all .spmd and .fountain files in dir, sorted
 * newest first by modification time. Returns nil (not an error) when dir does
 * not exist.
 *
 * Parameters:
 *   dir (string) — directory to scan.
 *
 * Returns:
 *   []SessionFileInfo — session files, newest first; nil if none found.
 *   error             — on read failure (not on missing dir).
 *
 * Example:
 *   files, err := ListSessionFiles("agents/sessions")
 *   for _, f := range files {
 *       fmt.Printf("%s  %s\n", f.ModTime.Format("2006-01-02 15:04"), f.Name)
 *   }
 */
func ListSessionFiles(dir string) ([]SessionFileInfo, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []SessionFileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".spmd") && !strings.HasSuffix(name, ".fountain") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(strings.TrimSuffix(name, ".fountain"), ".spmd")
		files = append(files, SessionFileInfo{
			Path:    filepath.Join(dir, name),
			Name:    base,
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files, nil
}

/** ExtractModelFromSession parses a .spmd or .fountain session file and
 * returns the model name recorded as a CHARACTER element. Returns an empty
 * string when no model character can be identified.
 *
 * Parameters:
 *   path (string) — path to a Harvey session file (.spmd or .fountain).
 *
 * Returns:
 *   string — ALL-CAPS model name (e.g. "GEMMA4"), or "" if not found.
 *   error  — on parse failure.
 *
 * Example:
 *   model, err := ExtractModelFromSession("agents/sessions/session.spmd")
 *   // model == "GEMMA4"
 */
func ExtractModelFromSession(path string) (string, error) {
	_, model, _, err := parseFountainSession(path)
	if err != nil {
		return "", err
	}
	return model, nil
}

/** ResolveSessionsDir returns the absolute path to the sessions directory,
 * creating it if it does not exist. customPath overrides the default location
 * (harvey/sessions/ inside the workspace); pass an empty string for the default.
 *
 * Parameters:
 *   ws         (*Workspace) — the Harvey workspace.
 *   customPath (string)     — override path; may be relative (to workspace root) or absolute.
 *
 * Returns:
 *   string — absolute path to the sessions directory.
 *   error  — if the directory cannot be created.
 *
 * Example:
 *   dir, err := ResolveSessionsDir(ws, cfg.SessionsDir)
 */
func ResolveSessionsDir(ws *Workspace, customPath string) (string, error) {
	var dir string
	if customPath != "" {
		if filepath.IsAbs(customPath) {
			dir = customPath
		} else {
			var err error
			dir, err = ws.AbsPath(customPath)
			if err != nil {
				return "", err
			}
		}
	} else {
		dir = filepath.Join(ws.HarveyDir(), "sessions")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
