package harvey

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PublicAIClient implements LLMClient for publicai.co.
// The API is assumed to be OpenAI-compatible: POST /v1/chat/completions
// with server-sent events (SSE) streaming.
type PublicAIClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewPublicAIClient returns a PublicAIClient for the given endpoint.
func NewPublicAIClient(baseURL, apiKey, model string) *PublicAIClient {
	return &PublicAIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{},
	}
}

func (p *PublicAIClient) Name() string  { return "publicai.co (" + p.model + ")" }
func (p *PublicAIClient) Close() error  { return nil }

func (p *PublicAIClient) Models(_ context.Context) ([]string, error) {
	return []string{p.model}, nil
}

type publicAIChatReq struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

type publicAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// Chat sends messages to publicai.co and streams the SSE response to out.
func (p *PublicAIClient) Chat(ctx context.Context, messages []Message, out io.Writer) error {
	req := publicAIChatReq{Model: p.model, Stream: true}
	for _, m := range messages {
		req.Messages = append(req.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: m.Content})
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.http.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("publicai: HTTP %d: %s", resp.StatusCode, b)
	}

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk publicAIChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, c := range chunk.Choices {
			fmt.Fprint(out, c.Delta.Content)
		}
	}
	return sc.Err()
}
