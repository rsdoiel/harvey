package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// confidenceInstruction is hidden-appended to every outgoing user message in a pipeline step.
const confidenceInstruction = "\n\n---\nAfter your response, append the following JSON block on its own line.\nDo not include any text after the block.\n{\"confidence\": 0.0-1.0, \"reason\": \"one-line rationale\"}"

// hedgingPhrases trigger the low-confidence score in the keyword-scan fallback.
var hedgingPhrases = []string{
	"i'm not sure",
	"i cannot determine",
	"i don't know",
	"unclear",
	"uncertain",
	"it's possible",
	"might be",
	"i'm unsure",
}

// atMentionRE matches the first @mention token in a pipeline file body.
var atMentionRE = regexp.MustCompile(`@([\w:.-]+)`)

/** parsePipelineArgs validates and parses /pipeline arguments.
 *
 * Parameters:
 *   root (string)   — absolute workspace root; used to validate file paths.
 *   args ([]string) — raw command arguments: [0] is the confidence %, rest are file paths.
 *
 * Returns:
 *   threshold (float64)  — confidence threshold in the range (0, 1].
 *   files     ([]string) — absolute paths to validated pipeline step files.
 *   err       (error)    — on bad format or path traversal.
 *
 * Example:
 *   threshold, files, err := parsePipelineArgs("/home/user/proj", []string{"90%", "step1.md"})
 */
func parsePipelineArgs(root string, args []string) (threshold float64, files []string, err error) {
	if len(args) < 2 {
		return 0, nil, fmt.Errorf("usage: /pipeline <CONFIDENCE%%> FILE [FILE ...]")
	}
	pct := args[0]
	if !strings.HasSuffix(pct, "%") {
		return 0, nil, fmt.Errorf("pipeline: first argument must be a confidence percentage (e.g. 90%%)")
	}
	val, parseErr := strconv.ParseFloat(strings.TrimSuffix(pct, "%"), 64)
	if parseErr != nil || val <= 0 || val > 100 {
		return 0, nil, fmt.Errorf("pipeline: invalid confidence %q — must be a number in (0, 100]", pct)
	}
	threshold = val / 100.0

	for _, f := range args[1:] {
		abs, resolveErr := resolveWorkspacePath(root, f)
		if resolveErr != nil {
			return 0, nil, fmt.Errorf("pipeline: cannot read %q: %w", f, resolveErr)
		}
		files = append(files, abs)
	}
	return threshold, files, nil
}

/** readPipelineFile reads a pipeline step file identified by its absolute path.
 *
 * Parameters:
 *   root    (string) — workspace root, used to compute the display path in errors.
 *   absPath (string) — absolute path to the file.
 *
 * Returns:
 *   body (string) — file contents.
 *   err  (error)  — on read failure.
 *
 * Example:
 *   body, err := readPipelineFile("/proj", "/proj/prompts/step1.md")
 */
func readPipelineFile(root, absPath string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		rel, _ := filepath.Rel(root, absPath)
		return "", fmt.Errorf("pipeline: cannot read %q: %w", rel, err)
	}
	return string(data), nil
}

/** scanAtMention returns the first @mention token found in body, or "" if none.
 *
 * Parameters:
 *   body (string) — pipeline file contents.
 *
 * Returns:
 *   string — mention name without the leading "@", or "".
 *
 * Example:
 *   name := scanAtMention("Use @llama3:8b for this step.")
 *   // "llama3:8b"
 */
func scanAtMention(body string) string {
	m := atMentionRE.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

/** resolvePipelineClient returns the LLMClient to use for a pipeline step.
 * An empty mention returns a.Client unchanged. Otherwise the route registry is
 * checked first; if the mention matches a registered route that client is used.
 * If no route matches, a same-provider client is constructed with the mention
 * as the model name.
 *
 * Parameters:
 *   a       (*Agent) — the active Harvey agent.
 *   mention (string) — @mention token from the pipeline file, or "".
 *
 * Returns:
 *   LLMClient — client to use for the step.
 *   error     — if the mention cannot be resolved.
 *
 * Example:
 *   client, err := resolvePipelineClient(agent, "llama3:8b")
 */
func resolvePipelineClient(a *Agent, mention string) (LLMClient, error) {
	if mention == "" {
		return a.Client, nil
	}
	if a.Routes != nil {
		if ep := a.Routes.Lookup(mention); ep != nil {
			return clientForEndpoint(ep, a.Config)
		}
	}
	ac, ok := a.Client.(*AnyLLMClient)
	if !ok {
		return nil, fmt.Errorf("pipeline: @mention %q did not resolve to a known model", mention)
	}
	switch ac.ProviderName() {
	case "ollama":
		return newOllamaLLMClient(ac.BackendURL(), mention, a.Config.OllamaTimeout), nil
	case "llamafile":
		return newLlamafileLLMClient(ac.BackendURL(), mention, a.Config.OllamaTimeout), nil
	case "llamacpp":
		return newLlamaCppLLMClient(ac.BackendURL(), mention, a.Config.OllamaTimeout), nil
	case "anthropic":
		return newAnthropicLLMClient(mention)
	case "deepseek":
		return newDeepSeekLLMClient(mention)
	case "gemini":
		return newGeminiLLMClient(mention)
	case "mistral":
		return newMistralLLMClient(mention)
	case "openai":
		return newOpenAILLMClient(mention)
	default:
		return nil, fmt.Errorf("pipeline: @mention %q did not resolve to a known model", mention)
	}
}

/** extractConfidence attempts to extract a confidence score from a model response
 * using three fallback methods in order: JSON block parsing, follow-up prompt,
 * and keyword scan. The confidence block is stripped from the response before
 * it is returned.
 *
 * Parameters:
 *   ctx      (context.Context) — cancellation context.
 *   client   (LLMClient)       — the client used for the step (for follow-up).
 *   response (string)          — the raw model response text.
 *
 * Returns:
 *   score   (float64) — confidence in [0, 1].
 *   stripped (string) — response with confidence block removed.
 *   method  (string)  — "json", "followup", or "keyword".
 *   err     (error)   — only set when the follow-up call itself errors (non-fatal).
 *
 * Example:
 *   score, clean, method, _ := extractConfidence(ctx, client, rawResponse)
 */
func extractConfidence(ctx context.Context, client LLMClient, response string) (score float64, stripped string, method string, err error) {
	// Method 1: JSON block — scan from the end for a line that is a JSON object
	// containing a "confidence" key.
	lines := strings.Split(strings.TrimRight(response, "\n "), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}
		var parsed struct {
			Confidence float64 `json:"confidence"`
		}
		if jsonErr := json.Unmarshal([]byte(line), &parsed); jsonErr == nil &&
			parsed.Confidence >= 0 && parsed.Confidence <= 1 {
			stripped = strings.TrimRight(strings.Join(lines[:i], "\n"), "\n ")
			return parsed.Confidence, stripped, "json", nil
		}
	}

	stripped = response

	// Method 2: Follow-up prompt.
	followUp := []Message{
		{Role: "assistant", Content: response},
		{Role: "user", Content: "Rate your confidence in your previous response 0.0–1.0. Reply only: CONFIDENCE: <score>"},
	}
	var buf bytes.Buffer
	if _, chatErr := client.Chat(ctx, followUp, &buf); chatErr == nil {
		reply := strings.TrimSpace(buf.String())
		upper := strings.ToUpper(reply)
		if idx := strings.Index(upper, "CONFIDENCE:"); idx >= 0 {
			numStr := strings.TrimSpace(reply[idx+len("CONFIDENCE:"):])
			if fields := strings.Fields(numStr); len(fields) > 0 {
				if f, fErr := strconv.ParseFloat(fields[0], 64); fErr == nil && f >= 0 && f <= 1 {
					return f, stripped, "followup", nil
				}
			}
		}
	}

	// Method 3: Keyword scan — hedging phrases → 0.30; no hedging → 0.80.
	lower := strings.ToLower(response)
	for _, phrase := range hedgingPhrases {
		if strings.Contains(lower, phrase) {
			return 0.30, stripped, "keyword", nil
		}
	}
	return 0.80, stripped, "keyword", nil
}

// pipelineStep holds the resolved state for one pipeline step.
type pipelineStep struct {
	absPath string
	relPath string
	body    string
	client  LLMClient
}

/** runPipelineStep executes one pipeline step: sends messages to the client,
 * streams the response, extracts confidence, and prints a per-step result line.
 *
 * Parameters:
 *   ctx       (context.Context) — cancellation context.
 *   a         (*Agent)          — the active Harvey agent (for spinner label).
 *   client    (LLMClient)       — client to use for this step.
 *   messages  ([]Message)       — messages to send.
 *   out       (io.Writer)       — terminal output writer.
 *   stepNum   (int)             — 1-based step index.
 *   total     (int)             — total number of steps.
 *   filename  (string)          — base filename for display.
 *   threshold (float64)         — minimum acceptable confidence score.
 *
 * Returns:
 *   strippedResponse (string)  — model response with confidence block removed.
 *   confidence       (float64) — extracted confidence score.
 *   err              (error)   — non-nil if the step fails or confidence is below threshold.
 *
 * Example:
 *   resp, conf, err := runPipelineStep(ctx, agent, client, msgs, os.Stdout, 1, 3, "step1.md", 0.90)
 */
func runPipelineStep(
	ctx context.Context,
	a *Agent,
	client LLMClient,
	messages []Message,
	out io.Writer,
	stepNum, total int,
	filename string,
	threshold float64,
) (strippedResponse string, confidence float64, err error) {
	// Build spinner label with context percentage when available.
	// Ollama: use the exact tokenizer API. All other backends: character estimate.
	label := fmt.Sprintf("[%d/%d] %s", stepNum, total, filename)
	if ac, ok := a.Client.(*AnyLLMClient); ok && len(messages) > 0 {
		var n int
		if ac.ProviderName() == "ollama" {
			n, _ = CountTokens(ctx, ac.BackendURL(), ac.ModelName(), HistoryText(messages))
		} else {
			n = len(HistoryText(messages)) / 4
		}
		if limit := a.effectiveContextLimit(); limit > 0 && n > 0 {
			pct := n * 100 / limit
			label = fmt.Sprintf("[%d/%d] %s | context %d%%", stepNum, total, filename, pct)
		}
	}

	sp := newSpinner(out, 0, label)
	var buf bytes.Buffer
	_, chatErr := client.Chat(ctx, messages, &buf)
	sp.stop()
	if chatErr != nil {
		return "", 0, fmt.Errorf("pipeline: step %d/%d [%s]: %w", stepNum, total, filename, chatErr)
	}
	rawResponse := buf.String()

	fmt.Fprint(out, rawResponse)
	if !strings.HasSuffix(rawResponse, "\n") {
		fmt.Fprintln(out)
	}

	score, cleaned, _, _ := extractConfidence(ctx, client, rawResponse)

	check := green("✓")
	if score < threshold {
		check = red("✗")
	}
	fmt.Fprintf(out, "Step %d/%d [%s] — confidence: %.2f %s", stepNum, total, filename, score, check)
	if score < threshold {
		fmt.Fprintf(out, " (threshold: %.2f)\n", threshold)
		return cleaned, score, fmt.Errorf("pipeline: step %d/%d [%s] confidence %.2f below threshold %.2f",
			stepNum, total, filename, score, threshold)
	}
	fmt.Fprintln(out)

	return cleaned, score, nil
}

/** cmdPipeline implements the /pipeline command, chaining Markdown prompt files
 * through models with a configurable confidence threshold gating each step.
 *
 * Parameters:
 *   a    (*Agent)    — the active Harvey agent.
 *   args ([]string)  — [0] confidence %, [1..] workspace-relative file paths.
 *   out  (io.Writer) — terminal output writer.
 *
 * Returns:
 *   error — on internal errors (not on threshold failures, which print and return nil).
 *
 * Example:
 *   err := cmdPipeline(agent, []string{"90%", "step1.md", "step2.md"}, os.Stdout)
 */
func cmdPipeline(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		return fmt.Errorf("pipeline: no workspace configured")
	}

	threshold, absPaths, parseErr := parsePipelineArgs(a.Workspace.Root, args)
	if parseErr != nil {
		fmt.Fprintln(out, parseErr)
		return nil
	}
	total := len(absPaths)

	// Phase 1 — read all files and resolve all clients BEFORE running any steps.
	steps := make([]pipelineStep, total)
	for i, abs := range absPaths {
		rel, _ := filepath.Rel(a.Workspace.Root, abs)
		body, readErr := readPipelineFile(a.Workspace.Root, abs)
		if readErr != nil {
			fmt.Fprintln(out, readErr)
			return nil
		}
		mention := scanAtMention(body)
		client, resolveErr := resolvePipelineClient(a, mention)
		if resolveErr != nil {
			fmt.Fprintln(out, resolveErr)
			return nil
		}
		steps[i] = pipelineStep{absPath: abs, relPath: rel, body: body, client: client}
	}

	// Collect display names for Fountain recording.
	relNames := make([]string, total)
	for i, s := range steps {
		relNames[i] = s.relPath
	}
	userName := strings.ToUpper(os.Getenv("USER"))
	if userName == "" {
		userName = "OPERATOR"
	}

	ctx := context.Background()
	var prevResponse string

	for i, step := range steps {
		stepNum := i + 1
		filename := filepath.Base(step.relPath)

		// Fountain: open a new pipeline scene for this step.
		if a.Recorder != nil {
			ts := time.Now().Format("2006-01-02 15:04:05")
			heading := fmt.Sprintf("INT. PIPELINE %s STEP %d/%d %s",
				strings.ToUpper(filename), stepNum, total, ts)
			a.Recorder.writeSceneHeading(heading)
			a.Recorder.writeAction(fmt.Sprintf(
				"%s executes /pipeline %.0f%% %s.\nModel: %s. Workspace: %s.",
				userName, threshold*100, strings.Join(relNames, " "),
				step.client.Name(), a.Workspace.Root,
			))
			a.Recorder.writeNote("hidden confidence instruction appended")
			a.Recorder.writeDialogue(userName, "", step.body)
		}

		// Build messages for this step.
		var messages []Message
		userBody := step.body + confidenceInstruction

		if i == 0 {
			messages = append(messages, a.History...)
			messages = append(messages, Message{Role: "user", Content: userBody})
		} else {
			if a.Config.SystemPrompt != "" {
				messages = []Message{{Role: "system", Content: a.Config.SystemPrompt}}
			}
			messages = append(messages, Message{Role: "user",
				Content: prevResponse + "\n\n---\n\n" + userBody})
		}

		strippedResp, score, stepErr := runPipelineStep(
			ctx, a, step.client, messages, out,
			stepNum, total, filename, threshold,
		)

		// Fountain: record step response.
		if a.Recorder != nil {
			a.Recorder.writeDialogue("HARVEY", "", strippedResp)
			if stepErr != nil {
				a.Recorder.writeNote(fmt.Sprintf(
					"confidence: %.2f — threshold not met (%.2f)", score, threshold))
			} else {
				a.Recorder.writeNote(fmt.Sprintf("confidence: %.2f — threshold met", score))
			}
		}

		if stepErr != nil {
			// Fountain: return to original scene after failure.
			if a.Recorder != nil {
				a.Recorder.writeTransition("CUT BACK TO:")
				ts := time.Now().Format("2006-01-02 15:04:05")
				a.Recorder.writeSceneHeading(fmt.Sprintf(
					"INT. HARVEY AND %s TALKING %s", userName, ts))
				a.Recorder.writeAction(fmt.Sprintf(
					"Harvey and %s are in chat mode. Model: %s. Workspace: %s.",
					userName, a.Client.Name(), a.Workspace.Root,
				))
				a.Recorder.writeNote(fmt.Sprintf(
					"pipeline stopped at step %d/%d: confidence %.2f below threshold %.2f",
					stepNum, total, score, threshold,
				))
			}
			return nil
		}

		// Fountain: CUT TO next step if there is one.
		if a.Recorder != nil && stepNum < total {
			a.Recorder.writeTransition("CUT TO:")
		}

		prevResponse = strippedResp
	}

	// All steps passed — append final response to history.
	a.AddMessage("assistant", prevResponse)

	// Fountain: return to original scene after success.
	if a.Recorder != nil {
		a.Recorder.writeTransition("CUT BACK TO:")
		ts := time.Now().Format("2006-01-02 15:04:05")
		a.Recorder.writeSceneHeading(fmt.Sprintf(
			"INT. HARVEY AND %s TALKING %s", userName, ts))
		a.Recorder.writeAction(fmt.Sprintf(
			"Harvey and %s are in chat mode. Model: %s. Workspace: %s.",
			userName, a.Client.Name(), a.Workspace.Root,
		))
		a.Recorder.writeDialogue("HARVEY", "", prevResponse)
	}

	fmt.Fprintln(out, green("✓")+" Pipeline complete.")
	return nil
}
