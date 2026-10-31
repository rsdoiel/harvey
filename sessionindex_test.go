package harvey

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fillLog appends n turns of user_input, model_request, model_response and a
// tool pair, with a pin on turn 2, its unpin on turn 4, and a compaction after
// turn 3. It returns the pin id.
func fillLog(t *testing.T, l *SessionLog, n int) (pin string) {
	t.Helper()
	add := func(k EventKind, d any) string {
		id, err := l.Append(k, LevelInfo, d)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	var firstInput string
	for i := 1; i <= n; i++ {
		in := add(KindUserInput, map[string]any{"text": fmt.Sprintf("question %d", i)})
		if i == 1 {
			firstInput = in
		}
		rq := add(KindModelRequest, map[string]any{"turn": i, "form": "assembled"})
		call := add(KindToolCall, map[string]any{"turn": i, "tool": "read_file", "origin": "model"})
		add(KindToolResult, map[string]any{"turn": i, "of": call})
		add(KindModelResponse, map[string]any{"request": rq, "status": "ok", "text": strings.Repeat("answer ", 20)})
		switch i {
		case 2:
			pin = add(KindPin, map[string]any{"text": "keep it short"})
		case 3:
			add(KindCompaction, map[string]any{"sources": []string{firstInput, in}})
		case 4:
			add(KindUnpin, map[string]any{"pin": pin})
		}
	}
	return pin
}

func TestSessionIndex_LiveEqualsRebuilt(t *testing.T) {
	l, _ := openTestLog(t, nil)
	fillLog(t, l, 6)
	if err := l.Close("quit"); err != nil {
		t.Fatal(err)
	}
	rebuilt, torn, err := BuildSessionIndex(l.Path())
	if err != nil || torn {
		t.Fatalf("rebuild: torn=%v err=%v", torn, err)
	}
	if !l.Index().Equal(rebuilt) {
		t.Errorf("the live index and the one rebuilt from the file differ")
	}
	if rebuilt.Len() != 1+6*5+3+1 || rebuilt.Turns() != 6 {
		t.Errorf("Len=%d Turns=%d", rebuilt.Len(), rebuilt.Turns())
	}
}

func TestSessionIndex_PinsAndCompactions(t *testing.T) {
	l, _ := openTestLog(t, nil)
	pin := fillLog(t, l, 3)
	x := l.Index()
	if got := x.Pins(); len(got) != 1 || got[0] != pin {
		t.Errorf("Pins = %v, want [%s]", got, pin)
	}
	c := x.Compactions()
	if len(c) != 1 || len(c[0].Sources) != 2 {
		t.Fatalf("Compactions = %+v", c)
	}
	if _, ok := x.Lookup(c[0].Sources[0]); !ok {
		t.Errorf("a compaction source %s is not in the index", c[0].Sources[0])
	}
	fillLogUnpin := func() {
		if _, err := l.Append(KindUnpin, LevelInfo, map[string]any{"pin": pin}); err != nil {
			t.Fatal(err)
		}
	}
	fillLogUnpin()
	if got := x.Pins(); len(got) != 0 {
		t.Errorf("after unpin, Pins = %v", got)
	}
}

func TestSessionIndex_LastTurnsAndEvent(t *testing.T) {
	l, _ := openTestLog(t, func(o *SessionLogOptions) { o.Debug = true })
	fillLog(t, l, 5)
	if _, err := l.Append(KindFault, LevelDebug, map[string]any{"note": "debug event in the last turn"}); err != nil {
		t.Fatal(err)
	}
	x := l.Index()
	last := x.LastTurns(2)
	if len(last) == 0 || last[0].Kind != KindUserInput || last[0].Turn != 4 {
		t.Fatalf("LastTurns(2) starts at %+v, want turn 4's user_input", last)
	}
	inputs := 0
	for _, e := range last {
		if e.Level == LevelDebug {
			t.Errorf("a debug event %s was returned", e.ID)
		}
		if e.Kind == KindUserInput {
			inputs++
		}
	}
	if inputs != 2 {
		t.Errorf("LastTurns(2) has %d user inputs", inputs)
	}
	if got := x.LastTurns(99); got[0].Turn != 1 || got[0].Kind != KindUserInput {
		t.Errorf("LastTurns(99) should start at the first turn, got %+v", got[0])
	}
	if x.LastTurns(0) != nil {
		t.Error("LastTurns(0) should be nil")
	}
	ev, err := x.Event(last[0].ID)
	if err != nil || ev.Kind != KindUserInput || !strings.Contains(string(ev.Data), "question 4") {
		t.Errorf("Event(%s) = %+v, %v", last[0].ID, ev, err)
	}
	if _, err := x.Event("nope#0001"); ExitCodeFor(err) != ClassNegative {
		t.Errorf("an unknown id: err = %v, want negative", err)
	}
}

func TestSessionIndex_ARenamedLogStillServesEvents(t *testing.T) {
	l, _ := openTestLog(t, nil)
	fillLog(t, l, 2)
	id := l.Index().LastTurns(1)[0].ID
	if err := l.Rename("renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Index().Event(id); err != nil {
		t.Errorf("Event after rename: %v", err)
	}
}

func TestBuildSessionIndex_ATornTailIsIgnored(t *testing.T) {
	l, _ := openTestLog(t, nil)
	fillLog(t, l, 2)
	want := l.Index().Len()
	f, err := os.OpenFile(l.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"id":"x#9999","t":"2026`)
	f.Close()
	x, torn, err := BuildSessionIndex(l.Path())
	if err != nil || !torn || x.Len() != want {
		t.Errorf("torn=%v err=%v len=%d, want torn, no error, %d events", torn, err, x.Len(), want)
	}
}

func TestBuildSessionIndex_Errors(t *testing.T) {
	if _, _, err := BuildSessionIndex(t.TempDir() + "/none.jsonl"); ExitCodeFor(err) != ClassNoInput {
		t.Errorf("missing file: %v", err)
	}
	p := t.TempDir() + "/bad.jsonl"
	os.WriteFile(p, []byte("not json\n{\"id\":\"a#0001\"}\n"), 0o644)
	if _, _, err := BuildSessionIndex(p); ExitCodeFor(err) != ClassData {
		t.Errorf("a bad interior line: %v", err)
	}
}

// TestSessionIndex_Measure builds the 2,000-event log the plan calls for and
// logs the rebuild time and the memory of the index; run with -v to see them.
// The bounds are loose: they catch a design that is off by an order of
// magnitude, not a Pi that is a little slow.
func TestSessionIndex_Measure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes int // size of each model_response text
	}{{"small", 140}, {"heavy", 6000}} {
		t.Run(tc.name, func(t *testing.T) { measureIndex(t, tc.bytes) })
	}
}

func measureIndex(t *testing.T, textBytes int) {
	l, _ := openTestLog(t, nil)
	fillLog(t, l, 400) // 5 events a turn plus a few: about 2,000
	if textBytes > 140 {
		// Top up with large responses so the file is the size a long session makes.
		for i := 0; i < 400; i++ {
			if _, err := l.Append(KindModelResponse, LevelInfo, map[string]any{"status": "ok", "text": strings.Repeat("x", textBytes)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := l.Close("quit"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(l.Path())

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	x, _, err := BuildSessionIndex(l.Path())
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	mem := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%d events, %d bytes of log: rebuild %v, index heap about %d KB", x.Len(), fi.Size(), took, mem/1024)
	runtime.KeepAlive(x)

	if took > 2*time.Second {
		t.Errorf("rebuild took %v for %d events", took, x.Len())
	}
	if mem > 8<<20 {
		t.Errorf("index heap %d bytes for %d events", mem, x.Len())
	}
	if !l.Index().Equal(x) {
		t.Error("live and rebuilt indexes differ")
	}
}
