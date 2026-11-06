package harvey

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// startup_check.go: the inputs a person names on the command line are checked
// before the session starts (exit-codes-plan.md H3), so a script gets the exit
// status before a model is loaded and not after.

/** CheckStartupInputs verifies the inputs named on the command line: the file
 * given to --continue and --replay must exist (66), a --replay file must parse
 * as a session (65), and the --record-file path must be creatable (73 when its
 * directory cannot be made, 77 when the operating system refuses). It touches
 * nothing but a short-lived probe file in the recording directory, and returns
 * the first problem.
 *
 * Parameters:
 *   cfg (*Config) — the configuration built from the command line.
 *
 * Returns:
 *   error — nil, or an error classified for the first input that is unusable.
 *
 * Example:
 *   if err := harvey.CheckStartupInputs(cfg); err != nil { return err }
 */
func CheckStartupInputs(cfg *Config) error {
	if p := cfg.Session.ContinuePath; p != "" {
		if _, err := os.Stat(p); err != nil {
			return NoInputf("--continue %s: %w", p, err)
		}
	}
	if p := cfg.Session.ReplayPath; p != "" {
		if _, err := LoadSessionInfo(p); err != nil {
			return err
		}
	}
	if p := cfg.Session.RecordPath; p != "" {
		if err := checkCreatable(p); err != nil {
			return err
		}
	}
	return nil
}

// checkCreatable reports whether a file could be created at path: the nearest
// directory that already exists must be a directory and accept a new file. The
// directories between it and path are made when the session records, so their
// absence is fine. It leaves nothing behind.
func checkCreatable(path string) error {
	dir := filepath.Dir(path)
	for {
		fi, err := os.Stat(dir)
		if err == nil {
			if !fi.IsDir() {
				return CantCreatef("cannot record to %s: %s is not a directory", path, dir)
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return CantCreatef("cannot record to %s: %w", path, err)
		}
		dir = parent
	}
	probe, err := os.CreateTemp(dir, ".harvey-record-check-*")
	if err != nil {
		// Report the operating system's reason, not the probe file's name.
		reason := err
		var pe *fs.PathError
		if errors.As(err, &pe) {
			reason = pe.Err
		}
		class := ClassCantCreate
		if ExitCodeFor(err) == ClassNoPermission {
			class = ClassNoPermission
		}
		return ClassedAs(class, fmt.Errorf("cannot record to %s: %v", path, reason))
	}
	probe.Close()
	os.Remove(probe.Name())
	return nil
}
