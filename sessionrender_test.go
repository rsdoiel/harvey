package harvey

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	renderTimeRE  = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
	renderDateRE  = regexp.MustCompile(`Draft date: \d{4}-\d{2}-\d{2}`)
	renderStatsRE = regexp.MustCompile(`· [0-9.]+(µs|ms|s) · [0-9.]+ tok/s`)
	renderDurRE   = regexp.MustCompile(`(?m)^(.+ \(.+\)) · [0-9.]+(µs|ms|s)$`)
	renderWSRE    = regexp.MustCompile(`Workspace: \S+\.`)
)

// normaliseFountain removes what differs between two runs of the same
// session: the times, and the timings in a stats line.
func normaliseFountain(s string) string {
	s = renderTimeRE.ReplaceAllString(s, "<ts>")
	s = renderDateRE.ReplaceAllString(s, "Draft date: <date>")
	s = renderStatsRE.ReplaceAllString(s, "· <dur> · <rate> tok/s")
	s = renderDurRE.ReplaceAllString(s, "$1 · <dur>")
	s = renderWSRE.ReplaceAllString(s, "Workspace: <ws>.")
	return s
}

// goldenRoot is the package directory, taken before any test changes directory.
var goldenRoot, _ = os.Getwd()

// goldenCheck compares a rendering with the golden file testdata/fountain/NAME.golden.
// The golden files are the live recorder's output for a scripted scenario,
// normalised, captured before the recorder's live writes were removed, except
// where the file says otherwise (the recorder lost tool notes the rendering has).
// Run with UPDATE_GOLDEN=1 to write them: the file is written only if the test has
// not failed, that is, only from a rendering already proved equal to the recorder.
func goldenCheck(t *testing.T, name, rendered string) {
	t.Helper()
	path := filepath.Join(goldenRoot, "testdata", "fountain", name+".golden")
	got := normaliseFountain(rendered)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if t.Failed() {
			t.Fatalf("not updating %s: the rendering did not match the live recorder", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden file %s (UPDATE_GOLDEN=1 writes it): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("the rendering differs from %s\n--- golden ---\n%s\n--- rendering ---\n%s", path, want, got)
	}
}

// recordedAndRendered runs a scripted session and returns what the live
// recorder wrote and what the renderer makes of the session log.
func recordedAndRendered(t *testing.T, debug bool, tools bool, lines ...string) (recorded, rendered string) {
	t.Helper()
	dir := scriptedRunWith(t, debug, func(root string) {
		os.WriteFile(filepath.Join(root, "note.txt"), []byte("the note says hello"), 0o644)
	}, tools, lines...)
	spmd, _ := filepath.Glob(filepath.Join(dir, "*.spmd"))
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(spmd) != 1 || len(logs) != 1 {
		t.Fatalf("want one .spmd and one .jsonl in %s, got %v %v", dir, spmd, logs)
	}
	rec, _ := os.ReadFile(spmd[0])
	events, torn, err := ReadSessionLog(logs[0])
	if err != nil || torn {
		t.Fatalf("ReadSessionLog: torn=%v err=%v", torn, err)
	}
	out := RenderFountain(events, RenderOptions{Zone: time.Local, Blobs: BlobDir(logs[0])})
	return string(rec), string(out)
}

func TestRenderFountain_MatchesTheLiveRecorder(t *testing.T) {
	rec, got := recordedAndRendered(t, false, false,
		"hello", "/model use qwen2.5-coder:1.5b", "again", "!echo hi", "/exit")
	if a, b := normaliseFountain(rec), normaliseFountain(got); a != b {
		t.Errorf("rendered Fountain differs from the recorder's\n--- recorder ---\n%s\n--- renderer ---\n%s", a, b)
	}
	goldenCheck(t, "turns_switch_shell", got)
}

func TestRenderFountain_DebugEventsDoNotAppear(t *testing.T) {
	rec, got := recordedAndRendered(t, true, false, "hello", "/exit")
	if a, b := normaliseFountain(rec), normaliseFountain(got); a != b {
		t.Errorf("--debug changed the rendering\n--- recorder ---\n%s\n--- renderer ---\n%s", a, b)
	}
	goldenCheck(t, "debug_hello", got)
}

// The recorder dropped the tool notes of a turn whose tool round was compacted;
// the log has the calls, so the renderer shows them, between the forwarding line
// and the reply, as DR-0014 says.
func TestRenderFountain_ToolNotesAreIncludedWhereTheRecorderLostThem(t *testing.T) {
	rec, got := recordedAndRendered(t, false, true, "read note.txt", "/exit")
	if strings.Contains(rec, "[[tool:") {
		t.Skip("the recorder now keeps tool notes; the comparison below is no longer the point")
	}
	fwd := strings.Index(got, "Forwarding to LLAMA3.2.")
	note := strings.Index(got, "[[tool: read_file(")
	reply := strings.Index(got, "\nLLAMA3.2\nTOOLDONE")
	if fwd < 0 || note < 0 || reply < 0 || !(fwd < note && note < reply) {
		t.Fatalf("want forwarding < tool note < reply, got %d %d %d in\n%s", fwd, note, reply, got)
	}
	if !strings.Contains(got, `note.txt`) || !strings.Contains(got, ") — ok]]") {
		t.Errorf("tool note should carry the arguments and the status ok:\n%s", got)
	}
	// Apart from that one note the two agree.
	noteLine := regexp.MustCompile(`(?m)^\[\[tool: .*\]\]\n\n`).FindString(got)
	if a, b := normaliseFountain(rec), normaliseFountain(strings.Replace(got, noteLine, "", 1)); a != b {
		t.Errorf("apart from the tool note the rendering should equal the recorder's\n--- recorder ---\n%s\n--- renderer ---\n%s", a, b)
	}
	// This golden is the rendering, which has the tool note the recorder lost.
	goldenCheck(t, "tool_call", got)
}

func TestRenderFountain_IsAPureFunctionOfTheLogAndTheZone(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.Start.User = "ALICE" })
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "hi"})
	_ = l.Close("quit")
	events, _, _ := ReadSessionLog(logPath(opts))
	a := RenderFountain(events, RenderOptions{Zone: time.UTC})
	b := RenderFountain(events, RenderOptions{Zone: time.UTC})
	if string(a) != string(b) {
		t.Error("two renders of the same log differ")
	}
	east := RenderFountain(events, RenderOptions{Zone: time.FixedZone("x", 5*3600)})
	if string(east) == string(a) {
		t.Error("the zone should change the times shown")
	}
	if normaliseFountain(string(east)) != normaliseFountain(string(a)) {
		t.Error("apart from the times the zone must change nothing")
	}
	if !strings.Contains(string(a), "Author: ALICE") || !strings.Contains(string(a), "ALICE") {
		t.Errorf("the user name comes from session_start, not the environment:\n%s", a)
	}
}

func TestRenderFountain_ACrashedSessionRendersAndSaysSo(t *testing.T) {
	l, opts := openTestLog(t, nil)
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "hi"})
	// no Close: the process died
	events, _, err := ReadSessionLog(logPath(opts))
	if err != nil {
		t.Fatal(err)
	}
	got := string(RenderFountain(events, RenderOptions{Zone: time.UTC}))
	if !strings.Contains(got, "THE END.") {
		t.Errorf("a crashed session still ends:\n%s", got)
	}
	if !strings.Contains(got, "[[session ended without a clean close]]") {
		t.Errorf("a crashed session should be marked:\n%s", got)
	}
}

func TestRenderFountain_AFailedTurnIsShownNotDropped(t *testing.T) {
	l, opts := openTestLog(t, nil)
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "will it work"})
	req := mustAppend(t, l, KindModelRequest, LevelInfo, map[string]any{"form": "assembled", "turn": 1, "backend": "ollama", "model": "llama3.2:3b"})
	mustAppend(t, l, KindModelResponse, LevelInfo, map[string]any{"turn": 1, "request": req, "status": "error", "error": "connection refused"})
	_ = l.Close("quit")
	events, _, _ := ReadSessionLog(logPath(opts))
	got := string(RenderFountain(events, RenderOptions{Zone: time.UTC}))
	if !strings.Contains(got, "will it work") || !strings.Contains(got, "[[model error: connection refused]]") {
		t.Errorf("the turn and its failure should both appear:\n%s", got)
	}
}

func TestRenderFountain_ShellOutputComesFromABlobWhenSpilled(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.SpillCap = 16 })
	big := strings.Repeat("line of output\n", 20)
	cmd, _ := l.Content("seq 1 20")
	callID := mustAppend(t, l, KindToolCall, LevelInfo, map[string]any{"tool": "shell", "origin": "user", "args": map[string]any{"cmd": cmd}})
	out, _ := l.Content(big)
	mustAppend(t, l, KindToolResult, LevelInfo, map[string]any{"call": callID, "call_id": callID, "status": "ok", "exit": 0, "output": out})
	_ = l.Close("quit")
	events, _, _ := ReadSessionLog(logPath(opts))
	withBlobs := string(RenderFountain(events, RenderOptions{Zone: time.UTC, Blobs: BlobDir(logPath(opts))}))
	if !strings.Contains(withBlobs, strings.TrimSpace(big)) {
		t.Errorf("the spilled output should be read back from its blob:\n%s", withBlobs)
	}
	without := string(RenderFountain(events, RenderOptions{Zone: time.UTC}))
	if strings.Contains(without, strings.TrimSpace(big)) || !strings.Contains(without, "line of output") {
		t.Errorf("without a blob reader the preview is shown, and nothing is invented:\n%s", without)
	}
	if !strings.Contains(withBlobs, "[[shell: seq 1 20 — exit 0]]") {
		t.Errorf("the shell note is missing:\n%s", withBlobs)
	}
}

func TestRenderFountain_SecretsStayRedacted(t *testing.T) {
	rec, got := recordedAndRendered(t, false, false, "!echo API_KEY=sk-live-424", "/exit")
	if strings.Contains(got, "sk-live-424") || !strings.Contains(got, "[REDACTED:secret]") {
		t.Errorf("the rendering must keep the log's redaction:\n%s", got)
	}
	_ = rec // the recorder, which writes what was typed, is unchanged and may contain it
}

// The recorder wrote a skill's scene when the skill was loaded. A skill loaded
// for a turn (a trigger match) therefore comes before that turn's scene; one
// the author loaded between turns comes between them.
func TestRenderFountain_SkillScenesKeepTheRecordersOrder(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.Start.User = "ALICE" })
	body, _ := l.ContentBlob("Do the review.\n")
	mustAppend(t, l, KindInjection, LevelInfo, map[string]any{"source": "skill", "origin": "user", "ref": "first", "text": body})
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "review please"})
	mustAppend(t, l, KindInjection, LevelInfo, map[string]any{"source": "skill", "origin": "auto", "turn": 1, "ref": "second", "text": body})
	req := mustAppend(t, l, KindModelRequest, LevelInfo, map[string]any{"form": "assembled", "backend": "ollama", "model": "m"})
	mustAppend(t, l, KindModelResponse, LevelInfo, map[string]any{"request": req, "status": "ok", "text": "done"})
	_ = l.Close("quit")
	events, _, _ := ReadSessionLog(logPath(opts))
	got := string(RenderFountain(events, RenderOptions{Zone: time.UTC, Blobs: BlobDir(logPath(opts))}))
	first := strings.Index(got, "INT. SKILL FIRST")
	second := strings.Index(got, "INT. SKILL SECOND")
	turn := strings.Index(got, "INT. HARVEY AND ALICE TALKING")
	if !(first >= 0 && first < second && second < turn) {
		t.Errorf("want skill FIRST < skill SECOND < the turn, got %d %d %d:\n%s", first, second, turn, got)
	}
	if strings.Count(got, "Do the review.") != 2 {
		t.Errorf("each skill scene should carry the body once:\n%s", got)
	}
}
