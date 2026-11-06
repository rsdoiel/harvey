package harvey

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testLogOpts(t *testing.T) SessionLogOptions {
	t.Helper()
	tick := time.Date(2026, 10, 30, 14, 12, 0, 0, time.UTC)
	return SessionLogOptions{
		Dir:     t.TempDir(),
		Session: "20261030-1412-harvey",
		Start: SessionStart{
			HarveyVersion: "0.0.test",
			Workspace:     "/ws",
			Backend:       "ollama",
			Model:         "llama3.2:3b",
			System:        SystemPrompt{ID: "sys-v1", Text: "You are Harvey."},
			Window:        Window{N: 4096, Source: "configured"},
		},
		Now: func() time.Time { tick = tick.Add(time.Second); return tick },
	}
}

func openTestLog(t *testing.T, mod func(*SessionLogOptions)) (*SessionLog, SessionLogOptions) {
	t.Helper()
	opts := testLogOpts(t)
	if mod != nil {
		mod(&opts)
	}
	l, err := OpenSessionLog(opts)
	if err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	t.Cleanup(func() { _ = l.Close("test") })
	return l, opts
}

func logPath(opts SessionLogOptions) string {
	return filepath.Join(opts.Dir, opts.Session+".jsonl")
}

func mustAppend(t *testing.T, l *SessionLog, kind EventKind, level EventLevel, data any) string {
	t.Helper()
	id, err := l.Append(kind, level, data)
	if err != nil {
		t.Fatalf("Append(%s): %v", kind, err)
	}
	return id
}

func TestSessionLog_FirstEventIsSessionStart(t *testing.T) {
	_, opts := openTestLog(t, nil)
	events, torn, err := ReadSessionLog(logPath(opts))
	if err != nil || torn {
		t.Fatalf("ReadSessionLog: torn=%v err=%v", torn, err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.ID != "20261030-1412-harvey#0001" || e.Kind != KindSessionStart || e.Level != LevelInfo {
		t.Errorf("first event = %+v", e)
	}
	var d struct {
		V       int    `json:"v"`
		Session string `json:"session"`
		Model   string `json:"model"`
		System  struct {
			ID     string `json:"id"`
			SHA256 string `json:"sha256"`
			Text   string `json:"text"`
		} `json:"system"`
		Window Window `json:"window"`
		Debug  bool   `json:"debug"`
	}
	if err := json.Unmarshal(e.Data, &d); err != nil {
		t.Fatalf("session_start data: %v", err)
	}
	sum := sha256.Sum256([]byte("You are Harvey."))
	if d.V != 1 || d.Session != opts.Session || d.Model != "llama3.2:3b" || d.System.ID != "sys-v1" ||
		d.System.SHA256 != hex.EncodeToString(sum[:]) || d.System.Text != "You are Harvey." ||
		d.Window.N != 4096 || d.Debug {
		t.Errorf("session_start data = %+v", d)
	}
}

func TestSessionLog_RejectsBadSessionIDs(t *testing.T) {
	for _, id := range []string{"", "a/b", "a#b", "..", `a\b`} {
		opts := testLogOpts(t)
		opts.Session = id
		if _, err := OpenSessionLog(opts); err == nil {
			t.Errorf("session %q: want an error", id)
		} else if c := ExitCodeFor(err); c != ClassUsage {
			t.Errorf("session %q: class %v, want usage", id, c)
		}
	}
}

func TestSessionLog_ExistingLogIsNotOverwritten(t *testing.T) {
	_, opts := openTestLog(t, nil)
	before, _ := os.ReadFile(logPath(opts))
	_, err := OpenSessionLog(opts)
	if err == nil {
		t.Fatal("second open of the same session succeeded")
	}
	if c := ExitCodeFor(err); c != ClassCantCreate {
		t.Errorf("class %v, want cant_create", c)
	}
	after, _ := os.ReadFile(logPath(opts))
	if !bytes.Equal(before, after) {
		t.Error("the existing log was modified")
	}
}

func TestSessionLog_IDsAreGaplessAndOrdered(t *testing.T) {
	l, opts := openTestLog(t, nil)
	var ids []string
	for i := 0; i < 12; i++ {
		ids = append(ids, mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "hi"}))
	}
	if ids[0] != "20261030-1412-harvey#0002" || ids[11] != "20261030-1412-harvey#0013" {
		t.Errorf("ids = %v", ids)
	}
	events, _, err := ReadSessionLog(logPath(opts))
	if err != nil || len(events) != 13 {
		t.Fatalf("read: %d events, %v", len(events), err)
	}
}

func TestSessionLog_EveryAppendIsCompleteOnDisk(t *testing.T) {
	l, opts := openTestLog(t, nil)
	for i := 0; i < 5; i++ {
		mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": strings.Repeat("x", i*10)})
		raw, err := os.ReadFile(logPath(opts))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasSuffix(raw, []byte("\n")) {
			t.Fatalf("after append %d the file does not end in a newline", i)
		}
		for n, line := range bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n")) {
			if !json.Valid(line) {
				t.Fatalf("after append %d line %d is not complete JSON: %q", i, n+1, line)
			}
		}
	}
}

func TestSessionLog_TimestampComesFromTheClock(t *testing.T) {
	l, opts := openTestLog(t, nil)
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "x"})
	events, _, _ := ReadSessionLog(logPath(opts))
	if events[0].T != "2026-10-30T14:12:01Z" || events[1].T != "2026-10-30T14:12:02Z" {
		t.Errorf("t = %q, %q", events[0].T, events[1].T)
	}
}

func TestSessionLog_DebugEventsOnlyWithDebugAndNoGap(t *testing.T) {
	for _, debug := range []bool{false, true} {
		l, opts := openTestLog(t, func(o *SessionLogOptions) {
			o.Session = map[bool]string{false: "s-nodebug", true: "s-debug"}[debug]
			o.Debug = debug
		})
		a := mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "a"})
		d := mustAppend(t, l, KindFault, LevelDebug, map[string]any{"source": "backend", "message": "m", "severity": "warning"})
		b := mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "b"})
		events, _, err := ReadSessionLog(logPath(opts))
		if err != nil {
			t.Fatalf("debug=%v read: %v", debug, err)
		}
		if debug {
			if d == "" || len(events) != 4 || events[3].ID != b {
				t.Errorf("debug on: d=%q events=%d b=%q", d, len(events), b)
			}
		} else {
			if d != "" || len(events) != 3 {
				t.Errorf("debug off: a debug event was written: d=%q events=%d", d, len(events))
			}
			if a != opts.Session+"#0002" || b != opts.Session+"#0003" {
				t.Errorf("debug off left a gap: a=%q b=%q", a, b)
			}
		}
	}
}

func TestSessionLog_SessionStartRecordsDebug(t *testing.T) {
	_, opts := openTestLog(t, func(o *SessionLogOptions) { o.Debug = true })
	events, _, _ := ReadSessionLog(logPath(opts))
	var d struct {
		Debug bool `json:"debug"`
	}
	_ = json.Unmarshal(events[0].Data, &d)
	if !d.Debug {
		t.Error("session_start.debug should be true when the log runs with --debug")
	}
}

func TestSessionLog_SessionStartRecordsReplayOf(t *testing.T) {
	_, opts := openTestLog(t, func(o *SessionLogOptions) { o.Start.ReplayOf = "20261001-0900-harvey" })
	events, _, _ := ReadSessionLog(logPath(opts))
	var d map[string]any
	_ = json.Unmarshal(events[0].Data, &d)
	if d["replay_of"] != "20261001-0900-harvey" {
		t.Errorf("replay_of = %v", d["replay_of"])
	}
	_, opts2 := openTestLog(t, func(o *SessionLogOptions) { o.Session = "plain" })
	events2, _, _ := ReadSessionLog(logPath(opts2))
	var d2 map[string]any
	_ = json.Unmarshal(events2[0].Data, &d2)
	if _, present := d2["replay_of"]; present {
		t.Error("replay_of must be absent for a normal session")
	}
}

func TestSessionLog_RejectsUnknownKindAndLevel(t *testing.T) {
	l, _ := openTestLog(t, nil)
	if _, err := l.Append(EventKind("bogus"), LevelInfo, map[string]any{}); err == nil {
		t.Error("unknown kind accepted")
	}
	if _, err := l.Append(KindUserInput, EventLevel("loud"), map[string]any{}); err == nil {
		t.Error("unknown level accepted")
	}
}

func TestSessionLog_KindVocabularyIsClosedAndComplete(t *testing.T) {
	want := []EventKind{
		"session_start", "session_end", "user_input", "model_request", "model_response",
		"tool_call", "tool_result", "kb_call", "kb_result", "injection", "compaction",
		"pin", "unpin", "model_switch", "command", "permission", "fault",
	}
	got := EventKinds()
	if len(got) != len(want) {
		t.Fatalf("%d kinds, want %d (DR-0039 as amended by DR-0040): %v", len(got), len(want), got)
	}
	for _, k := range want {
		if !k.Valid() {
			t.Errorf("%q is not a valid kind", k)
		}
	}
}

func TestSessionLog_SmallContentStaysInline(t *testing.T) {
	l, opts := openTestLog(t, nil)
	c, err := l.Content("hello")
	if err != nil {
		t.Fatal(err)
	}
	if c.Blob != "" || c.Text != "hello" {
		t.Errorf("content = %+v, want inline", c)
	}
	raw, _ := json.Marshal(c)
	if string(raw) != `"hello"` {
		t.Errorf("inline content marshals to %s, want a bare JSON string", raw)
	}
	if entries, _ := os.ReadDir(filepath.Join(opts.Dir, opts.Session+".blobs")); len(entries) != 0 {
		t.Errorf("a small payload made %d blob files", len(entries))
	}
}

func TestSessionLog_LargeContentSpillsToContentAddressedBlob(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 64 })
	big := strings.Repeat("0123456789", 20)
	c, err := l.Content(big)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(big))
	wantHash := hex.EncodeToString(sum[:])
	if c.Blob != wantHash || c.Bytes != len(big) || c.Text != "" {
		t.Errorf("content = %+v, want blob %s", c, wantHash)
	}
	if !strings.HasPrefix(big, c.Preview) || len(c.Preview) == 0 || len(c.Preview) > 200 {
		t.Errorf("preview = %q", c.Preview)
	}
	got, err := os.ReadFile(filepath.Join(opts.Dir, opts.Session+".blobs", wantHash))
	if err != nil || string(got) != big {
		t.Errorf("blob file: %v, %d bytes", err, len(got))
	}
	raw, _ := json.Marshal(c)
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj["blob"] != wantHash || obj["bytes"] != float64(len(big)) {
		t.Errorf("blob content marshals to %s", raw)
	}
}

func TestSessionLog_SameContentTwiceIsOneBlob(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 16 })
	big := strings.Repeat("abc", 100)
	a, _ := l.Content(big)
	b, _ := l.Content(big)
	if a.Blob != b.Blob {
		t.Errorf("hashes differ: %s %s", a.Blob, b.Blob)
	}
	entries, _ := os.ReadDir(filepath.Join(opts.Dir, opts.Session+".blobs"))
	if len(entries) != 1 {
		t.Errorf("%d blob files, want 1", len(entries))
	}
}

func TestSessionLog_BlobExistsBeforeAnyEventNamesIt(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 16 })
	c, err := l.Content(strings.Repeat("z", 100))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(opts.Dir, opts.Session+".blobs", c.Blob)); err != nil {
		t.Fatalf("Content returned before the blob was on disk: %v", err)
	}
	if raw, _ := os.ReadFile(logPath(opts)); bytes.Contains(raw, []byte(c.Blob)) {
		t.Fatal("the log names the blob before an event was appended")
	}
	mustAppend(t, l, KindToolResult, LevelInfo, map[string]any{"output": c})
	if raw, _ := os.ReadFile(logPath(opts)); !bytes.Contains(raw, []byte(c.Blob)) {
		t.Fatal("the appended event does not name the blob")
	}
}

func TestSessionLog_EveryBlobReferenceResolves(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 8 })
	for i := 0; i < 5; i++ {
		c, _ := l.Content(strings.Repeat(string(rune('a'+i)), 50))
		mustAppend(t, l, KindToolResult, LevelInfo, map[string]any{"output": c})
	}
	events, _, _ := ReadSessionLog(logPath(opts))
	seen := 0
	for _, e := range events {
		var d struct {
			Output *Content `json:"output"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.Output != nil && d.Output.Blob != "" {
			seen++
			if _, err := os.Stat(filepath.Join(opts.Dir, opts.Session+".blobs", d.Output.Blob)); err != nil {
				t.Errorf("%s names a missing blob: %v", e.ID, err)
			}
		}
	}
	if seen != 5 {
		t.Errorf("found %d blob references, want 5", seen)
	}
}

func TestSessionLog_ContentIsScrubbedBeforeHashing(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 16 })
	raw := "API_KEY=sk-live-123 " + strings.Repeat("filler ", 20)
	c, err := l.Content(raw)
	if err != nil {
		t.Fatal(err)
	}
	scrubbed, _ := ScrubEvent(raw, "/ws")
	sum := sha256.Sum256([]byte(scrubbed))
	if c.Blob != hex.EncodeToString(sum[:]) {
		t.Errorf("blob hash is not of the scrubbed text")
	}
	got, _ := os.ReadFile(filepath.Join(opts.Dir, opts.Session+".blobs", c.Blob))
	if bytes.Contains(got, []byte("sk-live-123")) || !bytes.Contains(got, []byte("[REDACTED:secret]")) {
		t.Errorf("blob holds %q", got)
	}
	small, _ := l.Content("token=abc")
	if strings.Contains(small.Text, "abc") {
		t.Errorf("inline content not scrubbed: %q", small.Text)
	}
}

func TestSessionLog_ContentNormalisesTheWorkspacePath(t *testing.T) {
	l, _ := openTestLog(t, nil)
	c, _ := l.Content("open /ws/src/main.go")
	if c.Text != "open <workspace>/src/main.go" {
		t.Errorf("text = %q", c.Text)
	}
}

func TestSessionLog_RedactionNoteOnlyWithDebugAndNeverHoldsTheValue(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.Debug = true })
	if _, err := l.Content("PASSWORD=hunter2 and TOKEN=abc"); err != nil {
		t.Fatal(err)
	}
	events, _, _ := ReadSessionLog(logPath(opts))
	var note *Event
	for i := range events {
		if events[i].Kind == KindFault {
			note = &events[i]
		}
	}
	if note == nil {
		t.Fatal("no scrubber note")
	}
	if note.Level != LevelDebug {
		t.Errorf("note level = %s, want debug", note.Level)
	}
	var d struct {
		Source, Message, Severity string
	}
	_ = json.Unmarshal(note.Data, &d)
	d2 := map[string]string{}
	_ = json.Unmarshal(note.Data, &d2)
	if d2["source"] != "scrubber" || !strings.Contains(d2["message"], "secret 2") {
		t.Errorf("note = %s", note.Data)
	}
	if bytes.Contains(note.Data, []byte("hunter2")) || bytes.Contains(note.Data, []byte("abc")) {
		t.Errorf("note leaks a value: %s", note.Data)
	}
	// Without --debug the note is not written.
	l2, opts2 := openTestLog(t, func(o *SessionLogOptions) { o.Session = "quiet" })
	_, _ = l2.Content("PASSWORD=hunter2")
	ev2, _, _ := ReadSessionLog(logPath(opts2))
	if len(ev2) != 1 {
		t.Errorf("a quiet log has %d events, want only session_start", len(ev2))
	}
}

func TestSessionLog_FlagsTravelWithTheContent(t *testing.T) {
	l, _ := openTestLog(t, nil)
	c, _ := l.Content("rotate the password someday")
	if c.Text != "rotate the password someday" {
		t.Errorf("a bare word must not be redacted: %q", c.Text)
	}
	if len(c.Flags) == 0 {
		t.Fatal("no flags on content with a sensitive word")
	}
	raw, _ := json.Marshal(c)
	var back Content
	if err := json.Unmarshal(raw, &back); err != nil || back.Text != c.Text || len(back.Flags) != len(c.Flags) {
		t.Errorf("flags did not round trip: %s -> %+v, %v", raw, back, err)
	}
}

func TestContent_UnmarshalAcceptsStringAndObject(t *testing.T) {
	var a, b Content
	if err := json.Unmarshal([]byte(`"plain"`), &a); err != nil || a.Text != "plain" {
		t.Errorf("string form: %+v, %v", a, err)
	}
	if err := json.Unmarshal([]byte(`{"blob":"abc","bytes":9,"preview":"p"}`), &b); err != nil || b.Blob != "abc" || b.Bytes != 9 {
		t.Errorf("object form: %+v, %v", b, err)
	}
}

func TestSessionLog_CloseWritesSessionEndAndSealsTheLog(t *testing.T) {
	l, opts := openTestLog(t, nil)
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "x"})
	if err := l.Close("quit"); err != nil {
		t.Fatal(err)
	}
	events, torn, err := ReadSessionLog(logPath(opts))
	if err != nil || torn {
		t.Fatalf("read: torn=%v err=%v", torn, err)
	}
	last := events[len(events)-1]
	if last.Kind != KindSessionEnd {
		t.Fatalf("last event = %s", last.Kind)
	}
	var d struct {
		Reason string `json:"reason"`
		Events int    `json:"events"`
	}
	_ = json.Unmarshal(last.Data, &d)
	if d.Reason != "quit" || d.Events != 3 || last.ID != opts.Session+"#0003" {
		t.Errorf("session_end = %+v, id %s", d, last.ID)
	}
	if _, err := l.Append(KindUserInput, LevelInfo, map[string]any{}); err == nil {
		t.Error("Append after Close succeeded")
	}
	if err := l.Close("again"); err != nil {
		t.Errorf("second Close: %v", err)
	}
	again, _, _ := ReadSessionLog(logPath(opts))
	if len(again) != len(events) {
		t.Error("a second Close wrote another event")
	}
}

func TestReadSessionLog_RejectsAGapInTheSequence(t *testing.T) {
	l, opts := openTestLog(t, nil)
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "a"})
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "b"})
	_ = l.Close("quit")
	raw, _ := os.ReadFile(logPath(opts))
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	lines = append(lines[:2], lines[3:]...) // drop event 3
	bad := filepath.Join(t.TempDir(), "gap.jsonl")
	_ = os.WriteFile(bad, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	_, _, err := ReadSessionLog(bad)
	if err == nil || ExitCodeFor(err) != ClassData {
		t.Errorf("gap: err = %v, class %v; want a data error", err, ExitCodeFor(err))
	}
}

func TestReadSessionLog_RejectsUnknownKindAndForeignSession(t *testing.T) {
	dir := t.TempDir()
	start := `{"id":"s#0001","t":"x","kind":"session_start","level":"info","data":{"v":1,"session":"s"}}`
	cases := map[string]string{
		"unknown kind":            start + "\n" + `{"id":"s#0002","t":"x","kind":"bogus","level":"info","data":{}}` + "\n",
		"foreign session":         start + "\n" + `{"id":"other#0002","t":"x","kind":"user_input","level":"info","data":{}}` + "\n",
		"bad level":               start + "\n" + `{"id":"s#0002","t":"x","kind":"user_input","level":"loud","data":{}}` + "\n",
		"not session_start first": `{"id":"s#0001","t":"x","kind":"user_input","level":"info","data":{}}` + "\n",
		"bad json inside":         start + "\n" + "{not json}\n" + `{"id":"s#0003","t":"x","kind":"user_input","level":"info","data":{}}` + "\n",
	}
	for name, body := range cases {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".jsonl")
		_ = os.WriteFile(p, []byte(body), 0o644)
		if _, _, err := ReadSessionLog(p); err == nil || ExitCodeFor(err) != ClassData {
			t.Errorf("%s: err = %v, want a data error", name, err)
		}
	}
}

func TestReadSessionLog_ToleratesATornFinalLine(t *testing.T) {
	l, opts := openTestLog(t, nil)
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "a"})
	f, _ := os.OpenFile(logPath(opts), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"id":"20261030-1412-harvey#0003","t":"x","ki`)
	_ = f.Close()
	events, torn, err := ReadSessionLog(logPath(opts))
	if err != nil {
		t.Fatalf("torn final line: %v", err)
	}
	if !torn || len(events) != 2 {
		t.Errorf("torn=%v events=%d, want torn and the 2 complete events", torn, len(events))
	}
}

func TestReadSessionLog_MissingFileIsNoInput(t *testing.T) {
	_, _, err := ReadSessionLog(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err == nil || ExitCodeFor(err) != ClassNoInput {
		t.Errorf("err = %v, class %v; want no_input", err, ExitCodeFor(err))
	}
}

func TestSessionLog_RoundTripsAScriptedSession(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 32; o.Debug = true })
	big, _ := l.Content(strings.Repeat("file line\n", 30))
	want := []string{
		mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "read main.go"}),
		mustAppend(t, l, KindToolCall, LevelInfo, map[string]any{"turn": 1, "tool": "read_file", "origin": "model", "call_id": "c1"}),
		mustAppend(t, l, KindToolResult, LevelInfo, map[string]any{"turn": 1, "call_id": "c1", "status": "ok", "output": big}),
		mustAppend(t, l, KindModelResponse, LevelInfo, map[string]any{"turn": 1, "status": "ok"}),
		mustAppend(t, l, KindPermission, LevelInfo, map[string]any{"action": "write", "decision": "allow", "scope": "once"}),
		mustAppend(t, l, KindFault, LevelDebug, map[string]any{"source": "probe", "message": "slow", "severity": "warning"}),
	}
	_ = l.Close("quit")
	events, torn, err := ReadSessionLog(logPath(opts))
	if err != nil || torn {
		t.Fatalf("read: torn=%v err=%v", torn, err)
	}
	if len(events) != len(want)+2 {
		t.Fatalf("%d events, want %d", len(events), len(want)+2)
	}
	for i, id := range want {
		if events[i+1].ID != id {
			t.Errorf("event %d id = %s, want %s", i+1, events[i+1].ID, id)
		}
	}
}

func TestSessionLog_ConcurrentAppendsStayGapless(t *testing.T) {
	l, opts := openTestLog(t, nil)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if _, err := l.Append(KindUserInput, LevelInfo, map[string]any{"text": "x"}); err != nil {
					t.Errorf("Append: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	_ = l.Close("quit")
	events, _, err := ReadSessionLog(logPath(opts))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 1+8*25+1 {
		t.Errorf("%d events, want %d", len(events), 1+8*25+1)
	}
}

func TestSessionLog_SessionStartStaysFirstWhenTheSystemPromptIsRedacted(t *testing.T) {
	_, opts := openTestLog(t, func(o *SessionLogOptions) {
		o.Debug = true
		o.Start.System.Text = "Use API_KEY=sk-live-999 when calling the service."
	})
	events, _, err := ReadSessionLog(logPath(opts))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if events[0].Kind != KindSessionStart {
		t.Fatalf("first event is %s, want session_start", events[0].Kind)
	}
	if bytes.Contains(events[0].Data, []byte("sk-live-999")) {
		t.Errorf("the system prompt was stored unscrubbed: %s", events[0].Data)
	}
	if len(events) < 2 || events[1].Kind != KindFault {
		t.Errorf("the scrubber note should follow session_start: %+v", events)
	}
}
