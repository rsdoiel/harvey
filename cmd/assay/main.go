// assay runs evaluation prompts from a corpus YAML against one or more
// Ollama models and writes a Markdown report plus JSON results for human
// review and machine processing.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

// ─── Ollama API types ─────────────────────────────────────────────────────────

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Model             string        `json:"model"`
	Message           ollamaMessage `json:"message"`
	Done              bool          `json:"done"`
	PromptEvalCount   int           `json:"prompt_eval_count"`
	EvalCount         int           `json:"eval_count"`
	EvalDuration      int64         `json:"eval_duration"` // nanoseconds
}

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

type PromptResult struct {
	PromptID     string        `json:"prompt_id"`
	Category     string        `json:"category"`
	Model        string        `json:"model"`
	Response     string        `json:"response"`
	Elapsed      time.Duration `json:"elapsed_ns"`
	PromptTokens int           `json:"prompt_tokens"`
	ReplyTokens  int           `json:"reply_tokens"`
	TokensPerSec float64       `json:"tokens_per_sec"`
	Checks       []CheckResult `json:"checks"`
	AutoPass     bool          `json:"auto_pass"` // true when all automated checks pass
}

type AssayResults struct {
	RunAt     time.Time      `json:"run_at"`
	OllamaURL string         `json:"ollama_url"`
	Results   []PromptResult `json:"results"`
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

// ─── Ollama helpers ───────────────────────────────────────────────────────────

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

func callOllama(baseURL, model, prompt string) (string, ollamaResponse, error) {
	req := ollamaRequest{
		Model:    model,
		Messages: []ollamaMessage{{Role: "user", Content: prompt}},
		Stream:   false,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", ollamaResponse{}, err
	}
	resp, err := http.Post(baseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", ollamaResponse{}, fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ollamaResponse{}, fmt.Errorf("ollama read body: %w", err)
	}
	var or ollamaResponse
	if err := json.Unmarshal(raw, &or); err != nil {
		return "", ollamaResponse{}, fmt.Errorf("ollama decode: %w", err)
	}
	return or.Message.Content, or, nil
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

	// Group results by model.
	byModel := make(map[string][]PromptResult)
	var modelOrder []string
	seen := make(map[string]bool)
	for _, r := range ar.Results {
		if !seen[r.Model] {
			modelOrder = append(modelOrder, r.Model)
			seen[r.Model] = true
		}
		byModel[r.Model] = append(byModel[r.Model], r)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Assay Report\n\n")
	fmt.Fprintf(&sb, "Run at: %s  \n", ar.RunAt.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Ollama: %s\n\n", ar.OllamaURL)

	// Summary table.
	fmt.Fprintf(&sb, "## Summary\n\n")
	fmt.Fprintf(&sb, "| Model | Prompts | Auto-pass | Avg tok/s |\n")
	fmt.Fprintf(&sb, "|---|---|---|---|\n")
	for _, model := range modelOrder {
		results := byModel[model]
		pass := 0
		var totalTPS float64
		for _, r := range results {
			if r.AutoPass {
				pass++
			}
			totalTPS += r.TokensPerSec
		}
		avgTPS := 0.0
		if len(results) > 0 {
			avgTPS = totalTPS / float64(len(results))
		}
		fmt.Fprintf(&sb, "| %s | %d | %d/%d | %.1f |\n",
			model, len(results), pass, len(results), avgTPS)
	}
	sb.WriteString("\n")

	// Per-model detail.
	for _, model := range modelOrder {
		fmt.Fprintf(&sb, "---\n\n## Model: %s\n\n", model)
		for _, r := range byModel[model] {
			p := promptByID[r.PromptID]
			fmt.Fprintf(&sb, "### %s\n\n", r.PromptID)
			fmt.Fprintf(&sb, "**%s**  \n", p.Description)

			elapsed := r.Elapsed.Round(time.Millisecond)
			fmt.Fprintf(&sb, "**Timing**: %d prompt + %d reply tokens · %s · %.1f tok/s  \n",
				r.PromptTokens, r.ReplyTokens, elapsed, r.TokensPerSec)

			// Check results inline.
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

			// Response (collapsed so the report stays readable).
			lang := p.Language
			if lang == "" {
				lang = "text"
			}
			fmt.Fprintf(&sb, "<details><summary>Response</summary>\n\n```%s\n%s\n```\n\n</details>\n\n",
				lang, strings.TrimSpace(r.Response))

			// Human review questions as a checklist.
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

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	corpusPath := flag.String("corpus", "agents/assay/corpus.yaml", "path to corpus YAML")
	modelsFlag := flag.String("models", "", "comma-separated model list (default: all from Ollama)")
	category := flag.String("category", "", "only run prompts from this category")
	ollamaURL := flag.String("ollama", "http://localhost:11434", "Ollama base URL")
	flag.Parse()

	corpus, err := loadCorpus(*corpusPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "assay: %v\n", err)
		os.Exit(1)
	}

	// Resolve model list.
	var models []string
	if *modelsFlag != "" {
		for _, m := range strings.Split(*modelsFlag, ",") {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}
	} else {
		models, err = listOllamaModels(*ollamaURL)
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

	// Create output directory.
	stamp := time.Now().Format("20060102-150405")
	outDir := filepath.Join("testout", "assay-"+stamp)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "assay: could not create output dir: %v\n", err)
		os.Exit(1)
	}

	ar := AssayResults{RunAt: time.Now(), OllamaURL: *ollamaURL}

	total := len(models) * len(prompts)
	done := 0

	for _, model := range models {
		// Per-model extracted code directory.
		extractedDir := filepath.Join(outDir, "extracted", sanitizeModelName(model))
		if err := os.MkdirAll(extractedDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "assay: mkdir extracted: %v\n", err)
		}

		for _, p := range prompts {
			done++
			fmt.Printf("[%d/%d] %s · %s ... ", done, total, model, p.ID)

			start := time.Now()
			response, or, err := callOllama(*ollamaURL, model, p.PromptText)
			elapsed := time.Since(start)

			if err != nil {
				fmt.Printf("ERROR: %v\n", err)
				ar.Results = append(ar.Results, PromptResult{
					PromptID: p.ID,
					Category: p.Category,
					Model:    model,
					Response: "ERROR: " + err.Error(),
					Elapsed:  elapsed,
					Checks:   []CheckResult{{Name: "call", Passed: false, Detail: err.Error()}},
				})
				continue
			}

			checks, _ := runChecks(p, response, extractedDir)

			tps := 0.0
			if or.EvalDuration > 0 && or.EvalCount > 0 {
				tps = float64(or.EvalCount) / (float64(or.EvalDuration) / 1e9)
			}

			pr := PromptResult{
				PromptID:     p.ID,
				Category:     p.Category,
				Model:        model,
				Response:     response,
				Elapsed:      elapsed,
				PromptTokens: or.PromptEvalCount,
				ReplyTokens:  or.EvalCount,
				TokensPerSec: tps,
				Checks:       checks,
				AutoPass:     allPassed(checks),
			}
			ar.Results = append(ar.Results, pr)

			passLabel := "PASS"
			if !pr.AutoPass {
				passLabel = "FAIL"
			}
			fmt.Printf("%s · %s · %.1f tok/s\n", passLabel, elapsed.Round(time.Millisecond), tps)
		}
	}

	if err := writeReport(outDir, ar, corpus); err != nil {
		fmt.Fprintf(os.Stderr, "assay: write report: %v\n", err)
	}
	if err := writeJSON(outDir, ar); err != nil {
		fmt.Fprintf(os.Stderr, "assay: write JSON: %v\n", err)
	}

	fmt.Printf("\nResults written to %s/\n", outDir)
}
