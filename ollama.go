package harvey

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// OllamaClient implements LLMClient for a local Ollama server.
type OllamaClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewOllamaClient returns an OllamaClient targeting the given base URL and model.
func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{}, // no global timeout; streaming responses run long
	}
}

func (o *OllamaClient) Name() string      { return "Ollama (" + o.model + ")" }
func (o *OllamaClient) Model() string     { return o.model }
func (o *OllamaClient) SetModel(m string) { o.model = m }
func (o *OllamaClient) Close() error      { return nil }

// ollamaMsg mirrors the message shape Ollama expects and returns.
type ollamaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatReq struct {
	Model    string      `json:"model"`
	Messages []ollamaMsg `json:"messages"`
	Stream   bool        `json:"stream"`
}

type ollamaChatResp struct {
	Message            ollamaMsg `json:"message"`
	Done               bool      `json:"done"`
	TotalDuration      int64     `json:"total_duration"`       // nanoseconds
	PromptEvalCount    int       `json:"prompt_eval_count"`    // tokens in prompt
	PromptEvalDuration int64     `json:"prompt_eval_duration"` // nanoseconds
	EvalCount          int       `json:"eval_count"`           // tokens generated
	EvalDuration       int64     `json:"eval_duration"`        // nanoseconds
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
}

// Chat sends messages to Ollama, streams the response tokens to out, and
// returns timing and token-count stats from the done packet.
func (o *OllamaClient) Chat(ctx context.Context, messages []Message, out io.Writer) (ChatStats, error) {
	msgs := make([]ollamaMsg, len(messages))
	for i, m := range messages {
		msgs[i] = ollamaMsg{Role: m.Role, Content: m.Content}
	}
	body, err := json.Marshal(ollamaChatReq{Model: o.model, Messages: msgs, Stream: true})
	if err != nil {
		return ChatStats{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ChatStats{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := o.http.Do(req)
	if err != nil {
		return ChatStats{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return ChatStats{}, fmt.Errorf("ollama: HTTP %d: %s", resp.StatusCode, b)
	}

	var stats ChatStats
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var chunk ollamaChatResp
		if err := json.Unmarshal(sc.Bytes(), &chunk); err != nil {
			continue
		}
		fmt.Fprint(out, chunk.Message.Content)
		if chunk.Done {
			stats.PromptTokens = chunk.PromptEvalCount
			stats.ReplyTokens = chunk.EvalCount
			if chunk.EvalDuration > 0 {
				stats.TokensPerSec = float64(chunk.EvalCount) / (float64(chunk.EvalDuration) / 1e9)
			}
			break
		}
	}
	stats.Elapsed = time.Since(start)
	return stats, sc.Err()
}

// Models returns the names of models installed on the Ollama server.
func (o *OllamaClient) Models(ctx context.Context) ([]string, error) {
	summaries, err := o.ModelSummaries(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(summaries))
	for i, s := range summaries {
		names[i] = s.Name
	}
	return names, nil
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
		Details    ollamaModelDetails     `json:"details"`
		Parameters string                 `json:"parameters"`
		Template   string                 `json:"template"`
		ModelInfo  map[string]interface{} `json:"model_info"`
		Size       int64                  `json:"size"`
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

/** LoadOllamaEnv reads ollama.env from the current working directory and applies
 * each KEY=VALUE line as an environment variable. If ollama.env does not exist,
 * it sets OLLAMA_MODELS to the current working directory so Ollama stores its
 * model files locally rather than in the default ~/.ollama location.
 *
 * Returns:
 *   error — if the working directory cannot be determined or a variable cannot be set.
 *
 * Example:
 *   if err := LoadOllamaEnv(); err != nil {
 *       log.Printf("warning: %v", err)
 *   }
 */
func LoadOllamaEnv() error {
	data, err := os.ReadFile("ollama.env")
	if os.IsNotExist(err) {
		cwd, werr := os.Getwd()
		if werr != nil {
			return werr
		}
		return os.Setenv("OLLAMA_MODELS", cwd)
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "OLLAMA_MODELS" {
			if abs, aerr := filepath.Abs(v); aerr == nil {
				v = abs
			}
		}
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return nil
}

// StartOllamaService launches "ollama serve" as a background process and waits
// up to 5 seconds for it to become reachable.
func StartOllamaService() error {
	cmd := exec.Command("ollama", "serve")
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
