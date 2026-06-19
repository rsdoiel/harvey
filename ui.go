package harvey

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

/** SelectItem is one choosable option in a numbered select list.
 *
 * Fields:
 *   Value  (string) — the string returned when this item is chosen; used
 *                     as the completion candidate in tab completion.
 *   Label  (string) — the display text shown in the list; may be longer
 *                     than Value (e.g. "store-name  (3 documents)").
 *   Active (bool)   — when true the item is rendered with a "→" prefix
 *                     to indicate the currently selected/active item.
 *
 * Example:
 *   items := []SelectItem{
 *       {Value: "harvey", Label: "harvey", Active: true},
 *       {Value: "docs",   Label: "docs"},
 *   }
 */
type SelectItem struct {
	Value  string
	Label  string
	Active bool
}

/** SelectFrom presents a numbered list of items and returns the Value of
 * the chosen item. If items contains exactly one entry it is returned
 * without displaying the list or prompting the user. Empty input, "q",
 * or "0" cancel the selection and return ("", nil). Non-numeric input
 * that is non-empty is returned as-is so callers can accept typed values
 * (e.g. file paths) in addition to numbered selections.
 *
 * Parameters:
 *   items  ([]SelectItem) — options to present; must not be nil.
 *   prompt (string)       — text shown after the list, e.g. "Select [1-3]: ".
 *   in     (io.Reader)    — input source; typically a.In.
 *   out    (io.Writer)    — output destination.
 *
 * Returns:
 *   string — the Value of the chosen item, the raw input if non-numeric,
 *            or "" on cancellation.
 *   error  — only non-nil on unexpected I/O failure.
 *
 * Example:
 *   stores := []SelectItem{
 *       {Value: "harvey", Label: "harvey", Active: true},
 *       {Value: "docs",   Label: "docs"},
 *   }
 *   name, err := SelectFrom(stores, "Select store: ", a.In, out)
 */
func SelectFrom(items []SelectItem, prompt string, in io.Reader, out io.Writer) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	if len(items) == 1 {
		return items[0].Value, nil
	}

	renderSelectList(items, out)
	fmt.Fprint(out, "  "+prompt)

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", nil
	}
	line = strings.TrimSpace(line)

	idx, raw := parseSelectInput(line, len(items))
	if idx >= 1 && idx <= len(items) {
		return items[idx-1].Value, nil
	}
	if raw != "" {
		return raw, nil
	}
	return "", nil
}

/** SelectFromStrings is a convenience wrapper around SelectFrom where each
 * item's Label and Value are identical. Use this when the display name is
 * the same string that the command handler needs (e.g. store names, type
 * names). For richer display (e.g. "ID — description") build []SelectItem
 * directly and call SelectFrom.
 *
 * Parameters:
 *   items  ([]string)  — options to present.
 *   prompt (string)    — text shown after the list.
 *   in     (io.Reader) — input source.
 *   out    (io.Writer) — output destination.
 *
 * Returns:
 *   string — the chosen item string, or "" on cancellation.
 *   error  — only non-nil on unexpected I/O failure.
 *
 * Example:
 *   types := []string{"tool_use", "workflow", "user_preference"}
 *   chosen, err := SelectFromStrings(types, "Select type: ", a.In, out)
 */
func SelectFromStrings(items []string, prompt string, in io.Reader, out io.Writer) (string, error) {
	si := make([]SelectItem, len(items))
	for i, s := range items {
		si[i] = SelectItem{Value: s, Label: s}
	}
	return SelectFrom(si, prompt, in, out)
}

// renderSelectList prints the numbered list to out. Active items are
// prefixed with "→"; inactive items with equivalent whitespace so columns
// align. Format:
//
//	→  [1] active-item
//	   [2] other-item
func renderSelectList(items []SelectItem, out io.Writer) {
	for i, item := range items {
		marker := "   "
		if item.Active {
			marker = "→  "
		}
		fmt.Fprintf(out, "  %s[%d] %s\n", marker, i+1, item.Label)
	}
	fmt.Fprintln(out)
}

// parseSelectInput parses the user's raw input line. If the input is a
// number between 1 and n it returns (number, ""). If the input is empty,
// "q", or "0" it returns (0, ""). Otherwise it returns (0, trimmedInput)
// so the caller can treat the input as a typed value (e.g. a file path).
func parseSelectInput(line string, n int) (idx int, raw string) {
	line = strings.TrimSpace(line)
	if line == "" || line == "q" || line == "0" {
		return 0, ""
	}
	if i, err := strconv.Atoi(line); err == nil {
		if i >= 1 && i <= n {
			return i, ""
		}
		return 0, "" // out-of-range number → cancel
	}
	return 0, line
}
