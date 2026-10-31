package harvey

import (
	"fmt"
	"testing"
	"time"
)

// writeLegacyFountain writes a session file in the format harvey wrote before
// the session log existed: one chat scene per (user, reply) pair. Only the
// reader's fallback for old files needs it; harvey no longer writes this.
func writeLegacyFountain(t *testing.T, path, client, workspace string, pairs ...string) {
	t.Helper()
	rec, err := NewRecorder(path, client, workspace)
	if err != nil {
		t.Fatal(err)
	}
	out := fountainOut{rec.f}
	for i := 0; i+1 < len(pairs); i += 2 {
		out.sceneHeading(fmt.Sprintf("INT. HARVEY AND %s TALKING %s", rec.userName, time.Now().Format("2006-01-02 15:04:05")))
		out.action(fmt.Sprintf("Harvey and %s are in chat mode. Model: %s. Workspace: %s.", rec.userName, rec.modelName, workspace))
		out.dialogue(rec.userName, "", pairs[i])
		out.dialogue("HARVEY", "", fmt.Sprintf("Forwarding to %s.", rec.modelName))
		out.dialogue(rec.modelName, "", pairs[i+1])
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
}
