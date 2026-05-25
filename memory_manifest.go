package harvey

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const manifestFile = "manifest.yaml"

/** ManifestEntry records the result of mining a single session file.
 *
 * Fields:
 *   Path             (string)   — workspace-relative path to the session file.
 *   MinedAt          (string)   — RFC3339 timestamp when review completed.
 *   MemoriesCreated  ([]string) — IDs of memories accepted during review.
 *   MemoriesSkipped  (int)      — count of proposed memories the user skipped.
 *
 * Example:
 *   fmt.Printf("mined %s: created %d, skipped %d\n",
 *       e.Path, len(e.MemoriesCreated), e.MemoriesSkipped)
 */
type ManifestEntry struct {
	Path            string   `yaml:"path"`
	MinedAt         string   `yaml:"mined_at"`
	MemoriesCreated []string `yaml:"memories_created"`
	MemoriesSkipped int      `yaml:"memories_skipped"`
}

/** Manifest tracks which session files have been fully reviewed for memory
 * extraction. It is stored as agents/memories/manifest.yaml so it is
 * human-readable and version-controllable alongside the memory files.
 *
 * A session is recorded only after the full interactive review completes;
 * an interrupted review leaves the session absent from the manifest so it
 * will be offered again on the next /memory mine run.
 *
 * Example:
 *   m, err := LoadManifest(store.Dir())
 *   if m.IsMined("agents/sessions/foo.spmd") {
 *       fmt.Println("already reviewed")
 *   }
 */
type Manifest struct {
	Sessions []ManifestEntry `yaml:"sessions"`
}

/** LoadManifest reads the manifest from dir/manifest.yaml. If the file does
 * not exist an empty Manifest is returned without error.
 *
 * Parameters:
 *   dir (string) — absolute path to agents/memories/.
 *
 * Returns:
 *   *Manifest — loaded or empty manifest.
 *   error     — on read or YAML parse failure (not on missing file).
 *
 * Example:
 *   m, err := LoadManifest(store.Dir())
 */
func LoadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, manifestFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Manifest{Sessions: []ManifestEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("manifest: read: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if m.Sessions == nil {
		m.Sessions = []ManifestEntry{}
	}
	return &m, nil
}

/** Save writes the manifest to dir/manifest.yaml, creating the file if it
 * does not exist.
 *
 * Parameters:
 *   dir (string) — absolute path to agents/memories/.
 *
 * Returns:
 *   error — on YAML marshal or file write failure.
 *
 * Example:
 *   err := m.Save(store.Dir())
 */
func (m *Manifest) Save(dir string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("manifest: marshal: %w", err)
	}
	path := filepath.Join(dir, manifestFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("manifest: write: %w", err)
	}
	return nil
}

/** IsMined returns true if sessionPath appears in the manifest, indicating
 * that it has already been fully reviewed.
 *
 * Parameters:
 *   sessionPath (string) — path to the session file (any form; compared
 *                          as a string against stored paths).
 *
 * Returns:
 *   bool — true if the session has a completed review entry.
 *
 * Example:
 *   if !m.IsMined("agents/sessions/foo.spmd") {
 *       // offer for review
 *   }
 */
func (m *Manifest) IsMined(sessionPath string) bool {
	for _, e := range m.Sessions {
		if e.Path == sessionPath {
			return true
		}
	}
	return false
}

/** Record appends a completed review entry for sessionPath. created is the
 * list of memory IDs that were accepted; skipped is the count that were
 * declined.
 *
 * Parameters:
 *   sessionPath (string)   — path to the reviewed session file.
 *   created     ([]string) — IDs of memories saved during review.
 *   skipped     (int)      — number of proposed memories the user declined.
 *
 * Example:
 *   m.Record("agents/sessions/foo.spmd", []string{"tool_use_a1b2c3"}, 2)
 *   m.Save(store.Dir())
 */
func (m *Manifest) Record(sessionPath string, created []string, skipped int) {
	if created == nil {
		created = []string{}
	}
	m.Sessions = append(m.Sessions, ManifestEntry{
		Path:            sessionPath,
		MinedAt:         time.Now().UTC().Format(time.RFC3339),
		MemoriesCreated: created,
		MemoriesSkipped: skipped,
	})
}

/** UnminedSessions returns all .spmd files under sessionsDir that do not
 * appear in the manifest, in filesystem order. This is the set of sessions
 * that /memory mine would process.
 *
 * Parameters:
 *   sessionsDir (string) — absolute path to the sessions directory.
 *
 * Returns:
 *   []string — absolute paths to unmined session files.
 *   error    — on directory read failure.
 *
 * Example:
 *   paths, err := m.UnminedSessions("/home/user/project/agents/sessions")
 *   for _, p := range paths {
 *       fmt.Println(p)
 *   }
 */
func (m *Manifest) UnminedSessions(sessionsDir string) ([]string, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("manifest: list sessions: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext != ".spmd" && ext != ".fountain" {
			continue
		}
		absPath := filepath.Join(sessionsDir, name)
		if !m.IsMined(absPath) {
			out = append(out, absPath)
		}
	}
	return out, nil
}
