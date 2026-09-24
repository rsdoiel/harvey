package harvey

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// findEditor returns the user's preferred editor command: $EDITOR if it is set
// to something other than whitespace, else micro, nano or vi. The result may
// carry arguments ("code --wait"); editorCommand splits them.
func findEditor() string {
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	for _, candidate := range []string{"micro", "nano", "vi"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return "vi"
}

/** editTextInEditor writes text to a temp file, opens the user's editor on it
 * (findEditor: $EDITOR, else micro, nano or vi), waits for it to close, and
 * returns what the file then contains. It is the round trip that editInEditor
 * did for a *MemoryDoc only, extracted (knowledge-learning-mode-plan.md, H3) so
 * learning mode can edit a multi-paragraph summary the same way, rather than
 * at a one-line prompt.
 *
 * The temp file is always removed, including when the editor fails. On
 * failure the returned text is empty and the error names the editor, so a
 * failed edit cannot be mistaken for the user having emptied the text.
 *
 * Parameters:
 *   text (string) — the text to edit.
 *   ext  (string) — the temp file's extension including the dot (".md",
 *                   ".fountain"), so the editor picks the right syntax
 *                   highlighting.
 *
 * Returns:
 *   string — the edited text, byte for byte as the editor saved it.
 *   error  — if the temp file cannot be written or read, or the editor
 *            exits non-zero or cannot be started.
 *
 * Example:
 *   summary, err := editTextInEditor(draft, ".md")
 */
func editTextInEditor(text, ext string) (string, error) {
	tmp, err := os.CreateTemp("", "harvey-edit-*"+ext)
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()

	editor := findEditor()
	cmd := editorCommand(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %q: %w", editor, err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}
	return string(edited), nil
}

/** editorCommand builds the command that opens path in editor. editor is the
 * user's $EDITOR string and may carry arguments, as `code --wait`, `subl -w`
 * and `emacsclient -t` do: it is split on whitespace, the first field is the
 * program, the remaining fields come first among the arguments, and path is
 * last. exec.Command takes a single program name, so passing the whole string
 * failed with "executable file not found" for any editor set with arguments.
 *
 * The split is plain whitespace, as `strings.Fields` does it: there is no
 * quote handling, so a program path that contains a space, or an argument
 * that does, is not supported. An editor string with no fields falls back to
 * vi.
 *
 * Parameters:
 *   editor (string) — the editor command, program then optional arguments.
 *   path   (string) — the file to open.
 *
 * Returns:
 *   *exec.Cmd — the command, not yet started, with no stdio attached.
 *
 * Example:
 *   cmd := editorCommand("code --wait", "/tmp/summary.md")
 *   // cmd.Args == ["code", "--wait", "/tmp/summary.md"]
 */
func editorCommand(editor, path string) *exec.Cmd {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	args := make([]string, 0, len(fields))
	args = append(args, fields[1:]...)
	args = append(args, path)
	return exec.Command(fields[0], args...)
}
