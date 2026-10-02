package harvey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"

	sqlite "github.com/glebarez/go-sqlite"
)

// exitcode.go is the workspace exit-code convention (workspace DR-0003) for
// both binaries built from this package, harvey and assay. The design and the
// order of work are in exit-codes-plan.md; the cases each binary produces are
// surveyed in exit-codes-survey.md.

/** ExitClass is one row of the workspace exit-code table: a class name a
 * machine-readable caller can match on, and the number the process exits with.
 *
 * Fields:
 *   Name (string) — the class name, for example "no_input".
 *   Code (int)    — the process exit status.
 *
 * Example:
 *   os.Exit(harvey.ClassNoInput.Code) // 66
 */
type ExitClass struct {
	Name string
	Code int
}

/** The twelve classes of the workspace convention. The numbers are the BSD
 * sysexits.h values except 0, 1 and 2. 64, 67, 68, 71, 72 and 76 are not
 * used, and nothing above 125 may be, because the shell owns those.
 *
 * Example:
 *   err := harvey.NoInputf("corpus %s does not exist", path) // exits 66
 */
var (
	ClassOK           = ExitClass{"ok", 0}
	ClassNegative     = ExitClass{"negative", 1}
	ClassUsage        = ExitClass{"usage", 2}
	ClassData         = ExitClass{"data", 65}
	ClassNoInput      = ExitClass{"no_input", 66}
	ClassUnavailable  = ExitClass{"unavailable", 69}
	ClassInternal     = ExitClass{"internal", 70}
	ClassCantCreate   = ExitClass{"cant_create", 73}
	ClassIO           = ExitClass{"io", 74}
	ClassTempFail     = ExitClass{"temp_fail", 75}
	ClassNoPermission = ExitClass{"no_permission", 77}
	ClassConfig       = ExitClass{"config", 78}
)

/** ErrNotFound marks an error raised because a named thing does not exist: a
 * model, a skill, a session. It classifies as negative (exit 1): the command
 * ran correctly and the answer is no. Wrap it with %w so the message says
 * what was not found.
 *
 * Example:
 *   return fmt.Errorf("model %q: %w", name, harvey.ErrNotFound)
 */
var ErrNotFound = errors.New("not found")

/** ErrInvalid marks an error raised because a value is not acceptable. It
 * classifies as usage (exit 2), which is right when the value came from the
 * command line. A value read from a file is wrong content instead: wrap the
 * error with ClassedAs(ClassData, err), and the outermost class wins.
 *
 * Example:
 *   return fmt.Errorf("--overlap %q: %w", v, harvey.ErrInvalid)
 */
var ErrInvalid = errors.New("invalid value")

/** ClassedError carries an exit class with an error. Error and Unwrap forward
 * to the wrapped error, so a message reads exactly as it would have without
 * the class, and errors.Is and errors.As still reach the cause.
 *
 * Fields:
 *   Class (ExitClass) — the class the error exits with.
 *   Err   (error)     — the underlying error.
 *
 * Example:
 *   var ce *harvey.ClassedError
 *   if errors.As(err, &ce) { fmt.Println(ce.Class.Name) }
 */
type ClassedError struct {
	Class ExitClass
	Err   error
}

/** Error returns the wrapped error's message unchanged.
 *
 * Returns:
 *   string — the message of the underlying error.
 *
 * Example:
 *   msg := harvey.Usagef("unknown flag %s", "-x").Error() // "unknown flag -x"
 */
func (c *ClassedError) Error() string { return c.Err.Error() }

/** Unwrap returns the underlying error, so errors.Is and errors.As reach it.
 *
 * Returns:
 *   error — the underlying error.
 *
 * Example:
 *   cause := errors.Unwrap(harvey.IOf("write: %w", io.ErrShortWrite)) // io.ErrShortWrite
 */
func (c *ClassedError) Unwrap() error { return c.Err }

/** ClassedAs marks an existing error with a class and leaves nil alone. Use it
 * to reclassify what a helper returned. The outermost class in the chain wins,
 * so it overrides both an inner ClassedError and what the wrapped error would
 * have classified as.
 *
 * Parameters:
 *   class (ExitClass) — the class to give the error.
 *   err   (error)     — the error to mark, or nil.
 *
 * Returns:
 *   error — nil for a nil err, otherwise a *ClassedError wrapping err.
 *
 * Example:
 *   return harvey.ClassedAs(harvey.ClassData, fmt.Errorf("%s: %w", path, err))
 */
func ClassedAs(class ExitClass, err error) error {
	if err == nil {
		return nil
	}
	return &ClassedError{Class: class, Err: err}
}

func classErrorf(class ExitClass, format string, a ...any) error {
	return &ClassedError{Class: class, Err: fmt.Errorf(format, a...)}
}

/** Usagef is fmt.Errorf that marks its result as a usage error (exit 2): an
 * unknown flag, a missing or surplus argument, or any bad value passed on the
 * command line. Nothing was attempted.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format; %w keeps a cause reachable.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassUsage.
 *
 * Example:
 *   return harvey.Usagef("%s requires an argument", "-m")
 */
func Usagef(format string, a ...any) error { return classErrorf(ClassUsage, format, a...) }

/** Negativef is fmt.Errorf that marks its result as a normal negative answer
 * (exit 1): the command ran correctly and the answer is no.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassNegative.
 *
 * Example:
 *   return harvey.Negativef("no prompts match category %q", category)
 */
func Negativef(format string, a ...any) error { return classErrorf(ClassNegative, format, a...) }

/** NotFoundf is fmt.Errorf that marks its result as "no such item" (exit 1). It
 * is the same class as Negativef; the name says what the caller means.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassNegative.
 *
 * Example:
 *   return harvey.NotFoundf("skill %q not found", name)
 */
func NotFoundf(format string, a ...any) error { return classErrorf(ClassNegative, format, a...) }

/** Dataf is fmt.Errorf that marks its result as wrong content (exit 65): a
 * malformed corpus, Fountain file or YAML the tool was asked to read. A bad
 * value on the command line is a usage error instead.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassData.
 *
 * Example:
 *   return harvey.Dataf("%s: not a Fountain session file", path)
 */
func Dataf(format string, a ...any) error { return classErrorf(ClassData, format, a...) }

/** NoInputf is fmt.Errorf that marks its result as a missing input (exit 66): a
 * named file or directory, or the workspace, that is not there or is the wrong
 * kind of thing.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassNoInput.
 *
 * Example:
 *   return harvey.NoInputf("session file %s does not exist", path)
 */
func NoInputf(format string, a ...any) error { return classErrorf(ClassNoInput, format, a...) }

/** Unavailablef is fmt.Errorf that marks its result as a service that cannot be
 * reached (exit 69): a model server that is down or will not start.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassUnavailable.
 *
 * Example:
 *   return harvey.Unavailablef("no backend connected; use /model use")
 */
func Unavailablef(format string, a ...any) error { return classErrorf(ClassUnavailable, format, a...) }

/** CantCreatef is fmt.Errorf that marks its result as an output that cannot be
 * created (exit 73): the target exists, or its directory cannot be made.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassCantCreate.
 *
 * Example:
 *   return harvey.CantCreatef("cannot create recording %s: %w", path, err)
 */
func CantCreatef(format string, a ...any) error { return classErrorf(ClassCantCreate, format, a...) }

/** IOf is fmt.Errorf that marks its result as a read or write that failed part
 * way (exit 74).
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassIO.
 *
 * Example:
 *   return harvey.IOf("writing %s: %w", path, err)
 */
func IOf(format string, a ...any) error { return classErrorf(ClassIO, format, a...) }

/** NoPermissionf is fmt.Errorf that marks its result as a permission refusal
 * (exit 77): the operating system or Harvey's own permission rules refused
 * access to a path or an action.
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format; %w keeps a cause reachable.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassNoPermission.
 *
 * Example:
 *   return harvey.NoPermissionf("%s: read permission denied", path)
 */
func NoPermissionf(format string, a ...any) error {
	return classErrorf(ClassNoPermission, format, a...)
}

/** defaultClass gives err the class when nothing has classified it yet, and
 * leaves an error that already carries a class (an explicit one, or a
 * standard-library error that classifies itself) unchanged.
 *
 * Parameters:
 *   class (ExitClass) — the class to apply to an unclassified error.
 *   err   (error)     — the error; may be nil.
 *
 * Returns:
 *   error — err, or err classed as class when it had no class of its own.
 *
 * Example:
 *   return defaultClass(ClassUnavailable, fmt.Errorf("fetch %s: %w", uri, err))
 */
func defaultClass(class ExitClass, err error) error {
	if err == nil {
		return nil
	}
	if _, classified := ExitClassOf(err); classified {
		return err
	}
	return ClassedAs(class, err)
}

/** Configf is fmt.Errorf that marks its result as a configuration file that is
 * present but wrong (exit 78).
 *
 * Parameters:
 *   format (string) — a fmt.Errorf format.
 *   a      (...any) — the format's arguments.
 *
 * Returns:
 *   error — a *ClassedError of ClassConfig.
 *
 * Example:
 *   return harvey.Configf("harvey.yaml: %w", err)
 */
func Configf(format string, a ...any) error { return classErrorf(ClassConfig, format, a...) }

/** ExitClassOf returns the exit class of err, and whether anything classified
 * it. An explicit class (the outermost *ClassedError in the chain) wins.
 * Otherwise standard-library errors classify themselves: ErrNotFound is
 * negative and ErrInvalid usage; a SQLite error is by result code (see
 * sqliteClass); content that will not decode as JSON is data; fs.ErrNotExist
 * is no_input, fs.ErrPermission no_permission, fs.ErrExist cant_create, any
 * other file error io; a network error (checked by type before the file errors,
 * since a refused connection wraps a syscall error) or an expired deadline is
 * unavailable.
 * An error nothing classified is ClassInternal with false. nil is ClassOK.
 *
 * Parameters:
 *   err (error) — the error to classify; may be nil.
 *
 * Returns:
 *   ExitClass — the class.
 *   bool      — false only when nothing classified err.
 *
 * Example:
 *   class, ok := harvey.ExitClassOf(fmt.Errorf("read: %w", fs.ErrNotExist)) // ClassNoInput, true
 */
func ExitClassOf(err error) (ExitClass, bool) {
	if err == nil {
		return ClassOK, true
	}
	var ce *ClassedError
	if errors.As(err, &ce) {
		return ce.Class, true
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return ClassNegative, true
	case errors.Is(err, ErrInvalid):
		return ClassUsage, true
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		return sqliteClass(se.Code()), true
	}
	// Content that would not decode is wrong content, whichever code read it.
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &syn) || errors.As(err, &typ) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ClassData, true
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ClassNoInput, true
	case errors.Is(err, fs.ErrPermission):
		return ClassNoPermission, true
	case errors.Is(err, fs.ErrExist):
		return ClassCantCreate, true
	}
	// Network errors, by concrete type first. A refused connection wraps an
	// *os.SyscallError, so the file errors below would call it a failed write;
	// but the net.Error interface cannot be tested first either, because a
	// bare syscall.Errno (what a *fs.PathError wraps) satisfies it too.
	var oe *net.OpError
	var ue *url.Error
	var de *net.DNSError
	if errors.As(err, &oe) || errors.As(err, &ue) || errors.As(err, &de) ||
		errors.Is(err, context.DeadlineExceeded) {
		return ClassUnavailable, true
	}
	var pe *fs.PathError
	var le *os.LinkError
	var sce *os.SyscallError
	if errors.As(err, &pe) || errors.As(err, &le) || errors.As(err, &sce) {
		return ClassIO, true
	}
	// Any other network error type (a custom net.Error, a timeout).
	var ne net.Error
	if errors.As(err, &ne) {
		return ClassUnavailable, true
	}
	return ClassInternal, false
}

/** ExitCodeFor is the class a binary exits with for err: what ExitClassOf
 * says, and ClassInternal (70, not 1) for an error nothing classified, so a
 * missing classification shows up as an exit code instead of hiding as an
 * ordinary "no".
 *
 * Parameters:
 *   err (error) — the error a run returned; may be nil.
 *
 * Returns:
 *   ExitClass — the class to exit with.
 *
 * Example:
 *   os.Exit(harvey.ExitCodeFor(err).Code)
 */
func ExitCodeFor(err error) ExitClass {
	class, _ := ExitClassOf(err)
	return class
}

/** AsCreate reclassifies a failure to create an output as cant_create (exit
 * 73): the file or directory could not be made. A missing parent or an
 * existing target is not "no input" here, it is an output that cannot be
 * created. A permission failure keeps its own class (77), a failure part way
 * through writing stays io (74), and an error that already carries an explicit
 * class is returned unchanged.
 *
 * Parameters:
 *   err (error) — an error from creating a file or directory; may be nil.
 *
 * Returns:
 *   error — err, or err classed as cant_create.
 *
 * Example:
 *   return harvey.AsCreate(fmt.Errorf("creating %s: %w", dir, err))
 */
func AsCreate(err error) error {
	var ce *ClassedError
	if err == nil || errors.As(err, &ce) || errors.Is(err, fs.ErrPermission) {
		return err
	}
	var pe *fs.PathError
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrExist) ||
		(errors.As(err, &pe) && (pe.Op == "open" || pe.Op == "mkdir" || pe.Op == "create")) {
		return ClassedAs(ClassCantCreate, err)
	}
	return err
}

// sqliteClass maps a SQLite result code to a class: busy or locked (5, 6) is
// temp_fail, since a retry may succeed; a corrupt file or one that is not a
// database (11, 26) and a constraint violation (19) are data; any other code is
// io. Extended result codes carry the primary code in their low byte.
func sqliteClass(code int) ExitClass {
	switch code & 0xff {
	case 5, 6:
		return ClassTempFail
	case 11, 26, 19:
		return ClassData
	}
	return ClassIO
}

/** PrintJSONError writes err to w as the workspace convention's machine-
 * readable error envelope, matching kb's --json shape (workspace DR-0003):
 * {"error": "...", "class": "...", "code": N}. A nil err writes nothing.
 * Both harvey and assay use this for their --json mode, so a script reading
 * either binary's stderr gets the same shape kb already produces.
 *
 * Parameters:
 *   w   (io.Writer) — destination, normally the process's stderr.
 *   err (error)     — the error to report; a no-op when nil.
 *
 * Returns:
 *   int — the exit code err classifies to (0 for nil).
 *
 * Example:
 *   os.Exit(harvey.PrintJSONError(os.Stderr, err))
 */
func PrintJSONError(w io.Writer, err error) int {
	class := ExitCodeFor(err)
	if err == nil {
		return class.Code
	}
	envelope := struct {
		Error string `json:"error"`
		Class string `json:"class"`
		Code  int    `json:"code"`
	}{Error: err.Error(), Class: class.Name, Code: class.Code}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(envelope)
	return class.Code
}

/** ExtractJSONFlag reports whether "-json" or "--json" appears anywhere in
 * args and returns args with every occurrence removed, so a flag parser
 * downstream never sees it as an unknown flag. Both spellings are accepted
 * since harvey's own flags are double-dash-only and assay's (the standard
 * library flag package) are conventionally single-dash. Both binaries call
 * this before their own argument parsing, so --json is honoured even when a
 * later flag on the same command line — or the flag package's own parse —
 * is what fails.
 *
 * Parameters:
 *   args ([]string) — the raw command line, args[0] is the program name.
 *
 * Returns:
 *   bool     — true when -json or --json was present.
 *   []string — args with every occurrence removed; the relative order of
 *              everything else is unchanged.
 *
 * Example:
 *   jsonOut, args := harvey.ExtractJSONFlag(os.Args) // ["prog","--json","-x"] -> true, ["prog","-x"]
 */
func ExtractJSONFlag(args []string) (bool, []string) {
	found := false
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" || a == "-json" {
			found = true
			continue
		}
		filtered = append(filtered, a)
	}
	return found, filtered
}
