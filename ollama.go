// Package harvey — ollama.go provides Ollama-specific client utilities for
// model inspection, capability probing, and embedding. This file complements
// the LLMClient interface (which handles chat/embedding via any-llm-go) with
// Ollama-specific features:
//
//   - ModelSummaries: List all installed models with metadata
//   - ShowModel: Get detailed info for a single model
//   - CountTokens: Estimate token count via /api/tokenize
//   - FastProbeModel/ThoroughProbeModel: Determine model capabilities
//   - OllamaEmbedder: Embedding via /api/embed endpoint
//   - ProbeOllama: Check if Ollama server is reachable
//   - StartOllamaService: Launch ollama serve as background process
//
// The OllamaClient type is separate from the LLMClient interface to keep
// Ollama-specific logic isolated. For chat/embedding operations, use
// newOllamaLLMClient (in a different file) which implements LLMClient.

package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// OllamaClient is a utility type for Ollama-specific operations (model
// inspection, capability probing) that are not part of the LLMClient
// interface. For chat and embeddings use newOllamaLLMClient instead.
type OllamaClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewOllamaClient returns an OllamaClient for Ollama-specific utility calls
// (ShowModel, ModelSummaries, probing). Pass model="" when only inspecting.
func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{},
	}
}

type ollamaModelDetails struct {
	Family        string `json:"family"`
	ParameterSize string `json:"parameter_size"`
	Quantization  string `json:"quantization_level"`
	Format        string `json:"format"`
}

type ollamaTagsEntry struct {
	Name    string             `json:"name"`
	Size    int64              `json:"size"`
	Details ollamaModelDetails `json:"details"`
}

type ollamaTagsResp struct {
	Models []ollamaTagsEntry `json:"models"`
}

/** ModelSummary holds the key properties of an installed Ollama model.
 *
 * Fields:
 *   Name          (string) — model identifier, e.g. "llama3.1:8b".
 *   Family        (string) — model family, e.g. "llama", "phi", "mistral".
 *   ParameterSize (string) — human-readable parameter count, e.g. "8.0B".
 *   Quantization  (string) — quantization level, e.g. "Q4_K_M".
 *   SizeBytes     (int64)  — bytes consumed on disk.
 *   Running       (bool)   — true when the model is currently loaded by Ollama.
 *
 * Example:
 *   summaries, _ := client.ModelSummaries(ctx)
 *   for _, s := range summaries { fmt.Println(s.Name) }
 */
type ModelSummary struct {
	Name          string
	Family        string
	ParameterSize string
	Quantization  string
	SizeBytes     int64
	Running       bool
}

/** ModelDetail holds the full inspection output for a single Ollama model,
 * extending ModelSummary with context-window size and Modelfile parameters.
 *
 * Fields:
 *   ModelSummary   — embedded summary fields.
 *   ContextLength  (int)    — context window in tokens (0 if not reported).
 *   RawParameters  (string) — raw parameter block from the Modelfile.
 *   Template       (string) — chat template string.
 *
 * Example:
 *   detail, _ := client.ShowModel(ctx, "llama3.1:8b")
 *   fmt.Printf("context: %d tokens\n", detail.ContextLength)
 */
type ModelDetail struct {
	ModelSummary
	ContextLength int
	RawParameters string
	Template      string
	Capabilities  []string // e.g. ["completion", "tools", "vision"]; nil on older Ollama
}


/** ModelSummaries returns a ModelSummary for every model installed on the
 * Ollama server. It also marks which models are currently loaded (running)
 * by querying /api/ps.
 *
 * Parameters:
 *   ctx (context.Context) — controls the HTTP request lifetime.
 *
 * Returns:
 *   []ModelSummary — one entry per installed model, sorted as Ollama returns them.
 *   error          — non-nil if the /api/tags request fails.
 *
 * Example:
 *   summaries, err := client.ModelSummaries(ctx)
 */
func (o *OllamaClient) ModelSummaries(ctx context.Context) ([]ModelSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tags ollamaTagsResp
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}

	// Collect names of currently running models (best-effort; ignore errors).
	running := map[string]bool{}
	if psReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/ps", nil); err == nil {
		if psResp, err := o.http.Do(psReq); err == nil {
			defer psResp.Body.Close()
			var ps ollamaTagsResp
			if json.NewDecoder(psResp.Body).Decode(&ps) == nil {
				for _, m := range ps.Models {
					running[m.Name] = true
				}
			}
		}
	}

	summaries := make([]ModelSummary, len(tags.Models))
	for i, m := range tags.Models {
		summaries[i] = ModelSummary{
			Name:          m.Name,
			Family:        m.Details.Family,
			ParameterSize: m.Details.ParameterSize,
			Quantization:  m.Details.Quantization,
			SizeBytes:     m.Size,
			Running:       running[m.Name],
		}
	}
	return summaries, nil
}

/** ShowModel fetches the full detail for a single installed model by calling
 * /api/show. The context window length is extracted from the model_info
 * metadata by searching for any key ending in ".context_length".
 *
 * Parameters:
 *   ctx   (context.Context) — controls the HTTP request lifetime.
 *   model (string)          — model name, e.g. "llama3.1:8b".
 *
 * Returns:
 *   ModelDetail — full model detail; zero value on error.
 *   error       — non-nil if the request fails or the model is not found.
 *
 * Example:
 *   detail, err := client.ShowModel(ctx, "llama3.1:8b")
 *   fmt.Printf("context: %d tokens\n", detail.ContextLength)
 */
func (o *OllamaClient) ShowModel(ctx context.Context, model string) (ModelDetail, error) {
	type showReq struct {
		Model   string `json:"model"`
		Verbose bool   `json:"verbose"`
	}
	type showResp struct {
		Details      ollamaModelDetails     `json:"details"`
		Parameters   string                 `json:"parameters"`
		Template     string                 `json:"template"`
		ModelInfo    map[string]interface{} `json:"model_info"`
		Size         int64                  `json:"size"`
		Capabilities []string               `json:"capabilities"`
	}
	body, _ := json.Marshal(showReq{Model: model, Verbose: true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return ModelDetail{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return ModelDetail{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return ModelDetail{}, fmt.Errorf("ollama show: HTTP %d: %s", resp.StatusCode, b)
	}
	var sr showResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return ModelDetail{}, err
	}

	// Extract context length — key is "<architecture>.context_length".
	var ctxLen int
	for k, v := range sr.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			switch n := v.(type) {
			case float64:
				ctxLen = int(n)
			}
			break
		}
	}

	return ModelDetail{
		ModelSummary: ModelSummary{
			Name:          model,
			Family:        sr.Details.Family,
			ParameterSize: sr.Details.ParameterSize,
			Quantization:  sr.Details.Quantization,
			SizeBytes:     sr.Size,
		},
		ContextLength: ctxLen,
		RawParameters: sr.Parameters,
		Template:      sr.Template,
		Capabilities:  sr.Capabilities,
	}, nil
}

/** CountTokens estimates the number of tokens in text for the named model.
 * It first tries the Ollama /api/tokenize endpoint; if that is unavailable
 * or fails, it falls back to a character-based heuristic (len(text)/4).
 *
 * Parameters:
 *   ctx     (context.Context) — controls the HTTP request lifetime.
 *   baseURL (string)          — Ollama server base URL.
 *   model   (string)          — model name (sent with the tokenize request).
 *   text    (string)          — text to count.
 *
 * Returns:
 *   count (int)  — number of tokens.
 *   exact (bool) — true when the count came from Ollama, false for the heuristic.
 *
 * Example:
 *   n, exact := CountTokens(ctx, "http://localhost:11434", "llama3", prompt)
 *   fmt.Printf("%d tokens (exact=%v)\n", n, exact)
 */
func CountTokens(ctx context.Context, baseURL, model, text string) (count int, exact bool) {
	type tokenizeReq struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	type tokenizeResp struct {
		Tokens []int `json:"tokens"`
	}
	body, err := json.Marshal(tokenizeReq{Model: model, Prompt: text})
	if err == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/tokenize", bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			c := &http.Client{Timeout: 5 * time.Second}
			resp, err := c.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var r tokenizeResp
					if json.NewDecoder(resp.Body).Decode(&r) == nil {
						return len(r.Tokens), true
					}
				}
			}
		}
	}
	// Heuristic: ~4 UTF-8 bytes per token; minimum 1.
	n := len(text) / 4
	if n < 1 {
		n = 1
	}
	return n, false
}

/** HistoryText concatenates every message's content with a newline separator.
 * Used to produce a single string for token-count estimation over the full
 * conversation history.
 *
 * Parameters:
 *   messages ([]Message) — conversation history.
 *
 * Returns:
 *   string — all message bodies joined by newlines.
 *
 * Example:
 *   text := HistoryText(agent.History)
 *   n, _ := CountTokens(ctx, baseURL, model, text)
 */
func HistoryText(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Content)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ProbeOllama returns true if an Ollama server is reachable at baseURL.
func ProbeOllama(baseURL string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(baseURL + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

/** PrintOllamaEnv writes the currently active Ollama environment variables to
 * out, skipping any that are not set. It never modifies the environment.
 *
 * Parameters:
 *   out (io.Writer) — destination for the variable listing.
 *
 * Example:
 *   PrintOllamaEnv(os.Stdout)
 */
func PrintOllamaEnv(out io.Writer) {
	vars := []string{
		"OLLAMA_HOST",
		"OLLAMA_MODELS",
		"OLLAMA_KEEP_ALIVE",
		"OLLAMA_NUM_THREAD",
		"OLLAMA_NUM_PARALLEL",
		"OLLAMA_MAX_LOADED_MODELS",
		"OLLAMA_CONTEXT_LENGTH",
		"OLLAMA_MAX_QUEUE",
		"OLLAMA_FLASH_ATTENTION",
		"OLLAMA_DEBUG",
	}
	for _, k := range vars {
		if v := os.Getenv(k); v != "" {
			fmt.Fprintf(out, "  %-28s %s\n", k, v)
		}
	}
}

// StartOllamaService launches "ollama serve" as a background process and waits
// up to 5 seconds for it to become reachable. OLLAMA_DEBUG is inherited from
// Harvey's process environment (set via --debug at startup). When ollamaLogPath
// is non-empty, subprocess stdout and stderr are redirected to that file so debug
// output is captured rather than discarded.
func StartOllamaService(ollamaLogPath string) error {
	cmd := exec.Command("ollama", "serve")
	if ollamaLogPath != "" {
		f, err := os.Create(ollamaLogPath)
		if err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
			// The subprocess inherits its own fd; safe to close the parent's copy.
			defer f.Close()
		}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not launch ollama: %w", err)
	}
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		if ProbeOllama("http://localhost:11434") {
			return nil
		}
	}
	return fmt.Errorf("ollama started but did not respond within 5s")
}

// embedKeywords are substrings that strongly indicate a dedicated embedding model.
var embedKeywords = []string{
	"embed", "e5-", "bge-", "gte-", "minilm", "nomic", "mxbai", "jina",
}

// toolMarkers are template substrings that indicate tool/function-calling support.
// They cover the major model families: Llama 3.x (Jinja2), Mistral, Qwen, Granite, Gemma.
var toolMarkers = []string{
	"{% if tools %}", "{%- if tools %}", // Llama 3, Granite (Jinja2)
	"[TOOL_CALLS]", "[AVAILABLE_TOOLS]", // Mistral, Ministral
	"<tool_call>", "✿FUNCTION✿",         // Qwen 2.x variants
	"<function_calls>",                  // Gemma 4 and others
}

// hasEmbedKeyword reports whether name contains a known embedding-model substring.
func hasEmbedKeyword(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range embedKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// hasToolMarker reports whether template contains a known tool-call marker.
func hasToolMarker(template string) bool {
	for _, m := range toolMarkers {
		if strings.Contains(template, m) {
			return true
		}
	}
	return false
}

// capabilitiesContain reports whether caps includes the named capability string.
func capabilitiesContain(caps []string, name string) bool {
	for _, c := range caps {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

/** FastProbeModel calls /api/show for the named model and applies heuristics
 * to determine tool and embedding capability. It does not make additional
 * network calls beyond the single /api/show request.
 *
 * Tool support is determined by:
 *   1. The Capabilities array from /api/show (authoritative on Ollama >= 0.3).
 *   2. Known tool-call template markers as a fallback for older Ollama.
 *
 * Embedding support is determined by:
 *   1. A known embedding-model keyword in the model name.
 *   2. An empty or absent Template field combined with the keyword heuristic.
 *   Both signals must agree to avoid false positives from base chat models
 *   that happen to have a minimal template.
 *
 * Parameters:
 *   ctx     (context.Context) — controls the HTTP request lifetime.
 *   baseURL (string)          — Ollama server base URL, e.g. "http://localhost:11434".
 *   name    (string)          — full model name, e.g. "llama3.2:latest".
 *
 * Returns:
 *   *ModelCapability — populated capability record with ProbeLevel "fast".
 *   error            — non-nil if /api/show fails or the model is not found.
 *
 * Example:
 *   cap, err := FastProbeModel(ctx, "http://localhost:11434", "llama3.2:latest")
 *   fmt.Printf("tools: %s  embed: %s\n", cap.SupportsTools, cap.SupportsEmbed)
 */
func FastProbeModel(ctx context.Context, baseURL, name string) (*ModelCapability, error) {
	c := NewOllamaClient(baseURL, name)
	detail, err := c.ShowModel(ctx, name)
	if err != nil {
		return nil, err
	}

	cap := &ModelCapability{
		Name:          name,
		Family:        detail.Family,
		ParameterSize: detail.ParameterSize,
		Quantization:  detail.Quantization,
		SizeBytes:     detail.SizeBytes,
		ContextLength: detail.ContextLength,
		SupportsTools: CapUnknown,
		SupportsEmbed: CapUnknown,
		ProbeLevel:    "fast",
		ProbedAt:      time.Now(),
	}

	// Tool support: prefer the capabilities array; fall back to template markers.
	if len(detail.Capabilities) > 0 {
		if capabilitiesContain(detail.Capabilities, "tools") {
			cap.SupportsTools = CapYes
		} else {
			cap.SupportsTools = CapNo
		}
	} else if detail.Template != "" {
		if hasToolMarker(detail.Template) {
			cap.SupportsTools = CapYes
		} else {
			cap.SupportsTools = CapNo
		}
	}

	// Embedding support: keyword in name is the primary signal.
	// An empty template without a keyword match is ambiguous (could be a
	// base model), so we require the keyword for a fast-probe CapYes.
	if hasEmbedKeyword(name) {
		cap.SupportsEmbed = CapYes
	} else {
		cap.SupportsEmbed = CapNo
	}

	return cap, nil
}

/** ThoroughProbeModel runs FastProbeModel and then makes a live /api/embed
 * request to confirm or deny embedding support definitively. A successful
 * response with a non-empty embeddings array sets SupportsEmbed = CapYes;
 * any error or empty response sets it to CapNo. Tool support is not re-tested
 * beyond what FastProbeModel determines — the capabilities API is authoritative.
 *
 * Parameters:
 *   ctx     (context.Context) — controls the HTTP request lifetime.
 *   baseURL (string)          — Ollama server base URL.
 *   name    (string)          — full model name.
 *
 * Returns:
 *   *ModelCapability — populated capability record with ProbeLevel "thorough".
 *   error            — non-nil if FastProbeModel fails; embed test errors are
 *                      absorbed into SupportsEmbed = CapNo rather than returned.
 *
 * Example:
 *   cap, err := ThoroughProbeModel(ctx, "http://localhost:11434", "nomic-embed-text")
 *   fmt.Printf("embed: %s\n", cap.SupportsEmbed)
 */
func ThoroughProbeModel(ctx context.Context, baseURL, name string) (*ModelCapability, error) {
	cap, err := FastProbeModel(ctx, baseURL, name)
	if err != nil {
		return nil, err
	}
	cap.ProbeLevel = "thorough"
	cap.ProbedAt = time.Now()

	hc := &http.Client{Timeout: 30 * time.Second}

	// ── Embedding probe ──────────────────────────────────────────────────────
	type embedReq struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	type embedResp struct {
		Embeddings [][]float64 `json:"embeddings"`
	}

	body, _ := json.Marshal(embedReq{Model: name, Input: "test"})
	req, err2 := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/embed", bytes.NewReader(body))
	if err2 != nil {
		cap.SupportsEmbed = CapNo
	} else {
		req.Header.Set("Content-Type", "application/json")
		resp, err2 := hc.Do(req)
		if err2 != nil || resp.StatusCode != http.StatusOK {
			cap.SupportsEmbed = CapNo
		} else {
			var er embedResp
			if json.NewDecoder(resp.Body).Decode(&er) != nil || len(er.Embeddings) == 0 || len(er.Embeddings[0]) == 0 {
				cap.SupportsEmbed = CapNo
			} else {
				cap.SupportsEmbed = CapYes
			}
			resp.Body.Close()
		}
	}

	// Skip tagged-block probe for embedding-only models — they don't do chat.
	if cap.SupportsEmbed == CapYes && cap.SupportsTools == CapNo {
		return cap, nil
	}

	// ── Tagged-block probe ───────────────────────────────────────────────────
	// Send a minimal chat request and check whether the model produces a
	// path-tagged code block (the format Harvey's auto-execute relies on).
	// Use a generous timeout: local inference can be slow on modest hardware.
	type generateReq struct {
		Model  string `json:"model"`
		System string `json:"system"`
		Prompt string `json:"prompt"`
		Stream bool   `json:"stream"`
	}
	type generateResp struct {
		Response string `json:"response"`
	}

	probeReq := generateReq{
		Model:  name,
		System: "When writing code, always tag the fenced code block with its target filename. Use the format: ```go path/to/file.go",
		Prompt: "Write a minimal Go function that returns the integer 42. Put it in probe.go",
		Stream: false,
	}
	probeBody, _ := json.Marshal(probeReq)
	probeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req2, err3 := http.NewRequestWithContext(probeCtx, http.MethodPost, baseURL+"/api/generate", bytes.NewReader(probeBody))
	if err3 != nil {
		// Leave SupportsTaggedBlocks as CapUnknown.
		return cap, nil
	}
	req2.Header.Set("Content-Type", "application/json")
	hc2 := &http.Client{} // no client-level timeout; rely on context
	resp2, err3 := hc2.Do(req2)
	if err3 != nil || resp2.StatusCode != http.StatusOK {
		// Timeout or server error — leave as CapUnknown rather than CapNo.
		return cap, nil
	}
	defer resp2.Body.Close()
	var gr generateResp
	if json.NewDecoder(resp2.Body).Decode(&gr) != nil {
		return cap, nil
	}
	if len(findTaggedBlocks(gr.Response)) > 0 {
		cap.SupportsTaggedBlocks = CapYes
	} else {
		cap.SupportsTaggedBlocks = CapNo
	}
	return cap, nil
}

// ─── OllamaEmbedder ──────────────────────────────────────────────────────────

/** OllamaEmbedder implements Embedder using Ollama's /api/embed endpoint.
 * It is scoped to a single embedding model on one Ollama server.
 *
 * Example:
 *   e := NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")
 *   vec, err := e.Embed("The sky is blue")
 */
type OllamaEmbedder struct {
	baseURL string
	model   string
	http    *http.Client
}

/** NewOllamaEmbedder returns an OllamaEmbedder targeting the given Ollama
 * server and embedding model.
 *
 * Parameters:
 *   baseURL (string) — Ollama server base URL, e.g. "http://localhost:11434".
 *   model   (string) — embedding model name, e.g. "nomic-embed-text".
 *
 * Returns:
 *   *OllamaEmbedder — ready to call Embed.
 *
 * Example:
 *   e := NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")
 */
func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

/** Name returns the embedding model name, satisfying the Embedder interface.
 *
 * Returns:
 *   string — model name, e.g. "nomic-embed-text".
 *
 * Example:
 *   fmt.Println(e.Name()) // "nomic-embed-text"
 */
func (e *OllamaEmbedder) Name() string { return e.model }

/** Embed sends text to Ollama's /api/embed endpoint and returns the embedding
 * vector. Returns an error if the server is unreachable, returns a non-200
 * status, or returns no embeddings.
 *
 * Parameters:
 *   ctx  (context.Context) — controls the HTTP request lifetime.
 *   text (string)          — text to embed.
 *
 * Returns:
 *   []float64 — embedding vector from the model.
 *   error     — on transport failure, HTTP error, or empty response.
 *
 * Example:
 *   vec, err := e.Embed(ctx, "The sky is blue")
 *   fmt.Printf("dims: %d\n", len(vec))
 */
func (e *OllamaEmbedder) Embed(text string) ([]float64, error) {
	type embedReq struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	type embedResp struct {
		Embeddings [][]float64 `json:"embeddings"`
	}

	body, err := json.Marshal(embedReq{Model: e.model, Input: text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed: HTTP %d: %s", resp.StatusCode, b)
	}

	var er embedResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("ollama embed: decode: %w", err)
	}
	if len(er.Embeddings) == 0 || len(er.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama embed: empty embeddings from model %q", e.model)
	}
	return er.Embeddings[0], nil
}
