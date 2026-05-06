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

// sensitiveCmdEnvVars contains environment variable names that should be
// EXCLUDED from child processes to prevent sensitive data leakage.
var sensitiveCmdEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"DEEPSEEK_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"OPENAI_API_KEY",
}

// safeCmdEnvPrefixes contains environment variable name prefixes that are
// safe to pass to child processes.
var safeCmdEnvPrefixes = []string{
	"PATH",
	"HOME",
	"USER",
	"USERNAME",
	"SHELL",
	"TERM",
	"LANG",
	"LC_",
	"PWD",
	"OLLAMA",
	"HARVEY",
}

/** filterCommandEnvironment returns a filtered copy of the environment for
 * commands executed via /run. Sensitive variables (API keys) are explicitly
 * excluded, and only safe variables are included.
 *
 * Parameters:
 *   env ([]string) — the original environment in "KEY=VALUE" format.
 *
 * Returns:
 *   []string — filtered environment with only safe variables.
 */
func filterCommandEnvironment(env []string) []string {
	sensitiveMap := make(map[string]bool)
	for _, v := range sensitiveCmdEnvVars {
		sensitiveMap[v] = true
	}

	safeMap := make(map[string]bool)
	for _, p := range safeCmdEnvPrefixes {
		safeMap[p] = true
	}

	var result []string
	for _, e := range env {
		idx := strings.IndexByte(e, '=')
		if idx == -1 {
			continue
		}
		varName := e[:idx]

		// Exclude sensitive variables
		if sensitiveMap[varName] {
			continue
		}

		// Include safe variables
		isSafe := false
		for prefix := range safeMap {
			if varName == prefix || strings.HasPrefix(varName, prefix+"_") {
				isSafe = true
				break
			}
		}
		// Also allow HARVEY_* and OLLAMA_* variables
		if strings.HasPrefix(varName, "HARVEY_") || strings.HasPrefix(varName, "OLLAMA_") {
			isSafe = true
		}

		if isSafe {
			result = append(result, e)
		}
	}

	// Always ensure PATH is set
	pathFound := false
	for _, e := range result {
		if strings.HasPrefix(e, "PATH=") {
			pathFound = true
			break
		}
	}
	if !pathFound {
		if path := os.Getenv("PATH"); path != "" {
			result = append(result, "PATH="+path)
		}
	}

	return result
}

/** Command describes a slash command available in the Harvey REPL.
 *
 * Fields:
 *   Usage       (string)   — short usage synopsis shown by /help.
 *   Description (string)   — one-line description shown by /help.
 *   UserDefined (bool)     — true for commands generated from compiled skills.
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
	UserDefined bool
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
			Usage:       "/ollama <start|stop|status|list|ps|run MODEL|pull MODEL|push MODEL|show MODEL|create NAME|cp SRC DEST|rm MODEL|logs|use MODEL|env>",
			Description: "Control the local Ollama service and manage models",
			Handler:     cmdOllama,
		},
		"kb": {
			Usage:       "/kb <status|search|inject|project|observe|concept> [args...]",
			Description: "Manage and query the workspace knowledge base",
			Handler:     cmdKB,
		},
		"rag": {
			Usage:       "/rag <list|new NAME|switch NAME|drop NAME|setup|ingest PATH|status|query TEXT|on|off>",
			Description: "Manage named RAG knowledge stores for context-augmented generation",
			Handler:     cmdRag,
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
			Usage:       "/model [NAME | ollama://NAME]",
			Description: "List available models, or switch to a named model",
			Handler:     cmdModel,
		},
		"context": {
			Usage:       "/context <show|add TEXT...|clear>",
			Description: "Manage pinned context that survives /clear",
			Handler:     cmdContext,
		},
		"audit": {
			Usage:       "/audit <show [n]|clear|status>",
			Description: "View or clear the audit log of security-relevant events",
			Handler:     cmdAudit,
		},
		"permissions": {
			Usage:       "/permissions <list [PATH]|set PATH PERMS|reset>",
			Description: "Manage workspace file permissions (read, write, exec, delete)",
			Handler:     cmdPermissions,
		},
		"security": {
			Usage:       "/security status",
			Description: "Show security settings status (safe mode, permissions, audit)",
			Handler:     cmdSecurity,
		},
		"record": {
			Usage:       "/record <start [FILE]|stop|status>",
			Description: "Record session exchanges to a Markdown file",
			Handler:     cmdRecord,
		},
		"session": {
			Usage:       "/session <continue FILE|replay FILE [OUTPUT]>",
			Description: "Continue or replay a .spmd/.fountain session recording",
			Handler:     cmdSession,
		},
		"skill": {
			Usage:       "/skill <list|load NAME|info NAME|status|new|run NAME>",
			Description: "List or load Agent Skills from the skill catalog",
			Handler:     cmdSkill,
		},
		"inspect": {
			Usage:       "/inspect [MODEL]",
			Description: "Show capability details for installed Ollama models; useful for multi-model routing",
			Handler:     cmdInspect,
		},
		"route": {
			Usage:       "/route <add NAME URL [MODEL] | rm NAME | list | on | off | status>",
			Description: "Register remote LLM endpoints and dispatch to them with @name in prompts",
			Handler:     cmdRoute,
		},
		"safemode": {
			Usage:       "/safemode <on|off|status|allow CMD|deny CMD|reset>",
			Description: "Enable/disable safe mode or manage the command allowlist",
			Handler:     cmdSafeMode,
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

/** registerSkillCommands registers each compiled skill in a.Skills as a
 * slash command, so users can invoke them as /skill-name [ARGS...] directly.
 * The full argument text after the command name is passed to the script as
 * HARVEY_PROMPT, so positional parameters map naturally to skill variables.
 * Built-in commands are never shadowed. Previously registered skill commands
 * are cleared before re-registering so this method is safe to call repeatedly
 * after compilation or after the skill catalog is refreshed.
 *
 * Parameters:
 *   (receiver) *Agent — agent whose Skills catalog and commands map are updated.
 *
 * Example:
 *   a.Skills = ScanSkills(a.Workspace.Root, a.Config.AgentsDir)
 *   a.registerSkillCommands()
 */
func (a *Agent) registerSkillCommands() {
	// Remove previously registered skill commands.
	for name, cmd := range a.commands {
		if cmd.UserDefined {
			delete(a.commands, name)
		}
	}

	for _, skill := range a.Skills {
		s := skill
		// Never shadow a built-in command.
		if existing, ok := a.commands[s.Name]; ok && !existing.UserDefined {
			continue
		}
		// Only register skills that have been compiled.
		if _, err := os.Stat(CompiledBashPath(s.Path)); err != nil {
			continue
		}

		// Build usage string: /name [VAR1] [VAR2] ...
		usage := "/" + s.Name
		for _, v := range s.Variables {
			usage += " [" + v.Name + "]"
		}

		// Trim description to its first line for the help listing.
		desc := strings.TrimSpace(s.Description)
		if nl := strings.IndexByte(desc, '\n'); nl >= 0 {
			desc = strings.TrimSpace(desc[:nl])
		}

		captured := s
		a.commands[captured.Name] = &Command{
			Usage:       usage,
			Description: desc,
			UserDefined: true,
			Handler: func(ag *Agent, args []string, out io.Writer) error {
				warnIfSkillStale(captured, out)
				prompt := strings.Join(args, " ")
				reader := bufio.NewReaderSize(ag.In, 1)
				return DispatchSkill(context.Background(), ag, captured, prompt, reader, out)
			},
		}
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
		case "clear":
			fmt.Fprint(out, FmtHelp(ClearHelpText, "", "", "", ""))
			return nil
		case "context":
			fmt.Fprint(out, FmtHelp(ContextHelpText, "", "", "", ""))
			return nil
		case "editing", "edit", "keybindings", "keys":
			fmt.Fprint(out, FmtHelp(EditingHelpText, "", "", "", ""))
			return nil
		case "kb", "knowledge", "knowledge-base":
			fmt.Fprint(out, FmtHelp(KBHelpText, "", "", "", ""))
			return nil
		case "ollama":
			fmt.Fprint(out, FmtHelp(OllamaHelpText, "", "", "", ""))
			return nil
		case "rag":
			fmt.Fprint(out, FmtHelp(RagHelpText, "", "", "", ""))
			return nil
		case "record", "recording":
			fmt.Fprint(out, FmtHelp(RecordHelpText, "", "", "", ""))
			return nil
		case "routing", "route", "router":
			fmt.Fprint(out, FmtHelp(RoutingHelpText, "", "", "", ""))
			return nil
		case "session", "sessions":
			fmt.Fprint(out, FmtHelp(SessionHelpText, "", "", "", ""))
			return nil
		case "skills", "skill":
			fmt.Fprint(out, FmtHelp(SkillsHelpText, "", "", "", ""))
			return nil
		default:
			fmt.Fprintf(out, "  Unknown help topic %q. Available topics: clear, context, editing, kb, ollama, rag, record, routing, session, skills\n\n", args[0])
		}
	}

	var builtins, userDefined []*Command
	for _, cmd := range a.commands {
		if cmd.UserDefined {
			userDefined = append(userDefined, cmd)
		} else {
			builtins = append(builtins, cmd)
		}
	}
	sort.Slice(builtins, func(i, j int) bool { return builtins[i].Usage < builtins[j].Usage })
	sort.Slice(userDefined, func(i, j int) bool { return userDefined[i].Usage < userDefined[j].Usage })

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %-50s %s\n", "! COMMAND", "Run a shell command, stream output, inject into context")
	fmt.Fprintf(out, "  %-50s %s\n", "@NAME PROMPT", "Send prompt to a registered remote endpoint")
	fmt.Fprintln(out)
	for _, cmd := range builtins {
		fmt.Fprintf(out, "  %-50s %s\n", cmd.Usage, cmd.Description)
	}
	if len(userDefined) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  User-defined commands (compiled skills):")
		for _, cmd := range userDefined {
			fmt.Fprintf(out, "  %-50s %s\n", cmd.Usage, cmd.Description)
		}
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
	if ac, ok := a.Client.(*AnyLLMClient); ok && ac.ProviderName() == "ollama" && len(a.History) > 0 {
		n, exact := CountTokens(context.Background(), ac.BackendURL(), ac.ModelName(), HistoryText(a.History))
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
	if a.Routes != nil && a.Routes.Enabled && len(a.Routes.Endpoints) > 0 {
		fmt.Fprintf(out, "Routing:   on (%d endpoint(s))\n", len(a.Routes.Endpoints))
	} else {
		fmt.Fprintln(out, "Routing:   off")
	}
	if a.Workspace != nil {
		fmt.Fprintf(out, "Workspace: %s\n", a.Workspace.Root)
	}
	if a.KB != nil {
		fmt.Fprintf(out, "KB:        open (%s)\n", a.KB.Path())
	} else {
		fmt.Fprintln(out, "KB:        not open")
	}
	if a.SessionsDir != "" {
		fmt.Fprintf(out, "Sessions:  %s\n", a.SessionsDir)
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
	fmt.Fprintln(out, "Conversation history cleared.")
	return nil
}

/** cmdRoute handles remote endpoint routing configuration for multi-model
 * workflows. Routes allow dispatching prompts to remote LLM endpoints via
 * @mention syntax (e.g., @claude, @mistral) or explicitly via /route.
 *
 * Subcommands:
 *   add NAME URL [MODEL]    — Register a new remote endpoint
 *   rm NAME                 — Remove a registered endpoint
 *   list                    — List all registered endpoints
 *   on                      — Enable routing globally
 *   off                     — Disable routing globally
 *   status                  — Show routing status and endpoints
 *
 * Supported endpoint types:
 *   Local:  ollama://host:port, llamafile://host:port, llamacpp://host:port
 *   Cloud:  anthropic://, deepseek://, gemini://, mistral://, openai://
 *
 * Cloud providers read API keys from environment variables:
 *   ANTHROPIC_API_KEY, DEEPSEEK_API_KEY, GEMINI_API_KEY, MISTRAL_API_KEY, OPENAI_API_KEY
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with route registry.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdRoute(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return routeStatus(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "add":
		return routeAdd(a, args[1:], out)
	case "rm", "remove":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /route rm NAME")
			return nil
		}
		return routeRemove(a, args[1], out)
	case "list":
		return routeList(a, out)
	case "on":
		return routeOn(a, out)
	case "off":
		return routeOff(a, out)
	case "status":
		return routeStatus(a, out)
	default:
		fmt.Fprintf(out, "  Unknown route subcommand: %q\n", args[0])
		fmt.Fprintln(out, "  Usage: /route <add NAME URL [MODEL] | rm NAME | list | on | off | status>")
	}
	return nil
}

func routeAdd(a *Agent, args []string, out io.Writer) error {
	if len(args) < 2 {
		fmt.Fprintln(out, "  Usage: /route add NAME URL [MODEL]")
		fmt.Fprintln(out, "  Local:  ollama://host:port  llamafile://host:port  llamacpp://host:port")
		fmt.Fprintln(out, "  Cloud:  anthropic://  deepseek://  gemini://  mistral://  openai://")
		fmt.Fprintln(out, "  Cloud providers read API keys from environment variables.")
		return nil
	}
	name, rawURL := args[0], args[1]
	model := ""
	if len(args) >= 3 {
		model = args[2]
	}
	kind, err := InferRouteKind(rawURL)
	if err != nil {
		fmt.Fprintf(out, "  %v\n", err)
		return nil
	}
	ep := &RouteEndpoint{Name: name, URL: rawURL, Model: model, Kind: kind}
	a.Routes.Add(ep)
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintf(out, "  Added: @%s → %s", name, rawURL)
	if model != "" {
		fmt.Fprintf(out, " (%s)", model)
	}
	fmt.Fprintln(out)
	return nil
}

func routeRemove(a *Agent, name string, out io.Writer) error {
	if a.Routes.Lookup(name) == nil {
		fmt.Fprintf(out, "  Endpoint %q not found. Use /route list to see registered endpoints.\n", name)
		return nil
	}
	a.Routes.Remove(name)
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintf(out, "  Removed: @%s\n", name)
	return nil
}

func routeList(a *Agent, out io.Writer) error {
	if len(a.Routes.Endpoints) == 0 {
		fmt.Fprintln(out, "  No endpoints registered. Use /route add NAME URL [MODEL].")
		return nil
	}
	names := make([]string, 0, len(a.Routes.Endpoints))
	for n := range a.Routes.Endpoints {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(out)
	for _, n := range names {
		ep := a.Routes.Endpoints[n]
		reach := probeRouteEndpoint(ep, a.Config)
		reachStr := green("✓")
		if !reach {
			reachStr = yellow("✗")
		}
		model := ep.Model
		if model == "" {
			model = "(default)"
		}
		fmt.Fprintf(out, "  %s  @%-16s  %-10s  %s  [%s]\n", reachStr, n, ep.Kind, ep.URL, model)
	}
	fmt.Fprintln(out)
	return nil
}

func routeOn(a *Agent, out io.Writer) error {
	a.Routes.Enabled = true
	a.Config.RoutingEnabled = true
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintln(out, "  Routing on. Prefix your prompt with @name to dispatch to a registered endpoint.")
	return nil
}

func routeOff(a *Agent, out io.Writer) error {
	a.Routes.Enabled = false
	a.Config.RoutingEnabled = false
	if saveErr := SaveRouteConfig(a.Workspace, a.Routes); saveErr != nil {
		fmt.Fprintf(out, "  Warning: could not persist route config: %v\n", saveErr)
	}
	fmt.Fprintln(out, "  Routing off. @mentions will be rejected until you run /route on.")
	return nil
}

func routeStatus(a *Agent, out io.Writer) error {
	enabled := a.Routes != nil && a.Routes.Enabled
	if enabled {
		fmt.Fprintln(out, "  Routing: on")
	} else {
		fmt.Fprintln(out, "  Routing: off")
	}
	count := 0
	if a.Routes != nil {
		count = len(a.Routes.Endpoints)
	}
	if count == 0 {
		fmt.Fprintln(out, "  Endpoints: none registered (use /route add NAME URL [MODEL])")
	} else {
		fmt.Fprintf(out, "  Endpoints: %d registered (use /route list for details)\n", count)
	}
	return nil
}

// ─── /safemode ──────────────────────────────────────────────────────────────

/** cmdSafeMode handles safe mode configuration for restricting which commands
 * can be executed via the ! prefix or /run command. Safe mode provides a
 * command allowlist to prevent execution of potentially dangerous commands.
 *
 * Subcommands:
 *   on       — Enable safe mode (restricts commands to allowlist)
 *   off      — Disable safe mode (all commands allowed)
 *   status  — Show current safe mode status and allowlist
 *   allow CMD — Add a command to the allowlist
 *   deny CMD  — Remove a command from the allowlist
 *   reset    — Reset allowlist to defaults
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with configuration.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdSafeMode(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /safemode <on|off|status|allow CMD|deny CMD|reset>")
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "on":
		return safeModeOn(a, out)
	case "off":
		return safeModeOff(a, out)
	case "status":
		return safeModeStatus(a, out)
	case "allow":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /safemode allow CMD")
			return nil
		}
		return safeModeAllow(a, args[1], out)
	case "deny":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /safemode deny CMD")
			return nil
		}
		return safeModeDeny(a, args[1], out)
	case "reset":
		return safeModeReset(a, out)
	default:
		fmt.Fprintf(out, "Unknown safemode subcommand: %q\n", args[0])
		fmt.Fprintln(out, "Usage: /safemode <on|off|status|allow CMD|deny CMD|reset>")
	}
	return nil
}

func safeModeOn(a *Agent, out io.Writer) error {
	a.Config.SafeMode = true
	if a.Workspace != nil {
		if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist safe mode: %v\n", err)
		}
	}
	fmt.Fprintln(out, "  Safe mode enabled. Only allowed commands can be executed.")
	fmt.Fprintf(out, "  Allowed: %s\n", strings.Join(a.Config.AllowedCommands, ", "))
	return nil
}

func safeModeOff(a *Agent, out io.Writer) error {
	a.Config.SafeMode = false
	if a.Workspace != nil {
		if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist safe mode: %v\n", err)
		}
	}
	fmt.Fprintln(out, "  Safe mode disabled. All commands are allowed.")
	return nil
}

func safeModeStatus(a *Agent, out io.Writer) error {
	if a.Config.SafeMode {
		fmt.Fprintln(out, "  Safe mode: on")
		fmt.Fprintf(out, "  Allowed commands (%d): %s\n", len(a.Config.AllowedCommands), strings.Join(a.Config.AllowedCommands, ", "))
	} else {
		fmt.Fprintln(out, "  Safe mode: off")
		fmt.Fprintln(out, "  All commands are allowed.")
	}
	return nil
}

func safeModeAllow(a *Agent, cmd string, out io.Writer) error {
	oldLen := len(a.Config.AllowedCommands)
	a.Config.AddAllowedCommand(cmd)
	if len(a.Config.AllowedCommands) > oldLen {
		fmt.Fprintf(out, "  Added %q to allowlist.\n", cmd)
	} else {
		fmt.Fprintf(out, "  %q is already in the allowlist.\n", cmd)
	}
	if a.Workspace != nil {
		if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist allowlist: %v\n", err)
		}
	}
	return nil
}

func safeModeDeny(a *Agent, cmd string, out io.Writer) error {
	oldLen := len(a.Config.AllowedCommands)
	a.Config.RemoveAllowedCommand(cmd)
	if len(a.Config.AllowedCommands) < oldLen {
		fmt.Fprintf(out, "  Removed %q from allowlist.\n", cmd)
	} else {
		fmt.Fprintf(out, "  %q is not in the allowlist.\n", cmd)
	}
	if a.Workspace != nil {
		if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist allowlist: %v\n", err)
		}
	}
	return nil
}

func safeModeReset(a *Agent, out io.Writer) error {
	a.Config.ResetAllowedCommands()
	if a.Workspace != nil {
		if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist allowlist: %v\n", err)
		}
	}
	fmt.Fprintln(out, "  Allowlist reset to defaults.")
	fmt.Fprintf(out, "  Allowed commands: %s\n", strings.Join(a.Config.AllowedCommands, ", "))
	return nil
}

// probeRouteEndpoint returns true when ep appears reachable.
// Local providers are probed via HTTP; cloud providers check for a non-empty API key env var.
func probeRouteEndpoint(ep *RouteEndpoint, cfg *Config) bool {
	switch ep.Kind {
	case KindOllama:
		return ProbeOllama(ollamaBaseURL(ep.URL))
	case KindLlamafile:
		return ProbeEncoderfile(LlamafileHealthURL(ep.URL))
	case KindLlamaCpp:
		return ProbeEncoderfile(LlamafileHealthURL(LlamacppAPIURL(ep.URL)))
	case KindAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY") != ""
	case KindDeepSeek:
		return os.Getenv("DEEPSEEK_API_KEY") != ""
	case KindGemini:
		return os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != ""
	case KindMistral:
		return os.Getenv("MISTRAL_API_KEY") != ""
	case KindOpenAI:
		return os.Getenv("OPENAI_API_KEY") != ""
	}
	return false
}

func cmdInspect(a *Agent, args []string, out io.Writer) error {
	ac, ok := a.Client.(*AnyLLMClient)
	if !ok || ac.ProviderName() != "ollama" {
		fmt.Fprintln(out, "Inspect requires an Ollama backend. Use /ollama start first.")
		return nil
	}
	oc := NewOllamaClient(ac.BackendURL(), "")
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

/** cmdOllama handles Ollama server and model management commands.
 *
 * Subcommands:
 *   start     — Launch ollama serve as a background process
 *   stop      — Print instructions to stop Ollama via system service manager
 *   status    — Check if Ollama server is running
 *   list      — List all installed models with metadata
 *   ps        — Show running models (via ollama ps)
 *   run       — Start a model in interactive mode (detached from Harvey)
 *   pull      — Download a model from Ollama registry
 *   push      — Upload a model to Ollama registry
 *   show      — Display detailed model information
 *   create    — Create a model from a Modelfile
 *   cp        — Copy a model to a new name
 *   rm        — Remove a model from the local store
 *   probe     — Test model capabilities (tools, embeddings)
 *   logs      — Show Ollama server logs
 *   use       — Switch to a different model
 *   env       — Display active Ollama environment variables
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with configuration.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdOllama(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /ollama <start|stop|status|list|ps|run MODEL|pull MODEL|push MODEL|show MODEL|create NAME|cp SRC DEST|rm MODEL|probe [MODEL|--all]|logs|use MODEL|env>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "start":
		PrintOllamaEnv(out)
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
		summaries, err := NewOllamaClient(a.Config.OllamaURL, "").ModelSummaries(context.Background())
		if err != nil {
			return err
		}
		if len(summaries) == 0 {
			fmt.Fprintln(out, "No models installed. Run: /ollama pull <model>")
			return nil
		}
		ollamaListTable(a, summaries, out)
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
		model := args[1]
		cmd := exec.Command("ollama", "pull", model)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return err
		}
		// Fast-probe the newly pulled model and cache the result.
		if a.ModelCache != nil {
			ctx := context.Background()
			cap, err := FastProbeModel(ctx, a.Config.OllamaURL, model)
			if err == nil {
				_ = a.ModelCache.Set(cap)
				fmt.Fprintf(out, "  tools: %s   embed: %s   tagged: %s   ctx: %s   [fast probe]\n",
					cap.SupportsTools, cap.SupportsEmbed, cap.SupportsTaggedBlocks, ollamaFormatCtx(cap.ContextLength))
			}
		}
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama show MODEL")
			return nil
		}
		cmd := exec.Command("ollama", "show", args[1])
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	case "create":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama create NAME [-f MODELFILE]")
			return nil
		}
		cmd := exec.Command("ollama", append([]string{"create"}, args[1:]...)...)
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama run MODEL [PROMPT]")
			return nil
		}
		cmd := exec.Command("ollama", append([]string{"run"}, args[1:]...)...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "push":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama push MODEL")
			return nil
		}
		cmd := exec.Command("ollama", "push", args[1])
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	case "cp":
		if len(args) < 3 {
			fmt.Fprintln(out, "Usage: /ollama cp SOURCE DEST")
			return nil
		}
		cmd := exec.Command("ollama", "cp", args[1], args[2])
		cmd.Stdout = out
		cmd.Stderr = out
		return cmd.Run()
	case "rm":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama rm MODEL [MODEL...]")
			return nil
		}
		models := args[1:]
		cmd := exec.Command("ollama", append([]string{"rm"}, models...)...)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return err
		}
		// Remove each successfully deleted model from the cache.
		if a.ModelCache != nil {
			for _, m := range models {
				_ = a.ModelCache.Delete(m)
			}
		}
	case "probe":
		return ollamaProbe(a, args[1:], out)
	case "probe-all":
		return ollamaProbe(a, []string{"--all"}, out)
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
		fmt.Fprintln(out, "Ollama environment (Harvey process):")
		PrintOllamaEnv(out)
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
		a.Client = newOllamaLLMClient(a.Config.OllamaURL, model, a.Config.OllamaTimeout)
		fmt.Fprintf(out, "Now using Ollama model: %s\n", model)
	default:
		fmt.Fprintf(out, "Unknown ollama subcommand: %s\n", args[0])
	}
	return nil
}

// ollamaListTable prints the capability table for /ollama list.
func ollamaListTable(a *Agent, summaries []ModelSummary, out io.Writer) {
	const nameW = 36
	fmt.Fprintf(out, "%-*s  %7s  %-8s  %6s  %5s  %5s\n",
		nameW, "NAME", "SIZE", "FAMILY", "CTX", "TOOLS", "EMBED")
	fmt.Fprintf(out, "%s  %s  %s  %s  %s  %s\n",
		strings.Repeat("─", nameW),
		strings.Repeat("─", 7),
		strings.Repeat("─", 8),
		strings.Repeat("─", 6),
		strings.Repeat("─", 5),
		strings.Repeat("─", 5),
	)

	activeName := ""
	if ac, ok := a.Client.(*AnyLLMClient); ok {
		activeName = ac.ModelName()
	}

	unknownCount := 0
	for _, s := range summaries {
		var cap *ModelCapability
		if a.ModelCache != nil {
			cap, _ = a.ModelCache.Get(s.Name)
		}

		tools := CapUnknown
		embed := CapUnknown
		ctx := 0
		if cap != nil {
			tools = cap.SupportsTools
			embed = cap.SupportsEmbed
			ctx = cap.ContextLength
		} else {
			unknownCount++
		}

		marker := "  "
		if s.Name == activeName {
			marker = "* "
		}
		displayName := marker + ollamaTruncateName(s.Name, nameW-2)

		fmt.Fprintf(out, "%-*s  %7s  %-8s  %6s  %5s  %5s\n",
			nameW, displayName,
			ollamaFormatBytes(s.SizeBytes),
			ollamaTruncateName(s.Family, 8),
			ollamaFormatCtx(ctx),
			tools.String(),
			embed.String(),
		)
	}

	if unknownCount > 0 {
		fmt.Fprintf(out, "\n  %d model(s) not yet probed — run /ollama probe to fill in capabilities.\n", unknownCount)
	}
}

// ollamaProbe handles /ollama probe [MODEL|--all].
func ollamaProbe(a *Agent, args []string, out io.Writer) error {
	if !ProbeOllama(a.Config.OllamaURL) {
		fmt.Fprintln(out, "Ollama is not running.")
		return nil
	}
	if a.ModelCache == nil {
		fmt.Fprintln(out, "Model cache is not open.")
		return nil
	}

	ctx := context.Background()
	client := NewOllamaClient(a.Config.OllamaURL, "")

	// /ollama probe MODEL — probe a specific model, always refresh.
	if len(args) == 1 && args[0] != "--all" {
		model := args[0]
		fmt.Fprintf(out, "Probing %s...\n", model)
		cap, err := ThoroughProbeModel(ctx, a.Config.OllamaURL, model)
		if err != nil {
			return fmt.Errorf("probe %s: %w", model, err)
		}
		if err := a.ModelCache.Set(cap); err != nil {
			return err
		}
		fmt.Fprintf(out, "  tools: %s   embed: %s   tagged: %s   ctx: %s   [thorough]\n",
			cap.SupportsTools, cap.SupportsEmbed, cap.SupportsTaggedBlocks, ollamaFormatCtx(cap.ContextLength))
		return nil
	}

	// /ollama probe or /ollama probe --all — probe all installed models.
	// Without --all, skip models already in the cache.
	forceAll := len(args) == 1 && args[0] == "--all"

	summaries, err := client.ModelSummaries(ctx)
	if err != nil {
		return err
	}

	var targets []string
	for _, s := range summaries {
		if forceAll {
			targets = append(targets, s.Name)
			continue
		}
		cap, _ := a.ModelCache.Get(s.Name)
		if cap == nil || cap.ProbeLevel == "none" {
			targets = append(targets, s.Name)
		}
	}

	if len(targets) == 0 {
		fmt.Fprintln(out, "All models are already probed. Use /ollama probe --all to refresh.")
		return nil
	}

	fmt.Fprintf(out, "Probing %d model(s)...\n", len(targets))
	for _, name := range targets {
		cap, err := ThoroughProbeModel(ctx, a.Config.OllamaURL, name)
		if err != nil {
			fmt.Fprintf(out, "  %-36s  error: %v\n", ollamaTruncateName(name, 36), err)
			continue
		}
		if err := a.ModelCache.Set(cap); err != nil {
			fmt.Fprintf(out, "  %-36s  cache write error: %v\n", ollamaTruncateName(name, 36), err)
			continue
		}
		fmt.Fprintf(out, "  %-36s  tools: %s   embed: %s   tagged: %s   [thorough]\n",
			ollamaTruncateName(name, 36), cap.SupportsTools, cap.SupportsEmbed, cap.SupportsTaggedBlocks)
	}
	fmt.Fprintln(out, "Done.")
	return nil
}

// ollamaTruncateName truncates s to at most max runes, appending "…" when cut.
func ollamaTruncateName(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// ollamaFormatBytes returns a human-readable size string from a byte count.
func ollamaFormatBytes(n int64) string {
	if n == 0 {
		return "—"
	}
	if gb := float64(n) / (1024 * 1024 * 1024); gb >= 1 {
		return fmt.Sprintf("%.1f GB", gb)
	}
	mb := float64(n) / (1024 * 1024)
	return fmt.Sprintf("%.0f MB", mb)
}

// ollamaFormatCtx returns a human-readable context-length string.
func ollamaFormatCtx(tokens int) string {
	if tokens <= 0 {
		return "—"
	}
	if tokens >= 1024 {
		return fmt.Sprintf("%dk", tokens/1024)
	}
	return fmt.Sprintf("%d", tokens)
}

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
		fmt.Fprintln(out, "Knowledge base is not open. This should not happen — please restart Harvey.")
		return nil
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
	default:
		fmt.Fprintf(out, "Unknown kb subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /kb <status|search|inject|project|observe|concept> [args...]")
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

// kbSearch handles /kb search TERM [TERM...] using the FTS5 index.
func kbSearch(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /kb search TERM [TERM...]")
		fmt.Fprintln(out, "Tip:   quote phrases (\"WAL mode\"), use * for prefix (docker*)")
		return nil
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
	projectID := a.Config.CurrentProjectID
	label := "all projects"

	if len(args) > 0 {
		name := strings.Join(args, " ")
		p, err := a.KB.ProjectByName(name)
		if err != nil {
			return err
		}
		if p == nil {
			fmt.Fprintf(out, "  Project %q not found. Use /kb project list to see available projects.\n", name)
			return nil
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

/** cmdRecord manages session recording to Fountain screenplay files. Harvey
 * records all conversations to .spmd files for auditability and resumption.
 *
 * Subcommands:
 *   start [FILE]  — Begin recording to specified file (or auto-generated path)
 *   stop         — Stop current recording session
 *   status       — Show current recording status and file path
 *
 * Recording is enabled by default on startup. Sessions are saved to
 * harvey/sessions/ by default, with filenames like:
 *   harvey-session-YYYYMMDD-HHMMSS.spmd
 *
 * The Fountain format (.spmd) captures all chat turns, file operations,
 * shell commands, and skill executions with proper character attribution
 * (HARVEY, USER, MODEL_NAME, etc.).
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with recording state.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
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
			sessDir := a.SessionsDir
			if sessDir == "" {
				sessDir = "."
			}
			path = DefaultSessionPath(sessDir)
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
		if !a.CheckReadPermission(rel) {
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileRead, rel, StatusDenied)
			}
			fmt.Fprintf(out, "  ✗ %s: read permission denied\n", rel)
			continue
		}
		data, err := a.Workspace.ReadFile(rel)
		if err != nil {
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileRead, rel, StatusError)
			}
			fmt.Fprintf(out, "  ✗ %s: %v\n", rel, err)
			continue
		}
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileRead, rel, StatusSuccess)
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

	if !a.CheckWritePermission(dest) {
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileWrite, dest, StatusDenied)
		}
		fmt.Fprintf(out, "  write permission denied for %s\n", dest)
		return nil
	}
	if err := a.Workspace.WriteFile(dest, []byte(content), 0o644); err != nil {
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileWrite, dest, StatusError)
		}
		return fmt.Errorf("write: %w", err)
	}
	if a.AuditBuffer != nil {
		a.AuditBuffer.Log(ActionFileWrite, dest, StatusSuccess)
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
//
// Security: Uses direct exec (not shell) and filters environment to prevent
// sensitive data leakage. Uses a timeout context to prevent hanging.
func cmdRun(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /run COMMAND [ARGS...]")
		return nil
	}

	// Safe mode check: verify command is in allowlist
	if a.Config.SafeMode && !a.Config.IsCommandAllowed(args[0]) {
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionCommand, strings.Join(args, " "), StatusDenied)
		}
		fmt.Fprintf(out, yellow("  Command %q is not allowed in safe mode.\n"), args[0])
		fmt.Fprintf(out, "  Allowed commands: %s\n", strings.Join(a.Config.AllowedCommands, ", "))
		fmt.Fprintln(out, "  Use /safemode off to disable, or /safemode allow CMD to add it.")
		return nil
	}

	// Log allowed command execution
	if a.AuditBuffer != nil {
		a.AuditBuffer.Log(ActionCommand, strings.Join(args, " "), StatusAllowed)
	}

	cmdLine := strings.Join(args, " ")
	fmt.Fprintf(out, "  $ %s\n", cmdLine)

	// Use a context with an optional timeout to prevent hanging commands.
	var ctx context.Context
	var cancel context.CancelFunc
	if a.Config.RunTimeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), a.Config.RunTimeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = a.Workspace.Root
	// Filter environment to prevent sensitive data leakage
	cmd.Env = filterCommandEnvironment(os.Environ())
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
	// Filter environment to prevent sensitive data leakage
	cmd.Env = filterCommandEnvironment(os.Environ())
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

// ─── /summarize ──────────────────────────────────────────────────────────────

// summarizePrompt is appended to the history when requesting a summary.
const summarizePrompt = "Please summarize this conversation concisely. Capture the key topics discussed, files mentioned, code changes proposed or made, and any open questions or next steps. This summary will replace the full conversation history to keep the context window manageable."

// cmdSummarize asks the connected LLM to condense the conversation history
// into a single summary message, then replaces the history with that summary.
func cmdSummarize(a *Agent, args []string, out io.Writer) error {
	if a.Client == nil {
		fmt.Fprintln(out, "No backend connected. Use /ollama start.")
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
	sp := newSpinner(out, 0, a.spinnerLabel())
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

/** cmdSession manages Harvey session recordings.
 *
 * Subcommands:
 *   continue FILE     — load chat history from a .spmd/.fountain file and continue in REPL.
 *   replay FILE [OUT] — re-send turns from a session file to the current backend
 *                       and record fresh responses to OUT (default: auto-named in sessions dir).
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   args ([]string)  — subcommand and its arguments.
 *   out  (io.Writer) — destination for command output.
 *
 * Returns:
 *   error — on I/O failure.
 *
 * Example:
 *   /session continue harvey/sessions/harvey-session-20260430.spmd
 *   /session replay old.spmd new.spmd
 */
/** cmdSession handles session file operations for loading and replaying
 * recorded conversations. Session files use the Fountain screenplay format
 * (.spmd extension) and capture complete conversation history.
 *
 * Subcommands:
 *   continue FILE    — Load a session file's chat history and continue
 *   replay FILE [OUT] — Re-send all user prompts to current model, save to new file
 *
 * Continue mode loads the conversation history into the current session's
 * context, allowing you to pick up where you left off with full context intact.
 * The model used in the original session is automatically selected if available.
 *
 * Replay mode re-sends each user message from the source session to the
 * currently active LLM backend, capturing fresh responses in a new session file.
 * This is useful for comparing responses from different models or after
 * model updates. Tagged code blocks in replies are applied with backup
 * protection.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with workspace.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure (non-fatal errors are printed to out).
 */
func cmdSession(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /session <continue FILE|replay FILE [OUTPUT]>")
		return nil
	}
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
			outPath = DefaultSessionPath(a.SessionsDir)
		}
		if a.Client == nil {
			fmt.Fprintln(out, "  No backend connected. Use /ollama start.")
			return nil
		}
		return a.ReplayFromFountain(context.Background(), src, outPath, out)
	default:
		fmt.Fprintf(out, "Unknown session subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /session <continue FILE|replay FILE [OUTPUT]>")
	}
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
/** cmdSkill handles Agent Skills management and execution. Skills are
 * structured tasks defined in SKILL.md files that can be loaded into
 * context, executed directly, or triggered automatically.
 *
 * Subcommands:
 *   list    — List all discovered skills in the skills directory
 *   load NAME — Load a skill's instructions into the conversation context
 *   info NAME — Show metadata (description, version, author, etc.) for a skill
 *   status  — Show skills directory paths and loaded skill status
 *   new     — Create a new skill interactively via the skill wizard
 *   run NAME — Execute a compiled skill directly
 *
 * Skills are discovered from the agents/skills/ directory tree on startup.
 * Each skill is defined in a SKILL.md file with YAML frontmatter containing
 * metadata (name, description, trigger, etc.) and Markdown body containing
 * instructions.
 *
 * Skills can be triggered automatically when a user's prompt matches the
 * skill's trigger pattern (see skill_dispatch.go for trigger matching logic).
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with skills catalog.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
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
	case "new":
		return skillNew(a, out)
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /skill run NAME")
			return nil
		}
		return skillRun(a, args[1], out)
	default:
		fmt.Fprintf(out, "Unknown skill subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /skill <list|load NAME|info NAME|status|new|run NAME>")
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

	model := "(no model)"
	if a.Client != nil {
		model = a.Client.Name()
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Current model: %s\n", model)
	fmt.Fprintln(out)
	for _, n := range names {
		s := a.Skills[n]
		fmt.Fprintf(out, "  %-28s [%s]\n", n, s.Source)
		fmt.Fprintf(out, "    %s\n", s.Description)
		if s.Compatibility != "" {
			fmt.Fprintf(out, "    Compatibility: %s\n", s.Compatibility)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Use /skill load NAME to activate a skill.")
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
	a.ActiveSkill = name
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

// skillNew runs the interactive skill wizard via /skill new.
func skillNew(a *Agent, out io.Writer) error {
	reader := bufio.NewReaderSize(a.In, 1)
	relPath, err := RunSkillWizard(a.Workspace, reader, out)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, green("✓")+" Skill created: %s\n", relPath)
	fmt.Fprintln(out, "  To make it runnable, add compiled.bash / compiled.ps1 under scripts/.")
	a.Skills = ScanSkills(a.Workspace.Root, a.Config.AgentsDir)
	a.registerSkillCommands()
	return nil
}

// skillCompile compiles a named skill to compiled.bash and compiled.ps1.
func skillCompile(a *Agent, name string, out io.Writer) error {
	if a.Client == nil {
		fmt.Fprintln(out, "  No backend connected. Use /ollama start first.")
		return nil
	}
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	fmt.Fprintf(out, "  Compiling skill %q...\n", name)
	sp := newSpinner(out, 0, a.spinnerLabel()+" · compiling")
	err := CompileSkill(context.Background(), a.Client, skill, io.Discard)
	sp.stop()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, green("✓")+" Compiled: scripts/compiled.bash and scripts/compiled.ps1")
	a.Skills = ScanSkills(a.Workspace.Root, a.Config.AgentsDir)
	a.registerSkillCommands()
	fmt.Fprintf(out, "  Tip: you can now run it as /%s\n", name)
	return nil
}

// skillRun dispatches a named skill using DispatchSkill.
func skillRun(a *Agent, name string, out io.Writer) error {
	skill, ok := a.Skills[name]
	if !ok {
		fmt.Fprintf(out, "  Skill %q not found. Use /skill list to see available skills.\n", name)
		return nil
	}
	warnIfSkillStale(skill, out)
	reader := bufio.NewReaderSize(a.In, 1)
	return DispatchSkill(context.Background(), a, skill, "", reader, out)
}

// warnIfSkillStale prints a warning when SKILL.md is newer than the compiled scripts.
func warnIfSkillStale(skill *SkillMeta, out io.Writer) {
	stale, err := IsStale(skill)
	if err == nil && stale {
		fmt.Fprintf(out, "  Warning: %s/SKILL.md has been updated since it was compiled.\n", skill.Name)
		fmt.Fprintln(out, "  Running the old compiled version. Recompile on a capable system to pick up changes.")
	}
}

// ─── /model ──────────────────────────────────────────────────────────────────

/** cmdModel lists available models from all reachable backends, or switches
 * the active model when a name is supplied.
 *
 * Usage:
 *   /model                — list all available Ollama models.
 *   /model NAME           — switch to NAME.
 *   /model ollama://NAME  — switch Ollama to NAME (explicit prefix).
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
		models, err := newOllamaLLMClient(a.Config.OllamaURL, "", a.Config.OllamaTimeout).Models(ctx)
		if err == nil {
			found = true
			activeModel := ""
			if ac, ok := a.Client.(*AnyLLMClient); ok {
				activeModel = ac.ModelName()
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

	if !found {
		fmt.Fprintln(out, "  No backends available. Start Ollama or use /route add to register an endpoint.")
	}
	return nil
}

// modelSwitch changes the active model. An "ollama://" prefix forces the Ollama
// backend. Without a prefix the model is looked up in Ollama; if found it is
// activated, otherwise the user is told it was not found.
func modelSwitch(a *Agent, name string, out io.Writer) error {
	ctx := context.Background()

	const ollamaPrefix = "ollama://"

	if strings.HasPrefix(name, ollamaPrefix) {
		name = strings.TrimPrefix(name, ollamaPrefix)
	}

	if !ProbeOllama(a.Config.OllamaURL) {
		fmt.Fprintln(out, "  Ollama is not running. Use /ollama start first.")
		return nil
	}

	models, err := newOllamaLLMClient(a.Config.OllamaURL, "", a.Config.OllamaTimeout).Models(ctx)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}
	for _, m := range models {
		if m == name {
			a.Config.OllamaModel = name
			a.Client = newOllamaLLMClient(a.Config.OllamaURL, name, a.Config.OllamaTimeout)
			fmt.Fprintf(out, "  Switched to model: %s\n", name)
			return nil
		}
	}
	fmt.Fprintf(out, "  Model %q not found in Ollama.\n", name)
	fmt.Fprintln(out, "  Use /model to list available models.")
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
//
// Security: Filters environment to prevent sensitive data leakage.
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
	// Filter environment to prevent sensitive data leakage
	cmd.Env = filterCommandEnvironment(os.Environ())
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

/** autoExecuteReply scans reply for tagged code blocks, previews each with an
 * interactive confirmation prompt, writes confirmed blocks, and records the
 * full proposal/choice/outcome flow to the Recorder (if active).
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

	// Open an agent scene in the recording if there is anything to act on.
	if len(blocks) > 0 && a.Recorder != nil {
		desc := fmt.Sprintf("Harvey proposes to write %d file(s).", len(blocks))
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

// ─── /rag ────────────────────────────────────────────────────────────────────

/** cmdRag handles Retrieval-Augmented Generation (RAG) store management and
 * context injection. RAG allows Harvey to retrieve relevant document snippets
 * and inject them into the conversation context before each prompt.
 *
 * Subcommands:
 *   status    — Show active store and all registered stores
 *   list      — List all registered RAG stores
 *   on        — Enable RAG context injection for current session
 *   off       — Disable RAG context injection for current session
 *   setup     — Create a new RAG store (interactive or with defaults)
 *   new       — Create a named RAG store with interactive setup
 *   switch    — Activate a different RAG store
 *   drop      — Remove a store from the registry
 *   ingest    — Ingest files/directories into the active store
 *   query     — Query the active store and show matching chunks
 *
 * RAG stores are SQLite databases bound to a specific embedding model.
 * Only the active store is kept open in memory. Each store can be queried
 * independently and switched as needed for different projects or domains.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with RAG configuration.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdRag(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return ragStatus(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "status":
		return ragStatus(a, out)
	case "list":
		return ragList(a, out)
	case "on":
		if a.Rag == nil {
			fmt.Fprintln(out, "RAG is not configured. Run /rag setup first.")
			return nil
		}
		a.RagOn = true
		a.Config.RagEnabled = true
		fmt.Fprintln(out, "RAG context injection: on")
	case "off":
		a.RagOn = false
		a.Config.RagEnabled = false
		fmt.Fprintln(out, "RAG context injection: off")
	case "setup":
		return ragSetup(a, out)
	case "new":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /rag new NAME [--embedder ollama|encoderfile] [--embedder-url URL]")
			return nil
		}
		kind, url := ParseEmbedderFlags(args[2:])
		return ragWizard(a, args[1], kind, url, out)
	case "switch":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /rag switch NAME")
			return nil
		}
		return ragSwitch(a, args[1], out)
	case "drop":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /rag drop NAME")
			return nil
		}
		return ragDrop(a, args[1], out)
	case "ingest":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /rag ingest PATH [PATH...]")
			return nil
		}
		return ragIngest(a, args[1:], out)
	case "query":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /rag query TEXT")
			return nil
		}
		return ragQuery(a, strings.Join(args[1:], " "), out)
	default:
		fmt.Fprintf(out, "Unknown rag subcommand: %s\n", args[0])
	}
	return nil
}

// ragStatus prints the active store details and the full store registry.
func ragStatus(a *Agent, out io.Writer) error {
	enabled := "off"
	if a.RagOn {
		enabled = "on"
	}
	fmt.Fprintf(out, "RAG context injection: %s\n", enabled)

	entry := a.Config.ActiveRagStore()
	if entry == nil {
		fmt.Fprintln(out, "No store configured. Run /rag new NAME or /rag setup to get started.")
		return nil
	}

	fmt.Fprintf(out, "Active store:    %s\n", entry.Name)
	fmt.Fprintf(out, "  Database:      %s\n", entry.DBPath)
	fmt.Fprintf(out, "  Embed model:   %s\n", entry.EmbeddingModel)
	if entry.EmbedderKind == "encoderfile" {
		fmt.Fprintf(out, "  Embedder:      encoderfile (%s)\n", entry.EmbedderURL)
	}
	if a.Rag != nil {
		if n, err := a.Rag.Count(); err == nil {
			fmt.Fprintf(out, "  Chunks:        %d\n", n)
		}
	} else {
		fmt.Fprintln(out, "  (store not open)")
	}
	if len(entry.ModelMap) > 0 {
		fmt.Fprintln(out, "  Model map:")
		for gen, emb := range entry.ModelMap {
			fmt.Fprintf(out, "    %-36s → %s\n", gen, emb)
		}
	}

	if len(a.Config.RagStores) > 1 {
		fmt.Fprintf(out, "\nAll stores (%d):\n", len(a.Config.RagStores))
		for _, e := range a.Config.RagStores {
			marker := "  "
			if e.Name == a.Config.RagActive {
				marker = "* "
			}
			fmt.Fprintf(out, "  %s%-16s %s  (%s)\n", marker, e.Name, e.DBPath, e.EmbeddingModel)
		}
	}
	return nil
}

// ragList prints a brief listing of all registered stores.
func ragList(a *Agent, out io.Writer) error {
	if len(a.Config.RagStores) == 0 {
		fmt.Fprintln(out, "No RAG stores registered. Run /rag new NAME to create one.")
		return nil
	}
	fmt.Fprintf(out, "RAG stores (%d):\n", len(a.Config.RagStores))
	for _, e := range a.Config.RagStores {
		marker := "  "
		if e.Name == a.Config.RagActive {
			marker = "* "
		}
		fmt.Fprintf(out, "  %s%-16s %s  (%s)\n", marker, e.Name, e.DBPath, e.EmbeddingModel)
	}
	return nil
}

// ragSwitch closes the current store and opens the named one.
func ragSwitch(a *Agent, name string, out io.Writer) error {
	entry := a.Config.RagStoreByName(name)
	if entry == nil {
		fmt.Fprintf(out, "Store %q not found. Use /rag list to see available stores.\n", name)
		return nil
	}
	if a.Rag != nil {
		_ = a.Rag.Close()
		a.Rag = nil
	}
	dbPath, err := a.Workspace.AbsPath(entry.DBPath)
	if err != nil {
		return fmt.Errorf("rag switch: %w", err)
	}
	store, err := NewRagStore(dbPath, entry.EmbeddingModel)
	if err != nil {
		return fmt.Errorf("rag switch: open store: %w", err)
	}
	a.Rag = store
	a.Config.RagActive = name
	if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, "Warning: could not persist active store: %v\n", err)
	}
	fmt.Fprintf(out, "Active store: %s (%s)\n", entry.Name, entry.DBPath)
	return nil
}

// ragDrop removes a store from the registry (does not delete the .db file).
func ragDrop(a *Agent, name string, out io.Writer) error {
	entry := a.Config.RagStoreByName(name)
	if entry == nil {
		fmt.Fprintf(out, "Store %q not found.\n", name)
		return nil
	}
	fmt.Fprintf(out, "Remove store %q from registry? The .db file will NOT be deleted.\n", name)
	fmt.Fprintf(out, "  Database: %s\n", entry.DBPath)
	fmt.Fprint(out, "Confirm? [y/N] ")
	scanner := bufio.NewScanner(a.In)
	scanner.Scan()
	if answer := strings.ToLower(strings.TrimSpace(scanner.Text())); answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "Cancelled.")
		return nil
	}
	if name == a.Config.RagActive {
		if a.Rag != nil {
			_ = a.Rag.Close()
			a.Rag = nil
		}
		a.RagOn = false
		a.Config.RagActive = ""
	}
	a.Config.RemoveRagStore(name)
	if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, "Warning: could not persist registry: %v\n", err)
	}
	fmt.Fprintf(out, "Store %q removed. To delete the database: rm %s\n", name, entry.DBPath)
	return nil
}

// ragSetup is the backward-compat /rag setup entry point. It re-runs the
// wizard for the active store, or creates a "default" store when none exists.
func ragSetup(a *Agent, out io.Writer) error {
	name := a.Config.RagActive
	if name == "" {
		name = "default"
	}
	return ragWizard(a, name, "", "", out)
}

/** ParseEmbedderFlags extracts --embedder and --embedder-url values from args.
 * Unrecognised tokens are silently ignored. Both values default to "".
 *
 * Parameters:
 *   args ([]string) — remaining arguments after the store name.
 *
 * Returns:
 *   kind (string) — embedder kind: "ollama", "encoderfile", or "".
 *   url  (string) — embedder base URL, or "".
 *
 * Example:
 *   kind, url := ParseEmbedderFlags([]string{"--embedder", "encoderfile", "--embedder-url", "http://localhost:8080"})
 */
func ParseEmbedderFlags(args []string) (kind, url string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--embedder":
			if i+1 < len(args) {
				i++
				kind = args[i]
			}
		case "--embedder-url":
			if i+1 < len(args) {
				i++
				url = args[i]
			}
		}
	}
	return kind, url
}

// ragWizard runs the interactive setup wizard for a named store, creating or
// reconfiguring it in the registry. embedderKind and embedderURL select the
// embedder backend: "" or "ollama" uses Ollama; "encoderfile" uses an
// Encoderfile binary server at embedderURL.
func ragWizard(a *Agent, name, embedderKind, embedderURL string, out io.Writer) error {
	ctx := context.Background()
	ragDir := filepath.Join(harveySubdir, "rag")
	dbPath := filepath.Join(ragDir, name+".db")

	// ── Encoderfile path ───────────────────────────────────────────────────────
	if embedderKind == "encoderfile" {
		if embedderURL == "" {
			fmt.Fprintln(out, "Encoderfile requires --embedder-url, e.g. --embedder-url http://localhost:8080")
			return nil
		}
		if !ProbeEncoderfile(embedderURL) {
			fmt.Fprintf(out, "Encoderfile server not reachable at %s\n", embedderURL)
			fmt.Fprintln(out, "Start the server: ./your-model.encoderfile serve")
			return nil
		}
		modelID, err := ProbeEncoderfileModel(embedderURL)
		if err != nil {
			return fmt.Errorf("rag wizard: %w", err)
		}
		fmt.Fprintf(out, "Proposed RAG store %q (Encoderfile embedder: %s):\n\n", name, modelID)
		fmt.Fprintf(out, "  Database:      %s\n", dbPath)
		fmt.Fprintf(out, "  Embedder URL:  %s\n", embedderURL)
		fmt.Fprintf(out, "  Model ID:      %s\n\n", modelID)
		fmt.Fprint(out, "Accept? [Y/n] ")

		scanner := bufio.NewScanner(a.In)
		scanner.Scan()
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "" && answer != "y" && answer != "yes" {
			fmt.Fprintln(out, "Setup cancelled.")
			return nil
		}
		if err := a.Workspace.MkdirAll(ragDir); err != nil {
			return fmt.Errorf("rag setup: create directory: %w", err)
		}
		entry := RagStoreEntry{
			Name:           name,
			DBPath:         dbPath,
			EmbeddingModel: modelID,
			EmbedderKind:   "encoderfile",
			EmbedderURL:    embedderURL,
		}
		return ragCommitEntry(a, entry, out)
	}

	// ── Ollama path ────────────────────────────────────────────────────────────
	if !ProbeOllama(a.Config.OllamaURL) {
		fmt.Fprintln(out, "Ollama is not running. Use /ollama start first.")
		return nil
	}

	// Step 0: detect available embedding models via the model cache.
	var embedModels []string
	if a.ModelCache != nil {
		all, err := a.ModelCache.All()
		if err == nil {
			for _, c := range all {
				if c.SupportsEmbed == CapYes {
					embedModels = append(embedModels, c.Name)
				}
			}
		}
	}

	// If cache is empty or no embedding models found, fall back to live detection.
	if len(embedModels) == 0 {
		summaries, err := NewOllamaClient(a.Config.OllamaURL, "").ModelSummaries(ctx)
		if err == nil {
			for _, s := range summaries {
				if hasEmbedKeyword(s.Name) {
					embedModels = append(embedModels, s.Name)
				}
			}
		}
	}

	if len(embedModels) == 0 {
		fmt.Fprintln(out, "No embedding models found on this Ollama server.")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Recommended options (run /ollama pull to install):")
		fmt.Fprintln(out, "  nomic-embed-text        (~274 MB) — best general-purpose retrieval")
		fmt.Fprintln(out, "  mxbai-embed-large       (~670 MB) — high quality retrieval")
		fmt.Fprintln(out, "  qllama/bge-small-en-v1.5 (~46 MB) — small but retrieval-optimized")
		fmt.Fprintln(out, "  bge-m3                  (~1.2 GB) — multilingual (good for SEA-LION)")
		fmt.Fprintln(out, "  (avoid all-minilm — it is similarity-tuned, not retrieval-tuned)")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "After pulling an embedding model, run /rag setup again.")
		fmt.Fprintln(out, "Or use an Encoderfile binary: /rag new NAME --embedder encoderfile --embedder-url URL")
		return nil
	}

	// Pick preferred embedding model in quality order; all are retrieval-optimized
	// except all-minilm (similarity-only) which is the last resort.
	preferred := embedModels[0]
	for _, pref := range []string{"nomic-embed-text", "mxbai-embed-large", "bge-m3", "bge-", "gte-", "e5-", "jina", "all-minilm"} {
		for _, m := range embedModels {
			if strings.Contains(strings.ToLower(m), pref) {
				preferred = m
				goto foundPref
			}
		}
	}
foundPref:

	// Build proposed model map: all non-embedding generation models → preferred embedder.
	genModels, _ := newOllamaLLMClient(a.Config.OllamaURL, "", a.Config.OllamaTimeout).Models(ctx)
	proposed := make(map[string]string)
	for _, m := range genModels {
		if !hasEmbedKeyword(m) {
			embedFor := preferred
			// Multilingual hint: suggest bge-m3 for models with multilingual signals.
			lower := strings.ToLower(m)
			if strings.Contains(lower, "sea") || strings.Contains(lower, "lion") ||
				strings.Contains(lower, "multilingual") || strings.Contains(lower, "multi") {
				for _, em := range embedModels {
					if strings.Contains(strings.ToLower(em), "bge-m3") {
						embedFor = em
						break
					}
				}
			}
			proposed[m] = embedFor
		}
	}

	// Display proposed mapping for human review.
	fmt.Fprintf(out, "Proposed RAG store %q (embedding model: %s):\n\n", name, preferred)
	fmt.Fprintf(out, "  Database: %s\n\n", dbPath)
	if len(proposed) > 0 {
		fmt.Fprintf(out, "  %-36s  %s\n", "Generation model", "Embedding model")
		fmt.Fprintf(out, "  %s  %s\n", strings.Repeat("─", 36), strings.Repeat("─", 24))
		for gen, emb := range proposed {
			fmt.Fprintf(out, "  %-36s  %s\n", ollamaTruncateName(gen, 36), emb)
		}
	}
	fmt.Fprintln(out, "")
	fmt.Fprint(out, "Accept? [Y/n] ")

	scanner := bufio.NewScanner(a.In)
	scanner.Scan()
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "Setup cancelled.")
		return nil
	}

	// Ensure the rag/ subdirectory exists.
	if err := a.Workspace.MkdirAll(ragDir); err != nil {
		return fmt.Errorf("rag setup: create directory: %w", err)
	}

	entry := RagStoreEntry{
		Name:           name,
		DBPath:         dbPath,
		EmbeddingModel: preferred,
		ModelMap:       proposed,
	}
	return ragCommitEntry(a, entry, out)
}

// ragCommitEntry persists entry as the active RAG store, opens its database,
// and enables RAG injection. It is called by both the Ollama and Encoderfile
// wizard paths.
func ragCommitEntry(a *Agent, entry RagStoreEntry, out io.Writer) error {
	a.Config.AddOrUpdateRagStore(entry)
	a.Config.RagActive = entry.Name
	a.Config.RagEnabled = true

	if err := SaveRAGConfig(a.Workspace, a.Config); err != nil {
		return fmt.Errorf("rag setup: save config: %w", err)
	}

	// Close any previously open store, then open the new one.
	if a.Rag != nil {
		_ = a.Rag.Close()
		a.Rag = nil
	}
	absDB, err := a.Workspace.AbsPath(entry.DBPath)
	if err != nil {
		return err
	}
	store, err := NewRagStore(absDB, entry.EmbeddingModel)
	if err != nil {
		return fmt.Errorf("rag setup: open store: %w", err)
	}
	a.Rag = store
	a.RagOn = true

	fmt.Fprintf(out, "RAG store %q configured and enabled.\n", entry.Name)
	fmt.Fprintf(out, "Next step: run /rag ingest <file-or-directory> to populate the store.\n")
	return nil
}

// ragIngest chunks and embeds each path into the RAG store.
func ragIngest(a *Agent, paths []string, out io.Writer) error {
	if a.Rag == nil {
		fmt.Fprintln(out, "RAG is not configured. Run /rag setup first.")
		return nil
	}
	entry := a.Config.ActiveRagStore()
	if entry == nil {
		fmt.Fprintln(out, "No active RAG store. Run /rag switch NAME to select one.")
		return nil
	}
	embedder := NewEmbedderForEntry(entry, a.Config.OllamaURL)

	var total int
	for _, p := range paths {
		absPath, err := a.Workspace.AbsPath(p)
		if err != nil {
			fmt.Fprintf(out, "  skip %s: %v\n", p, err)
			continue
		}
		info, err := os.Stat(absPath)
		if err != nil {
			fmt.Fprintf(out, "  skip %s: %v\n", p, err)
			continue
		}
		if info.IsDir() {
			n, err := ragIngestDir(a.Rag, embedder, absPath, out)
			if err != nil {
				fmt.Fprintf(out, "  error in %s: %v\n", p, err)
			}
			total += n
		} else {
			n, err := ragIngestFile(a.Rag, embedder, absPath)
			if err != nil {
				fmt.Fprintf(out, "  error in %s: %v\n", p, err)
			} else {
				fmt.Fprintf(out, "  %s — %d chunk(s)\n", p, n)
				total += n
			}
		}
	}
	fmt.Fprintf(out, "Ingested %d chunk(s) total.\n", total)
	return nil
}

// ragIngestDir walks a directory and ingests all text files.
func ragIngestDir(store *RagStore, embedder Embedder, dir string, out io.Writer) (int, error) {
	var total int
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".md", ".txt", ".go", ".ts", ".py", ".yaml", ".yml", ".toml", ".sql":
			n, err := ragIngestFile(store, embedder, path)
			if err != nil {
				fmt.Fprintf(out, "  skip %s: %v\n", path, err)
			} else {
				fmt.Fprintf(out, "  %s — %d chunk(s)\n", path, n)
				total += n
			}
		}
		return nil
	})
	return total, err
}

// ragIngestFile reads a file, splits it into paragraph chunks, and ingests them.
func ragIngestFile(store *RagStore, embedder Embedder, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	chunks := ragChunk(string(data))
	if len(chunks) == 0 {
		return 0, nil
	}
	if err := store.Ingest(path, chunks, embedder); err != nil {
		return 0, err
	}
	return len(chunks), nil
}

// ragChunk splits text into paragraph-sized chunks of at most ~500 characters,
// further splitting oversized paragraphs at sentence boundaries.
func ragChunk(text string) []string {
	const maxChunk = 500

	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	var chunks []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) <= maxChunk {
			chunks = append(chunks, p)
			continue
		}
		// Split long paragraphs at sentence ends.
		sentences := strings.FieldsFunc(p, func(r rune) bool {
			return r == '.' || r == '!' || r == '?'
		})
		var buf strings.Builder
		for _, s := range sentences {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if buf.Len()+len(s)+2 > maxChunk && buf.Len() > 0 {
				chunks = append(chunks, buf.String())
				buf.Reset()
			}
			if buf.Len() > 0 {
				buf.WriteString(". ")
			}
			buf.WriteString(s)
		}
		if buf.Len() > 0 {
			chunks = append(chunks, buf.String())
		}
	}
	return chunks
}

// ragQuery runs a manual retrieval test against the RAG store.
func ragQuery(a *Agent, query string, out io.Writer) error {
	if a.Rag == nil {
		fmt.Fprintln(out, "RAG is not configured. Run /rag setup first.")
		return nil
	}
	entry := a.Config.ActiveRagStore()
	if entry == nil {
		fmt.Fprintln(out, "No active RAG store. Run /rag switch NAME to select one.")
		return nil
	}
	embedder := NewEmbedderForEntry(entry, a.Config.OllamaURL)
	chunks, err := a.Rag.Query(query, embedder, 5)
	if err != nil {
		return fmt.Errorf("rag query: %w", err)
	}
	if len(chunks) == 0 {
		fmt.Fprintln(out, "No results. The store may be empty — run /rag ingest first.")
		return nil
	}
	fmt.Fprintf(out, "Top %d result(s) for %q:\n\n", len(chunks), query)
	for i, c := range chunks {
		preview := c.Content
		if len([]rune(preview)) > 120 {
			preview = string([]rune(preview)[:119]) + "…"
		}
		if c.Source != "" {
			fmt.Fprintf(out, "  [%d] score=%.3f  source=%s\n      %s\n\n", i+1, c.Score, c.Source, preview)
		} else {
			fmt.Fprintf(out, "  [%d] score=%.3f  %s\n\n", i+1, c.Score, preview)
		}
	}
	return nil
}
