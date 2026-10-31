package harvey

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testSkill(a *Agent) *SkillMeta {
	return &SkillMeta{
		Name:        "go-review",
		Description: "Review Go code for the usual problems",
		Body:        "Read the diff. List problems by severity.\n",
		Path:        filepath.Join(a.Workspace.Root, "agents", "skills", "go-review", "SKILL.md"),
	}
}

type skillInjection struct {
	Source      string  `json:"source"`
	Origin      string  `json:"origin"`
	Ref         string  `json:"ref"`
	Description string  `json:"description"`
	Text        Content `json:"text"`
	Turn        *int    `json:"turn"`
}

func skillInjections(t *testing.T, a *Agent, path string) []skillInjection {
	t.Helper()
	var out []skillInjection
	for _, e := range eventsOf(t, a, path, KindInjection) {
		var d skillInjection
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		if d.Source == "skill" {
			out = append(out, d)
		}
	}
	return out
}

func TestLoadSkill_IsAnInjectionByTheAuthor(t *testing.T) {
	a, path := agentWithLog(t)
	if err := loadSkill(a, testSkill(a), io.Discard); err != nil {
		t.Fatal(err)
	}
	got := skillInjections(t, a, path)
	if len(got) != 1 {
		t.Fatalf("%d skill injections, want 1", len(got))
	}
	g := got[0]
	if g.Origin != "user" || g.Ref != "go-review" || g.Description != "Review Go code for the usual problems" || g.Turn != nil {
		t.Errorf("injection = %+v, want origin user, ref go-review, its description, and no turn", g)
	}
	// A skill body is kept once, by hash, however often the skill is loaded.
	if g.Text.Blob == "" {
		t.Errorf("the skill body should be a blob: %+v", g.Text)
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(path), "20261030-1412-harvey.blobs", g.Text.Blob)); err != nil || string(b) != testSkill(a).Body {
		t.Errorf("blob = %q, %v", b, err)
	}
}

func TestLoadSkillAs_ATriggerMatchIsAutoAndBelongsToTheTurn(t *testing.T) {
	a, path := agentWithLog(t)
	a.logUserInput("please review this")
	if err := loadSkillAs(a, testSkill(a), io.Discard, "auto"); err != nil {
		t.Fatal(err)
	}
	got := skillInjections(t, a, path)
	if len(got) != 1 || got[0].Origin != "auto" || got[0].Turn == nil || *got[0].Turn != 1 {
		t.Errorf("injection = %+v, want origin auto in turn 1", got)
	}
}

func TestLoadSkill_AnEmptySkillIsNotLogged(t *testing.T) {
	a, path := agentWithLog(t)
	s := testSkill(a)
	s.Body = ""
	if err := loadSkill(a, s, io.Discard); err == nil {
		t.Fatal("want the no-body error")
	}
	if n := len(skillInjections(t, a, path)); n != 0 {
		t.Errorf("%d injections for a skill that was not loaded", n)
	}
}

func TestLoadSkill_NoLogIsNoProblem(t *testing.T) {
	a := newTestAgent(t)
	if err := loadSkill(a, testSkill(a), io.Discard); err != nil {
		t.Fatalf("loadSkill with no log: %v", err)
	}
}

// Through the real Run: the log has the injection, and the rendering of the log
// has the same skill scene the live recorder wrote.
func TestRun_ASkillLoadIsLoggedAndRendersLikeTheRecorder(t *testing.T) {
	dir := scriptedRunWith(t, false, func(root string) {
		d := filepath.Join(root, "agents", "skills", "demo")
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: demo\ndescription: A demonstration skill\n---\n\nDo the demonstration.\n"), 0o644)
	}, false, "/skill load demo", "hello", "/exit")
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	spmd, _ := filepath.Glob(filepath.Join(dir, "*.spmd"))
	events, _, err := ReadSessionLog(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	var inj *Event
	for i := range events {
		if events[i].Kind == KindInjection {
			var d skillInjection
			_ = json.Unmarshal(events[i].Data, &d)
			if d.Source == "skill" {
				inj = &events[i]
				if d.Ref != "demo" || d.Origin != "user" || d.Description != "A demonstration skill" {
					t.Errorf("injection = %s", events[i].Data)
				}
			}
		}
	}
	if inj == nil {
		t.Fatal("no skill injection in the log")
	}
	rec, _ := os.ReadFile(spmd[0])
	got := RenderFountain(events, RenderOptions{Zone: time.Local, Blobs: BlobDir(logs[0])})
	if a, b := normaliseFountain(string(rec)), normaliseFountain(string(got)); a != b {
		t.Errorf("rendering differs from the recorder's\n--- recorder ---\n%s\n--- renderer ---\n%s", a, b)
	}
	goldenCheck(t, "skill_load", string(got))
}
