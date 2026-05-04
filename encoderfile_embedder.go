package harvey

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

/** EncoderfileEmbedder implements Embedder using an Encoderfile binary's
 * HTTP REST endpoint. The binary must be a model of type "embedding"
 * built with encoderfile. It is scoped to one Encoderfile server.
 *
 * Example:
 *   e := NewEncoderfileEmbedder("http://localhost:8080", "nomic-embed-text-v1_5")
 *   vec, err := e.Embed("hello world")
 */
type EncoderfileEmbedder struct {
	baseURL   string
	modelName string
	http      *http.Client
}

/** NewEncoderfileEmbedder returns an EncoderfileEmbedder targeting the
 * given Encoderfile server. modelName must match the model_id from the
 * server's GET /model response; use ProbeEncoderfileModel to fetch it.
 *
 * Parameters:
 *   baseURL   (string) — Encoderfile HTTP base URL, e.g. "http://localhost:8080".
 *   modelName (string) — model_id that identifies this embedding model.
 *
 * Returns:
 *   *EncoderfileEmbedder — ready to call Embed.
 *
 * Example:
 *   e := NewEncoderfileEmbedder("http://localhost:8080", "nomic-embed-text-v1_5")
 */
func NewEncoderfileEmbedder(baseURL, modelName string) *EncoderfileEmbedder {
	return &EncoderfileEmbedder{
		baseURL:   baseURL,
		modelName: modelName,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Name satisfies Embedder. Returns the model name used to identify this embedder.
func (e *EncoderfileEmbedder) Name() string { return e.modelName }

/** Embed sends text to the Encoderfile server's POST /predict endpoint and
 * returns the embedding vector for the first (CLS) token, which is the
 * standard sentence representation for BERT-style encoder models.
 *
 * Parameters:
 *   text (string) — text to embed.
 *
 * Returns:
 *   []float64 — embedding vector.
 *   error     — on transport failure, non-200 status, or empty response.
 *
 * Example:
 *   vec, err := e.Embed("The sky is blue")
 *   fmt.Printf("dims: %d\n", len(vec))
 */
func (e *EncoderfileEmbedder) Embed(text string) ([]float64, error) {
	type predictReq struct {
		Inputs    []string `json:"inputs"`
		Normalize bool     `json:"normalize"`
	}
	type tokenEmbedding struct {
		Embedding []float64 `json:"embedding"`
	}
	type sequenceResult struct {
		Embeddings []tokenEmbedding `json:"embeddings"`
	}
	type predictResp struct {
		Results []sequenceResult `json:"results"`
		ModelID string           `json:"model_id"`
	}

	body, err := json.Marshal(predictReq{Inputs: []string{text}, Normalize: true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, e.baseURL+"/predict", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("encoderfile embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("encoderfile embed: HTTP %d: %s", resp.StatusCode, b)
	}

	var pr predictResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("encoderfile embed: decode: %w", err)
	}
	if len(pr.Results) == 0 || len(pr.Results[0].Embeddings) == 0 {
		return nil, fmt.Errorf("encoderfile embed: empty response from %q", e.modelName)
	}
	vec := pr.Results[0].Embeddings[0].Embedding
	if len(vec) == 0 {
		return nil, fmt.Errorf("encoderfile embed: zero-length vector from %q", e.modelName)
	}
	return vec, nil
}

/** ProbeEncoderfile returns true if an Encoderfile server is reachable at
 * baseURL by calling GET /health.
 *
 * Parameters:
 *   baseURL (string) — Encoderfile HTTP base URL, e.g. "http://localhost:8080".
 *
 * Returns:
 *   bool — true when the server responds with HTTP 200.
 *
 * Example:
 *   if ProbeEncoderfile("http://localhost:8080") {
 *       fmt.Println("Encoderfile is running")
 *   }
 */
func ProbeEncoderfile(baseURL string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

/** ProbeEncoderfileModel queries the Encoderfile server's GET /model endpoint
 * and returns the model_id. Returns an error if the server is unreachable or
 * returns an unexpected response.
 *
 * Parameters:
 *   baseURL (string) — Encoderfile HTTP base URL.
 *
 * Returns:
 *   string — model_id from the server, e.g. "nomic-embed-text-v1_5".
 *   error  — on transport failure, non-200 status, or missing model_id.
 *
 * Example:
 *   id, err := ProbeEncoderfileModel("http://localhost:8080")
 *   fmt.Println(id) // "nomic-embed-text-v1_5"
 */
func ProbeEncoderfileModel(baseURL string) (string, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(baseURL + "/model")
	if err != nil {
		return "", fmt.Errorf("encoderfile: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("encoderfile: GET /model HTTP %d: %s", resp.StatusCode, b)
	}
	var meta struct {
		ModelID   string `json:"model_id"`
		ModelType string `json:"model_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", fmt.Errorf("encoderfile: decode model info: %w", err)
	}
	if meta.ModelID == "" {
		return "", fmt.Errorf("encoderfile: server returned empty model_id")
	}
	if meta.ModelType != "embedding" {
		return "", fmt.Errorf("encoderfile: model type is %q, want \"embedding\"", meta.ModelType)
	}
	return meta.ModelID, nil
}
