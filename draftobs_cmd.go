package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

// D6 of the observation-drafting plan (design/observation-drafting-design.md):
// the /kb draft command, which ties the reader, the passes, the model, the
// duplicate check and the writer together.

/** draftDefaultLimit is how many drafts one /kb draft run writes unless
 * --limit says otherwise. --limit 0 writes everything the model proposed.
 */
const draftDefaultLimit = 5

const draftUsage = "usage: /kb draft [--project NAME] [--limit N] [@route] [SESSION]"

// draftParams is a parsed /kb draft line.
type draftParams struct {
	project string
	limit   int
	mention string
	session string
}

// parseDraftArgs reads the arguments of /kb draft. Every mistake is a usage
// error, found before the model or kb is asked anything.
func parseDraftArgs(args []string) (draftParams, error) {
	p := draftParams{limit: draftDefaultLimit}
	mention, rest := splitMention(args)
	p.mention = mention
	var positional []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "--project":
			if i+1 >= len(rest) || strings.HasPrefix(rest[i+1], "--") {
				return p, Usagef("--project needs a name. %s", draftUsage)
			}
			p.project = rest[i+1]
			i++
		case strings.HasPrefix(arg, "--project="):
			p.project = strings.TrimPrefix(arg, "--project=")
			if p.project == "" {
				return p, Usagef("--project needs a name. %s", draftUsage)
			}
		case arg == "--limit":
			if i+1 >= len(rest) {
				return p, Usagef("--limit needs a number. %s", draftUsage)
			}
			n, err := strconv.Atoi(rest[i+1])
			if err != nil || n < 0 {
				return p, Usagef("--limit %q is not a number of drafts (0 means no limit). %s", rest[i+1], draftUsage)
			}
			p.limit = n
			i++
		case strings.HasPrefix(arg, "-"):
			return p, Usagef("unknown option %q. %s", arg, draftUsage)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) > 1 {
		return p, Usagef("one session at most, got %d. %s", len(positional), draftUsage)
	}
	if len(positional) == 1 {
		p.session = positional[0]
	}
	return p, nil
}

/** resolveDraftSession finds the session log to draft from: the open session
 * when none is named, else a session stem or a path (relative to the
 * workspace) to a .jsonl file.
 *
 * Parameters:
 *   a    (*Agent) — the running agent.
 *   name (string) — a stem such as harvey-session-20261030-143612, a path, or "".
 *
 * Returns:
 *   string — the path of the .jsonl log.
 *   error  — usage (2) when no session is open and none was named; no_input
 *            (66) when the named one does not exist.
 *
 * Example:
 *   path, err := resolveDraftSession(a, "harvey-session-20261030-143612")
 */
func resolveDraftSession(a *Agent, name string) (string, error) {
	if name == "" {
		if a.SessionLog == nil {
			return "", Usagef("no session is open; name one to draft from. %s", draftUsage)
		}
		return a.SessionLog.Path(), nil
	}
	path := name
	if !strings.ContainsAny(name, `/\`) && !strings.HasSuffix(name, ".jsonl") {
		path = filepath.Join(a.SessionsDir, name+".jsonl")
	} else if !filepath.IsAbs(path) {
		abs, err := a.Workspace.AbsPath(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	if _, err := os.Stat(path); err != nil {
		return "", NoInputf("no session log %s", path)
	}
	return path, nil
}

/** draftStop carries the two ways to interrupt /kb draft. The first Ctrl-C
 * cancels soft: the pass in flight finishes, its answer is kept, and no further
 * pass is read. The second cancels hard: the request in flight is abandoned,
 * which is the only way out of a model that is stuck, and which wedges a
 * hailo-ollama server, so it is the author's second choice and never the
 * first.
 *
 * Fields:
 *   soft (context.Context) — cancelled by the first interrupt.
 *   hard (context.Context) — cancelled by the second; the model calls use it.
 *
 * Example:
 *   stop := draftStop{soft: context.Background(), hard: context.Background()}
 */
type draftStop struct {
	soft context.Context
	hard context.Context
}

/** watchInterrupts turns interrupt signals into a draftStop: the first signal
 * cancels soft and tells the author what it did, the second cancels hard.
 *
 * Parameters:
 *   sig (<-chan os.Signal) — interrupts, from signal.Notify.
 *   out (io.Writer)        — where the one notice is written.
 *
 * Returns:
 *   draftStop — the two contexts.
 *   func()    — done; call it when the command ends to stop the watcher.
 *
 * Example:
 *   ch := make(chan os.Signal, 2)
 *   signal.Notify(ch, os.Interrupt)
 *   stop, done := watchInterrupts(ch, os.Stdout)
 *   defer done()
 */
func watchInterrupts(sig <-chan os.Signal, out io.Writer) (draftStop, func()) {
	soft, softCancel := context.WithCancel(context.Background())
	hard, hardCancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		for n := 0; ; {
			select {
			case <-sig:
				n++
				if n == 1 {
					fmt.Fprintln(out, "\nCtrl-C: stopping after this pass. Ctrl-C again cancels the request now.")
					softCancel()
				} else {
					hardCancel()
					return
				}
			case <-finished:
				return
			}
		}
	}()
	return draftStop{soft: soft, hard: hard}, func() {
		close(finished)
		softCancel()
		hardCancel()
	}
}

/** kbDraft runs /kb draft with Ctrl-C wired as draftStop describes. */
func kbDraft(a *Agent, args []string, out io.Writer) error {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	stop, done := watchInterrupts(sigCh, out)
	defer done()
	return runKBDraft(stop, a, args, out)
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	return cutAtWord(s, n)
}

/** runKBDraft is /kb draft: it reads a session log, asks the drafting model
 * about it in passes that fit the window, drops what kb already holds, and
 * writes up to the limit as drafts that wait for the author's review. It
 * never promotes one. A pass that fails does not stop the others; the run
 * prints its report and returns the class of the first failure (DR-0003).
 * An interrupt stops it and keeps what was written: see draftStop.
 *
 * Parameters:
 *   stop (draftStop)       — the soft and hard interrupts.
 *   a    (*Agent)          — the running agent.
 *   args ([]string)        — everything after /kb draft.
 *   out  (io.Writer)       — progress and the report.
 *
 * Returns:
 *   error — usage (2) for a bad line or no project; 1 for an unknown project;
 *           66 for an unknown session; 69 for a missing or old kb or an
 *           unreachable model; the first failed pass's class otherwise.
 *
 * Example:
 *   err := runKBDraft(draftStop{soft: ctx, hard: ctx}, a, []string{"--project", "harvey"}, os.Stdout)
 */
func runKBDraft(stop draftStop, a *Agent, args []string, out io.Writer) error {
	p, err := parseDraftArgs(args)
	if err != nil {
		return err
	}
	root, db := a.Workspace.Root, a.knowledgeDBPath()
	project, err := ResolveDraftProject(root, db, p.project, a.Config.KB.Project)
	if err != nil {
		return err
	}
	path, err := resolveDraftSession(a, p.session)
	if err != nil {
		return err
	}
	events, _, err := ReadSessionLog(path)
	if err != nil {
		return err
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	entries := DraftTranscript(events, LogBlobReader(path))
	if len(entries) == 0 {
		fmt.Fprintf(out, "Nothing to draft from %s: no conversation or tool use in it.\n", stem)
		return nil
	}

	// @NAME on the line beats draft.model; unset means the session's own model.
	name := strings.TrimPrefix(strings.TrimSpace(p.mention), "@")
	if name == "" {
		name = strings.TrimSpace(a.Config.Draft.Model)
	}
	drafter, restore, err := resolveDrafter(a, name, out)
	if err != nil {
		who := name
		if who == "" {
			who = "the active model"
		}
		class := ClassUnavailable
		if ExitCodeFor(err) == ClassNoInput {
			class = ClassNoInput // the model is not installed on its service
		}
		return ClassedAs(class, fmt.Errorf("could not use %s for drafting: %w", who, err))
	}
	defer restore()

	existing, err := ExistingObservationBodies(root, db, project)
	if err != nil {
		return err
	}
	budget, assumed := DraftBudget(a.effectiveContextLimit())
	passes := SplitPasses(entries, budget)
	by := DraftBy(drafter.Name())
	note := ""
	if assumed {
		note = fmt.Sprintf(", window not known, %d assumed", draftFallbackWindow)
	}
	fmt.Fprintf(out, "Drafting from %s with %s: %d entries in %d pass(es) (%d tokens each%s).\n",
		stem, a.drafterDisplay(drafter), len(entries), len(passes), budget, note)

	type pending struct {
		Proposal
		pass DraftPass
	}
	var queue []pending
	var skipped []DraftSkip
	var firstErr error
	failed, dropped, passesAsked, interrupted := 0, 0, 0, false
	held := append([]string(nil), existing...)
	for i, pass := range passes {
		if stop.soft.Err() != nil || stop.hard.Err() != nil {
			interrupted = true
			break
		}
		passesAsked++
		props, d, perr := RunDraftPass(stop.hard, drafter, pass, project, stem, i+1, len(passes))
		if perr != nil {
			if stop.hard.Err() != nil {
				interrupted = true
				break
			}
			failed++
			if firstErr == nil {
				firstErr = perr
			}
			fmt.Fprintf(out, "  %v\n", perr)
			continue
		}
		dropped += d
		fmt.Fprintf(out, "  pass %d of %d (%d events): %d proposal(s)\n", i+1, len(passes), len(pass.Entries), len(props))
		kept, sk := DedupAgainst(props, held)
		skipped = append(skipped, sk...)
		for _, k := range kept {
			held = append(held, k.Body)
			queue = append(queue, pending{k, pass})
		}
	}

	written, left := 0, 0
	var writeErr error
	for _, q := range queue {
		if p.limit > 0 && written >= p.limit {
			left++
			continue
		}
		if writeErr != nil {
			left++
			continue
		}
		id, werr := WriteDraft(root, db, project, by, Proposal{Kind: q.Kind, Body: FinishBody(q.Proposal, q.pass)})
		if werr != nil {
			writeErr = werr
			left++
			continue
		}
		written++
		a.noteKBItem(kbRefOf("observation", id, ""), "observation", nil)
		fmt.Fprintf(out, "  drafted #%d [%s] %s\n", id, q.Kind, shorten(q.Body, 80))
	}

	fmt.Fprintln(out)
	for _, s := range skipped {
		fmt.Fprintf(out, "  skipped (already in kb: %q): %s\n", shorten(s.Matched, 60), shorten(s.Proposal.Body, 60))
	}
	if dropped > 0 {
		fmt.Fprintf(out, "  %d line(s) the model wrote were not an observation and were dropped\n", dropped)
	}
	if left > 0 {
		fmt.Fprintf(out, "  %d left unwritten (the limit is %d; --limit 0 writes all)\n", left, p.limit)
	}
	if interrupted {
		fmt.Fprintf(out, "  interrupted after %d of %d pass(es); the rest were not asked\n", passesAsked, len(passes))
	}
	switch {
	case written == 0 && failed == 0 && writeErr == nil && !interrupted && len(skipped) == 0:
		fmt.Fprintf(out, "Nothing worth keeping found in %s.\n", stem)
	case written == 0:
		fmt.Fprintf(out, "No drafts written.\n")
	default:
		plural := "s"
		if written == 1 {
			plural = ""
		}
		fmt.Fprintf(out, "%d draft%s written to %s, signed %q. They are not searchable or recalled until you promote them.\n",
			written, plural, project, by)
		fmt.Fprintf(out, "Next, in a normal shell:  kb observation review list --project %s\n", project)
		fmt.Fprintf(out, "                          kb observation review promote ID\n")
	}
	if writeErr != nil {
		fmt.Fprintf(out, "  stopped writing: %v\n", writeErr)
		return writeErr
	}
	if firstErr != nil {
		return ClassedAs(ExitCodeFor(firstErr), fmt.Errorf("%d of %d pass(es) failed: %w", failed, len(passes), firstErr))
	}
	return nil
}

/** checkDraftModel warns, at start-up, when draft.model names a model that its
 * service does not have. It is only a warning: chat does not depend on the
 * drafter, and /kb draft keeps its own hard error. A service that is down or
 * cannot be asked proves nothing, so it passes without a word. Nothing is
 * printed when draft.model is unset or installed.
 *
 * Parameters:
 *   out (io.Writer) — destination for the one-line warning.
 *
 * Example:
 *   a.checkDraftModel(out)
 */
func (a *Agent) checkDraftModel(out io.Writer) {
	name := strings.TrimSpace(a.Config.Draft.Model)
	if name == "" {
		return
	}
	if err := a.requireInstalled(name); err != nil && ExitCodeFor(err) == ClassNoInput {
		fmt.Fprintf(out, yellow("  ⚠")+" draft.model: %v. Chat is unaffected; /kb draft will stop until this is fixed.\n", err)
	}
}
