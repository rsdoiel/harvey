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
// taggedBlock. The path token may be preceded by a language hint
// (e.g. "```go harvey/spinner.go").
func findTaggedBlocks(text string) []taggedBlock {
	var blocks []taggedBlock
	lines := strings.Split(text, "\n")
	var cur *taggedBlock
	for _, line := range lines {
		if cur == nil {
			if strings.HasPrefix(line, "```") {
				fence := strings.TrimSpace(strings.TrimPrefix(line, "```"))
				path := ""
				for _, tok := range strings.Fields(fence) {
					if looksLikePath(tok) {
						path = tok
						break
					}
				}
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
