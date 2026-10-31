package harvey

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// deprecationWriter receives notices raised where no output writer is at hand
// (a model tool call).
var deprecationWriter io.Writer = os.Stderr

var (
	deprecationMu   sync.Mutex
	deprecationSeen = map[string]bool{}
)

/** resetDeprecationNotices forgets which notices were shown, so each can be
 * shown again. Used by tests.
 *
 * Example:
 *   resetDeprecationNotices()
 */
func resetDeprecationNotices() {
	deprecationMu.Lock()
	deprecationSeen = map[string]bool{}
	deprecationMu.Unlock()
}

/** deprecationNotice writes msg to out once per process for each key.
 *
 * Parameters:
 *   out (io.Writer) — destination; nil is ignored.
 *   key (string)    — identity of the notice, such as "memory".
 *   msg (string)    — the notice text, without a leading marker or newline.
 *
 * Example:
 *   deprecationNotice(out, "memory", "the memory store is deprecated")
 */
func deprecationNotice(out io.Writer, key, msg string) {
	if out == nil {
		return
	}
	deprecationMu.Lock()
	seen := deprecationSeen[key]
	deprecationSeen[key] = true
	deprecationMu.Unlock()
	if seen {
		return
	}
	fmt.Fprintf(out, "  ⚠  %s\n", msg)
}

// memoryRetiringNotice is the one notice for everything DR-0037 retires.
const memoryRetiringNotice = "The memory store is deprecated and will be removed in a following release (DR-0037): " +
	"/memory, agents/memories/, onboarding profiles, mining and the add_memory, update_memory and delete_memory tools. " +
	"Files already in agents/memories/ stay on disk but will not be read. " +
	"Keep what you want with: /kb observation add --project NAME KIND BODY. See CHANGES.md."

/** noteMemoryRetiring prints the memory-store deprecation notice, once per
 * process, to out.
 *
 * Parameters:
 *   out (io.Writer) — destination.
 *
 * Example:
 *   noteMemoryRetiring(out)
 */
func noteMemoryRetiring(out io.Writer) {
	deprecationNotice(out, "memory", memoryRetiringNotice)
}

/** startupDeprecations prints a notice for each retiring feature the
 * workspace uses. The memory notice appears only when the memory store holds
 * at least one memory, so a workspace that never used it is not told.
 *
 * Parameters:
 *   out (io.Writer) — destination.
 *
 * Example:
 *   a.startupDeprecations(os.Stdout)
 */
func (a *Agent) startupDeprecations(out io.Writer) {
	if a.Memory != nil && a.Memory.Store != nil {
		if n, err := a.Memory.Store.Count(); err == nil && n > 0 {
			noteMemoryRetiring(out)
		}
	}
}
