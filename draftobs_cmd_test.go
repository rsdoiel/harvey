package harvey

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// D6 of the observation-drafting plan: /kb draft, end to end, with a scripted
// drafter and a scratch kb (skipped when kb is missing or older than kbMinVersion).

const draftSession = "harvey-session-20261101-100000"

// draftAgent is an agent whose workspace holds a scratch kb with the given
// projects and an open session log with a short conversation in it.
type draftEnv struct {
	a      *Agent
	root   string
	db     string
	drafts *scriptDrafter
}

func newDraftEnv(t *testing.T, wsName string, projects ...string) *draftEnv {
	t.Helper()
	root, db := scratchKB(t, wsName, projects...)
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t)
	a.Workspace = ws
	a.Config.Memory.KnowledgeDB = db
	sessions := filepath.Join(root, "agents", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	a.SessionsDir = sessions
	l, _ := openTestLog(t, func(o *SessionLogOptions) { o.Dir = sessions; o.Session = draftSession })
	a.SessionLog = l
	env := &draftEnv{a: a, root: root, db: db, drafts: &scriptDrafter{}}
	a.learnDrafterOverride = func(name string, out io.Writer) (Drafter, func(), error) {
		return env.drafts, func() {}, nil
	}
	logTalk(t, l, "How do I run the tests?", "Run go test in the harvey directory.")
	return env
}

func logTalk(t *testing.T, l *SessionLog, user, reply string) {
	t.Helper()
	if _, err := l.Append(KindUserInput, LevelInfo, map[string]any{"turn": 1, "text": user}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(KindModelResponse, LevelInfo, map[string]any{"turn": 1, "status": "ok", "text": reply}); err != nil {
		t.Fatal(err)
	}
}

func (e *draftEnv) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var b strings.Builder
	err := cmdKB(e.a, append([]string{"draft"}, args...), &b)
	return b.String(), err
}

func (e *draftEnv) queue(t *testing.T) string {
	t.Helper()
	return scratchRun(t, e.db, "observation", "review", "list")
}

func TestKBDraft_WritesDraftsThatWaitForReview(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.drafts.replies = []string{"finding: go test runs from the harvey directory\nquestion: does the suite need -race on the Pi?"}
	histBefore := len(e.a.History)
	out, err := e.run(t, "--project", "demo")
	if err != nil {
		t.Fatalf("/kb draft: %v\n%s", err, out)
	}
	q := e.queue(t)
	for _, want := range []string{"go test runs from the harvey directory", "does the suite need -race", "harvey fake"} {
		if !strings.Contains(q, want) {
			t.Errorf("review queue lacks %q:\n%s", want, q)
		}
	}
	show := scratchRun(t, e.db, "observation", "show", "1")
	if !strings.Contains(show, "Drafted from "+draftSession+"#") {
		t.Errorf("a draft does not name its session:\n%s", show)
	}
	found, _ := exec.Command("kb", "-db", e.db, "search", "harvey").CombinedOutput()
	if strings.Contains(string(found), "go test runs from") {
		t.Errorf("a draft is searchable before promotion: %s", found)
	}
	for _, want := range []string{"2 draft", "kb observation review list --project demo", "kb observation review promote"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	if len(e.a.History) != histBefore {
		t.Errorf("/kb draft changed the prompt history: %d -> %d messages", histBefore, len(e.a.History))
	}
}

func TestKBDraft_TheSessionLogRecordsTheCommandAndTheDraftsItWrote(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.drafts.replies = []string{"note: one thing worth keeping"}
	if _, err := e.run(t, "--project", "demo"); err != nil {
		t.Fatal(err)
	}
	events, _, err := ReadSessionLog(e.a.SessionLog.Path())
	if err != nil {
		t.Fatal(err)
	}
	var call, result *Event
	for i := range events {
		switch events[i].Kind {
		case KindKBCall:
			call = &events[i]
		case KindKBResult:
			result = &events[i]
		}
	}
	if call == nil || result == nil || !strings.Contains(string(call.Data), `"draft"`) {
		t.Fatalf("want a kb_call for draft and its kb_result, got %v / %v", call, result)
	}
	if !strings.Contains(string(result.Data), "kb:observation:1") || !strings.Contains(string(result.Data), `"ok"`) {
		t.Errorf("the kb_result does not list the draft: %s", result.Data)
	}
}

func TestKBDraft_NothingWorthKeepingWritesNothingAndSucceeds(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.drafts.replies = []string{"NONE"}
	out, err := e.run(t, "--project", "demo")
	if err != nil || !strings.Contains(out, "Nothing worth keeping") {
		t.Fatalf("got %v\n%s", err, out)
	}
	if q := e.queue(t); strings.Contains(q, "Drafted") {
		t.Errorf("something was written:\n%s", q)
	}
}

func TestKBDraft_AnEmptySessionNeverAsksTheModel(t *testing.T) {
	root, db := scratchKB(t, "ws", "demo")
	ws, _ := NewWorkspace(root)
	a := newTestAgent(t)
	a.Workspace, a.Config.Memory.KnowledgeDB = ws, db
	a.SessionsDir = filepath.Join(root, "agents", "sessions")
	os.MkdirAll(a.SessionsDir, 0o755)
	l, _ := openTestLog(t, func(o *SessionLogOptions) { o.Dir = a.SessionsDir; o.Session = draftSession })
	a.SessionLog = l
	d := &scriptDrafter{}
	a.learnDrafterOverride = func(string, io.Writer) (Drafter, func(), error) { return d, func() {}, nil }
	var b strings.Builder
	if err := cmdKB(a, []string{"draft", "--project", "demo"}, &b); err != nil || !strings.Contains(b.String(), "Nothing to draft from") || d.calls != 0 {
		t.Errorf("err %v, calls %d, out %s", err, d.calls, b.String())
	}
}

func manyLines(n int) string {
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet"}
	var ls []string
	for i := 0; i < n; i++ {
		ls = append(ls, "note: "+words[i]+" "+words[(i+3)%10]+" "+words[(i+5)%10]+" "+words[(i+7)%10]+" seen")
	}
	return strings.Join(ls, "\n")
}

func TestKBDraft_AtMostFiveADayUnlessAskedForMore(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.drafts.replies = []string{manyLines(8)}
	out, err := e.run(t, "--project", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(e.queue(t), "Drafted from"); n != 5 {
		t.Errorf("%d drafts written, want the default cap of 5", n)
	}
	if !strings.Contains(out, "3 left") {
		t.Errorf("the report does not say what the cap left out:\n%s", out)
	}
}

func TestKBDraft_LimitZeroMeansNoCap(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.drafts.replies = []string{manyLines(8)}
	if _, err := e.run(t, "--project", "demo", "--limit", "0"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(e.queue(t), "Drafted from"); n != 8 {
		t.Errorf("%d drafts written, want 8", n)
	}
}

func TestKBDraft_LimitNChangesTheCap(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.drafts.replies = []string{manyLines(8)}
	if _, err := e.run(t, "--limit", "2", "--project", "demo"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(e.queue(t), "Drafted from"); n != 2 {
		t.Errorf("%d drafts written, want 2", n)
	}
}

func TestKBDraft_SkipsWhatKBAlreadyHoldsAndSaysWhat(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	scratchRun(t, e.db, "observation", "add", "--project", "demo", "finding", "go test runs from the harvey directory")
	e.drafts.replies = []string{"finding: go test runs from the harvey directory\nnote: a brand new thing about completion"}
	out, err := e.run(t, "--project", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(e.queue(t), "Drafted from"); n != 1 {
		t.Errorf("%d drafts written, want 1 (the duplicate skipped)", n)
	}
	if !strings.Contains(out, "already") || !strings.Contains(out, "go test runs from the harvey directory") {
		t.Errorf("the report does not name the duplicate and what it matched:\n%s", out)
	}
}

func TestKBDraft_AFailedPassDoesNotStopTheOthers(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	// A small window (budget 300 tokens): the short opening talk is one pass,
	// then the long question is one and the long answer is one, so three passes.
	e.a.Config.Ollama.ContextLength = 1000
	logTalk(t, e.a.SessionLog, strings.Repeat("filler words here ", 140), strings.Repeat("more filler stuff ", 140))
	cause := ClassedAs(ClassUnavailable, errors.New("connection refused"))
	e.drafts.replies = []string{"note: the opening pass found the sqlite wal setting", "", "note: kb refuses promotion without a terminal"}
	e.drafts.errs = []error{nil, cause, nil}
	out, err := e.run(t, "--project", "demo")
	if err == nil || ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("want the class of the first failure (69), got %v", err)
	}
	if !strings.Contains(out, "pass 2 of 3") || !strings.Contains(out, "connection refused") {
		t.Errorf("the report does not name the failed pass:\n%s", out)
	}
	q := e.queue(t)
	if !strings.Contains(q, "opening pass found the sqlite wal setting") || !strings.Contains(q, "kb refuses promotion without a terminal") {
		t.Errorf("a failed pass stopped the others:\n%s", q)
	}
	if e.drafts.calls != 3 {
		t.Errorf("%d model calls, want 3", e.drafts.calls)
	}
}

func TestKBDraft_UsageErrorsAskNothingAndWriteNothing(t *testing.T) {
	for _, args := range [][]string{
		{"--bogus"},
		{"--limit"},
		{"--limit", "abc"},
		{"--limit", "-1"},
		{"--project"},
		{"one", "two"},
	} {
		e := newDraftEnv(t, "ws", "demo")
		_, err := e.run(t, append([]string{"--project", "demo"}, args...)...)
		if args[0] == "--project" {
			_, err = e.run(t, args...)
		}
		if err == nil || ExitCodeFor(err) != ClassUsage {
			t.Errorf("%v: got %v (class %v), want usage (2)", args, err, ExitCodeFor(err))
		}
		if e.drafts.calls != 0 || strings.Contains(e.queue(t), "Drafted") {
			t.Errorf("%v: a rejected command asked the model or wrote", args)
		}
	}
}

func TestKBDraft_TheProjectComesFromTheFlagThenConfigThenTheDirectory(t *testing.T) {
	e := newDraftEnv(t, "alpha", "alpha", "beta")
	e.drafts.replies = []string{"note: one", "note: two", "note: three"}
	if _, err := e.run(t); err != nil { // directory name
		t.Fatalf("directory name: %v", err)
	}
	e.a.Config.KB.Project = "beta"
	if _, err := e.run(t); err != nil { // config over directory
		t.Fatalf("config: %v", err)
	}
	if _, err := e.run(t, "--project", "alpha"); err != nil { // flag over config
		t.Fatalf("flag: %v", err)
	}
	for proj, want := range map[string]int{"alpha": 2, "beta": 1} {
		if n := strings.Count(scratchRun(t, e.db, "observation", "review", "list", "--project", proj), "Drafted from"); n != want {
			t.Errorf("project %s has %d drafts, want %d", proj, n, want)
		}
	}
}

func TestKBDraft_NoProjectToGoOnStopsBeforeAskingTheModel(t *testing.T) {
	e := newDraftEnv(t, "Laboratory", "alpha", "beta")
	_, err := e.run(t)
	if err == nil || ExitCodeFor(err) != ClassUsage || !strings.Contains(err.Error(), "alpha") || e.drafts.calls != 0 {
		t.Errorf("got %v, calls %d", err, e.drafts.calls)
	}
}

func TestKBDraft_AnUnknownProjectIsExit1(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	if _, err := e.run(t, "--project", "nosuch"); err == nil || ExitCodeFor(err) != ClassNegative || e.drafts.calls != 0 {
		t.Errorf("got %v, calls %d", err, e.drafts.calls)
	}
}

func TestKBDraft_ASessionIsNamedByItsStem(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	other := "harvey-session-20261030-143612"
	l2, _ := openTestLog(t, func(o *SessionLogOptions) { o.Dir = e.a.SessionsDir; o.Session = other })
	logTalk(t, l2, "an earlier question about kb", "an earlier answer about kb")
	e.drafts.replies = []string{"note: drafted from the earlier session"}
	if _, err := e.run(t, "--project", "demo", other); err != nil {
		t.Fatal(err)
	}
	if show := scratchRun(t, e.db, "observation", "show", "1"); !strings.Contains(show, "Drafted from "+other+"#") {
		t.Errorf("the draft names the wrong session:\n%s", show)
	}
	if !strings.Contains(e.drafts.lastUser, "an earlier question about kb") || strings.Contains(e.drafts.lastUser, "How do I run the tests") {
		t.Errorf("the model was shown the wrong session:\n%s", e.drafts.lastUser)
	}
}

func TestKBDraft_AnUnknownSessionIsExit66(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	if _, err := e.run(t, "--project", "demo", "no-such-session"); err == nil || ExitCodeFor(err) != ClassNoInput || e.drafts.calls != 0 {
		t.Errorf("got %v (class %v), calls %d", err, ExitCodeFor(err), e.drafts.calls)
	}
}

func TestKBDraft_AnUnreachableRouteIsExit69AndNothingIsWritten(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.a.learnDrafterOverride = func(name string, out io.Writer) (Drafter, func(), error) {
		return nil, nil, errors.New("no route is named " + name)
	}
	_, err := e.run(t, "--project", "demo", "@nosuch")
	if err == nil || ExitCodeFor(err) != ClassUnavailable || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("got %v (class %v)", err, ExitCodeFor(err))
	}
	if strings.Contains(e.queue(t), "Drafted") {
		t.Error("something was written")
	}
}

func TestKBDraft_TheRouteNamedOnTheLineIsTheDrafter(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	var asked string
	e.a.learnDrafterOverride = func(name string, out io.Writer) (Drafter, func(), error) {
		asked = name
		return e.drafts, func() {}, nil
	}
	e.drafts.replies = []string{"note: written by the named route"}
	if _, err := e.run(t, "@granite", "--project", "demo"); err != nil {
		t.Fatal(err)
	}
	if asked != "granite" {
		t.Errorf("asked for %q, want granite", asked)
	}
}

// learn_model is for /kb learn draft; /kb draft uses the active model unless a
// route is named (design decision 2).
func TestKBDraft_IgnoresLearnModel(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.a.Config.LearnModel = "granite"
	var asked = "unset"
	e.a.learnDrafterOverride = func(name string, out io.Writer) (Drafter, func(), error) {
		asked = name
		return e.drafts, func() {}, nil
	}
	if _, err := e.run(t, "--project", "demo"); err != nil {
		t.Fatal(err)
	}
	if asked != "" {
		t.Errorf("/kb draft resolved the drafter as %q, want the active model (empty name)", asked)
	}
}

func TestKBDraft_AnOldKBIsUnavailableBeforeAnythingElse(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	fakeKBWithVersion(t, "0.0.29")
	if _, err := e.run(t, "--project", "demo"); err == nil || ExitCodeFor(err) != ClassUnavailable || e.drafts.calls != 0 {
		t.Errorf("got %v, calls %d", err, e.drafts.calls)
	}
}

// Ctrl-C, once: stop after the pass in flight. The request is not abandoned
// (a hailo-ollama server wedges if one is), what it returns is kept, and no
// further pass is read.
func TestKBDraft_TheFirstCtrlCLetsTheRequestFinishAndStopsAfterThePass(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.a.Config.Ollama.ContextLength = 1000
	logTalk(t, e.a.SessionLog, strings.Repeat("filler words here ", 140), "ok")
	soft, softCancel := context.WithCancel(context.Background())
	e.drafts.replies = []string{"note: kept from the pass the interrupt arrived in", "note: never asked for"}
	e.drafts.during = func(ctx context.Context, n int) error {
		softCancel() // the first Ctrl-C arrives while the request is in flight
		if ctx.Err() != nil {
			t.Error("the first Ctrl-C cancelled the request in flight")
		}
		return nil
	}
	var b strings.Builder
	err := runKBDraft(draftStop{soft: soft, hard: context.Background()}, e.a, []string{"--project", "demo"}, &b)
	if err != nil {
		t.Errorf("a stopped run is not a failure: %v", err)
	}
	if e.drafts.calls != 1 {
		t.Errorf("%d model calls, want 1", e.drafts.calls)
	}
	if !strings.Contains(b.String(), "interrupted") || !strings.Contains(e.queue(t), "kept from the pass the interrupt arrived in") {
		t.Errorf("\n%s\n%s", b.String(), e.queue(t))
	}
}

// Ctrl-C twice: cancel the request now, for a model that is stuck. What was
// written before stays, and the cancelled pass is not counted as a failure.
func TestKBDraft_TheSecondCtrlCCancelsTheRequest(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	e.a.Config.Ollama.ContextLength = 1000
	logTalk(t, e.a.SessionLog, strings.Repeat("filler words here ", 140), "ok")
	hard, hardCancel := context.WithCancel(context.Background())
	e.drafts.replies = []string{"note: written before the second interrupt"}
	e.drafts.during = func(ctx context.Context, n int) error {
		if n == 2 { // the second pass hangs until the second Ctrl-C
			go hardCancel()
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	var b strings.Builder
	err := runKBDraft(draftStop{soft: context.Background(), hard: hard}, e.a, []string{"--project", "demo"}, &b)
	if err != nil {
		t.Errorf("a cancelled run is not a failure: %v", err)
	}
	if strings.Contains(b.String(), "failed") {
		t.Errorf("the cancelled pass was reported as a failure:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "interrupted") || !strings.Contains(e.queue(t), "written before the second interrupt") {
		t.Errorf("\n%s\n%s", b.String(), e.queue(t))
	}
	if e.drafts.calls != 2 {
		t.Errorf("%d model calls, want 2", e.drafts.calls)
	}
}

// The signal watcher: one signal is soft, the second is hard, and the author is
// told once what the first did.
func TestWatchInterrupts(t *testing.T) {
	sig := make(chan os.Signal, 2)
	var out strings.Builder
	stop, done := watchInterrupts(sig, &out)
	defer done()
	if stop.soft.Err() != nil || stop.hard.Err() != nil {
		t.Fatal("cancelled before any signal")
	}
	sig <- os.Interrupt
	<-stop.soft.Done()
	if stop.hard.Err() != nil {
		t.Error("the first signal cancelled the request")
	}
	sig <- os.Interrupt
	<-stop.hard.Done()
	if !strings.Contains(out.String(), "again") || strings.Count(out.String(), "stopping after this pass") != 1 {
		t.Errorf("the notice: %q", out.String())
	}
}

// If kb stops accepting writes mid-run, the run stops writing, says what it
// did not write, and exits with kb's class; it does not keep trying.
func TestKBDraft_AKBThatStopsAcceptingWritesStopsTheRun(t *testing.T) {
	e := newDraftEnv(t, "ws", "demo")
	dir := t.TempDir()
	script := "#!/bin/sh\nd=$(dirname \"$0\")\n" +
		"if [ \"$1\" = \"-version\" ]; then echo 'kb " + kbMinVersion + " abc1234'; exit 0; fi\n" +
		"case \"$*\" in\n" +
		"  *\"project list\"*) echo '[{\"Name\": \"demo\"}]';;\n" +
		"  *\"observation list\"*) echo '[]';;\n" +
		"  *\"observation add\"*) echo x >> \"$d/adds\"; echo '{\"error\": \"database is locked\", \"class\": \"temp_fail\"}' >&2; exit 75;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "kb"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	e.drafts.replies = []string{manyLines(3)}
	out, err := e.run(t, "--project", "demo")
	if err == nil || ExitCodeFor(err) != ClassTempFail || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("got %v (class %v), want kb's class 75 and its message", err, ExitCodeFor(err))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "adds"))
	if n := strings.Count(string(raw), "x"); n != 1 {
		t.Errorf("%d write attempts, want 1: the run must stop at the first refusal", n)
	}
	if !strings.Contains(out, "stopped writing") || !strings.Contains(out, "3 left unwritten") {
		t.Errorf("the report does not say what was left:\n%s", out)
	}
}

// Tab after /kb draft offers the recorded sessions (design: completion of the
// session stem). A directory read only: no kb and no model.
func TestArgCompletion_KBDraftOffersSessionStems(t *testing.T) {
	a := newTestAgent(t)
	dir := t.TempDir()
	a.SessionsDir = dir
	for _, f := range []string{
		"harvey-session-20261030-143612.jsonl",
		"harvey-session-20261031-090000.jsonl",
		"harvey-session-20261031-090000.spmd", // rendered from the log: not offered twice
		"harvey-session-20261007-124448.spmd", // an old session with no log: nothing to draft from
		"notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "harvey-session-20261031-090000.blobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.registerCommands()
	completer := a.buildCompleter()

	got := completer("/kb draft ")
	want := []string{"harvey-session-20261030-143612", "harvey-session-20261031-090000"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("/kb draft <Tab> = %v, want %v", got, want)
	}
	if got := completer("/kb draft harvey-session-2026103"); len(got) != 2 {
		t.Errorf("a prefix still offers both: %v", got)
	}
	if got := completer("/kb draft harvey-session-20261031"); len(got) != 1 {
		t.Errorf("a longer prefix narrows: %v", got)
	}
	if got := completer("/kb search "); len(got) != 0 {
		t.Errorf("other kb verbs offer no sessions: %v", got)
	}
	a.SessionsDir = filepath.Join(dir, "missing")
	if got := completer("/kb draft "); len(got) != 0 {
		t.Errorf("a missing sessions directory offers nothing: %v", got)
	}
}

func TestDraftSessionCandidates_NoSessionsDirectoryOffersNothing(t *testing.T) {
	a := newTestAgent(t)
	a.SessionsDir = ""
	if got := draftSessionCandidates(a); len(got) != 0 {
		t.Errorf("got %v", got)
	}
	if got := draftSessionCandidates(nil); len(got) != 0 {
		t.Errorf("a nil agent: %v", got)
	}
}
