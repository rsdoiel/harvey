// Package harvey — llamafile_service.go provides helpers for managing a
// llamafile inference server process: probing whether one is running and
// launching it as a background service when it is not.
package harvey

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
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
 * HTTP server and waits up to timeout for it to become reachable. The
 * listening port is derived from baseURL; port 8080 is used when none is
 * present. When logPath is non-empty, subprocess stdout and stderr are
 * redirected to that file so output is captured rather than discarded.
 * Progress dots are written to progress every 5 seconds while waiting;
 * pass nil to suppress them.
 *
 * The binary is invoked with:
 *   /bin/sh <path> --server --host 127.0.0.1 --port <port> [-ngl N]
 *
 * Parameters:
 *   path      (string)        — absolute path to the llamafile executable.
 *   baseURL   (string)        — llamafile API base URL used to derive the port
 *                               and probe readiness, e.g. "http://localhost:8080".
 *   logPath   (string)        — file path for subprocess output; "" discards output.
 *   timeout   (time.Duration) — how long to wait for the server; 0 uses 120s.
 *   gpuLayers (int)           — layers to offload to GPU via -ngl; negative = omit flag (CPU).
 *   progress  (io.Writer)     — receives a dot every 5 s while waiting; nil = silent.
 *
 * Returns:
 *   *os.Process — the started process; nil on failure.
 *   error       — non-nil if the process fails to start or does not respond within timeout.
 *
 * Example:
 *   proc, err := StartLlamafileService("/models/gemma3.llamafile", "http://localhost:8080", "", 120*time.Second, 99, os.Stdout)
 */
func StartLlamafileService(path, baseURL, logPath string, timeout time.Duration, gpuLayers int, progress io.Writer) (*os.Process, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	port := "8080"
	if u, err := url.Parse(baseURL); err == nil && u.Port() != "" {
		port = u.Port()
	}

	// APE-format llamafiles (macOS) are rejected by execve; shells handle
	// them via a POSIX fallback that Go's exec.Command omits. Running through
	// sh reproduces what the terminal does when you run the file directly.
	args := []string{path, "--server", "--host", "127.0.0.1", "--port", port}
	if gpuLayers >= 0 {
		args = append(args, "-ngl", fmt.Sprintf("%d", gpuLayers))
	}
	cmd := exec.Command("/bin/sh", args...)

	// Always capture stderr so we can surface it in error messages.
	// If a log file is requested, tee to both the file and the buffer.
	var stderrBuf bytes.Buffer
	if logPath != "" {
		f, err := os.Create(logPath)
		if err == nil {
			cmd.Stdout = f
			cmd.Stderr = io.MultiWriter(f, &stderrBuf)
			defer f.Close()
		}
	} else {
		cmd.Stderr = &stderrBuf
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not launch llamafile: %w", err)
	}

	// Watch for early process exit in a goroutine so the poll loop can fail
	// fast instead of waiting out the full timeout (e.g. bad path, not executable).
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	exitErr := func(base string, waitErr error) error {
		msg := base
		if waitErr != nil {
			msg = fmt.Sprintf("%s: %v", base, waitErr)
		}
		if out := strings.TrimSpace(stderrBuf.String()); out != "" {
			msg += "\n" + out
		}
		return fmt.Errorf("%s", msg)
	}

	// llamafile must extract its embedded GGUF and initialise the inference
	// engine before the HTTP server accepts connections; on large models this
	// can take well over a minute on first run.
	deadline := time.Now().Add(timeout)
	dotTick := time.Now()
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-exited:
			if progress != nil {
				fmt.Fprintln(progress)
			}
			return nil, exitErr("llamafile exited before server became ready", waitErr)
		default:
		}
		time.Sleep(500 * time.Millisecond)
		if ProbeLlamafile(baseURL) {
			if progress != nil {
				fmt.Fprintln(progress)
			}
			return cmd.Process, nil
		}
		if progress != nil && time.Since(dotTick) >= 5*time.Second {
			fmt.Fprint(progress, ".")
			dotTick = time.Now()
		}
	}
	return nil, fmt.Errorf("llamafile started but did not respond within %s", timeout)
}
