// Package harvey — backend_ollama.go implements ManagedBackend for Ollama.
package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

/** OllamaBackend implements ManagedBackend for the Ollama inference server.
 * It wraps the existing ProbeOllama and newOllamaLLMClient helpers and adds
 * lifecycle tracking (start/stop, PID file, StartedByHarvey flag).
 *
 * Fields:
 *   url             (string)        — Ollama base URL, e.g. "http://127.0.0.1:11434".
 *   timeout         (time.Duration) — HTTP timeout passed to the LLM client.
 *   agentsDir       (string)        — directory for the PID file (typically workspace agents/).
 *   activeModel     (string)        — model name chosen at session start; "" until set.
 *   running         (bool)          — reflects the last Detect() probe result.
 *   startedByHarvey (bool)          — true when this instance launched ollama serve.
 *   proc            (*os.Process)   — process Harvey started; nil if not started here.
 *
 * Example:
 *   b := NewOllamaBackend("http://127.0.0.1:11434", 30*time.Second, "/workspace/agents")
 *   if b.Detect() {
 *       client, _ := b.NewClient()
 *   }
 */
type OllamaBackend struct {
	url             string
	timeout         time.Duration
	agentsDir       string
	activeModel     string
	running         bool
	startedByHarvey bool
	proc            *os.Process
}

/** NewOllamaBackend returns a new OllamaBackend configured with the given URL,
 * HTTP timeout, and agents directory. No network calls are made.
 *
 * Parameters:
 *   url       (string)        — Ollama server base URL.
 *   timeout   (time.Duration) — HTTP timeout for LLM client calls.
 *   agentsDir (string)        — directory for the PID file.
 *
 * Returns:
 *   *OllamaBackend — ready to call Detect() or Start().
 *
 * Example:
 *   b := NewOllamaBackend(cfg.OllamaURL, cfg.OllamaTimeout, filepath.Join(ws.Root, "agents"))
 */
func NewOllamaBackend(url string, timeout time.Duration, agentsDir string) *OllamaBackend {
	return &OllamaBackend{
		url:       url,
		timeout:   timeout,
		agentsDir: agentsDir,
	}
}

/** Name returns the backend identifier "ollama".
 *
 * Returns:
 *   string — always "ollama".
 *
 * Example:
 *   fmt.Println(b.Name()) // "ollama"
 */
func (b *OllamaBackend) Name() string { return "ollama" }

/** BaseURL returns the HTTP base URL of the Ollama server.
 *
 * Returns:
 *   string — the URL passed to NewOllamaBackend.
 *
 * Example:
 *   fmt.Println(b.BaseURL()) // "http://127.0.0.1:11434"
 */
func (b *OllamaBackend) BaseURL() string { return b.url }

/** ActiveModel returns the name of the model currently in use, or "" when none is set.
 *
 * Returns:
 *   string — model name, or "" before Start or SetActiveModel is called.
 *
 * Example:
 *   fmt.Println(b.ActiveModel()) // "granite3.3:8b"
 */
func (b *OllamaBackend) ActiveModel() string { return b.activeModel }

/** SetActiveModel records the given model name as the active model without
 * making any network calls. Used by callers that wire the model name through
 * Config (e.g. pickOllamaModel) rather than through Start.
 *
 * Parameters:
 *   model (string) — Ollama model name, e.g. "granite3.3:8b".
 *
 * Example:
 *   b.SetActiveModel("granite3.3:8b")
 */
func (b *OllamaBackend) SetActiveModel(model string) { b.activeModel = model }

/** IsRunning reports the result of the most recent Detect probe. It does not
 * make a network call; call Detect() first to refresh.
 *
 * Returns:
 *   bool — true if the last Detect() probe succeeded.
 *
 * Example:
 *   b.Detect()
 *   if b.IsRunning() { ... }
 */
func (b *OllamaBackend) IsRunning() bool { return b.running }

/** Detect probes the Ollama server at BaseURL and updates IsRunning.
 *
 * Returns:
 *   bool — true if the server is reachable.
 *
 * Example:
 *   if b.Detect() { fmt.Println("Ollama is up") }
 */
func (b *OllamaBackend) Detect() bool {
	b.running = ProbeOllama(b.url)
	return b.running
}

/** Start launches "ollama serve" if it is not already running, waits up to 5 s
 * for it to become reachable, sets the active model, marks StartedByHarvey,
 * and writes a PID file to agentsDir. If the server is already running, Start
 * is a no-op (it does not set StartedByHarvey). The model argument is recorded
 * as the active model name; pass "" when the model is not yet known.
 *
 * Parameters:
 *   ctx   (context.Context) — controls the launch lifetime.
 *   model (string)          — model name to record as active; "" is allowed.
 *   out   (io.Writer)       — status messages sink.
 *
 * Returns:
 *   error — non-nil if ollama cannot be launched or does not respond.
 *
 * Example:
 *   err := b.Start(ctx, "granite3.3:8b", os.Stdout)
 */
func (b *OllamaBackend) Start(ctx context.Context, model string, out io.Writer) error {
	if b.Detect() {
		b.activeModel = model
		return nil
	}
	cmd := exec.CommandContext(ctx, "ollama", "serve")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ollama: could not launch: %w", err)
	}
	b.proc = cmd.Process
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		if ProbeOllama(b.url) {
			b.running = true
			b.startedByHarvey = true
			b.activeModel = model
			_ = writePIDFile(b.agentsDir, BackendPID{
				Backend: "ollama",
				PID:     cmd.Process.Pid,
				Model:   model,
				URL:     b.url,
			})
			return nil
		}
	}
	return fmt.Errorf("ollama started but did not respond within 5s")
}

/** Stop sends SIGINT to the ollama process Harvey started. Safe to call when
 * Harvey did not start Ollama (no-op) or when the process is already gone.
 * Also deletes the PID file on success.
 *
 * Returns:
 *   error — non-nil only on unexpected kill failures.
 *
 * Example:
 *   defer b.Stop()
 */
func (b *OllamaBackend) Stop() error {
	if !b.startedByHarvey || b.proc == nil {
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

/** ListModels returns all models installed on the Ollama server as backend-neutral
 * ModelSummary values. Returns an error when the server is not reachable.
 *
 * Returns:
 *   []ModelSummary — one entry per installed Ollama model.
 *   error          — non-nil if /api/tags cannot be reached.
 *
 * Example:
 *   models, err := b.ListModels()
 *   for _, m := range models { fmt.Println(m.Name, m.Engine) }
 */
func (b *OllamaBackend) ListModels() ([]ModelSummary, error) {
	client := NewOllamaClient(b.url, "")
	raw, err := client.ModelSummaries(context.Background())
	if err != nil {
		return nil, fmt.Errorf("ollama ListModels: %w", err)
	}
	out := make([]ModelSummary, len(raw))
	for i, r := range raw {
		out[i] = ModelSummary{
			Name:      r.Name,
			Engine:    "ollama",
			SizeBytes: r.SizeBytes,
		}
	}
	return out, nil
}

/** NewClient returns a wired LLMClient pointing at the running Ollama server
 * using the active model. Returns an error when no active model has been set.
 *
 * Returns:
 *   LLMClient — ready for chat and embed calls.
 *   error     — non-nil when ActiveModel() is "".
 *
 * Example:
 *   client, err := b.NewClient()
 */
func (b *OllamaBackend) NewClient() (LLMClient, error) {
	if b.activeModel == "" {
		return nil, fmt.Errorf("ollama NewClient: no active model set")
	}
	return newOllamaLLMClient(b.url, b.activeModel, b.timeout), nil
}

/** StartedByHarvey reports whether this Harvey session launched the Ollama server.
 * Returns false when Harvey adopted an already-running Ollama instance.
 *
 * Returns:
 *   bool — true when Harvey ran "ollama serve" through this backend.
 *
 * Example:
 *   if b.StartedByHarvey() { defer b.Stop() }
 */
func (b *OllamaBackend) StartedByHarvey() bool { return b.startedByHarvey }
