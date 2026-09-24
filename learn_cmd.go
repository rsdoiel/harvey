package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	knowledge "github.com/rsdoiel/knowledge"
)

// learnDefaultDraftLimit is how many items one `/kb learn draft` run attempts
// unless --limit says otherwise. Ingesting the real hand-offs queues hundreds
// of sections (539 for 35 files), and a batch run against a small local model
// is slow, so an unbounded default would be a surprise. --limit 0 drafts
// everything queued.
const learnDefaultDraftLimit = 25

const learnUsage = "Usage: /kb learn <ingest|draft|review|concepts> [args...]   (bare /kb learn drafts, then reviews)"

/** learnDrafterFor resolves the model `/kb learn` drafts with, in the order
 * RSDOIEL chose on 2026-09-24: an @name given on the command, else learn_model
 * from harvey.yaml, else the active model. It returns the Drafter and a
 * restore function the caller must run when the work ends, which undoes a
 * local model switch. The active model is used as-is, with no switch.
 *
 * Parameters:
 *   a       (*Agent)    — the running agent.
 *   mention (string)    — an @name from the command line, with or without the @; may be empty.
 *   out     (io.Writer) — where model-switch progress is written.
 *
 * Returns:
 *   Drafter — the model to draft with.
 *   func()  — restore; never nil on success.
 *   error   — if the name matches no route or model, or no model is active.
 *
 * Example:
 *   d, restore, err := learnDrafterFor(a, "@granite", out)
 *   if err == nil { defer restore() }
 */
func learnDrafterFor(a *Agent, mention string, out io.Writer) (Drafter, func(), error) {
	name := strings.TrimPrefix(strings.TrimSpace(mention), "@")
	if name == "" {
		name = strings.TrimSpace(a.Config.LearnModel)
	}
	if a.learnDrafterOverride != nil {
		return a.learnDrafterOverride(name, out)
	}
	if name == "" {
		if a.Client == nil {
			return nil, nil, fmt.Errorf("no model is active; name one with @NAME or set learn_model in harvey.yaml")
		}
		return newLLMDrafter(a.Client, ""), func() {}, nil
	}
	target, ok, err := resolveDispatchTarget(a, name, out)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("no model or route is named %q", name)
	}
	return newLLMDrafter(target.Client, name), target.Restore, nil
}

// splitMention separates an @name token from the other arguments.
func splitMention(args []string) (mention string, rest []string) {
	for _, arg := range args {
		if strings.HasPrefix(arg, "@") && len(arg) > 1 {
			mention = arg
			continue
		}
		rest = append(rest, arg)
	}
	return mention, rest
}

func kbLearnDraft(a *Agent, args []string, out io.Writer) error {
	const usage = "Usage: /kb learn draft [--limit N] [--dry-run] [@model]"
	mention, rest := splitMention(args)
	limit, dryRun := learnDefaultDraftLimit, false
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--dry-run":
			dryRun = true
		case "--limit":
			if i+1 >= len(rest) {
				fmt.Fprintln(out, "--limit needs a number.", usage)
				return nil
			}
			n, err := strconv.Atoi(rest[i+1])
			if err != nil || n < 0 {
				fmt.Fprintf(out, "--limit %q is not a number of items. %s\n", rest[i+1], usage)
				return nil
			}
			limit = n
			i++
		default:
			fmt.Fprintf(out, "Unknown option %q. %s\n", rest[i], usage)
			return nil
		}
	}
	projectID := a.Config.Memory.CurrentProjectID
	if projectID == 0 {
		fmt.Fprintln(out, "No current project. Pick one first with /kb project use ID (see /kb project list).")
		return nil
	}

	items, err := a.KB.DocumentReviewQueue(projectID, "unsummarized")
	if err != nil {
		return err
	}
	sections, gists := 0, 0
	for _, it := range items {
		switch {
		case it.Level == "gist":
			gists++
		case strings.TrimSpace(it.Body) != "":
			sections++
		}
	}
	if sections+gists == 0 {
		fmt.Fprintln(out, "Nothing to draft: every section with text already has a summary. Ingest documents with /kb learn ingest.")
		return nil
	}
	if dryRun {
		model := strings.TrimPrefix(mention, "@")
		if model == "" {
			model = a.Config.LearnModel
		}
		if model == "" {
			model = "the active model"
		}
		fmt.Fprintf(out, "Would draft up to %d of %d item(s) (%d section(s), %d gist(s)) with %s.\n",
			limitOrAll(limit, sections+gists), sections+gists, sections, gists, model)
		return nil
	}

	drafter, restore, err := learnDrafterFor(a, mention, out)
	if err != nil {
		name := strings.TrimPrefix(mention, "@")
		if name == "" {
			name = a.Config.LearnModel
		}
		fmt.Fprintf(out, "Could not use model %q for drafting: %v\n", name, err)
		return nil
	}
	defer restore()

	limitNote := fmt.Sprintf("limit %d; --limit 0 drafts everything queued", limit)
	if limit == 0 {
		limitNote = "no limit"
	}
	fmt.Fprintf(out, "\nDrafting with %s (%s).\n", drafter.Name(), limitNote)
	s := &LearnSession{KB: a.KB, ProjectID: projectID, Drafter: drafter}
	rep, err := s.DraftPending(context.Background(), limit, func(o DraftOutcome, done, total int) {
		where := o.Document
		if o.Heading != "" {
			where += " › " + o.Heading
		} else if o.Level == "gist" {
			where += " (whole document)"
		}
		switch o.Status {
		case "drafted":
			conf := "confidence unknown"
			if o.Confidence != nil {
				conf = fmt.Sprintf("confidence %.2f", *o.Confidence)
			}
			fmt.Fprintf(out, "  [%d/%d] %s: drafted (%s)\n", done, total, where, conf)
		case "failed":
			fmt.Fprintf(out, "  [%d/%d] %s: failed (%s)\n", done, total, where, o.Reason)
		}
	})
	if err != nil {
		fmt.Fprintf(out, "\nStopped: %v\n", err)
	}
	fmt.Fprintf(out, "\nDrafted %d, failed %d, skipped %d. Nothing is trusted until you accept it: /kb learn review\n",
		rep.Drafted, rep.Failed, rep.Skipped)
	return nil
}

func limitOrAll(limit, total int) int {
	if limit == 0 || limit > total {
		return total
	}
	return limit
}

// showReviewItem prints one drafted item with the signals a human uses to
// decide: size, tag density, the model's confidence, who wrote it, whether the
// source has changed since, an excerpt of the source, and the draft.
func showReviewItem(out io.Writer, n, total int, it knowledge.DocumentReviewItem) {
	where := it.DocumentTitle
	kind := "section"
	if it.Level == "gist" {
		kind = "whole document"
	} else if it.Heading != "" {
		where += " › " + it.Heading
	}
	conf := "unknown"
	if it.Confidence != nil {
		conf = fmt.Sprintf("%.2f", *it.Confidence)
	}
	stale := ""
	if it.SummaryStale {
		stale = "  [STALE: the source changed after this was written]"
	}
	fmt.Fprintf(out, "\n── %d of %d ── %s (%s)%s\n", n, total, where, kind, stale)
	size := fmt.Sprintf("%d words · ", it.SourceSize)
	if it.Level == "gist" {
		size = "" // a gist has no source size of its own
	}
	fmt.Fprintf(out, "   %s%d known concept(s) mentioned · confidence %s · drafted by %s\n",
		size, it.TagDensity, conf, it.GeneratedBy)
	if it.Level == "gist" {
		fmt.Fprintln(out, "   source: (drafted from the section summaries)")
	} else {
		excerpt := strings.TrimSpace(it.Body)
		if len(excerpt) > 400 {
			excerpt = strings.TrimSpace(excerpt[:400]) + "…"
		}
		fmt.Fprintf(out, "   source: %s\n", strings.ReplaceAll(excerpt, "\n", "\n           "))
	}
	fmt.Fprintf(out, "   draft:  %s\n", strings.ReplaceAll(strings.TrimSpace(it.SummaryBody), "\n", "\n           "))
}

func kbLearnReview(a *Agent, args []string, out io.Writer) error {
	mention, rest := splitMention(args)
	if len(rest) > 0 {
		fmt.Fprintln(out, "Usage: /kb learn review [@model]   (the model is used only to redraft)")
		return nil
	}
	projectID := a.Config.Memory.CurrentProjectID
	if projectID == 0 {
		fmt.Fprintln(out, "No current project. Pick one first with /kb project use ID (see /kb project list).")
		return nil
	}
	s := &LearnSession{KB: a.KB, ProjectID: projectID}
	items, err := s.DraftedItems()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "Nothing to review: no drafted summaries. Make some with /kb learn draft.")
		return nil
	}

	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	reader := bufio.NewReaderSize(a.In, 1)
	refresh := func(id int64) (knowledge.DocumentReviewItem, bool) {
		fresh, err := s.DraftedItems()
		if err != nil {
			return knowledge.DocumentReviewItem{}, false
		}
		for _, it := range fresh {
			if it.ID == id {
				return it, true
			}
		}
		return knowledge.DocumentReviewItem{}, false
	}

	accepted, skipped := 0, 0
loop:
	for i := 0; i < len(items); {
		it := items[i]
		showReviewItem(out, i+1, len(items), it)
		for {
			fmt.Fprint(out, "\n  [a]ccept  [e]dit  [r]edraft  [s]kip  [q]uit: ")
			line, err := reader.ReadString('\n')
			if err != nil && strings.TrimSpace(line) == "" {
				fmt.Fprintln(out, "\nInput ended; nothing more was changed.")
				break loop
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "a", "accept":
				if err := s.Accept(it.ID); err != nil {
					fmt.Fprintf(out, "  Could not accept: %v\n", err)
					continue
				}
				fmt.Fprintln(out, "  accepted.")
				accepted++
				i++
			case "", "s", "skip":
				fmt.Fprintln(out, "  skipped.")
				skipped++
				i++
			case "q", "quit":
				break loop
			case "e", "edit":
				edited, err := editTextInEditor(it.SummaryBody, ".md")
				if err != nil {
					fmt.Fprintf(out, "  The editor failed: %v\n", err)
					continue
				}
				if strings.TrimSpace(edited) == strings.TrimSpace(it.SummaryBody) {
					fmt.Fprintln(out, "  No change.")
					continue
				}
				if err := s.Edit(it.ID, edited); err != nil {
					fmt.Fprintf(out, "  Could not save the edit: %v\n", err)
					continue
				}
				if fresh, ok := refresh(it.ID); ok {
					it, items[i] = fresh, fresh
				}
				showReviewItem(out, i+1, len(items), it)
				continue
			case "r", "redraft":
				if s.Drafter == nil {
					d, r, err := learnDrafterFor(a, mention, out)
					if err != nil {
						fmt.Fprintf(out, "  Could not use a model to redraft: %v\n", err)
						continue
					}
					s.Drafter, restore = d, r
				}
				if err := s.Redraft(context.Background(), it.ID); err != nil {
					fmt.Fprintf(out, "  %v\n", err)
					continue
				}
				if fresh, ok := refresh(it.ID); ok {
					it, items[i] = fresh, fresh
				}
				showReviewItem(out, i+1, len(items), it)
				continue
			default:
				fmt.Fprintln(out, "  Choose a, e, r, s or q (Enter skips).")
				continue
			}
			break
		}
	}
	fmt.Fprintf(out, "\nAccepted %d, skipped %d. Accepted summaries are now searchable.\n", accepted, skipped)
	return nil
}
