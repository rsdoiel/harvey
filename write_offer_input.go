package harvey

import (
	"fmt"
	"io"
	"strings"
)

/** nextQueuedInput pops the next line a prompt handed back to the REPL. The
 * REPL asks it before reading the keyboard, so a command typed at a prompt
 * that wanted a path or a yes/no is run, not lost.
 *
 * Returns:
 *   string — the queued line, or "" when the queue is empty.
 *   bool   — true when a line was returned.
 *
 * Example:
 *   if line, ok := a.nextQueuedInput(); ok { runIt(line) }
 */
func (a *Agent) nextQueuedInput() (string, bool) {
	if len(a.pendingInput) == 0 {
		return "", false
	}
	line := a.pendingInput[0]
	a.pendingInput = a.pendingInput[1:]
	return line, true
}

/** looksLikeCommand reports whether a line typed at a prompt is a Harvey
 * command rather than an answer: "!" followed by something, or "/" followed by
 * a registered command name (or exit, quit, bye), so "/exit" is a command and
 * "/out/run.sh" is a path.
 *
 * Parameters:
 *   line (string) — the line as typed.
 *
 * Returns:
 *   bool — true when the REPL would run the line as a command.
 *
 * Example:
 *   if a.looksLikeCommand("/exit") { a.pendingInput = append(a.pendingInput, "/exit") }
 */
func (a *Agent) looksLikeCommand(line string) bool {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "!"):
		return strings.TrimSpace(line[1:]) != ""
	case strings.HasPrefix(line, "/"):
		fields := strings.Fields(line[1:])
		if len(fields) == 0 {
			return false
		}
		name := strings.ToLower(fields[0])
		if name == "exit" || name == "quit" || name == "bye" {
			return true
		}
		_, ok := a.commands[name]
		return ok
	}
	return false
}

/** queueIfCommand hands line back to the REPL when it is a command, and says
 * so. A prompt that wanted a path or a yes/no calls it with whatever the user
 * typed, and must stop asking when it returns true.
 *
 * Parameters:
 *   line (string)    — the answer as typed, trimmed.
 *   out  (io.Writer) — where the notice goes.
 *
 * Returns:
 *   bool — true when line was a command and has been queued.
 *
 * Example:
 *   if a.queueIfCommand(answer, out) { return }
 */
func (a *Agent) queueIfCommand(line string, out io.Writer) bool {
	if !a.looksLikeCommand(line) {
		return false
	}
	a.pendingInput = append(a.pendingInput, line)
	fmt.Fprintf(out, "\n  %q is a command, not an answer; skipping the remaining write offers and running it next.\n", line)
	return true
}
