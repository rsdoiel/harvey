package harvey

import (
	"strings"
	"testing"
)

// A4 of assay-profile-plan.md: the exports assay uses to probe tool calling
// with the definitions Harvey really sends.

func TestProfileToolSet_IsTheHailoReadOnlySubsetWithHarveysSchemas(t *testing.T) {
	tools := ProfileToolSet()
	var names []string
	for _, tl := range tools {
		names = append(names, tl.Function.Name)
		if tl.Type != "function" || tl.Function.Description == "" || tl.Function.Parameters == nil {
			t.Errorf("%s: incomplete definition %+v", tl.Function.Name, tl)
		}
	}
	want := []string{"list_files", "read_file", "search_files"} // the registry sorts by name
	if len(names) != len(want) {
		t.Fatalf("tools %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tools %v, want %v", names, want)
			break
		}
	}
}

func TestProseToolCallsIn_ReadsAJSONBlockInAReply(t *testing.T) {
	text := "I will read it.\n```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"main.go\"}}\n```\n"
	calls := ProseToolCallsIn(text)
	if len(calls) != 1 || calls[0].Function.Name != "read_file" {
		t.Fatalf("calls %+v, want one read_file", calls)
	}
	if got := ProseToolCallsIn("no call here"); len(got) != 0 {
		t.Errorf("plain text gave calls %+v", got)
	}
}

func TestEstimateTokens_Exported(t *testing.T) {
	if EstimateTokens("") != 1 || EstimateTokens(strings.Repeat("abcd", 50)) != 50 {
		t.Error("EstimateTokens is len/4 with a floor of 1")
	}
}
