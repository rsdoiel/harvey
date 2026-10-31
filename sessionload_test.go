package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// twoTurnSession runs a scripted session with two turns and a model switch and
// returns the sessions directory.
func twoTurnSession(t *testing.T) string {
	t.Helper()
	return scriptedRun(t, false, "hello", "/model use qwen2.5-coder:1.5b", "again", "/exit")
}

func sessionPaths(t *testing.T, dir string) (logPath, spmdPath string) {
	t.Helper()
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	spmd, _ := filepath.Glob(filepath.Join(dir, "*.spmd"))
	if len(logs) != 1 || len(spmd) != 1 {
		t.Fatalf("want one log and one .spmd in %s", dir)
	}
	return logs[0], spmd[0]
}

func TestLoadSessionInfo_ReadsTheLog(t *testing.T) {
	dir := twoTurnSession(t)
	lp, _ := sessionPaths(t, dir)
	info, err := LoadSessionInfo(lp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != "log" || !info.Ended || info.Model != "ollama (llama3.2:3b)" || info.LastModel != "ollama (qwen2.5-coder:1.5b)" {
		t.Errorf("info = %+v", info)
	}
	if info.Date.IsZero() || time.Since(info.Date) > time.Hour {
		t.Errorf("Date = %v, want the session's start", info.Date)
	}
	if len(info.Turns) != 2 || info.Turns[0].UserInput != "hello" || info.Turns[0].ModelReply != "FIRST" ||
		info.Turns[1].UserInput != "again" || info.Turns[1].ModelReply != "SECOND" {
		t.Errorf("turns = %+v", info.Turns)
	}
}

func TestLoadSessionInfo_ASessionFilePathFindsItsLog(t *testing.T) {
	dir := twoTurnSession(t)
	lp, sp := sessionPaths(t, dir)
	byLog, err1 := LoadSessionInfo(lp)
	bySpmd, err2 := LoadSessionInfo(sp)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if bySpmd.Source != "log" || bySpmd.Path != lp || len(bySpmd.Turns) != len(byLog.Turns) {
		t.Errorf("a .spmd path should resolve to the log beside it: %+v", bySpmd)
	}
}

func TestLoadSessionInfo_AFailedTurnIsNotHistory(t *testing.T) {
	scriptedFailOn, scriptedTerminal = 2, true
	t.Cleanup(func() { scriptedFailOn, scriptedTerminal = 0, false })
	dir := scriptedRun(t, false, "hello", "this one fails", "/exit")
	lp, _ := sessionPaths(t, dir)
	info, err := LoadSessionInfo(lp)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Turns) != 1 || info.Turns[0].UserInput != "hello" {
		t.Errorf("turns = %+v, want only the turn that was answered (the live history drops a failed one)", info.Turns)
	}
}

func TestLoadSessionInfo_ARoutedTurnIsKeptAndMarked(t *testing.T) {
	events, _, lp := routedSession(t)
	_ = events
	info, err := LoadSessionInfo(lp)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Turns) != 1 || info.Turns[0].Route != "pi2" || !strings.Contains(info.Turns[0].UserInput, "weather") {
		t.Errorf("turns = %+v, want the routed turn kept with its route", info.Turns)
	}
}

func TestLoadSessionInfo_ACrashedSessionLoads(t *testing.T) {
	dir := t.TempDir()
	lp := crashedLog(t, dir, "crashed")
	info, err := LoadSessionInfo(lp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Ended {
		t.Error("a log with no session_end is not ended")
	}
}

func TestLoadSessionInfo_AnOldFountainFileStillLoads(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "old.spmd")
	writeLegacyFountain(t, sp, "ollama (llama3.2:3b)", dir, "an old question", "an old answer")
	info, err := LoadSessionInfo(sp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != "fountain" || len(info.Turns) != 1 || info.Turns[0].UserInput != "an old question" {
		t.Errorf("a session recorded before the log existed should load through the old parser: %+v", info)
	}
}

func TestLoadSessionInfo_Errors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadSessionInfo(filepath.Join(dir, "nope.spmd")); err == nil || ExitCodeFor(err) != ClassNoInput {
		t.Errorf("a missing session: %v (class %v), want no_input", err, ExitCodeFor(err))
	}
	bad := filepath.Join(dir, "bad.jsonl")
	os.WriteFile(bad, []byte("{not json}\n{also not}\n"), 0o644)
	if _, err := LoadSessionInfo(bad); err == nil || ExitCodeFor(err) != ClassData {
		t.Errorf("a malformed log: %v (class %v), want data", err, ExitCodeFor(err))
	}
}

func TestListSessions_MergesALogWithItsFountainFile(t *testing.T) {
	dir := twoTurnSession(t)
	lp, _ := sessionPaths(t, dir)
	// A session from before the log, and one that crashed.
	old := filepath.Join(dir, "legacy.spmd")
	writeLegacyFountain(t, old, "ollama (llama3.2:3b)", dir, "q", "a")
	os.Chtimes(old, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))
	crashedLog(t, dir, "crashed")

	got, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d sessions, want 3 (the log and its .spmd are one): %+v", len(got), got)
	}
	bySource := map[string]SessionListing{}
	for _, g := range got {
		bySource[g.Name] = g
	}
	stem := strings.TrimSuffix(filepath.Base(lp), ".jsonl")
	if g := bySource[stem]; g.Source != "log" || g.Turns != 2 || !g.Ended || g.Model != "ollama (llama3.2:3b)" {
		t.Errorf("the finished session = %+v", g)
	}
	if g := bySource["legacy"]; g.Source != "fountain" {
		t.Errorf("the old session = %+v", g)
	}
	if g := bySource["crashed"]; g.Source != "log" || g.Ended {
		t.Errorf("the crashed session = %+v", g)
	}
	if got[len(got)-1].Name != "legacy" {
		t.Errorf("sessions are newest first; the 48-hour-old one should be last: %+v", got)
	}
}

func TestContinueFromSession_RebuildsTheHistoryFromTheLog(t *testing.T) {
	dir := twoTurnSession(t)
	lp, _ := sessionPaths(t, dir)
	a := newTestAgent(t)
	n, err := a.ContinueFromSession(lp)
	if err != nil || n != 2 {
		t.Fatalf("ContinueFromSession = %d, %v", n, err)
	}
	var got []string
	for _, m := range a.History {
		got = append(got, m.Role+":"+m.Content)
	}
	want := "user:hello assistant:FIRST user:again assistant:SECOND"
	if strings.Join(got, " ") != want {
		t.Errorf("history = %v, want %s", got, want)
	}
}

func TestReplayFromSession_WritesALogMarkedAsAReplay(t *testing.T) {
	src := twoTurnSession(t)
	srcLog, _ := sessionPaths(t, src)

	a, ws := runFixture(t, scriptedTerminal)
	a.Client = &loggingMock{a: a}
	outPath := filepath.Join(ws.Root, "agents", "sessions", "replay-1.spmd")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := a.ReplayFromSession(t.Context(), srcLog, outPath, &out); err != nil {
		t.Fatalf("replay: %v\n%s", err, out.String())
	}
	if a.Recorder != nil || a.SessionLog != nil {
		t.Errorf("the running session's recorder and log should be restored, got %v %v", a.Recorder, a.SessionLog)
	}

	info, err := LoadSessionInfo(filepath.Join(ws.Root, "agents", "sessions", "replay-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Ended || len(info.Turns) != 2 || info.Turns[0].UserInput != "hello" || info.Turns[1].ModelReply != "REPLAYED" {
		t.Errorf("replay session = %+v", info)
	}
	raw, _ := os.ReadFile(filepath.Join(ws.Root, "agents", "sessions", "replay-1.jsonl"))
	for _, want := range []string{`"replay_of"`, `"origin":"replay"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("replay log lacks %s", want)
		}
	}
	spmd, _ := os.ReadFile(outPath)
	if !strings.Contains(string(spmd), "REPLAYED") {
		t.Errorf(".spmd should be rendered from the log at the end:\n%s", spmd)
	}
}

// loggingMock answers REPLAYED and reports each call to the agent's log the way
// OllamaChatClient does, so the replayed turn is logged as a model call.
type loggingMock struct{ a *Agent }

func (m *loggingMock) Name() string { return "mock" }
func (m *loggingMock) Chat(ctx context.Context, msgs []Message, out io.Writer) (ChatStats, error) {
	end := m.a.BeginModelCall(ctx, msgs, 0)
	fmt.Fprint(out, "REPLAYED")
	end("REPLAYED", ChatStats{}, nil)
	return ChatStats{}, nil
}
func (m *loggingMock) Models(context.Context) ([]string, error) { return nil, nil }
func (m *loggingMock) Close() error                             { return nil }

func TestRename_MovesTheLogItsBlobsAndTheFountainFileTogether(t *testing.T) {
	a, ws := runFixture(t, scriptedTerminal)
	dir := filepath.Join(ws.Root, "agents", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(dir, "harvey-session-1.spmd")
	rec, err := NewRecorder(sp, "ollama (llama3.2:3b)", ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	a.Recorder = rec
	a.openSessionLog(sp, "", io.Discard)
	if err := os.MkdirAll(filepath.Join(dir, "harvey-session-1.blobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldID := a.logEvent(KindUserInput, LevelInfo, map[string]any{"text": "hi"})

	var out strings.Builder
	if err := cmdRename(a, []string{"my-feature.spmd"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"my-feature.spmd", "my-feature.jsonl", "my-feature.blobs"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should exist after the rename: %v", name, err)
		}
	}
	for _, name := range []string{"harvey-session-1.spmd", "harvey-session-1.jsonl", "harvey-session-1.blobs"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s should be gone", name)
		}
	}
	// The log goes on appending to the renamed file, ids unchanged in kind.
	if id := a.logEvent(KindUserInput, LevelInfo, map[string]any{"text": "more"}); id == "" || id == oldID {
		t.Errorf("event after rename = %q (before: %q)", id, oldID)
	}
	if lp := a.SessionLog.Path(); filepath.Base(lp) != "my-feature.jsonl" {
		t.Errorf("log path = %s", lp)
	}

	// A name already taken is refused and leaves everything where it was.
	if err := os.WriteFile(filepath.Join(dir, "taken.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err = cmdRename(a, []string{"taken"}, &out)
	if ExitCodeFor(err) != ClassCantCreate {
		t.Errorf("rename onto an existing log: err = %v, want exit 73", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "my-feature.spmd")); serr != nil {
		t.Errorf("a refused rename must leave the .spmd alone: %v", serr)
	}
	a.closeSessionLog("quit")
	rec.Close()
}
