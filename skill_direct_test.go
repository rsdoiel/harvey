package harvey

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// directSkillAgent returns a test agent with one skill named "greeter" whose
// directory holds only SKILL.md (no scripts) and registers the skill commands.
func directSkillAgent(t *testing.T) (*Agent, *SkillMeta) {
	t.Helper()
	a := newTestAgent(t)
	a.Client = &mockLLMClient{}
	dir := t.TempDir()
	skill := &SkillMeta{
		Name:        "greeter",
		Description: "Greets people.",
		Path:        filepath.Join(dir, "SKILL.md"),
		Body:        "Greet the user warmly.",
	}
	if err := os.WriteFile(skill.Path, []byte("---\nname: greeter\n---\n"+skill.Body), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Skills = SkillCatalog{"greeter": skill}
	a.registerSkillCommands()
	return a, skill
}

// A skill with no compiled scripts is still a slash command.
func TestSkillCommand_registeredWithoutScripts(t *testing.T) {
	a, _ := directSkillAgent(t)
	if _, ok := a.commands["greeter"]; !ok {
		t.Fatal("want /greeter registered for a skill with no scripts/compiled.bash")
	}
}

// Running the command loads the skill body and queues the arguments as the
// user's next request; it never asks about compiling.
func TestSkillCommand_loadsBodyAndQueuesArguments(t *testing.T) {
	a, _ := directSkillAgent(t)
	var out strings.Builder
	if _, err := a.dispatch("/greeter say hello to Ada", &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if strings.Contains(strings.ToLower(out.String()), "compile") {
		t.Errorf("output mentions compiling: %q", out.String())
	}
	last := a.History[len(a.History)-1]
	if !strings.Contains(last.Content, "[skill: greeter]") || !strings.Contains(last.Content, "Greet the user warmly.") {
		t.Errorf("want the skill body in history, got %q", last.Content)
	}
	if a.ActiveSkill != "greeter" {
		t.Errorf("ActiveSkill = %q, want greeter", a.ActiveSkill)
	}
	line, ok := a.nextQueuedInput()
	if !ok || line != "say hello to Ada" {
		t.Errorf("queued input = %q, %v; want the arguments", line, ok)
	}
}

// With no arguments there is nothing to queue.
func TestSkillCommand_noArgumentsQueuesNothing(t *testing.T) {
	a, _ := directSkillAgent(t)
	var out strings.Builder
	if _, err := a.dispatch("/greeter", &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, ok := a.nextQueuedInput(); ok {
		t.Error("want nothing queued when the command has no arguments")
	}
}

// A compiled script left in a skill directory (a user-scope skill from before
// DR-0008) is ignored: it is not run.
func TestSkillCommand_leftoverScriptIsNotRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on Windows")
	}
	a, skill := directSkillAgent(t)
	marker := filepath.Join(t.TempDir(), "ran")
	scripts := filepath.Join(filepath.Dir(skill.Path), "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"compiled.bash": "#!/usr/bin/env bash\ntouch " + marker + "\n",
		"compiled.ps1":  "New-Item " + marker + "\n",
	} {
		if err := os.WriteFile(filepath.Join(scripts, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a.registerSkillCommands()
	var out strings.Builder
	if _, err := a.dispatch("/greeter hi", &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the leftover compiled script ran")
	}
}

// A trigger match loads the skill into context; it does not offer to compile it.
func TestAutoLoadSkill_triggerLoadsWithoutCompilePrompt(t *testing.T) {
	a, skill := directSkillAgent(t)
	skill.Trigger = "greet"
	var out strings.Builder
	if !a.autoLoadSkill("please greet Ada", &out) {
		t.Fatal("want the trigger to match")
	}
	if strings.Contains(strings.ToLower(out.String()), "compile") {
		t.Errorf("output mentions compiling: %q", out.String())
	}
	if last := a.History[len(a.History)-1]; !strings.Contains(last.Content, "[skill: greeter]") {
		t.Errorf("want the skill body in history, got %q", last.Content)
	}
}

func TestAutoLoadSkill_noMatchLoadsNothing(t *testing.T) {
	a, skill := directSkillAgent(t)
	skill.Trigger = "greet"
	var out strings.Builder
	if a.autoLoadSkill("summarize this file", &out) || len(a.History) != 0 {
		t.Error("want nothing loaded when no trigger matches")
	}
}
