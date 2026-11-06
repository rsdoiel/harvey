package main

import (
	"path/filepath"
	"strings"
	"testing"

	harvey "github.com/rsdoiel/harvey"
)

// A3 of assay-profile-plan.md (DR-0051 item 2): the corpus is organised by
// capability (category) and a prompt may carry a size hint (tokens).

func corpusWith(prompt string) string {
	return "version: \"1\"\ndescription: t\nprompts:\n" + prompt
}

const sizedPrompt = `  - id: long-1700
    category: long-prompt
    description: d
    language: text
    tokens: 1700
    prompt: hi
    checks:
      contains: ["hi"]
    human: []
    notes: ""
`

func TestLoadCorpus_TokensIsASizeHint(t *testing.T) {
	dir := t.TempDir()
	c, err := loadCorpus(writeFile(t, dir, "c.yaml", corpusWith(sizedPrompt)))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Prompts[0].Tokens; got != 1700 {
		t.Errorf("Tokens = %d, want 1700", got)
	}
}

func TestLoadCorpus_TheShippedCorpusLoadsAndIsWellFormed(t *testing.T) {
	c, err := loadCorpus(filepath.Join("..", "..", "..", "agents", "assay", "corpus.yaml"))
	if err != nil {
		t.Fatalf("agents/assay/corpus.yaml: %v", err)
	}
	counts := map[string]int{}
	for _, p := range c.Prompts {
		counts[p.Category]++
		switch p.Category {
		case "go", "sql":
			if p.Tokens != 0 {
				t.Errorf("%s: the original code prompts have no size hint, got %d", p.ID, p.Tokens)
			}
		case "long-prompt":
			if p.Tokens <= 0 || p.Checks.UsesTail == "" {
				t.Errorf("%s: a long-prompt probe needs tokens: and uses_tail", p.ID)
			}
		case "tool-call":
			if !p.Checks.wantsTools() {
				t.Errorf("%s: a tool-call probe needs tool_call or no_tool", p.ID)
			}
		}
	}
	if counts["go"] != 8 || counts["sql"] != 7 {
		t.Errorf("the original 15 prompts changed: go %d, sql %d, want 8 and 7", counts["go"], counts["sql"])
	}
	for _, cat := range []string{"tool-call", "long-prompt", "instruction", "file-read", "edit"} {
		if counts[cat] == 0 {
			t.Errorf("no %s probes", cat)
		}
	}
}

func TestLoadCorpus_BadValuesAreDataErrors(t *testing.T) {
	for _, tc := range []struct {
		name, prompt, mention string
	}{
		{"negative tokens", strings.Replace(sizedPrompt, "tokens: 1700", "tokens: -5", 1), "long-1700"},
		{"unknown check key", strings.Replace(sizedPrompt, "contains: [\"hi\"]", "contian: [\"hi\"]", 1), "contian"},
		{"unknown prompt key", strings.Replace(sizedPrompt, "tokens: 1700", "tokns: 1700", 1), "tokns"},
		{"empty id", strings.Replace(sizedPrompt, "id: long-1700", "id: \"\"", 1), "id"},
	} {
		_, err := loadCorpus(writeFile(t, t.TempDir(), "c.yaml", corpusWith(tc.prompt)))
		if err == nil {
			t.Errorf("%s: loaded, want an error", tc.name)
			continue
		}
		if got := harvey.ExitCodeFor(err).Code; got != 65 {
			t.Errorf("%s: exit class %d, want 65 (data): %v", tc.name, got, err)
		}
		if !strings.Contains(err.Error(), tc.mention) {
			t.Errorf("%s: %q does not mention %q", tc.name, err, tc.mention)
		}
	}
}

func TestLoadCorpus_DuplicateIDsAreDataErrors(t *testing.T) {
	_, err := loadCorpus(writeFile(t, t.TempDir(), "c.yaml", corpusWith(sizedPrompt+sizedPrompt)))
	if err == nil || harvey.ExitCodeFor(err).Code != 65 || !strings.Contains(err.Error(), "long-1700") {
		t.Errorf("duplicate id: %v, want a data error naming long-1700", err)
	}
}

func TestLoadCorpus_MissingFileStaysNoInput(t *testing.T) {
	_, err := loadCorpus(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || harvey.ExitCodeFor(err).Code != 66 {
		t.Errorf("missing corpus: %v, want 66", err)
	}
}

func TestMainRun_UnmatchedCategoryListsTheCapabilitiesPresent(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", corpusWith(sizedPrompt+strings.Replace(
		strings.Replace(sizedPrompt, "long-1700", "tc-1", 1), "long-prompt", "tool-call", 1)))
	srv := newRecordingServer(t, "m")
	code, _, errOut := runAssay(t, "--corpus", corpus, "--ollama", srv.URL, "--models", "m", "--category", "nosuch")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, want := range []string{"nosuch", "long-prompt", "tool-call"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q should mention %q", errOut, want)
		}
	}
}

func TestMainRun_ResultsRecordThePromptSizeHint(t *testing.T) {
	dir := t.TempDir()
	corpus := writeFile(t, dir, "c.yaml", corpusWith(sizedPrompt))
	srv := newRecordingServer(t, "m")
	out := filepath.Join(dir, "out")
	runAssay(t, "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m")
	rows := readResults(t, out)
	if got, _ := rows[0]["prompt_size_est"].(float64); got != 1700 {
		t.Errorf("prompt_size_est = %v, want 1700", rows[0]["prompt_size_est"])
	}
}

func TestMainRun_CategoryTakesSeveralNamesSeparatedByCommas(t *testing.T) {
	dir := t.TempDir()
	mk := func(id, cat string) string {
		return strings.Replace(strings.Replace(sizedPrompt, "long-1700", id, 1), "long-prompt", cat, 1)
	}
	corpus := writeFile(t, dir, "c.yaml", corpusWith(mk("a", "alpha")+mk("b", "beta")+mk("c", "gamma")))
	srv := newRecordingServer(t, "m")
	out := filepath.Join(dir, "out")
	code, _, errOut := runAssay(t, "--corpus", corpus, "--output", out, "--ollama", srv.URL, "--models", "ollama/m", "--category", "alpha, gamma")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	var ids []string
	for _, r := range readResults(t, out) {
		ids = append(ids, r["prompt_id"].(string))
	}
	if strings.Join(ids, ",") != "a,c" {
		t.Errorf("ran %v, want a and c (alpha and gamma; a space after the comma is allowed)", ids)
	}
	// One name that matches nothing is still reported, with the categories there are.
	code, _, errOut = runAssay(t, "--corpus", corpus, "--ollama", srv.URL, "--models", "ollama/m", "--category", "alpha,nosuch")
	if code != 1 || !strings.Contains(errOut, "nosuch") {
		t.Errorf("exit %d, stderr %q; want 1 naming nosuch", code, errOut)
	}
}
