package harvey

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseFountainSession_TwoExchangesInOneScene is a regression test for a
// silent data-loss bug: a scene with two complete user/model exchanges
// (built from the real structure at
// harvey/agents/sessions/harvey-session-20260502-mozilla-ai-integration.spmd
// lines 191-210) must yield two distinct turns. Before the fix, the second
// exchange silently overwrote the first in the shared PlaybackTurn struct,
// so only one (mixed-up) turn was ever returned.
func TestParseFountainSession_TwoExchangesInOneScene(t *testing.T) {
	content := `Title: Test Session
Author: RSDOIEL
Date: 2026-07-03

FADE IN:

INT. HARVEY AND RSDOIEL TALKING — 2026-07-03 19:15

Model: CLAUDE-SONNET-4-6

RSDOIEL
Let's do phase 2.

CLAUDE
Phase 2 scope confirmed.

RSDOIEL
I am going to hit token limits. We'll continue phase 2 later.

CLAUDE
Knowledge base initialized and populated.

THE END.
`
	path := filepath.Join(t.TempDir(), "two-exchanges.spmd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, _, turns, err := parseFountainSession(path)
	if err != nil {
		t.Fatalf("parseFountainSession: %v", err)
	}

	if len(turns) != 2 {
		t.Fatalf("got %d turns, want 2 (turns: %+v)", len(turns), turns)
	}

	if turns[0].UserInput != "Let's do phase 2." {
		t.Errorf("turns[0].UserInput = %q, want %q", turns[0].UserInput, "Let's do phase 2.")
	}
	if turns[0].ModelReply != "Phase 2 scope confirmed." {
		t.Errorf("turns[0].ModelReply = %q, want %q", turns[0].ModelReply, "Phase 2 scope confirmed.")
	}
	if turns[1].UserInput != "I am going to hit token limits. We'll continue phase 2 later." {
		t.Errorf("turns[1].UserInput = %q, want %q", turns[1].UserInput, "I am going to hit token limits. We'll continue phase 2 later.")
	}
	if turns[1].ModelReply != "Knowledge base initialized and populated." {
		t.Errorf("turns[1].ModelReply = %q, want %q", turns[1].ModelReply, "Knowledge base initialized and populated.")
	}
}

// TestParseFountainSession_SingleExchange guards the common case: one scene
// heading, one exchange, must still yield exactly one turn.
func TestParseFountainSession_SingleExchange(t *testing.T) {
	content := `Title: Test Session
Author: RSDOIEL
Date: 2026-07-03

FADE IN:

INT. HARVEY AND RSDOIEL TALKING — 2026-07-03 19:15

RSDOIEL
Hello there.

HARVEY
Forwarding to CLAUDE.

CLAUDE
General Kenobi.

THE END.
`
	path := filepath.Join(t.TempDir(), "single-exchange.spmd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, _, turns, err := parseFountainSession(path)
	if err != nil {
		t.Fatalf("parseFountainSession: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("got %d turns, want 1 (turns: %+v)", len(turns), turns)
	}
	if turns[0].UserInput != "Hello there." {
		t.Errorf("UserInput = %q, want %q", turns[0].UserInput, "Hello there.")
	}
	if turns[0].ModelReply != "General Kenobi." {
		t.Errorf("ModelReply = %q, want %q", turns[0].ModelReply, "General Kenobi.")
	}
}

// TestParseFountainSession_MultipleScenes guards the already-working case:
// separate scene headings per turn must still yield separate turns.
func TestParseFountainSession_MultipleScenes(t *testing.T) {
	content := `Title: Test Session
Author: RSDOIEL
Date: 2026-07-03

FADE IN:

INT. HARVEY AND RSDOIEL TALKING — 2026-07-03 19:00

RSDOIEL
First question.

CLAUDE
First answer.

INT. HARVEY AND RSDOIEL TALKING — 2026-07-03 19:05

RSDOIEL
Second question.

CLAUDE
Second answer.

THE END.
`
	path := filepath.Join(t.TempDir(), "multi-scene.spmd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, _, turns, err := parseFountainSession(path)
	if err != nil {
		t.Fatalf("parseFountainSession: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("got %d turns, want 2 (turns: %+v)", len(turns), turns)
	}
	if turns[0].UserInput != "First question." || turns[0].ModelReply != "First answer." {
		t.Errorf("turns[0] = %+v, want UserInput=%q ModelReply=%q", turns[0], "First question.", "First answer.")
	}
	if turns[1].UserInput != "Second question." || turns[1].ModelReply != "Second answer." {
		t.Errorf("turns[1] = %+v, want UserInput=%q ModelReply=%q", turns[1], "Second question.", "Second answer.")
	}
}
