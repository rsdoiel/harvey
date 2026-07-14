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
 * rec may be nil; when nil, recording calls are silently skipped. There is no
 * separate debug-log parameter — client (an *AnyLLMClient, in production)
 * already logs its own llm_request/llm_response events internally; a caller-
 * side debug log here would just duplicate that (see DECISIONS.md, the
 * chunk-analysis double-logging fix).
 *
 * Parameters:
 *   ctx    (context.Context)     — cancellation context.
 *   client (LLMClient)           — LLM backend for map and reduce calls.
 *   rec    (*Recorder)           — session recorder; nil is accepted.
 *   params (ChunkAnalysisParams) — all inputs.
 *   w      (io.Writer)           — progress output (usually os.Stdout).
 *
 * Returns:
 *   string — synthesised final answer.
 *   error  — synthesis error, or nil.
 *
 * Example:
 *   result, err := RunChunkedAnalysis(ctx, client, rec, params, os.Stdout)
 *   if err != nil { log.Fatal(err) }
 *   fmt.Println(result)
 */
// chunkSystemPrompt is prepended to every chunk and synthesis LLM call. It
// constrains the model to the text provided in each call, preventing it from
// drawing on training data to fill gaps between chunks.
const chunkSystemPrompt = "You are a focused document analysis assistant. " +
	"Analyse ONLY the text provided in this message. " +
	"Do not reference any information from outside the provided content."

func RunChunkedAnalysis(ctx context.Context, client LLMClient, rec *Recorder, params ChunkAnalysisParams, w io.Writer) (string, error) {
	n := len(params.Chunks)

	// Preflight reachability check — fail immediately with one clear message
	// instead of letting every chunk in the map phase fire its own connection
	// error before the run finally errors out at synthesis (TODO.md).
	if reachable, checked := probeClientReachable(client); checked && !reachable {
		return "", fmt.Errorf("chunked analysis: %s is unreachable — check the server is running before retrying", client.Name())
	}

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
		messages := []Message{
			{Role: "system", Content: chunkSystemPrompt},
			{Role: "user", Content: prompt},
		}

		var buf strings.Builder
		_, err := client.Chat(ctx, messages, &buf)

		outcome := "ok"
		var result string
		if err != nil {
			outcome = "error: " + firstLine(err.Error())
			result = fmt.Sprintf("[chunk %d failed: %v]", chunkNum, err)
		} else {
			result = buf.String()
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
	messages := []Message{
		{Role: "system", Content: chunkSystemPrompt},
		{Role: "user", Content: synthesisPrompt},
	}

	var synBuf strings.Builder
	_, err := client.Chat(ctx, messages, &synBuf)
	if err != nil {
		if rec != nil {
			_ = rec.RecordChunkSynthesis(params.Model, "", "error: "+firstLine(err.Error()))
		}
		return "", fmt.Errorf("chunked analysis synthesis: %w", err)
	}

	synthesis := synBuf.String()
	if rec != nil {
		_ = rec.RecordChunkSynthesis(params.Model, synthesis, "ok")
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

/** probeClientReachable reports whether client's backend is currently
 * reachable, for backends with a known local health-check endpoint (ollama,
 * llamafile, llamacpp). checked is false for backends with no local
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
	case "llamafile":
		return ProbeLlamafile(LlamafileHealthURL(ac.BackendURL())), true
	case "llamacpp":
		return probeLlamaCpp(LlamafileHealthURL(ac.BackendURL())), true
	default:
		return false, false
	}
}
