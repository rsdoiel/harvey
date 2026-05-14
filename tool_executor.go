package harvey

import (
	"context"
	"fmt"
	"io"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

/** ToolCapable is satisfied by LLM clients that support schema-based tool
 * calling. AnyLLMClient implements this interface; other backends that do not
 * support tools do not need to implement it.
 *
 * Example:
 *   if tc, ok := agent.Client.(ToolCapable); ok {
 *       stats, calls, err := tc.ChatWithTools(ctx, history, schemas, out)
 *   }
 */
type ToolCapable interface {
	ChatWithTools(ctx context.Context, messages []Message, tools []anyllm.Tool, out io.Writer) (ChatStats, []anyllm.ToolCall, error)
}

/** ToolExecutor drives the multi-turn tool call loop between the LLM and the
 * tool registry. It handles streaming, tool execution, and re-submission of
 * results until the LLM produces a final text response or the iteration limit
 * is reached.
 *
 * Fields:
 *   Registry      (*ToolRegistry) — registered tool handlers.
 *   Client        (LLMClient)     — LLM backend; must implement ToolCapable for tool calls.
 *   MaxIterations (int)           — hard limit on tool call rounds per user turn.
 *   MaxOutputBytes (int)          — cap on tool output sent back to the LLM.
 *
 * Example:
 *   ex := NewToolExecutor(agent.Tools, agent.Client, cfg)
 *   stats, err := ex.RunToolLoop(ctx, agent.History, os.Stdout)
 */
type ToolExecutor struct {
	Registry       *ToolRegistry
	Client         LLMClient
	MaxIterations  int
	MaxOutputBytes int
	DebugLog       *DebugLog
}

/** NewToolExecutor creates a ToolExecutor from the agent's tool registry,
 * client, and config.
 *
 * Parameters:
 *   registry (*ToolRegistry) — registered tools.
 *   client   (LLMClient)     — LLM backend.
 *   cfg      (*Config)       — source of MaxToolCallsPerTurn and MaxOutputBytes.
 *
 * Returns:
 *   *ToolExecutor — ready to use.
 *
 * Example:
 *   ex := NewToolExecutor(agent.Tools, agent.Client, agent.Config)
 */
func NewToolExecutor(registry *ToolRegistry, client LLMClient, cfg *Config) *ToolExecutor {
	maxIter := cfg.MaxToolCallsPerTurn
	if maxIter <= 0 {
		maxIter = defaultMaxToolCallsPerTurn
	}
	maxOut := cfg.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = defaultMaxOutputBytes
	}
	return &ToolExecutor{
		Registry:       registry,
		Client:         client,
		MaxIterations:  maxIter,
		MaxOutputBytes: maxOut,
	}
}

/** ExecuteToolCalls runs each tool call from the LLM and returns tool-role
 * messages ready to append to the conversation history. Each message has
 * Role="tool" and ToolCallID set so the next LLM turn can correlate results.
 *
 * Parameters:
 *   ctx       (context.Context)    — controls the request lifetime.
 *   toolCalls ([]anyllm.ToolCall)  — tool calls from the LLM's response.
 *
 * Returns:
 *   []Message — one tool-role message per call.
 *   error     — if any tool execution fails (first error wins).
 *
 * Example:
 *   results, err := ex.ExecuteToolCalls(ctx, calls)
 */
func (e *ToolExecutor) ExecuteToolCalls(ctx context.Context, toolCalls []anyllm.ToolCall) ([]Message, error) {
	results := make([]Message, 0, len(toolCalls))
	for _, tc := range toolCalls {
		start := time.Now()
		output, err := e.Registry.Dispatch(ctx, tc.Function.Name, tc.Function.Arguments, e.MaxOutputBytes)
		elapsed := time.Since(start)
		errStr := ""
		if err != nil {
			errStr = err.Error()
			output = fmt.Sprintf("error: %v", err)
		}
		e.DebugLog.LogToolCall(tc.Function.Name, len(output), elapsed, errStr)
		results = append(results, Message{
			Role:       "tool",
			Content:    output,
			ToolCallID: tc.ID,
		})
	}
	return results, nil
}

/** RunToolLoop drives the full multi-turn tool conversation:
 *  1. Sends messages + tool schemas to the LLM.
 *  2. If the LLM returns tool calls, executes them and appends results.
 *  3. Repeats until the LLM returns a plain text response or MaxIterations
 *     is reached — at which point an error is returned (not a silent stop).
 *
 * If the client does not implement ToolCapable, falls back to regular Chat.
 *
 * Parameters:
 *   ctx      (context.Context) — controls the request lifetime.
 *   messages ([]Message)       — full conversation history (modified in place).
 *   out      (io.Writer)       — destination for streamed reply tokens.
 *
 * Returns:
 *   []Message — updated history including tool results and the final reply.
 *   ChatStats — stats from the final LLM call.
 *   error     — non-nil on transport failure or iteration limit exceeded.
 *
 * Example:
 *   history, stats, err := ex.RunToolLoop(ctx, agent.History, os.Stdout)
 */
func (e *ToolExecutor) RunToolLoop(ctx context.Context, messages []Message, out io.Writer) ([]Message, ChatStats, error) {
	tc, ok := e.Client.(ToolCapable)
	if !ok || e.Registry == nil || e.Registry.Len() == 0 {
		// Fallback: client doesn't support tools or no tools registered.
		stats, err := e.Client.Chat(ctx, messages, out)
		return messages, stats, err
	}

	schemas := e.Registry.GetToolSchemas()
	history := messages

	for iter := 0; iter < e.MaxIterations; iter++ {
		stats, calls, err := tc.ChatWithTools(ctx, history, schemas, out)
		if err != nil {
			return history, stats, err
		}

		if len(calls) == 0 {
			// LLM returned a plain text response — we're done.
			return history, stats, nil
		}

		// Append the assistant's tool-call turn to history.
		history = append(history, Message{
			Role:      "assistant",
			ToolCalls: calls,
		})

		// Execute each tool and collect results.
		results, err := e.ExecuteToolCalls(ctx, calls)
		if err != nil {
			return history, stats, fmt.Errorf("tool execution: %w", err)
		}
		history = append(history, results...)
	}

	return history, ChatStats{}, fmt.Errorf("tool loop exceeded %d iterations without a final response", e.MaxIterations)
}