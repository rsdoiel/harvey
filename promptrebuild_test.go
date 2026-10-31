package harvey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkRebuild reads the session log in dir and fails for every turn request
// whose rebuilt prompt does not hash to the one the request stored.
func checkRebuild(t *testing.T, dir string) []RebuiltRequest {
	t.Helper()
	lp, _ := sessionPaths(t, dir)
	events, _, err := ReadSessionLog(lp)
	if err != nil {
		t.Fatal(err)
	}
	reqs := RebuildPrompts(events, RenderOptions{Blobs: BlobDir(lp)})
	if len(reqs) == 0 {
		t.Fatal("no turn requests in the log")
	}
	for _, r := range reqs {
		if r.Stored != r.Rebuilt {
			t.Errorf("request %s: the prompt rebuilt from the log (%d messages) hashes to %.12s, the request stored %.12s",
				r.RequestID, len(r.Messages), r.Rebuilt, r.Stored)
			if raw, err := json.Marshal(r.Messages); err == nil {
				t.Logf("  rebuilt: %.600s", raw)
			}
			for _, e := range events {
				if e.Level == LevelDebug && e.Kind == KindModelRequest && strings.Contains(string(e.Data), r.RequestID) {
					var d struct {
						Body json.RawMessage `json:"body"`
					}
					_ = json.Unmarshal(e.Data, &d)
					t.Logf("  sent:    %.600s", RenderOptions{Blobs: BlobDir(lp)}.contentOf(d.Body))
				}
			}
		}
	}
	return reqs
}

func TestRebuildPrompts_PlainChat(t *testing.T) {
	checkRebuild(t, scriptedRun(t, true, "hello", "again", "/exit"))
}

func writeNote(root string) {
	_ = os.WriteFile(filepath.Join(root, "note.txt"), []byte("the note says hello"), 0o644)
}

func TestRebuildPrompts_ToolLoop(t *testing.T) {
	checkRebuild(t, scriptedRunWith(t, true, writeNote, true, "read note.txt", "and again", "/exit"))
}

func TestRebuildPrompts_ReadCommandInjection(t *testing.T) {
	checkRebuild(t, scriptedRunWith(t, true, writeNote, false, "/read note.txt", "what does it say", "/exit"))
}

func TestRebuildPrompts_FileNamedInTheLine(t *testing.T) {
	checkRebuild(t, scriptedRunWith(t, true, writeNote, false, "summarise note.txt please", "/exit"))
}

func TestRebuildPrompts_ClearStartsOver(t *testing.T) {
	checkRebuild(t, scriptedRun(t, true, "hello", "/clear", "again", "/exit"))
}

func TestRebuildPrompts_ModelSwitch(t *testing.T) {
	checkRebuild(t, scriptedRun(t, true, "hello", "/model use qwen2.5-coder:1.5b", "again", "/exit"))
}

func TestRebuildPrompts_PinnedContextSurvivesClear(t *testing.T) {
	checkRebuild(t, scriptedRun(t, true, "/context add keep answers short", "hello", "/clear", "again", "/exit"))
}

func TestRebuildPrompts_ShellOutput(t *testing.T) {
	checkRebuild(t, scriptedRun(t, true, "!echo hi", "what was that", "/exit"))
}

func TestRebuildPrompts_ReadDir(t *testing.T) {
	checkRebuild(t, scriptedRunWith(t, true, writeNote, false, "/read-dir .", "what is there", "/exit"))
}

func TestRebuildPrompts_Summarize(t *testing.T) {
	checkRebuild(t, scriptedRun(t, true, "hello", "again", "/summarize", "and now", "/exit"))
}

func TestRebuildPrompts_AFailedTurnIsNotInTheNextPrompt(t *testing.T) {
	scriptedFailOn, scriptedTerminal = 2, true
	t.Cleanup(func() { scriptedFailOn, scriptedTerminal = 0, false })
	checkRebuild(t, scriptedRun(t, true, "hello", "this one fails", "third", "/exit"))
}

func TestRebuildPrompts_ResumedHistoryIsInTheLog(t *testing.T) {
	src := twoTurnSession(t)
	srcLog, _ := sessionPaths(t, src)
	dir := scriptedRunFull(t, true, nil, false, func(a *Agent) {
		a.Config.Session.ContinuePath = srcLog
	}, "next question", "/exit")
	lp := ""
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	for _, l := range logs {
		lp = l
	}
	if lp == "" {
		t.Fatal("no log")
	}
	reqs := checkRebuild(t, dir)
	if got := len(reqs[0].Messages); got < 5 {
		t.Errorf("the first request after a resume should carry the resumed turns, got %d messages", got)
	}
}

func TestRebuildPrompts_SkillLoad(t *testing.T) {
	setup := func(root string) {
		d := filepath.Join(root, "agents", "skills", "go-review")
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: go-review\ndescription: Review Go code\n---\nRead the diff. List problems by severity.\n"), 0o644)
	}
	reqs := checkRebuild(t, scriptedRunWith(t, true, setup, false, "/skill load go-review", "review it", "/exit"))
	if m := reqs[0].Messages; len(m) < 2 || !strings.Contains(m[0].Content, "[skill: go-review]") {
		t.Errorf("the first request should carry the loaded skill, got %+v", m)
	}
}

func TestRewriteOps_ApplyingThemGivesTheNewHistory(t *testing.T) {
	m := func(role, text string) Message { return Message{Role: role, Content: text} }
	sys := m("system", "S")
	a, b, c, d, e := m("user", "a"), m("assistant", "b"), m("user", "c"), m("assistant", "d"), m("user", "e")
	summary := m("user", "[Conversation summary] ...")
	cases := map[string]struct{ prev, next []Message }{
		"unchanged":        {[]Message{sys, a, b}, []Message{sys, a, b}},
		"trimmed head":     {[]Message{sys, a, b, c, d}, []Message{sys, c, d}},
		"emptied":          {[]Message{sys, a, b}, nil},
		"summary replaces": {[]Message{sys, a, b, c, d, e}, []Message{sys, summary, e}},
		"pop last":         {[]Message{sys, a, b, c}, []Message{sys, a, b}},
		"duplicates":       {[]Message{a, a, a, b}, []Message{a, b, a}},
		"system prepended": {[]Message{a, b}, []Message{sys, a, b}},
		"from empty":       {nil, []Message{sys, a}},
	}
	for name, tc := range cases {
		ops, changed := rewriteOps(tc.prev, tc.next)
		if !changed {
			if promptHash(tc.prev) != promptHash(tc.next) {
				t.Errorf("%s: reported unchanged for different lists", name)
			}
			continue
		}
		// Through JSON, as the log stores it.
		var raws []json.RawMessage
		for _, op := range ops {
			if msg, ok := op.(Message); ok {
				op = map[string]any{"role": msg.Role, "message": msg.Content}
			}
			b, err := json.Marshal(op)
			if err != nil {
				t.Fatal(err)
			}
			raws = append(raws, b)
		}
		got := applyRewriteOps(tc.prev, raws, RenderOptions{})
		if promptHash(got) != promptHash(tc.next) {
			t.Errorf("%s: applying the ops gives %v, want %v", name, got, tc.next)
		}
	}
}

// TestPromptAdditionsGoThroughTheLog is the guard of DR-0048: the package may
// change the history directly in only the places listed here, each of which
// logs what it does (addPrompt, logTurnMessage, logRewrite, the tool loop's
// events). A new direct write fails this test until it is logged and listed.
func TestPromptAdditionsGoThroughTheLog(t *testing.T) {
	allowed := map[string]int{
		// file -> number of lines that write the history directly
		"builtin_tools.go":    2, // filter_context, summary_context; both logRewrite
		"commands.go":         1, // /context clear; logRewrite
		"harvey.go":           4, // AddMessage, AddMessageParts, ClearHistory (2); the rest log through callers
		"memory_rolling.go":   1, // CompressHistory; logRewrite
		"prompt_fit.go":       2, // trim and retry; logRewrite
		"replay.go":           1, // the replayed turn; logTurnMessage
		"sessionlog_agent.go": 1, // dropLastMessage; addPromptParts goes through AddMessageParts
		"terminal.go":         5, // system prompt at start, the turn (logTurnMessage), the tool loop's history (events), skill catalog (system_text)
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, line := range strings.Split(string(raw), "\n") {
			l := strings.TrimSpace(line)
			if strings.HasPrefix(l, "//") || strings.HasPrefix(l, "*") {
				continue
			}
			if strings.Contains(l, "a.History = ") || strings.Contains(l, `AddMessage("user"`) || strings.Contains(l, `AddMessage("system"`) {
				n++
			}
		}
		if n != allowed[f] {
			t.Errorf("%s writes the history directly in %d place(s), the guard expects %d: log the change through addPrompt or logRewrite (DR-0048) and update the list", f, n, allowed[f])
		}
	}
}
