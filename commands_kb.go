// Package harvey — commands_kb.go implements the /kb slash command family
// for managing the knowledge base.
package harvey

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	knowledge "github.com/rsdoiel/knowledge"
)

// ─── /kb ─────────────────────────────────────────────────────────────────────

/** cmdKB handles Knowledge Base (KB) commands for managing projects,
 * observations, concepts, and full-text search.
 *
 * Subcommands:
 *   status    — Show summary of all projects and recent observations
 *   search    — Full-text search across all KB content
 *   inject   — Inject KB content into conversation context
 *   project  — Manage projects (add, list, info, status)
 *   observe  — Manage observations (add, list)
 *   concept  — Manage concepts (add, list, info)
 *   link     — Link observations/concepts to projects/concepts
 *
 * The knowledge base is a SQLite3 database storing projects, observations,
 * and concepts with FTS5 full-text search. Commands delegate to specialized
 * handlers (kbStatus, kbSearch, kbProject, etc.).
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with active KB connection.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdKB(a *Agent, args []string, out io.Writer) error {
	if a.KB == nil {
		return Unavailablef("the knowledge base is not open. This should not happen — please restart Harvey")
	}
	if len(args) == 0 {
		return kbStatus(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "status":
		return kbStatus(a, out)
	case "search":
		return kbSearch(a, args[1:], out)
	case "inject":
		return kbInject(a, args[1:], out)
	case "project":
		return kbProject(a, args[1:], out)
	case "observe":
		return kbObserve(a, args[1:], out)
	case "concept":
		return kbConcept(a, args[1:], out)
	case "source":
		return kbSource(a, args[1:], out)
	case "retract":
		return kbRetract(a, args[1:], out)
	case "cite":
		return kbCite(a, args[1:], out)
	case "show":
		return kbShow(a, args[1:], out)
	case "learn":
		return kbLearn(a, args[1:], out)
	case "check-retractions":
		return kbCheckRetractions(a, out)
	default:
		return Usagef("unknown kb subcommand: %s  (usage: /kb <status|search|inject|project|observe|concept|source|retract|cite|show|learn|check-retractions> [args...])", args[0])
	}
}

func kbStatus(a *Agent, out io.Writer) error {
	fmt.Fprintln(out)
	s, err := a.KB.Summary()
	if err != nil {
		return err
	}
	fmt.Fprint(out, s)
	return nil
}

// kbSearch handles /kb search TERM [TERM...] using the FTS5 index.
func kbSearch(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb search TERM [TERM...]  (quote phrases (\"WAL mode\"), use * for prefix (docker*))")
	}
	term := strings.Join(args, " ")
	results, err := a.KB.Search(term)
	if err != nil {
		return fmt.Errorf("kb search: %w", err)
	}
	if len(results) == 0 {
		fmt.Fprintf(out, "  No results for %q\n", term)
		return nil
	}
	fmt.Fprintln(out)
	for _, r := range results {
		switch {
		case r.Label != "" && r.Snippet != "":
			fmt.Fprintf(out, "  [%-10s] %s — %s\n", r.Kind, r.Label, r.Snippet)
		case r.Label != "":
			fmt.Fprintf(out, "  [%-10s] %s\n", r.Kind, r.Label)
		default:
			fmt.Fprintf(out, "  [%-10s] %s\n", r.Kind, r.Snippet)
		}
	}
	fmt.Fprintln(out)
	return nil
}

// kbInject formats KB content as Markdown and adds it to the conversation as
// context. With no argument it uses the current project (or all projects when
// none is set); with a project name it injects only that project.
func kbInject(a *Agent, args []string, out io.Writer) error {
	projectID := a.Config.Memory.CurrentProjectID
	label := "all projects"

	if len(args) > 0 {
		name := strings.Join(args, " ")
		p, err := a.KB.ProjectByName(name)
		if err != nil {
			return err
		}
		if p == nil {
			return Negativef("project %q not found. Use /kb project list to see available projects", name)
		}
		projectID = p.ID
		label = fmt.Sprintf("project %q", p.Name)
	} else if projectID > 0 {
		label = fmt.Sprintf("current project (id=%d)", projectID)
	}

	md, err := a.KB.FormatMarkdown(projectID)
	if err != nil {
		return err
	}
	if md == "" {
		fmt.Fprintln(out, "  Knowledge base is empty.")
		return nil
	}

	a.AddMessage("user", "[knowledge base context]\n\n"+md)
	fmt.Fprintf(out, green("✓")+" KB context for %s injected (%d bytes).\n", label, len(md))
	return nil
}

// kbProject handles /kb project <list|add NAME [DESC]|use ID>
func kbProject(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb project <list|add NAME [DESC]|use ID>")
	}
	switch strings.ToLower(args[0]) {
	case "list":
		projects, err := a.KB.Projects()
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Fprintln(out, "  (no projects)")
			return nil
		}
		for _, p := range projects {
			active := ""
			if a.Config.Memory.CurrentProjectID == p.ID {
				active = " *"
			}
			fmt.Fprintf(out, "  [%d]%s %s  (%s)\n", p.ID, active, p.Name, p.Status)
			if p.Description != "" {
				fmt.Fprintf(out, "      %s\n", p.Description)
			}
		}
	case "add":
		if len(args) < 2 {
			return Usagef("usage: /kb project add NAME [DESCRIPTION]")
		}
		name := args[1]
		desc := strings.Join(args[2:], " ")
		id, err := a.KB.AddProject(name, desc)
		if err != nil {
			return err
		}
		a.Config.Memory.CurrentProjectID = id
		fmt.Fprintf(out, "Project %q added (id=%d) and set as current.\n", name, id)
	case "use":
		if len(args) < 2 {
			return Usagef("usage: /kb project use ID")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return Usagef("invalid project ID: %s", args[1])
		}
		a.Config.Memory.CurrentProjectID = id
		fmt.Fprintf(out, "Current project set to id=%d.\n", id)
	default:
		return Usagef("unknown project subcommand: %s", args[0])
	}
	return nil
}

// kbObserve handles /kb observe KIND BODY...
// KIND defaults to "note" if omitted or invalid.
func kbObserve(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb observe [KIND] TEXT  (kinds: %s; default: note)", strings.Join(knowledge.ValidObservationKinds, ", "))
	}
	if a.Config.Memory.CurrentProjectID == 0 {
		return Negativef("no current project. Use /kb project add NAME or /kb project use ID first")
	}

	kind := "note"
	bodyArgs := args
	if knowledge.IsValidKind(strings.ToLower(args[0])) {
		kind = strings.ToLower(args[0])
		bodyArgs = args[1:]
	}
	if len(bodyArgs) == 0 {
		fmt.Fprintln(out, "Observation text is required.")
		return nil
	}
	body := strings.Join(bodyArgs, " ")
	id, err := a.KB.AddObservation(a.Config.Memory.CurrentProjectID, kind, body)
	if err != nil {
		return err
	}
	a.LastObservationID = id
	fmt.Fprintf(out, "Observation recorded (id=%d, kind=%s).\n", id, kind)
	if a.LastRAGInfo != nil && len(a.LastRAGInfo.Sources) > 0 {
		fmt.Fprintln(out, "  RAG sources available — link with /kb cite SOURCE_ID [SOURCE_ID ...]:")
		for _, src := range a.LastRAGInfo.Sources {
			label := src.Source
			var meta []string
			if src.SourceTitle != "" {
				meta = append(meta, src.SourceTitle)
			}
			if src.SourceDOI != "" {
				meta = append(meta, "doi:"+src.SourceDOI)
			}
			if len(meta) > 0 {
				label += " (" + strings.Join(meta, ", ") + ")"
			}
			fmt.Fprintf(out, "    %s\n", label)
		}
		fmt.Fprintln(out, "  Use /kb source list to find source IDs.")
	}
	return nil
}

// kbConcept handles /kb concept <list|add NAME [DESC]>
func kbConcept(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb concept <list|add NAME [DESCRIPTION]>")
	}
	switch strings.ToLower(args[0]) {
	case "list":
		concepts, err := a.KB.Concepts()
		if err != nil {
			return err
		}
		if len(concepts) == 0 {
			fmt.Fprintln(out, "  (no concepts)")
			return nil
		}
		for _, c := range concepts {
			fmt.Fprintf(out, "  [%d] %s", c.ID, c.Name)
			if c.Description != "" {
				fmt.Fprintf(out, " — %s", c.Description)
			}
			fmt.Fprintln(out)
		}
	case "add":
		if len(args) < 2 {
			return Usagef("usage: /kb concept add NAME [DESCRIPTION]")
		}
		name := args[1]
		desc := strings.Join(args[2:], " ")
		id, err := a.KB.AddConcept(name, desc)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Concept %q added (id=%d).\n", name, id)
	default:
		return Usagef("unknown concept subcommand: %s", args[0])
	}
	return nil
}

// ─── /kb source ──────────────────────────────────────────────────────────────

// kbSource handles /kb source <list|add|show|remove> [args...]
func kbSource(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb source <list|add|show ID|remove ID>")
	}
	switch strings.ToLower(args[0]) {
	case "list":
		sources, err := a.KB.ListSources()
		if err != nil {
			return err
		}
		if len(sources) == 0 {
			fmt.Fprintln(out, "  (no sources registered)")
			return nil
		}
		fmt.Fprintf(out, "  %-4s  %-32s  %-24s  %s\n", "ID", "Title", "Identifier", "Retracted")
		for _, s := range sources {
			ident := "—"
			if s.IdentifierType != "" && s.IdentifierValue != "" {
				ident = s.IdentifierType + ":" + s.IdentifierValue
			}
			retracted := "no"
			if s.Retracted {
				retracted = "[RETRACTED]"
			}
			fmt.Fprintf(out, "  %-4d  %-32s  %-24s  %s\n", s.ID, truncate(s.Title, 32), truncate(ident, 24), retracted)
		}
	case "add":
		var s knowledge.Source
		var i int
		for i = 1; i < len(args); i++ {
			switch args[i] {
			case "--doi":
				if i+1 < len(args) {
					i++
					s.IdentifierType = "doi"
					s.IdentifierValue = args[i]
				}
			case "--url":
				if i+1 < len(args) {
					i++
					if s.IdentifierType == "" {
						s.IdentifierType = "url"
						s.IdentifierValue = args[i]
					}
				}
			case "--title":
				if i+1 < len(args) {
					i++
					s.Title = args[i]
				}
			case "--authors":
				if i+1 < len(args) {
					i++
					s.Authors = args[i]
				}
			case "--publisher":
				if i+1 < len(args) {
					i++
					s.Publisher = args[i]
				}
			case "--date":
				if i+1 < len(args) {
					i++
					s.PublishedDate = args[i]
				}
			case "--rights":
				if i+1 < len(args) {
					i++
					s.Rights = args[i]
				}
			case "--version":
				if i+1 < len(args) {
					i++
					s.Version = args[i]
				}
			default:
				if s.Title == "" {
					s.Title = args[i]
				}
			}
		}
		if s.Title == "" {
			return Usagef("usage: /kb source add --title TITLE [--doi DOI] [--url URL] [--authors AUTHORS] ")
		}
		id, err := a.KB.AddSource(s)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Source added (id=%d).\n", id)
	case "show":
		if len(args) < 2 {
			return Usagef("usage: /kb source show ID")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return Usagef("invalid source ID %q", args[1])
		}
		s, err := a.KB.ShowSource(id)
		if err != nil {
			return Negativef("source %d not found", id)
		}
		fmt.Fprintf(out, "  ID:        %d\n", s.ID)
		fmt.Fprintf(out, "  Title:     %s\n", s.Title)
		if s.IdentifierType != "" {
			fmt.Fprintf(out, "  %s:      %s\n", s.IdentifierType, s.IdentifierValue)
		}
		if s.Authors != "" {
			fmt.Fprintf(out, "  Authors:   %s\n", s.Authors)
		}
		if s.PublishedDate != "" {
			fmt.Fprintf(out, "  Date:      %s\n", s.PublishedDate)
		}
		if s.Publisher != "" {
			fmt.Fprintf(out, "  Publisher: %s\n", s.Publisher)
		}
		if s.Retracted {
			fmt.Fprintf(out, "  ⚠ RETRACTED: %s\n", s.RetractionNote)
		}
	case "remove":
		if len(args) < 2 {
			return Usagef("usage: /kb source remove ID")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return Usagef("invalid source ID %q", args[1])
		}
		if err := a.KB.RemoveSource(id); err != nil {
			fmt.Fprintf(out, "  ✗ %v\n", err)
			return nil
		}
		fmt.Fprintf(out, "Source %d removed.\n", id)
	default:
		return Usagef("unknown source subcommand: %s", args[0])
	}
	return nil
}

// kbRetract handles /kb retract ID [--note NOTE]
func kbRetract(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb retract SOURCE_ID [--note NOTE]")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return Usagef("invalid source ID %q", args[0])
	}
	note := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--note" && i+1 < len(args) {
			i++
			note = args[i]
		}
	}
	if err := a.KB.RetractSource(id, note); err != nil {
		return err
	}
	fmt.Fprintf(out, "Source %d marked as retracted.\n", id)
	return nil
}

// ─── /kb cite ────────────────────────────────────────────────────────────────

// kbCite handles /kb cite SOURCE_ID [SOURCE_ID ...]
// Links one or more sources to the most recently recorded observation.
func kbCite(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb cite SOURCE_ID [SOURCE_ID ...]  (use /kb source list to see available source IDs)")
	}
	if a.LastObservationID == 0 {
		return Negativef("no recent observation to cite. Use /kb observe first")
	}
	var failed error // first failure; every id is still tried
	for _, arg := range args {
		id, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			fmt.Fprintf(out, "  Invalid source ID %q — skipping\n", arg)
			failed = keepFirstFailure(failed, Usagef("invalid source ID %q", arg))
			continue
		}
		if err := a.KB.LinkObservationSource(a.LastObservationID, id, "cited"); err != nil {
			fmt.Fprintf(out, "  ✗ source %d: %v\n", id, err)
			failed = keepFirstFailure(failed, fmt.Errorf("source %d: %w", id, err))
		} else {
			fmt.Fprintf(out, "  Source %d linked to observation %d.\n", id, a.LastObservationID)
		}
	}
	return failed
}

// ─── /kb show ────────────────────────────────────────────────────────────────

// kbShow handles /kb show OBS_ID — displays an observation with its linked
// sources and retraction warnings.
func kbShow(a *Agent, args []string, out io.Writer) error {
	var obsID int64
	if len(args) > 0 {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return Usagef("invalid observation ID %q", args[0])
		}
		obsID = id
	} else if a.LastObservationID != 0 {
		obsID = a.LastObservationID
	} else {
		return Usagef("usage: /kb show OBS_ID")
	}

	obs, err := a.KB.ObservationByID(obsID)
	if err != nil {
		return Negativef("observation %d not found", obsID)
	}
	fmt.Fprintf(out, "  [%s] %s\n", obs.Kind, obs.Body)
	if obs.SourceDOI != "" {
		fmt.Fprintf(out, "  DOI (legacy): %s\n", obs.SourceDOI)
	}

	sources, err := a.KB.ObservationSources(obsID)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		fmt.Fprintln(out, "  (no sources linked)")
		return nil
	}
	fmt.Fprintln(out, "  Sources:")
	for _, s := range sources {
		ident := ""
		if s.IdentifierType != "" && s.IdentifierValue != "" {
			ident = fmt.Sprintf(" [%s:%s]", s.IdentifierType, s.IdentifierValue)
		}
		if s.Retracted {
			fmt.Fprintf(out, "    ⚠ RETRACTED [%d] %s%s — %s\n", s.ID, s.Title, ident, s.RetractionNote)
		} else {
			fmt.Fprintf(out, "    [%d] %s%s\n", s.ID, s.Title, ident)
		}
	}
	return nil
}

// kbCheckRetractions handles /kb check-retractions: queries the Retraction
// Watch API for every non-retracted DOI source and marks hits as retracted.
func kbCheckRetractions(a *Agent, out io.Writer) error {
	fmt.Fprintln(out, "Checking registered DOIs against the Retraction Watch database...")
	checked, updated, err := a.KB.CheckRetractions(
		func(doi string) (bool, string, error) {
			return CheckDOIRetraction(doi, DefaultRetractionWatchURL)
		},
		out,
	)
	if err != nil {
		return fmt.Errorf("check-retractions: %w", err)
	}
	fmt.Fprintf(out, "\nChecked %d DOI source(s); %d newly marked as retracted.\n", checked, updated)
	if updated > 0 {
		fmt.Fprintln(out, "Run /kb source list to review retracted sources.")
	}
	return nil
}
