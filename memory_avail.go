package harvey

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
)

/** availableMemory reads how much memory the machine can give a new process,
 * in bytes: MemAvailable from /proc/meminfo on Linux. It is a variable so a
 * test can say how much there is. Elsewhere, or when /proc/meminfo cannot be
 * read, it reports false and the context window is limited by the ceiling
 * alone (design/memory-aware-context-window-design.md).
 *
 * Returns:
 *   int64 — bytes available.
 *   bool  — false when the amount is not known.
 *
 * Example:
 *   if n, ok := availableMemory(); ok { fmt.Println(n >> 20, "MiB") }
 */
var availableMemory = readAvailableMemory

// readAvailableMemory is availableMemory for this machine.
func readAvailableMemory() (int64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	return parseMemAvailable(f)
}

// parseMemAvailable finds "MemAvailable: N kB" in /proc/meminfo's text.
func parseMemAvailable(r io.Reader) (int64, bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "MemAvailable:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, false
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || kb <= 0 {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}
