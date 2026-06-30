package harvey

import (
	"context"
	"fmt"
	"io"
	"strings"
)

/** ChunkAnalysisParams bundles all inputs to RunChunkedAnalysis.
 * Keeping them in a struct avoids a long parameter list and makes it
 * easy to add fields (e.g. a synthesis model distinct from the chunk
 * model) without changing the call site.
 *
 * Fields:
 *   Filename    (string)          — display name of the document being analysed.
 *   Chunks      ([]DocumentChunk) — document segments from ChunkDocument.
 *   Instruction (string)          — the user's chunk prompt (any @mention already resolved out).
 *   Model       (string)      — model name for LLM calls; used in recording only.
 *   DocType     (DocType)     — DocTypeProse or DocTypeSource; determines boundary label.
 *   Config      (ChunkConfig) — chunking configuration from harvey.yaml or defaults.
 *
 * Example:
 *   params := ChunkAnalysisParams{
 *       Filename:    "README.md",
 *       Chunks:      chunks,
 *       Instruction: "Summarize each section.",
 *       Model:       "llama3.2:1b",
 *       DocType:     DocTypeProse,
 *       Config:      DefaultChunkConfig(),
 *   }
 */
type ChunkAnalysisParams struct {
	Filename    string
	Chunks      []DocumentChunk
	Instruction string
	Model       string
	DocType     DocType
	Config      ChunkConfig
}

/** RunChunkedAnalysis runs a map-reduce workflow over a pre-chunked document.
 * It calls the LLM once per chunk (map phase) with the user instruction and
 * the chunk content, then once more to synthesise all partial results into a
 * single coherent response (reduce phase). Progress lines are written to w.
 *
 * A chunk-level LLM failure does not abort the map phase: the failure is noted
 * in partialResults and synthesis runs over all available results. A synthesis
 * failure is unrecoverable and is returned as an error.
 *
 * rec and dbg may each be nil; when nil, the respective recording/logging calls
 * are silently skipped.
 *
 * Parameters:
 *   ctx    (context.Context)     — cancellation context.
 *   client (LLMClient)           — LLM backend for map and reduce calls.
 *   rec    (*Recorder)           — session recorder; nil is accepted.
 *   dbg    (*DebugLog)           — debug log for llm_request/llm_response events; nil is accepted.
 *   params (ChunkAnalysisParams) — all inputs.
 *   w      (io.Writer)           — progress output (usually os.Stdout).
 *
 * Returns:
 *   string — synthesised final answer.
 *   error  — synthesis error, or nil.
 *
 * Example:
 *   result, err := RunChunkedAnalysis(ctx, client, rec, dbg, params, os.Stdout)
 *   if err != nil { log.Fatal(err) }
 *   fmt.Println(result)
 */
func RunChunkedAnalysis(ctx context.Context, client LLMClient, rec *Recorder, dbg *DebugLog, params ChunkAnalysisParams, w io.Writer) (string, error) {
	n := len(params.Chunks)

	if rec != nil {
		_ = rec.RecordChunkAnalysisStart(params.Filename, n, params.Model, params.DocType, params.Config)
	}

	partialResults := make([]string, 0, n)

	// ── Map phase ────────────────────────────────────────────────────────────
	for i, chunk := range params.Chunks {
		chunkNum := i + 1
		fmt.Fprintf(w, "Processing chunk %d/%d…\n", chunkNum, n)

		prompt := fmt.Sprintf("%s\n\n---\nChunk %d of %d (lines %d–%d):\n%s",
			params.Instruction, chunkNum, n, chunk.StartLine, chunk.EndLine, chunk.Content)
		messages := []Message{{Role: "user", Content: prompt}}

		if dbg != nil {
			dbg.LogLLMRequest(params.Model, len(messages), 0)
		}

		var buf strings.Builder
		stats, err := client.Chat(ctx, messages, &buf)

		outcome := "ok"
		var result string
		if err != nil {
			outcome = "error: " + firstLine(err.Error())
			result = fmt.Sprintf("[chunk %d failed: %v]", chunkNum, err)
			if dbg != nil {
				dbg.LogError("chunk_llm", err.Error())
			}
		} else {
			result = buf.String()
			if dbg != nil {
				dbg.LogLLMResponse(stats, 0)
			}
		}

		partialResults = append(partialResults, result)

		if rec != nil {
			_ = rec.RecordChunkResult(chunkNum, n, params.Model, result, outcome)
		}
	}

	// ── Reduce phase ─────────────────────────────────────────────────────────
	fmt.Fprintf(w, "Synthesizing %d results…\n", n)

	synthesisPrompt := fmt.Sprintf(
		"%s\n\n---\nCombine the following %d section summaries into a single coherent response:\n\n%s",
		params.Instruction, n,
		strings.Join(partialResults, "\n\n---\n"),
	)
	messages := []Message{{Role: "user", Content: synthesisPrompt}}

	if dbg != nil {
		dbg.LogLLMRequest(params.Model, len(messages), 0)
	}

	var synBuf strings.Builder
	stats, err := client.Chat(ctx, messages, &synBuf)
	if err != nil {
		if rec != nil {
			_ = rec.RecordChunkSynthesis(params.Model, "", "error: "+firstLine(err.Error()))
		}
		if dbg != nil {
			dbg.LogError("chunk_synthesis", err.Error())
		}
		return "", fmt.Errorf("chunked analysis synthesis: %w", err)
	}

	synthesis := synBuf.String()
	if rec != nil {
		_ = rec.RecordChunkSynthesis(params.Model, synthesis, "ok")
	}
	if dbg != nil {
		dbg.LogLLMResponse(stats, 0)
	}
	return synthesis, nil
}

// firstLine returns the first line of s, or s itself when there is no newline.
func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}
