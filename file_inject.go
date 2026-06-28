package harvey

// file_inject.go — pre-resolves file path references in a user prompt into
// inline file content blocks, for models that do not reliably call read_file.
//
// When toolsReliable() returns false, runChatTurn calls injectFileContext
// before sending the prompt to the model. This means Phi4, Llama3, and any
// other model that ignores the tools schema will still see the file content
// they would have received via a read_file tool call.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxInjectFileBytes is the per-file size cap for inline injection. Files
// larger than this are skipped to avoid blowing up the context window.
// 16 KiB ≈ 4K tokens — safe for an 8B model on CPU with existing history.
// (64 KiB caused OOM crashes on Pi when combined with session history and
// injected memories; see small-model-budget-design.md.)
const maxInjectFileBytes = 16 * 1024

// injectableExts is the set of file extensions treated as injectable text.
// Binaries, images, and PDFs are excluded (PDFs require extraction tooling).
var injectableExts = map[string]bool{
	".md": true, ".txt": true, ".go": true, ".ts": true, ".js": true,
	".py": true, ".sh": true, ".bash": true, ".yaml": true, ".yml": true,
	".json": true, ".toml": true, ".html": true, ".htm": true, ".css": true,
	".sql": true, ".rs": true, ".c": true, ".h": true, ".cpp": true,
	".java": true, ".rb": true, ".pl": true, ".r": true, ".tex": true,
	".csv": true, ".tsv": true, ".xml": true, ".fountain": true, ".spmd": true,
}

/** injectFileContext scans prompt for path-like tokens (via looksLikePath) that
 * resolve to readable text files within the workspace, reads each one, and
 * prepends the file contents before the original prompt text. Returns the
 * original prompt unchanged when no injectable files are found.
 *
 * This is the primary mitigation for small language models that do not call
 * the read_file tool despite it being offered in the tools schema. By
 * injecting file content directly into the prompt, those models can still
 * reference the requested file without needing to fire a tool call.
 *
 * Parameters:
 *   ws     (*Workspace) — the active Harvey workspace (used for path resolution).
 *   prompt (string)     — the user's raw prompt text.
 *
 * Returns:
 *   string — the (possibly augmented) prompt; always ends with the original prompt.
 *
 * Example:
 *   augmented := injectFileContext(ws, "Please review notes.md")
 *   // augmented now starts with "### File: notes.md\n\n<content>\n\n---\n\n"
 */
func injectFileContext(ws *Workspace, prompt string) string {
	var header strings.Builder
	seen := map[string]bool{}

	for _, tok := range strings.Fields(prompt) {
		tok = strings.Trim(tok, ".,;:!?\"'`()")
		if tok == "" || !looksLikePath(tok) {
			continue
		}
		if seen[tok] {
			continue
		}
		seen[tok] = true

		// Idempotency guard — skip if the content block is already in the prompt.
		if strings.Contains(prompt, "### File: "+tok) {
			continue
		}

		// Verify the extension is injectable text before hitting the filesystem.
		if !injectableExts[strings.ToLower(filepath.Ext(tok))] {
			continue
		}

		absPath, err := ws.AbsPath(tok)
		if err != nil {
			continue
		}
		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > maxInjectFileBytes {
			continue
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		fmt.Fprintf(&header, "### File: %s\n\n%s\n\n---\n\n", tok, string(content))
	}

	if header.Len() == 0 {
		return prompt
	}
	return header.String() + prompt
}

// injectOrChunk extends the inject path with chunking support. It is called by
// runChatTurn in place of injectFileContext when !toolsReliable().
//
// Files ≤ maxInjectFileBytes are injected directly. Files that exceed the
// context-budget threshold trigger the interactive promptChunkInstruction →
// RunChunkedAnalysis flow when chunking is enabled. Files that exceed the cap
// but fit within the context budget are also injected directly. Files that
// exceed both the cap and the budget are skipped with a short hint when
// chunking is disabled or the context limit is unknown.
func (a *Agent) injectOrChunk(ctx context.Context, prompt string, out io.Writer) string {
	if a.Workspace == nil {
		return prompt
	}
	var header strings.Builder
	seen := map[string]bool{}

	for _, tok := range strings.Fields(prompt) {
		tok = strings.Trim(tok, ".,;:!?\"'`()")
		if tok == "" || !looksLikePath(tok) {
			continue
		}
		if seen[tok] {
			continue
		}
		seen[tok] = true

		if strings.Contains(prompt, "### File: "+tok) {
			continue
		}

		if !injectableExts[strings.ToLower(filepath.Ext(tok))] {
			continue
		}

		absPath, err := a.Workspace.AbsPath(tok)
		if err != nil {
			continue
		}
		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			continue
		}

		if info.Size() <= maxInjectFileBytes {
			// Within the conservative per-file cap — inject directly.
			content, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			fmt.Fprintf(&header, "### File: %s\n\n%s\n\n---\n\n", tok, string(content))
			continue
		}

		// File is larger than maxInjectFileBytes.
		// Try chunked analysis when it is enabled and the LLM is available.
		if !a.Config.Chunking.Enabled || a.Client == nil {
			fmt.Fprint(out, dim("  (skipping "+tok+" — file too large to inject; enable chunking: or use the read_file tool)\n"))
			continue
		}
		rem := remainingContext(a)
		if rem <= 0 {
			fmt.Fprint(out, dim("  (skipping "+tok+" — context full)\n"))
			continue
		}
		budget := int(float64(rem) * a.Config.Chunking.Threshold)
		exceeded, size, statErr := fileExceedsBudget(absPath, budget)
		if statErr != nil {
			continue
		}
		if !exceeded {
			// Larger than the safety cap but still fits within the context budget.
			// Inject directly with the global output cap applied.
			content, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			fmt.Fprintf(&header, "### File: %s\n\n%s\n\n---\n\n", tok, capOutput(string(content), a.Config.MaxOutputBytes))
			continue
		}

		// File exceeds both the safety cap and the context budget: prompt the
		// user for a chunk instruction and run the map-reduce analysis.
		lastMsg := lastUserMessage(a)
		instruction, cancelled := promptChunkInstruction(a.In, out, tok, int(size/4), budget, lastMsg)
		if cancelled {
			continue
		}
		model := a.Client.Name()
		if ac, ok := a.Client.(*AnyLLMClient); ok {
			model = ac.ModelName()
		}
		if mentionName, rest, ok := ParseAtMention(instruction); ok {
			model = mentionName
			instruction = rest
		}
		chunkData, readErr := os.ReadFile(absPath)
		if readErr != nil {
			continue
		}
		docType := DetectDocType(absPath)
		chunks := ChunkDocument(string(chunkData), a.Config.Chunking, docType)
		params := ChunkAnalysisParams{
			Filename:    filepath.Base(tok),
			Chunks:      chunks,
			Instruction: instruction,
			Model:       model,
			DocType:     docType,
			Config:      a.Config.Chunking,
		}
		synthesis, synthErr := RunChunkedAnalysis(ctx, a.Client, a.Recorder, params, out)
		if synthErr != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Chunked analysis: %v\n", synthErr)
			continue
		}
		fmt.Fprintf(&header, "### Analysis of %s\n\n%s\n\n---\n\n", tok, synthesis)
	}

	if header.Len() == 0 {
		return prompt
	}
	return header.String() + prompt
}

// cantReadPhrases lists lower-cased substrings found in model responses that
// indicate the model declined to read a file rather than reading it. These
// phrases are written by models that ignore the tools schema or lack file-read
// capability (Phi4, Llama3.x, etc.).
var cantReadPhrases = []string{
	"i don't have the capability to",
	"i don't have the ability to",
	"i cannot directly read",
	"i can't directly read",
	"i'm unable to read",
	"i'm unable to access",
	"i cannot access",
	"i can't access the file",
	"please provide the file content",
	"please provide the content",
	"please share the file",
	"please paste the",
	"could you provide the file",
	"i don't have access to the file",
	"i don't have direct access to",
}

/** looksLikeCantReadFile reports whether response contains phrases that indicate
 * the model declined to read a file instead of reading it. Used by the option 2
 * retry path in runChatTurn to detect and recover from models that ignore
 * the read_file tool schema.
 *
 * Parameters:
 *   response (string) — the model's raw text reply for this turn.
 *
 * Returns:
 *   bool — true when any known refusal phrase appears in the response.
 *
 * Example:
 *   if looksLikeCantReadFile(reply) {
 *       // retry with file content pre-loaded
 *   }
 */
func looksLikeCantReadFile(response string) bool {
	lower := strings.ToLower(response)
	for _, phrase := range cantReadPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

/** modelToolMode returns the explicit ToolMode for the active model from the
 * model cache, or ToolModeAuto when no entry exists or no AnyLLMClient is active.
 *
 * Returns:
 *   string — one of the ToolMode* constants; ToolModeAuto ("") when unknown.
 *
 * Example:
 *   if a.modelToolMode() == ToolModeInject { ... }
 */
func (a *Agent) modelToolMode() string {
	if a.ModelCache == nil {
		return ToolModeAuto
	}
	ac, ok := a.Client.(*AnyLLMClient)
	if !ok {
		return ToolModeAuto
	}
	cap, err := a.ModelCache.Get(ac.ModelName())
	if err != nil || cap == nil {
		return ToolModeAuto
	}
	return cap.ToolMode
}

/** toolsReliable reports whether the active model is known to reliably execute
 * structured tool calls via the OpenAI tools API. Returns false when:
 *   - tools are disabled globally (ToolsEnabled=false or Tools=nil), or
 *   - ToolMode is set to anything other than ToolModeStructured, or
 *   - the model cache has no entry for the current model (unknown capability), or
 *   - the model cache shows SupportsTools != CapYes.
 *
 * When toolsReliable returns false, callers should pre-inject file content via
 * injectFileContext rather than relying on the model to call read_file.
 *
 * Returns:
 *   bool — true only when tools are on and the model is known to use them.
 *
 * Example:
 *   if !a.toolsReliable() {
 *       augmented = injectFileContext(a.Workspace, augmented)
 *   }
 */
func (a *Agent) toolsReliable() bool {
	if a.toolsReliableOverride != nil {
		return a.toolsReliableOverride()
	}
	if !a.Config.ToolsEnabled || a.Tools == nil {
		return false
	}
	// Explicit ToolMode overrides CapabilityStatus.
	if mode := a.modelToolMode(); mode != ToolModeAuto {
		return mode == ToolModeStructured
	}
	if a.ModelCache == nil {
		return false
	}
	ac, ok := a.Client.(*AnyLLMClient)
	if !ok {
		return false
	}
	cap, err := a.ModelCache.Get(ac.ModelName())
	if err != nil || cap == nil {
		return false
	}
	return cap.SupportsTools == CapYes
}
