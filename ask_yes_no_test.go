package harvey

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// askYesNo is used for prompts whose default is yes, some of which start a
// server or run a script. End of input (a closed pipe, Ctrl-D, /dev/null) is
// not an answer, so it must never count as the default.
func TestAskYesNo(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      string
		defaultYes bool
		want       bool
	}{
		{"enter takes the yes default", "\n", true, true},
		{"enter takes the no default", "\n", false, false},
		{"y", "y\n", false, true},
		{"yes, any case", "YES\n", false, true},
		{"n", "n\n", true, false},
		{"anything else is no", "maybe\n", true, false},
		{"y with no newline before end of input", "y", false, true},
		{"n with no newline before end of input", "n", true, false},
		{"end of input is not the yes default", "", true, false},
		{"end of input is no when the default is no", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			got := askYesNo(bufio.NewReader(strings.NewReader(tc.input)), &out, "? ", tc.defaultYes)
			if got != tc.want {
				t.Errorf("askYesNo(%q, default %v) = %v, want %v", tc.input, tc.defaultYes, got, tc.want)
			}
		})
	}
}

// skillDispatchFixture writes an uncompiled skill and returns it with an agent
// whose model "compiles" it into a script that creates a marker file.
func skillDispatchFixture(t *testing.T) (a *Agent, skill *SkillMeta, marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "ran")
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: demo\n---\nDo the thing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reply := "```bash scripts/compiled.bash\n#!/bin/bash\ntouch " + marker + "\n```\n\n" +
		"```powershell scripts/compiled.ps1\nNew-Item " + marker + "\n```\n"
	a = newTestAgent(t)
	a.Client = &mockLLMClient{reply: reply}
	return a, &SkillMeta{Name: "demo", Path: skillPath, Body: "Do the thing."}, marker
}

// After compiling, "Run now? [Y/n]" must not run the script when input ends.
func TestDispatchSkill_RunNowIsNotAnsweredYesByEndOfInput(t *testing.T) {
	a, skill, marker := skillDispatchFixture(t)
	var out strings.Builder
	// "\n" takes the yes default for "Compile now?"; then the input ends.
	reader := bufio.NewReader(strings.NewReader("\n"))

	if _, err := DispatchSkill(t.Context(), a, skill, "", reader, &out); err != nil {
		t.Fatalf("DispatchSkill: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Compiled") {
		t.Fatalf("the skill was not compiled; the test does not reach the Run prompt:\n%s", out.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the compiled script ran although the input ended at \"Run now?\"")
	}
}

// The same flow with an explicit yes does run it, so the test above is
// checking the prompt and not a script that never runs.
func TestDispatchSkill_RunNowYesRunsTheScript(t *testing.T) {
	a, skill, marker := skillDispatchFixture(t)
	var out strings.Builder
	reader := bufio.NewReader(strings.NewReader("\n\n"))

	if _, err := DispatchSkill(t.Context(), a, skill, "", reader, &out); err != nil {
		t.Fatalf("DispatchSkill: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the compiled script did not run after two Enters: %v\n%s", err, out.String())
	}
}
