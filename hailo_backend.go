package harvey

import (
	"context"
	"io"
	"time"
)

/** HailoBackend is the ManagedBackend for hailo-ollama, the Ollama-shaped
 * server for the Raspberry Pi AI HAT+ 2. It is an engine label over the
 * Ollama client, not a fourth client (DR-0030): it embeds OllamaBackend, so
 * chat, listing and detection are Ollama's, and only the name, the engine
 * label on listed models, and the lifecycle differ. Harvey never starts or
 * stops hailo-ollama.
 *
 * Example:
 *   b := NewHailoBackend("http://localhost:8000", 0, "/workspace/agents")
 *   b.SetActiveModel("llama3.2:3b")
 *   client, _ := b.NewClient()
 */
type HailoBackend struct {
	*OllamaBackend
}

/** NewHailoBackend returns a HailoBackend for the hailo-ollama server at url.
 * No network calls are made.
 *
 * Parameters:
 *   url       (string)        — hailo-ollama base URL, e.g. "http://localhost:8000".
 *   timeout   (time.Duration) — HTTP timeout for LLM client calls.
 *   agentsDir (string)        — workspace agents directory.
 *
 * Returns:
 *   *HailoBackend — ready to call Detect() or NewClient().
 *
 * Example:
 *   b := NewHailoBackend(cfg.Hailo.URL, cfg.Ollama.Timeout, filepath.Join(ws.Root, "agents"))
 */
func NewHailoBackend(url string, timeout time.Duration, agentsDir string) *HailoBackend {
	return &HailoBackend{NewOllamaBackend(url, timeout, agentsDir)}
}

/** Name returns the engine label "hailo".
 *
 * Returns:
 *   string — always "hailo".
 *
 * Example:
 *   fmt.Println(b.Name()) // "hailo"
 */
func (b *HailoBackend) Name() string { return "hailo" }

/** Start does not start anything: Harvey does not launch hailo-ollama. It
 * returns an unavailable error that names the command to run.
 *
 * Parameters:
 *   ctx   (context.Context) — unused.
 *   model (string)          — unused.
 *   out   (io.Writer)       — unused.
 *
 * Returns:
 *   error — always non-nil, class unavailable.
 *
 * Example:
 *   err := b.Start(ctx, "", os.Stdout) // "hailo-ollama is not running; start it with: systemctl --user start hailo-ollama"
 */
func (b *HailoBackend) Start(_ context.Context, _ string, _ io.Writer) error {
	return Unavailablef("hailo-ollama is not running at %s; start it with: systemctl --user start hailo-ollama", b.url)
}

/** Stop does nothing: Harvey never owns the hailo-ollama process, and killing
 * it mid-request wedges the card.
 *
 * Returns:
 *   error — always nil.
 *
 * Example:
 *   _ = b.Stop()
 */
func (b *HailoBackend) Stop() error { return nil }

/** ListModels returns the models pulled on the hailo-ollama server, labelled
 * with the engine "hailo".
 *
 * Returns:
 *   []ModelSummary — one entry per pulled model.
 *   error          — non-nil if /api/tags cannot be reached.
 *
 * Example:
 *   models, err := b.ListModels()
 */
func (b *HailoBackend) ListModels() ([]ModelSummary, error) {
	models, err := b.OllamaBackend.ListModels()
	for i := range models {
		models[i].Engine = "hailo"
	}
	return models, err
}

var _ ManagedBackend = (*HailoBackend)(nil)
