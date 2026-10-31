package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// crashedLog writes a session log in dir that was never closed.
func crashedLog(t *testing.T, dir, session string) string {
	t.Helper()
	opts := testLogOpts(t)
	opts.Dir, opts.Session = dir, session
	l, err := OpenSessionLog(opts)
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "hello before the crash"})
	return filepath.Join(dir, session+".jsonl")
}

func cleanLog(t *testing.T, dir, session string) string {
	t.Helper()
	opts := testLogOpts(t)
	opts.Dir, opts.Session = dir, session
	l, err := OpenSessionLog(opts)
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "a finished session"})
	_ = l.Close("quit")
	return filepath.Join(dir, session+".jsonl")
}

func TestRenderSessionFile_WritesTheRenderingBesideTheLogAtomically(t *testing.T) {
	dir := t.TempDir()
	lp := cleanLog(t, dir, "s1")
	out := filepath.Join(dir, "s1.spmd")
	if err := RenderSessionFile(lp, out, time.UTC); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	events, _, _ := ReadSessionLog(lp)
	if string(got) != string(RenderFountain(events, RenderOptions{Zone: time.UTC, Blobs: BlobDir(lp)})) {
		t.Error("the file is not the rendering of the log")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".render-*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestRenderSessionFile_ABadLogLeavesTheExistingFileAlone(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "s.spmd")
	os.WriteFile(out, []byte("the live recording"), 0o644)
	bad := filepath.Join(dir, "s.jsonl")
	os.WriteFile(bad, []byte("{not json}\n{also not}\n"), 0o644)
	if err := RenderSessionFile(bad, out, time.UTC); err == nil {
		t.Fatal("want an error for an unreadable log")
	}
	if b, _ := os.ReadFile(out); string(b) != "the live recording" {
		t.Errorf("the existing file was changed: %q", b)
	}
}

func TestRecoverSessionFiles_OnlyACrashedSessionIsRendered(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	crashedNoFile := crashedLog(t, dir, "crashed-nofile")
	crashedOld := crashedLog(t, dir, "crashed-old")
	crashedNew := crashedLog(t, dir, "crashed-new")
	cleanNoFile := cleanLog(t, dir, "clean-nofile")
	cleanWithFile := cleanLog(t, dir, "clean-withfile")
	// A partial live recording older than its log is replaced; a newer one is not.
	os.WriteFile(filepath.Join(dir, "crashed-old.spmd"), []byte("partial live"), 0o644)
	os.Chtimes(filepath.Join(dir, "crashed-old.spmd"), now.Add(-time.Hour), now.Add(-time.Hour))
	os.WriteFile(filepath.Join(dir, "crashed-new.spmd"), []byte("already recovered"), 0o644)
	os.Chtimes(crashedNew, now.Add(-time.Hour), now.Add(-time.Hour))
	os.WriteFile(filepath.Join(dir, "clean-withfile.spmd"), []byte("rendered at close"), 0o644)
	_, _, _, _ = crashedNoFile, cleanNoFile, cleanWithFile, crashedOld

	got, err := RecoverSessionFiles(dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "crashed-nofile,crashed-old" {
		t.Errorf("recovered %v, want crashed-nofile and crashed-old", got)
	}
	read := func(n string) string { b, _ := os.ReadFile(filepath.Join(dir, n+".spmd")); return string(b) }
	if !strings.Contains(read("crashed-nofile"), "hello before the crash") || !strings.Contains(read("crashed-nofile"), "[[session ended without a clean close]]") {
		t.Errorf("crashed-nofile not rendered:\n%s", read("crashed-nofile"))
	}
	if strings.Contains(read("crashed-old"), "partial live") {
		t.Error("an older partial recording should have been replaced")
	}
	if read("crashed-new") != "already recovered" {
		t.Error("a recording newer than its log must be left alone")
	}
	if read("clean-withfile") != "rendered at close" {
		t.Error("a finished session's file must be left alone")
	}
	if _, err := os.Stat(filepath.Join(dir, "clean-nofile.spmd")); err == nil {
		t.Error("a finished session with no .spmd (renamed, perhaps) must not get one made")
	}
	again, _ := RecoverSessionFiles(dir, time.UTC)
	if len(again) != 0 {
		t.Errorf("a second pass recovered %v; recovery should be done once", again)
	}
}

// The file on disk after a session is the rendering of its log: here that shows
// as the tool note the live recorder loses.
func TestRun_TheFountainFileIsRenderedFromTheLogAtClose(t *testing.T) {
	scriptedRender = true
	t.Cleanup(func() { scriptedRender = false })
	dir := scriptedRunWith(t, false, func(root string) {
		os.WriteFile(filepath.Join(root, "note.txt"), []byte("the note says hello"), 0o644)
	}, true, "read note.txt", "/exit")
	events, rec, lp := logAndFountain(t, dir)
	if !strings.Contains(rec, "[[tool: read_file(") {
		t.Errorf("the .spmd on disk should be the rendering, which has the tool note:\n%s", rec)
	}
	want := string(RenderFountain(events, RenderOptions{Zone: time.Local, Blobs: BlobDir(lp)}))
	if normaliseFountain(rec) != normaliseFountain(want) {
		t.Errorf("the .spmd differs from the rendering of the log\n--- file ---\n%s\n--- rendering ---\n%s", rec, want)
	}
}

// A session that crashed is recovered the next time harvey starts in that workspace.
func TestRun_ACrashedSessionIsRecoveredAtStartup(t *testing.T) {
	scriptedRender = true
	t.Cleanup(func() { scriptedRender = false })
	var sessions string
	_ = scriptedRunFull(t, false, func(root string) {
		sessions = filepath.Join(root, "agents", "sessions")
		os.MkdirAll(sessions, 0o755)
		crashedLog(t, sessions, "old-crash")
	}, false, nil, "/exit")
	b, err := os.ReadFile(filepath.Join(sessions, "old-crash.spmd"))
	if err != nil || !strings.Contains(string(b), "hello before the crash") {
		t.Errorf("the crashed session should have been rendered at startup: %v\n%s", err, b)
	}
}
