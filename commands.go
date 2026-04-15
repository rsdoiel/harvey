package harvey

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

/** Command describes a slash command available in the Harvey REPL.
 *
 * Fields:
 *   Usage       (string)   — short usage synopsis shown by /help.
 *   Description (string)   — one-line description shown by /help.
 *   Handler     (func)     — called when the command is dispatched; nil for
 *                            commands handled directly in the REPL (exit, quit).
 *
 * Example:
 *   cmd := &Command{
 *       Usage:       "/greet NAME",
 *       Description: "Print a greeting",
 *       Handler: func(a *Agent, args []string, out io.Writer) error {
 *           fmt.Fprintln(out, "Hello,", args[0])
 *           return nil
 *       },
 *   }
 */
type Command struct {
	Usage       string
	Description string
	// Handler is nil for commands handled directly in the REPL (exit, quit).
	Handler func(a *Agent, args []string, out io.Writer) error
}

/** registerCommands wires the built-in slash commands onto the agent.
 *
 * Example:
 *   agent.registerCommands()
 */
func (a *Agent) registerCommands() {
	a.commands = map[string]*Command{
		"help": {
			Usage:       "/help",
			Description: "List available slash commands",
			Handler:     cmdHelp,
		},
		"status": {
			Usage:       "/status",
			Description: "Show current connection, workspace, and session status",
			Handler:     cmdStatus,
		},
		"clear": {
			Usage:       "/clear",
			Description: "Clear conversation history",
			Handler:     cmdClear,
		},
		"ollama": {
			Usage:       "/ollama <start|stop|status|list|use MODEL>",
			Description: "Control the local Ollama service",
			Handler:     cmdOllama,
		},
		"publicai": {
			Usage:       "/publicai <connect|disconnect|status>",
			Description: "Manage the publicai.co connection",
			Handler:     cmdPublicAI,
		},
		"kb": {
			Usage:       "/kb <status|project|observe|concept> [args...]",
			Description: "Manage the workspace knowledge base",
			Handler:     cmdKB,
		},
		"files": {
			Usage:       "/files [PATH]",
			Description: "List files in the workspace (or a sub-directory)",
			Handler:     cmdFiles,
		},
		"exit": {
			Usage:       "/exit",
			Description: "Exit Harvey",
			Handler:     nil,
		},
		"quit": {
			Usage:       "/quit",
			Description: "Exit Harvey",
			Handler:     nil,
		},
	}
}

/** dispatch parses a slash command line and runs its handler. Returns
 * (shouldExit, error).
 *
 * Parameters:
 *   input (string)   — the raw slash-command line typed by the user.
 *   out   (io.Writer) — destination for command output.
 *
 * Returns:
 *   bool  — true if the agent should exit after this command.
 *   error — any error returned by the handler.
 *
 * Example:
 *   exit, err := agent.dispatch("/kb status", os.Stdout)
 */
func (a *Agent) dispatch(input string, out io.Writer) (bool, error) {
	parts := strings.Fields(strings.TrimPrefix(input, "/"))
	if len(parts) == 0 {
		return false, nil
	}
	name := strings.ToLower(parts[0])
	args := parts[1:]

	if name == "exit" || name == "quit" {
		return true, nil
	}
	cmd, ok := a.commands[name]
	if !ok {
		fmt.Fprintf(out, "Unknown command: /%s  (type /help for a list)\n", name)
		return false, nil
	}
	if cmd.Handler != nil {
		return false, cmd.Handler(a, args, out)
	}
	return false, nil
}

// ─── Built-in handlers ───────────────────────────────────────────────────────

func cmdHelp(a *Agent, _ []string, out io.Writer) error {
	fmt.Fprintln(out)
	for _, cmd := range a.commands {
		fmt.Fprintf(out, "  %-50s %s\n", cmd.Usage, cmd.Description)
	}
	fmt.Fprintln(out)
	return nil
}

func cmdStatus(a *Agent, _ []string, out io.Writer) error {
	if a.Client == nil {
		fmt.Fprintln(out, "Backend:   none")
	} else {
		fmt.Fprintf(out, "Backend:   %s\n", a.Client.Name())
	}
	fmt.Fprintf(out, "History:   %d messages\n", len(a.History))
	if a.Workspace != nil {
		fmt.Fprintf(out, "Workspace: %s\n", a.Workspace.Root)
	}
	if a.KB != nil {
		fmt.Fprintln(out, "KB:        open (.harvey/knowledge.db)")
	} else {
		fmt.Fprintln(out, "KB:        not open")
	}
	return nil
}

func cmdClear(a *Agent, _ []string, out io.Writer) error {
	a.ClearHistory()
	fmt.Fprintln(out, "Conversation history cleared.")
	return nil
}

func cmdOllama(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /ollama <start|stop|status|list|use MODEL>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "start":
		fmt.Fprintln(out, "Starting Ollama...")
		if err := StartOllamaService(); err != nil {
			return err
		}
		fmt.Fprintln(out, "Ollama is running.")
	case "stop":
		fmt.Fprintln(out, "Use your system's service manager to stop Ollama (e.g. systemctl stop ollama).")
	case "status":
		if ProbeOllama(a.Config.OllamaURL) {
			fmt.Fprintln(out, "Ollama is running.")
		} else {
			fmt.Fprintln(out, "Ollama is not running.")
		}
	case "list":
		if !ProbeOllama(a.Config.OllamaURL) {
			fmt.Fprintln(out, "Ollama is not running.")
			return nil
		}
		models, err := NewOllamaClient(a.Config.OllamaURL, "").Models(context.Background())
		if err != nil {
			return err
		}
		if len(models) == 0 {
			fmt.Fprintln(out, "No models installed. Run: ollama pull <model>")
			return nil
		}
		for _, m := range models {
			marker := "  "
			if oc, ok := a.Client.(*OllamaClient); ok && oc.Model() == m {
				marker = "* "
			}
			fmt.Fprintf(out, "%s%s\n", marker, m)
		}
	case "use":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama use MODEL")
			return nil
		}
		model := args[1]
		if !ProbeOllama(a.Config.OllamaURL) {
			fmt.Fprintln(out, "Ollama is not running. Use /ollama start first.")
			return nil
		}
		a.Config.OllamaModel = model
		a.Client = NewOllamaClient(a.Config.OllamaURL, model)
		fmt.Fprintf(out, "Now using Ollama model: %s\n", model)
	default:
		fmt.Fprintf(out, "Unknown ollama subcommand: %s\n", args[0])
	}
	return nil
}

func cmdPublicAI(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /publicai <connect|disconnect|status>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "connect":
		if a.Config.PublicAIKey == "" {
			fmt.Fprintln(out, "No API key found. Set the PUBLICAI_API_KEY environment variable.")
			return nil
		}
		a.Client = NewPublicAIClient(a.Config.PublicAIURL, a.Config.PublicAIKey, a.Config.PublicAIModel)
		fmt.Fprintf(out, "Connected to publicai.co (%s).\n", a.Config.PublicAIModel)
	case "disconnect":
		if _, ok := a.Client.(*PublicAIClient); ok {
			a.Client = nil
			fmt.Fprintln(out, "Disconnected from publicai.co.")
		} else {
			fmt.Fprintln(out, "Not currently connected to publicai.co.")
		}
	case "status":
		if _, ok := a.Client.(*PublicAIClient); ok {
			fmt.Fprintf(out, "Connected to publicai.co (%s).\n", a.Config.PublicAIModel)
		} else {
			fmt.Fprintln(out, "Not connected to publicai.co.")
		}
	default:
		fmt.Fprintf(out, "Unknown publicai subcommand: %s\n", args[0])
	}
	return nil
}

// ─── /kb ─────────────────────────────────────────────────────────────────────

func cmdKB(a *Agent, args []string, out io.Writer) error {
	if a.KB == nil {
		fmt.Fprintln(out, "Knowledge base is not open. This should not happen — please restart Harvey.")
		return nil
	}
	if len(args) == 0 {
		return kbStatus(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "status":
		return kbStatus(a, out)
	case "project":
		return kbProject(a, args[1:], out)
	case "observe":
		return kbObserve(a, args[1:], out)
	case "concept":
		return kbConcept(a, args[1:], out)
	default:
		fmt.Fprintf(out, "Unknown kb subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /kb <status|project|observe|concept> [args...]")
	}
	return nil
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

// kbProject handles /kb project <list|add NAME [DESC]|use ID>
func kbProject(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /kb project <list|add NAME [DESC]|use ID>")
		return nil
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
			if a.Config.CurrentProjectID == p.ID {
				active = " *"
			}
			fmt.Fprintf(out, "  [%d]%s %s  (%s)\n", p.ID, active, p.Name, p.Status)
			if p.Description != "" {
				fmt.Fprintf(out, "      %s\n", p.Description)
			}
		}
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /kb project add NAME [DESCRIPTION]")
			return nil
		}
		name := args[1]
		desc := strings.Join(args[2:], " ")
		id, err := a.KB.AddProject(name, desc)
		if err != nil {
			return err
		}
		a.Config.CurrentProjectID = id
		fmt.Fprintf(out, "Project %q added (id=%d) and set as current.\n", name, id)
	case "use":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /kb project use ID")
			return nil
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintf(out, "Invalid project ID: %s\n", args[1])
			return nil
		}
		a.Config.CurrentProjectID = id
		fmt.Fprintf(out, "Current project set to id=%d.\n", id)
	default:
		fmt.Fprintf(out, "Unknown project subcommand: %s\n", args[0])
	}
	return nil
}

// kbObserve handles /kb observe KIND BODY...
// KIND defaults to "note" if omitted or invalid.
func kbObserve(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /kb observe [KIND] TEXT")
		fmt.Fprintf(out, "Kinds: %s  (default: note)\n", strings.Join(ValidObservationKinds, ", "))
		return nil
	}
	if a.Config.CurrentProjectID == 0 {
		fmt.Fprintln(out, "No current project. Use /kb project add NAME or /kb project use ID first.")
		return nil
	}

	kind := "note"
	bodyArgs := args
	if isValidKind(strings.ToLower(args[0])) {
		kind = strings.ToLower(args[0])
		bodyArgs = args[1:]
	}
	if len(bodyArgs) == 0 {
		fmt.Fprintln(out, "Observation text is required.")
		return nil
	}
	body := strings.Join(bodyArgs, " ")
	id, err := a.KB.AddObservation(a.Config.CurrentProjectID, kind, body)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Observation recorded (id=%d, kind=%s).\n", id, kind)
	return nil
}

// kbConcept handles /kb concept <list|add NAME [DESC]>
func kbConcept(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /kb concept <list|add NAME [DESCRIPTION]>")
		return nil
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
			fmt.Fprintln(out, "Usage: /kb concept add NAME [DESCRIPTION]")
			return nil
		}
		name := args[1]
		desc := strings.Join(args[2:], " ")
		id, err := a.KB.AddConcept(name, desc)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Concept %q added (id=%d).\n", name, id)
	default:
		fmt.Fprintf(out, "Unknown concept subcommand: %s\n", args[0])
	}
	return nil
}

// ─── /files ──────────────────────────────────────────────────────────────────

func cmdFiles(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	entries, err := a.Workspace.ListDir(path)
	if err != nil {
		return fmt.Errorf("files: %w", err)
	}
	fmt.Fprintf(out, "\n  %s/\n", path)
	for _, e := range entries {
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		fmt.Fprintf(out, "    %s%s\n", e.Name(), suffix)
	}
	fmt.Fprintln(out)
	return nil
}
