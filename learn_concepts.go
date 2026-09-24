package harvey

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	knowledge "github.com/rsdoiel/knowledge"
)

// H6: /kb learn concepts, the concept-curation pass. The library does the
// reading (SuggestConcepts) and the text edits (TagDocumentText,
// FuzzyTagDocumentText); this file owns the human steps and every write, so
// the permission check, the audit log and the both-or-neither rule live where
// harvey's other file writes live.

// learnConceptsDefaultLimit is how many candidates one run lists. It matches
// `kb concept suggest`'s habit of showing a screenful, not the whole corpus;
// --limit 0 lists them all.
const learnConceptsDefaultLimit = 20

// diffMaxEdits bounds the Myers search in lineDiff. Tagging touches a few
// dozen lines at most; a document that differs by more than this is not a
// tagging preview any more, and an unbounded search would hold O(D^2) memory.
const diffMaxEdits = 500

var (
	// errWriteDenied means the permissions: table forbids writing the path.
	errWriteDenied = errors.New("write permission denied")
	// errOutsideWorkspace means the document lives outside the workspace root,
	// where harvey does not write.
	errOutsideWorkspace = errors.New("the document is outside the workspace")
)

type lineEdit struct {
	op   byte // '-' (removed from before) or '+' (added in after)
	line int  // 1-based line number in before ('-') or after ('+')
	text string
}

// splitLines splits text into lines without the trailing empty element a
// final newline would leave.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// myersEdits returns the shortest edit script from a to b, or false when it
// needs more than diffMaxEdits edits.
func myersEdits(a, b []string) ([]lineEdit, bool) {
	n, m := len(a), len(b)
	limit := n + m
	if limit == 0 {
		return nil, true
	}
	if limit > diffMaxEdits {
		limit = diffMaxEdits
	}
	off := limit + 1
	v := make([]int, 2*limit+3)
	var trace [][]int // trace[d] is v as it stood before round d
	found := -1
outer:
	for d := 0; d <= limit; d++ {
		snap := make([]int, len(v))
		copy(snap, v)
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				found = d
				break outer
			}
		}
	}
	if found < 0 {
		return nil, false
	}
	var rev []lineEdit
	x, y := n, m
	for d := found; d > 0; d-- {
		vv := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && vv[off+k-1] < vv[off+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vv[off+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
		}
		if x == prevX {
			rev = append(rev, lineEdit{'+', prevY + 1, b[prevY]})
		} else {
			rev = append(rev, lineEdit{'-', prevX + 1, a[prevX]})
		}
		x, y = prevX, prevY
	}
	edits := make([]lineEdit, len(rev))
	for i, e := range rev {
		edits[len(rev)-1-i] = e
	}
	return edits, true
}

// A changed line longer than diffLongLine is shown as an excerpt: diffContext
// characters either side of the change when its other half can be found, else
// its first diffLongLine characters. Found in the live run on the real
// DECISIONS.md, where a paragraph is one line of about a thousand characters
// and a whole-line -/+ pair hides a two-word change.
const (
	diffLongLine = 160
	diffContext  = 40
)

// alignStart moves i forward to a rune boundary of s; alignEnd moves it back.
func alignStart(s string, i int) int {
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
}

func alignEnd(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// commonAffix returns how many leading and trailing bytes a and b share, with
// the suffix not overlapping the prefix.
func commonAffix(a, b string) (prefix, suffix int) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for prefix < n && a[prefix] == b[prefix] {
		prefix++
	}
	for suffix < n-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return prefix, suffix
}

// truncateLine keeps the first diffLongLine bytes of a long line.
func truncateLine(s string) string {
	if len(s) <= diffLongLine {
		return s
	}
	return s[:alignEnd(s, diffLongLine)] + "…"
}

// excerptEdits returns the text to show for each edit. A '-' and a '+' within
// three entries of each other that share nearly all their text are a modified
// line and are excerpted around the change, both to the same window; any other
// long line is truncated.
func excerptEdits(edits []lineEdit) []string {
	shown := make([]string, len(edits))
	done := make([]bool, len(edits))
	for i, e := range edits {
		if done[i] {
			continue
		}
		for j := i + 1; j < len(edits) && j <= i+3; j++ {
			o := edits[j]
			if done[j] || o.op == e.op {
				continue
			}
			minus, plus := e.text, o.text
			if e.op == '+' {
				minus, plus = plus, minus
			}
			if len(minus) <= diffLongLine && len(plus) <= diffLongLine {
				continue
			}
			pre, suf := commonAffix(minus, plus)
			shorter := len(minus)
			if len(plus) < shorter {
				shorter = len(plus)
			}
			if shorter == 0 || float64(pre+suf) < 0.8*float64(shorter) {
				continue
			}
			ex := func(t string) string {
				from := alignStart(t, max(0, pre-diffContext))
				to := alignEnd(t, min(len(t), len(t)-suf+diffContext))
				out := t[from:to]
				if from > 0 {
					out = "…" + out
				}
				if to < len(t) {
					out += "…"
				}
				return out
			}
			shown[i], shown[j] = ex(e.text), ex(o.text)
			done[i], done[j] = true, true
			break
		}
	}
	for i, e := range edits {
		if !done[i] {
			shown[i] = truncateLine(e.text)
		}
	}
	return shown
}

/** lineDiff renders the lines that differ between two texts, and only those:
 * "-N: text" for a line removed from before, "+N: text" for a line added in
 * after, N being its line number in that text. Tagging changes a handful of
 * lines in a long document, so a changed-lines view is what a human can check
 * before saying yes. Identical texts give "".
 *
 * Parameters:
 *   before (string) — the text as it is on disk.
 *   after  (string) — the text that would be written.
 *
 * Returns:
 *   string — the changed lines, one per line, each indented two spaces; ""
 *            when the texts are equal.
 *
 * Example:
 *   fmt.Print(lineDiff("a\nb\n", "a\nb [[x]]\n"))
 *   //   -2: b
 *   //   +2: b [[x]]
 */
func lineDiff(before, after string) string {
	if before == after {
		return ""
	}
	a, b := splitLines(before), splitLines(after)
	edits, ok := myersEdits(a, b)
	if !ok {
		return fmt.Sprintf("  (more than %d lines differ; too many to list)\n", diffMaxEdits)
	}
	shown := excerptEdits(edits)
	var sb strings.Builder
	for i, e := range edits {
		fmt.Fprintf(&sb, "  %c%d: %s\n", e.op, e.line, shown[i])
	}
	return sb.String()
}

/** planConceptTagging works out what tagging text for the accepted concepts
 * would change, without writing anything. It links each accepted concept the
 * text mentions more than once (the density threshold), then footnotes
 * near-miss spellings of the accepted concepts (plurals, typos) that are
 * within the fuzzy thresholds. Other known concepts are left alone: the human
 * picked these ones. It reads the knowledge base but never writes it.
 *
 * Parameters:
 *   kb       (*knowledge.KnowledgeBase) — the open knowledge base.
 *   text     (string)                   — the document as it is on disk.
 *   accepted ([]string)                 — the concepts the human picked.
 *
 * Returns:
 *   string                         — the text with the links and footnotes in.
 *   []string                       — concepts linked.
 *   []knowledge.FuzzyTagInsertion  — footnotes inserted.
 *   error                          — on database failure.
 *
 * Example:
 *   next, linked, notes, err := planConceptTagging(kb, text, []string{"chunking"})
 */
func planConceptTagging(kb *knowledge.KnowledgeBase, text string, accepted []string) (string, []string, []knowledge.FuzzyTagInsertion, error) {
	if len(accepted) == 0 {
		return text, nil, nil, nil
	}
	eligible, err := kb.EligibleTagConcepts(text, accepted)
	if err != nil {
		return text, nil, nil, err
	}
	next, linked := knowledge.TagDocumentText(text, eligible)

	matches, err := kb.FuzzyMatchConceptNames(next)
	if err != nil {
		return text, nil, nil, err
	}
	mine := map[string]bool{}
	for _, name := range accepted {
		mine[strings.ToLower(name)] = true
	}
	var picked []knowledge.FuzzyConceptMatch
	for _, m := range matches {
		if mine[strings.ToLower(m.Concept)] {
			picked = append(picked, m)
		}
	}
	next, notes := knowledge.FuzzyTagDocumentText(next, knowledge.FuzzyEligible(picked, nil, next))
	return next, linked, notes, nil
}

// resolveDocPath turns a stored document path into an absolute path and the
// workspace-relative form permissions are checked against. The stored form is
// absolute or relative to the working directory, which for harvey is the
// workspace root.
func resolveDocPath(a *Agent, stored string) (abs, rel string, err error) {
	abs = stored
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(a.Workspace.Root, stored)
	}
	abs = filepath.Clean(abs)
	rel, err = filepath.Rel(a.Workspace.Root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs, "", errOutsideWorkspace
	}
	return abs, filepath.ToSlash(rel), nil
}

// checkTagWritable is the gate in front of every curation write: inside the
// workspace and allowed by the permissions: table. It returns the relative
// path on success. It is asked before a preview is offered, so the user is
// never invited to say yes to a write that cannot happen.
func checkTagWritable(a *Agent, stored string) (abs, rel string, err error) {
	abs, rel, err = resolveDocPath(a, stored)
	if err != nil {
		return abs, rel, err
	}
	if !a.CheckWritePermission(rel) {
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileWrite, rel, StatusDenied)
		}
		return abs, rel, errWriteDenied
	}
	return abs, rel, nil
}

/** applyConceptTagWrite writes next over a document and re-ingests it, both
 * or neither: if the re-ingest fails, the original bytes are put back so the
 * file and the knowledge base agree. It checks the workspace boundary and the
 * write permission itself, so it is safe to call without the caller having
 * asked. The write is audited and recorded like harvey's other file writes.
 *
 * Parameters:
 *   a        (*Agent)                 — the running agent.
 *   d        (knowledge.Document)     — the document row; d.Path is the stored form to re-ingest.
 *   abs      (string)                 — the document's absolute path.
 *   raw      ([]byte)                 — the file's current bytes, restored on failure.
 *   next     (string)                 — the new content.
 *   reingest (func(string) (...))     — re-ingests the stored path; injected so a failure can be tested.
 *
 * Returns:
 *   knowledge.DocumentIngestResult — what the re-ingest did.
 *   error                          — errWriteDenied, errOutsideWorkspace, or a
 *                                    write/ingest failure (after restoring).
 *
 * Example:
 *   res, err := applyConceptTagWrite(a, d, abs, raw, next, func(p string) (knowledge.DocumentIngestResult, error) {
 *       return a.KB.IngestDocument(pid, p, knowledge.DocumentIngestOptions{})
 *   })
 */
func applyConceptTagWrite(a *Agent, d knowledge.Document, abs string, raw []byte, next string, reingest func(string) (knowledge.DocumentIngestResult, error)) (knowledge.DocumentIngestResult, error) {
	_, rel, err := checkTagWritable(a, abs)
	if err != nil {
		return knowledge.DocumentIngestResult{}, err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	if err := a.Workspace.WriteFile(rel, []byte(next), mode); err != nil {
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileWrite, rel, StatusError)
		}
		a.logAction("write", rel, actionYes, "error: "+err.Error())
		return knowledge.DocumentIngestResult{}, err
	}
	res, err := reingest(d.Path)
	if err != nil {
		if rerr := a.Workspace.WriteFile(rel, raw, mode); rerr != nil {
			err = fmt.Errorf("%w; and restoring %s failed too: %v", err, rel, rerr)
		}
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileWrite, rel, StatusError)
		}
		a.logAction("write", rel, actionYes, "error: "+err.Error())
		return knowledge.DocumentIngestResult{}, err
	}
	if a.AuditBuffer != nil {
		a.AuditBuffer.Log(ActionFileWrite, rel, StatusSuccess)
	}
	a.logAction("write", rel, actionYes, "ok")
	return res, nil
}

func kbLearnConcepts(a *Agent, args []string, out io.Writer) error {
	const usage = "Usage: /kb learn concepts [--limit N]"
	limit := learnConceptsDefaultLimit
	for i := 0; i < len(args); i++ {
		if args[i] != "--limit" {
			fmt.Fprintf(out, "Unknown option %q. %s\n", args[i], usage)
			return nil
		}
		if i+1 >= len(args) {
			fmt.Fprintln(out, "--limit needs a number.", usage)
			return nil
		}
		n, err := strconv.Atoi(args[i+1])
		if err != nil || n < 0 {
			fmt.Fprintf(out, "--limit %q is not a number of candidates. %s\n", args[i+1], usage)
			return nil
		}
		limit = n
		i++
	}
	projectID := a.Config.Memory.CurrentProjectID
	if projectID == 0 {
		fmt.Fprintln(out, "No current project. Pick one first with /kb project use ID (see /kb project list).")
		return nil
	}
	projects, err := a.KB.Projects()
	if err != nil {
		return err
	}
	projectName := ""
	for _, p := range projects {
		if p.ID == projectID {
			projectName = p.Name
		}
	}
	if projectName == "" {
		fmt.Fprintf(out, "The current project (id %d) no longer exists. Pick one with /kb project use ID.\n", projectID)
		return nil
	}

	sugg, err := a.KB.SuggestConcepts(projectName, limit)
	if err != nil {
		return err
	}
	if len(sugg.Candidates) == 0 {
		fmt.Fprintf(out, "No candidate concepts for %q: nothing distinctive and not already a concept. Ingest more documents with /kb learn ingest.\n", projectName)
		return nil
	}

	fmt.Fprintf(out, "\nCandidate concepts for %q, most distinctive first:\n", projectName)
	for i, c := range sugg.Candidates {
		also := ""
		if len(c.Variants) > 0 {
			also = "  (also: " + strings.Join(c.Variants, ", ") + ")"
		}
		fmt.Fprintf(out, "  %2d. %-22s %d mention(s) in %d place(s)%s\n", i+1, c.Term, c.Occurrences, c.Items, also)
	}
	if len(sugg.NearExisting) > 0 {
		fmt.Fprintln(out, "\nLeft out because they are close spellings of a concept you already have (the fuzzy pass covers these):")
		for i, n := range sugg.NearExisting {
			if i == 10 {
				fmt.Fprintf(out, "  … and %d more\n", len(sugg.NearExisting)-10)
				break
			}
			fmt.Fprintf(out, "  %s ≈ %s\n", n.Token, n.Concept)
		}
	}

	reader := bufio.NewReaderSize(a.In, 1)
	fmt.Fprint(out, "\nWhich should become concepts? Numbers, ranges like 1-3, all, or none: ")
	line, rerr := reader.ReadString('\n')
	if rerr != nil && strings.TrimSpace(line) == "" {
		fmt.Fprintln(out, "\nInput ended; nothing was changed.")
		return nil
	}
	picked, err := parseSelection(line, len(sugg.Candidates))
	if err != nil {
		fmt.Fprintf(out, "Could not read %q: %v. Nothing was changed.\n", strings.TrimSpace(line), err)
		return nil
	}
	if len(picked) == 0 {
		fmt.Fprintln(out, "Nothing chosen; nothing was changed.")
		return nil
	}
	var accepted []string
	for _, n := range picked {
		term := sugg.Candidates[n-1].Term
		if _, err := a.KB.AddConcept(term, ""); err != nil {
			return fmt.Errorf("adding concept %q: %w", term, err)
		}
		accepted = append(accepted, term)
	}
	fmt.Fprintf(out, "Added %d concept(s): %s\n", len(accepted), strings.Join(accepted, ", "))

	docs, err := a.KB.Documents(projectID)
	if err != nil {
		return err
	}
	var tagged, declined, refused, fountain, stale int
	applyAll, quit := false, false
	for _, d := range docs {
		if quit {
			break
		}
		if d.Format == "fountain" {
			fountain++
			continue
		}
		abs, rel, err := resolveDocPath(a, d.Path)
		if err != nil {
			fmt.Fprintf(out, "  %s: %v; not touched.\n", d.Path, err)
			refused++
			continue
		}
		if !a.CheckReadPermission(rel) {
			fmt.Fprintf(out, "  %s: read permission denied; not touched.\n", rel)
			refused++
			continue
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			fmt.Fprintf(out, "  %s: could not read it (%v); not touched.\n", rel, err)
			refused++
			continue
		}
		next, linked, notes, err := planConceptTagging(a.KB, string(raw), accepted)
		if err != nil {
			return err
		}
		if next == string(raw) {
			continue
		}
		if _, _, err := checkTagWritable(a, d.Path); err != nil {
			fmt.Fprintf(out, "  %s: %v; not touched.\n", rel, err)
			refused++
			continue
		}

		fmt.Fprintf(out, "\n%s: %d link(s), %d footnote(s)\n%s", rel, len(linked), len(notes), lineDiff(string(raw), next))
		if !applyAll {
			choice, ended := promptActionEOF(reader, out, "Write: "+rel, "")
			if ended {
				fmt.Fprintln(out, "\nInput ended; nothing more was changed.")
				quit = true
				continue
			}
			switch choice {
			case actionNo:
				declined++
				continue
			case actionQuit:
				quit = true
				continue
			case actionAll:
				applyAll = true
			}
		}
		pid := projectID
		res, err := applyConceptTagWrite(a, d, abs, raw, next, func(p string) (knowledge.DocumentIngestResult, error) {
			return a.KB.IngestDocument(pid, p, knowledge.DocumentIngestOptions{})
		})
		if err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v; the file was left as it was.\n", rel, err)
			refused++
			continue
		}
		fmt.Fprintf(out, "  ✓ wrote %s\n", rel)
		tagged++
		stale += res.SectionsStale
	}

	fmt.Fprintf(out, "\nTagged %d file(s)", tagged)
	if declined > 0 {
		fmt.Fprintf(out, ", declined %d", declined)
	}
	if refused > 0 {
		fmt.Fprintf(out, ", could not write %d", refused)
	}
	fmt.Fprintln(out, ".")
	if fountain > 0 {
		fmt.Fprintf(out, "Skipped %d Fountain document(s): sessions and hand-offs are records, and [[...]] in them reads as a note.\n", fountain)
	}
	if stale > 0 {
		fmt.Fprintf(out, "Tagging changed source text, so %d section summar(ies) are now marked stale; see /kb learn review.\n", stale)
	}
	return nil
}
