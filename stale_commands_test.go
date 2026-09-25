package harvey

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// removedCommandRef matches a mention of a slash command that no longer
// exists: "/ollama" or "/llamafile" standing alone as a word, not a URL or
// path segment (those have a letter or slash before the slash).
var removedCommandRef = regexp.MustCompile("(^|[\\s(\"'`])/(ollama|llamafile)\\b")

// TestNoReferencesToRemovedCommands verifies that no Go source file (messages,
// help text, completion code, comments) names /ollama or /llamafile. Both
// were folded into /model; a message telling the user to run one sends them
// to "unknown command".
func TestNoReferencesToRemovedCommands(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	for _, name := range []string{"ollama", "llamafile"} {
		if _, ok := a.commands[name]; ok {
			t.Skipf("/%s is registered again; this test no longer applies", name)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if removedCommandRef.MatchString(line) {
				t.Errorf("%s:%d names a command that is not registered: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// TestCompletion_ModelAliasAddCompletesTheModelName verifies that the model
// argument of "/model alias add ALIAS MODEL" completes from known model names
// and aliases. That completion once hung off /ollama, which no longer exists.
func TestCompletion_ModelAliasAddCompletesTheModelName(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	a.Config.ModelAliases = map[string]ModelAlias{"qwen-coder": {}}
	completer := a.buildCompleter()

	for _, line := range []string{"/model alias add qc qw", "/model alias add qc "} {
		found := false
		for _, g := range completer(line) {
			if g == "qwen-coder" {
				found = true
			}
		}
		if !found {
			t.Errorf("completer(%q) did not offer %q", line, "qwen-coder")
		}
	}
	// The alias name itself (third argument) is free text, not a model.
	for _, g := range completer("/model alias add qw") {
		if g == "qwen-coder" {
			t.Errorf("completer offered a model name where the new alias is typed: %q", g)
		}
	}
}
