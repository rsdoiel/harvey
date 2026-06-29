// Package harvey — backend_llamacpp.go implements ManagedBackend for llama.cpp's llama-server.
package harvey

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

/** LlamaCppBackend implements ManagedBackend for the llama.cpp llama-server process.
 * It exposes an OpenAI-compatible API at BaseURL/v1, so the same LLM client
 * used for llamafile works here. Start launches llama-server with --model, --port,
 * and optional --ctx-size / --threads / --n-gpu-layers flags. ListModels scans
 * modelsDir for *.gguf files.
 *
 * Fields:
 *   serverBin     (string)        — path to llama-server binary; "" = "llama-server" (PATH lookup).
 *   url           (string)        — API base URL, e.g. "http://127.0.0.1:8081".
 *   agentsDir     (string)        — directory for the PID file.
 *   modelsDir     (string)        — directory scanned by ListModels for *.gguf files.
 *   ctxSize       (int)           — --ctx-size; 0 = server default.
 *   threads       (int)           — --threads; 0 = server default.
 *   gpuLayers     (int)           — --n-gpu-layers; 0 = CPU-only.
 *   startTimeout  (time.Duration) — how long to wait for the server after launch.
 *   activeModel   (string)        — model name set on Start.
 *   running       (bool)          — reflects the last Detect probe.
 *   proc          (*os.Process)   — process Harvey started; nil if adopted externally.
 *
 * Example:
 *   b := NewLlamaCppBackend(cfg, filepath.Join(ws.Root, "agents"))
 *   if err := b.Start(ctx, "/home/user/Models/phi4-Q4_K_M.gguf", os.Stdout); err != nil { ... }
 */
type LlamaCppBackend struct {
	serverBin    string
	url          string
	agentsDir    string
	modelsDir    string
	ctxSize      int
	threads      int
	gpuLayers    int
	startTimeout time.Duration
	activeModel  string
	running      bool
	proc         *os.Process
}

/** NewLlamaCppBackend returns a LlamaCppBackend configured from cfg.
 * No network calls are made during construction.
 *
 * Parameters:
 *   cfg       (*Config) — source of LlamaCpp settings (URL, GPULayers, CtxSize, etc.).
 *   agentsDir (string)  — directory for the PID file.
 *
 * Returns:
 *   *LlamaCppBackend — ready to call Detect() or Start().
 *
 * Example:
 *   b := NewLlamaCppBackend(cfg, filepath.Join(ws.Root, "agents"))
 */
func NewLlamaCppBackend(cfg *Config, agentsDir string) *LlamaCppBackend {
	modelsDir := cfg.LlamaCpp.ModelsDir
	if modelsDir == "" {
		modelsDir = llamafileDefaultModelsDir()
	}
	serverBin := cfg.LlamaCpp.ServerBin
	if serverBin == "" {
		serverBin = "llama-server"
	}
	startTimeout := cfg.LlamaCpp.StartTimeout
	if startTimeout <= 0 {
		startTimeout = 120 * time.Second
	}
	return &LlamaCppBackend{
		serverBin:    serverBin,
		url:          cfg.LlamaCpp.URL,
		agentsDir:    agentsDir,
		modelsDir:    modelsDir,
		ctxSize:      cfg.LlamaCpp.CtxSize,
		threads:      cfg.LlamaCpp.Threads,
		gpuLayers:    cfg.LlamaCpp.GPULayers,
		startTimeout: startTimeout,
	}
}

/** Name returns the backend identifier "llamacpp".
 *
 * Returns:
 *   string — always "llamacpp".
 *
 * Example:
 *   fmt.Println(b.Name()) // "llamacpp"
 */
func (b *LlamaCppBackend) Name() string { return "llamacpp" }

/** BaseURL returns the HTTP base URL of the llama-server.
 *
 * Returns:
 *   string — the URL configured for this backend.
 *
 * Example:
 *   fmt.Println(b.BaseURL()) // "http://127.0.0.1:8081"
 */
func (b *LlamaCppBackend) BaseURL() string { return b.url }

/** ActiveModel returns the name of the model currently in use, or "" when none is set.
 *
 * Returns:
 *   string — model name, or "" before Start is called.
 *
 * Example:
 *   fmt.Println(b.ActiveModel()) // "phi4-Q4_K_M"
 */
func (b *LlamaCppBackend) ActiveModel() string { return b.activeModel }

/** IsRunning reports the result of the most recent Detect probe without making
 * a network call. Call Detect() first to refresh.
 *
 * Returns:
 *   bool — true if the last Detect() succeeded.
 *
 * Example:
 *   b.Detect()
 *   if b.IsRunning() { ... }
 */
func (b *LlamaCppBackend) IsRunning() bool { return b.running }

/** Detect probes the llama-server at BaseURL/health and updates IsRunning.
 * llama-server exposes GET /health which returns {"status":"ok"} when ready.
 *
 * Returns:
 *   bool — true if the server is reachable and healthy.
 *
 * Example:
 *   if b.Detect() { fmt.Println("llama-server is up") }
 */
func (b *LlamaCppBackend) Detect() bool {
	b.running = probeLlamaCpp(b.url)
	return b.running
}

/** StartedByHarvey reports whether Harvey launched this server process.
 *
 * Returns:
 *   bool — true when Harvey started the process (proc != nil).
 *
 * Example:
 *   if b.StartedByHarvey() { defer b.Stop() }
 */
func (b *LlamaCppBackend) StartedByHarvey() bool { return b.proc != nil }

/** Start launches llama-server for the given model path, waits for the health
 * endpoint to respond, and writes a PID file. If a server is already running
 * at BaseURL, Start adopts it without launching a new process.
 *
 * Parameters:
 *   ctx   (context.Context) — controls the launch lifetime (passed for interface conformance; actual timeout is b.startTimeout).
 *   model (string)          — absolute path to a .gguf model file.
 *   out   (io.Writer)       — status messages sink.
 *
 * Returns:
 *   error — non-nil if the binary cannot be launched or the server does not respond.
 *
 * Example:
 *   err := b.Start(ctx, "/home/user/Models/phi4-Q4_K_M.gguf", os.Stdout)
 */
func (b *LlamaCppBackend) Start(ctx context.Context, model string, out io.Writer) error {
	if b.Detect() {
		// Adopt the running server.
		b.activeModel = ggufModelName(model)
		return nil
	}

	port := portFromURL(b.url)
	if port == "" {
		return fmt.Errorf("cannot extract port from llamacpp URL %q", b.url)
	}

	args := []string{"--model", model, "--port", port}
	if b.ctxSize > 0 {
		args = append(args, "--ctx-size", fmt.Sprintf("%d", b.ctxSize))
	}
	if b.threads > 0 {
		args = append(args, "--threads", fmt.Sprintf("%d", b.threads))
	}
	if b.gpuLayers > 0 {
		args = append(args, "--n-gpu-layers", fmt.Sprintf("%d", b.gpuLayers))
	}

	bin, err := exec.LookPath(b.serverBin)
	if err != nil {
		return fmt.Errorf("llama-server not found (%s): %w", b.serverBin, err)
	}

	fmt.Fprintf(out, "  Starting llama-server for %s...\n", ggufModelName(model))
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start llama-server: %w", err)
	}

	// Wait for the server to become reachable.
	deadline := time.Now().Add(b.startTimeout)
	for time.Now().Before(deadline) {
		if probeLlamaCpp(b.url) {
			b.proc = cmd.Process
			b.running = true
			b.activeModel = ggufModelName(model)
			_ = writePIDFile(b.agentsDir, BackendPID{
				Backend: "llamacpp",
				PID:     cmd.Process.Pid,
				Model:   b.activeModel,
				URL:     b.url,
			})
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Timed out — kill the process we started.
	_ = cmd.Process.Kill()
	return fmt.Errorf("llama-server did not respond within %v", b.startTimeout)
}

/** Stop sends SIGINT to the llama-server Harvey started. Safe to call when
 * Harvey did not start the server (no-op) or when the process is already gone.
 * Also deletes the PID file on success.
 *
 * Returns:
 *   error — non-nil only on unexpected kill failures.
 *
 * Example:
 *   defer b.Stop()
 */
func (b *LlamaCppBackend) Stop() error {
	if b.proc == nil {
		return nil
	}
	err := b.proc.Signal(os.Interrupt)
	if err != nil {
		_ = b.proc.Kill()
	}
	b.proc = nil
	b.running = false
	_ = deletePIDFile(b.agentsDir)
	return err
}

/** ListModels scans modelsDir for *.gguf files and returns a backend-neutral
 * ModelSummary for each one found.
 *
 * Returns:
 *   []ModelSummary — one entry per .gguf file found in modelsDir.
 *   error          — non-nil only on unexpected filesystem errors.
 *
 * Example:
 *   models, err := b.ListModels()
 *   for _, m := range models { fmt.Println(m.Name, m.SizeBytes) }
 */
func (b *LlamaCppBackend) ListModels() ([]ModelSummary, error) {
	paths := scanGGUFModels(b.modelsDir)
	out := make([]ModelSummary, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, ModelSummary{
			Name:      ggufModelName(p),
			Path:      p,
			Engine:    "llamacpp",
			SizeBytes: info.Size(),
			Modified:  info.ModTime(),
		})
	}
	return out, nil
}

/** NewClient returns an LLM client configured for the active model.
 * Returns an error when no active model has been set (call Start first).
 *
 * Returns:
 *   LLMClient — client pointing at BaseURL/v1 with the active model name.
 *   error     — non-nil when ActiveModel is empty.
 *
 * Example:
 *   client, err := b.NewClient()
 */
func (b *LlamaCppBackend) NewClient() (LLMClient, error) {
	if b.activeModel == "" {
		return nil, fmt.Errorf("llamacpp: no active model — call Start first")
	}
	return newLlamafileLLMClient(b.url+"/v1", b.activeModel, 0), nil
}

// wireLlamaCppBackend wires a freshly-started LlamaCppBackend as a.Backend.
func (a *Agent) wireLlamaCppBackend(proc *os.Process, name string) {
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	b := NewLlamaCppBackend(a.Config, agentsDir)
	b.proc = proc
	b.activeModel = name
	b.running = true
	a.Backend = b
}

// probeLlamaCpp returns true when the llama-server at baseURL is healthy.
// It probes GET /health which llama-server serves when ready.
func probeLlamaCpp(baseURL string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// portFromURL extracts the port string from a URL, e.g. "8081" from "http://127.0.0.1:8081".
// Returns "" when the URL cannot be parsed or has no explicit port.
func portFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Port()
}

// ggufModelName returns the filename stem of a .gguf path, e.g.
// "/home/user/Models/phi4-Q4_K_M.gguf" → "phi4-Q4_K_M".
func ggufModelName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".gguf")
}

// scanGGUFModels returns the absolute paths of all *.gguf files in dir.
func scanGGUFModels(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gguf") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths
}
