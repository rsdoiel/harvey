package harvey

import (
	"bufio"
	"strings"
	"testing"
)

// askYesNo is used for prompts whose default is yes, some of which start a
// server or run a script. End of input (a closed pipe, Ctrl-D, /dev/null) is
// not an answer, so it must never count as the default.
func TestAskYesNo(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      string
		defaultYes bool
		want       bool
	}{
		{"enter takes the yes default", "\n", true, true},
		{"enter takes the no default", "\n", false, false},
		{"y", "y\n", false, true},
		{"yes, any case", "YES\n", false, true},
		{"n", "n\n", true, false},
		{"anything else is no", "maybe\n", true, false},
		{"y with no newline before end of input", "y", false, true},
		{"n with no newline before end of input", "n", true, false},
		{"end of input is not the yes default", "", true, false},
		{"end of input is no when the default is no", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			got := askYesNo(bufio.NewReader(strings.NewReader(tc.input)), &out, "? ", tc.defaultYes)
			if got != tc.want {
				t.Errorf("askYesNo(%q, default %v) = %v, want %v", tc.input, tc.defaultYes, got, tc.want)
			}
		})
	}
}
