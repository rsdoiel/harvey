package harvey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingKB puts a kb on PATH that writes its working directory and
// arguments to a file, prints a line, prints the contents of its "err" file on
// stderr, and exits with the code in its "exit" file (the command environment
// filter drops unknown variables, so a file carries the settings).
func recordingKB(t *testing.T) (record string, setResult func(code, stderr string)) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "calls")
	script := "#!/bin/sh\nd=$(dirname \"$0\")\n[ \"$1\" = \"-version\" ] && { echo 'kb " + kbMinVersion + " test'; exit 0; }\n{ pwd; for a in \"$@\"; do printf '%s\\n' \"$a\"; done; } > " + record + "\necho \"kb says hi\"\n[ -f \"$d/err\" ] && cat \"$d/err\" >&2\n[ -f \"$d/exit\" ] && exit $(cat \"$d/exit\")\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "kb"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record, func(code, stderr string) {
		_ = os.WriteFile(filepath.Join(dir, "exit"), []byte(code), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "err"), []byte(stderr), 0o644)
	}
}

func TestKBPassthrough_runsTheInstalledKB(t *testing.T) {
	record, _ := recordingKB(t)
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdKB(a, []string{"observation", "list", "--project", "harvey"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "kb says hi") {
		t.Errorf("kb's output was not shown: %q", out.String())
	}
	b, _ := os.ReadFile(record)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if lines[0] != a.Workspace.Root {
		t.Errorf("kb ran in %q, want the workspace %q", lines[0], a.Workspace.Root)
	}
	want := []string{"-db", filepath.Join(a.Workspace.Root, "agents", "knowledge.db"), "observation", "list", "--project", "harvey"}
	if got := lines[1:]; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("kb got %v, want %v", got, want)
	}
}

// A leading option such as -json stays before the verb, and a -db the user gave
// is not overridden.
func TestKBPassthrough_optionsAndAnExplicitDB(t *testing.T) {
	record, _ := recordingKB(t)
	a := newTestAgent(t)
	if err := cmdKB(a, []string{"-json", "search", "wal"}, new(strings.Builder)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(record)
	if !strings.Contains(string(b), "-db\n") || strings.Index(string(b), "-json") > strings.Index(string(b), "search") {
		t.Errorf("kb got %q", b)
	}
	if err := cmdKB(a, []string{"-db", "/elsewhere/k.db", "stats"}, new(strings.Builder)); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(record)
	if strings.Count(string(b), "-db\n") != 1 || !strings.Contains(string(b), "/elsewhere/k.db") {
		t.Errorf("an explicit -db was overridden: %q", b)
	}
}

// kb's exit status is harvey's, by the workspace table (DR-0003).
func TestKBPassthrough_exitStatusKeepsItsClass(t *testing.T) {
	_, setResult := recordingKB(t)
	a := newTestAgent(t)
	for code, want := range map[string]ExitClass{
		"1": ClassNegative, "2": ClassUsage, "65": ClassData, "66": ClassNoInput, "69": ClassUnavailable,
		"73": ClassCantCreate, "74": ClassIO, "75": ClassTempFail, "77": ClassNoPermission, "78": ClassConfig,
		"70": ClassInternal, "9": ClassInternal,
	} {
		setResult(code, "kb: it failed")
		var out strings.Builder
		err := cmdKB(a, []string{"stats"}, &out)
		if err == nil || ExitCodeFor(err) != want {
			t.Errorf("exit %s: class %v (%v), want %v", code, ExitCodeFor(err), err, want)
		}
		if !strings.Contains(out.String(), "kb: it failed") {
			t.Errorf("exit %s: stderr of kb was not shown: %q", code, out.String())
		}
	}
}

func TestKBPassthrough_noKBInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := newTestAgent(t)
	err := cmdKB(a, []string{"stats"}, new(strings.Builder))
	if ExitCodeFor(err) != ClassUnavailable || !strings.Contains(err.Error(), "go install") {
		t.Fatalf("want an unavailable error with the install command, got %v", err)
	}
}

func TestKBPassthrough_noArgumentsIsUsage(t *testing.T) {
	err := cmdKB(newTestAgent(t), nil, new(strings.Builder))
	if ExitCodeFor(err) != ClassUsage {
		t.Fatalf("class %v (%v), want usage", ExitCodeFor(err), err)
	}
}

// inject and learn are still harvey's own (design brief decision 4 is open): they
// are not sent to kb.
func TestKBPassthrough_injectAndLearnStayHarveys(t *testing.T) {
	record, _ := recordingKB(t)
	a, _ := newTestAgentWithKB(t)
	if err := cmdKB(a, []string{"inject"}, new(strings.Builder)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(record); err == nil {
		t.Error("/kb inject ran the installed kb")
	}
}

// The session log records the verb, not a leading option, and the exit class.
func TestKBPassthrough_isLogged(t *testing.T) {
	_, setResult := recordingKB(t)
	setResult("1", "")
	a, path := agentWithLog(t)
	_ = cmdKB(a, []string{"-json", "search", "wal"}, new(strings.Builder))
	var call, result map[string]any
	for _, e := range eventsOf(t, a, path, KindKBCall) {
		_ = json.Unmarshal(e.Data, &call)
	}
	for _, e := range eventsOf(t, a, path, KindKBResult) {
		_ = json.Unmarshal(e.Data, &result)
	}
	if call == nil || call["verb"] != "search" {
		t.Fatalf("kb_call = %v", call)
	}
	if result == nil || result["status"] != "negative" {
		t.Errorf("kb_result = %v", result)
	}
}

// The /kb entry lists the installed kb's verbs plus harvey's own, for
// completion and /help.
func TestKBRegistry_ListsKBVerbsAndHarveysOwn(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = \"-version\" ] && { echo 'kb " + kbMinVersion + " test'; exit 0; }\n[ \"$1 $2\" = \"-json verbs\" ] && echo '{\"verbs\":[{\"name\":\"search\"},{\"name\":\"observation\"}]}'\n"
	if err := os.WriteFile(filepath.Join(dir, "kb"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got := strings.Join(kbVerbNames(), ",")
	if got != "inject,learn,observation,search" {
		t.Errorf("verbs = %s", got)
	}
	t.Setenv("PATH", t.TempDir())
	if got := strings.Join(kbVerbNames(), ","); got != "inject,learn" {
		t.Errorf("without kb, verbs = %s, want harvey's own only", got)
	}
}

func TestKBPassthrough_projectUseIsHarveysSessionState(t *testing.T) {
	record, _ := recordingKB(t)
	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdKB(a, []string{"project", "use", "7"}, &out); err != nil {
		t.Fatal(err)
	}
	if a.Config.Memory.CurrentProjectID != 7 {
		t.Errorf("CurrentProjectID = %d, want 7", a.Config.Memory.CurrentProjectID)
	}
	if _, err := os.Stat(record); err == nil {
		t.Error("/kb project use was sent to the installed kb")
	}
	for _, bad := range [][]string{{"project", "use"}, {"project", "use", "x"}, {"project", "use", "0"}} {
		if err := cmdKB(a, bad, new(strings.Builder)); ExitCodeFor(err) != ClassUsage {
			t.Errorf("%v: class %v, want usage", bad, ExitCodeFor(err))
		}
	}
}
