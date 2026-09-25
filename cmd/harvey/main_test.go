package main

import (
	"bytes"
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
