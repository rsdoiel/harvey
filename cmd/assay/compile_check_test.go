package main

import (
	"os"
	"strings"
	"testing"
)

// The compile check must build the model's code, not whatever module the
// process happens to run in. Run from a directory that is no module at all
// (as an overnight run from the workspace root was) valid code must pass
// and broken code must fail for its own reason.
func TestRunCompileCheck_BuildsTheAnswerWhereverAssayRuns(t *testing.T) {
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	good := "```go\npackage main\n\nfunc main() {}\n```"
	_, c, v := runCompileCheck(good)
	if !c.Passed {
		t.Errorf("valid code failed to compile: %s", c.Detail)
	}
	if !v.Passed {
		t.Errorf("valid code failed vet: %s", v.Detail)
	}

	bad := "```go\npackage main\n\nfunc main() { undefinedThing() }\n```"
	_, c, _ = runCompileCheck(bad)
	if c.Passed || !strings.Contains(c.Detail, "undefinedThing") {
		t.Errorf("broken code: passed=%v detail=%q", c.Passed, c.Detail)
	}
}

// From inside a module that does not compile, valid answers must still pass.
func TestRunCompileCheck_IgnoresTheSurroundingModule(t *testing.T) {
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	d := t.TempDir()
	os.WriteFile(d+"/go.mod", []byte("module other\n\ngo 1.21\n"), 0644)
	os.WriteFile(d+"/broken.go", []byte("package other\n\nfunc f() { nope() }\n"), 0644)
	if err := os.Chdir(d); err != nil {
		t.Fatal(err)
	}
	_, c, _ := runCompileCheck("```go\npackage main\n\nfunc main() {}\n```")
	if !c.Passed {
		t.Errorf("answer was judged against the surrounding module: %s", c.Detail)
	}
}
