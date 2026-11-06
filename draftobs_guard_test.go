package harvey

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// D0 of the observation-drafting plan (harvey DR-0037 item 3): harvey drafts
// observations and never makes one trusted. Two package-wide guards, the same
// device as TestPromptAdditionsGoThroughTheLog.

func nonTestGoFiles(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var code []string
		for _, line := range strings.Split(string(raw), "\n") {
			l := strings.TrimSpace(line)
			if strings.HasPrefix(l, "//") || strings.HasPrefix(l, "*") || strings.HasPrefix(l, "/*") {
				continue
			}
			code = append(code, line)
		}
		out[f] = strings.Join(code, "\n")
	}
	return out
}

// Nothing in harvey promotes an observation: not through the library and not
// through the command line. kb refuses without a terminal anyway; this keeps
// harvey from ever asking.
func TestDraftObs_HarveyNeverPromotesAnObservation(t *testing.T) {
	promoteArg := regexp.MustCompile(`"promote"`)
	for f, code := range nonTestGoFiles(t) {
		if strings.Contains(code, "PromoteObservation") {
			t.Errorf("%s calls PromoteObservation: harvey drafts, the author promotes", f)
		}
		if promoteArg.MatchString(code) {
			t.Errorf("%s builds a \"promote\" argument: harvey must never run kb review promote", f)
		}
	}
}

// The only code that adds an observation is draftobs_write.go, and it adds
// drafts: --draft and --by on every call. The /kb passthrough builds its
// arguments from the author's own line, so it never names the verb.
func TestDraftObs_OnlyDraftobsWriteAddsObservationsAndOnlyAsDrafts(t *testing.T) {
	add := regexp.MustCompile(`"observation",\s*"add"`)
	for f, code := range nonTestGoFiles(t) {
		if f == "draftobs_write.go" {
			continue
		}
		if add.MatchString(code) {
			t.Errorf("%s adds an observation: only draftobs_write.go may, and only as a draft", f)
		}
	}
	if code, ok := nonTestGoFiles(t)["draftobs_write.go"]; ok {
		for _, want := range []string{`"--draft"`, `"--by"`} {
			if !strings.Contains(code, want) {
				t.Errorf("draftobs_write.go adds an observation without %s", want)
			}
		}
	}
}
