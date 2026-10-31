package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKBWithVersion puts a kb on PATH that reports version on -version and
// otherwise records its arguments in dir/calls and prints "ran".
func fakeKBWithVersion(t *testing.T, version string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := "#!/bin/sh\nd=$(dirname \"$0\")\n" +
		"if [ \"$1\" = \"-version\" ]; then echo 'kb " + version + " abc1234'; echo v >> \"$d/versions\"; exit 0; fi\n" +
		"echo \"$@\" >> \"$d/calls\"\necho ran\n"
	if err := os.WriteFile(filepath.Join(dir, "kb"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestParseKBVersion(t *testing.T) {
	for _, tc := range []struct {
		in      string
		a, b, c int
		wantErr bool
	}{
		{"kb 0.0.29 7d292d7\n", 0, 0, 29, false},
		{"kb v1.2.3", 1, 2, 3, false},
		{"kb 0.1.0-rc1 abc", 0, 1, 0, false},
		{"kb version 0.0.29", 0, 0, 0, true},
		{"", 0, 0, 0, true},
		{"something else entirely", 0, 0, 0, true},
	} {
		a, b, c, err := parseKBVersion(tc.in)
		if (err != nil) != tc.wantErr || (err == nil && (a != tc.a || b != tc.b || c != tc.c)) {
			t.Errorf("parseKBVersion(%q) = %d.%d.%d, %v", tc.in, a, b, c, err)
		}
	}
}

func TestKBVersionAtLeast(t *testing.T) {
	for _, tc := range []struct {
		have, min string
		ok        bool
	}{
		{"0.0.29", "0.0.29", true},
		{"0.0.30", "0.0.29", true},
		{"0.1.0", "0.0.29", true},
		{"1.0.0", "0.0.29", true},
		{"0.0.28", "0.0.29", false},
		{"0.0.9", "0.0.29", false},
	} {
		a, b, c, _ := parseKBVersion("kb " + tc.have)
		x, y, z, _ := parseKBVersion("kb " + tc.min)
		if got := kbVersionAtLeast([3]int{a, b, c}, [3]int{x, y, z}); got != tc.ok {
			t.Errorf("%s >= %s = %v, want %v", tc.have, tc.min, got, tc.ok)
		}
	}
}

func TestKB_TooOldIsUnavailableAndIsNotRun(t *testing.T) {
	dir := fakeKBWithVersion(t, "0.0.1")
	a := newTestAgent(t)
	var out strings.Builder
	err := a.runKB([]string{"search", "x"}, &out)
	if err == nil || !strings.Contains(err.Error(), "0.0.1") || !strings.Contains(err.Error(), kbMinVersion) {
		t.Fatalf("want an error naming both versions, got %v", err)
	}
	if ExitCodeFor(err) != ClassUnavailable {
		t.Errorf("class = %v, want unavailable (exit 69)", ExitCodeFor(err))
	}
	if _, statErr := os.Stat(filepath.Join(dir, "calls")); statErr == nil {
		t.Error("an unsupported kb must not be run")
	}
	if _, err := a.kbRecall("zebra", 3); err == nil || ExitCodeFor(err) != ClassUnavailable {
		t.Errorf("kbRecall with an old kb: %v", err)
	}
	if got := strings.Join(kbVerbNames(), ","); got != "inject,learn" {
		t.Errorf("an old kb must not supply verbs, got %s", got)
	}
}

func TestKB_UnreadableVersionIsUnavailable(t *testing.T) {
	fakeKBWithVersion(t, "banana")
	a := newTestAgent(t)
	if err := a.runKB([]string{"search", "x"}, &strings.Builder{}); err == nil || ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("got %v", err)
	}
}

func TestKB_SupportedVersionRunsAndIsAskedOnce(t *testing.T) {
	dir := fakeKBWithVersion(t, kbMinVersion)
	a := newTestAgent(t)
	for i := 0; i < 3; i++ {
		var out strings.Builder
		if err := a.runKB([]string{"search", "x"}, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "ran") {
			t.Fatalf("kb did not run: %q", out.String())
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "versions"))
	if n := strings.Count(string(raw), "v"); n != 1 {
		t.Errorf("kb -version asked %d times, want 1 per process", n)
	}
}
