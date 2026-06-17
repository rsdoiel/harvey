// Package harvey — llamafile_service.go provides helpers for managing a
// llamafile inference server process: probing whether one is running and
// launching it as a background service when it is not.
package harvey

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"
)

/** ProbeLlamafile returns true if a llamafile HTTP server is reachable at
 * baseURL. It sends a GET request to the /v1/models endpoint with a short
 * timeout so that Harvey's startup sequence does not stall when nothing is
 * listening.
 *
 * Parameters:
 *   baseURL (string) — llamafile server base URL, e.g. "http://localhost:8080".
 *
 * Returns:
 *   bool — true when the server responds with HTTP 200.
 *
 * Example:
 *   if ProbeLlamafile("http://localhost:8080") {
 *       fmt.Println("llamafile is ready")
 *   }
 */
func ProbeLlamafile(baseURL string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(baseURL + "/v1/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

/** StartLlamafileService launches the llamafile binary at path as a background
 * HTTP server and waits up to 10 seconds for it to become reachable. The
 * listening port is derived from baseURL; port 8080 is used when none is
 * present. When logPath is non-empty, subprocess stdout and stderr are
 * redirected to that file so output is captured rather than discarded.
 *
 * The binary is invoked with:
 *   <path> --host 127.0.0.1 --port <port> --nobrowser
 *
 * Parameters:
 *   path    (string) — absolute path to the llamafile executable.
 *   baseURL (string) — llamafile API base URL used to derive the port and
 *                      probe readiness, e.g. "http://localhost:8080".
 *   logPath (string) — file path for subprocess output; "" discards output.
 *
 * Returns:
 *   *os.Process — the started process; nil on failure.
 *   error       — non-nil if the process fails to start or does not respond within 10s.
 *
 * Example:
 *   proc, err := StartLlamafileService("/models/gemma3.llamafile", "http://localhost:8080", "")
 */
func StartLlamafileService(path, baseURL, logPath string) (*os.Process, error) {
	port := "8080"
	if u, err := url.Parse(baseURL); err == nil && u.Port() != "" {
		port = u.Port()
	}

	cmd := exec.Command(path, "--host", "127.0.0.1", "--port", port, "--nobrowser")
	if logPath != "" {
		f, err := os.Create(logPath)
		if err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
			defer f.Close()
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not launch llamafile: %w", err)
	}

	// llamafile needs to map and decompress the embedded GGUF before the
	// server accepts connections, so allow up to 10 seconds (double Ollama's
	// budget) before giving up.
	for range 20 {
		time.Sleep(500 * time.Millisecond)
		if ProbeLlamafile(baseURL) {
			return cmd.Process, nil
		}
	}
	return nil, fmt.Errorf("llamafile started but did not respond within 10s")
}
