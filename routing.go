package harvey

import (
	"context"
	"strings"
)

// routingSystemPrompt is sent as the system message in every routing call.
// The small model must reply with either a direct answer or exactly "ROUTE:full".
// Tuned in Session 4.
const routingSystemPrompt = `You are a routing assistant for a coding agent.
Read the conversation and the latest user message, then do exactly one of:

1. Answer directly — for simple, factual, conversational, or short requests
   that you can handle well as a small model.

2. Reply with exactly "ROUTE:full" and nothing else — for requests that need
   deep reasoning, long code generation, architecture decisions, complex
   debugging, or detailed analysis beyond your capability.

Lean toward answering directly. Only route when the task clearly exceeds your
ability. Do not explain your routing decision.`

/** RouterConfig holds the two Ollama model names used for routing.
 *
 * Fields:
 *   FastModel (string) — small, quick model that classifies and answers simple prompts.
 *   FullModel (string) — larger model for complex prompts that the fast model routes away.
 *
 * Example:
 *   cfg := RouterConfig{FastModel: "llama3.2:1b", FullModel: "llama3.1:8b"}
 */
type RouterConfig struct {
	FastModel string
	FullModel string
}

/** Router sends each prompt to a small fast model, which either answers
 * directly or emits "ROUTE:full" to signal that the full model should handle it.
 * The trimmed history sent to the fast model is capped at 25% of its context
 * window to leave room for its reply and avoid overwhelming a small model.
 *
 * Example:
 *   r, err := NewRouter(RouterConfig{FastModel: "llama3.2:1b", FullModel: "llama3.1:8b"}, "http://localhost:11434")
 *   answer, routeTo, err := r.Classify(ctx, history)
 */
type Router struct {
	cfg         RouterConfig
	baseURL     string
	smallCtxLen int // context window of the fast model in tokens
}

/** NewRouter creates a Router and probes the fast model's context window via
 * ShowModel. If the probe fails, a conservative default of 4096 tokens is used.
 *
 * Parameters:
 *   cfg     (RouterConfig) — fast and full model names.
 *   baseURL (string)       — Ollama server base URL.
 *
 * Returns:
 *   *Router — ready to classify prompts.
 *   error   — currently always nil; reserved for future validation.
 *
 * Example:
 *   r, _ := NewRouter(RouterConfig{FastModel: "llama3.2:1b", FullModel: "llama3.1:8b"}, "http://localhost:11434")
 */
func NewRouter(cfg RouterConfig, baseURL string) (*Router, error) {
	r := &Router{
		cfg:         cfg,
		baseURL:     baseURL,
		smallCtxLen: 4096, // conservative fallback
	}
	c := NewOllamaClient(baseURL, cfg.FastModel)
	if detail, err := c.ShowModel(context.Background(), cfg.FastModel); err == nil && detail.ContextLength > 0 {
		r.smallCtxLen = detail.ContextLength
	}
	return r, nil
}

/** FastModel returns the configured fast model name.
 *
 * Returns:
 *   string — fast model identifier.
 *
 * Example:
 *   fmt.Println(router.FastModel()) // "llama3.2:1b"
 */
func (r *Router) FastModel() string { return r.cfg.FastModel }

/** FullModel returns the configured full model name.
 *
 * Returns:
 *   string — full model identifier.
 *
 * Example:
 *   fmt.Println(router.FullModel()) // "llama3.1:8b"
 */
func (r *Router) FullModel() string { return r.cfg.FullModel }

/** Classify sends a trimmed slice of the conversation history to the fast
 * model and interprets its reply. If the reply begins with "ROUTE:full" the
 * full model name is returned in routeTo and answer is empty. Otherwise the
 * reply is a direct answer and routeTo is empty. Stats from the fast model
 * call are always returned so callers can display timing and token counts.
 *
 * Parameters:
 *   ctx     (context.Context) — controls the HTTP request lifetime.
 *   history ([]Message)       — full conversation history including the latest user message.
 *
 * Returns:
 *   answer  (string)    — direct answer from the fast model; empty when routing.
 *   routeTo (string)    — model name to re-send to; empty when answered directly.
 *   stats   (ChatStats) — timing and token counts from the fast model call.
 *   err     (error)     — non-nil if the fast model call fails.
 *
 * Example:
 *   answer, routeTo, stats, err := router.Classify(ctx, agent.History)
 *   if routeTo != "" {
 *       agent.Client = NewOllamaClient(baseURL, routeTo)
 *   }
 */
func (r *Router) Classify(ctx context.Context, history []Message) (answer, routeTo string, stats ChatStats, err error) {
	msgs := r.buildRoutingMessages(history)
	c := NewOllamaClient(r.baseURL, r.cfg.FastModel)
	var buf strings.Builder
	if stats, err = c.Chat(ctx, msgs, &buf); err != nil {
		return "", "", stats, err
	}
	reply := strings.TrimSpace(buf.String())
	if strings.HasPrefix(reply, "ROUTE:") {
		return "", r.cfg.FullModel, stats, nil
	}
	return reply, "", stats, nil
}

// buildRoutingMessages constructs the message slice sent to the fast model.
// It always includes the routing system prompt and the latest user message.
// Recent non-system history is prepended newest-first until 25% of the fast
// model's context window is consumed.
func (r *Router) buildRoutingMessages(history []Message) []Message {
	system := Message{Role: "system", Content: routingSystemPrompt}

	if len(history) == 0 {
		return []Message{system}
	}

	// Budget: 25% of the fast model's context window.
	budget := r.smallCtxLen / 4
	budget -= estimateTokens(routingSystemPrompt)
	if budget < 50 {
		budget = 50
	}

	// The latest user message is always included.
	current := history[len(history)-1]
	budget -= estimateTokens(current.Content)

	// Walk backwards through the preceding history, skipping system messages.
	var recent []Message
	for i := len(history) - 2; i >= 0 && budget > 0; i-- {
		msg := history[i]
		if msg.Role == "system" {
			continue
		}
		cost := estimateTokens(msg.Content)
		if cost > budget {
			break
		}
		budget -= cost
		recent = append([]Message{msg}, recent...)
	}

	msgs := []Message{system}
	msgs = append(msgs, recent...)
	msgs = append(msgs, current)
	return msgs
}

// estimateTokens returns a fast token estimate using the 4-bytes-per-token
// heuristic. Used during message trimming to avoid HTTP calls.
func estimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 {
		n = 1
	}
	return n
}
