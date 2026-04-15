package harvey

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
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
	Message ollamaMsg `json:"message"`
	Done    bool      `json:"done"`
}

type ollamaTagsResp struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// Chat sends messages to Ollama and streams the response tokens to out.
func (o *OllamaClient) Chat(ctx context.Context, messages []Message, out io.Writer) error {
	msgs := make([]ollamaMsg, len(messages))
	for i, m := range messages {
		msgs[i] = ollamaMsg{Role: m.Role, Content: m.Content}
	}
	body, err := json.Marshal(ollamaChatReq{Model: o.model, Messages: msgs, Stream: true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama: HTTP %d: %s", resp.StatusCode, b)
	}

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var chunk ollamaChatResp
		if err := json.Unmarshal(sc.Bytes(), &chunk); err != nil {
			continue
		}
		fmt.Fprint(out, chunk.Message.Content)
		if chunk.Done {
			break
		}
	}
	return sc.Err()
}

// Models returns the names of models installed on the Ollama server.
func (o *OllamaClient) Models(ctx context.Context) ([]string, error) {
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
	names := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		names[i] = m.Name
	}
	return names, nil
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
