package harvey

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rsdoiel/termlib"
)

// ANSI escape codes for terminal styling.
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiCyan    = "\033[36m"
	ansiRed     = "\033[31m"
	ansiMagenta = "\033[35m"
	ansiBlue    = "\033[34m"
	sep         = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
)

// sensitiveEnvPrefixes contains environment variable name prefixes that
// should be EXCLUDED to prevent accidental exposure of sensitive data (e.g., API keys).
// Variables matching these prefixes will NOT be passed to child processes.
var sensitiveEnvPrefixes = []string{
	"ANTHROPIC_API_KEY",
	"DEEPSEEK_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"OPENAI_API_KEY",
}

// safeEnvPrefixes contains environment variable name prefixes that are
// SAFE to pass to child processes. All other variables are filtered out.
var safeEnvPrefixes = []string{
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


// isConnectionError returns true when err indicates a transport-level failure
// consistent with the llamafile server having stopped: connection refused,
// EOF, or reset by peer.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no route to host")
}

// restartActiveLlamafile stops any Harvey-managed llamafile process and
// starts a fresh one for the currently active entry. Returns an error when
// there is no active entry, or when the active entry has an empty path
// (i.e. an adopted external server that Harvey cannot restart).
func restartActiveLlamafile(a *Agent, out io.Writer) error {
	entry := a.Config.ActiveLlamafileEntry()
	if entry == nil {
		return fmt.Errorf("no active llamafile entry to restart")
	}
	if entry.Path == "" {
		return fmt.Errorf("cannot restart %q: server was adopted (path unknown)", entry.Name)
	}
	if a.Backend != nil {
		_ = a.Backend.Stop()
	}
	absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
	fmt.Fprintf(out, "  Starting %s...\n", entry.Name)
	proc, err := StartLlamafileService(absPath, a.Config.Llamafile.URL, "", a.Config.Llamafile.StartupTimeout, a.Config.Llamafile.GPULayers, a.Config.ActiveLlamafileContextLength(), out)
	if err != nil {
		return fmt.Errorf("restart failed: %w", err)
	}
	a.wireLlamafileBackend(proc, entry.Name)
	fmt.Fprintln(out, green("  ✓")+" Restarted "+entry.Name)
	return a.useLlamafileEntry(entry.Name, out)
}

/** runFirstRunWizard prints onboarding text for new users who have no backend
 * configured, then reads one line of input. An empty line returns an error
 * (no backend available). A non-empty line is treated as a llamafile path and
 * passed to cmdLlamafileAdd to register and connect the model.
 *
 * Parameters:
 *   a   (*Agent)   — the running Harvey agent.
 *   in  (io.Reader) — source for user input (typically a.In or a bufio.Reader).
 *   out (io.Writer) — destination for the wizard text and prompts.
 *
 * Returns:
 *   error — "no backend available" when the user provides no path; any error
 *           from cmdLlamafileAdd when the given path is invalid.
 *
 * Example:
 *   if err := runFirstRunWizard(a, reader, out); err != nil {
 *       // user pressed Enter or path was invalid
 *   }
 */
func runFirstRunWizard(a *Agent, in io.Reader, out io.Writer) error {
	// If ~/Models already has .llamafile files, show a picker instead of
	// asking for a raw path — this is the common case for users who
	// downloaded a llamafile but haven't registered it yet.
	if paths := scanLlamafileModels(a.Config.Llamafile.ModelsDir); len(paths) > 0 {
		selected, err := llamafilePickFromDir(a, out)
		if err != nil || selected == "" {
			return fmt.Errorf("no backend available")
		}
		return cmdLlamafileAdd(a, []string{selected}, out)
	}
	fmt.Fprint(out, FirstRunWizardText)
	fmt.Fprint(out, "Enter a llamafile path (or press Enter to exit): ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	path := strings.TrimSpace(line)
	if path == "" {
		return fmt.Errorf("no backend available")
	}
	return cmdLlamafileAdd(a, []string{path}, out)
}

/** attemptModelSwitch tries to switch the active model to the named backend
 * entry. It checks LlamafileModels first, then Ollama models. Returns
 * (true, err) when the name is found regardless of whether the switch
 * succeeded; returns (false, nil) when the name is not registered anywhere
 * so the caller can fall through to treating the input as a normal prompt.
 *
 * Parameters:
 *   a    (*Agent)    — the running Harvey agent.
 *   name (string)   — model name to switch to (without the "@" prefix).
 *   out  (io.Writer) — destination for status messages.
 *
 * Returns:
 *   switched (bool)  — true when a matching model entry was found.
 *   error           — non-nil when the switch was attempted but failed.
 *
 * Example:
 *   if switched, err := attemptModelSwitch(a, "phi-mini", out); switched { ... }
 */
func attemptModelSwitch(a *Agent, name string, out io.Writer) (bool, error) {
	// Check llamafile registry first (case-insensitive).
	for _, e := range a.Config.Llamafile.Models {
		if strings.EqualFold(e.Name, name) {
			err := cmdLlamafileUse(a, []string{e.Name}, out)
			return true, err
		}
	}
	// Check model aliases (stored with lowercase keys).
	if entry, ok := a.Config.ModelAliases[strings.ToLower(name)]; ok {
		full := entry.Model
		err := cmdLlamafileUse(a, []string{full}, out)
		if err != nil {
			// Not a llamafile alias — fall through to Ollama.
			a.Config.Ollama.Model = full
			a.Client = newOllamaLLMClient(a.Config.Ollama.URL, full, a.Config.Ollama.Timeout)
			fmt.Fprintf(out, "  Using model: %s\n", cyan(full))
			if a.Recorder != nil {
				_ = a.Recorder.RecordModelSwitch(full, "ollama")
			}
		}
		return true, nil
	}
	// Name not found in any registry.
	return false, nil
}

/** activeModelLabel returns a short human-readable label for the currently
 * configured model backend, e.g. "qwen-coding (llamafile)" or "llama3.2:3b (ollama)".
 * Returns "none" when no backend is configured. Llamafile takes priority when
 * both LlamafileActive and OllamaModel are set.
 *
 * Parameters:
 *   a (*Agent) — the running Harvey agent.
 *
 * Returns:
 *   string — label of the form "name (backend)" or "none".
 *
 * Example:
 *   fmt.Fprintf(out, "Connecting to %s…\n", activeModelLabel(a))
 */
func activeModelLabel(a *Agent) string {
	if a.Config.Llamafile.Active != "" {
		return a.Config.Llamafile.Active + " (llamafile)"
	}
	if a.Config.Ollama.Model != "" {
		return a.Config.Ollama.Model + " (ollama)"
	}
	return "none"
}


/** parseCommandLine splits a command line string into a program name and
 * arguments, handling quoted strings. This provides basic shell-like parsing
 * without supporting shell metacharacters (|, >, <, &, ;, etc.) for security.
 *
 * Supported features:
 *   - Single and double quotes
 *   - Escaped quotes with backslash (\' and \")
 *   - Basic whitespace splitting
 *
 * Not supported (intentionally for security):
 *   - Pipes (|)
 *   - Redirects (>, <, >>, 2>, etc.)
 *   - Command substitution ($(), backticks)
 *   - Globbing (*)
 *   - Semicolons (;)
 *   - Background processes (&)
 *
 * Parameters:
 *   line (string) — the command line to parse.
 *
 * Returns:
 *   program (string) — the command/program name.
 *   args ([]string)   — the arguments (not including the program).
 *   error            — if parsing fails (e.g., unclosed quotes).
 *
 * Example:
 *   prog, args, _ := parseCommandLine("grep -r 'hello world' .")
 *   // prog = "grep", args = ["-r", "hello world", "."]
 */
func parseCommandLine(line string) (program string, args []string, err error) {
	var tokens []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	escapeNext := false

	for _, ch := range line {
		if escapeNext {
			current.WriteRune(ch)
			escapeNext = false
			continue
		}

		switch ch {
		case '\\':
			escapeNext = true
		case '\'':
			if inDoubleQuote {
				current.WriteRune(ch)
			} else {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if inSingleQuote {
				current.WriteRune(ch)
			} else {
				inDoubleQuote = !inDoubleQuote
			}
		case ' ', '\t':
			if !inSingleQuote && !inDoubleQuote {
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Handle the last token
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	// Check for unclosed quotes
	if inSingleQuote || inDoubleQuote {
		return "", nil, fmt.Errorf("unclosed quote in command line")
	}

	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}

	return tokens[0], tokens[1:], nil
}

func bold(s string) string    { return ansiBold + s + ansiReset }
func dim(s string) string     { return ansiDim + s + ansiReset }
func dimGreen(s string) string { return "\033[2;32m" + s + ansiReset }
func green(s string) string   { return ansiGreen + s + ansiReset }
func yellow(s string) string  { return ansiYellow + s + ansiReset }
func red(s string) string     { return ansiRed + s + ansiReset }
func cyan(s string) string    { return ansiCyan + s + ansiReset }
func magenta(s string) string { return ansiMagenta + s + ansiReset }
func blue(s string) string    { return ansiBlue + s + ansiReset }

// prompt returns the input prompt string reflecting the current backend state.
func (a *Agent) prompt() string {
	prefix := "harvey"
	if a.Client == nil {
		prefix = "harvey (no backend)"
	}
	if !a.Config.Security.SafeMode {
		return prefix + " " + red("[unsafe]") + " > "
	}
	return prefix + " > "
}

/** Run prints the startup banner, initialises the workspace and knowledge base,
 * runs the backend selection sequence, then starts the interactive REPL. It
 * reads from os.Stdin and writes to out.
 *
 * Parameters:
 *   out (io.Writer) — destination for all REPL output.
 *
 * Returns:
 *   error — only on fatal startup errors; normal exit returns nil.
 *
 * Example:
 *   agent := NewAgent(DefaultConfig(), ws)
 *   if err := agent.Run(os.Stdout); err != nil {
 *       log.Fatal(err)
 *   }
 */
func (a *Agent) Run(out io.Writer) error {
	a.registerCommands()
	if v := os.Getenv("OLLAMA_CONTEXT_LENGTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			a.Config.Ollama.ContextLength = n
		}
	}
	defer func() {
		if a.Recorder != nil {
			path := a.Recorder.Path()
			a.Recorder.Close()
			a.Recorder = nil
			fmt.Fprintf(out, dim("  Session saved to %s\n"), path)
		}
		if a.DebugLog != nil {
			a.DebugLog.Close()
			a.DebugLog = nil
		}
	}()
	// reader is used only for startup yes/no prompts. A 1-byte buffer prevents
	// it from consuming bytes that the LineEditor needs for the REPL loop.
	reader := bufio.NewReaderSize(os.Stdin, 1)
	le := termlib.NewLineEditor(os.Stdin, out)

	// Banner
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintf(out, "  %s  %s\n", bold("Harvey"), dim(Version+" ("+ReleaseHash+")"))
	fmt.Fprintln(out, cyan(bold(sep)))

	// Workspace
	if err := a.initWorkspace(out); err != nil {
		return err
	}
	loadCmdHistory(a.Workspace, le)

	// harvey/harvey.yaml — apply path overrides before any path-dependent init.
	if err := LoadHarveyYAML(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" harvey.yaml: %v\n", err)
	}
	if a.Config.Security.SafeMode {
		fmt.Fprintln(out, green("✓")+" Safe mode on")
	} else {
		fmt.Fprintln(out, yellow("!")+" Safe mode OFF — all commands permitted")
	}

	// Knowledge base
	a.initKnowledgeBase(out)

	// Model capability cache
	a.initModelCache(out)

	// RAG store (optional — only when configured in harvey.yaml)
	a.initRag(out)

	// Memory subsystems (session-scoped; opened once, closed on exit)
	a.initMemory(out)
	defer a.Memory.Close()

	// Sessions directory
	sessDir, err := ResolveSessionsDir(a.Workspace, a.Config.SessionsDir)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Sessions dir: %v\n", err)
	} else {
		a.SessionsDir = sessDir
		fmt.Fprintf(out, green("✓")+" Sessions: %s\n", sessDir)
	}

	// Session resume — offer before backend selection so the chosen session's
	// model can pre-select the backend below.
	var resumePath string
	var sessionModel string
	if a.Config.Session.ContinuePath == "" && a.Config.Session.ReplayPath == "" { // --continue and --replay bypass the picker
		resumePath, sessionModel = a.pickSession(reader, out, sessDir)
	}

	// For --continue / --resume: extract the session's model as a backend hint
	// so selectBackend can auto-select it without an interactive picker.
	if a.Config.Session.ContinuePath != "" && sessionModel == "" {
		if m, err := ExtractModelFromSession(a.Config.Session.ContinuePath); err == nil {
			sessionModel = m
		}
	}

	// System prompt
	if a.Config.SystemPrompt != "" {
		expanded := ExpandDynamicSections(a.Config.SystemPrompt, a.Workspace)
		fmt.Fprintln(out, green("✓")+" Loaded HARVEY.md as system prompt")
		a.AddMessage("system", expanded)
	} else {
		fmt.Fprintln(out, dim("  No HARVEY.md found in current directory"))
	}

	// Skills — scan and inject catalog into system prompt
	a.loadSkills(out)

	// PID re-adoption: if a prior Harvey session left a running backend, adopt it.
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	if adopted := a.tryAdoptPriorBackend(agentsDir, out); !adopted {
		// Backend selection — use sessionModel as the preferred model hint.
		if err := a.selectBackend(reader, out, sessionModel); err != nil {
			return err
		}
	}

	// Debug log — open after backend is known so session_start can record the model.
	if a.Config.Debug && a.SessionsDir != "" {
		logsDir := filepath.Join(filepath.Dir(a.SessionsDir), "logs")
		if dl, err := OpenDebugLog(logsDir); err != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Debug log: %v\n", err)
		} else {
			a.DebugLog = dl
			fmt.Fprintf(out, green("✓")+" Debug log: %s\n", dl.Path())
			// Wire the debug log to the LLM client.
			if ac, ok := a.Client.(*AnyLLMClient); ok {
				ac.DebugLog = dl
			}
			modelID, modelDisplay, provider := "", "", ""
			if a.Client != nil {
				modelDisplay = a.Client.Name()
				if ac, ok := a.Client.(*AnyLLMClient); ok {
					modelID = ac.ModelName()
					provider = ac.ProviderName()
				}
			}
			dl.LogSessionStart(modelID, modelDisplay, provider, a.Workspace.Root, Version)
		}
	}

	// Resume history from chosen session file.
	if resumePath != "" {
		n, contErr := a.ContinueFromFountain(resumePath)
		if contErr != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Resume failed: %v\n", contErr)
		} else {
			fmt.Fprintf(out, green("✓")+" Resumed %d turns from %s\n", n, resumePath)
		}
	}

	// Auto-start recording — skipped during replay to prevent both recorders
	// from writing to the same auto-generated path (same-second collision).
	// When --replay-continue is set, the recorder is started after replay finishes.
	if a.Config.Session.AutoRecord && a.Config.Session.ReplayPath == "" {
		recPath := a.Config.Session.RecordPath
		if recPath == "" {
			recPath = DefaultSessionPath(a.SessionsDir)
		}
		// Guard: never truncate the session being resumed or continued.
		// filepath.Clean normalises both sides so relative vs absolute doesn't matter.
		if (resumePath != "" && filepath.Clean(recPath) == filepath.Clean(resumePath)) ||
			(a.Config.Session.ContinuePath != "" && filepath.Clean(recPath) == filepath.Clean(a.Config.Session.ContinuePath)) {
			recPath = DefaultSessionPath(a.SessionsDir)
			fmt.Fprintf(out, yellow("  ⚠")+" Record path conflicts with resumed session; redirecting to %s\n", recPath)
		}
		model := "none"
		if a.Client != nil {
			model = a.Client.Name()
		}
		if rec, err := NewRecorder(recPath, model, a.Workspace.Root); err != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Auto-record failed: %v\n", err)
		} else {
			a.Recorder = rec
			fmt.Fprintf(out, green("✓")+" Recording to %s\n", recPath)
		}
	}

	// Replay mode — re-send turns to the current backend.
	// Without --replay-continue, returns after replay (no REPL).
	// With --replay-continue, falls through to the REPL with replay history loaded.
	if a.Config.Session.ReplayPath != "" {
		outPath := a.Config.Session.ReplayOutputPath
		if outPath == "" {
			outPath = DefaultSessionPath(a.SessionsDir)
		}
		replayCtx, replayCancel := context.WithCancel(context.Background())
		defer replayCancel()
		fmt.Fprintln(out, cyan(bold(sep)))
		fmt.Fprintf(out, "  Replay mode: %s\n", a.Config.Session.ReplayPath)
		fmt.Fprintln(out, cyan(bold(sep)))
		fmt.Fprintln(out)
		if err := a.ReplayFromFountain(replayCtx, a.Config.Session.ReplayPath, outPath, out); err != nil {
			return err
		}
		if !a.Config.Session.ReplayContinue {
			return nil
		}
		// --replay-continue: start a fresh recorder for the REPL session that follows.
		if a.Config.Session.AutoRecord {
			contRecPath := DefaultSessionPath(a.SessionsDir)
			model := "none"
			if a.Client != nil {
				model = a.Client.Name()
			}
			if rec, err := NewRecorder(contRecPath, model, a.Workspace.Root); err != nil {
				fmt.Fprintf(out, yellow("  ✗")+" Auto-record failed: %v\n", err)
			} else {
				a.Recorder = rec
				fmt.Fprintf(out, green("✓")+" Recording continuation to %s\n", contRecPath)
			}
		}
		fmt.Fprintln(out, cyan(bold(sep)))
		fmt.Fprintln(out, "  Replay complete — continuing in REPL with replay history loaded.")
		fmt.Fprintln(out, cyan(bold(sep)))
		fmt.Fprintln(out)
	}

	// --continue flag: pre-load history from a named session file.
	if a.Config.Session.ContinuePath != "" {
		if a.Client == nil {
			fmt.Fprintf(out, yellow("  ⚠")+" No backend connected — %s will load read-only.\n", a.Config.Session.ContinuePath)
			fmt.Fprintln(out, dim("  Use /llamafile start or /ollama start to connect a model."))
		} else if sessionModel != "" {
			// Health check: warn when the connected backend differs from the
			// session's recorded model so the user knows responses may change.
			connected := strings.ToUpper(activeModelLabel(a))
			if !strings.Contains(connected, sessionModel) {
				fmt.Fprintf(out, yellow("  ⚠")+" Session used %s but connected to %s — responses may differ.\n",
					sessionModel, activeModelLabel(a))
				fmt.Fprintln(out, dim("  Use /model use NAME or /llamafile use NAME to switch."))
			}
		}
		n, contErr := a.ContinueFromFountain(a.Config.Session.ContinuePath)
		if contErr != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Continue failed: %v\n", contErr)
		} else {
			fmt.Fprintf(out, green("✓")+" Loaded %d turns from %s\n", n, a.Config.Session.ContinuePath)
		}
	}

	// Ready line
	fmt.Fprintln(out, cyan(bold(sep)))
	if a.Client != nil {
		fmt.Fprintf(out, "  Connected: %s\n", green(activeModelLabel(a)))
	} else {
		fmt.Fprintf(out, "  %s\n", yellow("No backend — use /llamafile start or /ollama start"))
	}
	fmt.Fprintln(out, dim("  /help for commands · /exit to quit"))
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintln(out)

	// Memory digest — dim hints about pending actions, only printed when actionable.
	sessionMemoryDigest(a, out)

	// Workspace profile onboarding — runs once when workspace_profile/ is empty.
	if a.Workspace != nil && a.Config.Memory.Enabled && a.Config.Session.ReplayPath == "" &&
		a.Memory != nil && a.Memory.Store != nil {
		if NeedsOnboarding(a.Memory.Store) {
			var onboardEmbedder Embedder
			if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
				onboardEmbedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
			}
			if onboardErr := RunOnboarding(a, a.Memory.Store, onboardEmbedder, out, reader); onboardErr != nil {
				fmt.Fprintf(out, yellow("  ✗")+" Onboarding: %v\n", onboardErr)
			}
		}
	}

	// Attach completer now that all state (models, routes, aliases) is loaded.
	le.Completer = a.buildCompleter()

	// REPL
	for {
		input, err := le.Prompt(a.prompt())
		if err == io.EOF || err == termlib.ErrInterrupted {
			saveCmdHistory(a.Workspace, le)
			fmt.Fprintln(out, dim("Goodbye."))
			return nil
		}
		if err != nil {
			return err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// charName carries the ALL-CAPS model name for @mention local-switch turns
		// so tool call notes in the session are attributed to the switched model.
		// Reset each iteration; set below when attemptModelSwitch succeeds.
		charName := ""

		// Sticky route: prepend @ActiveRoute when set and no explicit @ prefix given.
		if a.ActiveRoute != "" && !strings.HasPrefix(input, "@") && !strings.HasPrefix(input, "/") {
			input = "@" + a.ActiveRoute + " " + input
		}

		// "@name" prefix — inline model switch; remainder is forwarded as prompt.
		if strings.HasPrefix(input, "@") && !strings.HasPrefix(input, "@route:") {
			parts := strings.SplitN(input, " ", 2)
			name := strings.TrimPrefix(parts[0], "@")
			rest := ""
			if len(parts) > 1 {
				rest = strings.TrimSpace(parts[1])
			}
			switched, switchErr := attemptModelSwitch(a, name, out)
			if switchErr != nil {
				fmt.Fprintf(out, yellow("  ⚠ Model switch failed: ")+"%v\n", switchErr)
				continue
			}
			if switched {
				le.AppendHistory(input)
				if rest == "" {
					continue // switch-only turn
				}
				input = rest // forward remainder as prompt
			}
			// name not found — fall through to normal chat
		}

		if strings.HasPrefix(input, "/") {
			le.AppendHistory(input)
			shouldExit, cmdErr := a.dispatch(input, out)
			if cmdErr != nil {
				fmt.Fprintf(out, red("Error: ")+"%v\n", cmdErr)
			}
			if shouldExit {
				break
			}
			continue
		}

		// "!" prefix — run a shell command, stream output live, inject into context.
		// Security: Parses the command line to avoid shell injection via sh -c.
		// Supports simple commands with quoted arguments but does NOT support
		// shell metacharacters (|, >, <, &, ;, etc.) for security.
		if strings.HasPrefix(input, "!") {
			cmdLine := strings.TrimSpace(strings.TrimPrefix(input, "!"))
			if cmdLine == "" {
				continue
			}
			le.AppendHistory(input)
			fmt.Fprintf(out, "  $ %s\n", cmdLine)

			// Parse command line into program and arguments
			// Uses shell-like quoting but does NOT process shell metacharacters
			program, args, err := parseCommandLine(cmdLine)
			if err != nil {
				fmt.Fprintf(out, red("Error parsing command: %v\n"), err)
				continue
			}

			// Safe mode check: verify command is in allowlist
			if a.Config.Security.SafeMode && !a.Config.IsCommandAllowed(program) {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionCommand, program+" "+strings.Join(args, " "), StatusDenied)
				}
				fmt.Fprintf(out, yellow("  Command %q is not allowed in safe mode.\n"), program)
				fmt.Fprintf(out, "  Allowed commands: %s\n", strings.Join(a.Config.Security.AllowedCommands, ", "))
				fmt.Fprintln(out, "  Use /safemode off to disable, or /safemode allow CMD to add it.")
				continue
			}

			// Log allowed command execution
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionCommand, program+" "+strings.Join(args, " "), StatusAllowed)
			}

			var capBuf bytes.Buffer
			mw := io.MultiWriter(out, &capBuf)

			var bashCtx context.Context
			var cancelBash context.CancelFunc
			if a.Config.Security.RunTimeout > 0 {
				bashCtx, cancelBash = context.WithTimeout(context.Background(), a.Config.Security.RunTimeout)
			} else {
				bashCtx, cancelBash = context.WithCancel(context.Background())
			}
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			wasCancelled := false
			watchDone := make(chan struct{})
			go func() {
				defer signal.Stop(sigCh)
				select {
				case <-sigCh:
					wasCancelled = true
					cancelBash()
				case <-watchDone:
				}
			}()

			shCmd := exec.CommandContext(bashCtx, program, args...)
			if a.Workspace != nil {
				shCmd.Dir = a.Workspace.Root
			}
			// Restrict environment to prevent inheriting sensitive variables
			shCmd.Env = filterCommandEnvironment(os.Environ())
			shCmd.Stdout = mw
			shCmd.Stderr = mw
			runErr := shCmd.Run()
			close(watchDone)
			cancelBash()

			fmt.Fprintln(out)
			if wasCancelled || errors.Is(runErr, context.Canceled) {
				fmt.Fprintln(out, dim("  Cancelled."))
				continue
			}

			exitCode := 0
			exitNote := ""
			if shCmd.ProcessState != nil {
				exitCode = shCmd.ProcessState.ExitCode()
				if exitCode != 0 {
					exitNote = fmt.Sprintf(" (exit %d)", exitCode)
				}
			}

			output := capBuf.Bytes()
			truncated := false
			if len(output) > maxRunOutput {
				output = output[:maxRunOutput]
				truncated = true
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("[context: ! %s%s]\n\n```\n", cmdLine, exitNote))
			sb.Write(output)
			if truncated {
				sb.WriteString("\n... (output truncated)")
			}
			sb.WriteString("\n```\n")
			a.AddMessage("user", sb.String())
			fmt.Fprintf(out, dim("  %d bytes added to context%s.\n"), len(output), exitNote)

			if a.Recorder != nil {
				if recErr := a.Recorder.RecordShellCommand(cmdLine, string(output), exitCode); recErr != nil {
					fmt.Fprintf(out, yellow("  ✗")+" Recording error: %v\n", recErr)
				}
			}
			continue
		}

		// @mention: dispatch to a registered remote endpoint, or switch to a
		// local model when the name matches a registered llamafile or alias.
		if name, prompt, ok := ParseAtMention(input); ok {
			// Step 1: Route dispatch — only when routing is enabled and the
			// name matches a registered endpoint.
			if a.Routes != nil && a.Routes.Enabled {
				if ep := a.Routes.Lookup(name); ep != nil {
					le.AppendHistory(input)
					fmt.Fprintf(out, dim("  → dispatching to @%s\n"), name)
					fmt.Fprintln(out)

					dispCtx, cancelDisp := context.WithCancel(context.Background())
					sigCh := make(chan os.Signal, 1)
					signal.Notify(sigCh, os.Interrupt)
					wasCancelled := false
					watchDone := make(chan struct{})
					go func() {
						defer signal.Stop(sigCh)
						select {
						case <-sigCh:
							wasCancelled = true
							cancelDisp()
						case <-watchDone:
						}
					}()

					sp := newSpinner(out, 0, routeSpinnerLabel(name, ep))
					sp.UpdateStatus("routed → " + name)
					reply, dispErr := DispatchToEndpoint(dispCtx, ep, a.History, prompt, a.Config, a.Tools, io.Discard)
					sp.stop()
					close(watchDone)
					cancelDisp()

					if wasCancelled || errors.Is(dispErr, context.Canceled) {
						fmt.Fprintln(out, dim("  Cancelled."))
						continue
					}
					if dispErr != nil {
						fmt.Fprintf(out, red("Error: ")+"%v\n", dispErr)
						continue
					}

					fmt.Fprint(out, reply)
					fmt.Fprintln(out)
					fmt.Fprintln(out, dim("  @"+name))
					a.AddMessage("user", input)
					a.AddMessage("assistant", reply)

					if a.Recorder != nil {
						// Route dispatch is remote computation — use EXT. scene.
						if recErr := a.Recorder.RecordExteriorTurn(strings.ToUpper(name), input, reply); recErr != nil {
							fmt.Fprintf(out, yellow("  ✗")+" Recording error: %v\n", recErr)
						}
					}
					a.autoExecuteReply(reply, out, reader, context.Background())
					continue
				}
			}

			// Step 2: Local model switch — try registered llamafiles and aliases.
			switched, switchErr := attemptModelSwitch(a, name, out)
			if switchErr != nil {
				fmt.Fprintf(out, red("  @%s switch failed: ")+"%v\n", name, switchErr)
				continue
			}
			if switched {
				if prompt == "" {
					continue // pure model switch, no prompt to send this turn
				}
				// Switch succeeded with a trailing prompt — send it to the new model.
				// Local computation: stays INT., but attribute tool calls to the
				// switched model by passing its name as charName.
				input = prompt
				charName = strings.ToUpper(name)
				// Fall through to the normal chat path below.
			} else {
				// Step 3: Tag-based lookup — @code resolves to any alias tagged "code".
				if tagModel, ok := resolveTagAlias(a.Config, name); ok {
					tagSwitched, tagErr := attemptModelSwitch(a, tagModel, out)
					if tagErr != nil {
						fmt.Fprintf(out, red("  @%s switch failed: ")+"%v\n", name, tagErr)
						continue
					}
					if tagSwitched {
						if prompt == "" {
							continue
						}
						input = prompt
						charName = strings.ToUpper(name)
						// Fall through to the normal chat path below.
					} else {
						fmt.Fprintf(out, yellow("  @%s not found.")+" Use /route list or /model list to see available models.\n", name)
						continue
					}
				} else {
					// Name not found as a route, local model, or tag.
					fmt.Fprintf(out, yellow("  @%s not found.")+" Use /route list or /model list to see available models.\n", name)
					continue
				}
			}
		}

		// Chat
		if a.Client == nil {
			fmt.Fprintln(out, yellow("No backend connected.")+" Use /ollama start.")
			continue
		}

		le.AppendHistory(input)

		// Intercept skill-related questions and answer directly from the
		// catalog. Small models (e.g. tinyllama) reliably ignore the
		// <available_skills> system-prompt block, so we handle these
		// locally rather than letting the LLM hallucinate an answer.
		if len(a.Skills) > 0 && LooksLikeSkillQuery(input) {
			fmt.Fprintln(out, dim("  (answered from skill catalog — use /skill load NAME to activate a skill)"))
			cmdSkill(a, []string{"list"}, out)
			continue
		}

		// Auto-dispatch compiled skills whose trigger pattern matches the input.
		// Iterate sorted names for deterministic first-match semantics.
		skillWantsLLM := false
		if len(a.Skills) > 0 {
			triggered := false
			for _, name := range SortedSkillNames(a.Skills) {
				skill := a.Skills[name]
				if MatchesTrigger(skill, input) {
					fmt.Fprintf(out, dim("  (trigger matched skill %q)\n"), name)
					triggerReader := bufio.NewReaderSize(a.In, 1)
					var err error
					skillWantsLLM, err = DispatchSkill(context.Background(), a, skill, input, triggerReader, out)
					if err != nil {
						fmt.Fprintf(out, red("  ✗ skill dispatch error: ")+"%v\n", err)
					}
					triggered = true
					break
				}
			}
			// Compiled scripts are self-contained — skip the LLM call.
			// LLM-fallback skills inject context and need the LLM to respond.
			if triggered && !skillWantsLLM {
				continue
			}
		}

		// Send the prompt through Harvey's shared chat pipeline. runChatTurn
		// owns RAG augmentation, the tool-loop-or-plain-chat branch, display,
		// recording, and rolling-summary compression — see its definition
		// below (also used by /loop, with interactive write-offers disabled).
		chatCtx, cancelChat := context.WithCancel(context.Background())
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		watchDone := make(chan struct{})
		go func() {
			defer signal.Stop(sigCh)
			select {
			case <-sigCh:
				cancelChat()
			case <-watchDone:
			}
		}()

		reply, _, turnErr := a.runChatTurn(chatCtx, input, out, reader, true, charName)
		close(watchDone)
		cancelChat()

		// Auto-reconnect: when the llamafile server drops mid-session, offer
		// to restart it and retry the turn rather than surfacing a raw HTTP error.
		if turnErr != nil && isConnectionError(turnErr) &&
			a.Backend != nil && a.Backend.StartedByHarvey() && !probeActiveBackend(a) {
			fmt.Fprintln(out, yellow("  ⚠ The llamafile server stopped unexpectedly."))
			if askYesNo(reader, out, fmt.Sprintf("  Restart %s? [Y/n] ", a.Config.Llamafile.Active), true) {
				if restartErr := restartActiveLlamafile(a, out); restartErr == nil {
					retryCtx, retryCancel := context.WithCancel(context.Background())
					reply, _, turnErr = a.runChatTurn(retryCtx, input, out, reader, true, charName)
					retryCancel()
				} else {
					fmt.Fprintf(out, red("  Restart failed: ")+"%v\n", restartErr)
				}
			}
		}

		// When a skill used the LLM-fallback path and the model responded with a
		// parseable plan checklist, auto-save it to agents/plan.md so the user
		// can immediately run /plan next without any extra steps.
		if skillWantsLLM && reply != "" && a.Workspace != nil {
			if p, err := PlanFromLLMResponse(reply, input); err == nil && len(p.Steps) > 0 {
				if saveErr := SavePlan(a.Workspace, p); saveErr == nil {
					fmt.Fprintf(out, green("  ✓")+" Plan saved to agents/plan.md (%d steps). Run %s to start.\n",
						len(p.Steps), cyan("/plan next"))
				}
			}
		}

		if errors.Is(turnErr, context.Canceled) {
			fmt.Fprintln(out, dim("  Cancelled."))
			continue
		}
		if turnErr != nil {
			fmt.Fprintf(out, red("Error: ")+"%v\n", turnErr)
			continue
		}
	}

	saveCmdHistory(a.Workspace, le)

	// Stop any backend Harvey started and clean up the PID file.
	if a.Backend != nil && a.Backend.StartedByHarvey() {
		_ = a.Backend.Stop()
		_ = deletePIDFile(agentsDir)
	}

	fmt.Fprintln(out, dim("Goodbye."))

	// Auto-mine on exit when the session had >= 10 user turns.
	// Close the recorder here so the session file is complete before mining.
	// The deferred recorder-close checks a.Recorder != nil, so setting it to
	// nil here prevents a double-close.
	const autoMineTurnThreshold = 10
	if a.Config.Memory.Enabled && a.Workspace != nil &&
		a.Recorder != nil && a.sessionTurns >= autoMineTurnThreshold {
		sessionPath := a.Recorder.Path()
		a.Recorder.Close()
		a.Recorder = nil
		fmt.Fprintf(out, dim("  Session saved to %s\n"), sessionPath)

		fmt.Fprintln(out, dim("  [auto-mining session for memories — use /memory mine to review manually]"))
		if a.Memory != nil && a.Memory.Store != nil {
			manifest, mErr := LoadManifest(a.Memory.Store.Dir())
			if mErr == nil && !manifest.IsMined(sessionPath) {
				var embedder Embedder
				if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
					embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
				}
				miner := NewMiner(a.Memory.Store, manifest, a.Workspace)
				if mineErr := miner.MineAuto(context.Background(), sessionPath, a, embedder, out); mineErr != nil {
					fmt.Fprintf(out, "%s Auto-mine: %v\n", yellow("  ✗"), mineErr)
				}
			}
		}
	}

	// Record session memory stats for adaptive budget tuning.
	if a.Config.Memory.Enabled && a.Memory != nil && a.Memory.Store != nil {
		sessionID := ""
		if a.Recorder != nil {
			sessionID = filepath.Base(a.Recorder.Path())
		}
		budget := 512
		if a.Config.Ollama.ContextLength > 0 && a.Config.Memory.BudgetPct > 0 {
			budget = int(float64(a.Config.Ollama.ContextLength) * a.Config.Memory.BudgetPct)
		}
		_ = a.Memory.Store.RecordSessionStats(sessionID, budget, a.sessionInjectedTokens,
			a.sessionCompressed, a.avgToksPerSec())
	}

	return nil
}

/** runChatTurn sends input through Harvey's chat pipeline — RAG augmentation,
 * the tool-loop-or-plain-chat branch, reply display, stats recording, and
 * rolling-summary compression — and returns the assembled reply. It is shared
 * by the REPL's chat path and /loop, which is why the interactive write-offers
 * (the fenced-code-block "write to file?" prompts and autoExecuteReply) are
 * gated by the interactive flag: an unattended /loop run must never block on
 * stdin waiting for a Y/n answer that nothing will type.
 *
 * Parameters:
 *   ctx (context.Context) — cancelled by the caller's SIGINT watcher; checked
 *     both during and after the model call
 *   input (string) — the raw user prompt for this turn
 *   out (io.Writer) — destination for displayed output (reply, stats, warnings)
 *   reader (*bufio.Reader) — stdin reader, used only when interactive is true
 *   interactive (bool) — when true, offers to write fenced code blocks to
 *     files and runs autoExecuteReply; when false (e.g. /loop), both are skipped
 *
 * Returns:
 *   string — the assistant's reply text (empty on error or cancellation)
 *   ChatStats — token/timing stats for the turn (zero value on error or cancellation)
 *   error — context.Canceled if the turn was cancelled, otherwise any chat error
 *
 * Example:
 *   reply, stats, err := a.runChatTurn(ctx, "Summarize the diff", out, reader, true)
 *   if err == nil {
 *       fmt.Fprintln(out, reply)
 *   }
 */
func (a *Agent) runChatTurn(ctx context.Context, input string, out io.Writer, reader *bufio.Reader, interactive bool, charName string) (string, ChatStats, error) {
	// Semantic memory injection — fires once per session (or after /clear).
	if a.memoryContextPending {
		a.injectMemoryContext(input)
		a.memoryContextPending = false
	}

	// RAG context injection — prepend relevant chunks before sending.
	augmented, ragInfo := a.ragAugment(input)
	a.LastRAGInfo = ragInfo

	// File-reference injection — for models that don't reliably call read_file,
	// resolve any mentioned file paths and prepend their content directly.
	// injectOrChunk extends injectFileContext with chunked analysis for large files.
	if !a.toolsReliable() {
		augmented = a.injectOrChunk(ctx, augmented, out)
	}

	augmented += stmWarnNudge(a)
	a.AddMessage("user", augmented)

	// Token-count warning — runs only when the backend is Ollama.
	if ac, ok := a.Client.(*AnyLLMClient); ok && ac.ProviderName() == "ollama" {
		histText := HistoryText(a.History)
		n, exact := CountTokens(context.Background(), ac.BackendURL(), ac.ModelName(), histText)
		limit := a.Config.Ollama.ContextLength
		qualifier := "~"
		if exact {
			qualifier = ""
		}
		if limit > 0 {
			pct := n * 100 / limit
			switch {
			case pct >= 100:
				fmt.Fprintf(out, red("  ✗ Context full: %s%d / %d tokens (%d%%) — reply may be truncated\n"), qualifier, n, limit, pct)
			case pct >= 80:
				fmt.Fprintf(out, yellow("  ⚠ Context %d%% full: %s%d / %d tokens\n"), pct, qualifier, n, limit)
			}
		}
	}
	var modelsUsed []string
	if a.Client != nil {
		modelsUsed = []string{a.Client.Name()}
	}
	fmt.Fprintln(out)

	// Spinner label: when routing escalated, name the full model and note it is working.
	spLabel := a.spinnerLabel()
	if len(modelsUsed) > 1 {
		spLabel = a.Client.Name() + " · working on it"
		if a.ActiveSkill != "" {
			spLabel += " · " + a.ActiveSkill
		}
	}

	histLenBeforeChat := len(a.History)
	var buf strings.Builder
	sp := newSpinner(out, a.estimateDuration(), spLabel)
	var stats ChatStats
	var chatErr error
	var toolCallRecords []ToolCallRecord
	// Use RunToolLoop only when tools are configured AND the model's ToolMode
	// allows structured calls. Explicit modes prose/inject/none bypass the loop.
	useStructuredTools := a.Tools != nil && a.Config.ToolsEnabled
	if mode := a.modelToolMode(); mode == ToolModeProse || mode == ToolModeInject || mode == ToolModeNone {
		useStructuredTools = false
	}
	if useStructuredTools {
		ex := NewToolExecutor(a.Tools, a.Client, a.Config)
		ex.DebugLog = a.DebugLog
		ex.Status = sp
		ex.CharacterName = charName
		var updatedHistory []Message
		updatedHistory, stats, chatErr = ex.RunToolLoop(ctx, a.History, &buf)
		if chatErr == nil {
			// Preserve any intermediate tool-call/result messages added by the loop.
			a.History = updatedHistory
			toolCallRecords = toolCallsFromHistory(a.History[histLenBeforeChat:], charName)
		}
	} else {
		stats, chatErr = a.Client.Chat(ctx, a.History, &buf)
	}
	sp.stop()

	if ctx.Err() != nil || errors.Is(chatErr, context.Canceled) {
		a.History = a.History[:len(a.History)-1]
		return "", ChatStats{}, context.Canceled
	}
	if errors.Is(chatErr, ErrToolLoopExceeded) {
		// Small models (e.g. llama3.2:3b) sometimes call tools indefinitely
		// and never produce a text response. Warn and retry without tools.
		// Use a fresh context — ctx may already be near the end of its life,
		// and this fallback call should not inherit that.
		fmt.Fprintln(out, yellow("  ⚠")+" Model entered a tool-calling loop; retrying without tools.")
		fmt.Fprintln(out, dim("  Tip: /tools off disables tools for this model permanently in this session."))
		buf.Reset()
		fallbackCtx, cancelFallback := context.WithCancel(context.Background())
		stats, chatErr = a.Client.Chat(fallbackCtx, a.History, &buf)
		cancelFallback()
		if chatErr != nil {
			a.History = a.History[:len(a.History)-1]
			return "", ChatStats{}, fmt.Errorf("fallback: %w", chatErr)
		}
		// Fall through to the normal display and recording path.
	} else if chatErr != nil {
		a.History = a.History[:len(a.History)-1]
		return "", ChatStats{}, chatErr
	}

	// hadToolCalls records whether the first pass used RunToolLoop and executed
	// at least one tool. Captured before option-2 may roll back history so that
	// noToolCalls is accurate even after the rollback+retry.
	hadToolCalls := len(a.History) > histLenBeforeChat

	// Option 2: if the model claimed it cannot read files, retry once with file
	// content pre-loaded. Only fires when injection would add something new (i.e.,
	// option 1 didn't already inject the same content via toolsReliable==false).
	if looksLikeCantReadFile(buf.String()) {
		retryAugmented := injectFileContext(a.Workspace, augmented)
		if retryAugmented != augmented {
			fmt.Fprintln(out, yellow("  ⚠")+" Model declined file access; retrying with content pre-loaded.")
			// Surgical rollback: remove only the assistant refusal; preserve
			// any tool-call/result pairs RunToolLoop added before the refusal
			// so the retry sees the full prior context.
			a.History = a.History[:len(a.History)-1]
			a.AddMessage("user", retryAugmented)
			buf.Reset()
			var retryStats ChatStats
			if useStructuredTools {
				retrySp := newSpinner(out, a.estimateDuration(), spLabel)
				ex := NewToolExecutor(a.Tools, a.Client, a.Config)
				ex.DebugLog = a.DebugLog
				ex.Status = retrySp
				ex.CharacterName = charName
				var updatedHistory []Message
				updatedHistory, retryStats, chatErr = ex.RunToolLoop(ctx, a.History, &buf)
				retrySp.stop()
				if chatErr == nil {
					a.History = updatedHistory
					toolCallRecords = toolCallsFromHistory(a.History[histLenBeforeChat:], charName)
				}
			} else {
				retryStats, chatErr = a.Client.Chat(ctx, a.History, &buf)
			}
			if chatErr == nil {
				stats = retryStats
			} else {
				// Retry failed — restore history and surface the error.
				a.History = a.History[:len(a.History)-1]
				return "", ChatStats{}, fmt.Errorf("retry: %w", chatErr)
			}
		}
	}

	displayText := buf.String()
	if a.Config.SyntaxHighlight {
		displayText = highlightCodeBlocks(displayText)
	}
	fmt.Fprint(out, displayText)
	fmt.Fprintln(out)
	fmt.Fprintln(out, dim(formatStatLine(modelsUsed, stats, a.effectiveContextLimit())))
	if warn := groundingCheck(buf.String(), a.History[histLenBeforeChat:]); warn != "" {
		fmt.Fprintln(out, yellow("  ⚠ ")+warn)
	}
	a.recordStats(stats)
	a.sessionTurns++

	// noToolCalls is true when neither the first pass nor the retry executed
	// any tool calls. hadToolCalls guards against option-2's rollback making
	// the history length temporarily equal to histLenBeforeChat even when the
	// first pass ran tools (whose side-effects are already real).
	noToolCalls := !hadToolCalls && len(a.History) == histLenBeforeChat

	var proseUnknownTools []string
	if a.Workspace != nil && noToolCalls {
		blocks := extractCodeBlocks(buf.String())
		// Warn when the model produced only tool-call-syntax blocks and no
		// substantive content — a sign it ignored the prompt (common in
		// small models like llama3.2 when tools are enabled). This detection
		// and dispatch runs regardless of interactivity — it is a correction
		// mechanism, not a user prompt.
		if len(blocks) > 0 {
			allToolCalls := true
			for _, b := range blocks {
				if !isToolCallBlock(b) {
					allToolCalls = false
					break
				}
			}
			if allToolCalls {
				// Try to parse and execute the prose tool calls before warning.
				dispatched, unknowns := tryExecuteProseToolCalls(a, blocks, out)
				proseUnknownTools = unknowns
				if !dispatched {
					fmt.Fprintln(out, yellow("  ⚠")+" Model produced only tool-call syntax; it may not have answered the question. Try /tools off or a larger model.")
				}
			}
		}
		// Check for Apertus-native <SPECIAL_71>...<SPECIAL_72> tool calls in the
		// raw response text (llama.cpp does not parse this format server-side).
		if _, unknowns := tryExecuteApertusToolCalls(a, buf.String(), out); len(unknowns) > 0 {
			proseUnknownTools = append(proseUnknownTools, unknowns...)
		}
		// Code-block extraction: when the model produced fenced code blocks
		// but made no tool calls, offer to write each block to a file. This
		// handles small local models that respond with prose + a code block
		// instead of invoking the write_file tool. Interactive only — an
		// unattended /loop run must never block on stdin for a Y/n answer.
		if interactive {
			for i, block := range blocks {
				// Skip JSON blocks that look like tool call invocations — small
				// models sometimes write these as prose instead of structured calls.
				if isToolCallBlock(block) {
					continue
				}
				label := "code block"
				if block.Lang != "" {
					label = block.Lang + " block"
				}
				if len(blocks) > 1 {
					label += fmt.Sprintf(" %d/%d", i+1, len(blocks))
				}
				fmt.Fprintf(out, dim("\n[Write %s to file? Path (or Enter to skip)]: "), label)
				pathLine, _ := reader.ReadString('\n')
				pathLine = strings.TrimSpace(pathLine)
				if pathLine == "" {
					continue
				}
				if _, pathErr := resolveWorkspacePath(a.Workspace.Root, pathLine); pathErr != nil {
					fmt.Fprintf(out, red("  ✗")+" %v\n", pathErr)
					continue
				}
				if !a.CheckWritePermission(pathLine) {
					fmt.Fprintf(out, red("  ✗")+" write permission denied for %q\n", pathLine)
					if a.AuditBuffer != nil {
						a.AuditBuffer.Log(ActionFileWrite, pathLine, StatusDenied)
					}
					continue
				}
				if writeErr := a.Workspace.WriteFile(pathLine, []byte(block.Content), 0o644); writeErr != nil {
					fmt.Fprintf(out, red("  ✗")+" write failed: %v\n", writeErr)
					if a.AuditBuffer != nil {
						a.AuditBuffer.Log(ActionFileWrite, pathLine, StatusError)
					}
				} else {
					fmt.Fprintf(out, green("✓")+" wrote %d bytes to %s\n", len(block.Content), pathLine)
					if a.AuditBuffer != nil {
						a.AuditBuffer.Log(ActionFileWrite, pathLine, StatusSuccess)
					}
				}
			}
		}
	}

	a.AddMessage("assistant", buf.String())
	if a.Recorder != nil {
		if recErr := a.Recorder.RecordTurnWithStats(input, buf.String(), stats, modelsUsed, "", toolCallRecords, ragInfo); recErr != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Recording error: %v\n", recErr)
		}
	}
	// Feed unknown-tool errors back into history so the model can self-correct
	// on the next turn (now that the assistant message is in place, the history
	// order is correct: …, assistant: <hallucinated call>, user: <correction>).
	if len(proseUnknownTools) > 0 && a.Tools != nil {
		schemas := a.Tools.GetToolSchemas()
		available := make([]string, len(schemas))
		for i, s := range schemas {
			available[i] = s.Function.Name
		}
		correction := "Your tool call(s) failed: the tool name(s) " +
			strings.Join(proseUnknownTools, ", ") +
			" do not exist. Available tools: " +
			strings.Join(available, ", ") +
			". Please retry using one of these exact tool names."
		a.AddMessage("user", correction)
	}
	// Skip autoExecuteReply when the model already wrote files via tool calls —
	// the response text may contain display-only code blocks (e.g. directory
	// trees) that should not be offered as files to write. Interactive only,
	// for the same stdin-blocking reason as the code-block-write offers above.
	if interactive && noToolCalls {
		a.autoExecuteReply(buf.String(), out, reader, ctx)
	}

	// Rolling summary: compress history when approaching the context limit.
	if a.Config.Memory.RollingSummary.Enabled && a.Client != nil {
		contextLen := a.effectiveContextLimit()
		if contextLen > 0 {
			histTokens := estimateTokens(HistoryText(a.History))
			if ShouldCompress(histTokens, contextLen, a.Config.Memory.RollingSummary.WarnAtPct) {
				pct := histTokens * 100 / contextLen
				fmt.Fprintln(out, dim(fmt.Sprintf("  [context ~%d%% full — compressing older turns]", pct)))
				if compErr := CompressHistory(a, a.Config.Memory.RollingSummary.KeepTurns, out); compErr != nil {
					fmt.Fprintf(out, "%s Compression failed: %v\n", yellow("  ✗"), compErr)
				} else {
					a.sessionCompressed = true
				}
			}
		}
	}

	return buf.String(), stats, nil
}


const (
	cmdHistoryFile    = "agents/harvey_history"
	cmdHistoryMaxSize = 1000
)

/** loadCmdHistory reads agents/harvey_history from the workspace and seeds the
 * LineEditor's history list. Missing or unreadable files are silently ignored
 * so a missing history file on first run is not an error.
 *
 * Parameters:
 *   ws (*Workspace)         — workspace to resolve the history file path.
 *   le (*termlib.LineEditor) — editor whose history is seeded.
 *
 * Example:
 *   loadCmdHistory(a.Workspace, le)
 */
func loadCmdHistory(ws *Workspace, le *termlib.LineEditor) {
	if ws == nil {
		return
	}
	data, err := ws.ReadFile(cmdHistoryFile)
	if err != nil {
		return
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	le.SetHistory(lines)
}

/** saveCmdHistory writes the LineEditor's current history to agents/harvey_history
 * in the workspace, keeping only the most recent cmdHistoryMaxSize entries.
 * Errors are silently ignored so a write failure never crashes Harvey on exit.
 *
 * Parameters:
 *   ws (*Workspace)         — workspace to resolve the history file path.
 *   le (*termlib.LineEditor) — editor whose history is persisted.
 *
 * Example:
 *   saveCmdHistory(a.Workspace, le)
 */
func saveCmdHistory(ws *Workspace, le *termlib.LineEditor) {
	if ws == nil {
		return
	}
	entries := le.History()
	if len(entries) > cmdHistoryMaxSize {
		entries = entries[len(entries)-cmdHistoryMaxSize:]
	}
	data := []byte(strings.Join(entries, "\n") + "\n")
	_ = ws.WriteFile(cmdHistoryFile, data, 0o600)
}

// toolCallsFromHistory extracts ToolCallRecords from a slice of history
// messages. It pairs each tool call from assistant turns with its result
// status from the corresponding tool-role message. charName is stamped onto
// every record's Character field; pass "" for HARVEY (default, no prefix).
func toolCallsFromHistory(msgs []Message, charName string) []ToolCallRecord {
	// Build a map from ToolCallID to result status derived from tool-role messages.
	resultByID := make(map[string]string)
	for _, m := range msgs {
		if m.Role != "tool" || m.ToolCallID == "" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		first := strings.SplitN(content, "\n", 2)[0]
		if strings.HasPrefix(first, "error:") {
			resultByID[m.ToolCallID] = first
		} else {
			resultByID[m.ToolCallID] = "ok"
		}
	}
	var out []ToolCallRecord
	for _, m := range msgs {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			result := resultByID[tc.ID]
			if result == "" {
				result = "ok"
			}
			out = append(out, ToolCallRecord{
				Name:      tc.Function.Name,
				Args:      tc.Function.Arguments,
				Result:    result,
				Character: charName,
			})
		}
	}
	return out
}

// routeSpinnerLabel returns the spinner label for an @mention dispatch turn.
// When the endpoint has a model name configured, it is shown alongside the
// route alias so the user can see exactly which model is handling the request.
func routeSpinnerLabel(name string, ep *RouteEndpoint) string {
	if ep != nil && ep.Model != "" {
		return "@" + name + " · " + ep.Model
	}
	return "@" + name + " · working"
}

// formatStatLine produces the permanent post-response status line.
// models lists the model name(s) that handled the turn (e.g. ["fast", "full"]
// when routing escalated, or a single name otherwise).
// contextLimit is the model's context window in tokens (0 = unknown).
func formatStatLine(models []string, stats ChatStats, contextLimit int) string {
	modelPart := strings.Join(models, " → ")
	elapsed := stats.Elapsed.Round(time.Millisecond)

	if stats.ReplyTokens == 0 {
		if modelPart == "" {
			return ""
		}
		return "  " + modelPart + " · " + elapsed.String()
	}

	var ctxPart string
	if contextLimit > 0 && stats.PromptTokens > 0 {
		pct := stats.PromptTokens * 100 / contextLimit
		ctxPart = fmt.Sprintf("%d/%d ctx (%d%%)", stats.PromptTokens, contextLimit, pct)
	} else {
		ctxPart = fmt.Sprintf("%d ctx", stats.PromptTokens)
	}

	core := fmt.Sprintf("%d reply + %s · %s · %.1f tok/s",
		stats.ReplyTokens, ctxPart, elapsed, stats.TokensPerSec)
	if modelPart == "" {
		return "  " + core
	}
	return "  " + modelPart + " · " + core
}

// initWorkspace resolves and announces the workspace root. It is a fatal error
// if the workspace cannot be created.
func (a *Agent) initWorkspace(out io.Writer) error {
	ws, err := NewWorkspace(a.Config.WorkDir)
	if err != nil {
		return fmt.Errorf("workspace init: %w", err)
	}
	a.Workspace = ws
	fmt.Fprintf(out, green("✓")+" Workspace: %s\n", ws.Root)
	return nil
}

// initKnowledgeBase opens (or creates) the SQLite knowledge base. Failures are
// non-fatal: the user is warned but Harvey continues without a KB.
func (a *Agent) initKnowledgeBase(out io.Writer) {
	kb, err := OpenKnowledgeBase(a.Workspace, a.Config.Memory.KnowledgeDB)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Knowledge base unavailable: %v\n", err)
		return
	}
	a.KB = kb
	fmt.Fprintf(out, green("✓")+" Knowledge base: %s\n", kb.Path())
}

// initModelCache opens (or creates) the SQLite model capability cache. Failures
// are non-fatal: the user is warned but Harvey continues without a cache.
func (a *Agent) initModelCache(out io.Writer) {
	mc, err := OpenModelCache(a.Workspace, a.Config.ModelCacheDB)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Model cache unavailable: %v\n", err)
		return
	}
	a.ModelCache = mc
	fmt.Fprintf(out, green("✓")+" Model cache: %s\n", mc.Path())
}

// initRag opens the active RAG store when one is configured in harvey.yaml.
// Failures are non-fatal. RagOn is set to match cfg.Memory.RagEnabled.
func (a *Agent) initRag(out io.Writer) {
	entry := a.Config.Memory.ActiveRagStore()
	if entry == nil {
		return
	}
	dbPath, err := a.Workspace.AbsPath(entry.DBPath)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" RAG store unavailable: %v\n", err)
		return
	}
	store, err := NewRagStore(dbPath, entry.EmbeddingModel)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" RAG store unavailable: %v\n", err)
		return
	}
	a.Rag = store
	a.RagOn = a.Config.Memory.RagEnabled
	status := "off"
	if a.RagOn {
		status = "on"
	}
	fmt.Fprintf(out, green("✓")+" RAG store: %s (%s) [%s]\n", entry.Name, entry.DBPath, status)
}

// initMemory opens the session-scoped MemorySystem. Failures are non-fatal —
// memory features degrade gracefully when the store cannot be opened.
func (a *Agent) initMemory(out io.Writer) {
	if !a.Config.Memory.Enabled || a.Workspace == nil {
		return
	}
	ms, err := OpenMemory(a.Workspace, &a.Config.Memory)
	a.Memory = ms
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Memory store unavailable: %v\n", err)
	}
}

// pickSession scans sessDir for .spmd and .fountain files and, if any exist,
// asks the user whether to resume one. Returns the chosen file path and the
// ALL-CAPS model name extracted from it; both are empty if no session is chosen.
func (a *Agent) pickSession(reader *bufio.Reader, out io.Writer, sessDir string) (path, model string) {
	files, err := ListSessionFiles(sessDir)
	if err != nil || len(files) == 0 {
		return "", ""
	}
	if !askYesNo(reader, out, "  Resume a prior session? [y/N] ", false) {
		return "", ""
	}
	fmt.Fprintln(out)
	limit := len(files)
	if limit > 20 {
		limit = 20
	}
	for i, f := range files[:limit] {
		fmt.Fprintf(out, "    [%d] %-44s  %s\n", i+1,
			f.Name, f.ModTime.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(out, "    Select session [1-%d, 0=none]: ", limit)
	line, _ := reader.ReadString('\n')
	idx := 0
	fmt.Sscanf(strings.TrimSpace(line), "%d", &idx)
	if idx < 1 || idx > limit {
		return "", ""
	}
	chosen := files[idx-1]
	m, _ := ExtractModelFromSession(chosen.Path)
	return chosen.Path, m
}

// loadSkills scans the standard skill directories, stores the catalog on the
// agent, and appends the XML catalog block to the system prompt so the model
// knows what skills are available. It also updates Config.SystemPrompt so
// that /clear re-injects the catalog after resetting history. Non-fatal:
// if no skills are found the function returns silently.
func (a *Agent) loadSkills(out io.Writer) {
	cat := ScanSkills(a.Workspace.Root, a.Config.AgentsDir)
	if len(cat) == 0 {
		return
	}
	a.Skills = cat
	block := CatalogSystemPromptBlock(cat)

	// Persist in Config so ClearHistory() keeps the catalog across /clear.
	if a.Config.SystemPrompt != "" {
		a.Config.SystemPrompt += "\n\n" + block
	} else {
		a.Config.SystemPrompt = block
	}

	// Update the system message already in History (added before this call).
	injected := false
	for i, m := range a.History {
		if m.Role == "system" {
			a.History[i].Content += "\n\n" + block
			injected = true
			break
		}
	}
	if !injected {
		a.History = append([]Message{{Role: "system", Content: block}}, a.History...)
	}

	proj, user := 0, 0
	for _, s := range cat {
		if s.Source == SkillSourceProject {
			proj++
		} else {
			user++
		}
	}
	detail := ""
	switch {
	case proj > 0 && user > 0:
		detail = fmt.Sprintf(" (%d project, %d user)", proj, user)
	case proj > 0:
		detail = " (project)"
	default:
		detail = " (user)"
	}
	fmt.Fprintf(out, green("✓")+" Skills: %d skill(s) available%s\n", len(cat), detail)
	a.registerSkillCommands()
}

/** askYesNo prints prompt, reads a line, and returns true for "y"/"yes".
 * defaultYes controls what an empty (Enter) response means.
 *
 * Parameters:
 *   reader     (*bufio.Reader) — source for the user's answer.
 *   out        (io.Writer)     — destination for the prompt string.
 *   prompt     (string)        — text to print before reading.
 *   defaultYes (bool)          — return value when the user presses Enter.
 *
 * Returns:
 *   bool — true if the user answered yes (or pressed Enter with defaultYes=true).
 *
 * Example:
 *   if askYesNo(out, reader, "Continue? [Y/n] ", true) {
 *       // proceed
 *   }
 */
func askYesNo(reader *bufio.Reader, out io.Writer, prompt string, defaultYes bool) bool {
	fmt.Fprint(out, prompt)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

// buildCompleter returns a completion function for the LineEditor. It examines
// the text typed up to the cursor and returns candidate completions for the
// last word, covering four contexts:
//
//   - Ollama model names and aliases: for `/ollama use`, `/ollama probe`,
//     `/ollama alias set` (third token position)
//   - Route @names: when input starts with "@"
//   - Slash command names: when the first token starts with "/"
//   - Workspace file/directory paths: for file-system commands
func (a *Agent) buildCompleter() func(string) []string {
	return func(line string) []string {
		tokens := strings.Fields(line)
		// Determine the word being completed: last token, or "" if line ends with space.
		word := ""
		if len(tokens) > 0 && !strings.HasSuffix(line, " ") {
			word = tokens[len(tokens)-1]
		}

		// @mention — complete registered route names.
		if strings.HasPrefix(word, "@") {
			prefix := word[1:]
			var matches []string
			if a.Routes != nil {
				for name := range a.Routes.Endpoints {
					if strings.HasPrefix(name, prefix) {
						matches = append(matches, "@"+name)
					}
				}
			}
			sortStrings(matches)
			return matches
		}

		// Slash command — complete command names.
		if len(tokens) == 1 && strings.HasPrefix(word, "/") {
			prefix := strings.ToLower(word[1:])
			var matches []string
			for name := range a.commands {
				if strings.HasPrefix(name, prefix) {
					matches = append(matches, "/"+name)
				}
			}
			sortStrings(matches)
			return matches
		}

		// Layer 1: subcommand completion (second token position).
		if len(tokens) >= 1 && strings.HasPrefix(tokens[0], "/") {
			cmdName := strings.ToLower(tokens[0][1:])
			// Fire when the user has typed the command and a space (no second token yet)
			// or is mid-way through typing the second token.
			atSubcmd := (len(tokens) == 1 && strings.HasSuffix(line, " ")) ||
				(len(tokens) == 2 && !strings.HasSuffix(line, " "))
			if atSubcmd {
				if cmd, ok := a.commands[cmdName]; ok && len(cmd.Subcommands) > 0 {
					prefix := strings.ToLower(word)
					var matches []string
					for _, sub := range cmd.Subcommands {
						if strings.HasPrefix(sub, prefix) {
							matches = append(matches, sub)
						}
					}
					sortStrings(matches)
					return matches
				}
			}
		}

		// Layer 2: argument value completion (third token position).
		// Checked before file-path switch; commands with ArgCompletion short-circuit here.
		if len(tokens) >= 2 && strings.HasPrefix(tokens[0], "/") {
			cmdName := strings.ToLower(tokens[0][1:])
			sub := strings.ToLower(tokens[1])
			atArg := (len(tokens) == 2 && strings.HasSuffix(line, " ")) ||
				(len(tokens) == 3 && !strings.HasSuffix(line, " "))
			if atArg {
				if cmd, ok := a.commands[cmdName]; ok && cmd.ArgCompletion != nil {
					if fn, ok := cmd.ArgCompletion[sub]; ok {
						candidates := fn(a)
						prefix := strings.ToLower(word)
						var matches []string
						for _, c := range candidates {
							if strings.HasPrefix(strings.ToLower(c), prefix) {
								matches = append(matches, c)
							}
						}
						sortStrings(matches)
						return matches
					}
				}
			}
		}

		// Ollama model / alias completion for subcommands that take a model name.
		if len(tokens) >= 2 {
			cmd := strings.ToLower(tokens[0])
			sub := strings.ToLower(tokens[1])
			needsModel := (cmd == "/ollama" && (sub == "use" || sub == "probe")) ||
				(cmd == "/ollama" && sub == "alias" && len(tokens) == 4) // alias set ALIAS <model>
			if needsModel {
				return a.modelAndAliasCandidates(word)
			}
		}

		// File-system path completion for workspace-aware commands.
		if len(tokens) >= 1 && a.Workspace != nil {
			cmd := strings.ToLower(tokens[0])

			// pathStart is the token index (counting from 0, where 0 is the
			// command itself) at which workspace path arguments begin.
			pathStart := -1
			onlyDirs := false
			var extFilter map[string]bool

			switch cmd {
			case "/files", "/file-tree", "/read-dir":
				pathStart = 1
				onlyDirs = true
			case "/read", "/write", "/attach":
				pathStart = 1
			case "/read-pdf":
				pathStart = 1
				extFilter = map[string]bool{".pdf": true}
			case "/search":
				pathStart = 2 // tokens[1] is the search pattern, not a path
			case "/pipeline":
				pathStart = 2 // tokens[1] is the confidence %, paths start at index 2
			case "/rag":
				if len(tokens) >= 2 && strings.ToLower(tokens[1]) == "ingest" {
					pathStart = 2 // /rag ingest PATH [PATH...]
				}
			}

			if pathStart >= 0 {
				// Current token position: the index of the word being completed.
				currentPos := len(tokens) - 1
				if strings.HasSuffix(line, " ") {
					currentPos = len(tokens)
				}
				if currentPos >= pathStart {
					if strings.HasPrefix(word, "s3://") {
						candidates := remotePathCandidates(word)
						sortStrings(candidates)
						return candidates
					}
					matches := workspacePathCandidates(a.Workspace.Root, word, onlyDirs, extFilter)
					sortStrings(matches)
					return matches
				}
			}
		}

		return nil
	}
}

/** workspacePathCandidates returns workspace-relative path completions for the
 * partial word being typed. It lists entries in the directory component of word,
 * filters to names beginning with the base component, skips hidden entries, and
 * appends "/" to directories. If onlyDirs is true only directories are returned.
 * extFilter (non-nil) further restricts files to those with the given extensions.
 * Candidates containing spaces are double-quoted. The workspace root acts as the
 * boundary: paths that escape it produce no results.
 *
 * Parameters:
 *   root      (string)          — absolute workspace root path.
 *   word      (string)          — partial path typed by the user (may be empty).
 *   onlyDirs  (bool)            — when true, exclude regular files.
 *   extFilter (map[string]bool) — allowed lowercase extensions; nil means any.
 *
 * Returns:
 *   []string — workspace-relative path candidates (unsorted).
 *
 * Example:
 *   matches := workspacePathCandidates("/home/user/proj", "har", false, nil)
 *   // returns ["harvey/"] if harvey/ exists under the workspace root
 */
func workspacePathCandidates(root, word string, onlyDirs bool, extFilter map[string]bool) []string {
	// Split word into its directory prefix and the base name being completed.
	dir, base := filepath.Split(word)

	absDir := filepath.Clean(filepath.Join(root, dir))

	// Enforce workspace boundary.
	rootSep := root
	if !strings.HasSuffix(rootSep, string(filepath.Separator)) {
		rootSep += string(filepath.Separator)
	}
	if absDir != root && !strings.HasPrefix(absDir, rootSep) {
		return nil
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue // skip hidden entries
		}
		if !strings.HasPrefix(name, base) {
			continue
		}
		if onlyDirs && !e.IsDir() {
			continue
		}
		if extFilter != nil && !e.IsDir() {
			if !extFilter[strings.ToLower(filepath.Ext(name))] {
				continue
			}
		}
		candidate := dir + name
		if e.IsDir() {
			candidate += "/"
		}
		if strings.ContainsAny(candidate, " \t") {
			candidate = `"` + candidate + `"`
		}
		matches = append(matches, candidate)
	}
	return matches
}

// modelAndAliasCandidates returns Ollama model names and defined aliases that
// start with prefix, sourced from the model cache (no live Ollama call needed).
func (a *Agent) modelAndAliasCandidates(prefix string) []string {
	var candidates []string
	seen := make(map[string]bool)

	// Aliases from config.
	for alias := range a.Config.ModelAliases {
		if strings.HasPrefix(alias, strings.ToLower(prefix)) {
			candidates = append(candidates, alias)
			seen[alias] = true
		}
	}

	// Model names from cache.
	if a.ModelCache != nil {
		if caps, err := a.ModelCache.All(); err == nil {
			for _, cap := range caps {
				if strings.HasPrefix(cap.Name, prefix) && !seen[cap.Name] {
					candidates = append(candidates, cap.Name)
				}
			}
		}
	}

	sortStrings(candidates)
	return candidates
}

/** sessionMemoryDigest prints dim startup hints when any knowledge silo
 * has an actionable state: unmined sessions, empty RAG store, or RAG disabled
 * while chunks exist. Prints nothing when everything is healthy. Non-fatal —
 * any failure loading the manifest or counting chunks is silently ignored.
 *
 * Parameters:
 *   a   (*Agent)    — the running agent (workspace, Rag, RagOn, Config must be set).
 *   out (io.Writer) — output destination.
 *
 * Returns:
 *   nothing — all errors are swallowed so startup is never blocked.
 *
 * Example:
 *   sessionMemoryDigest(a, out)  // called once after the ready line
 */
func sessionMemoryDigest(a *Agent, out io.Writer) {
	if a.Workspace == nil || !a.Config.Memory.Enabled {
		return
	}

	// Count unmined sessions via manifest.
	if a.Memory != nil && a.Memory.Store != nil {
		if manifest, err := LoadManifest(a.Memory.Store.Dir()); err == nil {
			sessDir := a.SessionsDir
			if sessDir == "" {
				sessDir = filepath.Join(a.Workspace.Root, harveySubdir, "sessions")
			}
			if unmined, err := manifest.UnminedSessions(sessDir); err == nil && len(unmined) > 0 {
				fmt.Fprint(out, dim(fmt.Sprintf("  %d session(s) unmined — /memory mine to extract learnings\n", len(unmined))))
			}
		}
	}

	// RAG store hints.
	entry := a.Config.Memory.ActiveRagStore()
	if entry == nil {
		return
	}
	if a.Rag == nil {
		return
	}
	n, err := a.Rag.Count()
	if err != nil {
		return
	}
	if n == 0 {
		fmt.Fprint(out, dim(fmt.Sprintf("  RAG store %q is empty — /rag ingest <files> to add knowledge\n", entry.Name)))
	} else if !a.RagOn {
		fmt.Fprint(out, dim(fmt.Sprintf("  RAG has %d chunk(s) but is off — /rag on to enable context injection\n", n)))
	}
}



/** remotePathCandidates returns tab-completion candidates for a partial s3:// URI.
 * It lists objects under the directory portion of word using the S3 backend.
 * Returns nil silently when credentials are absent, the URI is malformed, or
 * the scheme is not s3://.
 *
 * Parameters:
 *   word (string) — the partial URI being typed, e.g. "s3://bucket/docs/".
 *
 * Returns:
 *   []string — full s3:// URIs of matching objects; nil when completion is unavailable.
 *
 * Example:
 *   candidates := remotePathCandidates("s3://mybucket/docs/")
 *   // returns ["s3://mybucket/docs/spec.md", "s3://mybucket/docs/guide.md"]
 */
func remotePathCandidates(word string) []string {
	if !strings.HasPrefix(word, "s3://") {
		return nil
	}
	rest := word[5:] // after "s3://"
	slashIdx := strings.IndexByte(rest, '/')
	if slashIdx < 0 {
		return nil // still typing the bucket name
	}
	bucket := rest[:slashIdx]
	if bucket == "" {
		return nil
	}
	keyPart := rest[slashIdx+1:] // everything after s3://bucket/

	// List under the directory portion (up to the last '/').
	dir := ""
	base := keyPart
	if lastSlash := strings.LastIndexByte(keyPart, '/'); lastSlash >= 0 {
		dir = keyPart[:lastSlash+1]
		base = keyPart[lastSlash+1:]
	}
	listURI := "s3://" + bucket + "/" + dir

	s3r, err := newS3Reader()
	if err != nil {
		return nil
	}
	objects, err := s3r.List(context.Background(), listURI)
	if err != nil {
		return nil
	}
	var results []string
	for _, obj := range objects {
		key := strings.TrimPrefix(obj.URI, "s3://"+bucket+"/")
		if base == "" || strings.HasPrefix(key, dir+base) {
			results = append(results, obj.URI)
		}
	}
	return results
}
