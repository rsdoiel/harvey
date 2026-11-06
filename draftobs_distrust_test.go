package harvey

import (
	"strings"
	"testing"
)

// D7, second real session (2026-11-02): granite4, qwen2.5:3b and llama3.2:3b all
// drafted an invented description of a project as a note, finding or decision,
// although the same excerpt held the tool output that said otherwise. The
// instructions already said to trust commands over the assistant; small models
// need the two cases stated: a claim nothing backs is not kept, and a claim a
// tool result contradicts is kept as the contradiction.

func TestDraftInstructions_sayWhatToDoWithClaimsNothingBacks(t *testing.T) {
	msgs := BuildDraftMessages(DraftPass{First: "s#1", Last: "s#2"}, "harvey")
	sys := msgs[0].Content
	for _, want := range []string{
		"only the assistant", // the unbacked claim
		"do not write it",    // and what not to do with it
		"did not match",      // the contradiction is itself the finding
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("the drafting instructions lack %q:\n%s", want, sys)
		}
	}
}
