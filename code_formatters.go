package harvey

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─── PipeExternalFormatter ────────────────────────────────────────────────────

/** PipeExternalFormatter invokes an external tool that reads source code from
 * stdin and writes formatted code to stdout.  This is the pipe-mode path and
 * does not require safe mode to be disabled; the caller is responsible for any
 * allowed-command checks before invoking Format.
 *
 * If the executable is not found on $PATH, Format returns (content, nil)
 * (identity) so that a missing tool degrades gracefully.
 *
 * Fields (set via newPipeExtFormatter):
 *   langID  — language this formatter targets, e.g. "go".
 *   exe     — executable name, e.g. "gofmt".
 *   args    — additional arguments appended before content is piped in.
 *   timeout — per-invocation deadline; 0 = 30 s default.
 *
 * Example:
 *   f := newPipeExtFormatter("go", "gofmt")
 *   formatted, err := f.Format("package main\n", "main.go")
 */
type PipeExternalFormatter struct {
	langID  string
	exe     string
	args    []string
	timeout time.Duration
}

/** newPipeExtFormatter constructs a PipeExternalFormatter with the given
 * language ID, executable name, and optional extra arguments.
 *
 * Parameters:
 *   langID (string)    — language ID, e.g. "go".
 *   exe    (string)    — executable, e.g. "gofmt".
 *   args   (...string) — additional CLI arguments.
 *
 * Returns:
 *   *PipeExternalFormatter — ready-to-use formatter.
 *
 * Example:
 *   f := newPipeExtFormatter("c", "clang-format", "-")
 */
func newPipeExtFormatter(langID, exe string, args ...string) *PipeExternalFormatter {
	return &PipeExternalFormatter{langID: langID, exe: exe, args: append([]string(nil), args...)}
}

/** Language returns the language ID this formatter targets.
 *
 * Returns:
 *   string — language ID, e.g. "go".
 *
 * Example:
 *   fmt.Println(f.Language()) // "go"
 */
func (f *PipeExternalFormatter) Language() string { return f.langID }

/** Mode returns PipeFormatter, indicating this formatter uses stdin/stdout.
 *
 * Returns:
 *   FormatterMode — always PipeFormatter.
 *
 * Example:
 *   fmt.Println(f.Mode() == PipeFormatter) // true
 */
func (f *PipeExternalFormatter) Mode() FormatterMode { return PipeFormatter }

/** Format passes content through the external formatter and returns the result.
 * filePath is used for context (e.g. to pick a parser) but is not read or
 * written; content is passed via stdin.
 *
 * Parameters:
 *   content  (string) — source code to format.
 *   filePath (string) — file path hint passed to the formatter if needed.
 *
 * Returns:
 *   string — formatted content, or original content when the tool is absent.
 *   error  — non-nil only when the tool ran but returned an error.
 *
 * Example:
 *   out, err := f.Format("package main\n", "main.go")
 */
func (f *PipeExternalFormatter) Format(content string, filePath string) (string, error) {
	if _, err := exec.LookPath(f.exe); err != nil {
		return content, nil // graceful degradation — tool not installed
	}

	timeout := f.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build arg list; interpolate {{filepath}} placeholder if present.
	args := make([]string, len(f.args))
	for i, a := range f.args {
		if a == "{{filepath}}" {
			args[i] = filePath
		} else {
			args[i] = a
		}
	}

	cmd := exec.CommandContext(ctx, f.exe, args...)
	cmd.Env = filterCommandEnvironment(os.Environ())
	cmd.Stdin = strings.NewReader(content)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return content, fmt.Errorf("formatter %q: %w — %s", f.exe, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

/** Check reports whether content is already formatted.  Returns true and no
 * issues when content matches its formatted form; false and a single FormatIssue
 * when it differs or the tool is unavailable.
 *
 * Parameters:
 *   content  (string) — source code to check.
 *   filePath (string) — file path hint.
 *
 * Returns:
 *   bool         — true when content is already properly formatted.
 *   []FormatIssue — empty on pass; single entry on fail.
 *
 * Example:
 *   ok, issues := f.Check("package main\n", "main.go")
 */
func (f *PipeExternalFormatter) Check(content string, filePath string) (bool, []FormatIssue) {
	formatted, err := f.Format(content, filePath)
	if err != nil {
		return false, nil // tool error, can't determine
	}
	if formatted == content {
		return true, nil
	}
	return false, []FormatIssue{{Line: 1, Column: 0, Message: "gofmt: content is not in canonical format — " +
		"if auto-format is disabled, run gofmt or write already-formatted content; " +
		"if it was enabled, a syntax error may have prevented it (check the file compiles)", Severity: "info"}}
}

// ─── FileExternalFormatter ────────────────────────────────────────────────────

/** FileExternalFormatter invokes an external tool that reads and rewrites a
 * file in-place.  The caller MUST write the file to disk first, then call
 * Format.  This mode requires safe mode to be disabled at the call site; the
 * formatter itself does not enforce that constraint.
 *
 * If the executable is not found on $PATH, Format returns (content, nil).
 *
 * Example:
 *   f := newFileExtFormatter("pascal", "ptop", "-")
 *   formatted, err := f.Format(content, "/abs/path/module.pas")
 */
type FileExternalFormatter struct {
	langID  string
	exe     string
	args    []string
	timeout time.Duration
}

/** newFileExtFormatter constructs a FileExternalFormatter.
 *
 * Parameters:
 *   langID (string)    — language ID.
 *   exe    (string)    — executable name.
 *   args   (...string) — extra arguments; "{{filepath}}" is interpolated.
 *
 * Returns:
 *   *FileExternalFormatter — ready-to-use formatter.
 *
 * Example:
 *   f := newFileExtFormatter("pascal", "ptop", "{{filepath}}")
 */
func newFileExtFormatter(langID, exe string, args ...string) *FileExternalFormatter {
	return &FileExternalFormatter{langID: langID, exe: exe, args: append([]string(nil), args...)}
}

/** Language returns the language ID this formatter targets.
 *
 * Returns:
 *   string — language ID.
 */
func (f *FileExternalFormatter) Language() string { return f.langID }

/** Mode returns FileFormatter.
 *
 * Returns:
 *   FormatterMode — always FileFormatter.
 */
func (f *FileExternalFormatter) Mode() FormatterMode { return FileFormatter }

/** Format runs the external formatter on filePath (which must already be
 * written to disk) and returns the re-read content.
 *
 * Parameters:
 *   content  (string) — original content; returned unchanged on tool error.
 *   filePath (string) — absolute path to the already-written file.
 *
 * Returns:
 *   string — reformatted file content, or original content on error.
 *   error  — non-nil when the tool ran but failed.
 *
 * Example:
 *   out, err := f.Format(src, "/workspace/module.pas")
 */
func (f *FileExternalFormatter) Format(content string, filePath string) (string, error) {
	if _, err := exec.LookPath(f.exe); err != nil {
		return content, nil
	}

	timeout := f.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := make([]string, len(f.args))
	for i, a := range f.args {
		if a == "{{filepath}}" {
			args[i] = filePath
		} else {
			args[i] = a
		}
	}

	cmd := exec.CommandContext(ctx, f.exe, args...)
	cmd.Env = filterCommandEnvironment(os.Environ())
	cmd.Dir = filepath.Dir(filePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return content, fmt.Errorf("formatter %q: %w — %s", f.exe, err, strings.TrimSpace(stderr.String()))
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return content, fmt.Errorf("formatter %q: re-read failed: %w", f.exe, err)
	}
	return string(data), nil
}

/** Check reports whether the file is already formatted.
 *
 * Parameters:
 *   content  (string) — source code to check.
 *   filePath (string) — absolute path hint.
 *
 * Returns:
 *   bool         — true when already formatted.
 *   []FormatIssue — empty on pass.
 */
func (f *FileExternalFormatter) Check(content string, filePath string) (bool, []FormatIssue) {
	return false, nil // file-mode check requires running the formatter; not supported
}

// ─── BuiltinFormatter ─────────────────────────────────────────────────────────

/** BuiltinFormatter applies a pure-Go formatting function to source text.
 * It implements the PipeFormatter mode and requires no external tools, safe
 * mode, or workspace access.
 *
 * Example:
 *   f := newBuiltinFormatter("pascal", normaliseText)
 *   formatted, err := f.Format("  x := 1;  \n", "module.pas")
 */
type BuiltinFormatter struct {
	langID string
	fn     func(content string) string
}

/** newBuiltinFormatter constructs a BuiltinFormatter that applies fn to source.
 *
 * Parameters:
 *   langID (string)                  — language ID.
 *   fn     (func(string) string)     — pure-Go formatting function.
 *
 * Returns:
 *   *BuiltinFormatter — ready-to-use formatter.
 *
 * Example:
 *   f := newBuiltinFormatter("basic", normaliseText)
 */
func newBuiltinFormatter(langID string, fn func(string) string) *BuiltinFormatter {
	return &BuiltinFormatter{langID: langID, fn: fn}
}

/** Language returns the language ID.
 *
 * Returns:
 *   string — language ID.
 */
func (f *BuiltinFormatter) Language() string { return f.langID }

/** Mode returns PipeFormatter (built-ins are pure in-process formatters).
 *
 * Returns:
 *   FormatterMode — always PipeFormatter.
 */
func (f *BuiltinFormatter) Mode() FormatterMode { return PipeFormatter }

/** Format applies the built-in formatting function to content.
 *
 * Parameters:
 *   content  (string) — source code to format.
 *   filePath (string) — unused; present for interface conformance.
 *
 * Returns:
 *   string — formatted content.
 *   error  — always nil.
 *
 * Example:
 *   out, _ := f.Format("var x: integer;  \n", "prog.pas")
 */
func (f *BuiltinFormatter) Format(content string, _ string) (string, error) {
	return f.fn(content), nil
}

/** Check reports whether content matches its formatted form.
 *
 * Parameters:
 *   content  (string) — source code to check.
 *   filePath (string) — unused.
 *
 * Returns:
 *   bool         — true when already formatted.
 *   []FormatIssue — empty on pass.
 */
func (f *BuiltinFormatter) Check(content string, _ string) (bool, []FormatIssue) {
	formatted := f.fn(content)
	if formatted == content {
		return true, nil
	}
	return false, []FormatIssue{{Line: 1, Column: 0, Message: "trailing whitespace or line endings need normalisation", Severity: "info"}}
}

// ─── Built-in formatting helpers ─────────────────────────────────────────────

// normaliseText removes trailing whitespace from each line, normalises CRLF to
// LF, and ensures the content ends with exactly one newline.  It never changes
// indentation or line content beyond trailing spaces and carriage returns.
func normaliseText(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	// Drop trailing blank lines, then append a single newline.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n") + "\n"
}

// ─── Registry wiring ─────────────────────────────────────────────────────────

/** SetFormatter registers a CodeFormatter for an already-registered language ID.
 *
 * Parameters:
 *   id (string)        — language identifier, e.g. "go".
 *   f  (CodeFormatter) — formatter to register.
 *
 * Example:
 *   r.SetFormatter("go", newPipeExtFormatter("go", "gofmt"))
 */
func (r *LanguageRegistry) SetFormatter(id string, f CodeFormatter) {
	r.formatters[id] = f
}

// initFormatters wires Phase-6 formatters into r.
// External formatters degrade gracefully when the tool is not installed.
// Built-in formatters are always available.
func initFormatters(r *LanguageRegistry) {
	// ── External pipe-mode formatters ─────────────────────────────────────
	r.SetFormatter("go", newPipeExtFormatter("go", "gofmt"))
	r.SetFormatter("c", newPipeExtFormatter("c", "clang-format", "-"))
	r.SetFormatter("cpp", newPipeExtFormatter("cpp", "clang-format", "-"))
	r.SetFormatter("python", newPipeExtFormatter("python", "black", "-"))
	r.SetFormatter("rust", newPipeExtFormatter("rust", "rustfmt"))
	// prettier reads from stdin when given --stdin-filepath; the placeholder is
	// replaced with the actual file path at runtime so prettier picks the right
	// parser.
	r.SetFormatter("javascript", newPipeExtFormatter("javascript",
		"prettier", "--stdin-filepath", "{{filepath}}"))
	r.SetFormatter("typescript", newPipeExtFormatter("typescript",
		"prettier", "--stdin-filepath", "{{filepath}}"))

	// ── Built-in formatters (no external tools required) ──────────────────
	r.SetFormatter("pascal", newBuiltinFormatter("pascal", normaliseText))
	r.SetFormatter("oberon", newBuiltinFormatter("oberon", normaliseText))
	r.SetFormatter("basic", newBuiltinFormatter("basic", normaliseText))
}
