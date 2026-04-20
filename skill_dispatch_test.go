package harvey

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─── MatchesTrigger ──────────────────────────────────────────────────────────

func TestMatchesTrigger_empty(t *testing.T) {
	skill := &SkillMeta{Trigger: ""}
	if MatchesTrigger(skill, "anything") {
		t.Error("want false when Trigger is empty")
	}
}

func TestMatchesTrigger_keywordHit(t *testing.T) {
	skill := &SkillMeta{Trigger: "pdf extract"}
	if !MatchesTrigger(skill, "please extract the pdf for me") {
		t.Error("want true: prompt contains keyword 'pdf'")
	}
}

func TestMatchesTrigger_keywordMiss(t *testing.T) {
	skill := &SkillMeta{Trigger: "pdf extract"}
	if MatchesTrigger(skill, "summarize the document") {
		t.Error("want false: prompt has no matching keyword")
	}
}

func TestMatchesTrigger_keywordCaseInsensitive(t *testing.T) {
	skill := &SkillMeta{Trigger: "PDF"}
	if !MatchesTrigger(skill, "convert the Pdf file") {
		t.Error("want true: keyword match is case-insensitive")
	}
}

func TestMatchesTrigger_regexpHit(t *testing.T) {
	skill := &SkillMeta{Trigger: `/\bpdf\b/`}
	if !MatchesTrigger(skill, "process a pdf now") {
		t.Error("want true: regexp matches 'pdf'")
	}
}

func TestMatchesTrigger_regexpMiss(t *testing.T) {
	skill := &SkillMeta{Trigger: `/\bpdf\b/`}
	if MatchesTrigger(skill, "there is no match here") {
		t.Error("want false: regexp does not match")
	}
}

func TestMatchesTrigger_malformedRegexp(t *testing.T) {
	skill := &SkillMeta{Trigger: `/[unclosed/`}
	// Must not panic; must return false.
	if MatchesTrigger(skill, "anything") {
		t.Error("want false for malformed regexp")
	}
}

// ─── SortedSkillNames ────────────────────────────────────────────────────────

func TestSortedSkillNames(t *testing.T) {
	cat := SkillCatalog{
		"zebra": {},
		"alpha": {},
		"beta":  {},
	}
	got := SortedSkillNames(cat)
	want := []string{"alpha", "beta", "zebra"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("SortedSkillNames[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

// ─── runCompiledScript ───────────────────────────────────────────────────────

func TestRunCompiledScript_setsEnvVars(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on Windows")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "compiled.bash")
	// Script echoes HARVEY_PROMPT to stdout.
	script := "#!/usr/bin/env bash\necho \"${HARVEY_PROMPT}\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Client = &mockLLMClient{}
	skill := &SkillMeta{Name: "env-test", Path: filepath.Join(dir, "SKILL.md")}

	var out strings.Builder
	reader := bufio.NewReader(strings.NewReader(""))
	_ = reader // runCompiledScript doesn't read from reader

	err := runCompiledScript(context.Background(), a, skill, scriptPath, "hello world", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "hello world") {
		t.Errorf("want 'hello world' in output, got: %q", out.String())
	}
}

func TestRunCompiledScript_injectsHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on Windows")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "compiled.bash")
	script := "#!/usr/bin/env bash\necho \"script output\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	a.Client = &mockLLMClient{}
	skill := &SkillMeta{Name: "hist-test", Path: filepath.Join(dir, "SKILL.md")}
	initialLen := len(a.History)

	var out strings.Builder
	if err := runCompiledScript(context.Background(), a, skill, scriptPath, "", &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(a.History) != initialLen+1 {
		t.Fatalf("want 1 new history message, got %d total", len(a.History)-initialLen)
	}
	msg := a.History[len(a.History)-1]
	if !strings.Contains(msg.Content, "[skill:hist-test output]") {
		t.Errorf("want '[skill:hist-test output]' in history, got: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "script output") {
		t.Errorf("want 'script output' in history, got: %q", msg.Content)
	}
}
