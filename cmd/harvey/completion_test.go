package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Shell completion for the harvey binary's own verbs and flags. It is separate
// from the in-REPL /command tab completion. The verb and option lists come
// from the code (the switch in runArgs and flagSpecs), never from a second
// hand-kept list, and these tests fail when they drift.

func completionScript(t *testing.T, shell string) string {
	t.Helper()
	code, out, errOut := run(t, "completion", shell)
	if code != 0 {
		t.Fatalf("harvey completion %s: exit %d, stderr %q", shell, code, errOut)
	}
	return out
}

// ─── the lists ────────────────────────────────────────────────────────────────

// The verbs are the non-option cases of the switch in runArgs. A verb added
// there and not to the completion list (or the reverse) fails here.
func TestCompletion_VerbsMatchTheSwitchInMain(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	var fromSource []string
	for _, m := range regexp.MustCompile(`(?m)^\t\tcase "([a-z][a-z-]*)":`).FindAllStringSubmatch(string(src), -1) {
		fromSource = append(fromSource, m[1])
	}
	got := completionVerbs()
	sort.Strings(fromSource)
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(fromSource, " ") {
		t.Errorf("completionVerbs = %v, the verb cases in runArgs = %v", got, fromSource)
	}
	for _, want := range []string{"init", "help", "completion"} {
		if !contains(got, want) {
			t.Errorf("verb %q missing from completionVerbs %v", want, got)
		}
	}
}

// Every option that takes a value must say what kind of value, so completion
// does not offer verbs where a model name goes or fall silent on a path.
func TestCompletion_EveryValueFlagIsClassified(t *testing.T) {
	spelled := map[string]bool{}
	for _, f := range flagSpecs {
		for _, n := range f.names {
			spelled[n] = true
			if f.takesValue {
				if k := completionValueKind[n]; k != valuePath && k != valueFree {
					t.Errorf("%s takes a value but completionValueKind has no kind for it", n)
				}
			}
		}
	}
	for n := range completionValueKind {
		if !spelled[n] {
			t.Errorf("completionValueKind names %s, which is not an option", n)
		}
	}
}

func TestCompletion_OptionsCoverTheCodeAndTheImmediateOnes(t *testing.T) {
	opts := completionOptions()
	var want []string
	for _, f := range flagSpecs {
		want = append(want, f.names...)
	}
	want = append(want, immediateOptions...)
	for _, n := range want {
		if !contains(opts, n) {
			t.Errorf("option %s missing from completionOptions", n)
		}
	}
	// A single-dash long form is accepted by runArgs and is what a person types.
	if !contains(opts, "-help") {
		t.Error("-help is accepted by harvey and should be completed")
	}
}

func TestCompletion_HelpTopicsAreRealTopics(t *testing.T) {
	topics := completionHelpTopics()
	if len(topics) < 30 {
		t.Fatalf("only %d help topics found; the parse of the topic index has drifted: %v", len(topics), topics)
	}
	for _, name := range topics {
		if code, out, errOut := run(t, "help", name); code != 0 || out == "" {
			t.Errorf("harvey help %s: exit %d, %d bytes, stderr %.80q", name, code, len(out), errOut)
		}
	}
	for _, want := range []string{"kb", "model", "topics"} {
		if !contains(topics, want) {
			t.Errorf("topic %q missing from %v", want, topics)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ─── the scripts ──────────────────────────────────────────────────────────────

func TestCompletion_BashScriptNamesEverything(t *testing.T) {
	s := completionScript(t, "bash")
	want := []string{"-F _harvey harvey", "completion", "init", "--workdir", "--replay-continue", "--ollama", "-help", "powershell", "topics", "kb"}
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("bash script missing %q", w)
		}
	}
	for _, f := range flagSpecs {
		for _, n := range f.names {
			if !strings.Contains(s, n) {
				t.Errorf("bash script missing option %s", n)
			}
		}
	}
}

func TestCompletion_PowerShellScriptNamesEverything(t *testing.T) {
	s := completionScript(t, "powershell")
	for _, w := range []string{"Register-ArgumentCompleter -Native -CommandName harvey,harvey.exe", "CompletionResult", "init", "--workdir", "completion", "kb"} {
		if !strings.Contains(s, w) {
			t.Errorf("powershell script missing %q", w)
		}
	}
	if code, out, _ := run(t, "completion", "pwsh"); code != 0 || out != s {
		t.Errorf("pwsh should be accepted as powershell: exit %d", code)
	}
}

// The script has to work, not only name things: source it in bash, fill in
// COMP_WORDS the way the shell does, call the function, read COMPREPLY.
func bashComplete(t *testing.T, dir string, words ...string) []string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available; skipping the behavioural completion tests")
	}
	script := filepath.Join(t.TempDir(), "harvey.bash")
	if err := os.WriteFile(script, []byte(completionScript(t, "bash")), 0o644); err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = "'" + w + "'"
	}
	prog := "source '" + script + "'\n" +
		"COMP_WORDS=(" + strings.Join(quoted, " ") + ")\n" +
		"COMP_CWORD=" + strconv.Itoa(len(words)-1) + "\n" +
		"_harvey\n" +
		"printf '%s\\n' \"${COMPREPLY[@]}\"\n"
	cmd := exec.Command(bash, "-c", prog)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			got = append(got, l)
		}
	}
	return got
}

func TestCompletion_Bash_VerbsFirst(t *testing.T) {
	got := bashComplete(t, t.TempDir(), "harvey", "")
	for _, want := range []string{"init", "help", "completion"} {
		if !contains(got, want) {
			t.Errorf("first word: %q missing from %v", want, got)
		}
	}
	if contains(got, "--workdir") {
		t.Errorf("options should wait for a leading dash: %v", got)
	}
}

func TestCompletion_Bash_OptionsOnDash(t *testing.T) {
	got := bashComplete(t, t.TempDir(), "harvey", "--")
	for _, want := range []string{"--workdir", "--model", "--replay", "--debug"} {
		if !contains(got, want) {
			t.Errorf("%q missing from %v", want, got)
		}
	}
	if contains(got, "init") {
		t.Errorf("verbs offered for a dash: %v", got)
	}
}

func TestCompletion_Bash_HelpTakesTopics(t *testing.T) {
	got := bashComplete(t, t.TempDir(), "harvey", "help", "")
	for _, want := range []string{"kb", "model", "topics"} {
		if !contains(got, want) {
			t.Errorf("%q missing from %v", want, got)
		}
	}
}

// A value flag before the verb must not be mistaken for the verb or for the
// last word.
func TestCompletion_Bash_VerbFoundPastAValueFlag(t *testing.T) {
	got := bashComplete(t, t.TempDir(), "harvey", "-w", "somedir", "help", "")
	if !contains(got, "kb") {
		t.Errorf("topics not offered after -w somedir help: %v", got)
	}
}

func TestCompletion_Bash_CompletionTakesShells(t *testing.T) {
	got := bashComplete(t, t.TempDir(), "harvey", "completion", "")
	if strings.Join(got, " ") != "bash powershell" {
		t.Errorf("shells = %v, want bash powershell", got)
	}
	got = bashComplete(t, t.TempDir(), "harvey", "completion", "-")
	if !contains(got, "-install") && !contains(got, "--install") {
		t.Errorf("install option missing: %v", got)
	}
}

func TestCompletion_Bash_PathValuesCompleteFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zzfile.spmd"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, words := range [][]string{
		{"harvey", "--replay", "zz"},
		{"harvey", "-w", "zz"},
		{"harvey", "--continue", "zz"},
		{"harvey", "init", "zz"},
	} {
		got := bashComplete(t, dir, words...)
		if !contains(got, "zzfile.spmd") {
			t.Errorf("%v: file not completed: %v", words, got)
		}
	}
}

func TestCompletion_Bash_FreeValuesOfferNothing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "zzfile"), []byte("x"), 0o644)
	for _, flag := range []string{"-m", "--model", "--ollama"} {
		if got := bashComplete(t, dir, "harvey", flag, ""); len(got) != 0 {
			t.Errorf("%s value: offered %v, want nothing", flag, got)
		}
	}
}

// ─── the verb ─────────────────────────────────────────────────────────────────

func TestCompletionVerb_MistakesAreUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"no shell":           {"completion"},
		"unknown shell":      {"completion", "zsh"},
		"surplus argument":   {"completion", "bash", "extra"},
		"install no shell":   {"completion", "-install"},
		"unknown option":     {"completion", "bash", "--bogus-zzz"},
		"two shells":         {"completion", "bash", "powershell"},
		"install unknown":    {"completion", "zsh", "--install"},
		"option before verb": {"--bogus-zzz", "completion", "bash"},
	}
	for what, args := range cases {
		code, out, errOut := run(t, args...)
		if code != 2 || out != "" {
			t.Errorf("%s: harvey %v: exit %d, stdout %.60q, stderr %.100q; want 2 and no stdout", what, args, code, out, errOut)
		}
	}
}

// ─── install ──────────────────────────────────────────────────────────────────

func TestInstallCompletion_BashDefaultPath(t *testing.T) {
	home := t.TempDir()
	path, err := installCompletion("harvey", "bash", home, "", "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "share", "bash-completion", "completions", "harvey")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "-F _harvey harvey") {
		t.Errorf("installed file is not the completion script: %v", err)
	}
	if _, err := installCompletion("harvey", "bash", home, "", "linux"); err != nil {
		t.Errorf("reinstall over our own file: %v", err)
	}
}

func TestInstallCompletion_BashHonoursXDGDataHome(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	path, err := installCompletion("harvey", "bash", home, xdg, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, xdg) {
		t.Errorf("path %q should be under %q", path, xdg)
	}
}

// Never overwrite a file harvey did not write: exit 73, file untouched.
func TestInstallCompletion_BashRefusesAForeignFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
	os.MkdirAll(dir, 0o755)
	foreign := filepath.Join(dir, "harvey")
	os.WriteFile(foreign, []byte("something else\n"), 0o644)

	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	code, out, errOut := run(t, "completion", "bash", "--install")
	if code != 73 {
		t.Errorf("exit %d (stdout %q, stderr %q), want 73 cant_create", code, out, errOut)
	}
	if got, _ := os.ReadFile(foreign); string(got) != "something else\n" {
		t.Errorf("foreign file was modified: %q", got)
	}
}

func TestInstallCompletion_PowerShellProfileLineOnce(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	os.MkdirAll(filepath.Dir(profile), 0o755)
	os.WriteFile(profile, []byte("Set-Alias ll ls\n"), 0o644)

	path, err := installCompletion("harvey", "powershell", home, "", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installCompletion("harvey", "powershell", home, "", "linux"); err != nil {
		t.Fatal(err)
	}
	prof, _ := os.ReadFile(profile)
	if !strings.HasPrefix(string(prof), "Set-Alias ll ls\n") {
		t.Errorf("existing profile content lost:\n%s", prof)
	}
	if n := strings.Count(string(prof), path); n != 1 {
		t.Errorf("profile sources the script %d times, want 1:\n%s", n, prof)
	}
}

func TestInstallCompletion_PowerShellWindowsLocation(t *testing.T) {
	home := t.TempDir()
	if _, err := installCompletion("harvey.exe", "powershell", home, "", "windows"); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	if _, err := os.Stat(profile); err != nil {
		t.Errorf("windows profile not written: %v", err)
	}
}

func TestInstallCompletion_UnknownShellCreatesNothing(t *testing.T) {
	home := t.TempDir()
	if _, err := installCompletion("harvey", "zsh", home, "", "linux"); err == nil {
		t.Error("expected an error for an unsupported shell")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("created %d entries for an unsupported shell", len(entries))
	}
}

// Both spellings, both orders, through the real command line. The output names
// the file so a person knows where it went.
func TestCompletionVerb_InstallThroughMainRun(t *testing.T) {
	for _, args := range [][]string{
		{"completion", "bash", "-install"}, {"completion", "bash", "--install"},
		{"completion", "-install", "bash"}, {"completion", "--install", "bash"},
	} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_DATA_HOME", "")
		code, out, errOut := run(t, args...)
		if code != 0 || !strings.Contains(out, "bash-completion") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(home, ".local", "share", "bash-completion", "completions", "harvey")); err != nil {
			t.Errorf("%v: nothing installed: %v", args, err)
		}
	}
}

// The manual lists the verb, so it cannot ship undocumented.
func TestCompletionVerb_IsInTheManual(t *testing.T) {
	_, out, _ := run(t, "--help")
	if !strings.Contains(out, "completion SHELL") {
		t.Error("harvey --help does not document `completion SHELL`")
	}
}
