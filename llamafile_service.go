// Package harvey — llamafile_service.go provides helpers for managing a
// llamafile inference server process: probing whether one is running and
// launching it as a background service when it is not.
package harvey

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

/** FindFreePort asks the OS for an available TCP port by briefly binding to
 * :0, then returns the assigned port number. Used to select a distinct port
 * for an assay llamafile run so it does not conflict with a running Harvey
 * session on the default llamafile port.
 *
 * Returns:
 *   int   — a free port number.
 *   error — if no port could be obtained.
 *
 * Example:
 *   port, err := FindFreePort()
 *   url := fmt.Sprintf("http://localhost:%d", port)
 */
func FindFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

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

/** ProbeLlamafileContextLength queries the /v1/models endpoint of a running
 * llamafile server and returns the runtime context window size in tokens from
 * the data[0].meta.n_ctx field. Returns 0 when the server is unreachable,
 * the field is absent, or any error occurs.
 *
 * Parameters:
 *   baseURL (string) — llamafile API base URL, e.g. "http://localhost:8080".
 *
 * Returns:
 *   int — context window in tokens, or 0 if unknown.
 *
 * Example:
 *   ctxLen := ProbeLlamafileContextLength("http://localhost:8080")
 *   // ctxLen == 16384 for Qwen3.5 models at default settings
 */
func ProbeLlamafileContextLength(baseURL string) int {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(baseURL + "/v1/models")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 0
	}
	defer resp.Body.Close()
	var payload struct {
		Data []struct {
			Meta struct {
				NCtx int `json:"n_ctx"`
			} `json:"meta"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload.Data) == 0 {
		return 0
	}
	return payload.Data[0].Meta.NCtx
}

/** LlamafileProps holds the capability information extracted from the /props
 * endpoint of a running llamafile or llama.cpp server.
 *
 * Fields:
 *   SupportsTools (CapabilityStatus) — CapYes when a recognised tool-call marker
 *                                      was found in chat_template; CapNo otherwise.
 *   ToolMode      (string)           — ToolModeStructured when tools are supported;
 *                                      ToolModeNone otherwise.
 *
 * Example:
 *   props := ProbeLlamafileProps("http://localhost:8080")
 *   fmt.Println(props.SupportsTools) // "✓" or "—"
 */
type LlamafileProps struct {
	SupportsTools CapabilityStatus
	ToolMode      string
}

/** ProbeLlamafileProps reads the /props endpoint of a running llamafile or
 * llama.cpp server and returns the detected tool-calling capability derived
 * from the chat_template field. It recognises the following markers:
 *
 *   <tool_call>      — Qwen / Hermes style
 *   [TOOL_CALLS]     — Mistral style
 *   <|python_tag|>   — Llama 3.1+ style
 *   <|tool_call|>    — Phi-3/4 style
 *
 * When any marker is found, SupportsTools is CapYes and ToolMode is
 * ToolModeStructured. When /props is unreachable or returns no template,
 * SupportsTools is CapNo and ToolMode is ToolModeNone.
 *
 * Parameters:
 *   baseURL (string) — llamafile server base URL, e.g. "http://localhost:8080".
 *
 * Returns:
 *   LlamafileProps — detected capability; CapNo/ToolModeNone on any error.
 *
 * Example:
 *   props := ProbeLlamafileProps("http://localhost:8080")
 *   if props.SupportsTools == CapYes {
 *       fmt.Println("model supports structured tool calls")
 *   }
 */
func ProbeLlamafileProps(baseURL string) LlamafileProps {
	none := LlamafileProps{SupportsTools: CapNo, ToolMode: ToolModeNone}

	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(baseURL + "/props")
	if err != nil || resp.StatusCode != http.StatusOK {
		return none
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return none
	}

	var payload struct {
		ChatTemplate string `json:"chat_template"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.ChatTemplate == "" {
		return none
	}

	markers := []string{
		"<tool_call>",    // Qwen / Hermes
		"[TOOL_CALLS]",   // Mistral
		"<|python_tag|>", // Llama 3.1+
		"<|tool_call|>",  // Phi-3/4
	}
	for _, m := range markers {
		if strings.Contains(payload.ChatTemplate, m) {
			return LlamafileProps{SupportsTools: CapYes, ToolMode: ToolModeStructured}
		}
	}
	return none
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
 *   ctxSize   (int)           — context window in tokens passed via -c; 0 uses the model default.
 *   progress  (io.Writer)     — receives a dot every 5 s while waiting; nil = silent.
 *
 * Returns:
 *   *os.Process — the started process; nil on failure.
 *   error       — non-nil if the process fails to start or does not respond within timeout.
 *
 * Example:
 *   proc, err := StartLlamafileService("/models/gemma3.llamafile", "http://localhost:8080", "", 120*time.Second, 99, 49152, os.Stdout)
 */
// buildLlamafileArgs assembles the argument list for launching a llamafile
// server process. Negative gpuLayers omits the -ngl flag (CPU-only). Zero
// ctxSize omits -c (server uses its compiled-in default).
func buildLlamafileArgs(path, port string, gpuLayers, ctxSize int) []string {
	args := []string{path, "--server", "--host", "127.0.0.1", "--port", port}
	if gpuLayers >= 0 {
		args = append(args, "-ngl", fmt.Sprintf("%d", gpuLayers))
	}
	if ctxSize > 0 {
		args = append(args, "-c", fmt.Sprintf("%d", ctxSize))
	}
	return args
}

func StartLlamafileService(path, baseURL, logPath string, timeout time.Duration, gpuLayers, ctxSize int, progress io.Writer) (*os.Process, error) {
	// Guard against a crafted harvey.yaml entry executing an arbitrary script.
	// Only binaries with a recognised llamafile extension are launched.
	lower := strings.ToLower(path)
	if !strings.HasSuffix(lower, ".llamafile") && !strings.HasSuffix(lower, ".llamafile.exe") {
		return nil, fmt.Errorf("llamafile: %q does not have a .llamafile or .llamafile.exe extension", path)
	}

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
	args := buildLlamafileArgs(path, port, gpuLayers, ctxSize)
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
