package harvey

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

/** kbMinVersion is the oldest kb harvey works with: the release whose JSON
 * the fixtures in testdata/kb were captured from and whose verbs harvey calls.
 * Raise it when harvey starts to depend on something a newer kb added (a
 * contract test and a fixture go with the change); a kb older than this, or
 * one that does not report a version, is treated as unavailable.
 */
const kbMinVersion = "0.0.29"

var kbVersionPattern = regexp.MustCompile(`^kb\s+v?(\d+)\.(\d+)\.(\d+)`)

/** parseKBVersion reads the semver out of the first line `kb -version` prints
 * ("kb 0.0.29 7d292d7"). A pre-release or build suffix on the version is
 * ignored.
 *
 * Parameters:
 *   out (string) — kb's output.
 *
 * Returns:
 *   major, minor, patch (int) — the version.
 *   error — when the text is not "kb MAJOR.MINOR.PATCH ...".
 *
 * Example:
 *   a, b, c, err := parseKBVersion("kb 0.0.29 7d292d7") // 0, 0, 29, nil
 */
func parseKBVersion(out string) (major, minor, patch int, err error) {
	m := kbVersionPattern.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, 0, fmt.Errorf("unreadable kb version %q", out)
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	patch, _ = strconv.Atoi(m[3])
	return major, minor, patch, nil
}

/** kbVersionAtLeast reports whether have is the same as or newer than min.
 *
 * Parameters:
 *   have, min ([3]int) — major, minor, patch.
 *
 * Returns:
 *   bool — true when have >= min.
 *
 * Example:
 *   ok := kbVersionAtLeast([3]int{0, 0, 30}, [3]int{0, 0, 29}) // true
 */
func kbVersionAtLeast(have, min [3]int) bool {
	for i := range have {
		if have[i] != min[i] {
			return have[i] > min[i]
		}
	}
	return true
}

var (
	kbVersionMu    sync.Mutex
	kbVersionCache = map[string]error{}
)

/** lookKB finds the installed kb and checks that it is a version harvey pairs
 * with (kbMinVersion or newer). The check runs `kb -version` once per binary
 * per process. A kb that is missing, too old or unreadable is unavailable
 * (exit 69), and callers must not run it.
 *
 * Returns:
 *   string — the path of kb.
 *   error — an Unavailable error naming what was found and what is needed.
 *
 * Example:
 *   bin, err := lookKB()
 */
func lookKB() (string, error) {
	bin, err := exec.LookPath("kb")
	if err != nil {
		return "", Unavailablef("kb is not installed (or not on PATH). Install it with: %s", kbInstallHint)
	}
	kbVersionMu.Lock()
	cached, seen := kbVersionCache[bin]
	kbVersionMu.Unlock()
	if !seen {
		cached = checkKBVersion(bin)
		kbVersionMu.Lock()
		kbVersionCache[bin] = cached
		kbVersionMu.Unlock()
	}
	if cached != nil {
		return "", cached
	}
	return bin, nil
}

// checkKBVersion asks bin for its version and compares it with kbMinVersion.
func checkKBVersion(bin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-version").Output()
	if err != nil {
		return Unavailablef("kb at %s did not report its version (%v); harvey needs kb %s or newer. Install it with: %s",
			bin, err, kbMinVersion, kbInstallHint)
	}
	a, b, c, perr := parseKBVersion(string(out))
	if perr != nil {
		return Unavailablef("kb at %s: %v; harvey needs kb %s or newer. Install it with: %s",
			bin, perr, kbMinVersion, kbInstallHint)
	}
	ma, mb, mc, _ := parseKBVersion("kb " + kbMinVersion)
	if !kbVersionAtLeast([3]int{a, b, c}, [3]int{ma, mb, mc}) {
		return Unavailablef("kb %d.%d.%d at %s is older than the %s harvey needs. Install a newer one with: %s",
			a, b, c, bin, kbMinVersion, kbInstallHint)
	}
	return nil
}
