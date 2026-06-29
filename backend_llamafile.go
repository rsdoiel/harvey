// Package harvey — backend_llamafile.go implements ManagedBackend for llamafile.
package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

/** LlamafileBackend implements ManagedBackend for llamafile inference servers.
 * Each llamafile binary is its own single-model server; Start launches the
 * binary for the requested model path. It wraps StartLlamafileService and
 * adds unified lifecycle tracking through the ManagedBackend interface.
 *
 * Fields:
 *   url             (string)        — llamafile server base URL, e.g. "http://localhost:8080".
 *   timeout         (time.Duration) — HTTP client timeout passed to the LLM client.
 *   agentsDir       (string)        — directory for the PID file.
 *   modelsDir       (string)        — directory scanned by ListModels for *.llamafile files.
 *   workspaceRoot   (string)        — workspace root used to resolve relative model paths.
 *   gpuLayers       (int)           — -ngl value; -1 = CPU, 99 = maximise GPU.
 *   ctxSize         (int)           — -c value; 0 = server default.
 *   startupTimeout  (time.Duration) — how long to wait for the server after launch.
 *   maxTokens       (int)           — max completion tokens; 0 = no limit.
 *   activeModel     (string)        — model name recorded when Start is called.
 *   running         (bool)          — reflects the last Detect probe.
 *   proc            (*os.Process)   — process Harvey started; nil if adopted externally.
 *
 * Example:
 *   b := NewLlamafileBackend(cfg, filepath.Join(ws.Root, "agents"), ws.Root)
 *   if err := b.Start(ctx, "/home/user/Models/phi4.llamafile", out); err != nil { ... }
 */
type LlamafileBackend struct {
	url            string
	timeout        time.Duration
	agentsDir      string
	modelsDir      string
	workspaceRoot  string
	gpuLayers      int
	ctxSize        int
	startupTimeout time.Duration
	maxTokens      int
	activeModel    string
	running        bool
	proc           *os.Process
}

/** NewLlamafileBackend returns a LlamafileBackend configured from cfg.
 * No network calls are made during construction.
 *
 * Parameters:
 *   cfg           (*Config) — source of URL, GPU layers, timeout, models dir, etc.
 *   agentsDir     (string)  — directory for the PID file.
 *   workspaceRoot (string)  — workspace root for resolving relative model paths.
 *
 * Returns:
 *   *LlamafileBackend — ready to call Detect() or Start().
 *
 * Example:
 *   b := NewLlamafileBackend(cfg, filepath.Join(ws.Root, "agents"), ws.Root)
 */
func NewLlamafileBackend(cfg *Config, agentsDir, workspaceRoot string) *LlamafileBackend {
	modelsDir := cfg.Llamafile.ModelsDir
	if modelsDir == "" {
		modelsDir = llamafileDefaultModelsDir()
	}
	timeout := cfg.Llamafile.StartupTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &LlamafileBackend{
		url:            cfg.Llamafile.URL,
		timeout:        cfg.Ollama.Timeout,
		agentsDir:      agentsDir,
		modelsDir:      modelsDir,
		workspaceRoot:  workspaceRoot,
		gpuLayers:      cfg.Llamafile.GPULayers,
		ctxSize:        cfg.ActiveLlamafileContextLength(),
		startupTimeout: timeout,
		maxTokens:      cfg.Llamafile.MaxTokens,
	}
}

/** Name returns the backend identifier "llamafile".
 *
 * Returns:
 *   string — always "llamafile".
 *
 * Example:
 *   fmt.Println(b.Name()) // "llamafile"
 */
func (b *LlamafileBackend) Name() string { return "llamafile" }

/** BaseURL returns the HTTP base URL of the llamafile server.
 *
 * Returns:
 *   string — the URL configured for this backend.
 *
 * Example:
 *   fmt.Println(b.BaseURL()) // "http://localhost:8080"
 */
func (b *LlamafileBackend) BaseURL() string { return b.url }

/** ActiveModel returns the name of the model currently in use, or "" when none is set.
 *
 * Returns:
 *   string — model name, or "" before Start is called.
 *
 * Example:
 *   fmt.Println(b.ActiveModel()) // "phi4-Q4_K_M"
 */
func (b *LlamafileBackend) ActiveModel() string { return b.activeModel }

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
func (b *LlamafileBackend) IsRunning() bool { return b.running }

/** Detect probes the llamafile server at BaseURL and updates IsRunning.
 *
 * Returns:
 *   bool — true if the server is reachable at /v1/models.
 *
 * Example:
 *   if b.Detect() { fmt.Println("llamafile is up") }
 */
func (b *LlamafileBackend) Detect() bool {
	b.running = ProbeLlamafile(b.url)
	return b.running
}

/** Start launches the llamafile binary at model (an absolute path or a path
 * relative to workspaceRoot), waits for the server to become reachable, sets
 * the active model name to the stem of the path, marks StartedByHarvey, and
 * writes a PID file. If a server is already running at BaseURL, Start adopts
 * it without launching a new process.
 *
 * Parameters:
 *   ctx   (context.Context) — controls the launch lifetime (passed for interface conformance; actual timeout is b.startupTimeout).
 *   model (string)          — absolute or workspace-relative path to the .llamafile binary.
 *   out   (io.Writer)       — status messages sink.
 *
 * Returns:
 *   error — non-nil if the binary cannot be launched or the server does not respond.
 *
 * Example:
 *   err := b.Start(ctx, "/home/user/Models/phi4.llamafile", os.Stdout)
 */
func (b *LlamafileBackend) Start(ctx context.Context, model string, out io.Writer) error {
	absPath := resolveLlamafilePath(model, b.workspaceRoot)

	if b.Detect() {
		// Adopt the running server without launching a new process.
		detected := probeRunningLlamafileName(b.url)
		if detected != "" {
			b.activeModel = detected
		} else {
			b.activeModel = llamafileModelName(absPath)
		}
		return nil
	}

	proc, err := StartLlamafileService(absPath, b.url, "", b.startupTimeout, b.gpuLayers, b.ctxSize, out)
	if err != nil {
		return err
	}
	b.proc = proc
	b.running = true
	b.activeModel = llamafileModelName(absPath)
	_ = writePIDFile(b.agentsDir, BackendPID{
		Backend: "llamafile",
		PID:     proc.Pid,
		Model:   b.activeModel,
		URL:     b.url,
	})
	return nil
}

/** Stop sends SIGINT to the llamafile process Harvey started. Safe to call
 * when Harvey did not start the server (no-op) or when the process is gone.
 * Also deletes the PID file on success.
 *
 * Returns:
 *   error — non-nil only on unexpected kill failures.
 *
 * Example:
 *   defer b.Stop()
 */
func (b *LlamafileBackend) Stop() error {
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

/** ListModels scans modelsDir for *.llamafile (and *.llamafile.exe) files and
 * returns a backend-neutral ModelSummary for each one found.
 *
 * Returns:
 *   []ModelSummary — one entry per .llamafile file found in modelsDir.
 *   error          — non-nil only on unexpected filesystem errors.
 *
 * Example:
 *   models, err := b.ListModels()
 *   for _, m := range models { fmt.Println(m.Name, m.SizeBytes) }
 */
func (b *LlamafileBackend) ListModels() ([]ModelSummary, error) {
	paths := scanLlamafileModels(b.modelsDir)
	out := make([]ModelSummary, 0, len(paths))
	for _, p := range paths {
		s := ModelSummary{
			Name:   llamafileModelName(p),
			Path:   p,
			Engine: "llamafile",
		}
		if info, err := os.Stat(p); err == nil {
			s.SizeBytes = info.Size()
			s.Modified = info.ModTime()
		}
		out = append(out, s)
	}
	return out, nil
}

/** NewClient returns a wired LLMClient pointing at the running llamafile server
 * using the active model name. Returns an error when no active model has been set.
 *
 * Returns:
 *   LLMClient — ready for chat calls.
 *   error     — non-nil when ActiveModel() is "".
 *
 * Example:
 *   client, err := b.NewClient()
 */
func (b *LlamafileBackend) NewClient() (LLMClient, error) {
	if b.activeModel == "" {
		return nil, fmt.Errorf("llamafile NewClient: no active model set")
	}
	client := newLlamafileLLMClient(b.url+"/v1", b.activeModel, b.timeout)
	if b.maxTokens > 0 {
		client.SetMaxTokens(b.maxTokens)
	}
	return client, nil
}

/** StartedByHarvey reports whether Harvey launched the llamafile process in
 * this session. Returns false when the server was adopted from an external process.
 *
 * Returns:
 *   bool — true when Harvey started the llamafile server.
 *
 * Example:
 *   if b.StartedByHarvey() { defer b.Stop() }
 */
func (b *LlamafileBackend) StartedByHarvey() bool { return b.proc != nil }

// wireLlamafileBackend creates a new LlamafileBackend for proc (a process
// Harvey just started), sets its active model, and assigns it to a.Backend.
// Used by cmdLlamafileAdd, cmdLlamafileUse, cmdLlamafileStart, and the
// selectBackend startup path after a StartLlamafileService call.
func (a *Agent) wireLlamafileBackend(proc *os.Process, name string) {
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	lb := NewLlamafileBackend(a.Config, agentsDir, a.Workspace.Root)
	lb.proc = proc
	lb.activeModel = name
	lb.running = true
	a.Backend = lb
}

