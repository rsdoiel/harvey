package harvey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const twoBlockReply = "Here are two files.\n\n```text:out/a.txt\nalpha\n```\n\n```text:out/b.txt\nbeta\n```\n"

// offerSession runs a scripted session in which the model replies with two
// tagged code blocks and the author answers the write offers with answers.
// It returns the sessions directory and the workspace root.
func offerSession(t *testing.T, answers ...string) (dir, root string) {
	t.Helper()
	scriptedReply, scriptedTerminal = twoBlockReply, true
	t.Cleanup(func() { scriptedReply, scriptedTerminal = "", false })
	// An interactive session first asks, for each fenced block, for a path to
	// write it to (Enter skips); the tagged-block offers of the agent scene follow.
	lines := append([]string{"make two files", "", ""}, answers...)
	lines = append(lines, "/exit")
	dir = scriptedRunWith(t, false, func(r string) { root = r }, false, lines...)
	return dir, root
}

func logAndFountain(t *testing.T, dir string) (events []Event, rec string, logPath string) {
	t.Helper()
	logs, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	spmd, _ := filepath.Glob(filepath.Join(dir, "*.spmd"))
	if len(logs) != 1 || len(spmd) != 1 {
		t.Fatalf("want one log and one .spmd in %s", dir)
	}
	ev, torn, err := ReadSessionLog(logs[0])
	if err != nil || torn {
		t.Fatalf("ReadSessionLog: %v %v", torn, err)
	}
	b, _ := os.ReadFile(spmd[0])
	return ev, string(b), logs[0]
}

func TestWriteOffers_TheLogRecordsEachAnswerItsOutcomeAndWhichOffer(t *testing.T) {
	dir, root := offerSession(t, "y", "n")
	events, _, _ := logAndFountain(t, dir)
	if b, err := os.ReadFile(filepath.Join(root, "out", "a.txt")); err != nil || !strings.Contains(string(b), "alpha") {
		t.Fatalf("the approved file should have been written: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "b.txt")); err == nil {
		t.Error("the declined file should not exist")
	}
	type ans struct {
		Target   Content `json:"target"`
		Decision string  `json:"decision"`
		Scope    string  `json:"scope"`
		Choice   string  `json:"choice"`
		Outcome  string  `json:"outcome"`
		Offer    *struct {
			Index int `json:"index"`
			Of    int `json:"of"`
		} `json:"offer"`
	}
	var got []ans
	for _, e := range events {
		if e.Kind != KindPermission {
			continue
		}
		var a ans
		_ = json.Unmarshal(e.Data, &a)
		if a.Choice != "" {
			got = append(got, a)
		}
	}
	if len(got) != 2 {
		t.Fatalf("%d answers in the log, want 2: %+v", len(got), got)
	}
	if got[0].Target.Text != "out/a.txt" || got[0].Choice != "yes" || got[0].Decision != "allow" || got[0].Outcome != "ok" ||
		got[0].Offer == nil || got[0].Offer.Index != 1 || got[0].Offer.Of != 2 {
		t.Errorf("first answer = %+v", got[0])
	}
	if got[1].Target.Text != "out/b.txt" || got[1].Choice != "no" || got[1].Decision != "deny" || got[1].Outcome != "skipped" ||
		got[1].Offer == nil || got[1].Offer.Index != 2 {
		t.Errorf("second answer = %+v", got[1])
	}
}

func TestRenderFountain_WriteOffersMatchTheRecorder(t *testing.T) {
	for name, answers := range map[string][]string{
		"yes then no":       {"y", "n"},
		"all":               {"a"}, // the second file is written without being asked, and the recorder says "yes" for it
		"quit at the first": {"q"},
		"no then yes":       {"n", "y"},
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := offerSession(t, answers...)
			events, rec, lp := logAndFountain(t, dir)
			got := string(RenderFountain(events, RenderOptions{Zone: time.Local, Blobs: BlobDir(lp)}))
			if a, b := normaliseFountain(rec), normaliseFountain(got); a != b {
				t.Errorf("rendering differs from the recorder's\n--- recorder ---\n%s\n--- renderer ---\n%s", a, b)
			}
			if !strings.Contains(got, "INT. AGENT MODE") || !strings.Contains(got, "Harvey proposes to write 2 file(s).") {
				t.Errorf("no agent scene in the rendering:\n%s", got)
			}
			goldenCheck(t, "offers_"+strings.ReplaceAll(name, " ", "_"), got)
		})
	}
}

// A refusal by the permissions table is one event, not two: the rule's denial
// and the author-facing record of it are the same fact.
func TestPermission_ARuleDenialInsideAnOfferIsOneEvent(t *testing.T) {
	a, path := permAgent(t)
	a.offerIndex, a.offerOf = 1, 2
	a.AuditBuffer.Log(ActionFileWrite, "agents/harvey.yaml", StatusDenied)
	a.logAction("write", "agents/harvey.yaml", actionNo, "denied")
	a.offerIndex, a.offerOf = 0, 0
	evs := eventsOf(t, a, path, KindPermission)
	if len(evs) != 1 {
		t.Fatalf("%d permission events for one refusal, want 1: %v", len(evs), evs)
	}
	var d struct {
		Decision, Scope, Choice, Outcome string
	}
	_ = json.Unmarshal(evs[0].Data, &d)
	if d.Decision != "deny" || d.Scope != "rule" || d.Choice != "no" || d.Outcome != "denied" {
		t.Errorf("event = %s", evs[0].Data)
	}
}
