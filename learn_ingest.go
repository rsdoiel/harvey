package harvey

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	knowledge "github.com/rsdoiel/knowledge"
)

// learnDefaultMinWords is the smallest session `/kb learn ingest` offers by
// default. RSDOIEL chose a word gate over the miner's 10-turn gate on
// 2026-09-24 because of real data: 31 of 34 recorded sessions are under 200
// words (median 24), none reaches 10 chat turns, and the 32 hand-offs are all
// substantial. Hand-offs are always offered whatever their size.
const learnDefaultMinWords = 200

// chatTurnHeading matches the scene heading Recorder.RecordTurnWithStats
// writes for one chat turn.
var chatTurnHeading = regexp.MustCompile(`(?m)^INT\. HARVEY AND .+ TALKING `)

/** LearnCandidate is one file `/kb learn ingest` can offer to the knowledge
 * base as a document: a recorded session or a hand-off note.
 *
 * Fields:
 *   Path       (string)    — absolute path of the file.
 *   StoredPath (string)    — the path form passed to IngestDocument: relative
 *                            to the working directory, with forward slashes,
 *                            which is what `kb document ingest FILE` stores
 *                            when run from there. DocumentByPath is an exact
 *                            match, so this form is a contract across tools.
 *   Kind       (string)    — "hand-off" or "session".
 *   Words      (int)       — whitespace-separated words in the file.
 *   Turns      (int)       — chat turns recorded in it, for information; it
 *                            is not used to decide anything.
 *   ModTime    (time.Time) — the file's modification time, used to list the
 *                            newest first.
 *
 * Example:
 *   cs, _ := learnCandidates(kb, ws.Root, 200)
 *   fmt.Println(cs[0].Kind, cs[0].StoredPath)
 */
type LearnCandidate struct {
	Path       string
	StoredPath string
	Kind       string
	Words      int
	Turns      int
	ModTime    time.Time
}

/** learnCandidates lists the hand-off notes and recorded sessions under root
 * (agents/hand-off and agents/sessions) that are not yet in the knowledge
 * base, newest first. Only .spmd and .fountain files count. A hand-off is
 * always a candidate; a session is a candidate only if it has at least
 * minWords words. A file already stored as a document, under either its
 * working-directory-relative path or its absolute path, is left out, so a
 * second consumer that stored the absolute form cannot cause a duplicate. A
 * directory that does not exist is not an error.
 *
 * Parameters:
 *   kb       (*knowledge.KnowledgeBase) — the open knowledge base.
 *   root     (string)                   — the workspace root.
 *   minWords (int)                      — the session word gate; 0 offers every session.
 *
 * Returns:
 *   []LearnCandidate — the candidates, newest first.
 *   error            — on database failure or an unreadable directory.
 *
 * Example:
 *   cs, err := learnCandidates(a.KB, a.Workspace.Root, learnDefaultMinWords)
 */
func learnCandidates(kb *knowledge.KnowledgeBase, root string, minWords int) ([]LearnCandidate, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}

	var out []LearnCandidate
	for _, src := range []struct{ dir, kind string }{
		{filepath.Join(root, harveySubdir, "hand-off"), "hand-off"},
		{filepath.Join(root, harveySubdir, "sessions"), "session"},
	} {
		entries, err := os.ReadDir(src.dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			ext := filepath.Ext(e.Name())
			if e.IsDir() || (ext != ".spmd" && ext != ".fountain") {
				continue
			}
			abs := filepath.Join(src.dir, e.Name())
			data, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			words := len(strings.Fields(string(data)))
			if src.kind == "session" && words < minWords {
				continue
			}
			stored := storedPathFor(abs, cwd)
			done, err := alreadyIngested(kb, stored, abs)
			if err != nil {
				return nil, err
			}
			if done {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, LearnCandidate{
				Path: abs, StoredPath: stored, Kind: src.kind, Words: words,
				Turns: len(chatTurnHeading.FindAllString(string(data), -1)), ModTime: info.ModTime(),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Path > out[j].Path
	})
	return out, nil
}

// storedPathFor returns abs relative to cwd with forward slashes, the form
// `kb document ingest` stores when run from cwd, or abs itself if no relative
// form exists.
func storedPathFor(abs, cwd string) string {
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

/** parseSelection turns a user's answer into 1-based item numbers. It accepts
 * "all", an empty answer or "none" (no items), a single number, a comma list,
 * and inclusive ranges: "1,3-5". Numbers are returned ascending with
 * duplicates removed. Anything it cannot mean, such as 0, a number past n, a
 * reversed range or a non-number, is an error naming the bad part, rather
 * than a guess: this chooses what gets written to the knowledge base.
 *
 * Parameters:
 *   in (string) — the answer as typed.
 *   n  (int)    — how many items were listed.
 *
 * Returns:
 *   []int — the chosen numbers, ascending; nil for none.
 *   error — for an answer that cannot be understood.
 *
 * Example:
 *   picked, err := parseSelection("1, 3-4", 5) // [1 3 4]
 */
func parseSelection(in string, n int) ([]int, error) {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" || in == "none" {
		return nil, nil
	}
	if in == "all" {
		all := make([]int, n)
		for i := range all {
			all[i] = i + 1
		}
		return all, nil
	}
	chosen := map[int]bool{}
	for _, part := range strings.Split(in, ",") {
		part = strings.TrimSpace(part)
		lo, hi := 0, 0
		if a, b, isRange := strings.Cut(part, "-"); isRange {
			var err1, err2 error
			lo, err1 = strconv.Atoi(strings.TrimSpace(a))
			hi, err2 = strconv.Atoi(strings.TrimSpace(b))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("cannot read %q as a range", part)
			}
		} else {
			v, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("cannot read %q as a number", part)
			}
			lo, hi = v, v
		}
		if lo < 1 || hi > n || lo > hi {
			return nil, fmt.Errorf("%q is outside 1-%d", part, n)
		}
		for i := lo; i <= hi; i++ {
			chosen[i] = true
		}
	}
	out := make([]int, 0, len(chosen))
	for i := range chosen {
		out = append(out, i)
	}
	sort.Ints(out)
	return out, nil
}

/** kbLearn implements `/kb learn`, the knowledge-base learning mode
 * (knowledge-learning-mode-design.md). Its first subcommand, ingest, offers
 * harvey's own session recordings and hand-off notes to the knowledge base as
 * documents, so the review queue has something real to work on.
 *
 * Usage:
 *   /kb learn ingest [--min-words N] [--all] [--dry-run]
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with an open KB and a workspace.
 *   args ([]string)  — the arguments after "learn".
 *   out  (io.Writer) — where output is written.
 *
 * Returns:
 *   error — only on a failure listing candidates; per-file failures are
 *           reported in the output and do not stop the others.
 *
 * Example:
 *   err := kbLearn(a, []string{"ingest", "--dry-run"}, os.Stdout)
 */
func kbLearn(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		// Bare /kb learn: draft what is missing (default limit, configured model),
		// then walk the drafts. Both halves ask before anything is trusted.
		if err := kbLearnDraft(a, nil, out); err != nil {
			return err
		}
		return kbLearnReview(a, nil, out)
	}
	switch strings.ToLower(args[0]) {
	case "ingest":
		return kbLearnIngest(a, args[1:], out)
	case "draft":
		return kbLearnDraft(a, args[1:], out)
	case "review":
		return kbLearnReview(a, args[1:], out)
	case "concepts":
		return kbLearnConcepts(a, args[1:], out)
	}
	fmt.Fprintln(out, learnUsage)
	return nil
}

func kbLearnIngest(a *Agent, args []string, out io.Writer) error {
	const usage = "Usage: /kb learn ingest [--min-words N] [--all] [--dry-run]"
	minWords, all, dryRun := learnDefaultMinWords, false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			all = true
		case "--dry-run":
			dryRun = true
		case "--min-words":
			if i+1 >= len(args) {
				fmt.Fprintln(out, "--min-words needs a number.", usage)
				return nil
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 0 {
				fmt.Fprintf(out, "--min-words %q is not a number of words. %s\n", args[i+1], usage)
				return nil
			}
			minWords = n
			i++
		default:
			fmt.Fprintf(out, "Unknown option %q. %s\n", args[i], usage)
			return nil
		}
	}
	projectID := a.Config.Memory.CurrentProjectID
	if !dryRun && projectID == 0 {
		fmt.Fprintln(out, "No current project. Pick one first with /kb project use ID (see /kb project list).")
		return nil
	}

	cands, err := learnCandidates(a.KB, a.Workspace.Root, minWords)
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		fmt.Fprintf(out, "Nothing to ingest: every hand-off, and every session of at least %d words, is already in the knowledge base.\n", minWords)
		return nil
	}

	fmt.Fprintf(out, "\nCandidates for the knowledge base (%d, newest first):\n", len(cands))
	for i, c := range cands {
		turns := ""
		if c.Turns > 0 {
			turns = fmt.Sprintf("  %d turn(s)", c.Turns)
		}
		fmt.Fprintf(out, "  %3d. %-8s %6d words%s  %s\n", i+1, c.Kind, c.Words, turns, c.StoredPath)
	}
	if dryRun {
		fmt.Fprintln(out, "\n(dry run: nothing was ingested)")
		return nil
	}

	picked := make([]int, 0, len(cands))
	if all {
		for i := range cands {
			picked = append(picked, i+1)
		}
	} else {
		fmt.Fprint(out, "\n  Ingest which? [numbers like 1,3-5 | all | Enter to cancel]: ")
		line, _ := bufio.NewReader(a.In).ReadString('\n')
		picked, err = parseSelection(line, len(cands))
		if err != nil {
			fmt.Fprintf(out, "\nNothing ingested: %v.\n", err)
			return nil
		}
	}
	if len(picked) == 0 {
		fmt.Fprintln(out, "\nNothing ingested.")
		return nil
	}

	fmt.Fprintln(out)
	var added, skipped, failed int
	for _, n := range picked {
		c := cands[n-1]
		res, err := a.KB.IngestDocument(projectID, c.StoredPath, knowledge.DocumentIngestOptions{})
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(out, "  failed  %s: %v\n", c.StoredPath, err)
		case res.Action == "skipped":
			skipped++
			fmt.Fprintf(out, "  skipped %s (already up to date)\n", c.StoredPath)
		default:
			added++
			fmt.Fprintf(out, "  %-7s %s (%d section(s))\n", res.Action, c.StoredPath, res.SectionsAdded)
		}
	}
	fmt.Fprintf(out, "\nIngested %d, skipped %d, failed %d. New sections start unsummarized in the review queue.\n", added, skipped, failed)
	return nil
}

// alreadyIngested reports whether the knowledge base already holds a document
// stored under the working-directory-relative path or the absolute one.
func alreadyIngested(kb *knowledge.KnowledgeBase, stored, abs string) (bool, error) {
	for _, p := range []string{stored, abs} {
		d, err := kb.DocumentByPath(p)
		if err != nil {
			return false, err
		}
		if d != nil {
			return true, nil
		}
	}
	return false, nil
}
