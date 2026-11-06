package harvey

import (
	"strings"
	"testing"
)

// D1 of the observation-drafting plan: what a drafting pass reads from the
// session log (design decision 3). The log is written by the real writer, so
// the shapes are the ones harvey produces.

type drafted struct {
	l   *SessionLog
	ids map[string]string
}

func (d *drafted) add(t *testing.T, name string, kind EventKind, level EventLevel, data any) string {
	t.Helper()
	id, err := d.l.Append(kind, level, data)
	if err != nil {
		t.Fatal(err)
	}
	d.ids[name] = id
	return id
}

func readBack(t *testing.T, l *SessionLog) []Event {
	t.Helper()
	ev, _, err := ReadSessionLog(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func transcriptText(es []TranscriptEntry) string {
	var b strings.Builder
	for _, e := range es {
		b.WriteString(e.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func TestDraftTranscript_KeepsTheConversationAndToolResults(t *testing.T) {
	l, _ := openTestLog(t, nil)
	d := &drafted{l: l, ids: map[string]string{}}
	d.add(t, "user", KindUserInput, LevelInfo, map[string]any{"turn": 1, "text": "How do I run the tests here?"})
	d.add(t, "reply", KindModelResponse, LevelInfo, map[string]any{"turn": 1, "status": "ok", "text": "Run go test in the harvey directory."})
	call := d.add(t, "call", KindToolCall, LevelInfo, map[string]any{"turn": 2, "tool": "run_command", "origin": "model", "call_id": "c1", "args": "go test ./..."})
	d.add(t, "result", KindToolResult, LevelInfo, map[string]any{"turn": 2, "call": call, "call_id": "c1", "status": "ok", "output": "ok  github.com/rsdoiel/harvey 8.1s"})

	got := DraftTranscript(readBack(t, l), nil)
	if len(got) != 3 {
		t.Fatalf("want 3 entries (user, reply, tool call with its result), got %d:\n%s", len(got), transcriptText(got))
	}
	text := transcriptText(got)
	for _, want := range []string{"How do I run the tests here?", "Run go test in the harvey directory.", "run_command", "go test ./...", "ok", "8.1s"} {
		if !strings.Contains(text, want) {
			t.Errorf("transcript lacks %q:\n%s", want, text)
		}
	}
	if got[2].First != d.ids["call"] || got[2].Last != d.ids["result"] {
		t.Errorf("tool entry spans %s..%s, want %s..%s", got[2].First, got[2].Last, d.ids["call"], d.ids["result"])
	}
	if got[0].First != d.ids["user"] || got[0].Last != d.ids["user"] {
		t.Errorf("user entry spans %s..%s, want %s", got[0].First, got[0].Last, d.ids["user"])
	}
	// Order is the log's order.
	if strings.Index(text, "How do I run") > strings.Index(text, "Run go test in") || strings.Index(text, "Run go test in") > strings.Index(text, "run_command") {
		t.Errorf("entries are out of order:\n%s", text)
	}
}

func TestDraftTranscript_LeavesOutWhatTheSessionDidNotLearn(t *testing.T) {
	// Debug on, so the debug events below are really in the log.
	l, _ := openTestLog(t, func(o *SessionLogOptions) { o.Debug = true })
	d := &drafted{l: l, ids: map[string]string{}}
	d.add(t, "user", KindUserInput, LevelInfo, map[string]any{"turn": 1, "text": "keep this question"})
	d.add(t, "skill", KindInjection, LevelInfo, map[string]any{"source": "skill", "origin": "user", "ref": "go-tests", "text": "SKILL BODY SHOULD NOT APPEAR"})
	d.add(t, "kbinj", KindInjection, LevelInfo, map[string]any{"source": "kb", "origin": "auto", "ref": "kb:observation:1", "text": "RECALLED TEXT SHOULD NOT APPEAR"})
	d.add(t, "prompt", KindInjection, LevelInfo, map[string]any{"source": "prompt", "via": "turn", "role": "user", "message": "PROMPT COPY SHOULD NOT APPEAR"})
	d.add(t, "req", KindModelRequest, LevelInfo, map[string]any{"form": "assembled", "turn": 1, "prompt_sha256": "abc"})
	d.add(t, "bodyreq", KindModelRequest, LevelDebug, map[string]any{"form": "body", "body": "DEBUG BODY SHOULD NOT APPEAR"})
	d.add(t, "pin", KindPin, LevelInfo, map[string]any{"turn": 1, "ref": "text", "text": "PIN SHOULD NOT APPEAR", "tokens": 4})
	d.add(t, "comp", KindCompaction, LevelInfo, map[string]any{"turn": 1, "reason": "summary", "summary": "SUMMARY SHOULD NOT APPEAR"})
	d.add(t, "switch", KindModelSwitch, LevelInfo, map[string]any{"from": map[string]any{"model": "a"}, "to": map[string]any{"model": "b"}})
	d.add(t, "perm", KindPermission, LevelInfo, map[string]any{"action": "run", "target": "PERMISSION SHOULD NOT APPEAR", "decision": "allow"})
	d.add(t, "fault", KindFault, LevelDebug, map[string]any{"source": "scrubber", "message": "FAULT SHOULD NOT APPEAR"})
	d.add(t, "bad", KindModelResponse, LevelInfo, map[string]any{"turn": 1, "status": "error", "error": "ERROR SHOULD NOT APPEAR"})
	d.add(t, "cancelled", KindModelResponse, LevelInfo, map[string]any{"turn": 1, "status": "cancelled", "text": "PARTIAL REPLY SHOULD NOT APPEAR"})
	rk := d.add(t, "recall", KindKBCall, LevelInfo, map[string]any{"verb": "concept recall", "args": "zebra", "purpose": "retrieval", "turn": 1})
	d.add(t, "recallres", KindKBResult, LevelInfo, map[string]any{"call": rk, "status": "ok", "turn": 1, "output": "RETRIEVAL OUTPUT SHOULD NOT APPEAR"})
	d.add(t, "dbg", KindUserInput, LevelDebug, map[string]any{"text": "DEBUG INPUT SHOULD NOT APPEAR"})

	events := readBack(t, l)
	debug := 0
	for _, ev := range events {
		if ev.Level == LevelDebug {
			debug++
		}
	}
	if debug == 0 {
		t.Fatal("the log holds no debug events, so this test proves nothing")
	}
	got := DraftTranscript(events, nil)
	text := transcriptText(got)
	if len(got) != 1 || !strings.Contains(text, "keep this question") {
		t.Fatalf("want only the user question, got %d entries:\n%s", len(got), text)
	}
	if strings.Contains(text, "SHOULD NOT APPEAR") {
		t.Errorf("transcript holds something the design leaves out:\n%s", text)
	}
}

func TestDraftTranscript_ACallWithoutAResultIsKeptAndSaysSo(t *testing.T) {
	l, _ := openTestLog(t, nil)
	d := &drafted{l: l, ids: map[string]string{}}
	d.add(t, "call", KindToolCall, LevelInfo, map[string]any{"turn": 1, "tool": "read_file", "origin": "model", "call_id": "c9", "args": "main.go"})
	got := DraftTranscript(readBack(t, l), nil)
	if len(got) != 1 || !strings.Contains(got[0].Text, "read_file") || !strings.Contains(got[0].Text, "no result") {
		t.Fatalf("got %+v", got)
	}
	if got[0].First != d.ids["call"] || got[0].Last != d.ids["call"] {
		t.Errorf("span %s..%s", got[0].First, got[0].Last)
	}
}

func TestDraftTranscript_ACancelledOrFailedResultKeepsItsStatus(t *testing.T) {
	l, _ := openTestLog(t, nil)
	d := &drafted{l: l, ids: map[string]string{}}
	call := d.add(t, "call", KindToolCall, LevelInfo, map[string]any{"turn": 1, "tool": "run_command", "origin": "model", "call_id": "c1", "args": "make"})
	d.add(t, "res", KindToolResult, LevelInfo, map[string]any{"turn": 1, "call": call, "call_id": "c1", "status": "error", "output": "make: *** No rule to make target"})
	got := DraftTranscript(readBack(t, l), nil)
	if len(got) != 1 || !strings.Contains(got[0].Text, "error") || !strings.Contains(got[0].Text, "No rule to make target") {
		t.Fatalf("got %+v", got)
	}
}

func TestDraftTranscript_TheAuthorsShellAndKBCommandsAreKeptTheSameWay(t *testing.T) {
	l, _ := openTestLog(t, nil)
	d := &drafted{l: l, ids: map[string]string{}}
	sc := d.add(t, "sc", KindToolCall, LevelInfo, map[string]any{"tool": "shell", "origin": "user", "args": map[string]any{"cmd": "git status --short", "cwd": "<workspace>"}})
	d.add(t, "sr", KindToolResult, LevelInfo, map[string]any{"call": sc, "status": "ok", "output": " M harvey.go", "exit": 0})
	kc := d.add(t, "kc", KindKBCall, LevelInfo, map[string]any{"verb": "search", "args": "streaming", "purpose": "user_command"})
	d.add(t, "kr", KindKBResult, LevelInfo, map[string]any{"call": kc, "status": "ok", "output": "[observation] harvey — streaming closes the chunked response"})
	got := DraftTranscript(readBack(t, l), nil)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d:\n%s", len(got), transcriptText(got))
	}
	text := transcriptText(got)
	for _, want := range []string{"git status --short", "M harvey.go", "kb search", "streaming closes the chunked response"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

func TestDraftTranscript_ToolArgumentsAndOutputAreCut(t *testing.T) {
	l, _ := openTestLog(t, nil)
	d := &drafted{l: l, ids: map[string]string{}}
	call := d.add(t, "call", KindToolCall, LevelInfo, map[string]any{"turn": 1, "tool": "run_command", "origin": "model", "call_id": "c1", "args": strings.Repeat("A", 1000)})
	d.add(t, "res", KindToolResult, LevelInfo, map[string]any{"turn": 1, "call": call, "call_id": "c1", "status": "ok", "output": strings.Repeat("B", 2000)})
	got := DraftTranscript(readBack(t, l), nil)
	if len(got) != 1 {
		t.Fatal(got)
	}
	if n := strings.Count(got[0].Text, "A"); n > transcriptArgCap+10 {
		t.Errorf("arguments not cut: %d A's", n)
	}
	if n := strings.Count(got[0].Text, "B"); n > transcriptOutputCap+10 || n < 100 {
		t.Errorf("output cut to %d B's, want about %d", n, transcriptOutputCap)
	}
	if !strings.Contains(got[0].Text, "…") {
		t.Errorf("a cut is not marked:\n%s", got[0].Text)
	}
}

func TestDraftTranscript_SpilledOutputIsReadFromItsBlobAndCut(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 64 })
	d := &drafted{l: l, ids: map[string]string{}}
	call := d.add(t, "call", KindToolCall, LevelInfo, map[string]any{"turn": 1, "tool": "run_command", "origin": "model", "call_id": "c1", "args": "cat big"})
	big := strings.Repeat("line of output\n", 200)
	c, err := l.Content(big)
	if err != nil || c.Blob == "" {
		t.Fatalf("expected the output to spill: %+v, %v", c, err)
	}
	d.add(t, "res", KindToolResult, LevelInfo, map[string]any{"turn": 1, "call": call, "call_id": "c1", "status": "ok", "output": c})

	ev := readBack(t, l)
	withBlob := DraftTranscript(ev, LogBlobReader(logPath(opts)))
	if len(withBlob) != 1 || !strings.Contains(withBlob[0].Text, "line of output") {
		t.Fatalf("blob not read: %+v", withBlob)
	}
	if len(withBlob[0].Text) > transcriptOutputCap+300 {
		t.Errorf("a spilled output was not cut: %d bytes", len(withBlob[0].Text))
	}
	// Without a blob reader the preview stands in, and it is still the output's start.
	noBlob := DraftTranscript(ev, nil)
	if len(noBlob) != 1 || !strings.Contains(noBlob[0].Text, "line of output") {
		t.Errorf("preview not used: %+v", noBlob)
	}
}

func TestDraftTranscript_RedactionMarkersStayMarkers(t *testing.T) {
	l, _ := openTestLog(t, nil)
	d := &drafted{l: l, ids: map[string]string{}}
	c, err := l.Content("set api_key=hunter2hunter2hunter2 before running")
	if err != nil {
		t.Fatal(err)
	}
	d.add(t, "user", KindUserInput, LevelInfo, map[string]any{"turn": 1, "text": c})
	got := DraftTranscript(readBack(t, l), nil)
	if len(got) != 1 || strings.Contains(got[0].Text, "hunter2") || !strings.Contains(got[0].Text, "[REDACTED:") {
		t.Fatalf("the log's redaction must reach the transcript as a marker, never the value: %+v", got)
	}
}

func TestDraftTranscript_EmptyLogIsNoEntries(t *testing.T) {
	l, _ := openTestLog(t, nil)
	if got := DraftTranscript(readBack(t, l), nil); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

// /kb draft is itself in the log of the session it reads (loggedKB writes its
// kb_call before it runs), and an earlier run's report is there too. Neither is
// something the session learned.
func TestDraftTranscript_TheDraftCommandItselfIsLeftOut(t *testing.T) {
	l, _ := openTestLog(t, nil)
	d := &drafted{l: l, ids: map[string]string{}}
	d.add(t, "user", KindUserInput, LevelInfo, map[string]any{"turn": 1, "text": "a real question"})
	k1 := d.add(t, "k1", KindKBCall, LevelInfo, map[string]any{"verb": "draft", "args": "--project harvey", "purpose": "user_command"})
	d.add(t, "r1", KindKBResult, LevelInfo, map[string]any{"call": k1, "status": "ok", "output": "3 drafts written REPORT SHOULD NOT APPEAR"})
	k2 := d.add(t, "k2", KindKBCall, LevelInfo, map[string]any{"verb": "draft", "args": "", "purpose": "user_command"})
	_ = k2
	got := DraftTranscript(readBack(t, l), nil)
	if len(got) != 1 || !strings.Contains(got[0].Text, "a real question") {
		t.Fatalf("want only the question, got:\n%s", transcriptText(got))
	}
}
