package harvey

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/** DebugLog writes structured JSONL diagnostic events to a per-session log
 * file. Every method is nil-safe: calling any method on a nil *DebugLog is
 * a no-op, so callers never need to guard with if statements.
 *
 * Each line written is a JSON object with at least a "time" field (RFC3339Nano)
 * and an "event" field identifying the event type. Additional fields vary by
 * event type. The file is safe for concurrent writes.
 *
 * Example:
 *   dl, err := OpenDebugLog("/path/to/agents/logs")
 *   if err != nil { log.Fatal(err) }
 *   defer dl.Close()
 *   dl.LogSessionStart("gemma4:e2b", "ollama", "/workspace", "0.0.3")
 */
type DebugLog struct {
	f    *os.File
	mu   sync.Mutex
	path string
}

/** OpenDebugLog creates agents/logs/harvey-TIMESTAMP.jsonl in logsDir.
 * The directory is created with 0755 permissions if it does not exist.
 *
 * Parameters:
 *   logsDir (string) — directory to write the log file into.
 *
 * Returns:
 *   *DebugLog — open log ready for writing.
 *   error     — on directory creation or file creation failure.
 *
 * Example:
 *   dl, err := OpenDebugLog(filepath.Join(workspace.Root, "agents", "logs"))
 */
func OpenDebugLog(logsDir string) (*DebugLog, error) {
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("debug log: mkdir %s: %w", logsDir, err)
	}
	ts := time.Now().Format("20060102-150405")
	path := filepath.Join(logsDir, "harvey-"+ts+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("debug log: create %s: %w", path, err)
	}
	return &DebugLog{f: f, path: path}, nil
}

/** OllamaLogPath returns the companion path for capturing Ollama subprocess
 * output, derived from the Harvey log filename by substituting the prefix.
 * Returns "" when called on a nil receiver.
 *
 * Example:
 *   // dl.path == "agents/logs/harvey-20260514-094620.jsonl"
 *   // dl.OllamaLogPath() == "agents/logs/ollama-20260514-094620.log"
 */
func (d *DebugLog) OllamaLogPath() string {
	if d == nil {
		return ""
	}
	dir := filepath.Dir(d.path)
	base := filepath.Base(d.path)
	ts := strings.TrimPrefix(strings.TrimSuffix(base, ".jsonl"), "harvey-")
	return filepath.Join(dir, "ollama-"+ts+".log")
}

/** Path returns the absolute path of the log file, or "" for nil receiver. */
func (d *DebugLog) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

/** Close flushes and closes the underlying file. Safe to call on nil. */
func (d *DebugLog) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.f.Close()
}

// Log writes a single JSONL line. fields must include "event".
// The "time" key is always overwritten with the current UTC time.
func (d *DebugLog) Log(fields map[string]any) {
	if d == nil {
		return
	}
	fields["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(fields)
	if err != nil {
		return
	}
	d.mu.Lock()
	_, _ = d.f.Write(b)
	_, _ = d.f.Write([]byte("\n"))
	d.mu.Unlock()
}

/** LogSessionStart records Harvey startup information.
 *
 * Parameters:
 *   modelID      (string) — bare model identifier, e.g. "gemma4:e2b".
 *   modelDisplay (string) — full display name, e.g. "ollama (gemma4:e2b)".
 *   provider     (string) — provider name, e.g. "ollama".
 *   workspace    (string) — absolute workspace root path.
 *   version      (string) — Harvey version string.
 */
func (d *DebugLog) LogSessionStart(modelID, modelDisplay, provider, workspace, version string) {
	d.Log(map[string]any{
		"event":         "session_start",
		"model_id":      modelID,
		"model_display": modelDisplay,
		"provider":      provider,
		"workspace":     workspace,
		"version":       version,
	})
}

/** LogLLMRequest records outgoing LLM request parameters. */
func (d *DebugLog) LogLLMRequest(model string, messageCount, toolCount int) {
	d.Log(map[string]any{
		"event":    "llm_request",
		"model":    model,
		"messages": messageCount,
		"tools":    toolCount,
	})
}

/** LogLLMResponse records the outcome of an LLM call. */
func (d *DebugLog) LogLLMResponse(stats ChatStats, toolCallCount int) {
	d.Log(map[string]any{
		"event":          "llm_response",
		"prompt_tokens":  stats.PromptTokens,
		"reply_tokens":   stats.ReplyTokens,
		"elapsed_ms":     stats.Elapsed.Milliseconds(),
		"tokens_per_sec": math.Round(stats.TokensPerSec*100) / 100,
		"tool_calls":     toolCallCount,
	})
}

/** LogRAGInject records a RAG context augmentation event.
 *
 * Parameters:
 *   store      (string)  — name of the active RAG store.
 *   query      (string)  — the user prompt used as the retrieval query.
 *   chunkCount (int)     — number of chunks injected above the score threshold.
 *   topScore   (float64) — cosine similarity of the best matching chunk.
 */
func (d *DebugLog) LogRAGInject(store, query string, chunkCount int, topScore float64) {
	d.Log(map[string]any{
		"event":       "rag_inject",
		"store":       store,
		"query":       query,
		"chunk_count": chunkCount,
		"top_score":   topScore,
	})
}

/** LogToolCall records a single tool invocation.
 *
 * Parameters:
 *   name        (string)        — tool name.
 *   outputBytes (int)           — byte length of the tool's output.
 *   elapsed     (time.Duration) — wall time for the call.
 *   callErr     (string)        — error message, or "" on success.
 */
func (d *DebugLog) LogToolCall(name string, outputBytes int, elapsed time.Duration, callErr string) {
	fields := map[string]any{
		"event":        "tool_call",
		"name":         name,
		"output_bytes": outputBytes,
		"elapsed_ms":   elapsed.Milliseconds(),
	}
	if callErr != "" {
		fields["error"] = callErr
	}
	d.Log(fields)
}

/** LogSkillDispatch records a skill execution event. */
func (d *DebugLog) LogSkillDispatch(name, trigger string) {
	d.Log(map[string]any{
		"event":   "skill_dispatch",
		"name":    name,
		"trigger": trigger,
	})
}

/** LogOllamaStart records when Harvey launches the Ollama subprocess. */
func (d *DebugLog) LogOllamaStart(debugMode bool, logPath string) {
	d.Log(map[string]any{
		"event":    "ollama_start",
		"debug":    debugMode,
		"log_path": logPath,
	})
}

/** LogError records a non-fatal diagnostic error from a named source. */
func (d *DebugLog) LogError(source, message string) {
	d.Log(map[string]any{
		"event":   "error",
		"source":  source,
		"message": message,
	})
}
