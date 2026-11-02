package harvey

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// An upgrade notice tells a person, once, what changed since the version their
// workspace last ran. It replaces the per-process deprecation notices. It goes
// to standard error only: Run reads the session's input from standard input and
// writes the session to standard output, and a notice that touched either could
// bring back the bug the profile prompt had, where a prompt before the REPL took
// the first line of piped input as its answer.

/** upgradeNotice is one release's note to a person upgrading to it.
 *
 * Fields:
 *   Version (string) — the release that introduced the change, e.g. "0.0.21".
 *   Text    (string) — what changed and what to do about it; one or more
 *                      lines, written for someone who has not read CHANGES.md.
 *
 * Example:
 *   upgradeNotice{Version: "0.0.21", Text: "profiles are gone"}
 */
type upgradeNotice struct {
	Version string
	Text    string
}

/** upgradeNoticeTable is what Run announces, oldest first. Add an entry for a
 * release that changes something a person using the previous one would notice:
 * a command removed, a prompt gone, a default moved. A release with no entry
 * prints nothing. CHANGES.md stays the full record.
 */
var upgradeNoticeTable = []upgradeNotice{
	{"0.0.21", `Workspace profiles are gone: the first-run "Select [1-7]" prompt, /profile and /memory profile.
    A profile file already in agents/memories/ stays on disk and is no longer injected.
  The memory store (/memory, agents/memories/, mining) is deprecated and will be removed.
    Keep what you want with: /kb observation add --project NAME KIND BODY. /kb draft proposes observations from a session.
  Tool results are now read by the model before they are compacted; before, only read_file's were (CHANGES.md, Fixed).`},
}

// runningVersion is the version Run announces an upgrade to. A variable so a
// test can stand in a release; the build sets Version.
var runningVersion = Version

// upgradeBaseline is the version assumed for a workspace that has been used but
// has no last_version file: the last release before this one existed.
const upgradeBaseline = "0.0.20"

/** versionLess reports whether version a is older than b, comparing dotted
 * numbers field by field. A leading "v" is ignored, and so is a suffix after "-"
 * or "+" (so "0.0.21-dev" is not older than "0.0.21"). A field that is not a
 * number counts as 0.
 *
 * Parameters:
 *   a, b (string) — versions such as "0.0.20" or "v0.0.21".
 *
 * Returns:
 *   bool — true when a is strictly older than b.
 *
 * Example:
 *   versionLess("0.0.9", "0.0.10") // true: 9 < 10, not "9" > "1"
 */
func versionLess(a, b string) bool {
	pa, pb := versionFields(a), versionFields(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func versionFields(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, f := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	return out
}

// workspaceHasBeenUsed reports whether the workspace held anything before this
// run: a session on disk, a memory, or a harvey.yaml. This run has already made
// its own empty directories, so those do not count.
func (a *Agent) workspaceHasBeenUsed() bool {
	root := a.Workspace.HarveyDir()
	for _, sub := range []string{"sessions", "memories"} {
		if entries, err := os.ReadDir(filepath.Join(root, sub)); err == nil && len(entries) > 0 {
			return true
		}
	}
	_, err := os.Stat(filepath.Join(root, "harvey.yaml"))
	return err == nil
}

/** announceUpgrade writes to stderr what changed since the version this
 * workspace last ran, then records current in agents/last_version.
 *
 * Nothing is written for a brand-new workspace (it lived through no change), for
 * the same version again, for a downgrade (the record is left alone, so the next
 * upgrade still announces), or for a release with no table entry; the version is
 * still recorded in all but the downgrade. A workspace that has been used but has
 * no record counts as coming from upgradeBaseline. A failure to read or write the
 * record is ignored: the worst result is that the notice shows again.
 *
 * Parameters:
 *   stderr  (io.Writer)       — where the notice goes; nil is ignored.
 *   current (string)          — the running version.
 *   table   ([]upgradeNotice) — the notices, oldest first.
 *
 * Example:
 *   a.announceUpgrade(os.Stderr, "0.0.21", upgradeNoticeTable)
 */
func (a *Agent) announceUpgrade(stderr io.Writer, current string, table []upgradeNotice) {
	if a.Workspace == nil || current == "" {
		return
	}
	path := filepath.Join(a.Workspace.HarveyDir(), "last_version")
	prev := ""
	if b, err := os.ReadFile(path); err == nil {
		prev = strings.TrimSpace(string(b))
	}
	record := func() { _ = os.WriteFile(path, []byte(current+"\n"), 0o644) }

	if prev == "" {
		if !a.workspaceHasBeenUsed() {
			record()
			return
		}
		prev = upgradeBaseline
	}
	if versionLess(current, prev) {
		return
	}
	if !versionLess(prev, current) {
		return
	}
	var lines []string
	for _, n := range table {
		if versionLess(prev, n.Version) && !versionLess(current, n.Version) {
			lines = append(lines, "  "+n.Text)
		}
	}
	if len(lines) > 0 && stderr != nil {
		fmt.Fprintf(stderr, "harvey: upgraded from %s to %s. What changed:\n%s\n", prev, current, strings.Join(lines, "\n"))
	}
	record()
}
