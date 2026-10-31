// assay runs evaluation prompts from a corpus YAML against one or more
// LLM backends and writes a Markdown report plus JSON results for human
// review and machine processing. Supports Ollama.
package main

import (
	"context"
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
// Variant is "base" (no guide), "guide" (the guide file as a system message), or ""
// (not a comparison run).
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
}

type AssayResults struct {
	RunAt         time.Time      `json:"run_at"`
	Backend       string         `json:"backend"`                  // always "Ollama"
	OllamaURL     string         `json:"ollama_url,omitempty"`
	GuideFile     string         `json:"guide_file,omitempty"`
	GuideCompare  bool           `json:"guide_compare,omitempty"`
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
		return nil, harvey.Dataf("parse corpus: %w", err)
	}
	if len(c.Prompts) == 0 {
		return nil, harvey.Dataf("corpus %s has no prompts", path)
	}
	return &c, nil
}

// ─── LLM client helpers ───────────────────────────────────────────────────────

/** newAssayClient creates an LLMClient backed by an Ollama server.
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
	return harvey.NewOllamaLLMClient(baseURL, model, 120*time.Second)
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
		return nil, harvey.Unavailablef("ollama list decode: %w", err)
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

// ─── Guide-compare helpers ────────────────────────────────────────────────────

// buildGuideMessages returns the message list for one prompt dispatch. When
// useGuide is true and guideText is non-empty, guideText is sent as a system
// message ahead of the user prompt; otherwise the message list is exactly
// what assay has always sent (a single user message) — the "base" variant of
// --guide-compare is identical to today's non-compare dispatch, not a new
// behavior of its own.
func buildGuideMessages(guideText, promptText string, useGuide bool) []harvey.Message {
	if useGuide && guideText != "" {
		return []harvey.Message{
			{Role: "system", Content: guideText},
			{Role: "user", Content: promptText},
		}
	}
	return []harvey.Message{{Role: "user", Content: promptText}}
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
	if ar.OllamaURL != "" {
		fmt.Fprintf(&sb, "URL: %s  \n", ar.OllamaURL)
	}
	fmt.Fprintln(&sb)

	// Summary table.
	fmt.Fprintf(&sb, "## Summary\n\n")
	if ar.GuideCompare {
		fmt.Fprintf(&sb, "| Model | Base pass | Guide pass | Δ | Avg tok/s |\n")
		fmt.Fprintf(&sb, "|---|---|---|---|---|\n")
		for _, model := range modelOrder {
			basePass, guidePass, total := 0, 0, 0
			var totalTPS float64
			count := 0
			for _, pid := range promptIDOrder {
				base, hasBase := byVariant[variantKey{model, pid, "base"}]
				guide, hasGuide := byVariant[variantKey{model, pid, "guide"}]
				if hasBase {
					total++
					if base.AutoPass {
						basePass++
					}
					totalTPS += base.TokensPerSec
					count++
				}
				if hasGuide && hasBase {
					if guide.AutoPass {
						guidePass++
					}
				}
			}
			delta := guidePass - basePass
			deltaStr := fmt.Sprintf("%+d", delta)
			avgTPS := 0.0
			if count > 0 {
				avgTPS = totalTPS / float64(count)
			}
			fmt.Fprintf(&sb, "| %s | %d/%d | %d/%d | %s | %.1f |\n",
				model, basePass, total, guidePass, total, deltaStr, avgTPS)
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

			if ar.GuideCompare {
				base, hasBase := byVariant[variantKey{model, pid, "base"}]
				guide, hasGuide := byVariant[variantKey{model, pid, "guide"}]
				if !hasBase && !hasGuide {
					continue
				}
				fmt.Fprintf(&sb, "### %s\n\n", pid)
				fmt.Fprintf(&sb, "**%s**\n\n", p.Description)
				fmt.Fprintf(&sb, "| | Base | Guide |\n|---|---|---|\n")
				baseLabel, guideLabel := "—", "—"
				if hasBase {
					if base.AutoPass {
						baseLabel = "✓ PASS"
					} else {
						baseLabel = "✗ FAIL"
					}
				}
				if hasGuide {
					if guide.AutoPass {
						guideLabel = "✓ PASS"
					} else {
						guideLabel = "✗ FAIL"
					}
				}
				fmt.Fprintf(&sb, "| Auto checks | %s | %s |\n", baseLabel, guideLabel)
				if hasBase && hasGuide {
					fmt.Fprintf(&sb, "| Tokens/s | %.1f | %.1f |\n", base.TokensPerSec, guide.TokensPerSec)
					fmt.Fprintf(&sb, "| Elapsed | %s | %s |\n",
						base.Elapsed.Round(time.Millisecond),
						guide.Elapsed.Round(time.Millisecond))
				}
				sb.WriteString("\n")

				// Per-check delta table.
				if hasBase && hasGuide && len(base.Checks) > 0 {
					fmt.Fprintf(&sb, "| Check | Base | Guide |\n|---|---|---|\n")
					for i, cr := range base.Checks {
						baseMark := "✓"
						if !cr.Passed {
							baseMark = "✗"
						}
						guideMark := "—"
						if i < len(guide.Checks) {
							if guide.Checks[i].Passed {
								guideMark = "✓"
							} else {
								guideMark = "✗"
							}
						}
						fmt.Fprintf(&sb, "| %s | %s | %s |\n", cr.Name, baseMark, guideMark)
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
				if hasGuide {
					fmt.Fprintf(&sb, "<details><summary>Guide response</summary>\n\n```%s\n%s\n```\n\n</details>\n\n",
						lang, strings.TrimSpace(guide.Response))
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
	os.Exit(mainRun(os.Args, os.Stdout, os.Stderr))
}

// assayFlags holds the value each assay option was parsed into.
type assayFlags struct {
	corpusPath *string
	modelsFlag *string
	category *string
	ollamaURL *string
	outputDir *string
	guideFile *string
	guideCompare *bool
}

// defineAssayFlags registers every assay option on fs. It is the single list
// of them: mainRun parses with it, and the enforcement test compares it with
// the OPTIONS section of the manual.
func defineAssayFlags(fs *flag.FlagSet) assayFlags {
	corpusPath    := fs.String("corpus", "agents/assay/corpus.yaml", "path to corpus YAML")
	modelsFlag    := fs.String("models", "", "comma-separated model list (default: all from Ollama)")
	category      := fs.String("category", "", "only run prompts from this category")
	ollamaURL     := fs.String("ollama", "http://localhost:11434", "Ollama base URL")
	outputDir     := fs.String("output", defaultOutputDir(),
		"write report and results to PATH\n\t\t\t(default: $WORKSPACE/assay-results/assay-TIMESTAMP/\n\t\t\t or assay-results/assay-TIMESTAMP/ if not in a workspace)")
	guideFile     := fs.String("guide-file", "", "path to a text file whose content becomes the system message for the 'guide' variant")
	guideCompare  := fs.Bool("guide-compare", false, "run each prompt twice (base + guide) and show delta; requires --guide-file")
	return assayFlags{corpusPath: corpusPath, modelsFlag: modelsFlag, category: category, ollamaURL: ollamaURL, outputDir: outputDir, guideFile: guideFile, guideCompare: guideCompare}
}

// mainRun is the whole of assay's command line, with the process's arguments and
// streams passed in so it can be tested. args[0] is the program name. It returns
// the exit status; main only calls os.Exit with it. --json is recognised here,
// wherever it sits on the line, so it still applies when a later flag — or the
// flag package's own parse — is what fails.
func mainRun(args []string, out, errOut io.Writer) int {
	jsonOut, args := harvey.ExtractJSONFlag(args)
	appName := filepath.Base(args[0])
	for _, arg := range args[1:] {
		switch arg {
		case "-h", "-help", "--help":
			fmt.Fprint(out, harvey.FmtHelp(harvey.AssayHelpText, appName, harvey.Version, harvey.ReleaseDate, harvey.ReleaseHash))
			return 0
		case "-v", "-version", "--version":
			fmt.Fprintf(out, "%s %s %s\n", appName, harvey.Version, harvey.ReleaseHash)
			return 0
		}
	}

	// fail reports err and returns the exit status of its class; usageFail is
	// the same for a plain-text usage message assay checks by hand (not the
	// flag package's own). Both honour --json.
	fail := func(err error) int {
		if jsonOut {
			return harvey.PrintJSONError(errOut, err)
		}
		fmt.Fprintf(errOut, "assay: %v\n", err)
		return harvey.ExitCodeFor(err).Code
	}
	usageFail := func(msg string) int { return fail(harvey.Usagef("%s", msg)) }

	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	if jsonOut {
		fs.SetOutput(io.Discard)
	} else {
		fs.SetOutput(errOut)
	}
	f := defineAssayFlags(fs)
	corpusPath := f.corpusPath
	modelsFlag := f.modelsFlag
	category := f.category
	ollamaURL := f.ollamaURL
	outputDir := f.outputDir
	guideFile := f.guideFile
	guideCompare := f.guideCompare
	if err := fs.Parse(args[1:]); err != nil {
		if jsonOut {
			return harvey.PrintJSONError(errOut, harvey.Usagef("%v", err))
		}
		return 2 // the flag set has already described the problem on errOut
	}
	if fs.NArg() > 0 {
		return usageFail(fmt.Sprintf("unexpected argument: %s", fs.Arg(0)))
	}

	if *guideCompare && *guideFile == "" {
		return usageFail("--guide-compare requires --guide-file")
	}

	// A run does everything it can, then exits with the class of the first
	// failed step (workspace DR-0003: no exit 0 with failures). note remembers
	// it; failedCalls counts the model calls that errored.
	var firstFailure error
	failedCalls := 0
	note := func(err error) {
		if firstFailure == nil {
			firstFailure = err
		}
	}

	llmURL := *ollamaURL

	corpus, err := loadCorpus(*corpusPath)
	if err != nil {
		return fail(err)
	}

	// Resolve model list.
	var models []string
	switch {
	case *modelsFlag != "":
		for _, m := range strings.Split(*modelsFlag, ",") {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}

	default:
		models, err = listOllamaModels(llmURL)
		if err != nil {
			return fail(fmt.Errorf("could not list Ollama models: %w", err))
		}
	}
	if len(models) == 0 {
		return fail(harvey.Negativef("no models to run (use --models or start Ollama)"))
	}

	// Filter prompts by category.
	var prompts []Prompt
	for _, p := range corpus.Prompts {
		if *category == "" || p.Category == *category {
			prompts = append(prompts, p)
		}
	}
	if len(prompts) == 0 {
		return fail(harvey.Negativef("no prompts match category %q", *category))
	}

	// Create output directory.
	outDir := *outputDir
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fail(harvey.AsCreate(fmt.Errorf("could not create output dir: %w", err)))
	}

	ar := AssayResults{
		RunAt:         time.Now(),
		Backend:       "Ollama",
		OllamaURL:     *ollamaURL,
		GuideFile:     *guideFile,
		GuideCompare:  *guideCompare,
	}

	// Determine which variants to run per prompt: base and guide in compare
	// mode, otherwise one unnamed run.
	type variant struct {
		name     string
		useGuide bool
	}
	var variants []variant
	switch {
	case *guideCompare:
		variants = []variant{{"base", false}, {"guide", true}}
	default:
		variants = []variant{{"", false}}
	}

	// Load the guide file's content once, when configured.
	var guideText string
	if *guideFile != "" {
		data, err := os.ReadFile(*guideFile)
		if err != nil {
			return fail(fmt.Errorf("read guide file: %w", err))
		}
		guideText = strings.TrimSpace(string(data))
	}

	total := len(models) * len(prompts) * len(variants)
	done := 0

	for _, model := range models {
		// Create one LLMClient per model; the client is stateless so this is cheap.
		client := newAssayClient(llmURL, model)

		// Per-model extracted code directory.
		extractedDir := filepath.Join(outDir, "extracted", sanitizeModelName(model))
		if err := os.MkdirAll(extractedDir, 0755); err != nil {
			e := harvey.AsCreate(fmt.Errorf("mkdir extracted: %w", err))
			fmt.Fprintf(errOut, "assay: %v\n", e)
			note(e)
		}

		for _, p := range prompts {
			for _, v := range variants {
				done++
				variantLabel := ""
				if v.name != "" {
					variantLabel = "[" + v.name + "] "
				}
				fmt.Fprintf(out, "[%d/%d] %s%s · %s ... ", done, total, variantLabel, model, p.ID)

				promptText := p.PromptText

				messages := buildGuideMessages(guideText, promptText, v.useGuide)

				start := time.Now()
				var buf strings.Builder
				stats, callErr := client.Chat(context.Background(), messages, &buf)
				response := buf.String()
				elapsed := time.Since(start)

				if callErr != nil {
					fmt.Fprintf(out, "ERROR: %v\n", callErr)
					failedCalls++
					note(callFailure(callErr))
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
				}
				ar.Results = append(ar.Results, pr)

				passLabel := "PASS"
				if !pr.AutoPass {
					passLabel = "FAIL"
				}
				fmt.Fprintf(out, "%s · %s · %.1f tok/s\n",
					passLabel, elapsed.Round(time.Millisecond), stats.TokensPerSec)
			}
		}

		_ = client.Close()
	}

	if err := writeReport(outDir, ar, corpus); err != nil {
		e := writeFailure(fmt.Errorf("write report: %w", err))
		fmt.Fprintf(errOut, "assay: %v\n", e)
		note(e)
	}
	if err := writeJSON(outDir, ar); err != nil {
		e := writeFailure(fmt.Errorf("write JSON: %w", err))
		fmt.Fprintf(errOut, "assay: %v\n", e)
		note(e)
	}

	fmt.Fprintf(out, "\nResults written to %s/\n", outDir)
	if firstFailure != nil {
		if failedCalls > 0 {
			fmt.Fprintf(errOut, "assay: %d model call(s) failed\n", failedCalls)
		}
		return harvey.ExitCodeFor(firstFailure).Code
	}
	return 0
}

// callFailure classifies a failed model call. One that classifies itself (a
// refused connection is unavailable) keeps its class; any other failure of the
// call, such as an HTTP error from the server, means the service could not do
// the work, which is also unavailable.
func callFailure(err error) error {
	if _, ok := harvey.ExitClassOf(err); ok {
		return err
	}
	return harvey.Unavailablef("%w", err)
}

// writeFailure classifies a failure to write a result file: the operating
// system's refusal is no_permission and anything else is io (a write that
// failed), including a target that vanished or is not a file.
func writeFailure(err error) error {
	if harvey.ExitCodeFor(err) == harvey.ClassNoPermission {
		return err
	}
	return harvey.ClassedAs(harvey.ClassIO, err)
}
