// assay runs evaluation prompts from a corpus YAML against one or more
// LLM backends and writes a Markdown report plus JSON results for human
// review and machine processing. Supports Ollama, Llamafile, and llama.cpp.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	harvey "github.com/rsdoiel/harvey"
	"gopkg.in/yaml.v3"
)

// ─── Corpus YAML types ────────────────────────────────────────────────────────

type Corpus struct {
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Prompts     []Prompt `yaml:"prompts"`
}

type Prompt struct {
	ID          string   `yaml:"id"`
	Category    string   `yaml:"category"`
	Description string   `yaml:"description"`
	Language    string   `yaml:"language"`
	Dialect     string   `yaml:"dialect,omitempty"`
	PromptText  string   `yaml:"prompt"`
	Checks      Checks   `yaml:"checks"`
	Human       []string `yaml:"human"`
	Notes       string   `yaml:"notes"`
}

type Checks struct {
	Compiles    bool     `yaml:"compiles,omitempty"`
	GoVet       bool     `yaml:"go_vet,omitempty"`
	Contains    []string `yaml:"contains,omitempty"`
	NotContains []string `yaml:"not_contains,omitempty"`
	SQLParses   bool     `yaml:"sql_parses,omitempty"`
}

// ─── Ollama model-list type ───────────────────────────────────────────────────

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ─── Result types ─────────────────────────────────────────────────────────────

type CheckResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// PromptResult holds the outcome of running one prompt against one model.
// Variant is "base" (no RAG), "rag" (RAG context injected), or "" (RAG not
// configured for this run).
type PromptResult struct {
	PromptID     string        `json:"prompt_id"`
	Category     string        `json:"category"`
	Model        string        `json:"model"`
	Variant      string        `json:"variant,omitempty"`
	Response     string        `json:"response"`
	Elapsed      time.Duration `json:"elapsed_ns"`
	PromptTokens int           `json:"prompt_tokens"`
	ReplyTokens  int           `json:"reply_tokens"`
	TokensPerSec float64       `json:"tokens_per_sec"`
	Checks       []CheckResult `json:"checks"`
	AutoPass     bool          `json:"auto_pass"`
	RagChunks    int           `json:"rag_chunks,omitempty"` // chunks injected; 0 = no RAG
}

type AssayResults struct {
	RunAt         time.Time      `json:"run_at"`
	Backend       string         `json:"backend"`                  // "Ollama", "Llamafile", or "LlamaCpp"
	LlamafilePath string         `json:"llamafile_path,omitempty"` // binary path when Backend=="Llamafile"
	OllamaURL     string         `json:"ollama_url,omitempty"`
	BackendURL    string         `json:"backend_url,omitempty"`    // inference URL for Llamafile and LlamaCpp
	RagDB         string         `json:"rag_db,omitempty"`
	RagEmbedModel string         `json:"rag_embed_model,omitempty"`
	RagCompare    bool           `json:"rag_compare,omitempty"`
	Results       []PromptResult `json:"results"`
}

// ─── Corpus loading ───────────────────────────────────────────────────────────

func loadCorpus(path string) (*Corpus, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	var c Corpus
	if err := yaml.Unmarshal(src, &c); err != nil {
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	return &c, nil
}

// ─── LLM client helpers ───────────────────────────────────────────────────────

/** newAssayClient creates an LLMClient backed by any OpenAI-compatible server.
 * It appends "/v1" to baseURL so callers pass the bare host:port.
 *
 * Parameters:
 *   baseURL (string) — bare base URL, e.g. "http://localhost:11434"
 *   model (string)   — model name for each chat request
 *
 * Returns:
 *   harvey.LLMClient — ready-to-use client; caller should Close() when done
 *
 * Example:
 *   client := newAssayClient("http://localhost:11434", "llama3.2:3b")
 *   defer client.Close()
 */
func newAssayClient(baseURL, model string) harvey.LLMClient {
	return harvey.NewLlamafileLLMClient(baseURL+"/v1", model, 120*time.Second)
}

// ─── Ollama model-list helper ─────────────────────────────────────────────────

func listOllamaModels(baseURL string) ([]string, error) {
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama list: %w", err)
	}
	defer resp.Body.Close()
	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("ollama list decode: %w", err)
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		// skip embedding-only models
		if strings.Contains(m.Name, "embed") {
			continue
		}
		names = append(names, m.Name)
	}
	return names, nil
}

/** listOpenAIModels queries GET /v1/models on an OpenAI-compatible server and
 * returns the model IDs reported. llama-server returns only the loaded model.
 *
 * Parameters:
 *   baseURL (string) — bare base URL, e.g. "http://localhost:8081"
 *
 * Returns:
 *   []string — model IDs (empty IDs are skipped)
 *   error    — non-nil on HTTP or JSON error
 *
 * Example:
 *   models, err := listOpenAIModels("http://localhost:8081")
 */
func listOpenAIModels(baseURL string) ([]string, error) {
	resp, err := http.Get(baseURL + "/v1/models")
	if err != nil {
		return nil, fmt.Errorf("openai models list: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("openai models decode: %w", err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// ─── RAG helpers ─────────────────────────────────────────────────────────────

// ragMinScore is the minimum cosine similarity for a chunk to be injected.
// Matches the threshold used by Harvey's interactive chat loop.
const ragMinScore = 0.3

// buildRAGContext queries the RAG store for chunks relevant to promptText and
// returns a formatted context prefix ready to prepend to the prompt, plus the
// number of chunks included. Returns ("", 0) when no relevant chunks are found
// or the store cannot be queried.
func buildRAGContext(store *harvey.RagStore, embedder harvey.Embedder, promptText string, topK int) (string, int) {
	chunks, err := store.Query(promptText, embedder, topK)
	if err != nil || len(chunks) == 0 {
		return "", 0
	}
	var relevant []harvey.Chunk
	for _, c := range chunks {
		if c.Score >= ragMinScore {
			relevant = append(relevant, c)
		}
	}
	if len(relevant) == 0 {
		return "", 0
	}
	var sb strings.Builder
	sb.WriteString("### Context (from knowledge base)\n\n")
	for i, c := range relevant {
		if c.Source != "" {
			fmt.Fprintf(&sb, "[%d] (source: %s)\n%s\n\n", i+1, c.Source, c.Content)
		} else {
			fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, c.Content)
		}
	}
	sb.WriteString("---\n\n")
	return sb.String(), len(relevant)
}

// ─── Code extraction ──────────────────────────────────────────────────────────

// extractFirstBlock returns the content of the first fenced code block whose
// language tag matches lang (case-insensitive). Returns "" if none found.
func extractFirstBlock(response, lang string) string {
	lines := strings.Split(response, "\n")
	inFence := false
	var sb strings.Builder
	for _, line := range lines {
		if !inFence {
			if strings.HasPrefix(line, "```") {
				tag := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "```")))
				// accept "go", "go:path/file.go", "go path/file.go"
				if strings.HasPrefix(tag, lang) {
					inFence = true
					sb.Reset()
				}
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			inFence = false
			return strings.TrimRight(sb.String(), "\n")
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return ""
}

// ─── Automated checks ─────────────────────────────────────────────────────────

func runContainsChecks(response string, want, banned []string) []CheckResult {
	var results []CheckResult
	for _, s := range want {
		passed := strings.Contains(response, s)
		detail := ""
		if !passed {
			detail = fmt.Sprintf("%q not found in response", s)
		}
		results = append(results, CheckResult{
			Name:   fmt.Sprintf("contains(%q)", s),
			Passed: passed,
			Detail: detail,
		})
	}
	for _, s := range banned {
		passed := !strings.Contains(response, s)
		detail := ""
		if !passed {
			detail = fmt.Sprintf("forbidden string %q found in response", s)
		}
		results = append(results, CheckResult{
			Name:   fmt.Sprintf("not_contains(%q)", s),
			Passed: passed,
			Detail: detail,
		})
	}
	return results
}

// runCompileCheck extracts the first Go code block from response, writes it
// to a temp directory, and runs go build. Returns the CheckResult and the
// extracted source (for saving to disk).
func runCompileCheck(response string) (src string, compileResult, vetResult CheckResult) {
	compileResult = CheckResult{Name: "compiles"}
	vetResult = CheckResult{Name: "go_vet"}

	src = extractFirstBlock(response, "go")
	if src == "" {
		compileResult.Detail = "no Go code block found in response"
		vetResult.Detail = "skipped (no code block)"
		return src, compileResult, vetResult
	}

	// Ensure there is a package declaration so go build won't reject it.
	if !strings.Contains(src, "package ") {
		src = "package assaytest\n\n" + src
	}

	dir, err := os.MkdirTemp("", "assay-compile-*")
	if err != nil {
		compileResult.Detail = "could not create temp dir: " + err.Error()
		vetResult.Detail = "skipped"
		return src, compileResult, vetResult
	}
	defer os.RemoveAll(dir)

	goFile := filepath.Join(dir, "assay_test.go")
	if err := os.WriteFile(goFile, []byte(src), 0644); err != nil {
		compileResult.Detail = "could not write temp file: " + err.Error()
		vetResult.Detail = "skipped"
		return src, compileResult, vetResult
	}

	// Write a minimal go.mod so go build doesn't require the workspace.
	goMod := "module assaytest\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		compileResult.Detail = "could not write go.mod: " + err.Error()
		vetResult.Detail = "skipped"
		return src, compileResult, vetResult
	}

	buildOut, err := exec.Command("go", "build", "./...").CombinedOutput()
	compileResult.Passed = err == nil
	if err != nil {
		compileResult.Detail = strings.TrimSpace(string(buildOut))
		vetResult.Detail = "skipped (build failed)"
		return src, compileResult, vetResult
	}

	vetOut, err := exec.Command("go", "vet", "./...").CombinedOutput()
	vetResult.Passed = err == nil
	if err != nil {
		vetResult.Detail = strings.TrimSpace(string(vetOut))
	}
	return src, compileResult, vetResult
}

// runChecks executes all automated checks for a prompt against the response.
// It saves extracted code blocks to extractedDir/<promptID>.<ext> when present.
func runChecks(p Prompt, response, extractedDir string) ([]CheckResult, string) {
	var results []CheckResult
	var extractedSrc string

	results = append(results, runContainsChecks(response, p.Checks.Contains, p.Checks.NotContains)...)

	if p.Checks.Compiles {
		src, cr, vr := runCompileCheck(response)
		extractedSrc = src
		results = append(results, cr)
		if p.Checks.GoVet {
			results = append(results, vr)
		}
	}

	// Save extracted source if we have one and a destination dir.
	if extractedSrc != "" && extractedDir != "" {
		ext := extensionForLang(p.Language)
		outPath := filepath.Join(extractedDir, p.ID+ext)
		_ = os.WriteFile(outPath, []byte(extractedSrc), 0644)
	}

	return results, extractedSrc
}

func extensionForLang(lang string) string {
	switch strings.ToLower(lang) {
	case "go":
		return ".go"
	case "typescript", "ts":
		return ".ts"
	case "javascript", "js":
		return ".js"
	case "sql":
		return ".sql"
	case "css":
		return ".css"
	default:
		return ".txt"
	}
}

func allPassed(results []CheckResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// ─── Output ───────────────────────────────────────────────────────────────────

// sanitizeModelName converts a model name to a filesystem-safe directory name.
func sanitizeModelName(model string) string {
	r := strings.NewReplacer(":", "-", "/", "-", " ", "-")
	return r.Replace(model)
}

func writeReport(outDir string, ar AssayResults, corpus *Corpus) error {
	// Build a prompt lookup by ID for quick access to human questions.
	promptByID := make(map[string]Prompt, len(corpus.Prompts))
	for _, p := range corpus.Prompts {
		promptByID[p.ID] = p
	}

	// Group results by model, then by promptID, then by variant.
	type variantKey struct {
		model    string
		promptID string
		variant  string
	}
	byVariant := make(map[variantKey]PromptResult)
	var modelOrder []string
	seenModel := make(map[string]bool)
	for _, r := range ar.Results {
		if !seenModel[r.Model] {
			modelOrder = append(modelOrder, r.Model)
			seenModel[r.Model] = true
		}
		byVariant[variantKey{r.Model, r.PromptID, r.Variant}] = r
	}

	// Collect ordered prompt IDs from results (preserving corpus order).
	promptIDOrder := make([]string, 0, len(corpus.Prompts))
	seenPrompt := make(map[string]bool)
	for _, p := range corpus.Prompts {
		if !seenPrompt[p.ID] {
			promptIDOrder = append(promptIDOrder, p.ID)
			seenPrompt[p.ID] = true
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Assay Report\n\n")
	fmt.Fprintf(&sb, "Run at: %s  \n", ar.RunAt.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Backend: %s  \n", ar.Backend)
	if ar.LlamafilePath != "" {
		fmt.Fprintf(&sb, "Binary: `%s`  \n", ar.LlamafilePath)
	}
	if ar.BackendURL != "" {
		fmt.Fprintf(&sb, "URL: %s  \n", ar.BackendURL)
	} else if ar.OllamaURL != "" {
		fmt.Fprintf(&sb, "URL: %s  \n", ar.OllamaURL)
	}
	fmt.Fprintln(&sb)
	if ar.RagDB != "" {
		fmt.Fprintf(&sb, "RAG store: `%s` (embed: %s)\n\n", ar.RagDB, ar.RagEmbedModel)
	}

	// Summary table.
	fmt.Fprintf(&sb, "## Summary\n\n")
	if ar.RagCompare {
		fmt.Fprintf(&sb, "| Model | Base pass | RAG pass | Δ | Avg tok/s |\n")
		fmt.Fprintf(&sb, "|---|---|---|---|---|\n")
		for _, model := range modelOrder {
			basePass, ragPass, total := 0, 0, 0
			var totalTPS float64
			count := 0
			for _, pid := range promptIDOrder {
				base, hasBase := byVariant[variantKey{model, pid, "base"}]
				rag, hasRag := byVariant[variantKey{model, pid, "rag"}]
				if hasBase {
					total++
					if base.AutoPass {
						basePass++
					}
					totalTPS += base.TokensPerSec
					count++
				}
				if hasRag && hasBase {
					if rag.AutoPass {
						ragPass++
					}
				}
			}
			delta := ragPass - basePass
			deltaStr := fmt.Sprintf("%+d", delta)
			avgTPS := 0.0
			if count > 0 {
				avgTPS = totalTPS / float64(count)
			}
			fmt.Fprintf(&sb, "| %s | %d/%d | %d/%d | %s | %.1f |\n",
				model, basePass, total, ragPass, total, deltaStr, avgTPS)
		}
	} else {
		fmt.Fprintf(&sb, "| Model | Prompts | Auto-pass | Avg tok/s |\n")
		fmt.Fprintf(&sb, "|---|---|---|---|\n")
		for _, model := range modelOrder {
			pass := 0
			var totalTPS float64
			count := 0
			for _, r := range ar.Results {
				if r.Model != model {
					continue
				}
				if r.AutoPass {
					pass++
				}
				totalTPS += r.TokensPerSec
				count++
			}
			total := count
			avgTPS := 0.0
			if count > 0 {
				avgTPS = totalTPS / float64(count)
			}
			fmt.Fprintf(&sb, "| %s | %d | %d/%d | %.1f |\n",
				model, total, pass, total, avgTPS)
		}
	}
	sb.WriteString("\n")

	// Per-model detail.
	for _, model := range modelOrder {
		fmt.Fprintf(&sb, "---\n\n## Model: %s\n\n", model)

		for _, pid := range promptIDOrder {
			p, ok := promptByID[pid]
			if !ok {
				continue
			}

			if ar.RagCompare {
				base, hasBase := byVariant[variantKey{model, pid, "base"}]
				rag, hasRag := byVariant[variantKey{model, pid, "rag"}]
				if !hasBase && !hasRag {
					continue
				}
				fmt.Fprintf(&sb, "### %s\n\n", pid)
				fmt.Fprintf(&sb, "**%s**\n\n", p.Description)
				fmt.Fprintf(&sb, "| | Base | RAG |\n|---|---|---|\n")
				baseLabel, ragLabel := "—", "—"
				if hasBase {
					if base.AutoPass {
						baseLabel = "✓ PASS"
					} else {
						baseLabel = "✗ FAIL"
					}
				}
				if hasRag {
					if rag.AutoPass {
						ragLabel = "✓ PASS"
					} else {
						ragLabel = "✗ FAIL"
					}
				}
				fmt.Fprintf(&sb, "| Auto checks | %s | %s |\n", baseLabel, ragLabel)
				if hasBase && hasRag {
					fmt.Fprintf(&sb, "| Chunks injected | — | %d |\n", rag.RagChunks)
					fmt.Fprintf(&sb, "| Tokens/s | %.1f | %.1f |\n", base.TokensPerSec, rag.TokensPerSec)
					fmt.Fprintf(&sb, "| Elapsed | %s | %s |\n",
						base.Elapsed.Round(time.Millisecond),
						rag.Elapsed.Round(time.Millisecond))
				}
				sb.WriteString("\n")

				// Per-check delta table.
				if hasBase && hasRag && len(base.Checks) > 0 {
					fmt.Fprintf(&sb, "| Check | Base | RAG |\n|---|---|---|\n")
					for i, cr := range base.Checks {
						baseMark := "✓"
						if !cr.Passed {
							baseMark = "✗"
						}
						ragMark := "—"
						if i < len(rag.Checks) {
							if rag.Checks[i].Passed {
								ragMark = "✓"
							} else {
								ragMark = "✗"
							}
						}
						fmt.Fprintf(&sb, "| %s | %s | %s |\n", cr.Name, baseMark, ragMark)
					}
					sb.WriteString("\n")
				}

				// Collapsed responses.
				lang := p.Language
				if lang == "" {
					lang = "text"
				}
				if hasBase {
					fmt.Fprintf(&sb, "<details><summary>Base response</summary>\n\n```%s\n%s\n```\n\n</details>\n\n",
						lang, strings.TrimSpace(base.Response))
				}
				if hasRag {
					fmt.Fprintf(&sb, "<details><summary>RAG response (%d chunks)</summary>\n\n```%s\n%s\n```\n\n</details>\n\n",
						rag.RagChunks, lang, strings.TrimSpace(rag.Response))
				}
			} else {
				// Normal (non-compare) mode: show single result.
				r, ok := byVariant[variantKey{model, pid, ar.Results[0].Variant}]
				if !ok {
					// Fall back to scanning by model+prompt.
					for _, res := range ar.Results {
						if res.Model == model && res.PromptID == pid {
							r = res
							ok = true
							break
						}
					}
				}
				if !ok {
					continue
				}

				fmt.Fprintf(&sb, "### %s\n\n", pid)
				fmt.Fprintf(&sb, "**%s**  \n", p.Description)

				variantNote := ""
				if r.Variant == "rag" {
					variantNote = fmt.Sprintf(" · RAG (%d chunks)", r.RagChunks)
				}
				elapsed := r.Elapsed.Round(time.Millisecond)
				fmt.Fprintf(&sb, "**Timing**: %d prompt + %d reply tokens · %s · %.1f tok/s%s  \n",
					r.PromptTokens, r.ReplyTokens, elapsed, r.TokensPerSec, variantNote)

				passCount, total := 0, len(r.Checks)
				for _, cr := range r.Checks {
					if cr.Passed {
						passCount++
					}
				}
				autoLabel := "PASS"
				if !r.AutoPass {
					autoLabel = "FAIL"
				}
				fmt.Fprintf(&sb, "**Auto checks**: %s (%d/%d)\n\n", autoLabel, passCount, total)

				if len(r.Checks) > 0 {
					fmt.Fprintf(&sb, "| Check | Result | Detail |\n|---|---|---|\n")
					for _, cr := range r.Checks {
						mark := "✓"
						if !cr.Passed {
							mark = "✗"
						}
						fmt.Fprintf(&sb, "| %s | %s | %s |\n", cr.Name, mark, cr.Detail)
					}
					sb.WriteString("\n")
				}

				lang := p.Language
				if lang == "" {
					lang = "text"
				}
				fmt.Fprintf(&sb, "<details><summary>Response</summary>\n\n```%s\n%s\n```\n\n</details>\n\n",
					lang, strings.TrimSpace(r.Response))
			}

			// Human review questions (same regardless of mode).
			if len(p.Human) > 0 {
				fmt.Fprintf(&sb, "**Human review**:\n\n")
				for _, q := range p.Human {
					fmt.Fprintf(&sb, "- [ ] %s\n", q)
				}
				sb.WriteString("\n")
			}

			fmt.Fprintf(&sb, "**Notes**: \n\n")
		}
	}

	return os.WriteFile(filepath.Join(outDir, "report.md"), []byte(sb.String()), 0644)
}

func writeJSON(outDir string, ar AssayResults) error {
	src, err := json.MarshalIndent(ar, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "results.json"), src, 0644)
}

// ─── Workspace helpers ────────────────────────────────────────────────────────

// findWorkspaceRoot walks up from start looking for the directory containing
// agents/harvey.yaml. Returns "" if not found before the filesystem root.
func findWorkspaceRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "agents", "harvey.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// defaultOutputDir returns $WORKSPACE/assay-results/assay-TIMESTAMP/ when run
// inside a Harvey workspace, or assay-results/assay-TIMESTAMP/ relative to cwd
// when no workspace is found.
func defaultOutputDir() string {
	cwd, _ := os.Getwd()
	ts := time.Now().Format("20060102-150405")
	if root := findWorkspaceRoot(cwd); root != "" {
		return filepath.Join(root, "assay-results", "assay-"+ts)
	}
	return filepath.Join("assay-results", "assay-"+ts)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	appName := filepath.Base(os.Args[0])
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-h", "-help", "--help":
			fmt.Print(harvey.FmtHelp(harvey.AssayHelpText, appName, harvey.Version, harvey.ReleaseDate, harvey.ReleaseHash))
			os.Exit(0)
		case "-v", "-version", "--version":
			fmt.Printf("%s %s %s\n", appName, harvey.Version, harvey.ReleaseHash)
			os.Exit(0)
		}
	}

	corpusPath    := flag.String("corpus", "agents/assay/corpus.yaml", "path to corpus YAML")
	modelsFlag    := flag.String("models", "", "comma-separated model list (default: all from Ollama)")
	category      := flag.String("category", "", "only run prompts from this category")
	ollamaURL     := flag.String("ollama", "http://localhost:11434", "Ollama base URL")
	llamafilePath := flag.String("llamafile", "", "path to a llamafile binary to evaluate; starts and stops the process automatically")
	llamacppURL   := flag.String("llamacpp", "", "base URL of a running llama-server (e.g. http://localhost:8081); user manages the process")
	outputDir     := flag.String("output", defaultOutputDir(),
		"write report and results to PATH\n\t\t\t(default: $WORKSPACE/assay-results/assay-TIMESTAMP/\n\t\t\t or assay-results/assay-TIMESTAMP/ if not in a workspace)")
	ragDB         := flag.String("rag-db", "", "RAG store SQLite path; enables RAG context injection when set")
	ragEmbedModel := flag.String("rag-embed-model", "nomic-embed-text", "embedding model for RAG queries")
	ragTopK       := flag.Int("rag-top-k", 3, "number of RAG chunks to retrieve per prompt")
	ragCompare    := flag.Bool("rag-compare", false, "run each prompt twice (base + RAG) and show delta; requires --rag-db")
	flag.Parse()

	if *ragCompare && *ragDB == "" {
		fmt.Fprintln(os.Stderr, "assay: --rag-compare requires --rag-db")
		os.Exit(1)
	}
	if *llamafilePath != "" && *llamacppURL != "" {
		fmt.Fprintln(os.Stderr, "assay: --llamafile and --llamacpp are mutually exclusive")
		os.Exit(1)
	}

	// Backend selection: determine llmURL, backend name, and start any managed process.
	llmURL    := *ollamaURL
	backend   := "Ollama"
	backendURL := ""

	switch {
	case *llamafilePath != "":
		// RAG + llamafile requires Ollama for embeddings.
		if *ragDB != "" && !harvey.ProbeLlamafile(*ollamaURL+"/api/tags") {
			fmt.Fprintf(os.Stderr, "assay: RAG evaluation with --llamafile requires Ollama for embeddings.\n"+
				"Start Ollama or use --ollama to specify a running instance.\nOllama URL: %s\n", *ollamaURL)
			os.Exit(1)
		}
		port, err := harvey.FindFreePort()
		if err != nil {
			fmt.Fprintf(os.Stderr, "assay: llamafile: cannot find free port: %v\n", err)
			os.Exit(1)
		}
		llmURL = fmt.Sprintf("http://localhost:%d", port)
		fmt.Printf("Starting llamafile %s on %s ...\n", filepath.Base(*llamafilePath), llmURL)
		proc, err := harvey.StartLlamafileService(*llamafilePath, llmURL, "", 30*time.Second, -1, 0, os.Stdout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "assay: llamafile: %v\n", err)
			os.Exit(1)
		}
		defer proc.Kill()
		fmt.Printf("  Llamafile ready at %s\n", llmURL)
		backend    = "Llamafile"
		backendURL = llmURL

	case *llamacppURL != "":
		// RAG + llamacpp requires Ollama for embeddings.
		if *ragDB != "" && !harvey.ProbeLlamafile(*ollamaURL+"/api/tags") {
			fmt.Fprintf(os.Stderr,
				"assay: RAG evaluation with --llamacpp requires Ollama for embeddings.\n"+
					"Start Ollama or use --ollama to specify a running instance.\nOllama URL: %s\n",
				*ollamaURL)
			os.Exit(1)
		}
		llmURL    = *llamacppURL
		backend   = "LlamaCpp"
		backendURL = llmURL
	}

	corpus, err := loadCorpus(*corpusPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "assay: %v\n", err)
		os.Exit(1)
	}

	// Resolve model list.
	var models []string
	switch {
	case *llamafilePath != "":
		// Llamafile exposes a single model; derive its name from the binary.
		models = []string{harvey.LlamafileModelNameFromPath(*llamafilePath)}

	case *llamacppURL != "":
		if *modelsFlag != "" {
			for _, m := range strings.Split(*modelsFlag, ",") {
				if m = strings.TrimSpace(m); m != "" {
					models = append(models, m)
				}
			}
		} else {
			models, err = listOpenAIModels(*llamacppURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "assay: could not list llama.cpp models: %v\n"+
					"Use --models NAME to specify the model explicitly.\n", err)
				os.Exit(1)
			}
		}

	case *modelsFlag != "":
		for _, m := range strings.Split(*modelsFlag, ",") {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}

	default:
		models, err = listOllamaModels(llmURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "assay: could not list Ollama models: %v\n", err)
			os.Exit(1)
		}
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "assay: no models to run (use --models or start Ollama)")
		os.Exit(1)
	}

	// Filter prompts by category.
	var prompts []Prompt
	for _, p := range corpus.Prompts {
		if *category == "" || p.Category == *category {
			prompts = append(prompts, p)
		}
	}
	if len(prompts) == 0 {
		fmt.Fprintf(os.Stderr, "assay: no prompts match category %q\n", *category)
		os.Exit(1)
	}

	// Open RAG store when requested.
	var ragStore *harvey.RagStore
	var ragEmbedder harvey.Embedder
	if *ragDB != "" {
		ragStore, err = harvey.NewRagStore(*ragDB, *ragEmbedModel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "assay: open RAG store: %v\n", err)
			os.Exit(1)
		}
		ragEmbedder = harvey.NewOllamaEmbedder(*ollamaURL, *ragEmbedModel)
		fmt.Printf("RAG store: %s (embed: %s, top-k: %d)\n", *ragDB, *ragEmbedModel, *ragTopK)
		if *ragCompare {
			fmt.Println("Compare mode: each prompt runs twice (base + RAG)")
		}
	}

	// Create output directory.
	outDir := *outputDir
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "assay: could not create output dir: %v\n", err)
		os.Exit(1)
	}

	ar := AssayResults{
		RunAt:         time.Now(),
		Backend:       backend,
		LlamafilePath: *llamafilePath,
		OllamaURL:     *ollamaURL,
		BackendURL:    backendURL,
		RagDB:         *ragDB,
		RagEmbedModel: *ragEmbedModel,
		RagCompare:    *ragCompare,
	}

	// Determine which variants to run per prompt.
	// In compare mode: ["base", "rag"]. RAG-only: ["rag"]. Base-only: [""].
	type variant struct {
		name   string
		useRAG bool
	}
	var variants []variant
	switch {
	case *ragCompare:
		variants = []variant{{"base", false}, {"rag", true}}
	case *ragDB != "":
		variants = []variant{{"rag", true}}
	default:
		variants = []variant{{"", false}}
	}

	total := len(models) * len(prompts) * len(variants)
	done := 0

	for _, model := range models {
		// Create one LLMClient per model; the client is stateless so this is cheap.
		client := newAssayClient(llmURL, model)

		// Per-model extracted code directory.
		extractedDir := filepath.Join(outDir, "extracted", sanitizeModelName(model))
		if err := os.MkdirAll(extractedDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "assay: mkdir extracted: %v\n", err)
		}

		for _, p := range prompts {
			for _, v := range variants {
				done++
				variantLabel := ""
				if v.name != "" {
					variantLabel = "[" + v.name + "] "
				}
				fmt.Printf("[%d/%d] %s%s · %s ... ", done, total, variantLabel, model, p.ID)

				// Build prompt text, injecting RAG context when requested.
				promptText := p.PromptText
				ragChunks := 0
				if v.useRAG && ragStore != nil {
					ctx, n := buildRAGContext(ragStore, ragEmbedder, promptText, *ragTopK)
					if n > 0 {
						promptText = ctx + promptText
						ragChunks = n
					}
				}

				start := time.Now()
				var buf strings.Builder
				stats, callErr := client.Chat(context.Background(), []harvey.Message{{Role: "user", Content: promptText}}, &buf)
				response := buf.String()
				elapsed := time.Since(start)

				if callErr != nil {
					fmt.Printf("ERROR: %v\n", callErr)
					ar.Results = append(ar.Results, PromptResult{
						PromptID: p.ID,
						Category: p.Category,
						Model:    model,
						Variant:  v.name,
						Response: "ERROR: " + callErr.Error(),
						Elapsed:  elapsed,
						Checks:   []CheckResult{{Name: "call", Passed: false, Detail: callErr.Error()}},
					})
					continue
				}

				checks, _ := runChecks(p, response, extractedDir)

				pr := PromptResult{
					PromptID:     p.ID,
					Category:     p.Category,
					Model:        model,
					Variant:      v.name,
					Response:     response,
					Elapsed:      elapsed,
					PromptTokens: stats.PromptTokens,
					ReplyTokens:  stats.ReplyTokens,
					TokensPerSec: stats.TokensPerSec,
					Checks:       checks,
					AutoPass:     allPassed(checks),
					RagChunks:    ragChunks,
				}
				ar.Results = append(ar.Results, pr)

				passLabel := "PASS"
				if !pr.AutoPass {
					passLabel = "FAIL"
				}
				chunkNote := ""
				if ragChunks > 0 {
					chunkNote = fmt.Sprintf(" · %d RAG chunks", ragChunks)
				}
				fmt.Printf("%s · %s · %.1f tok/s%s\n",
					passLabel, elapsed.Round(time.Millisecond), stats.TokensPerSec, chunkNote)
			}
		}

		_ = client.Close()
	}

	if err := writeReport(outDir, ar, corpus); err != nil {
		fmt.Fprintf(os.Stderr, "assay: write report: %v\n", err)
	}
	if err := writeJSON(outDir, ar); err != nil {
		fmt.Fprintf(os.Stderr, "assay: write JSON: %v\n", err)
	}

	fmt.Printf("\nResults written to %s/\n", outDir)
}
