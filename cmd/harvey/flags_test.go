package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	harvey "github.com/rsdoiel/harvey"
)

// H5 of exit-codes-plan.md: the enforcement test. The list of options comes
// from the code (flagSpecs) and from the manual (the OPTIONS section), the two
// are compared, and every option is then run with a bogus flag, without its
// value, and with a surplus argument. A new option that skips the manual, or
// one whose mistakes are not usage errors, fails here.

// manualOption is one option line of a manual: its spellings and whether the
// line shows a value placeholder.
type manualOption struct {
	names       []string
	placeholder bool
}

// manualOptions returns the options listed in the OPTIONS section of a
// manual page: every line that starts with "-".
func manualOptions(t *testing.T, manual string) []manualOption {
	t.Helper()
	i := strings.Index(manual, "\n# OPTIONS\n")
	if i < 0 {
		t.Fatal("the manual has no OPTIONS section")
	}
	section := manual[i+len("\n# OPTIONS\n"):]
	if j := strings.Index(section, "\n# "); j >= 0 {
		section = section[:j]
	}
	var opts []manualOption
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "-") {
			continue
		}
		var o manualOption
		for _, tok := range strings.Fields(line) {
			if strings.HasPrefix(tok, "-") {
				o.names = append(o.names, strings.TrimSuffix(tok, ","))
			} else {
				o.placeholder = true
			}
		}
		opts = append(opts, o)
	}
	return opts
}

// The options outside the table: they print something and exit, or (--json)
// are extracted before flagSpecs ever sees them.
var immediateOptions = []string{"-h", "--help", "-v", "--version", "-l", "--license", "--json"}

func TestFlags_TheManualAndTheCodeListTheSameOptions(t *testing.T) {
	fromManual := map[string]bool{}
	valueInManual := map[string]bool{}
	for _, o := range manualOptions(t, harvey.HelpText) {
		for _, n := range o.names {
			fromManual[n] = true
			valueInManual[n] = o.placeholder
		}
	}
	fromCode := map[string]bool{}
	for _, n := range immediateOptions {
		fromCode[n] = true
	}
	for _, f := range flagSpecs {
		for _, n := range f.names {
			fromCode[n] = true
			if fromManual[n] && valueInManual[n] != f.takesValue {
				t.Errorf("%s: takesValue is %v in the code and the manual shows a placeholder: %v", n, f.takesValue, valueInManual[n])
			}
		}
	}
	for n := range fromCode {
		if !fromManual[n] {
			t.Errorf("option %s is in the code but not in the OPTIONS section of the manual", n)
		}
	}
	for n := range fromManual {
		if !fromCode[n] {
			t.Errorf("option %s is in the manual but not in the code", n)
		}
	}
}

// Each option is checked with a valid value where it takes one, so the only
// mistake in the command line is the one under test.
func TestEveryOption_MistakesAreUsageErrors(t *testing.T) {
	for _, f := range flagSpecs {
		for _, name := range f.names {
			args := []string{name}
			if f.takesValue {
				args = append(args, "x")
			}
			cases := map[string][]string{
				"bogus flag before": append([]string{"--bogus-flag-zzz"}, args...),
				"bogus flag after":  append(append([]string{}, args...), "--bogus-flag-zzz"),
				"surplus argument":  append([]string{"surplus-zzz"}, args...),
			}
			if f.takesValue {
				cases["missing value"] = []string{name}
			}
			for what, a := range cases {
				code, out, errOut := run(t, a...)
				if code != 2 || out != "" {
					t.Errorf("harvey %v (%s): exit %d, stdout %q, stderr %.120q; want 2 and no stdout", a, what, code, out, errOut)
				}
			}
		}
	}
}

// Every documented help topic, and every alias the index lists, prints and
// exits 0. The index is the list of topics (two-space indent, name first) and
// then an "Aliases:" block of "a, b → topic" groups.
func TestHelpTopics_EveryListedTopicAndAliasExitsZero(t *testing.T) {
	_, index, _ := run(t, "help", "topics")
	var names []string
	inAliases := false
	for _, line := range strings.Split(index, "\n") {
		if strings.Contains(line, "Aliases:") {
			inAliases = true
			line = strings.Replace(line, "Aliases:", "        ", 1)
		}
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		if !inAliases {
			names = append(names, strings.Fields(line)[0])
			continue
		}
		// "audit, permissions → security    compact → summarize": groups end at "→ x".
		for _, group := range regexp.MustCompile(`([\w, -]+?)\s*→\s*[\w-]+`).FindAllStringSubmatch(line, -1) {
			for _, n := range strings.Split(group[1], ",") {
				if n = strings.TrimSpace(n); n != "" {
					names = append(names, n)
				}
			}
		}
	}
	sort.Strings(names)
	if len(names) < 40 {
		t.Fatalf("only %d topics and aliases found in the index; the parser has drifted from the format:\n%s", len(names), index)
	}
	for _, name := range names {
		if code, out, errOut := run(t, "help", name); code != 0 || out == "" {
			t.Errorf("harvey help %s: exit %d, %d bytes, stderr %.100q", name, code, len(out), errOut)
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

// The EXIT STATUS section documents exactly the codes harvey can return, and
// each of them is a row of the workspace table.
func TestManual_ExitStatusDocumentsTheCodesHarveyReturns(t *testing.T) {
	want := []int{0, 2, 65, 66, 69, 70, 73, 74, 75, 77, 78}
	got := documentedCodes(t, harvey.HelpText)
	if !sameInts(got, want) {
		t.Errorf("EXIT STATUS documents %v, want %v", got, want)
	}
	for _, c := range got {
		if !isWorkspaceCode(c) {
			t.Errorf("documented code %d is not in the workspace table", c)
		}
	}
}
