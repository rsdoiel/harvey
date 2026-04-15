package harvey

import (
	"context"
	"io"
)

/** Message represents a single chat message exchanged with a backend.
 *
 * Fields:
 *   Role    (string) — "system", "user", or "assistant".
 *   Content (string) — the message body.
 *
 * Example:
 *   msg := Message{Role: "user", Content: "Hello!"}
 */
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

/** LLMClient is the interface implemented by each LLM backend (Ollama,
 * publicai.co, etc.).
 *
 * Methods:
 *   Name()   string                                    — human-readable backend identifier.
 *   Chat()   func(ctx, messages, out) error            — send history, stream reply to out.
 *   Models() func(ctx) ([]string, error)               — list available models.
 *   Close()  error                                     — release held resources.
 *
 * Example:
 *   var c LLMClient = NewOllamaClient("http://localhost:11434", "llama3")
 *   err := c.Chat(ctx, history, os.Stdout)
 */
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

/** Agent holds the state of an active Harvey session, including the LLM
 * backend, conversation history, workspace, knowledge base, and registered
 * slash commands.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   ws, _ := NewWorkspace(".")
 *   agent := NewAgent(cfg, ws)
 */
type Agent struct {
	Client    LLMClient
	Config    *Config
	History   []Message
	Workspace *Workspace
	KB        *KnowledgeBase
	commands  map[string]*Command
}

/** NewAgent creates an Agent from cfg and ws with an empty conversation
 * history. The knowledge base is opened lazily — it is nil if
 * OpenKnowledgeBase has not been called.
 *
 * Parameters:
 *   cfg (*Config)    — runtime configuration.
 *   ws  (*Workspace) — workspace that anchors all file I/O.
 *
 * Returns:
 *   *Agent — initialised agent ready for Run().
 *
 * Example:
 *   ws, _ := NewWorkspace(".")
 *   agent := NewAgent(DefaultConfig(), ws)
 */
func NewAgent(cfg *Config, ws *Workspace) *Agent {
	return &Agent{
		Config:    cfg,
		Workspace: ws,
		commands:  make(map[string]*Command),
	}
}

/** AddMessage appends a message to the conversation history.
 *
 * Parameters:
 *   role    (string) — "system", "user", or "assistant".
 *   content (string) — message body.
 *
 * Example:
 *   agent.AddMessage("user", "What is the capital of France?")
 */
func (a *Agent) AddMessage(role, content string) {
	a.History = append(a.History, Message{Role: role, Content: content})
}

/** ClearHistory resets the conversation, re-injecting the system prompt if
 * one is configured.
 *
 * Example:
 *   agent.ClearHistory()
 */
func (a *Agent) ClearHistory() {
	if a.Config.SystemPrompt != "" {
		a.History = []Message{{Role: "system", Content: a.Config.SystemPrompt}}
	} else {
		a.History = nil
	}
}
