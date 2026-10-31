package harvey

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// deprecationWriter receives notices raised where no output writer is at hand
// (building a client for a route, opening an s3:// reader).
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
 *   key (string)    — identity of the notice, such as "route:gemini".
 *   msg (string)    — the notice text, without a leading marker or newline.
 *
 * Example:
 *   deprecationNotice(out, "s3", "s3:// is deprecated")
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

const deprecationTail = "It is deprecated and will be removed in the next release (workspace DR-0033, DR-0042). See CHANGES.md."

/** routeKindNotice returns the notice key and text for a route kind that is
 * being retired.
 *
 * Parameters:
 *   kind (RouteKind) — the endpoint kind.
 *
 * Returns:
 *   string — notice key; "" when the kind is not deprecated.
 *   string — notice text.
 *
 * Example:
 *   key, msg := routeKindNotice(KindGemini)
 */
func routeKindNotice(kind RouteKind) (string, string) {
	switch kind {
	case KindLlamafile, KindLlamaCpp:
		return "route:" + string(kind), fmt.Sprintf("The %s route kind reaches a backend harvey is dropping. %s", kind, deprecationTail)
	case KindAnthropic, KindDeepSeek, KindGemini, KindMistral, KindOpenAI:
		return "route:" + string(kind), fmt.Sprintf("The %s route kind sends prompts to a commercial service. %s", kind, deprecationTail)
	}
	return "", ""
}

/** noteRouteKind prints the deprecation notice for kind, once, when there is one.
 *
 * Parameters:
 *   out  (io.Writer) — destination.
 *   kind (RouteKind) — the endpoint kind.
 *
 * Example:
 *   noteRouteKind(out, ep.Kind)
 */
func noteRouteKind(out io.Writer, kind RouteKind) {
	if key, msg := routeKindNotice(kind); key != "" {
		deprecationNotice(out, key, msg)
	}
}

/** startupDeprecations prints a notice for each retiring feature the loaded
 * configuration uses: registered llamafiles, an active llamafile, and routes
 * of a retiring kind. The llama.cpp and llamafile backends are noted again
 * once one is chosen (noteActiveBackend).
 *
 * Parameters:
 *   out (io.Writer) — destination.
 *
 * Example:
 *   a.startupDeprecations(os.Stdout)
 */
func (a *Agent) startupDeprecations(out io.Writer) {
	if a.Config.Llamafile.Active != "" || len(a.Config.Llamafile.Models) > 0 {
		deprecationNotice(out, "backend:llamafile", "llamafile support is being removed. "+deprecationTail)
	}
	if a.Routes != nil {
		for _, ep := range a.Routes.Endpoints {
			noteRouteKind(out, ep.Kind)
		}
	}
}

/** noteActiveBackend prints a notice when the selected backend is llama.cpp
 * or llamafile.
 *
 * Parameters:
 *   out (io.Writer) — destination.
 *
 * Example:
 *   a.noteActiveBackend(out)
 */
func (a *Agent) noteActiveBackend(out io.Writer) {
	if a.Backend == nil {
		return
	}
	switch name := a.Backend.Name(); name {
	case "llamafile", "llamacpp":
		deprecationNotice(out, "backend:"+name, name+" support is being removed. "+deprecationTail)
	}
}
