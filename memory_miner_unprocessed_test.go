package harvey

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptedMinerClient replies from a script and counts its calls. A reply that
// is an error is returned as the Chat error.
type scriptedMinerClient struct {
	name    string
	replies []any // string or error
	calls   int
}

func (c *scriptedMinerClient) Name() string                               { return c.name }
func (c *scriptedMinerClient) Models(_ context.Context) ([]string, error) { return nil, nil }
func (c *scriptedMinerClient) Close() error                               { return nil }
func (c *scriptedMinerClient) Chat(_ context.Context, _ []Message, out io.Writer) (ChatStats, error) {
	i := c.calls
	c.calls++
	if i >= len(c.replies) {
		i = len(c.replies) - 1
	}
	switch r := c.replies[i].(type) {
	case error:
		return ChatStats{}, r
	case string:
		_, _ = io.WriteString(out, r)
	}
	return ChatStats{}, nil
}

// emptySessionText is what Harvey records for a session with no dialogue.
const emptySessionText = `Title: Harvey Session
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-10-07 12:45:51
Draft date: 2026-10-07
Model: QWEN2.5-CODER (ollama)
Backend: ollama


FADE IN:

THE END.
`

const talkingSessionText = `Title: Harvey Session
Author: RSDOIEL
Date: 2026-10-08 09:00:00

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-10-08 09:00:00

RSDOIEL
Always run go test from inside harvey/.

HARVEY
Noted.

THE END.
`

// minerFixture builds a store, manifest and miner in a temp workspace and
// writes text as a session file.
func minerFixture(t *testing.T, text string) (*Miner, *MemoryStore, *Manifest, string) {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMemoryStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manifest, err := LoadManifest(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	sess := filepath.Join(t.TempDir(), "session.spmd")
	if err := os.WriteFile(sess, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewMiner(store, manifest, ws), store, manifest, sess
}

func TestSessionIsEmpty(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"recorded empty session", emptySessionText, true},
		{"empty string", "", true},
		{"whitespace only", "\n\n  \n", true},
		{"scene heading but no dialogue", emptySessionText + "\nINT. HARVEY AND RSDOIEL TALKING 2026-10-08 09:00:00\n", true},
		{"model switch note only", emptySessionText + "\n[[model switch: phi (llamafile) at 2026-10-08 09:00:00]]\n", true},
		{"dialogue", talkingSessionText, false},
		{"plain text", "a short session", false},
	}
	for _, c := range cases {
		if got := sessionIsEmpty(c.text); got != c.want {
			t.Errorf("%s: sessionIsEmpty = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMine_EmptySession_SkipsLLMAndRecordsEmpty(t *testing.T) {
	miner, store, manifest, sess := minerFixture(t, emptySessionText)
	client := &scriptedMinerClient{name: "m1", replies: []any{"[]"}}
	var out bytes.Buffer

	if err := miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, &out, strings.NewReader("")); err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if client.calls != 0 {
		t.Errorf("model was called %d time(s) for an empty session", client.calls)
	}
	if !strings.Contains(out.String(), "empty") {
		t.Errorf("output should say the session is empty, got %q", out.String())
	}
	if !manifest.IsMined(sess) {
		t.Error("empty session should be recorded so it stops counting as unmined")
	}
	reloaded, err := LoadManifest(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IsMined(sess) {
		t.Error("manifest was not saved to disk")
	}
	if got := reloaded.Sessions[len(reloaded.Sessions)-1].Status; got != ManifestStatusEmpty {
		t.Errorf("Status = %q, want %q", got, ManifestStatusEmpty)
	}
}

func TestMineAuto_EmptySession_SkipsLLMAndRecordsEmpty(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, emptySessionText)
	client := &scriptedMinerClient{name: "m1", replies: []any{"[]"}}

	if err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}
	if client.calls != 0 {
		t.Errorf("model was called %d time(s) for an empty session", client.calls)
	}
	if !manifest.IsMined(sess) {
		t.Error("empty session should be recorded")
	}
}

func TestMine_TruncatedReply_NotMinedAndReported(t *testing.T) {
	miner, store, manifest, sess := minerFixture(t, talkingSessionText)
	client := &scriptedMinerClient{name: "m1", replies: []any{fmt.Errorf("chat: %w", ErrStreamTruncated)}}
	var out bytes.Buffer

	err := miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, &out, strings.NewReader(""))
	if err == nil {
		t.Fatal("Mine should report that the session could not be processed")
	}
	if !strings.Contains(err.Error(), "could not be processed") || !strings.Contains(err.Error(), "m1") {
		t.Errorf("error should name the model and say it could not process the session: %v", err)
	}
	if ExitCodeFor(err) != ClassNegative {
		t.Errorf("exit class = %v, want negative (the answer is no)", ExitCodeFor(err))
	}
	if manifest.IsMined(sess) {
		t.Error("a truncated mining must not count as mined")
	}
	reloaded, err := LoadManifest(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.IsMined(sess) {
		t.Error("a truncated mining must not be mined on disk")
	}
	if !reloaded.FailedOn(sess, "m1") {
		t.Error("the failure should be remembered for this session and model")
	}
	if reloaded.FailedOn(sess, "other") {
		t.Error("a different model has not failed on this session")
	}
}

func TestMine_UnparseableReplyTwice_NotMinedAndReported(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, talkingSessionText)
	client := &scriptedMinerClient{name: "m1", replies: []any{"I cannot do that.", "Still prose."}}

	err := miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, io.Discard, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "could not be processed") {
		t.Fatalf("want a could-not-be-processed error, got %v", err)
	}
	if manifest.IsMined(sess) || !manifest.FailedOn(sess, "m1") {
		t.Error("unparseable replies are a failure of that model, not a mined session")
	}
}

func TestMine_ServerDown_IsNotRememberedAsModelFailure(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, talkingSessionText)
	client := &scriptedMinerClient{name: "m1", replies: []any{fmt.Errorf("dial tcp: connection refused")}}

	if err := miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, io.Discard, strings.NewReader("")); err == nil {
		t.Fatal("want an error")
	}
	if manifest.IsMined(sess) || manifest.FailedOn(sess, "m1") {
		t.Error("an unreachable server says nothing about the model; retry later must be allowed")
	}
}

func TestMine_EmptyProposalList_StillRecordedAsMined(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, talkingSessionText)
	client := &scriptedMinerClient{name: "m1", replies: []any{"[]"}}
	var out bytes.Buffer

	if err := miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, &out, strings.NewReader("")); err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if !manifest.IsMined(sess) {
		t.Error("a model that answers [] has processed the session")
	}
	if strings.Contains(out.String(), "empty") {
		t.Errorf("a session with dialogue must not be called empty: %q", out.String())
	}
}

func TestMineAuto_SkipsSessionThatFailedOnThisModel(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, talkingSessionText)
	manifest.RecordFailure(sess, "m1", "reply truncated")
	client := &scriptedMinerClient{name: "m1", replies: []any{"[]"}}

	if err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}
	if client.calls != 0 {
		t.Errorf("auto-mine retried a session that already failed on this model (%d calls)", client.calls)
	}
	if manifest.IsMined(sess) {
		t.Error("skipping a known failure must not mark the session mined")
	}
}

func TestMineAuto_TriesSessionThatFailedOnAnotherModel(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, talkingSessionText)
	manifest.RecordFailure(sess, "m1", "reply truncated")
	client := &scriptedMinerClient{name: "m2", replies: []any{"[]"}}

	if err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard); err != nil {
		t.Fatalf("MineAuto: %v", err)
	}
	if client.calls != 1 || !manifest.IsMined(sess) {
		t.Errorf("a different model should get its turn: calls=%d mined=%v", client.calls, manifest.IsMined(sess))
	}
}

func TestMineAuto_TruncatedReply_NotMinedAndRemembered(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, talkingSessionText)
	client := &scriptedMinerClient{name: "m1", replies: []any{ErrStreamTruncated}}

	err := miner.MineAuto(context.Background(), sess, &Agent{Client: client}, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not be processed") {
		t.Fatalf("auto-mine must report the failure, got %v", err)
	}
	if manifest.IsMined(sess) || !manifest.FailedOn(sess, "m1") {
		t.Error("auto-mine: truncated reply is not mined and is remembered")
	}
}

func TestMine_ExplicitRunRetriesAfterPriorFailure(t *testing.T) {
	miner, _, manifest, sess := minerFixture(t, talkingSessionText)
	manifest.RecordFailure(sess, "m1", "reply truncated")
	client := &scriptedMinerClient{name: "m1", replies: []any{"[]"}}

	if err := miner.Mine(context.Background(), sess, &Agent{Client: client}, nil, io.Discard, strings.NewReader("")); err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if client.calls != 1 {
		t.Errorf("an explicit /memory mine should try again, calls=%d", client.calls)
	}
	if manifest.FailedOn(sess, "m1") {
		t.Error("a successful mining clears the recorded failure")
	}
}

func TestManifest_StatusAndFailuresRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{Sessions: []ManifestEntry{}}
	m.RecordEmpty("a.spmd")
	m.RecordFailure("b.spmd", "hailo/qwen", "reply truncated")
	m.RecordFailure("b.spmd", "hailo/qwen", "reply truncated again") // same pair: one entry
	if err := m.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsMined("a.spmd") || got.Sessions[0].Status != ManifestStatusEmpty {
		t.Errorf("empty entry lost: %+v", got.Sessions)
	}
	if got.IsMined("b.spmd") || !got.FailedOn("b.spmd", "hailo/qwen") {
		t.Error("failure entry lost or counted as mined")
	}
	if len(got.Failures) != 1 {
		t.Errorf("repeated failure on the same session and model should be one entry, got %d", len(got.Failures))
	}
}

func TestManifest_OldFileWithoutNewFieldsStillLoads(t *testing.T) {
	dir := t.TempDir()
	old := "sessions:\n    - path: x.spmd\n      mined_at: \"2026-10-07T19:46:21Z\"\n      memories_created: []\n      memories_skipped: 0\n"
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsMined("x.spmd") || m.Sessions[0].Status != "" {
		t.Errorf("old entry should load as a reviewed session: %+v", m.Sessions)
	}
}
