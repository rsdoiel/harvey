package harvey

import (
	"fmt"
	"os"
	"time"
)

/** Recorder writes a Harvey session to a Markdown file for later analysis.
 * Each exchange is appended as a numbered turn with clearly delimited
 * User and Harvey sections.
 *
 * Markdown structure:
 *
 *   # Harvey Session
 *
 *   - **Started:** 2026-04-15 14:23:00
 *   - **Model:** Ollama (llama3:latest)
 *   - **Workspace:** /home/user/project
 *
 *   ---
 *
 *   ### Turn 1
 *
 *   **User**
 *
 *   What is the capital of France?
 *
 *   **Harvey**
 *
 *   The capital of France is Paris.
 *
 *   ---
 *
 * Example:
 *   r, err := NewRecorder("session.md", "Ollama (llama3)", "/home/user/proj")
 *   if err != nil { ... }
 *   defer r.Close()
 *   r.RecordTurn("Hello", "Hi there!")
 */
type Recorder struct {
	f    *os.File
	path string
	turn int
}

/** NewRecorder creates (or truncates) the file at path, writes the session
 * header, and returns a ready-to-use Recorder.
 *
 * Parameters:
 *   path      (string) — file path for the Markdown session log.
 *   model     (string) — human-readable model/backend identifier.
 *   workspace (string) — absolute path of the Harvey workspace root.
 *
 * Returns:
 *   *Recorder — open recorder; caller must call Close() when done.
 *   error     — if the file cannot be created or the header cannot be written.
 *
 * Example:
 *   r, err := NewRecorder("harvey-session.md", "Ollama (llama3)", "/home/user/proj")
 */
func NewRecorder(path, model, workspace string) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("recorder: cannot create %s: %w", path, err)
	}
	r := &Recorder{f: f, path: path}
	started := time.Now().Format("2006-01-02 15:04:05")
	_, err = fmt.Fprintf(f,
		"# Harvey Session\n\n- **Started:** %s\n- **Model:** %s\n- **Workspace:** %s\n\n---\n",
		started, model, workspace,
	)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("recorder: write header: %w", err)
	}
	return r, nil
}

/** Path returns the file path this recorder is writing to.
 *
 * Returns:
 *   string — the file path passed to NewRecorder.
 *
 * Example:
 *   fmt.Println(r.Path()) // "/home/user/harvey-session.md"
 */
func (r *Recorder) Path() string { return r.path }

/** RecordTurn appends one user/Harvey exchange to the session file as a
 * numbered turn section.
 *
 * Parameters:
 *   userInput    (string) — the user's raw input text.
 *   harveyReply  (string) — the assistant's complete response text.
 *
 * Returns:
 *   error — if the write fails.
 *
 * Example:
 *   err := r.RecordTurn("What is 2+2?", "2 + 2 = 4.")
 */
func (r *Recorder) RecordTurn(userInput, harveyReply string) error {
	r.turn++
	_, err := fmt.Fprintf(r.f,
		"\n### Turn %d\n\n**User**\n\n%s\n\n**Harvey**\n\n%s\n\n---\n",
		r.turn, userInput, harveyReply,
	)
	return err
}

/** Close flushes and closes the session file. After Close, the Recorder
 * must not be used.
 *
 * Returns:
 *   error — if the file cannot be closed.
 *
 * Example:
 *   defer r.Close()
 */
func (r *Recorder) Close() error {
	return r.f.Close()
}

/** DefaultSessionPath returns a timestamped default filename for a session
 * log in the given directory.
 *
 * Parameters:
 *   dir (string) — directory in which to place the file.
 *
 * Returns:
 *   string — path of the form "<dir>/harvey-session-YYYYMMDD-HHMMSS.md".
 *
 * Example:
 *   path := DefaultSessionPath("/home/user/project")
 *   // "/home/user/project/harvey-session-20260415-142300.md"
 */
func DefaultSessionPath(dir string) string {
	ts := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s/harvey-session-%s.md", dir, ts)
}
