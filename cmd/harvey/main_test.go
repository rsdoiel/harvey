package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harvey "github.com/rsdoiel/harvey"
)

// H2 of exit-codes-plan.md: main is split into mainRun so the command line can
// be tested in process. Only usage errors change their exit status here (2);
// everything else still exits as before until H3.

func run(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = mainRun(append([]string{"harvey"}, args...), &o, &e)
	return code, o.String(), e.String()
}

func TestMainRun_VersionHelpAndLicenseExitZero(t *testing.T) {
	for _, args := range [][]string{
		{"--version"}, {"-v"}, {"--help"}, {"-h"}, {"-help"}, {"help"},
		{"help", "topics"}, {"--help", "topics"}, {"help", "model"}, {"--license"}, {"-l"},
	} {
		code, out, errOut := run(t, args...)
		if code != 0 || out == "" || errOut != "" {
			t.Errorf("harvey %v: exit %d, stdout %d bytes, stderr %q; want 0, some output, none",
				args, code, len(out), errOut)
		}
	}
	if _, out, _ := run(t, "--version"); !strings.Contains(out, harvey.Version) {
		t.Errorf("--version prints %q, want it to contain %q", out, harvey.Version)
	}
}

func TestMainRun_UsageErrorsExitTwoWithNothingOnStdout(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		msg  string
	}{
		{"unknown flag", []string{"--bogus"}, "Unknown flag: --bogus"},
		{"unknown short flag", []string{"-z"}, "Unknown flag: -z"},
		{"unknown flag after a valid one", []string{"-r", "--bogus"}, "Unknown flag: --bogus"},
		{"surplus positional argument", []string{"extra"}, "unexpected argument: extra"},
		{"surplus positional after a flag", []string{"-r", "extra"}, "unexpected argument: extra"},
		{"unknown help topic", []string{"help", "nosuchtopic"}, "Unknown help topic"},
		{"unknown help topic via --help", []string{"--help", "nosuchtopic"}, "Unknown help topic"},
		{"init without a source", []string{"init"}, "Usage: harvey init"},
	} {
		code, out, errOut := run(t, tc.args...)
		if code != 2 {
			t.Errorf("%s: harvey %v exit %d, want 2", tc.name, tc.args, code)
		}
		if out != "" {
			t.Errorf("%s: stdout %q, want nothing", tc.name, out)
		}
		if !strings.Contains(errOut, tc.msg) {
			t.Errorf("%s: stderr %q, want it to contain %q", tc.name, errOut, tc.msg)
		}
	}
}

// Every flag that takes a value is refused when the value is missing.
func TestMainRun_FlagWithoutItsArgumentIsUsage(t *testing.T) {
	for _, flag := range []string{
		"-m", "--model", "--ollama", "--llamafile", "--llamafile-url", "--llamafile-dir",
		"-w", "--workdir", "--record-file", "--continue", "--replay", "--replay-output",
	} {
		code, out, errOut := run(t, flag)
		if code != 2 || out != "" || !strings.Contains(errOut, flag+" requires an argument") {
			t.Errorf("harvey %s: exit %d, stdout %q, stderr %q; want 2, none, %q",
				flag, code, out, errOut, flag+" requires an argument")
		}
	}
}

// H3: inputs named on the command line are checked before the session starts,
// and each failure has its own class. None of these reaches Agent.Run.

// inWorkspace runs the test from a fresh directory with an empty HOME, so no
// real workspace, model directory or Ollama is involved.
func inWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HARVEY_LLAMAFILE_DIR", "")
	return dir
}

func TestMainRun_NamedInputsFailFastByClass(t *testing.T) {
	dir := inWorkspace(t)
	closed := []string{"--ollama", "http://127.0.0.1:1"}
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badYAML := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badYAML, []byte("model_aliases: [broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir() // a real directory the working directory is not inside

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"-w a directory that does not exist", []string{"-w", filepath.Join(dir, "nope")}, 66},
		{"-w a file", []string{"-w", file}, 66},
		{"-w a directory the cwd is not inside", []string{"-w", other}, 2},
		{"init a source that does not exist", []string{"init", filepath.Join(dir, "nope.yaml")}, 66},
		{"init a source that is malformed", []string{"init", badYAML}, 65},
		{"--continue a file that does not exist", append([]string{"--continue", filepath.Join(dir, "nope.spmd")}, closed...), 66},
		{"--replay a file that does not exist", append([]string{"--replay", filepath.Join(dir, "nope.spmd")}, closed...), 66},
		{"--record-file in a directory that cannot be made", append([]string{"--record-file", filepath.Join(file, "sub", "x.spmd")}, closed...), 73},
		{"--llamafile a path that does not exist", append([]string{"--llamafile", filepath.Join(dir, "nope.llamafile")}, closed...), 66},
		{"--llamafile a file that is not a llamafile", append([]string{"--llamafile", file}, closed...), 66},
	} {
		code, out, errOut := run(t, tc.args...)
		if code != tc.want {
			t.Errorf("%s: exit %d, want %d\nstdout: %.200s\nstderr: %.300s", tc.name, code, tc.want, out, errOut)
		}
	}
}

// The workspace-boundary check is a bad -w value (2); a working directory that
// does not exist is a missing input (66). The message says which.
func TestMainRun_WorkDirMessagesSayWhichProblem(t *testing.T) {
	dir := inWorkspace(t)
	_, _, errOut := run(t, "-w", filepath.Join(dir, "nope"))
	if !strings.Contains(errOut, "does not exist") {
		t.Errorf("a missing -w directory: stderr %q, want it to say the directory does not exist", errOut)
	}
	_, _, errOut = run(t, "-w", t.TempDir())
	if !strings.Contains(errOut, "not inside") {
		t.Errorf("a -w directory the cwd is outside: stderr %q, want the containment message", errOut)
	}
}
