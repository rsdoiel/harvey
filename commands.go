package harvey

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// sensitiveCmdEnvVars contains environment variable names that should be
// EXCLUDED from child processes to prevent sensitive data leakage.
//
// Note: filterCommandEnvironment uses a whitelist approach, so variables not
// matching a safe prefix are already blocked. This denylist is defence-in-depth:
// it ensures that if a broad prefix is ever added to safeCmdEnvPrefixes, known
// sensitive names are still stripped first.
var sensitiveCmdEnvVars = []string{
	// LLM provider API keys
	"ANTHROPIC_API_KEY",
	"COHERE_API_KEY",
	"DEEPSEEK_API_KEY",
	"FIREWORKS_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"GROQ_API_KEY",
	"HUGGINGFACE_TOKEN",
	"MISTRAL_API_KEY",
	"OPENAI_API_KEY",
	"PERPLEXITY_API_KEY",
	"PUBLICAI_API_KEY",
	"REPLICATE_API_KEY",
	"TOGETHER_API_KEY",
	"XAI_API_KEY",
	// S3-compatible storage credentials (AWS, MinIO, Cloudflare R2)
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"AWS_SECURITY_TOKEN",
	"MINIO_ACCESS_KEY",
	"MINIO_SECRET_KEY",
	// SFTP/SCP credentials
	"SFTP_PASSWORD",
	"SFTP_KEY_PATH",
	// HTTP authentication credentials
	"HTTP_BEARER_TOKEN",
	"HTTP_BASIC_PASSWORD",
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
 *   Usage          (string)   — short usage synopsis shown by /help.
 *   Description    (string)   — one-line description shown by /help.
 *   UserDefined    (bool)     — true for commands generated from compiled skills.
 *   Handler        (func)     — called when the command is dispatched; nil for
 *                               commands handled directly in the REPL (exit, quit).
 *   Subcommands    ([]string) — valid second-token subcommand names; used by
 *                               buildCompleter for tab completion. Empty for
 *                               commands that take no subcommand.
 *   ArgCompletion  (map)      — maps each subcommand name to a function that
 *                               returns candidate strings for its first positional
 *                               argument. Called at tab time; must not make LLM
 *                               or network calls. nil if not applicable.
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
	Handler       func(a *Agent, args []string, out io.Writer) error
	Subcommands   []string
	ArgCompletion map[string]func(*Agent) []string
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
		"kb": {
			Usage:       "/kb <status|search|inject|project|observe|concept> [args...]",
			Description: "Manage and query the workspace knowledge base",
			Handler:     cmdKB,
			Subcommands: []string{"status", "search", "inject", "project", "observe", "concept"},
		},
		"memory": {
			Usage:       "/memory <mine|list|show|flag|forget|status|recall|profile> [args...]",
			Description: "Mine sessions for memories and manage the memory store",
			Handler:     cmdMemory,
			Subcommands: []string{"mine", "list", "show", "flag", "forget", "status", "recall", "profile"},
			ArgCompletion: map[string]func(*Agent) []string{
				"list":   memoryTypeCandidates,
				"show":   memoryIDCandidates,
				"flag":   memoryIDCandidates,
				"forget": memoryIDCandidates,
			},
		},
		"rag": {
			Usage:       "/rag <list|new NAME|use NAME|drop NAME|ingest PATH|status|query TEXT|on|off>",
			Description: "Manage named RAG knowledge stores for context-augmented generation",
			Handler:     cmdRag,
			Subcommands: []string{"list", "new", "use", "show", "remove", "drop", "ingest", "status", "query", "on", "off"},
			ArgCompletion: map[string]func(*Agent) []string{
				"use":    ragStoreNameCandidates,
				"remove": ragStoreNameCandidates,
				"drop":   ragStoreNameCandidates,
			},
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
		"attach": {
			Usage:       "/attach FILE",
			Description: "Attach a file to the next turn: image (native or text fallback), PDF (text extraction), or plain text",
			Handler:     cmdAttach,
		},
		"read-pdf": {
			Usage:       "/read-pdf FILE [PAGES]",
			Description: "Extract text from a PDF and inject it into context (requires poppler)",
			Handler:     cmdReadPDF,
		},
		"write": {
			Usage:       "/write PATH",
			Description: "Write the last assistant reply (or its first code block) to a file",
			Handler:     cmdWrite,
		},
		"loop": {
			Usage:       "/loop INTERVAL [--count N] PROMPT|/COMMAND",
			Description: "Repeat a prompt or command on an interval, up to N times (default 10, max 100)",
			Handler:     cmdLoop,
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
			Subcommands: []string{"status", "diff", "log", "show", "blame"},
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
		"context": {
			Usage:       "/context <show|add TEXT...|clear>",
			Description: "Manage pinned context that survives /clear",
			Handler:     cmdContext,
			Subcommands: []string{"show", "add", "clear"},
		},
		"audit": {
			Usage:       "/audit <show [n]|clear|status>",
			Description: "View or clear the audit log of security-relevant events",
			Handler:     cmdAudit,
			Subcommands: []string{"show", "clear", "status"},
		},
		"permissions": {
			Usage:       "/permissions <list [PATH]|set PATH PERMS|reset>",
			Description: "Manage workspace file permissions (read, write, exec, delete)",
			Handler:     cmdPermissions,
			Subcommands: []string{"list", "set", "reset"},
		},
		"security": {
			Usage:       "/security status",
			Description: "Show security settings status (safe mode, permissions, audit)",
			Handler:     cmdSecurity,
			Subcommands: []string{"status"},
		},
		"record": {
			Usage:       "/record <start [FILE]|stop|status>",
			Description: "Record session exchanges to a Markdown file",
			Handler:     cmdRecord,
			Subcommands: []string{"start", "stop", "status"},
		},
		"session": {
			Usage:       "/session <list|show [FILE]|use FILE|continue FILE|replay FILE [OUTPUT]>",
			Description: "List, inspect, load, or replay .spmd/.fountain session recordings",
			Handler:     cmdSession,
			Subcommands: []string{"list", "show", "use", "continue", "replay"},
		},
		"rename": {
			Usage:       "/rename NAME",
			Description: "Rename the active session recording file",
			Handler:     cmdRename,
		},
		"read-dir": {
			Usage:       "/read-dir [PATH] [--depth N]",
			Description: "Read all eligible files in a directory into context",
			Handler:     cmdReadDir,
		},
		"file-tree": {
			Usage:       "/file-tree [PATH]",
			Description: "Display a tree listing of the workspace (or a subdirectory)",
			Handler:     cmdFileTree,
		},
		"skill": {
			Usage:       "/skill <list|load NAME|show NAME|info NAME|status|new|run NAME|suggest [SESSION]>",
			Description: "List or load Agent Skills from the skill catalog",
			Handler:     cmdSkill,
			Subcommands: []string{"list", "load", "show", "info", "status", "new", "run", "suggest"},
			ArgCompletion: map[string]func(*Agent) []string{
				"load": skillNameCandidates,
				"show": skillNameCandidates,
				"info": skillNameCandidates,
				"run":  skillNameCandidates,
			},
		},
		"skill-set": {
			Usage:       "/skill-set <list|load NAME|show NAME|info NAME|new NAME|create NAME|status|unload>",
			Description: "Load or manage named bundles of skills from agents/skill-sets/",
			Handler:     cmdSkillSet,
			Subcommands: []string{"list", "load", "show", "info", "new", "create", "status", "unload"},
		},
		"inspect": {
			Usage:       "/inspect [MODEL]",
			Description: "Show capability details for installed Ollama models; useful for multi-model routing",
			Handler:     cmdInspect,
		},
		"route": {
			Usage:       "/route <add NAME URL [MODEL] | rm NAME | use [NAME] | models URL | probe NAME | set NAME tools on|off | list | on | off | status>",
			Description: "Register remote LLM endpoints and dispatch to them with @name in prompts",
			Handler:     cmdRoute,
			Subcommands: []string{"add", "rm", "remove", "use", "models", "probe", "set", "list", "on", "off", "status"},
			ArgCompletion: map[string]func(*Agent) []string{
				"rm":     routeNameCandidates,
				"remove": routeNameCandidates,
				"use":    routeNameCandidates,
				"probe":  routeNameCandidates,
				"set":   routeNameCandidates,
			},
		},
		"safemode": {
			Usage:       "/safemode <on|off|status|allow CMD|deny CMD|reset>",
			Description: "Enable/disable safe mode or manage the command allowlist",
			Handler:     cmdSafeMode,
			Subcommands: []string{"on", "off", "status", "allow", "deny", "reset"},
		},
		"safe": {
			Usage:       "/safe <on|off|status|allow CMD|deny CMD|reset>",
			Description: "Alias for /safemode",
			Handler:     cmdSafeMode,
			Subcommands: []string{"on", "off", "status", "allow", "deny", "reset"},
		},
		"pipeline": {
			Usage:       "/pipeline <CONFIDENCE%> FILE [FILE ...]",
			Description: "Chain Markdown prompt files through models with confidence gating",
			Handler:     cmdPipeline,
		},
		"model": {
			Usage:       "/model [list|use [NAME]|show [NAME]|status|stop|clean|mode [MODEL] MODE|alias ...]",
			Description: "Unified model management across llamafile, llama.cpp, and Ollama backends",
			Handler:     cmdModel,
			Subcommands: []string{"list", "use", "show", "status", "stop", "clean", "mode", "alias"},
			ArgCompletion: map[string]func(*Agent) []string{
				"use": func(a *Agent) []string { return allModelNames(a) },
			},
		},
		"plan": {
			Usage:       "/plan <TASK | next | status | show | clear>",
			Description: "Generate a step-by-step plan and execute it with bounded context per step",
			Handler:     cmdPlan,
			Subcommands: []string{"next", "status", "show", "clear"},
		},
		"hint": {
			Usage:       "/hint",
			Description: "Show suggestions for improving results (RAG, memory, KB)",
			Handler:     cmdHint,
		},
		"recall": {
			Usage:       "/recall <query>",
			Description: "Alias for /memory recall — search all knowledge silos",
			Handler: func(a *Agent, args []string, out io.Writer) error {
				return cmdMemory(a, append([]string{"recall"}, args...), out)
			},
		},
		"profile": {
			Usage:       "/profile <list|show|edit|use|rename|on|off> [args...]",
			Description: "Alias for /memory profile — manage workspace profile",
			Handler: func(a *Agent, args []string, out io.Writer) error {
				return cmdMemory(a, append([]string{"profile"}, args...), out)
			},
			Subcommands: []string{"list", "show", "edit", "use", "rename", "on", "off"},
			ArgCompletion: map[string]func(*Agent) []string{
				"use": profileTemplateNameCandidates,
			},
		},
		"format": {
			Usage:       "/format FILE [FILE...]",
			Description: "Format source file(s) in-place using the registered formatter for each file's language",
			Handler:     cmdFormat,
		},
		"workspace": {
			Usage:       "/workspace <init [FROM_PATH]|status>",
			Description: "Manage workspace settings — init copies aliases from another workspace or YAML file",
			Handler:     cmdWorkspace,
			Subcommands: []string{"init", "status"},
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
				_, err := DispatchSkill(context.Background(), ag, captured, prompt, reader, out)
				return err
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
		if a.Backend != nil {
			_ = a.Backend.Stop()
		}
		return true, nil
	}
	cmd, ok := a.commands[name]
	if !ok {
		fmt.Fprintf(out, yellow("Unknown command: ")+"/%s  (type /help for a list)\n", name)
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
		topic := strings.ToLower(args[0])
		if topic == "topics" || topic == "index" {
			fmt.Fprint(out, HelpTopicsText())
			return nil
		}
		if !PrintHelpTopic(out, topic, "", "", "", "") {
			fmt.Fprintf(out, "  Unknown help topic %q.\n  Type /help topics for the topic index.\n\n", args[0])
		}
		return nil
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
	fmt.Fprintln(out, "  Type /help TOPIC for a full guide, or /help topics for the topic index.")
	fmt.Fprintln(out)
	return nil
}

func cmdStatus(a *Agent, _ []string, out io.Writer) error {
	if a.Client == nil {
		fmt.Fprintln(out, "Backend:   none")
	} else {
		tag := ""
		if ac, ok := a.Client.(*AnyLLMClient); ok {
			switch ac.ProviderName() {
			case "ollama", "llamafile", "llamacpp":
				if a.Backend != nil && a.Backend.StartedByHarvey() {
					tag = " [Harvey]"
				} else {
					tag = " [external]"
				}
			}
		}
		fmt.Fprintf(out, "Backend:   %s%s\n", a.Client.Name(), tag)
	}
	if a.Config.Debug {
		fmt.Fprintf(out, "Debug:     on (%s)\n", a.DebugLog.Path())
	}
	fmt.Fprintf(out, "History:   %d messages\n", len(a.History))
	if ac, ok := a.Client.(*AnyLLMClient); ok && len(a.History) > 0 {
		var n int
		var qualifier string
		switch ac.ProviderName() {
		case "ollama":
			exact := false
			n, exact = CountTokens(context.Background(), ac.BackendURL(), ac.ModelName(), HistoryText(a.History))
			if exact {
				qualifier = ""
			} else {
				qualifier = "~"
			}
		default:
			// For llamafile and cloud providers: estimate via character count.
			n = estimateTokens(HistoryText(a.History))
			qualifier = "~"
		}
		limit := a.effectiveContextLimit()
		if limit > 0 {
			pct := n * 100 / limit
			fmt.Fprintf(out, "Tokens:    %s%d / %d (%d%%)\n", qualifier, n, limit, pct)
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
	// Memory store summary
	if a.Config.Memory.Enabled && a.Memory != nil && a.Memory.Store != nil {
		store := a.Memory.Store
		if n, err := store.Count(); err == nil {
			sessDir := a.SessionsDir
			if sessDir == "" {
				sessDir = filepath.Join(a.Workspace.Root, harveySubdir, "sessions")
			}
			unminedCount := 0
			if manifest, err := LoadManifest(store.Dir()); err == nil {
				if unmined, err := manifest.UnminedSessions(sessDir); err == nil {
					unminedCount = len(unmined)
				}
			}
			fmt.Fprintf(out, "Memory:    %d active", n)
			if unminedCount > 0 {
				fmt.Fprintf(out, "  (%d session(s) unmined)", unminedCount)
			}
			fmt.Fprintln(out)
		}
		// Active workspace profile
		if profiles, err := store.List(string(MemoryTypeWorkspaceProfile)); err == nil {
			if len(profiles) > 0 {
				fmt.Fprintf(out, "Profile:   %s (%s)\n", profiles[0].Description, profiles[0].ID)
			} else {
				fmt.Fprintln(out, "Profile:   (none — run /profile use to set one)")
			}
		}
	}
	// RAG summary
	if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
		ragState := "off"
		if a.RagOn {
			ragState = "on"
		}
		if a.Rag != nil {
			if n, err := a.Rag.Count(); err == nil {
				fmt.Fprintf(out, "RAG:       %s — store %q, %d chunk(s)\n", ragState, entry.Name, n)
			} else {
				fmt.Fprintf(out, "RAG:       %s — store %q\n", ragState, entry.Name)
			}
		} else {
			fmt.Fprintf(out, "RAG:       %s — store %q (not open)\n", ragState, entry.Name)
		}
	} else {
		fmt.Fprintln(out, "RAG:       not configured")
	}
	if a.Config.Security.SafeMode {
		fmt.Fprintln(out, "Safe mode: on")
	} else {
		fmt.Fprintln(out, "Safe mode: OFF (all commands permitted)")
	}
	return nil
}

func cmdClear(a *Agent, _ []string, out io.Writer) error {
	a.ClearHistory()
	fmt.Fprintln(out, "Conversation history cleared.")
	return nil
}

/** cmdHint prints on-demand suggestions for improving Harvey's results by
 * auditing the three knowledge silos and surfacing actionable advice.
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   args ([]string)  — unused.
 *   out  (io.Writer) — output destination.
 *
 * Returns:
 *   error — always nil; errors are printed inline.
 *
 * Example:
 *   /hint
 */
// allModelNames returns all registered model names across llamafile and ollama
// for use in tab completion.
func allModelNames(a *Agent) []string {
	var names []string
	for _, e := range a.Config.Llamafile.Models {
		names = append(names, e.Name)
	}
	for alias := range a.Config.ModelAliases {
		names = append(names, alias)
	}
	return names
}

/** cmdModel dispatches /model subcommands: list, use, show, status.
 * With no subcommand, prints the active model and backend.
 *
 * Parameters:
 *   a    (*Agent)   — the running Harvey agent.
 *   args ([]string) — subcommand and arguments.
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — non-nil on unexpected failures.
 *
 * Example:
 *   cmdModel(agent, []string{"list"}, os.Stdout)
 */
func cmdModel(a *Agent, args []string, out io.Writer) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		return cmdModelList(a, out)
	case "use":
		if len(args) < 2 {
			return pickAndUseModel(a, out)
		}
		switched, err := attemptModelSwitch(a, args[1], out)
		if err != nil {
			return err
		}
		if !switched {
			fmt.Fprintf(out, "  Model %q not found — use /model use (no arg) for a picker.\n", args[1])
		}
		return nil
	case "alias":
		return cmdModelAlias(a, args[1:], out)
	case "mode":
		return cmdModelMode(a, args[1:], out)
	case "status":
		return cmdModelStatus(a, out)
	case "stop":
		return cmdModelStop(a, out)
	case "show":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		return cmdModelShowEntry(a, name, out)
	case "clean":
		return cmdModelClean(a, out)
	default:
		return cmdModelShowEntry(a, "", out)
	}
}

// cmdModelClean removes stale model aliases whose model is no longer available
// on any known backend. Aliases with Engine=="" (legacy, no engine recorded)
// are preserved because their backend cannot be determined safely.
func cmdModelClean(a *Agent, out io.Writer) error {
	// Collect live model names per engine.
	var liveOllama, liveLlamafile, liveLlamaCpp []string

	agentsDir := ""
	if a.Workspace != nil {
		agentsDir = a.Workspace.Root + "/agents"
	}
	workspaceRoot := ""
	if a.Workspace != nil {
		workspaceRoot = a.Workspace.Root
	}

	lb := NewLlamafileBackend(a.Config, agentsDir, workspaceRoot)
	if models, err := lb.ListModels(); err == nil {
		for _, m := range models {
			liveLlamafile = append(liveLlamafile, m.Name)
		}
	}

	cb := NewLlamaCppBackend(a.Config, agentsDir)
	if models, err := cb.ListModels(); err == nil {
		for _, m := range models {
			liveLlamaCpp = append(liveLlamaCpp, m.Name)
		}
	}

	if ProbeOllama(a.Config.Ollama.URL) {
		if summaries, err := NewOllamaClient(a.Config.Ollama.URL, "").ModelSummaries(context.Background()); err == nil {
			for _, s := range summaries {
				liveOllama = append(liveOllama, s.Name)
			}
		}
	}

	n, err := pruneStaleModelRefs(a, liveOllama, liveLlamafile, liveLlamaCpp, out)
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Fprintln(out, "  No stale aliases found.")
	} else {
		fmt.Fprintf(out, "  Removed %d stale alias(es).\n", n)
	}
	return nil
}

// cmdModelShowEntry prints details for the named model, or the active model when name is "".
// For llamafile models it shows path, size, and context length.
func cmdModelShowEntry(a *Agent, name string, out io.Writer) error {
	if name == "" {
		label := activeModelLabel(a)
		if a.Client != nil {
			fmt.Fprintf(out, "  Active model: %s\n", green(label))
		} else {
			fmt.Fprintf(out, "  Active model: %s\n", yellow(label))
		}
		// If the active model is a llamafile entry, show details too.
		if a.Config.Llamafile.Active != "" {
			name = a.Config.Llamafile.Active
		}
	}
	if name == "" {
		fmt.Fprintln(out, "  No active llamafile model.")
		return nil
	}
	entry := a.Config.LlamafileEntryByName(name)
	if entry == nil {
		fmt.Fprintf(out, "  llamafile %q not found.\n", name)
		return nil
	}
	active := ""
	if entry.Name == a.Config.Llamafile.Active {
		active = " (active)"
	}
	fmt.Fprintf(out, "  Name:    %s%s\n", entry.Name, active)
	absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
	fmt.Fprintf(out, "  Path:    %s\n", absPath)
	if info, err := os.Stat(absPath); err == nil {
		fmt.Fprintf(out, "  Size:    %s\n", llamafileFormatBytes(info.Size()))
	} else {
		fmt.Fprintln(out, "  Size:    (file not found)")
	}
	if entry.ContextLength > 0 {
		fmt.Fprintf(out, "  Context: %d tokens\n", entry.ContextLength)
	} else {
		fmt.Fprintln(out, "  Context: unknown")
	}
	return nil
}

// cmdModelList prints all locally available models across every backend.
// It uses aggregateModels so llamafile, llama.cpp, and Ollama entries
// appear together without separate per-engine commands.
func cmdModelList(a *Agent, out io.Writer) error {
	models, err := aggregateModels(a)
	if err != nil {
		return fmt.Errorf("/model list: %w", err)
	}
	if len(models) == 0 {
		fmt.Fprintln(out, "  No models found. Use /model use to start one.")
		return nil
	}

	activeEngine := ""
	activeModel := ""
	if a.Backend != nil {
		activeEngine = a.Backend.Name()
		activeModel = a.Backend.ActiveModel()
	} else if a.Config.Llamafile.Active != "" {
		activeEngine = "llamafile"
		activeModel = a.Config.Llamafile.Active
	} else if a.Config.Ollama.Model != "" {
		activeEngine = "ollama"
		activeModel = a.Config.Ollama.Model
	}

	for _, m := range models {
		active := m.Engine == activeEngine && strings.EqualFold(m.Name, activeModel)
		marker := "  "
		if active {
			marker = "→ "
		}
		fmt.Fprintf(out, "  %s%-30s (%s)\n", marker, m.Name, m.Engine)
	}
	return nil
}

// cmdModelStatus prints backend-specific connection status for the active backend.
func cmdModelStatus(a *Agent, out io.Writer) error {
	if a.Backend == nil {
		fmt.Fprintln(out, "  No active backend.")
		return nil
	}
	switch a.Backend.Name() {
	case "llamafile":
		active := a.Config.Llamafile.Active
		if active == "" {
			active = "(none)"
		}
		reachable := "no"
		if ProbeLlamafile(a.Config.Llamafile.URL) {
			reachable = "yes"
		}
		managed := "no"
		if a.Backend.StartedByHarvey() {
			managed = "yes (started by Harvey)"
		}
		fmt.Fprintf(out, "  Active model:    %s\n", active)
		fmt.Fprintf(out, "  API URL:         %s\n", a.Config.Llamafile.URL)
		fmt.Fprintf(out, "  Reachable:       %s\n", reachable)
		fmt.Fprintf(out, "  Process managed: %s\n", managed)
		fmt.Fprintf(out, "  Models dir:      %s\n", a.Config.Llamafile.ModelsDir)
		fmt.Fprintf(out, "  Registered:      %d model(s)\n", len(a.Config.Llamafile.Models))
	case "llamacpp":
		cfg := &a.Config.LlamaCpp
		url := cfg.URL
		if url == "" {
			url = "http://127.0.0.1:8081"
		}
		reachable := "no"
		if probeLlamaCpp(url) {
			reachable = "yes"
		}
		managed := "no"
		if a.Backend.StartedByHarvey() {
			managed = "yes (started by Harvey)"
		}
		active := a.Backend.ActiveModel()
		if active == "" {
			active = "(none)"
		}
		fmt.Fprintf(out, "  Active model:    %s\n", active)
		fmt.Fprintf(out, "  API URL:         %s\n", url)
		fmt.Fprintf(out, "  Reachable:       %s\n", reachable)
		fmt.Fprintf(out, "  Managed:         %s\n", managed)
		if cfg.GPULayers > 0 {
			fmt.Fprintf(out, "  GPU layers:      %d\n", cfg.GPULayers)
		}
	default:
		label := activeModelLabel(a)
		reachable := probeActiveBackend(a)
		status := red("✗ not reachable")
		if reachable {
			status = green("✓ reachable")
		}
		fmt.Fprintf(out, "  Model:  %s\n", label)
		fmt.Fprintf(out, "  Status: %s\n", status)
	}
	return nil
}

// cmdModelStop stops the active managed backend (llamafile or llama.cpp).
// Backends not started by Harvey are not stopped.
func cmdModelStop(a *Agent, out io.Writer) error {
	if a.Backend == nil {
		fmt.Fprintln(out, "  No managed backend is active.")
		return nil
	}
	if !a.Backend.StartedByHarvey() {
		fmt.Fprintf(out, "  The %s backend was not started by Harvey — not stopping.\n", a.Backend.Name())
		return nil
	}
	backendName := a.Backend.Name()
	modelName := a.Backend.ActiveModel()
	if err := a.Backend.Stop(); err != nil {
		return err
	}
	a.Backend = nil
	a.Client = nil
	fmt.Fprintf(out, "  Stopped %s backend (model: %s).\n", backendName, modelName)
	return nil
}

// isValidToolMode reports whether s is a recognised tool-mode value.
// "auto" (the empty-string alias) clears a previously set override.
func isValidToolMode(s string) bool {
	switch s {
	case "auto", ToolModeStructured, ToolModeProse, ToolModeInject, ToolModeNone:
		return true
	}
	return false
}

/** cmdModelMode sets or displays the ToolMode for a model in the cache.
 *
 * Usage:
 *   /model mode                       — show active model's current mode
 *   /model mode MODE                  — set active model's mode
 *   /model mode MODEL MODE            — set named model's mode
 *
 * Valid modes: structured, prose, inject, none.
 *
 * Parameters:
 *   a    (*Agent)   — the active Harvey agent.
 *   args ([]string) — subcommand arguments (after "mode").
 *   out  (io.Writer) — output stream for user-facing messages.
 *
 * Returns:
 *   error — non-nil when the model cache is unavailable.
 *
 * Example:
 *   cmdModel(a, []string{"mode", "inject"}, os.Stdout)
 */
func cmdModelMode(a *Agent, args []string, out io.Writer) error {
	if a.ModelCache == nil {
		return fmt.Errorf("no model cache — run /probe first to populate it")
	}

	activeModelName := func() (string, error) {
		ac, ok := a.Client.(*AnyLLMClient)
		if !ok {
			return "", fmt.Errorf("no active Ollama model")
		}
		return ac.ModelName(), nil
	}

	var modelName, mode string
	switch len(args) {
	case 0:
		// Show current mode for the active model.
		name, err := activeModelName()
		if err != nil {
			return err
		}
		cap, err := a.ModelCache.Get(name)
		if err != nil {
			return err
		}
		if cap == nil || cap.ToolMode == ToolModeAuto {
			fmt.Fprintf(out, "  %s: tool mode auto (not set)\n", name)
		} else {
			fmt.Fprintf(out, "  %s: tool mode %s\n", name, cap.ToolMode)
		}
		return nil
	case 1:
		name, err := activeModelName()
		if err != nil {
			return err
		}
		modelName = name
		mode = args[0]
	case 2:
		modelName = args[0]
		mode = args[1]
	default:
		fmt.Fprintf(out, "  Usage: /model mode [MODEL] {auto|structured|prose|inject|none}\n")
		return nil
	}

	if !isValidToolMode(mode) {
		fmt.Fprintf(out, "  Unknown mode %q. Valid modes: auto, structured, prose, inject, none\n", mode)
		return nil
	}

	cap, err := a.ModelCache.Get(modelName)
	if err != nil {
		return err
	}
	if cap == nil {
		cap = &ModelCapability{Name: modelName, ProbeLevel: "none", ProbedAt: time.Now()}
	}
	if mode == "auto" {
		cap.ToolMode = ToolModeAuto
		if err := a.ModelCache.Set(cap); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s: tool mode reset to auto (capability-detected)\n", modelName)
		return nil
	}
	cap.ToolMode = mode
	if err := a.ModelCache.Set(cap); err != nil {
		return err
	}
	fmt.Fprintf(out, "  %s: tool mode set to %s\n", modelName, mode)
	return nil
}

func cmdHint(a *Agent, _ []string, out io.Writer) error {
	hints := 0

	// --- Memory store ---
	if a.Config.Memory.Enabled && a.Memory != nil && a.Memory.Store != nil {
		sessDir := a.SessionsDir
		if sessDir == "" {
			sessDir = filepath.Join(a.Workspace.Root, harveySubdir, "sessions")
		}
		if manifest, err := LoadManifest(a.Memory.Store.Dir()); err == nil {
			if unmined, err := manifest.UnminedSessions(sessDir); err == nil && len(unmined) > 0 {
				fmt.Fprintf(out, "  Sessions unmined: %d\n", len(unmined))
				fmt.Fprintln(out, "    Harvey can extract learnings from these sessions.")
				fmt.Fprintln(out, "    Run: /memory mine")
				fmt.Fprintln(out)
				hints++
			}
		}
	}

	// --- RAG store ---
	if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
		if a.Rag != nil {
			if n, err := a.Rag.Count(); err == nil {
				if n == 0 {
					fmt.Fprintf(out, "  RAG store %q is empty.\n", entry.Name)
					fmt.Fprintln(out, "    Ingest reference documents to give Harvey topic-specific knowledge.")
					fmt.Fprintln(out, "    Run: /rag ingest <file>   (PDF, .md, .txt, .go, .ts, ...)")
					fmt.Fprintln(out, "    See: /help rag")
					fmt.Fprintln(out)
					hints++
				} else if !a.RagOn {
					fmt.Fprintf(out, "  RAG is off but store %q has %d chunk(s).\n", entry.Name, n)
					fmt.Fprintln(out, "    Enabling RAG prepends relevant chunks to each prompt.")
					fmt.Fprintln(out, "    Run: /rag on")
					fmt.Fprintln(out)
					hints++
				}
			}
		} else {
			fmt.Fprintf(out, "  RAG store %q is configured but not open.\n", entry.Name)
			fmt.Fprintln(out, "    Run: /rag status")
			fmt.Fprintln(out)
			hints++
		}
	} else {
		fmt.Fprintln(out, "  No RAG store configured.")
		fmt.Fprintln(out, "    Create one to give Harvey access to reference documents.")
		fmt.Fprintln(out, "    Run: /rag new NAME   then   /rag ingest <file>")
		fmt.Fprintln(out, "    See: /help learn")
		fmt.Fprintln(out)
		hints++
	}

	// --- Knowledge base ---
	if a.KB == nil {
		fmt.Fprintln(out, "  Knowledge base not open.")
		fmt.Fprintln(out, "    Use /kb observe to record experiment findings that persist across sessions.")
		fmt.Fprintln(out, "    See: /help kb")
		fmt.Fprintln(out)
		hints++
	}

	if hints == 0 {
		fmt.Fprintln(out, "  Everything looks good — RAG is on with chunks, sessions are mined, KB is open.")
		fmt.Fprintln(out, "  Use /help learn for the full memory overview.")
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
	a.Config.Security.SafeMode = true
	if a.Workspace != nil {
		if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist safe mode: %v\n", err)
		}
	}
	fmt.Fprintln(out, "  Safe mode enabled. Only allowed commands can be executed.")
	fmt.Fprintf(out, "  Allowed: %s\n", strings.Join(a.Config.Security.AllowedCommands, ", "))
	return nil
}

func safeModeOff(a *Agent, out io.Writer) error {
	a.Config.Security.SafeMode = false
	if a.Workspace != nil {
		if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist safe mode: %v\n", err)
		}
	}
	fmt.Fprintln(out, "  Safe mode disabled. All commands are allowed.")
	return nil
}

func safeModeStatus(a *Agent, out io.Writer) error {
	if a.Config.Security.SafeMode {
		fmt.Fprintln(out, "  Safe mode: on")
		fmt.Fprintf(out, "  Allowed commands (%d): %s\n", len(a.Config.Security.AllowedCommands), strings.Join(a.Config.Security.AllowedCommands, ", "))
	} else {
		fmt.Fprintln(out, "  Safe mode: off")
		fmt.Fprintln(out, "  All commands are allowed.")
	}
	return nil
}

func safeModeAllow(a *Agent, cmd string, out io.Writer) error {
	oldLen := len(a.Config.Security.AllowedCommands)
	a.Config.AddAllowedCommand(cmd)
	if len(a.Config.Security.AllowedCommands) > oldLen {
		fmt.Fprintf(out, "  Added %q to allowlist.\n", cmd)
	} else {
		fmt.Fprintf(out, "  %q is already in the allowlist.\n", cmd)
	}
	if a.Workspace != nil {
		if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist allowlist: %v\n", err)
		}
	}
	return nil
}

func safeModeDeny(a *Agent, cmd string, out io.Writer) error {
	oldLen := len(a.Config.Security.AllowedCommands)
	a.Config.RemoveAllowedCommand(cmd)
	if len(a.Config.Security.AllowedCommands) < oldLen {
		fmt.Fprintf(out, "  Removed %q from allowlist.\n", cmd)
	} else {
		fmt.Fprintf(out, "  %q is not in the allowlist.\n", cmd)
	}
	if a.Workspace != nil {
		if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist allowlist: %v\n", err)
		}
	}
	return nil
}

func safeModeReset(a *Agent, out io.Writer) error {
	a.Config.ResetAllowedCommands()
	if a.Workspace != nil {
		if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  Warning: could not persist allowlist: %v\n", err)
		}
	}
	fmt.Fprintln(out, "  Allowlist reset to defaults.")
	fmt.Fprintf(out, "  Allowed commands: %s\n", strings.Join(a.Config.Security.AllowedCommands, ", "))
	return nil
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

// pruneStaleModelRefs removes model_aliases that point to models no longer
// available on their respective backend. Each live-model slice covers one
// engine (ollama, llamafile, llamacpp); pass nil to skip that engine.
// Aliases with Engine=="" are preserved — their backend is unknown.
// Returns the count of removed aliases; saves config when changed.
func pruneStaleModelRefs(a *Agent, liveOllama, liveLlamafile, liveLlamaCpp []string, out io.Writer) (int, error) {
	byEngine := map[string]map[string]bool{
		"ollama":    setOf(liveOllama),
		"llamafile": setOf(liveLlamafile),
		"llamacpp":  setOf(liveLlamaCpp),
	}

	removed := 0
	changed := false
	for alias, entry := range a.Config.ModelAliases {
		if entry.Engine == "" {
			continue // legacy alias — cannot determine backend safely
		}
		live, known := byEngine[strings.ToLower(entry.Engine)]
		if !known {
			continue
		}
		if !live[strings.ToLower(entry.Model)] {
			if out != nil {
				fmt.Fprintf(out, "  removing alias %q → %s (%s) — model not found\n", alias, entry.Model, entry.Engine)
			}
			delete(a.Config.ModelAliases, alias)
			changed = true
			removed++
		}
	}

	if changed && a.Workspace != nil {
		if err := SaveModelAliases(a.Workspace, a.Config); err != nil {
			return removed, fmt.Errorf("pruneStaleModelRefs: %w", err)
		}
	}
	return removed, nil
}

// setOf returns a case-insensitive presence set from a string slice.
func setOf(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[strings.ToLower(s)] = true
	}
	return m
}

// ollamaModelTable prints the model capability table.
// When numbered is true each row is prefixed with [N] for interactive selection;
// when false the active model is marked with * instead.
func ollamaModelTable(a *Agent, summaries []OllamaModelSummary, out io.Writer, numbered bool) {
	const nameW = 36
	fmt.Fprintf(out, "%-*s  %7s  %-8s  %6s  %5s  %5s  %6s\n",
		nameW, "NAME", "SIZE", "FAMILY", "CTX", "TOOLS", "EMBED", "TAGGED")
	fmt.Fprintf(out, "%s  %s  %s  %s  %s  %s  %s\n",
		strings.Repeat("─", nameW),
		strings.Repeat("─", 7),
		strings.Repeat("─", 8),
		strings.Repeat("─", 6),
		strings.Repeat("─", 5),
		strings.Repeat("─", 5),
		strings.Repeat("─", 6),
	)

	activeName := ""
	if !numbered {
		if ac, ok := a.Client.(*AnyLLMClient); ok {
			activeName = ac.ModelName()
		}
	}

	unknownCount := 0
	for i, s := range summaries {
		var cap *ModelCapability
		if a.ModelCache != nil {
			cap, _ = a.ModelCache.Get(s.Name)
		}

		tools := CapUnknown
		embed := CapUnknown
		tagged := CapUnknown
		ctx := 0
		if cap != nil {
			tools = cap.SupportsTools
			embed = cap.SupportsEmbed
			tagged = cap.SupportsTaggedBlocks
			ctx = cap.ContextLength
		} else {
			unknownCount++
		}

		var prefix string
		if numbered {
			prefix = fmt.Sprintf("[%2d] ", i+1)
		} else if s.Name == activeName {
			prefix = "* "
		} else {
			prefix = "  "
		}
		displayName := prefix + ollamaTruncateName(s.Name, nameW-len(prefix))
		sizeStr := "—"
		if s.SizeBytes > 0 {
			sizeStr = formatBytes(s.SizeBytes)
		}

		fmt.Fprintf(out, "%-*s  %7s  %-8s  %6s  %5s  %5s  %6s\n",
			nameW, displayName,
			sizeStr,
			ollamaTruncateName(s.Family, 8),
			ollamaFormatCtx(ctx),
			tools.String(),
			embed.String(),
			tagged.String(),
		)
	}

	if unknownCount > 0 {
		fmt.Fprintf(out, "\n  %d model(s) not yet probed — run /ollama probe to fill in capabilities.\n", unknownCount)
	}
}

// ollamaTruncateName truncates s to at most max runes, appending "…" when cut.
func ollamaTruncateName(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// ─── /rename ─────────────────────────────────────────────────────────────────

/** cmdRename renames the active session recording file without ending the
 * session. The new name is placed in the same directory as the current file.
 * A .spmd extension is added automatically when omitted.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with an active Recorder.
 *   args ([]string)  — [0] new filename (path components are stripped).
 *   out  (io.Writer) — destination for command output.
 *
 * Returns:
 *   error — on rename failure.
 *
 * Example:
 *   /rename my-feature-session
 */
func cmdRename(a *Agent, args []string, out io.Writer) error {
	if a.Recorder == nil {
		fmt.Fprintln(out, "No active recording. Start one with /record start.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /rename NAME")
		return nil
	}
	name := filepath.Base(args[0])
	if !strings.HasSuffix(name, ".spmd") && !strings.HasSuffix(name, ".fountain") {
		name += ".spmd"
	}
	newPath := filepath.Join(filepath.Dir(a.Recorder.Path()), name)
	if err := a.Recorder.Rename(newPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	fmt.Fprintf(out, "Session renamed to: %s\n", newPath)
	return nil
}

// ─── /file-tree ──────────────────────────────────────────────────────────────

/** cmdFileTree prints a tree-style listing of the workspace directory,
 * skipping hidden files and directories. An optional path argument restricts
 * the listing to a subdirectory of the workspace root.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with a configured Workspace.
 *   args ([]string)  — optional [0] subdirectory path (relative to workspace root).
 *   out  (io.Writer) — destination for command output.
 *
 * Returns:
 *   error — if the path is outside the workspace.
 *
 * Example:
 *   /file-tree
 *   /file-tree harvey/
 */
func cmdFileTree(a *Agent, args []string, out io.Writer) error {
	root := a.Workspace.Root
	if len(args) > 0 {
		abs, err := resolveWorkspacePath(a.Workspace.Root, args[0])
		if err != nil {
			return fmt.Errorf("file-tree: %w", err)
		}
		root = abs
	}
	rel, _ := filepath.Rel(a.Workspace.Root, root)
	fmt.Fprintf(out, "%s\n", rel)
	printDirTree(root, "", out)
	return nil
}

// printDirTree recursively prints a directory tree using box-drawing characters.
// Hidden entries (names starting with ".") are skipped.
func printDirTree(dir, prefix string, out io.Writer) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var visible []fs.DirEntry
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			visible = append(visible, e)
		}
	}
	for i, e := range visible {
		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(visible)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		fmt.Fprintf(out, "%s%s%s\n", prefix, connector, e.Name())
		if e.IsDir() {
			printDirTree(filepath.Join(dir, e.Name()), childPrefix, out)
		}
	}
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
 * agents/sessions/ by default, with filenames like:
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
		// Remote URI: bypass workspace permissions and read directly.
		if parseURIScheme(rel) != "" {
			rr, err := NewRemoteReader(rel)
			if err != nil {
				fmt.Fprintf(out, "  ✗ %s: %v\n", rel, err)
				continue
			}
			var buf bytes.Buffer
			if err := rr.Get(context.Background(), rel, &buf); err != nil {
				fmt.Fprintf(out, "  ✗ %s: %v\n", rel, err)
				continue
			}
			data := buf.Bytes()
			fmt.Fprintf(out, "  ✓ %s (%d bytes)\n", rel, len(data))
			sb.WriteString("\n```" + rel + "\n")
			sb.Write(data)
			if len(data) > 0 && data[len(data)-1] != '\n' {
				sb.WriteByte('\n')
			}
			sb.WriteString("```\n")
			ok++
			continue
		}

		if !a.CheckReadPermission(rel) {
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileRead, rel, StatusDenied)
			}
			fmt.Fprintf(out, "  ✗ %s: read permission denied\n", rel)
			continue
		}

		// PDF files: extract text via poppler instead of reading raw bytes.
		if strings.ToLower(filepath.Ext(rel)) == ".pdf" {
			absPath, resolveErr := a.Workspace.AbsPath(rel)
			if resolveErr != nil {
				fmt.Fprintf(out, "  ✗ %s: %v\n", rel, resolveErr)
				continue
			}
			result, pdfErr := pdfExtract(absPath, "")
			if pdfErr != nil {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileRead, rel, StatusError)
				}
				fmt.Fprintf(out, "  ✗ %s: %v\n", rel, pdfErr)
				continue
			}
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileRead, rel, StatusSuccess)
			}
			fmt.Fprintf(out, "  ✓ %s (PDF, %d page(s))\n", rel, result.Info.Pages)
			sb.WriteString("\n```" + rel + "\n")
			if result.Info.Title != "" {
				fmt.Fprintf(&sb, "Title: %s\n", result.Info.Title)
			}
			sb.WriteString(result.Text)
			if !strings.HasSuffix(result.Text, "\n") {
				sb.WriteByte('\n')
			}
			sb.WriteString("```\n")
			ok++
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

// ─── /read-dir ───────────────────────────────────────────────────────────────

// defaultMaxReadDirBytes is the total context cap for /read-dir.
const defaultMaxReadDirBytes = 256 * 1024

/** cmdReadDir walks a workspace directory and injects all eligible files into
 * the conversation as a context message. Files are skipped when hidden,
 * binary, inside agents/, matching sensitive patterns, or over 64 KB.
 * The total injected bytes are capped at 256 KB.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with a configured Workspace.
 *   args ([]string)  — optional [PATH] and [--depth N] flags.
 *   out  (io.Writer) — destination for progress output.
 *
 * Returns:
 *   error — path-resolution or OS errors only; skips are reported, not errored.
 *
 * Example:
 *   /read-dir harvey/ --depth 1
 */
func cmdReadDir(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "No workspace initialised.")
		return nil
	}

	dirArg := "."
	maxDepth := 2

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--depth", "-d":
			if i+1 >= len(args) {
				fmt.Fprintln(out, "read-dir: --depth requires a number")
				return nil
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				fmt.Fprintf(out, "read-dir: invalid depth %q\n", args[i])
				return nil
			}
			maxDepth = n
		default:
			if dirArg != "." {
				fmt.Fprintln(out, "Usage: /read-dir [PATH] [--depth N]")
				return nil
			}
			dirArg = args[i]
		}
	}

	absDir, err := a.Workspace.AbsPath(dirArg)
	if err != nil {
		return fmt.Errorf("read-dir: %w", err)
	}

	relDir, _ := filepath.Rel(a.Workspace.Root, absDir)
	if !a.CheckReadPermission(relDir) {
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileRead, relDir, StatusDenied)
		}
		fmt.Fprintf(out, "read-dir: read permission denied for %s\n", relDir)
		return nil
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("read-dir: %w", err)
	}
	if !info.IsDir() {
		fmt.Fprintf(out, "read-dir: %s is not a directory\n", dirArg)
		return nil
	}

	const perFileCap = defaultMaxOutputBytes

	var sb strings.Builder
	sb.WriteString("[context: /read-dir " + dirArg + "]\n")

	var ok, skipped, totalBytes int
	stopped := false

	filepath.WalkDir(absDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || stopped {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			skipped++
			return nil
		}

		if d.IsDir() {
			if p == absDir {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			// Enforce maxDepth (0 = unlimited).
			if maxDepth > 0 {
				rel, _ := filepath.Rel(absDir, p)
				level := strings.Count(rel, string(filepath.Separator)) + 1
				if level >= maxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip hidden files.
		if strings.HasPrefix(d.Name(), ".") {
			skipped++
			return nil
		}

		relPath, _ := filepath.Rel(a.Workspace.Root, p)

		if isAgentsDir(a.Workspace.Root, p) || sensitiveFileDenied(p) {
			skipped++
			return nil
		}

		if !a.CheckReadPermission(relPath) {
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileRead, relPath, StatusDenied)
			}
			skipped++
			return nil
		}

		data, err := os.ReadFile(p)
		if err != nil {
			skipped++
			return nil
		}

		if isBinary(data) {
			skipped++
			return nil
		}

		if len(data) > perFileCap {
			fmt.Fprintf(out, "  ~ %s (%d bytes, exceeds per-file cap — skipped)\n", relPath, len(data))
			skipped++
			return nil
		}

		if totalBytes+len(data) > defaultMaxReadDirBytes {
			stopped = true
			return nil
		}

		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionFileRead, relPath, StatusSuccess)
		}

		sb.WriteString("\n```" + relPath + "\n")
		sb.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			sb.WriteByte('\n')
		}
		sb.WriteString("```\n")

		totalBytes += len(data)
		ok++
		fmt.Fprintf(out, "  ✓ %s (%d bytes)\n", relPath, len(data))
		return nil
	})

	if ok == 0 {
		fmt.Fprintln(out, "  No readable files found.")
		return nil
	}

	a.AddMessage("user", sb.String())
	if stopped {
		fmt.Fprintf(out, "  %d file(s) added (%d bytes). Reached %d KB cap — narrow scope with a subdirectory path or --depth.\n",
			ok, totalBytes, defaultMaxReadDirBytes/1024)
	} else {
		fmt.Fprintf(out, "  %d file(s) added to context (%d bytes)", ok, totalBytes)
		if skipped > 0 {
			fmt.Fprintf(out, ", %d skipped (hidden/binary/sensitive/too large)", skipped)
		}
		fmt.Fprintln(out, ".")
	}
	return nil
}

// ─── /read-pdf ───────────────────────────────────────────────────────────────

// readPDFMaxPages is the maximum number of pages /read-pdf will inject in one
// call. PDFs larger than this require an explicit page range.
const readPDFMaxPages = 20

/** cmdReadPDF extracts text from a PDF file and injects it into the
 * conversation as a user-role context message. It uses pdfExtract internally,
 * which requires the poppler utilities (pdfinfo, pdftotext, pdfimages).
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent.
 *   args ([]string)  — [FILE] and optional [PAGES] (e.g. "40-55").
 *   out  (io.Writer) — destination for progress output.
 *
 * Returns:
 *   error — only for unexpected internal failures; user errors are printed to out.
 *
 * Example:
 *   /read-pdf ~/docs/oberon2.pdf 49-63
 */
func cmdReadPDF(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /read-pdf FILE [PAGES]")
		fmt.Fprintln(out, "  Example: /read-pdf ~/docs/spec.pdf 40-55")
		return nil
	}

	if err := checkPopplerTools(); err != nil {
		fmt.Fprintln(out, err.Error())
		return nil
	}

	filePath := args[0]
	var pages string
	if len(args) > 1 {
		pages = args[1]
	}

	absPath, err := resolvePDFPath(filePath)
	if err != nil {
		fmt.Fprintf(out, "  ✗ %s: %v\n", filePath, err)
		return nil
	}

	// Enforce page cap before the expensive extraction.
	if pages == "" {
		infoOut, err := runTool("pdfinfo", absPath)
		if err != nil {
			fmt.Fprintf(out, "  ✗ cannot read PDF: %v\n", err)
			return nil
		}
		info := parsePDFInfo(infoOut)
		if info.Pages > readPDFMaxPages {
			fmt.Fprintf(out, "  ✗ %s has %d pages; /read-pdf is limited to %d pages per call.\n",
				filePath, info.Pages, readPDFMaxPages)
			fmt.Fprintf(out, "     Specify a range, e.g.: /read-pdf %s 1-%d\n", filePath, readPDFMaxPages)
			return nil
		}
	} else {
		first, last, err := parsePDFPageRange(pages)
		if err != nil {
			fmt.Fprintf(out, "  ✗ %v\n", err)
			return nil
		}
		if last-first+1 > readPDFMaxPages {
			fmt.Fprintf(out, "  ✗ page range %s spans %d pages; limit is %d.\n",
				pages, last-first+1, readPDFMaxPages)
			fmt.Fprintf(out, "     Narrow the range, e.g.: %d-%d\n", first, first+readPDFMaxPages-1)
			return nil
		}
	}

	fmt.Fprintf(out, "  Extracting %s", filePath)
	if pages != "" {
		fmt.Fprintf(out, " pages %s", pages)
	}
	fmt.Fprintln(out, " …")

	result, err := pdfExtract(absPath, pages)
	if err != nil {
		fmt.Fprintf(out, "  ✗ %v\n", err)
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[context: /read-pdf %s", filePath)
	if pages != "" {
		fmt.Fprintf(&sb, " pages %s", pages)
	}
	fmt.Fprintln(&sb, "]")
	fmt.Fprintln(&sb)

	if result.Info.Title != "" {
		fmt.Fprintf(&sb, "Title:  %s\n", result.Info.Title)
	}
	if result.Info.Author != "" {
		fmt.Fprintf(&sb, "Author: %s\n", result.Info.Author)
	}
	if result.Info.Pages > 0 {
		fmt.Fprintf(&sb, "Pages:  %d\n", result.Info.Pages)
	}
	if result.Info.CreatedAt != "" {
		fmt.Fprintf(&sb, "Date:   %s\n", result.Info.CreatedAt)
	}
	if len(result.DiagramPages) > 0 {
		pageNums := make([]string, len(result.DiagramPages))
		for i, p := range result.DiagramPages {
			pageNums[i] = strconv.Itoa(p)
		}
		fmt.Fprintf(&sb, "\nNote: page(s) %s appear to contain only vector diagrams — content may be incomplete. Use a vision-capable model to process those pages.\n",
			strings.Join(pageNums, ", "))
	}
	fmt.Fprintln(&sb)
	sb.WriteString(result.Text)

	a.AddMessage("user", sb.String())

	// Count non-empty injected pages for the confirmation message.
	injected := 0
	for _, pt := range strings.Split(result.Text, "\f") {
		if strings.TrimSpace(pt) != "" {
			injected++
		}
	}
	fmt.Fprintf(out, "  ✓ %d page(s) added to context", injected)
	if len(result.DiagramPages) > 0 {
		fmt.Fprintf(out, " (%d diagram-only page(s) flagged)", len(result.DiagramPages))
	}
	fmt.Fprintln(out)
	return nil
}

// resolvePDFPath expands ~ and converts a relative path to absolute.
// Unlike Workspace.AbsPath it does not enforce workspace boundaries, because
// /read-pdf is designed to accept arbitrary file system paths.
func resolvePDFPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	return path, nil
}

// ─── /attach ─────────────────────────────────────────────────────────────────

// attachMaxImageBytes is the file-size ceiling for native image attachment.
const attachMaxImageBytes = 5 * 1024 * 1024 // 5 MB

// attachMaxTextBytes is the ceiling for plain-text file injection.
const attachMaxTextBytes = 256 * 1024 // 256 KB

/** cmdAttach attaches a file to the conversation as the most useful form the
 * current route can accept. Images are sent as base64 data-URL ContentParts
 * when the active model supports vision; otherwise a text description is
 * injected. PDFs are extracted via pdfExtract (same page cap as /read-pdf).
 * All other files are injected as plain text when ≤ 256 KB.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent.
 *   args ([]string)  — [FILE].
 *   out  (io.Writer) — destination for progress output.
 *
 * Returns:
 *   error — only for unexpected internal failures; user errors are printed to out.
 *
 * Example:
 *   /attach ~/photos/diagram.png
 *   /attach ~/docs/spec.pdf
 */
func cmdAttach(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /attach FILE")
		fmt.Fprintln(out, "  Images: attached natively if the route supports vision, text description otherwise.")
		fmt.Fprintln(out, "  PDFs:   text extracted via pdfExtract (20-page cap; requires poppler).")
		fmt.Fprintln(out, "  Other:  injected as plain text (≤ 256 KB).")
		return nil
	}

	filePath := args[0]

	// Remote URI: download content and route through the same MIME detection.
	if parseURIScheme(filePath) != "" {
		return cmdAttachRemote(a, filePath, out)
	}

	absPath, err := resolvePDFPath(filePath)
	if err != nil {
		fmt.Fprintf(out, "  ✗ %s: %v\n", filePath, err)
		return nil
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		fmt.Fprintf(out, "  ✗ %s: %v\n", filePath, err)
		return nil
	}
	if fi.IsDir() {
		fmt.Fprintf(out, "  ✗ %s is a directory; use /read-dir for directories\n", filePath)
		return nil
	}

	// PDFs are routed before reading the full file to avoid loading 100 MB
	// into memory only to hand it off to pdftotext.
	if strings.ToLower(filepath.Ext(absPath)) == ".pdf" {
		return cmdReadPDF(a, []string{absPath}, out)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		fmt.Fprintf(out, "  ✗ %s: %v\n", filePath, err)
		return nil
	}

	mime := attachDetectMIME(absPath, data)
	base := filepath.Base(absPath)

	if attachIsImageMIME(mime) {
		return attachImage(a, filePath, base, data, mime, out)
	}
	return attachText(a, filePath, base, data, mime, out)
}

// cmdAttachRemote downloads a remote URI and attaches it using the same MIME
// routing as cmdAttach for local files. PDFs are written to a temp file so
// that the existing pdftotext pipeline can operate on them. The URI is shown
// in output; credentials are never revealed.
func cmdAttachRemote(a *Agent, uri string, out io.Writer) error {
	rr, err := NewRemoteReader(uri)
	if err != nil {
		fmt.Fprintf(out, "  ✗ %s: %v\n", uri, err)
		return nil
	}
	var buf bytes.Buffer
	if err := rr.Get(context.Background(), uri, &buf); err != nil {
		fmt.Fprintf(out, "  ✗ %s: %v\n", uri, err)
		return nil
	}
	data := buf.Bytes()
	base := filepath.Base(uri)

	// PDFs: write to temp file and route through the existing pdftotext pipeline.
	if strings.ToLower(filepath.Ext(base)) == ".pdf" {
		f, err := os.CreateTemp("", "harvey-remote-*.pdf")
		if err != nil {
			fmt.Fprintf(out, "  ✗ %s: create temp: %v\n", uri, err)
			return nil
		}
		tmpPath := f.Name()
		defer os.Remove(tmpPath)
		if _, werr := f.Write(data); werr != nil {
			f.Close()
			fmt.Fprintf(out, "  ✗ %s: write temp: %v\n", uri, werr)
			return nil
		}
		f.Close()
		return cmdReadPDF(a, []string{tmpPath}, out)
	}

	mime := attachDetectMIME(base, data)
	if attachIsImageMIME(mime) {
		return attachImage(a, uri, base, data, mime, out)
	}
	return attachText(a, uri, base, data, mime, out)
}

// attachImage attaches an image file to the conversation. When the active
// route reports vision capability the image is encoded as a base64 data-URL
// ContentPart; otherwise a text description is injected so the turn still
// carries the attachment metadata.
func attachImage(a *Agent, filePath, base string, data []byte, mime string, out io.Writer) error {
	if len(data) > attachMaxImageBytes {
		fmt.Fprintf(out, "  ✗ %s: image too large (%s); maximum is 5 MB\n",
			filePath, formatBytes(int64(len(data))))
		return nil
	}

	if attachClientSupportsVision(a) {
		encoded := base64.StdEncoding.EncodeToString(data)
		parts := []anyllm.ContentPart{
			{Type: "text", Text: fmt.Sprintf("[attached: %s]", base)},
			{Type: "image_url", ImageURL: &anyllm.ImageURL{
				URL: "data:" + mime + ";base64," + encoded,
			}},
		}
		a.AddMessageParts("user", parts)
		fmt.Fprintf(out, "  ✓ %s attached natively (%s, %s)\n",
			base, mime, formatBytes(int64(len(data))))
	} else {
		text := fmt.Sprintf(
			"[attached: %s — %s, %s — vision not available on current route; switch to a vision-capable route to process this image natively]",
			base, mime, formatBytes(int64(len(data))))
		a.AddMessage("user", text)
		fmt.Fprintf(out, "  ✓ %s attached as text description (route has no vision capability)\n", base)
		fmt.Fprintln(out, "     Tip: use @name to route the next turn to a vision-capable endpoint.")
	}
	return nil
}

// attachText injects a plain-text (or unknown-format) file into the
// conversation. Binary files are rejected with an explanation.
func attachText(a *Agent, filePath, base string, data []byte, mime string, out io.Writer) error {
	if len(data) > attachMaxTextBytes {
		fmt.Fprintf(out, "  ✗ %s: file too large (%s) for text injection; maximum is 256 KB\n",
			filePath, formatBytes(int64(len(data))))
		fmt.Fprintln(out, "     Use /rag ingest to index large files for retrieval instead.")
		return nil
	}
	// Reject binary content (null byte in sample is the classic heuristic).
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	for _, b := range sample {
		if b == 0 {
			fmt.Fprintf(out, "  ✗ %s appears to be binary (%s); /attach supports images, PDFs, and text files\n",
				filePath, mime)
			return nil
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[attached: %s]\n\n", base)
	sb.Write(data)
	a.AddMessage("user", sb.String())
	fmt.Fprintf(out, "  ✓ %s attached as text (%s)\n", base, formatBytes(int64(len(data))))
	return nil
}

// attachDetectMIME returns the MIME type of the file. The extension is
// checked first for formats (e.g. WebP) that the sniff algorithm misidentifies;
// then http.DetectContentType is applied to the first 512 bytes.
func attachDetectMIME(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webp":
		return "image/webp"
	}
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	return http.DetectContentType(sample)
}

// attachIsImageMIME reports whether mime is an image type that can be
// attached natively or described textually.
func attachIsImageMIME(mime string) bool {
	switch strings.SplitN(mime, ";", 2)[0] {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

// attachClientSupportsVision reports whether the current LLM client declares
// image completion capability.
func attachClientSupportsVision(a *Agent) bool {
	ac, ok := a.Client.(*AnyLLMClient)
	if !ok {
		return false
	}
	return ac.ProviderCapabilities().CompletionImage
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

// suggestPathFromHistory scans the last user message in history for a single
// token that looks like a file path (via looksLikePath). Returns that token
// when exactly one candidate is found, or "" when there are zero or more than
// one (ambiguous). Punctuation and backtick quotes are stripped before testing.
func suggestPathFromHistory(history []Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		var candidates []string
		for _, tok := range strings.Fields(history[i].Content) {
			tok = strings.Trim(tok, ".,;:!?\"'`()")
			if looksLikePath(tok) {
				candidates = append(candidates, tok)
			}
		}
		if len(candidates) == 1 {
			return candidates[0]
		}
		return ""
	}
	return ""
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
	if a.Config.Security.SafeMode && !a.Config.IsCommandAllowed(args[0]) {
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionCommand, strings.Join(args, " "), StatusDenied)
		}
		fmt.Fprintf(out, yellow("  Command %q is not allowed in safe mode.\n"), args[0])
		fmt.Fprintf(out, "  Allowed commands: %s\n", strings.Join(a.Config.Security.AllowedCommands, ", "))
		fmt.Fprintln(out, "  Use /safemode off to disable, or /safemode allow CMD to add it.")
		return nil
	}

	// Log allowed command execution
	if a.AuditBuffer != nil {
		a.AuditBuffer.Log(ActionCommand, strings.Join(args, " "), StatusAllowed)
	}

	cmdLine := strings.Join(args, " ")
	fmt.Fprintf(out, "  $ %s\n", cmdLine)

	// Validate command line to prevent shell metacharacter injection
	program, cmdArgs, err := parseCommandLine(cmdLine)
	if err != nil {
		fmt.Fprintf(out, yellow("  Invalid command: %v\n"), err)
		if a.AuditBuffer != nil {
			a.AuditBuffer.Log(ActionCommand, cmdLine, StatusDenied)
		}
		return nil
	}

	// Use a context with an optional timeout to prevent hanging commands.
	var ctx context.Context
	var cancel context.CancelFunc
	if a.Config.Security.RunTimeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), a.Config.Security.RunTimeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, program, cmdArgs...)
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
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if path == filepath.Join(a.Workspace.Root, "agents") {
				return filepath.SkipDir
			}
			return nil
		}
		if isAgentsDir(a.Workspace.Root, path) {
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
// Extension recognition is delegated to the language registry so that adding
// new languages automatically extends this function.
func looksLikePath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	return registryHasExt(s)
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
 *   /session continue agents/sessions/harvey-session-20260430.spmd
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
		fmt.Fprintln(out, "Usage: /session <list|show [FILE]|use FILE|continue FILE|replay FILE [OUTPUT]>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "list":
		sessDir := a.SessionsDir
		if sessDir == "" {
			fmt.Fprintln(out, "  No sessions directory configured.")
			return nil
		}
		files, err := ListSessionFiles(sessDir)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			fmt.Fprintln(out, "  No sessions found in "+sessDir)
			return nil
		}
		fmt.Fprintf(out, "  Sessions in %s:\n", sessDir)
		for _, f := range files {
			fmt.Fprintf(out, "    %-40s  %s\n", f.Name, f.ModTime.Format("2006-01-02 15:04"))
		}
	case "show":
		path := ""
		if len(args) >= 2 {
			path = args[1]
		} else if a.Recorder != nil {
			path = a.Recorder.Path()
		} else {
			fmt.Fprintln(out, "  Usage: /session show FILE")
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(out, "  ✗ %v\n", err)
			return nil
		}
		_, model, turns, err := parseFountainSession(path)
		if err != nil {
			fmt.Fprintf(out, "  ✗ Could not parse session: %v\n", err)
			return nil
		}
		fmt.Fprintf(out, "  File:    %s\n", path)
		fmt.Fprintf(out, "  Date:    %s\n", info.ModTime().Format("2006-01-02 15:04"))
		fmt.Fprintf(out, "  Model:   %s\n", model)
		fmt.Fprintf(out, "  Turns:   %d\n", len(turns))
		if len(turns) > 0 {
			preview := turns[0].UserInput
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			fmt.Fprintf(out, "  First:   %s\n", preview)
		}
	case "use", "continue":
		if len(args) < 2 {
			if a.SessionsDir == "" {
				fmt.Fprintln(out, "  No sessions directory configured.")
				return nil
			}
			files, err := ListSessionFiles(a.SessionsDir)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				fmt.Fprintln(out, "  No sessions found. Start a conversation to create one.")
				return nil
			}
			items := make([]SelectItem, len(files))
			for i, f := range files {
				items[i] = SelectItem{
					Value: f.Path,
					Label: fmt.Sprintf("%-40s  %s", f.Name, f.ModTime.Format("2006-01-02 15:04")),
				}
			}
			chosen, sErr := SelectFrom(items, fmt.Sprintf("Select session [1-%d] or Enter to cancel: ", len(items)), a.In, out)
			if sErr != nil || chosen == "" {
				return sErr
			}
			args = append(args, chosen)
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
			fmt.Fprintln(out, "  No backend connected. Use /llamafile start or /ollama start.")
			return nil
		}
		return a.ReplayFromFountain(context.Background(), src, outPath, out)
	default:
		fmt.Fprintf(out, "Unknown session subcommand: %s\n", args[0])
		fmt.Fprintln(out, "Usage: /session <list|show [FILE]|use FILE|continue FILE|replay FILE [OUTPUT]>")
	}
	return nil
}


// cmdModelAlias manages the model_aliases map in harvey.yaml.
// Subcommands: list, set ALIAS FULLNAME [--tags T,T], tags ALIAS TAG..., remove ALIAS
func cmdModelAlias(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "list" {
		if len(a.Config.ModelAliases) == 0 {
			fmt.Fprintln(out, "  No model aliases defined.")
			fmt.Fprintln(out, "  Use: /model alias set ALIAS FULL_MODEL_NAME [--tags T,T]")
			return nil
		}
		// Collect and sort for deterministic output.
		keys := make([]string, 0, len(a.Config.ModelAliases))
		for k := range a.Config.ModelAliases {
			keys = append(keys, k)
		}
		sortStrings(keys)
		fmt.Fprintf(out, "  %-20s  %-36s  %s\n", "Alias", "Model", "Tags")
		fmt.Fprintln(out, "  "+strings.Repeat("-", 70))
		for _, k := range keys {
			entry := a.Config.ModelAliases[k]
			tags := strings.Join(entry.Tags, ", ")
			fmt.Fprintf(out, "  %-20s  %-36s  %s\n", k, entry.Model, tags)
		}
		return nil
	}

	switch args[0] {
	case "set", "add":
		if len(args) < 3 {
			fmt.Fprintln(out, "Usage: /model alias set ALIAS FULL_MODEL_NAME [--tags tag1,tag2]")
			return nil
		}
		alias := strings.ToLower(args[1])
		full := args[2]
		var tags []string
		for i := 3; i < len(args)-1; i++ {
			if args[i] == "--tags" {
				for _, t := range strings.Split(args[i+1], ",") {
					if t = strings.TrimSpace(t); t != "" {
						tags = append(tags, strings.ToLower(t))
					}
				}
			}
		}
		if a.Config.ModelAliases == nil {
			a.Config.ModelAliases = make(map[string]ModelAlias)
		}
		// Reject if the alias name clashes with an installed model name.
		if aliasClashesWithModel(a, alias) {
			fmt.Fprintf(out, "  ✗ %q is already an installed model name — choose a different alias.\n", alias)
			return nil
		}
		// Warn if updating an existing alias.
		if existing, ok := a.Config.ModelAliases[alias]; ok && existing.Model != full {
			fmt.Fprintf(out, "  ⚠ Updating alias %q: %s → %s\n", alias, existing.Model, full)
		}
		a.Config.ModelAliases[alias] = ModelAlias{Model: full, Tags: tags}
		if err := SaveModelAliases(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  ✗ Failed to save: %v\n", err)
			return nil
		}
		tagStr := ""
		if len(tags) > 0 {
			tagStr = " [" + strings.Join(tags, ", ") + "]"
		}
		fmt.Fprintf(out, "  Alias set: %s → %s%s\n", alias, full, tagStr)

	case "tags":
		// /model alias tags ALIAS TAG [TAG...]
		if len(args) < 3 {
			fmt.Fprintln(out, "Usage: /model alias tags ALIAS TAG [TAG...]")
			return nil
		}
		alias := strings.ToLower(args[1])
		entry, ok := a.Config.ModelAliases[alias]
		if !ok {
			fmt.Fprintf(out, "  Alias %q not found.\n", alias)
			return nil
		}
		for _, t := range args[2:] {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			found := false
			for _, existing := range entry.Tags {
				if existing == t {
					found = true
					break
				}
			}
			if !found {
				entry.Tags = append(entry.Tags, t)
			}
		}
		a.Config.ModelAliases[alias] = entry
		if err := SaveModelAliases(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  ✗ Failed to save: %v\n", err)
			return nil
		}
		fmt.Fprintf(out, "  Tags for %q: [%s]\n", alias, strings.Join(entry.Tags, ", "))

	case "remove", "rm", "delete":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /model alias remove ALIAS")
			return nil
		}
		alias := strings.ToLower(args[1])
		if _, ok := a.Config.ModelAliases[alias]; !ok {
			fmt.Fprintf(out, "  Alias %q not found.\n", alias)
			return nil
		}
		delete(a.Config.ModelAliases, alias)
		if err := SaveModelAliases(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, "  ✗ Failed to save: %v\n", err)
			return nil
		}
		fmt.Fprintf(out, "  Alias %q removed.\n", alias)

	default:
		fmt.Fprintf(out, "  Unknown subcommand %q. Use: list, set, tags, remove\n", args[0])
	}
	return nil
}

// aliasClashesWithModel reports whether name matches an installed Ollama model.
// It checks the model cache first (no network call); if the cache is empty it
// falls back to a live Ollama query. Returns false when neither source is
// available so that a downed Ollama server never blocks alias creation.
func aliasClashesWithModel(a *Agent, name string) bool {
	// Check model cache first.
	if a.ModelCache != nil {
		if caps, err := a.ModelCache.All(); err == nil && len(caps) > 0 {
			for _, cap := range caps {
				if strings.EqualFold(cap.Name, name) {
					return true
				}
			}
			return false
		}
	}
	// Fall back to live Ollama list.
	if !ProbeOllama(a.Config.Ollama.URL) {
		return false
	}
	models, err := newOllamaLLMClient(a.Config.Ollama.URL, "", a.Config.Ollama.Timeout).
		Models(context.Background())
	if err != nil {
		return false
	}
	for _, m := range models {
		if strings.EqualFold(m, name) {
			return true
		}
	}
	return false
}

// ─── auto-execute ─────────────────────────────────────────────────────────────

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
func (a *Agent) autoExecuteReply(reply string, out io.Writer, reader *bufio.Reader, _ context.Context) {
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

	// 2. Fallback for models that ignore the tagged-fence convention: if no
	// tagged blocks were found but the reply contains a plain fenced code block,
	// offer the user an interactive write prompt.
	if len(blocks) == 0 {
		content, ok := extractCodeBlock(reply)
		if ok {
			var dest string
			if suggested := suggestPathFromHistory(a.History); suggested != "" {
				// Path inferred from conversation — show the promptAction box
				// (same UX as tagged blocks: Enter = yes, n = skip).
				choice := promptAction(reader, out, "Write: "+suggested, content)
				if choice != actionNo && choice != actionQuit {
					dest = suggested
				}
			} else {
				// No path known — ask the user to supply one.
				fmt.Fprint(out, "  Untagged code block found. Write to file? (enter path, or press Enter to skip)\n  Path: ")
				line, _ := reader.ReadString('\n')
				dest = strings.TrimSpace(line)
			}
			if dest != "" {
				if !a.CheckWritePermission(dest) {
					if a.AuditBuffer != nil {
						a.AuditBuffer.Log(ActionFileWrite, dest, StatusDenied)
					}
					fmt.Fprintf(out, "  write permission denied for %s\n", dest)
				} else if err := a.Workspace.WriteFile(dest, []byte(content), 0o644); err != nil {
					if a.AuditBuffer != nil {
						a.AuditBuffer.Log(ActionFileWrite, dest, StatusError)
					}
					fmt.Fprintf(out, "  ✗ %s: %v\n", dest, err)
					a.logAction("write", dest, actionYes, "error: "+err.Error())
				} else {
					if a.AuditBuffer != nil {
						a.AuditBuffer.Log(ActionFileWrite, dest, StatusSuccess)
					}
					fmt.Fprintf(out, "  ✓ wrote %s (%d bytes)\n", dest, len(content))
					a.logAction("write", dest, actionYes, "ok")
				}
			}
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


// ─── /format ─────────────────────────────────────────────────────────────────

// cmdFormat formats one or more workspace files in-place using the registered
// formatter for each file's language.  File-mode (external, in-place) formatters
// are skipped when safe mode is enabled.
func cmdFormat(a *Agent, args []string, out io.Writer) error {
	if a.Workspace == nil {
		fmt.Fprintln(out, "  /format requires a workspace.")
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /format FILE [FILE...]")
		return nil
	}
	for _, relPath := range args {
		data, err := a.Workspace.ReadFile(relPath)
		if err != nil {
			fmt.Fprintf(out, "  %s: read error: %v\n", relPath, err)
			continue
		}
		ext := filepath.Ext(relPath)
		langID, ok := globalRegistry.DetectFromExtension(ext)
		if !ok {
			fmt.Fprintf(out, "  %s: no language registered for extension %q\n", relPath, ext)
			continue
		}
		f := globalRegistry.GetFormatter(langID)
		if f == nil {
			fmt.Fprintf(out, "  %s: no formatter registered for %q\n", relPath, langID)
			continue
		}
		if f.Mode() == FileFormatter && a.Config.Security.SafeMode {
			fmt.Fprintf(out, "  %s: file-mode formatter requires safe mode off (/safemode off)\n", relPath)
			continue
		}
		absPath, err := a.Workspace.AbsPath(relPath)
		if err != nil {
			fmt.Fprintf(out, "  %s: path error: %v\n", relPath, err)
			continue
		}
		original := string(data)
		formatted, err := f.Format(original, absPath)
		if err != nil {
			fmt.Fprintf(out, "  %s: formatter error: %v\n", relPath, err)
			continue
		}
		if formatted == original {
			fmt.Fprintf(out, "  %s: already formatted\n", relPath)
			continue
		}
		if err := os.WriteFile(absPath, []byte(formatted), 0o644); err != nil {
			fmt.Fprintf(out, "  %s: write error: %v\n", relPath, err)
			continue
		}
		fmt.Fprintf(out, "  %s: formatted (%d → %d bytes)\n", relPath, len(original), len(formatted))
	}
	return nil
}

// ─── /workspace ──────────────────────────────────────────────────────────────

/** cmdWorkspace handles the /workspace command.
 *
 * Subcommands:
 *   init [FROM_PATH] — initialise this workspace, optionally copying model
 *                      aliases from a source workspace directory or YAML file.
 *   status           — show workspace root and alias count.
 *
 * Parameters:
 *   a    (*Agent)    — running Harvey agent.
 *   args ([]string) — subcommand and optional arguments.
 *   out  (io.Writer) — output sink.
 *
 * Returns:
 *   error — on I/O failure.
 *
 * Example:
 *   /workspace init /other/project
 *   /workspace init ~/shared-aliases.yaml
 *   /workspace status
 */
func cmdWorkspace(a *Agent, args []string, out io.Writer) error {
	sub := ""
	if len(args) > 1 {
		sub = args[1]
	}
	switch sub {
	case "init":
		if a.Workspace == nil {
			return fmt.Errorf("no workspace is open")
		}
		fromPath := ""
		if len(args) > 2 {
			fromPath = args[2]
		}
		if fromPath == "" {
			fmt.Fprintf(out, "  Workspace: %s\n", a.Workspace.Root)
			fmt.Fprintf(out, "  Aliases:   %d defined\n", len(a.Config.ModelAliases))
			fmt.Fprintln(out, "  Tip: run /workspace init <path> to import aliases from another workspace.")
			return nil
		}
		copied, skipped, err := ImportAliasesFrom(fromPath, a.Workspace, a.Config, out)
		if err != nil {
			return err
		}
		if copied == 0 && skipped == 0 {
			// message already printed by ImportAliasesFrom
			return nil
		}
		_ = skipped // already reported in ImportAliasesFrom output
		return nil
	case "", "status":
		if a.Workspace == nil {
			fmt.Fprintln(out, "  No workspace open.")
			return nil
		}
		fmt.Fprintf(out, "  Root:    %s\n", a.Workspace.Root)
		fmt.Fprintf(out, "  Aliases: %d defined\n", len(a.Config.ModelAliases))
		// Profile + injection status
		if a.Config.Memory.Enabled && a.Memory != nil && a.Memory.Store != nil {
			profiles, err := a.Memory.Store.List(string(MemoryTypeWorkspaceProfile))
			if err == nil && len(profiles) > 0 {
				p := profiles[0]
				inject := "injection off — enable with: memory.inject_on_start: true in harvey.yaml"
				if a.Config.Memory.InjectOnStart {
					inject = "injection on"
				}
				fmt.Fprintf(out, "  Profile: %q (%s)\n", p.Description, inject)
			} else {
				fmt.Fprintln(out, "  Profile: none  (create one with /memory profile new)")
			}
		}
		return nil
	default:
		fmt.Fprintf(out, "  Unknown subcommand %q. Usage: /workspace <init [FROM_PATH]|status>\n", sub)
		return nil
	}
}
