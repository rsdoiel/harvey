package main

import (
	"flag"
	"io"
	"strconv"
	"strings"
	"testing"

	harvey "github.com/rsdoiel/harvey"
)

// H5 of exit-codes-plan.md, assay's half of the enforcement test: the options
// come from the flag set the program parses with and from the OPTIONS section
// of its manual, the two are compared, and each option is run with a bogus
// flag, without its value and with a surplus argument.

type assayOption struct {
	name    string // without its dashes
	isBool  bool
	isInt   bool
	dummyOK string // a valid value for a non-boolean option
}

func definedOptions() []assayOption {
	fs := flag.NewFlagSet("assay", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	defineAssayFlags(fs)
	var opts []assayOption
	fs.VisitAll(func(f *flag.Flag) {
		o := assayOption{name: f.Name, dummyOK: "x"}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			o.isBool = true
		}
		if f.Name == "rag-top-k" {
			o.isInt, o.dummyOK = true, "1"
		}
		opts = append(opts, o)
	})
	return opts
}

// manualOptions lists the options in a manual's OPTIONS section, by name
// without dashes, with whether the line shows a value placeholder.
func manualOptions(t *testing.T, manual string) map[string]bool {
	t.Helper()
	i := strings.Index(manual, "\n# OPTIONS\n")
	if i < 0 {
		t.Fatal("the manual has no OPTIONS section")
	}
	section := manual[i+len("\n# OPTIONS\n"):]
	if j := strings.Index(section, "\n# "); j >= 0 {
		section = section[:j]
	}
	out := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "-") {
			continue
		}
		placeholder := false
		var names []string
		for _, tok := range strings.Fields(line) {
			if strings.HasPrefix(tok, "-") {
				names = append(names, strings.TrimLeft(strings.TrimSuffix(tok, ","), "-"))
			} else {
				placeholder = true
			}
		}
		for _, n := range names {
			out[n] = placeholder
		}
	}
	return out
}

func TestFlags_TheManualAndTheCodeListTheSameOptions(t *testing.T) {
	manual := manualOptions(t, harvey.AssayHelpText)
	inCode := map[string]bool{"h": true, "help": true, "v": true, "version": true, "json": true}
	for _, o := range definedOptions() {
		inCode[o.name] = true
		placeholder, listed := manual[o.name]
		if !listed {
			t.Errorf("option -%s is in the code but not in the OPTIONS section of the manual", o.name)
		} else if placeholder == o.isBool {
			t.Errorf("option -%s: boolean is %v in the code and the manual shows a placeholder: %v", o.name, o.isBool, placeholder)
		}
	}
	for n := range manual {
		if !inCode[n] {
			t.Errorf("option -%s is in the manual but not in the code", n)
		}
	}
}

func TestEveryOption_MistakesAreUsageErrors(t *testing.T) {
	for _, o := range definedOptions() {
		args := []string{"--" + o.name}
		if !o.isBool {
			args = append(args, o.dummyOK)
		}
		cases := map[string][]string{
			"bogus flag before": append([]string{"--bogus-flag-zzz"}, args...),
			"bogus flag after":  append(append([]string{}, args...), "--bogus-flag-zzz"),
			"surplus argument":  append([]string{"surplus-zzz"}, args...),
		}
		if !o.isBool {
			cases["missing value"] = []string{"--" + o.name}
		}
		if o.isInt {
			cases["value that is not a number"] = []string{"--" + o.name, "many"}
		}
		for what, a := range cases {
			code, out, errOut := runAssay(t, a...)
			if code != 2 || out != "" {
				t.Errorf("assay %v (%s): exit %d, stdout %q, stderr %.120q; want 2 and no stdout", a, what, code, out, errOut)
			}
		}
	}
}

// documentedCodes returns the codes listed in a manual's EXIT STATUS section:
// the lines that are a bare number.
func documentedCodes(t *testing.T, manual string) []int {
	t.Helper()
	i := strings.Index(manual, "\n# EXIT STATUS\n")
	if i < 0 {
		t.Fatal("the manual has no EXIT STATUS section")
	}
	section := manual[i+len("\n# EXIT STATUS\n"):]
	if j := strings.Index(section, "\n# "); j >= 0 {
		section = section[:j]
	}
	var codes []int
	for _, line := range strings.Split(section, "\n") {
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && line == strings.TrimSpace(line) {
			codes = append(codes, n)
		}
	}
	return codes
}

func isWorkspaceCode(c int) bool {
	for _, k := range []harvey.ExitClass{harvey.ClassOK, harvey.ClassNegative, harvey.ClassUsage, harvey.ClassData,
		harvey.ClassNoInput, harvey.ClassUnavailable, harvey.ClassInternal, harvey.ClassCantCreate, harvey.ClassIO,
		harvey.ClassTempFail, harvey.ClassNoPermission, harvey.ClassConfig} {
		if k.Code == c {
			return true
		}
	}
	return false
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The EXIT STATUS section documents exactly the codes assay can return, and
// each of them is a row of the workspace table.
func TestManual_ExitStatusDocumentsTheCodesAssayReturns(t *testing.T) {
	want := []int{0, 1, 2, 65, 66, 69, 70, 73, 74, 77}
	got := documentedCodes(t, harvey.AssayHelpText)
	if !sameInts(got, want) {
		t.Errorf("EXIT STATUS documents %v, want %v", got, want)
	}
	for _, c := range got {
		if !isWorkspaceCode(c) {
			t.Errorf("documented code %d is not in the workspace table", c)
		}
	}
}
