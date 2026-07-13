package harvey

import (
	"context"
	"io"
)

/** runBoundedTurn runs messages against client — through registry's
 * ToolExecutor when useTools is true and registry is non-nil, otherwise a
 * plain client.Chat call — streaming to w. Shared by /plan next
 * (plan_cmd.go) and @mention route dispatch (routing.go), which previously
 * duplicated this same branch independently. Chunk analysis
 * (chunk_analyzer.go) deliberately does not use this — it stays tool-free
 * by design (see subagent-dispatch-design.md).
 *
 * Parameters:
 *   ctx      (context.Context) — cancellation context.
 *   client   (LLMClient)       — LLM backend for the call.
 *   registry (*ToolRegistry)   — tool registry; nil disables tool calling
 *     regardless of useTools.
 *   cfg      (*Config)         — source of MaxToolCallsPerTurn/MaxOutputBytes
 *     for the ToolExecutor, when used.
 *   useTools (bool)            — caller's request to use tools this turn.
 *   messages ([]Message)       — the bounded context to send.
 *   dbg      (*DebugLog)       — debug log; nil is accepted.
 *   w        (io.Writer)       — destination for streamed reply tokens.
 *
 * Returns:
 *   updatedHistory ([]Message) — messages, possibly extended with tool
 *     call/result pairs, when the ToolExecutor path ran; nil on the
 *     plain-chat path (nothing to inspect for a caller like /plan next's
 *     "did a tool call fail?" check).
 *   stats (ChatStats) — token/timing stats from the call.
 *   err   (error)     — non-nil on transport or tool-loop failure.
 *
 * Example:
 *   updatedHistory, stats, err := runBoundedTurn(ctx, client, registry, cfg, true, messages, dbg, out)
 */
func runBoundedTurn(ctx context.Context, client LLMClient, registry *ToolRegistry, cfg *Config, useTools bool, messages []Message, dbg *DebugLog, w io.Writer) ([]Message, ChatStats, error) {
	if useTools && registry != nil {
		ex := NewToolExecutor(registry, client, cfg)
		ex.DebugLog = dbg
		updatedHistory, stats, err := ex.RunToolLoop(ctx, messages, w)
		return updatedHistory, stats, err
	}
	stats, err := client.Chat(ctx, messages, w)
	return nil, stats, err
}
