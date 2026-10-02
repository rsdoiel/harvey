package harvey

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
)

/** errNoWorkspace is the error a command returns when it needs a workspace and
 * the agent has none. A missing workspace is a missing input (exit 66).
 *
 * Returns:
 *   error — a no_input *ClassedError.
 *
 * Example:
 *   if a.Workspace == nil {
 *       return errNoWorkspace()
 *   }
 */
func errNoWorkspace() error { return NoInputf("no workspace initialised") }

/** errNoBackend is the error a command returns when it needs a model and none
 * is connected. A model server that cannot be reached is unavailable (exit 69).
 *
 * Returns:
 *   error — an unavailable *ClassedError.
 *
 * Example:
 *   if a.Client == nil {
 *       return errNoBackend()
 *   }
 */
func errNoBackend() error {
	return Unavailablef("no backend connected. Use /model use to connect a model (for Ollama, run `ollama serve` first)")
}

/** startFailure classifies an external program that could not be started. A
 * program that does not exist is a missing input (exit 66); anything else that
 * stopped it from starting is an I/O failure (exit 74).
 *
 * Parameters:
 *   program (string) — the program name, for the message.
 *   err     (error)  — the error from exec; never nil here.
 *
 * Returns:
 *   error — a classed error naming the program.
 *
 * Example:
 *   if cmd.ProcessState == nil && runErr != nil {
 *       return startFailure("git", runErr)
 *   }
 */
func startFailure(program string, err error) error {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return ClassedAs(ClassNoInput, fmt.Errorf("%s: %w", program, err))
	}
	return defaultClass(ClassIO, fmt.Errorf("%s: %w", program, err))
}
