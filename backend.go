// Package harvey — backend.go defines the ManagedBackend abstraction shared
// by OllamaBackend, LlamafileBackend, and LlamaCppBackend.
package harvey

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const backendPIDFile = ".harvey-backend.pid"

/** ManagedBackend represents a local inference backend Harvey can start,
 * stop, and query. It is implemented by OllamaBackend, LlamafileBackend,
 * and LlamaCppBackend. All methods must be safe to call when the backend
 * is not running; Start and Stop are idempotent.
 *
 * Methods:
 *   Name()            — backend identifier: "ollama", "llamafile", or "llamacpp".
 *   BaseURL()         — HTTP base URL of the server, e.g. "http://127.0.0.1:11434".
 *   ActiveModel()     — name of the currently loaded model, or "" when idle.
 *   IsRunning()       — true when the server is reachable via HTTP probe.
 *   Detect()          — probe the server and return IsRunning().
 *   Start()           — launch the server with the given model; writes PID file.
 *   Stop()            — stop the server Harvey started; no-op if not owned.
 *   ListModels()      — enumerate locally available models.
 *   NewClient()       — return a wired LLMClient pointing at the running server.
 *   StartedByHarvey() — true when Harvey started the currently running server.
 *
 * Example:
 *   var b ManagedBackend = NewOllamaBackend(cfg)
 *   if b.Detect() {
 *       client, _ := b.NewClient()
 *   }
 */
type ManagedBackend interface {
	Name() string
	BaseURL() string
	ActiveModel() string
	IsRunning() bool
	Detect() bool
	Start(ctx context.Context, model string, out io.Writer) error
	Stop() error
	ListModels() ([]ModelSummary, error)
	NewClient() (LLMClient, error)
	StartedByHarvey() bool
}

/** ModelSummary is a backend-neutral description of a locally available model.
 * OllamaBackend.ListModels returns Ollama models; LlamafileBackend and
 * LlamaCppBackend return filesystem-scanned entries. The Engine field
 * distinguishes the source.
 *
 * Fields:
 *   Name      (string)    — display or alias name, e.g. "phi4-Q4_K_M".
 *   Path      (string)    — absolute filesystem path; empty for Ollama models.
 *   Engine    (string)    — "ollama", "llamafile", or "llamacpp".
 *   SizeBytes (int64)     — model weight size in bytes; 0 if unknown.
 *   Modified  (time.Time) — last modified time of the file; zero if unknown.
 *
 * Example:
 *   models, _ := backend.ListModels()
 *   for _, m := range models {
 *       fmt.Printf("%s (%s)\n", m.Name, m.Engine)
 *   }
 */
type ModelSummary struct {
	Name      string
	Path      string
	Engine    string
	SizeBytes int64
	Modified  time.Time
}

/** BackendPID is the JSON payload written to agents/.harvey-backend.pid when
 * Harvey starts a backend process. It persists across Harvey restarts so that
 * the next session can re-adopt or clean up the prior server.
 *
 * Fields:
 *   Backend (string) — backend name: "ollama", "llamafile", or "llamacpp".
 *   PID     (int)    — OS process ID of the server process.
 *   Model   (string) — model name or path the server was started with.
 *   URL     (string) — base HTTP URL the server is listening on.
 *
 * Example:
 *   p := BackendPID{Backend: "llamafile", PID: 12345, Model: "phi4", URL: "http://127.0.0.1:8080"}
 *   _ = writePIDFile("/workspace/agents", p)
 */
type BackendPID struct {
	Backend string `json:"backend"`
	PID     int    `json:"pid"`
	Model   string `json:"model"`
	URL     string `json:"url"`
}

/** writePIDFile writes p as JSON to <dir>/.harvey-backend.pid, creating or
 * truncating the file. The file records which backend process Harvey started
 * so it can be re-adopted or cleaned up on next startup.
 *
 * Parameters:
 *   dir (string)     — directory in which to write the PID file (typically agents/).
 *   p   (BackendPID) — payload to serialise.
 *
 * Returns:
 *   error — non-nil if the file cannot be created or written.
 *
 * Example:
 *   err := writePIDFile(a.Workspace.AgentsDir(), BackendPID{Backend: "ollama", PID: 1234, Model: "granite3.3:8b", URL: "http://127.0.0.1:11434"})
 */
func writePIDFile(dir string, p BackendPID) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("writePIDFile: marshal: %w", err)
	}
	path := filepath.Join(dir, backendPIDFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writePIDFile: %w", err)
	}
	return nil
}

/** readPIDFile reads and unmarshals the BackendPID from <dir>/.harvey-backend.pid.
 * Returns an error wrapping os.ErrNotExist when the file is absent.
 *
 * Parameters:
 *   dir (string) — directory containing the PID file.
 *
 * Returns:
 *   BackendPID — the decoded payload; zero value on error.
 *   error      — non-nil if the file is missing or malformed.
 *
 * Example:
 *   p, err := readPIDFile(a.Workspace.AgentsDir())
 *   if errors.Is(err, os.ErrNotExist) { /* no prior backend *\/ }
 */
func readPIDFile(dir string) (BackendPID, error) {
	path := filepath.Join(dir, backendPIDFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return BackendPID{}, fmt.Errorf("readPIDFile: %w", err)
	}
	var p BackendPID
	if err := json.Unmarshal(data, &p); err != nil {
		return BackendPID{}, fmt.Errorf("readPIDFile: unmarshal: %w", err)
	}
	return p, nil
}

/** deletePIDFile removes <dir>/.harvey-backend.pid. Safe to call when the
 * file does not exist; a missing file is silently ignored.
 *
 * Parameters:
 *   dir (string) — directory containing the PID file.
 *
 * Returns:
 *   error — non-nil only for unexpected filesystem errors.
 *
 * Example:
 *   _ = deletePIDFile(a.Workspace.AgentsDir())
 */
func deletePIDFile(dir string) error {
	path := filepath.Join(dir, backendPIDFile)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deletePIDFile: %w", err)
	}
	return nil
}

/** probeOwnedProcess reports whether the process recorded in p is still alive.
 * It uses os.FindProcess and sends signal 0 to check liveness without
 * interrupting the process. On platforms where FindProcess always succeeds
 * (Windows), this relies on the signal-0 check only.
 *
 * Parameters:
 *   p (BackendPID) — payload read from the PID file.
 *
 * Returns:
 *   bool — true if the process with PID p.PID is alive; false otherwise.
 *
 * Example:
 *   if pid, err := readPIDFile(dir); err == nil && probeOwnedProcess(pid) {
 *       // adopt the running server
 *   }
 */
func probeOwnedProcess(p BackendPID) bool {
	if p.PID <= 0 {
		return false
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		return false
	}
	// Signal 0 checks existence without affecting the process.
	return proc.Signal(syscall.Signal(0)) == nil
}
