package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
			Usage:       "/ollama <start|stop|status|list|ps|pull MODEL|show MODEL|logs|use MODEL|env>",
			Description: "Control the local Ollama service and manage models",
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
		"read": {
			Usage:       "/read FILE [FILE...]",
			Description: "Inject workspace file(s) into conversation context",
			Handler:     cmdRead,
		},
		"write": {
			Usage:       "/write PATH",
			Description: "Write the last assistant reply (or its first code block) to a file",
			Handler:     cmdWrite,
		},
		"run": {
			Usage:       "/run COMMAND [ARGS...]",
			Description: "Run a command in the workspace and inject its output into context",
			Handler:     cmdRun,
		},
		"search": {
			Usage:       "/search PATTERN [PATH]",
			Description: "Search workspace files for a pattern and inject matches into context",
			Handler:     cmdSearch,
		},
		"git": {
			Usage:       "/git <status|diff|log|show|blame> [ARGS...]",
			Description: "Run a read-only git command and inject its output into context",
			Handler:     cmdGit,
		},
		"apply": {
			Usage:       "/apply",
			Description: "Write tagged code blocks from the last reply to their named files",
			Handler:     cmdApply,
		},
		"summarize": {
			Usage:       "/summarize",
			Description: "Ask the LLM to summarize history and replace it with the summary",
			Handler:     cmdSummarize,
		},
		"compact": {
			Usage:       "/compact",
			Description: "Alias for /summarize — condense conversation history",
			Handler:     cmdSummarize,
		},
		"model": {
			Usage:       "/model [NAME | ollama://NAME | publicai.co://NAME]",
			Description: "List available models, or switch to a named model",
			Handler:     cmdModel,
		},
		"context": {
			Usage:       "/context <show|add TEXT...|clear>",
			Description: "Manage pinned context that survives /clear",
			Handler:     cmdContext,
		},
		"record": {
			Usage:       "/record <start [FILE]|stop|status>",
			Description: "Record session exchanges to a Markdown file",
			Handler:     cmdRecord,
		},
		"agent": {
			Usage:       "/agent <on|off|status>",
			Description: "Toggle agent mode: auto-apply files and auto-run suggested commands",
			Handler:     cmdAgent,
		},
		"session": {
			Usage:       "/session <list|load ID|new|name LABEL|status|continue FILE|replay FILE [OUTPUT]>",
			Description: "Manage sessions and replay Fountain recordings",
			Handler:     cmdSession,
		},
		"skill": {
			Usage:       "/skill <list|load NAME|info NAME|status>",
			Description: "List or load Agent Skills from the skill catalog",
			Handler:     cmdSkill,
		},
		"inspect": {
			Usage:       "/inspect [MODEL]",
			Description: "Show capability details for installed Ollama models; useful for multi-model routing",
			Handler:     cmdInspect,
		},
		"route": {
			Usage:       "/route <on FAST FULL | off | status>",
			Description: "Configure multi-model routing between a fast and a full Ollama model",
			Handler:     cmdRoute,
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
		"bye": {
			Usage:       "/bye",
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

	if name == "exit" || name == "quit" || name == "bye" {
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

func cmdHelp(a *Agent, args []string, out io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "skills", "skill":
			fmt.Fprint(out, FmtHelp(SkillsHelpText, "", "", "", ""))
			return nil
		default:
			fmt.Fprintf(out, "  Unknown help topic %q. Available topics: skills\n\n", args[0])
		}
	}
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
	if oc, ok := a.Client.(*OllamaClient); ok && len(a.History) > 0 {
		n, exact := CountTokens(context.Background(), oc.baseURL, oc.model, HistoryText(a.History))
		qualifier := "~"
		if exact {
			qualifier = ""
		}
		limit := a.Config.OllamaContextLength
		if limit > 0 {
			fmt.Fprintf(out, "Tokens:    %s%d / %d\n", qualifier, n, limit)
		} else {
			fmt.Fprintf(out, "Tokens:    %s%d\n", qualifier, n)
		}
	}
	if a.Router != nil {
		fmt.Fprintf(out, "Router:    %s → %s\n", a.Router.FastModel(), a.Router.FullModel())
	} else {
		fmt.Fprintln(out, "Router:    off")
	}
	if a.Workspace != nil {
		fmt.Fprintf(out, "Workspace: %s\n", a.Workspace.Root)
	}
	if a.KB != nil {
		fmt.Fprintln(out, "KB:        open (.harvey/knowledge.db)")
	} else {
		fmt.Fprintln(out, "KB:        not open")
	}
	if a.SM != nil && a.SessionID != 0 {
		s, err := a.SM.Load(a.SessionID)
		if err == nil && s != nil {
			name := s.Name
			if name == "" {
				name = "(unnamed)"
			}
			fmt.Fprintf(out, "Session:   #%d %s\n", s.ID, name)
		}
	} else {
		fmt.Fprintln(out, "Session:   none")
	}
	if a.Recorder != nil {
		fmt.Fprintf(out, "Recording: %s\n", a.Recorder.Path())
	} else {
		fmt.Fprintln(out, "Recording: off")
	}
	return nil
}

func cmdClear(a *Agent, _ []string, out io.Writer) error {
	a.ClearHistory()
	if a.SM != nil {
		model := ""
		if a.Client != nil {
			model = a.Client.Name()
		}
		id, err := a.SM.Create(a.Workspace.Root, model, a.History)
		if err != nil {
			fmt.Fprintf(out, "  Warning: could not create new session: %v\n", err)
		} else {
			a.SessionID = id
			fmt.Fprintf(out, "Conversation history cleared. New session #%d started.\n", id)
			return nil
		}
	}
	fmt.Fprintln(out, "Conversation history cleared.")
	return nil
}

func cmdRoute(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /route <on FAST FULL | off | status>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on":
		if len(args) < 3 {
			fmt.Fprintln(out, "Usage: /route on FAST_MODEL FULL_MODEL")
			return nil
		}
		cfg := RouterConfig{FastModel: args[1], FullModel: args[2]}
		r, err := NewRouter(cfg, a.Config.OllamaURL)
		if err != nil {
			return err
		}
		a.Router = r
		a.Config.Router = &cfg
		fmt.Fprintf(out, "Router enabled: %s → %s  (fast ctx budget: %d tokens)\n",
			cfg.FastModel, cfg.FullModel, r.smallCtxLen/4)
	case "off":
		a.Router = nil
		a.Config.Router = nil
		fmt.Fprintln(out, "Router disabled.")
	case "status":
		if a.Router == nil {
			fmt.Fprintln(out, "Router:    off")
			return nil
		}
		fmt.Fprintf(out, "Router:    on\n")
		fmt.Fprintf(out, "  fast:    %s\n", a.Router.FastModel())
		fmt.Fprintf(out, "  full:    %s\n", a.Router.FullModel())
		fmt.Fprintf(out, "  budget:  %d tokens (25%% of fast model context)\n", a.Router.smallCtxLen/4)
	default:
		fmt.Fprintf(out, "Unknown route subcommand: %s\n", args[0])
	}
	return nil
}

func cmdInspect(a *Agent, args []string, out io.Writer) error {
	oc, ok := a.Client.(*OllamaClient)
	if !ok {
		fmt.Fprintln(out, "Inspect requires an Ollama backend. Use /ollama start first.")
		return nil
	}
	ctx := context.Background()

	if len(args) > 0 {
		// Detail view for a single named model.
		detail, err := oc.ShowModel(ctx, args[0])
		if err != nil {
			return err
		}
		state := ""
		if detail.Running {
			state = " [loaded]"
		}
		fmt.Fprintf(out, "Model:        %s%s\n", detail.Name, state)
		fmt.Fprintf(out, "Family:       %s\n", detail.Family)
		fmt.Fprintf(out, "Parameters:   %s\n", detail.ParameterSize)
		fmt.Fprintf(out, "Quantization: %s\n", detail.Quantization)
		if detail.ContextLength > 0 {
			fmt.Fprintf(out, "Context:      %d tokens\n", detail.ContextLength)
		}
		if detail.SizeBytes > 0 {
			fmt.Fprintf(out, "Disk size:    %s\n", formatBytes(detail.SizeBytes))
		}
		if detail.RawParameters != "" {
			fmt.Fprintln(out, "\nModelfile parameters:")
			for _, line := range strings.Split(strings.TrimSpace(detail.RawParameters), "\n") {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
		return nil
	}

	// Summary table for all installed models.
	summaries, err := oc.ModelSummaries(ctx)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		fmt.Fprintln(out, "No models installed. Pull one with: /ollama pull <model>")
		return nil
	}

	const colFmt = "%-36s %-10s %-8s %-10s %-10s %6s\n"
	fmt.Fprintf(out, colFmt, "NAME", "FAMILY", "PARAMS", "QUANT", "SIZE", "STATE")
	fmt.Fprintf(out, colFmt,
		strings.Repeat("─", 36),
		strings.Repeat("─", 10),
		strings.Repeat("─", 8),
		strings.Repeat("─", 10),
		strings.Repeat("─", 10),
		strings.Repeat("─", 6),
	)
	for _, s := range summaries {
		state := ""
		if s.Running {
			state = "loaded"
		}
		fmt.Fprintf(out, colFmt,
			truncate(s.Name, 36),
			truncate(s.Family, 10),
			truncate(s.ParameterSize, 8),
			truncate(s.Quantization, 10),
			formatBytes(s.SizeBytes),
			state,
		)
	}
	fmt.Fprintf(out, "\nRun /inspect MODEL for context window size and Modelfile parameters.\n")
	return nil
}

// formatBytes converts a byte count to a human-readable string (GB / MB / KB).
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	default:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	}
}

// truncate shortens s to at most n runes, appending "…" if clipped.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func cmdOllama(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /ollama <start|stop|status|list|ps|pull MODEL|show MODEL|logs|use MODEL>")
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
			fmt.Fprintln(out, "No models installed. Run: /ollama pull <model>")
			return nil
		}
		for _, m := range models {
			marker := "  "
			if oc, ok := a.Client.(*OllamaClient); ok && oc.Model() == m {
				marker = "* "
			}
			fmt.Fprintf(out, "%s%s\n", marker, m)
		}
	case "ps":
		cmd := exec.Command("ollama", "ps")
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	case "pull":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama pull MODEL")
			return nil
		}
		cmd := exec.Command("ollama", "pull", args[1])
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama show MODEL")
			return nil
		}
		cmd := exec.Command("ollama", "show", args[1])
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	case "logs":
		// Try the native ollama logs subcommand first; fall back to journalctl.
		cmd := exec.Command("ollama", "logs")
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			jcmd := exec.Command("journalctl", "-u", "ollama", "--no-pager", "-n", "100")
			jcmd.Stdout = out
			jcmd.Stderr = out
			return jcmd.Run()
		}
		return nil
	case "env":
		// Show the Ollama-related environment variables currently in effect.
		vars := []string{
			"OLLAMA_HOST",
			"OLLAMA_MODELS",
			"OLLAMA_KEEP_ALIVE",
			"OLLAMA_NUM_THREAD",
			"OLLAMA_NUM_PARALLEL",
			"OLLAMA_MAX_LOADED_MODELS",
			"OLLAMA_CONTEXT_LENGTH",
			"OLLAMA_MAX_QUEUE",
			"OLLAMA_FLASH_ATTENTION",
			"OLLAMA_DEBUG",
		}
		fmt.Fprintln(out, "Ollama environment (Harvey process):")
		for _, k := range vars {
			v := os.Getenv(k)
			if v == "" {
				v = dim("(not set)")
			}
			fmt.Fprintf(out, "  %-28s %s\n", k, v)
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

// ─── /record ─────────────────────────────────────────────────────────────────

func cmdRecord(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /record <start [FILE]|stop|status>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "start":
		if a.Recorder != nil {
			fmt.Fprintf(out, "Already recording to %s. Use /record stop first.\n", a.Recorder.Path())
			return nil
		}
		path := ""
		if len(args) >= 2 {
			path = args[1]
		} else {
			ws := "."
			if a.Workspace != nil {
				ws = a.Workspace.Root
			}
			path = DefaultSessionPath(ws)
		}
		model := "none"
		if a.Client != nil {
			model = a.Client.Name()
		}
		ws := "."
		if a.Workspace != nil {
			ws = a.Workspace.Root
		}
		r, err := NewRecorder(path, model, ws)
		if err != nil {
			return err
		}
		a.Recorder = r
		fmt.Fprintf(out, "Recording started: %s\n", path)
	case "stop":
		if a.Recorder == nil {
			fmt.Fprintln(out, "Not currently recording.")
			return nil
		}
		path := a.Recorder.Path()
		if err := a.Recorder.Close(); err != nil {
			return err
		}
		a.Recorder = nil
		fmt.Fprintf(out, "Recording stopped. Session saved to %s\n", path)
	case "status":
		if a.Recorder != nil {
			fmt.Fprintf(out, "Recording to: %s\n", a.Recorder.Path())
		} else {
			fmt.Fprintln(out, "Not recording.")
		}
	default:
		fmt.Fprintf(out, "Unknown record subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /record <start [FILE]|stop|status>")
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

// ─── /read ───────────────────────────────────────────────────────────────────

// cmdRead reads one or more workspace files and injects their contents into
// the conversation as a user-role context message.
func cmdRead(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /read FILE [FILE...]")
		return nil
	}

	var sb strings.Builder
	sb.WriteString("[context: /read")
	for _, f := range args {
		sb.WriteString(" " + f)
	}
	sb.WriteString("]\n")

	ok := 0
	for _, rel := range args {
		data, err := a.Workspace.ReadFile(rel)
		if err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", rel, err)
			continue
		}
		fmt.Fprintf(out, "  ✓ %s (%d bytes)\n", rel, len(data))
		sb.WriteString("\n```" + rel + "\n")
		sb.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			sb.WriteByte('\n')
		}
		sb.WriteString("```\n")
		ok++
	}

	if ok == 0 {
		return nil
	}
	a.AddMessage("user", sb.String())
	fmt.Fprintf(out, "  %d file(s) added to context.\n", ok)
	return nil
}

// ─── /write ──────────────────────────────────────────────────────────────────

// cmdWrite writes the last assistant reply to a workspace file. If the reply
// contains a fenced code block the first such block is extracted; otherwise
// the full reply text is written.
func cmdWrite(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /write PATH")
		return nil
	}
	dest := args[0]

	// Find the last assistant message.
	var reply string
	for i := len(a.History) - 1; i >= 0; i-- {
		if a.History[i].Role == "assistant" {
			reply = a.History[i].Content
			break
		}
	}
	if reply == "" {
		fmt.Fprintln(out, "No assistant reply in history to write.")
		return nil
	}

	content, ok := extractCodeBlock(reply)
	if !ok {
		content = reply
	}

	if err := a.Workspace.WriteFile(dest, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	source := "full reply"
	if ok {
		source = "first code block"
	}
	fmt.Fprintf(out, "  ✓ Wrote %s to %s (%d bytes)\n", source, dest, len(content))
	return nil
}

// extractCodeBlock finds the first fenced code block (``` ... ```) in text
// and returns its contents without the fence lines. Returns ("", false) if
// no fenced block is found.
func extractCodeBlock(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	inBlock := false
	var buf strings.Builder
	for _, line := range lines {
		if !inBlock {
			if strings.HasPrefix(line, "```") {
				inBlock = true
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			return buf.String(), true
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return "", false
}

// ─── /run ────────────────────────────────────────────────────────────────────

// maxRunOutput is the maximum number of bytes of command output injected into
// context. Output beyond this is truncated to protect the context window.
const maxRunOutput = 8000

// cmdRun executes a shell command inside the workspace root, captures combined
// stdout+stderr, and injects the result into the conversation as a user-role
// context message.
func cmdRun(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /run COMMAND [ARGS...]")
		return nil
	}

	cmdLine := strings.Join(args, " ")
	fmt.Fprintf(out, "  $ %s\n", cmdLine)

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = a.Workspace.Root
	raw, _ := cmd.CombinedOutput() // error reflected via exit code note below

	truncated := false
	output := raw
	if len(output) > maxRunOutput {
		output = output[:maxRunOutput]
		truncated = true
	}

	exitNote := ""
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0 {
		exitNote = fmt.Sprintf(" (exit %d)", cmd.ProcessState.ExitCode())
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[context: /run %s%s]\n\n```\n", cmdLine, exitNote))
	sb.Write(output)
	if truncated {
		sb.WriteString("\n... (output truncated)")
	}
	sb.WriteString("\n```\n")

	a.AddMessage("user", sb.String())
	fmt.Fprintf(out, "  %d bytes of output added to context%s.\n", len(output), exitNote)
	return nil
}

// ─── /search ─────────────────────────────────────────────────────────────────

// maxSearchMatches is the maximum number of matching lines injected into context.
const maxSearchMatches = 100

// cmdSearch searches workspace files for a regexp pattern and injects matches
// into the conversation as a user-role context message.
func cmdSearch(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /search PATTERN [PATH]")
		return nil
	}

	re, err := regexp.Compile(args[0])
	if err != nil {
		return fmt.Errorf("search: invalid pattern: %w", err)
	}
	searchRoot := "."
	if len(args) > 1 {
		searchRoot = args[1]
	}
	absRoot, err := a.Workspace.AbsPath(searchRoot)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	type match struct {
		file string
		line int
		text string
	}
	var matches []match
	truncated := false

	filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || truncated {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinary(data) {
			return nil
		}
		rel, _ := filepath.Rel(a.Workspace.Root, path)
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			if re.MatchString(scanner.Text()) {
				matches = append(matches, match{rel, lineNum, scanner.Text()})
				if len(matches) >= maxSearchMatches {
					truncated = true
					return fs.SkipAll
				}
			}
		}
		return nil
	})

	if len(matches) == 0 {
		fmt.Fprintf(out, "  No matches for %q\n", args[0])
		return nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[context: /search %s]\n\n", strings.Join(args, " ")))
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("%s:%d: %s\n", m.file, m.line, m.text))
	}
	if truncated {
		sb.WriteString(fmt.Sprintf("... (results truncated at %d matches)\n", maxSearchMatches))
	}

	a.AddMessage("user", sb.String())
	fmt.Fprintf(out, "  %d match(es) for %q added to context", len(matches), args[0])
	if truncated {
		fmt.Fprint(out, " (truncated)")
	}
	fmt.Fprintln(out)
	return nil
}

// isBinary reports whether data appears to be a binary (non-text) file by
// looking for null bytes in the first 512 bytes.
func isBinary(data []byte) bool {
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}

// ─── /git ────────────────────────────────────────────────────────────────────

// gitAllowedSubcmds is the set of read-only git subcommands /git will run.
var gitAllowedSubcmds = map[string]bool{
	"status": true,
	"diff":   true,
	"log":    true,
	"show":   true,
	"blame":  true,
}

// cmdGit runs a read-only git subcommand in the workspace root and injects
// the output into the conversation as a user-role context message.
func cmdGit(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /git <status|diff|log|show|blame> [ARGS...]")
		return nil
	}

	sub := strings.ToLower(args[0])
	if !gitAllowedSubcmds[sub] {
		fmt.Fprintf(out, "  /git only supports read-only subcommands: status, diff, log, show, blame\n")
		return nil
	}

	gitArgs := append([]string{sub}, args[1:]...)
	cmdLine := "git " + strings.Join(gitArgs, " ")
	fmt.Fprintf(out, "  $ %s\n", cmdLine)

	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = a.Workspace.Root
	raw, _ := cmd.CombinedOutput()

	if len(raw) == 0 {
		fmt.Fprintln(out, "  (no output)")
		return nil
	}

	truncated := false
	output := raw
	if len(output) > maxRunOutput {
		output = output[:maxRunOutput]
		truncated = true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[context: /git %s]\n\n```\n", strings.Join(gitArgs, " ")))
	sb.Write(output)
	if truncated {
		sb.WriteString("\n... (output truncated)")
	}
	sb.WriteString("\n```\n")

	a.AddMessage("user", sb.String())
	fmt.Fprintf(out, "  %d bytes of output added to context.\n", len(output))
	return nil
}

// ─── /apply ──────────────────────────────────────────────────────────────────

// taggedBlock is a fenced code block whose opening fence names a target file.
type taggedBlock struct {
	path    string
	content string
}

// findTaggedBlocks scans text for fenced code blocks whose opening fence line
// includes a token that looks like a file path, and returns each as a
// taggedBlock. Two formats are supported:
//
//   - Space-separated:  ```go harvey/spinner.go
//   - Colon-separated:  ```bash:testout/hello.bash
//
// In both cases the language hint is stripped and only the path is stored.
func findTaggedBlocks(text string) []taggedBlock {
	var blocks []taggedBlock
	lines := strings.Split(text, "\n")
	var cur *taggedBlock
	for _, line := range lines {
		if cur == nil {
			if strings.HasPrefix(line, "```") {
				fence := strings.TrimSpace(strings.TrimPrefix(line, "```"))
				path := fencePathToken(fence)
				if path != "" {
					cur = &taggedBlock{path: path}
				}
			}
		} else {
			if strings.HasPrefix(line, "```") {
				blocks = append(blocks, *cur)
				cur = nil
			} else {
				cur.content += line + "\n"
			}
		}
	}
	return blocks
}

// fencePathToken extracts a file path from a fenced-code-block opening line's
// content (the text after the triple backtick). It handles two conventions:
//
//   - "lang path"  (space-separated, e.g. "go harvey/spinner.go")
//   - "lang:path"  (colon-separated, e.g. "bash:testout/hello.bash")
//
// Returns the path token, or "" if none is found.
func fencePathToken(fence string) string {
	// Colon-separated: treat everything after the first colon as the path.
	if idx := strings.IndexByte(fence, ':'); idx >= 0 {
		candidate := fence[idx+1:]
		if looksLikePath(candidate) {
			return candidate
		}
	}
	// Space-separated: find the first token that looks like a path.
	for _, tok := range strings.Fields(fence) {
		if looksLikePath(tok) {
			return tok
		}
	}
	return ""
}

// looksLikePath reports whether s looks like a file path rather than a
// language identifier. A token is treated as a path if it contains a
// directory separator or ends with a recognised file extension.
func looksLikePath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	knownExts := []string{
		".go", ".ts", ".js", ".py", ".rb", ".md", ".txt",
		".json", ".yaml", ".yml", ".sh", ".sql", ".html",
		".css", ".toml", ".mod", ".sum", ".env",
	}
	for _, ext := range knownExts {
		if strings.HasSuffix(s, ext) {
			return true
		}
	}
	return false
}

// cmdApply finds tagged code blocks in the last assistant reply and writes
// each one to its named file in the workspace, prompting the user once before
// making any changes.
func cmdApply(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}

	var reply string
	for i := len(a.History) - 1; i >= 0; i-- {
		if a.History[i].Role == "assistant" {
			reply = a.History[i].Content
			break
		}
	}
	if reply == "" {
		fmt.Fprintln(out, "No assistant reply in history.")
		return nil
	}

	blocks := findTaggedBlocks(reply)
	if len(blocks) == 0 {
		fmt.Fprintln(out, "  No tagged code blocks found in last reply.")
		fmt.Fprintln(out, "  Tag a block with its target path, e.g.: ```go harvey/spinner.go")
		return nil
	}

	fmt.Fprintf(out, "  Found %d tagged block(s):\n", len(blocks))
	for _, b := range blocks {
		fmt.Fprintf(out, "    %s (%d bytes)\n", b.path, len(b.content))
	}

	fmt.Fprint(out, "  Apply all? [Y/n] ")
	scanner := bufio.NewScanner(a.In)
	answer := ""
	if scanner.Scan() {
		answer = strings.ToLower(strings.TrimSpace(scanner.Text()))
	}
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "  Aborted.")
		return nil
	}

	for _, b := range blocks {
		if err := a.Workspace.WriteFile(b.path, []byte(b.content), 0o644); err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", b.path, err)
		} else {
			fmt.Fprintf(out, "  ✓ %s\n", b.path)
		}
	}
	return nil
}

// ─── /summarize ──────────────────────────────────────────────────────────────

// summarizePrompt is appended to the history when requesting a summary.
const summarizePrompt = "Please summarize this conversation concisely. Capture the key topics discussed, files mentioned, code changes proposed or made, and any open questions or next steps. This summary will replace the full conversation history to keep the context window manageable."

// cmdSummarize asks the connected LLM to condense the conversation history
// into a single summary message, then replaces the history with that summary.
func cmdSummarize(a *Agent, args []string, out io.Writer) error {
	if a.Client == nil {
		fmt.Fprintln(out, "No backend connected. Use /ollama start or /publicai connect.")
		return nil
	}

	// Count non-system messages to decide if there's anything worth summarising.
	meaningful := 0
	for _, m := range a.History {
		if m.Role != "system" {
			meaningful++
		}
	}
	if meaningful < 2 {
		fmt.Fprintln(out, "Not enough conversation history to summarize.")
		return nil
	}

	request := append(append([]Message(nil), a.History...),
		Message{Role: "user", Content: summarizePrompt})

	fmt.Fprintln(out)
	var buf strings.Builder
	sp := newSpinner(out, 0)
	_, chatErr := a.Client.Chat(context.Background(), request, &buf)
	sp.stop()

	if chatErr != nil {
		return fmt.Errorf("summarize: %w", chatErr)
	}
	summary := strings.TrimSpace(buf.String())
	if summary == "" {
		fmt.Fprintln(out, "  Received empty summary — history unchanged.")
		return nil
	}

	// Replace history: system prompt + pinned context + summary.
	a.ClearHistory()
	a.AddMessage("user", "[Conversation summary]\n\n"+summary)
	fmt.Fprintf(out, "  History condensed to %d chars.\n", len(summary))
	return nil
}

// ─── /context ────────────────────────────────────────────────────────────────

// cmdContext manages the agent's PinnedContext: text that persists across
// /clear and is re-injected into history after the system prompt.
func cmdContext(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 || strings.ToLower(args[0]) == "show" {
		if a.PinnedContext == "" {
			fmt.Fprintln(out, "  (pinned context is empty)")
		} else {
			fmt.Fprintf(out, "  Pinned context (%d chars):\n\n%s\n", len(a.PinnedContext), a.PinnedContext)
		}
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "clear":
		a.PinnedContext = ""
		// Remove any existing pinned context message from history.
		filtered := a.History[:0]
		for _, m := range a.History {
			if !(m.Role == "user" && strings.HasPrefix(m.Content, "[pinned context]")) {
				filtered = append(filtered, m)
			}
		}
		a.History = filtered
		fmt.Fprintln(out, "  Pinned context cleared.")

	case "add":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /context add TEXT...")
			return nil
		}
		text := strings.Join(args[1:], " ")
		if a.PinnedContext == "" {
			a.PinnedContext = text
		} else {
			a.PinnedContext += "\n" + text
		}
		// Update or insert the pinned context message in history.
		updated := false
		for i, m := range a.History {
			if m.Role == "user" && strings.HasPrefix(m.Content, "[pinned context]") {
				a.History[i].Content = "[pinned context]\n\n" + a.PinnedContext
				updated = true
				break
			}
		}
		if !updated {
			a.AddMessage("user", "[pinned context]\n\n"+a.PinnedContext)
		}
		fmt.Fprintf(out, "  Pinned context updated (%d chars).\n", len(a.PinnedContext))

	default:
		fmt.Fprintf(out, "Unknown context subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /context <show|add TEXT...|clear>")
	}
	return nil
}

// ─── /session ────────────────────────────────────────────────────────────────

/** cmdSession manages Harvey's persistent conversation sessions and Fountain
 * recording playback.
 *
 * Subcommands:
 *   list              — list all sessions, most-recent first.
 *   load ID           — restore history from session ID into the current conversation.
 *   new               — clear history and start a fresh session row.
 *   name LABEL        — assign a human-readable label to the current session.
 *   status            — show the current session ID and name.
 *   continue FILE     — load chat history from a Fountain file and continue in REPL.
 *   replay FILE [OUT] — re-send turns from a Fountain file to the current backend
 *                       and record fresh responses to OUT (default: auto-named).
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   args ([]string)  — subcommand and its arguments.
 *   out  (io.Writer) — destination for command output.
 *
 * Returns:
 *   error — on database or I/O failure.
 *
 * Example:
 *   /session list
 *   /session continue old-session.fountain
 *   /session replay old-session.fountain new-session.fountain
 */
func cmdSession(a *Agent, args []string, out io.Writer) error {
	// continue and replay don't require the session manager.
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "continue":
			if len(args) < 2 {
				fmt.Fprintln(out, "Usage: /session continue FILE")
				return nil
			}
			n, err := a.ContinueFromFountain(args[1])
			if err != nil {
				fmt.Fprintf(out, "  ✗ %v\n", err)
				return nil
			}
			fmt.Fprintf(out, green("✓")+" Loaded %d turns from %s\n", n, args[1])
			return nil
		case "replay":
			if len(args) < 2 {
				fmt.Fprintln(out, "Usage: /session replay FILE [OUTPUT]")
				return nil
			}
			src := args[1]
			outPath := ""
			if len(args) >= 3 {
				outPath = args[2]
			} else {
				outPath = DefaultSessionPath(a.Workspace.Root)
			}
			if a.Client == nil {
				fmt.Fprintln(out, "  No backend connected. Use /ollama start or /publicai connect.")
				return nil
			}
			return a.ReplayFromFountain(context.Background(), src, outPath, out)
		}
	}

	if a.SM == nil {
		fmt.Fprintln(out, "Session manager is not available.")
		return nil
	}
	if len(args) == 0 || strings.ToLower(args[0]) == "status" {
		return sessionStatus(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "list":
		return sessionList(a, out)
	case "load":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /session load ID")
			return nil
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintf(out, "Invalid session ID: %s\n", args[1])
			return nil
		}
		return sessionLoad(a, id, out)
	case "new":
		return sessionNew(a, out)
	case "name":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /session name LABEL")
			return nil
		}
		label := strings.Join(args[1:], " ")
		return sessionName(a, label, out)
	default:
		fmt.Fprintf(out, "Unknown session subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /session <list|load ID|new|name LABEL|status|continue FILE|replay FILE [OUTPUT]>")
	}
	return nil
}

func sessionStatus(a *Agent, out io.Writer) error {
	if a.SessionID == 0 {
		fmt.Fprintln(out, "  No active session.")
		return nil
	}
	s, err := a.SM.Load(a.SessionID)
	if err != nil {
		return err
	}
	if s == nil {
		fmt.Fprintln(out, "  No active session.")
		return nil
	}
	name := s.Name
	if name == "" {
		name = "(unnamed)"
	}
	turns := 0
	for _, m := range s.History {
		if m.Role == "user" {
			turns++
		}
	}
	fmt.Fprintf(out, "  Session #%d  %s\n", s.ID, name)
	fmt.Fprintf(out, "  Model:      %s\n", s.Model)
	fmt.Fprintf(out, "  Turns:      %d\n", turns)
	fmt.Fprintf(out, "  Created:    %s\n", s.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Fprintf(out, "  Last saved: %s\n", s.LastActive.Format("2006-01-02 15:04"))
	return nil
}

func sessionList(a *Agent, out io.Writer) error {
	sessions, err := a.SM.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(out, "  (no sessions)")
		return nil
	}
	fmt.Fprintln(out)
	for _, s := range sessions {
		active := "  "
		if s.ID == a.SessionID {
			active = "* "
		}
		name := s.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(out, "  %s[%d] %-28s  %s  %s\n",
			active, s.ID, name, s.Model, s.LastActive.Format("2006-01-02 15:04"))
	}
	fmt.Fprintln(out)
	return nil
}

func sessionLoad(a *Agent, id int64, out io.Writer) error {
	s, err := a.SM.Load(id)
	if err != nil {
		return err
	}
	if s == nil {
		fmt.Fprintf(out, "  Session %d not found.\n", id)
		return nil
	}
	// Keep the current system prompt fresh.
	currentSys := ""
	for _, m := range a.History {
		if m.Role == "system" {
			currentSys = m.Content
			break
		}
	}
	a.History = s.History
	if currentSys != "" {
		replaced := false
		for i, m := range a.History {
			if m.Role == "system" {
				a.History[i].Content = currentSys
				replaced = true
				break
			}
		}
		if !replaced {
			a.History = append([]Message{{Role: "system", Content: currentSys}}, a.History...)
		}
	}
	a.SessionID = s.ID
	name := s.Name
	if name == "" {
		name = fmt.Sprintf("#%d", s.ID)
	}
	fmt.Fprintf(out, "  Loaded session %s (%d messages).\n", name, len(a.History))
	return nil
}

func sessionNew(a *Agent, out io.Writer) error {
	a.ClearHistory()
	model := ""
	if a.Client != nil {
		model = a.Client.Name()
	}
	workspace := ""
	if a.Workspace != nil {
		workspace = a.Workspace.Root
	}
	id, err := a.SM.Create(workspace, model, a.History)
	if err != nil {
		return err
	}
	a.SessionID = id
	fmt.Fprintf(out, "  History cleared. New session #%d started.\n", id)
	return nil
}

func sessionName(a *Agent, label string, out io.Writer) error {
	if a.SessionID == 0 {
		fmt.Fprintln(out, "  No active session to rename.")
		return nil
	}
	if err := a.SM.Rename(a.SessionID, label); err != nil {
		return err
	}
	fmt.Fprintf(out, "  Session #%d named %q.\n", a.SessionID, label)
	return nil
}

// ─── /skill ──────────────────────────────────────────────────────────────────

/** cmdSkill lists or loads Agent Skills from the catalog discovered at startup.
 *
 * Subcommands:
 *   list           — list all available skills with name and description.
 *   load NAME      — inject the full skill body into the conversation as context.
 *   info NAME      — show path, source, license, and compatibility for a skill.
 *   status         — show total skill count broken down by scope.
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   args ([]string)  — subcommand and its arguments.
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — always nil (errors are reported inline).
 *
 * Example:
 *   /skill list
 *   /skill load go-review
 *   /skill info go-review
 */
func cmdSkill(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 || strings.ToLower(args[0]) == "list" {
		return skillList(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "load":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /skill load NAME")
			return nil
		}
		return skillLoad(a, args[1], out)
	case "info":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /skill info NAME")
			return nil
		}
		return skillInfo(a, args[1], out)
	case "status":
		return skillStatus(a, out)
	default:
		fmt.Fprintf(out, "Unknown skill subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /skill <list|load NAME|info NAME|status>")
	}
	return nil
}

func skillList(a *Agent, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills discovered. See /help skills for setup instructions.")
		return nil
	}
	names := make([]string, 0, len(a.Skills))
	for n := range a.Skills {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(out)
	for _, n := range names {
		s := a.Skills[n]
		fmt.Fprintf(out, "  %-28s [%s]\n", n, s.Source)
		fmt.Fprintf(out, "    %s\n", s.Description)
	}
	fmt.Fprintln(out)
	return nil
}

func skillLoad(a *Agent, name string, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills available. See /help skills for setup instructions.")
		return nil
	}
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	if skill.Body == "" {
		fmt.Fprintf(out, "  Skill %q has no body content.\n", name)
		return nil
	}
	a.AddMessage("user", fmt.Sprintf("[skill: %s]\n\n%s", name, skill.Body))
	if a.Recorder != nil {
		_ = a.Recorder.RecordSkillLoad(name, skill.Description, skill.Body)
	}
	fmt.Fprintf(out, "  ✓ Skill %q loaded into context (%d chars).\n", name, len(skill.Body))
	return nil
}

func skillInfo(a *Agent, name string, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills available.")
		return nil
	}
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	fmt.Fprintf(out, "  Name:          %s\n", skill.Name)
	fmt.Fprintf(out, "  Description:   %s\n", skill.Description)
	fmt.Fprintf(out, "  Source:        %s\n", skill.Source)
	fmt.Fprintf(out, "  Path:          %s\n", skill.Path)
	if skill.License != "" {
		fmt.Fprintf(out, "  License:       %s\n", skill.License)
	}
	if skill.Compatibility != "" {
		fmt.Fprintf(out, "  Compatibility: %s\n", skill.Compatibility)
	}
	if len(skill.Metadata) > 0 {
		keys := make([]string, 0, len(skill.Metadata))
		for k := range skill.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(out, "  Metadata:")
		for _, k := range keys {
			fmt.Fprintf(out, "    %s: %s\n", k, skill.Metadata[k])
		}
	}
	return nil
}

func skillStatus(a *Agent, out io.Writer) error {
	if len(a.Skills) == 0 {
		fmt.Fprintln(out, "  No skills discovered. See /help skills for setup instructions.")
		return nil
	}
	proj, user := 0, 0
	for _, s := range a.Skills {
		if s.Source == SkillSourceProject {
			proj++
		} else {
			user++
		}
	}
	fmt.Fprintf(out, "  Total: %d skill(s)\n", len(a.Skills))
	if proj > 0 {
		fmt.Fprintf(out, "    Project scope: %d\n", proj)
	}
	if user > 0 {
		fmt.Fprintf(out, "    User scope:    %d\n", user)
	}
	return nil
}

// ─── /model ──────────────────────────────────────────────────────────────────

/** cmdModel lists available models from all reachable backends, or switches
 * the active model when a name is supplied.
 *
 * Usage:
 *   /model                     — list all models from every connected backend.
 *   /model NAME                — switch to NAME; error if ambiguous across backends.
 *   /model ollama://NAME       — switch Ollama to NAME.
 *   /model publicai.co://NAME  — switch publicai.co to NAME.
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   args ([]string)  — optional model name with optional backend prefix.
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — on backend communication failure.
 *
 * Example:
 *   /model
 *   /model mistral:latest
 *   /model ollama://mistral:latest
 */
func cmdModel(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return modelList(a, out)
	}
	return modelSwitch(a, args[0], out)
}

// modelList prints all models from every reachable backend, grouped by backend.
// The currently active model is marked with *.
func modelList(a *Agent, out io.Writer) error {
	ctx := context.Background()
	found := false

	// Ollama
	if ProbeOllama(a.Config.OllamaURL) {
		models, err := NewOllamaClient(a.Config.OllamaURL, "").Models(ctx)
		if err == nil {
			found = true
			activeModel := ""
			if oc, ok := a.Client.(*OllamaClient); ok {
				activeModel = oc.Model()
			}
			fmt.Fprintln(out, "  Ollama:")
			if len(models) == 0 {
				fmt.Fprintln(out, "    (no models installed — run: ollama pull <model>)")
			}
			for _, m := range models {
				marker := "  "
				if m == activeModel {
					marker = "* "
				}
				fmt.Fprintf(out, "    %s%s\n", marker, m)
			}
		}
	}

	// publicai.co
	if a.Config.PublicAIKey != "" {
		pc := NewPublicAIClient(a.Config.PublicAIURL, a.Config.PublicAIKey, a.Config.PublicAIModel)
		models, err := pc.Models(ctx)
		if err == nil {
			found = true
			activeModel := ""
			if ac, ok := a.Client.(*PublicAIClient); ok {
				activeModel = ac.Model()
			}
			fmt.Fprintln(out, "  publicai.co:")
			for _, m := range models {
				marker := "  "
				if m == activeModel {
					marker = "* "
				}
				fmt.Fprintf(out, "    %s%s\n", marker, m)
			}
		}
	}

	if !found {
		fmt.Fprintln(out, "  No backends available. Start Ollama or set PUBLICAI_API_KEY and use /publicai connect.")
	}
	return nil
}

// modelSwitch changes the active model. The name may carry a backend prefix
// ("ollama://" or "publicai.co://") to force a specific backend. Without a
// prefix the model name is matched against all reachable backends; if the name
// is found in exactly one backend that backend is activated, otherwise the user
// is prompted to use a prefix.
func modelSwitch(a *Agent, name string, out io.Writer) error {
	ctx := context.Background()

	const ollamaPrefix = "ollama://"
	const publicaiPrefix = "publicai.co://"

	switch {
	case strings.HasPrefix(name, ollamaPrefix):
		model := strings.TrimPrefix(name, ollamaPrefix)
		if !ProbeOllama(a.Config.OllamaURL) {
			fmt.Fprintln(out, "  Ollama is not running. Use /ollama start first.")
			return nil
		}
		a.Config.OllamaModel = model
		a.Client = NewOllamaClient(a.Config.OllamaURL, model)
		fmt.Fprintf(out, "  Switched to Ollama model: %s\n", model)

	case strings.HasPrefix(name, publicaiPrefix):
		model := strings.TrimPrefix(name, publicaiPrefix)
		if a.Config.PublicAIKey == "" {
			fmt.Fprintln(out, "  PUBLICAI_API_KEY is not set.")
			return nil
		}
		a.Config.PublicAIModel = model
		a.Client = NewPublicAIClient(a.Config.PublicAIURL, a.Config.PublicAIKey, model)
		fmt.Fprintf(out, "  Switched to publicai.co model: %s\n", model)

	default:
		// No prefix — search all backends.
		var inOllama, inPublicAI bool

		if ProbeOllama(a.Config.OllamaURL) {
			models, err := NewOllamaClient(a.Config.OllamaURL, "").Models(ctx)
			if err == nil {
				for _, m := range models {
					if m == name {
						inOllama = true
						break
					}
				}
			}
		}
		if a.Config.PublicAIKey != "" {
			pc := NewPublicAIClient(a.Config.PublicAIURL, a.Config.PublicAIKey, a.Config.PublicAIModel)
			models, err := pc.Models(ctx)
			if err == nil {
				for _, m := range models {
					if m == name {
						inPublicAI = true
						break
					}
				}
			}
		}

		switch {
		case inOllama && inPublicAI:
			fmt.Fprintf(out, "  %q exists in both backends. Use a prefix to disambiguate:\n", name)
			fmt.Fprintf(out, "    /model ollama://%s\n", name)
			fmt.Fprintf(out, "    /model publicai.co://%s\n", name)
		case inOllama:
			a.Config.OllamaModel = name
			a.Client = NewOllamaClient(a.Config.OllamaURL, name)
			fmt.Fprintf(out, "  Switched to Ollama model: %s\n", name)
		case inPublicAI:
			a.Config.PublicAIModel = name
			a.Client = NewPublicAIClient(a.Config.PublicAIURL, a.Config.PublicAIKey, name)
			fmt.Fprintf(out, "  Switched to publicai.co model: %s\n", name)
		default:
			fmt.Fprintf(out, "  Model %q not found in any available backend.\n", name)
			fmt.Fprintln(out, "  Use /model to list available models.")
		}
	}
	return nil
}

// ─── /agent ──────────────────────────────────────────────────────────────────

func cmdAgent(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 || strings.ToLower(args[0]) == "status" {
		if a.AgentMode {
			fmt.Fprintln(out, "  Agent mode: on  (tagged files are auto-applied; /run hints are auto-executed)")
		} else {
			fmt.Fprintln(out, "  Agent mode: off  (tagged files are auto-applied; /run hints require manual /run)")
		}
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on":
		a.AgentMode = true
		fmt.Fprintln(out, "  Agent mode on. Tagged files will be auto-applied; backtick /run hints will be auto-executed.")
	case "off":
		a.AgentMode = false
		fmt.Fprintln(out, "  Agent mode off. Tagged files still auto-applied; use /run for commands.")
	default:
		fmt.Fprintf(out, "Unknown agent subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /agent <on|off|status>")
	}
	return nil
}

// ─── auto-execute ─────────────────────────────────────────────────────────────

// extractRunSuggestions scans text for backtick-wrapped /run commands that the
// LLM suggests the user should run (e.g. "`/run mkdir testout`") and returns
// each as a slice of arguments (the command and its arguments, without "run").
//
// Example:
//
//	cmds := extractRunSuggestions("Try `/run mkdir testout` first.")
//	// cmds == [["mkdir", "testout"]]
func extractRunSuggestions(text string) [][]string {
	re := regexp.MustCompile("`/run ([^`]+)`")
	matches := re.FindAllStringSubmatch(text, -1)
	var cmds [][]string
	for _, m := range matches {
		parts := strings.Fields(m[1])
		if len(parts) > 0 {
			cmds = append(cmds, parts)
		}
	}
	return cmds
}

// actionChoice represents the user's decision at an action confirmation prompt.
type actionChoice int

const (
	actionYes  actionChoice = iota // execute this action
	actionNo                       // skip this action
	actionAll                      // execute this and all remaining actions without prompting
	actionQuit                     // skip this and all remaining actions
)

// promptAction displays a box-drawing preview of a proposed action and reads
// the user's choice. Returns actionYes for empty input (Enter = yes).
//
// Parameters:
//
//	r       (*bufio.Reader) — reads the user's single-key response.
//	out     (io.Writer)     — destination for the preview box.
//	header  (string)        — short label shown in the box title (e.g. "Write: path/to/file").
//	preview (string)        — content preview shown inside the box; empty = no body.
//
// Returns:
//
//	actionChoice — the user's decision.
func promptAction(r *bufio.Reader, out io.Writer, header, preview string) actionChoice {
	const boxWidth = 56
	const maxPreviewLines = 8

	// Top border
	title := "  ┌─ " + header + " "
	pad := boxWidth - len(title) + 2
	if pad < 1 {
		pad = 1
	}
	fmt.Fprint(out, title+strings.Repeat("─", pad)+"┐\n")

	// Preview lines
	if preview != "" {
		lines := strings.Split(strings.TrimRight(preview, "\n"), "\n")
		for i, line := range lines {
			if i >= maxPreviewLines {
				fmt.Fprintf(out, "  │  %s… (%d more lines)\n", "", len(lines)-maxPreviewLines)
				break
			}
			fmt.Fprintf(out, "  │  %s\n", line)
		}
	}

	// Bottom border + prompt
	fmt.Fprintf(out, "  └%s┘\n", strings.Repeat("─", boxWidth-1))
	fmt.Fprint(out, "  [y]es  [n]o  [A]ll  [q]uit > ")

	line, _ := r.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "n", "no":
		return actionNo
	case "a", "all":
		return actionAll
	case "q", "quit":
		return actionQuit
	default: // "", "y", "yes" — Enter defaults to yes
		return actionYes
	}
}

// cmdRunCtx is like cmdRun but uses a context so the spawned process can be
// cancelled (e.g. via Ctrl+C). Output is injected into conversation context.
func cmdRunCtx(a *Agent, ctx context.Context, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 {
		return nil
	}

	cmdLine := strings.Join(args, " ")
	fmt.Fprintf(out, "  $ %s\n", cmdLine)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = a.Workspace.Root
	raw, _ := cmd.CombinedOutput()

	truncated := false
	output := raw
	if len(output) > maxRunOutput {
		output = output[:maxRunOutput]
		truncated = true
	}

	exitNote := ""
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0 {
		exitNote = fmt.Sprintf(" (exit %d)", cmd.ProcessState.ExitCode())
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[context: /run %s%s]\n\n```\n", cmdLine, exitNote))
	sb.Write(output)
	if truncated {
		sb.WriteString("\n... (output truncated)")
	}
	sb.WriteString("\n```\n")

	a.AddMessage("user", sb.String())
	fmt.Fprintf(out, "  %d bytes of output added to context%s.\n", len(output), exitNote)
	return nil
}

/** autoExecuteReply scans reply for actionable content, previews each action
 * with an interactive confirmation prompt, executes confirmed actions, and
 * records the full proposal/choice/outcome flow to the Recorder (if active).
 *
 *   1. Tagged code blocks (e.g. ```bash:path/to/file) are always presented for
 *      confirmation and then written. Parent directories are created as needed.
 *
 *   2. When AgentMode is true, backtick-wrapped /run suggestions from the LLM
 *      (e.g. "`/run mkdir testout`") are also presented and executed.
 *
 * Parameters:
 *   reply  (string)          — the raw assistant reply text.
 *   out    (io.Writer)       — destination for status messages.
 *   reader (*bufio.Reader)   — reads confirmation keystrokes from the user.
 *   ctx    (context.Context) — used to cancel long-running commands.
 *
 * Example:
 *   agent.autoExecuteReply(replyText, os.Stdout, reader, ctx)
 */
func (a *Agent) autoExecuteReply(reply string, out io.Writer, reader *bufio.Reader, ctx context.Context) {
	if a.Workspace == nil {
		return
	}

	blocks := findTaggedBlocks(reply)
	var suggestions [][]string
	if a.AgentMode {
		suggestions = extractRunSuggestions(reply)
	}

	// Open an agent scene in the recording if there is anything to act on.
	if len(blocks)+len(suggestions) > 0 && a.Recorder != nil {
		var parts []string
		if len(blocks) > 0 {
			parts = append(parts, fmt.Sprintf("write %d file(s)", len(blocks)))
		}
		if len(suggestions) > 0 {
			parts = append(parts, fmt.Sprintf("run %d command(s)", len(suggestions)))
		}
		desc := fmt.Sprintf("Harvey proposes to %s.", strings.Join(parts, " and "))
		_ = a.Recorder.StartAgentScene(desc)
	}

	applyAll := false

	// 1. Tagged code blocks — always offer to apply.
	for _, b := range blocks {
		choice := actionYes
		if !applyAll {
			choice = promptAction(reader, out, "Write: "+b.path, b.content)
		}
		switch choice {
		case actionNo:
			fmt.Fprintf(out, "  skipped %s\n", b.path)
			a.logAction("write", b.path, choice, "skipped")
			continue
		case actionQuit:
			fmt.Fprintln(out, "  aborted remaining actions.")
			a.logAction("write", b.path, choice, "aborted")
			return
		case actionAll:
			applyAll = true
		}
		if err := a.Workspace.WriteFile(b.path, []byte(b.content), 0o644); err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", b.path, err)
			a.logAction("write", b.path, choice, "error: "+err.Error())
		} else {
			fmt.Fprintf(out, "  ✓ wrote %s (%d bytes)\n", b.path, len(b.content))
			a.logAction("write", b.path, choice, "ok")
		}
	}

	// 2. Agent mode: /run suggestions.
	for _, args := range suggestions {
		cmdLine := strings.Join(args, " ")
		choice := actionYes
		if !applyAll {
			choice = promptAction(reader, out, "Run: "+cmdLine, "")
		}
		switch choice {
		case actionNo:
			fmt.Fprintf(out, "  skipped: %s\n", cmdLine)
			a.logAction("run", cmdLine, choice, "skipped")
			continue
		case actionQuit:
			fmt.Fprintln(out, "  aborted remaining actions.")
			a.logAction("run", cmdLine, choice, "aborted")
			return
		case actionAll:
			applyAll = true
		}
		var runOut strings.Builder
		if err := cmdRunCtx(a, ctx, args, &runOut); err != nil {
			fmt.Fprint(out, runOut.String())
			a.logAction("run", cmdLine, choice, "error: "+err.Error())
		} else {
			fmt.Fprint(out, runOut.String())
			a.logAction("run", cmdLine, choice, "ok")
		}
	}
}

// choiceStr converts an actionChoice to the string recorded in the script.
func choiceStr(c actionChoice) string {
	switch c {
	case actionNo:
		return "no"
	case actionAll:
		return "all"
	case actionQuit:
		return "quit"
	default:
		return "yes"
	}
}

// logAction records one agent action to the Recorder if one is active.
func (a *Agent) logAction(kind, target string, choice actionChoice, outcome string) {
	if a.Recorder != nil {
		_ = a.Recorder.RecordAgentAction(kind, target, choiceStr(choice), outcome)
	}
}
