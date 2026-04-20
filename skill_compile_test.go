package harvey

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSkillMeta creates a SkillMeta backed by a real SKILL.md in a temp dir.
func fakeSkillMeta(t *testing.T, name, body string) *SkillMeta {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(dir, "SKILL.md")
	content := "---\nname: " + name + "\ndescription: test skill\n---\n\n" + body
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return &SkillMeta{Name: name, Body: body, Path: skillPath}
}

// validCompileReply returns a mock LLM reply containing both required code blocks.
func validCompileReply() string {
	return "```bash:scripts/compiled.bash\n#!/usr/bin/env bash\necho \"$HARVEY_PROMPT\"\n```\n\n" +
		"```powershell:scripts/compiled.ps1\nWrite-Output $env:HARVEY_PROMPT\n```\n"
}

// ─── CompileSkill ─────────────────────────────────────────────────────────────

func TestCompileSkill_success(t *testing.T) {
	skill := fakeSkillMeta(t, "test-skill", "Do something useful.")
	client := &mockLLMClient{reply: validCompileReply()}

	var out strings.Builder
	err := CompileSkill(context.Background(), client, skill, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both files exist.
	bashPath := CompiledBashPath(skill.Path)
	ps1Path := CompiledPS1Path(skill.Path)

	if _, err := os.Stat(bashPath); err != nil {
		t.Errorf("compiled.bash not created: %v", err)
	}
	if _, err := os.Stat(ps1Path); err != nil {
		t.Errorf("compiled.ps1 not created: %v", err)
	}

	// Verify bash script is executable.
	info, err := os.Stat(bashPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("compiled.bash is not executable: mode=%o", info.Mode())
	}

	// Verify content.
	bashContent, _ := os.ReadFile(bashPath)
	if !strings.Contains(string(bashContent), "HARVEY_PROMPT") {
		t.Error("compiled.bash missing HARVEY_PROMPT reference")
	}
}

func TestCompileSkill_missingBash(t *testing.T) {
	skill := fakeSkillMeta(t, "no-bash", "body")
	// Only PS1 block in reply.
	reply := "```powershell:scripts/compiled.ps1\nWrite-Output $env:HARVEY_PROMPT\n```\n"
	client := &mockLLMClient{reply: reply}

	var out strings.Builder
	err := CompileSkill(context.Background(), client, skill, &out)
	if err == nil {
		t.Fatal("want error when bash block missing, got nil")
	}
	if !strings.Contains(err.Error(), "compiled.bash") {
		t.Errorf("want error mentioning compiled.bash, got: %v", err)
	}
}

func TestCompileSkill_missingPS1(t *testing.T) {
	skill := fakeSkillMeta(t, "no-ps1", "body")
	// Only bash block in reply.
	reply := "```bash:scripts/compiled.bash\n#!/usr/bin/env bash\necho hi\n```\n"
	client := &mockLLMClient{reply: reply}

	var out strings.Builder
	err := CompileSkill(context.Background(), client, skill, &out)
	if err == nil {
		t.Fatal("want error when ps1 block missing, got nil")
	}
	if !strings.Contains(err.Error(), "compiled.ps1") {
		t.Errorf("want error mentioning compiled.ps1, got: %v", err)
	}
}

func TestCompileSkill_llmError(t *testing.T) {
	skill := fakeSkillMeta(t, "err-skill", "body")
	sentinel := errors.New("connection refused")
	client := &mockLLMClient{callErr: sentinel}

	var out strings.Builder
	err := CompileSkill(context.Background(), client, skill, &out)
	if err == nil {
		t.Fatal("want error when LLM fails, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error wrapped, got: %v", err)
	}
}

// ─── compileMetaPrompt ────────────────────────────────────────────────────────

func TestCompileMetaPrompt_containsEnvVars(t *testing.T) {
	for _, want := range []string{
		"HARVEY_PROMPT",
		"HARVEY_WORKDIR",
		"HARVEY_MODEL",
		"HARVEY_SESSION_ID",
	} {
		if !strings.Contains(compileMetaPrompt, want) {
			t.Errorf("compileMetaPrompt missing %q", want)
		}
	}
}

func TestCompileMetaPrompt_containsFenceHeaders(t *testing.T) {
	for _, want := range []string{
		"bash:scripts/compiled.bash",
		"powershell:scripts/compiled.ps1",
	} {
		if !strings.Contains(compileMetaPrompt, want) {
			t.Errorf("compileMetaPrompt missing fence header %q", want)
		}
	}
}
