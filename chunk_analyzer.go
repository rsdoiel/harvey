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
 * The client (an *AnyLLMClient, in production) reports each of its calls to the
 * session log itself, as ad-hoc requests with the purposes chunk and synthesis
 * that the context here sets, so this function writes no record of its own.
 *
 * Parameters:
 *   ctx    (context.Context)     — cancellation context.
 *   client (LLMClient)           — LLM backend for map and reduce calls.
 *   params (ChunkAnalysisParams) — all inputs.
 *   w      (io.Writer)           — progress output (usually os.Stdout).
 *
 * Returns:
 *   string — synthesised final answer.
 *   error  — synthesis error, or nil.
 *
 * Example:
 *   result, err := RunChunkedAnalysis(ctx, client, params, os.Stdout)
 *   if err != nil { log.Fatal(err) }
 *   fmt.Println(result)
 */
// chunkSystemPrompt is prepended to every chunk and synthesis LLM call. It
// constrains the model to the text provided in each call, preventing it from
// drawing on training data to fill gaps between chunks.
const chunkSystemPrompt = "You are a focused document analysis assistant. " +
	"Analyse ONLY the text provided in this message. " +
	"Do not reference any information from outside the provided content."

func RunChunkedAnalysis(ctx context.Context, client LLMClient, params ChunkAnalysisParams, w io.Writer) (string, error) {
	n := len(params.Chunks)

	// Preflight reachability check — fail immediately with one clear message
	// instead of letting every chunk in the map phase fire its own connection
	// error before the run finally errors out at synthesis (TODO.md).
	if reachable, checked := probeClientReachable(client); checked && !reachable {
		return "", fmt.Errorf("chunked analysis: %s is unreachable — check the server is running before retrying", client.Name())
	}

	partialResults := make([]string, 0, n)

	// ── Map phase ────────────────────────────────────────────────────────────
	for i, chunk := range params.Chunks {
		chunkNum := i + 1
		fmt.Fprintf(w, "Processing chunk %d/%d…\n", chunkNum, n)

		prompt := fmt.Sprintf("%s\n\n---\nChunk %d of %d (lines %d–%d):\n%s",
			params.Instruction, chunkNum, n, chunk.StartLine, chunk.EndLine, chunk.Content)
		messages := []Message{
			{Role: "system", Content: chunkSystemPrompt},
			{Role: "user", Content: prompt},
		}

		var buf strings.Builder
		chunkCtx := withCallPurpose(ctx, "chunk", map[string]any{
			"path": params.Filename, "from_line": chunk.StartLine, "to_line": chunk.EndLine,
			"chunk": chunkNum, "of": n,
			"doc_type": docTypeLabel(params.DocType), "chunk_size": params.Config.ChunkSizeBytes,
		})
		_, err := client.Chat(chunkCtx, messages, &buf)

		var result string
		if err != nil {
			result = fmt.Sprintf("[chunk %d failed: %v]", chunkNum, err)
		} else {
			result = buf.String()
		}

		partialResults = append(partialResults, result)
	}

	// ── Reduce phase ─────────────────────────────────────────────────────────
	fmt.Fprintf(w, "Synthesizing %d results…\n", n)

	synthesisPrompt := fmt.Sprintf(
		"%s\n\n---\nCombine the following %d section summaries into a single coherent response:\n\n%s",
		params.Instruction, n,
		strings.Join(partialResults, "\n\n---\n"),
	)
	messages := []Message{
		{Role: "system", Content: chunkSystemPrompt},
		{Role: "user", Content: synthesisPrompt},
	}

	var synBuf strings.Builder
	synCtx := withCallPurpose(ctx, "synthesis", map[string]any{"path": params.Filename, "chunks": n})
	_, err := client.Chat(synCtx, messages, &synBuf)
	if err != nil {
		return "", fmt.Errorf("chunked analysis synthesis: %w", err)
	}

	synthesis := synBuf.String()
	return synthesis, nil
}

// firstLine returns the first line of s, or s itself when there is no newline.
func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

/** probeClientReachable reports whether client's backend is currently
 * reachable, for backends with a known local health-check endpoint (ollama).
 * checked is false for backends with no local
 * reachability probe (cloud providers, or any client that isn't an
 * *AnyLLMClient, e.g. a test double) — callers should treat checked=false as
 * "nothing to verify, proceed as usual," not as a failure.
 *
 * Parameters:
 *   client (LLMClient) — the client whose backend to probe.
 *
 * Returns:
 *   reachable (bool) — true when the backend responded to its health probe.
 *   checked   (bool) — true when a probe was actually attempted.
 *
 * Example:
 *   if reachable, checked := probeClientReachable(client); checked && !reachable {
 *       return "", fmt.Errorf("backend unreachable")
 *   }
 */
func probeClientReachable(client LLMClient) (reachable, checked bool) {
	ac, ok := client.(*AnyLLMClient)
	if !ok {
		return false, false
	}
	switch ac.ProviderName() {
	case "ollama":
		return ProbeOllama(ac.BackendURL()), true
	default:
		return false, false
	}
}

// docTypeLabel names a document type as the session log and the Fountain scene
// do: "source" for code, "prose" for everything else.
func docTypeLabel(t DocType) string {
	if t == DocTypeSource {
		return "source"
	}
	return "prose"
}
