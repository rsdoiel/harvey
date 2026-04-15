package harvey

import (
	"context"
	"io"
)

// Message represents a single chat message exchanged with a backend.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMClient is the interface implemented by each LLM backend.
type LLMClient interface {
	// Name returns a human-readable identifier shown in the UI.
	Name() string
	// Chat sends the full conversation history and streams the response to out.
	Chat(ctx context.Context, messages []Message, out io.Writer) error
	// Models lists models available on this backend.
	Models(ctx context.Context) ([]string, error)
	// Close releases any resources held by the client.
	Close() error
}

// Agent holds the state of an active Harvey session.
type Agent struct {
	Client   LLMClient
	Config   *Config
	History  []Message
	commands map[string]*Command
}

// NewAgent creates an Agent from cfg with an empty history.
func NewAgent(cfg *Config) *Agent {
	return &Agent{
		Config:   cfg,
		commands: make(map[string]*Command),
	}
}

// AddMessage appends a message to the conversation history.
func (a *Agent) AddMessage(role, content string) {
	a.History = append(a.History, Message{Role: role, Content: content})
}

// ClearHistory resets the conversation, preserving the system prompt if set.
func (a *Agent) ClearHistory() {
	if a.Config.SystemPrompt != "" {
		a.History = []Message{{Role: "system", Content: a.Config.SystemPrompt}}
	} else {
		a.History = nil
	}
}
